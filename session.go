package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	errSyntax = errors.New("syntax")
	errLogout = errors.New("logout")
)

// session is one IMAP connection. box, readonly, seenID, and wake are
// guarded by Store.mu.
type session struct {
	srv        *Server
	conn       net.Conn
	br         *bufio.Reader
	bw         *bufio.Writer
	wmu        sync.Mutex
	id         int
	authed     bool
	user       string
	secure     bool
	box        *Mailbox
	readonly   bool
	seenID     uint64
	wake       chan struct{}
	enabled    map[string]struct{}
	savedUIDs  []uint32 // SEARCHRES result. UIDs, including ones later expunged.
	pers       *Personality
	fault      *fault
	capture    *bytes.Buffer
	compressed bool
}

type command struct {
	tag          string
	name         string
	uid          bool
	args         []Token
	fetch        []string
	changedSince *uint64
}

func (s *session) jmapBound() bool {
	return s.srv.bound.Jmap != "" || s.srv.bound.JmapTLS != ""
}

func (s *session) caps() []string {
	return s.pers.advertise(s.authed, s.secure, s.srv.hasCert, s.jmapBound())
}

func (s *session) sendLine(line string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.capture != nil {
		if _, err := io.WriteString(s.capture, line); err != nil {
			return err
		}
		if !strings.HasSuffix(line, "\n") {
			if _, err := io.WriteString(s.capture, "\r\n"); err != nil {
				return err
			}
		}
		return nil
	}
	if _, err := io.WriteString(s.bw, line); err != nil {
		return err
	}
	if !strings.HasSuffix(line, "\n") {
		if _, err := io.WriteString(s.bw, "\r\n"); err != nil {
			return err
		}
	}
	if err := s.bw.Flush(); err != nil {
		return err
	}
	s.srv.logLine("reply", s.id, line)
	return nil
}

func (s *session) sendRaw(b []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.capture != nil {
		_, err := s.capture.Write(b)
		return err
	}
	if _, err := s.bw.Write(b); err != nil {
		return err
	}
	if err := s.bw.Flush(); err != nil {
		return err
	}
	s.srv.logLine("reply", s.id, string(b))
	return nil
}

func (s *session) ok(tag, text string) error {
	return s.sendLine(tag + " OK " + text)
}

func (s *session) no(tag, text string) error {
	return s.sendLine(tag + " NO " + text)
}

func (s *session) bad(tag, text string) error {
	return s.sendLine(tag + " BAD " + text)
}

func (s *session) serve() {
	defer s.srv.store.deselect(s)
	var greet string
	if s.pers.GreetingCaps != nil && len(s.pers.GreetingCaps) == 0 {
		greet = "* OK " + s.pers.Greeting
	} else {
		greetCaps := s.caps()
		if len(s.pers.GreetingCaps) > 0 {
			greetCaps = s.pers.advertiseList(s.pers.GreetingCaps, false, s.secure, s.srv.hasCert, s.jmapBound())
		}
		greet = fmt.Sprintf("* OK [CAPABILITY %s] %s", strings.Join(greetCaps, " "), s.pers.Greeting)
	}
	if err := s.sendLine(greet); err != nil {
		return
	}
	for {
		cmd, err := s.readCommand()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			if cmd.tag != "" {
				_ = s.bad(cmd.tag, "syntax error")
			}
			return
		}
		if err := s.runCommand(cmd); err != nil {
			return
		}
	}
}

func (s *session) readCommand() (command, error) {
	p := &proto{br: s.br, send: s.sendLine}
	var cmd command
	if err := p.skipSP(); err != nil {
		return cmd, err
	}
	b, err := s.br.ReadByte()
	if err != nil {
		return cmd, err
	}
	if b == '\n' {
		return cmd, errSyntax
	}
	if b == '\r' {
		if _, err := s.br.ReadByte(); err != nil {
			return cmd, err
		}
		return cmd, errSyntax
	}
	if err := s.br.UnreadByte(); err != nil {
		return cmd, err
	}
	tag, err := p.readAtom()
	if err != nil {
		return cmd, err
	}
	cmd.tag = tag
	if err := p.skipSP(); err != nil {
		return cmd, err
	}
	name, err := p.readAtom()
	if err != nil {
		return cmd, err
	}
	cmd.name = strings.ToUpper(name)
	if cmd.name == "UID" {
		cmd.uid = true
		if err := p.skipSP(); err != nil {
			return cmd, err
		}
		name, err = p.readAtom()
		if err != nil {
			return cmd, err
		}
		cmd.name = strings.ToUpper(name)
		switch cmd.name {
		case "FETCH", "STORE", "SEARCH", "COPY", "MOVE", "XMOVE", "EXPUNGE", "SORT", "THREAD", "FILTER":
		default:
			return cmd, errSyntax
		}
	}
	if cmd.name == "FETCH" {
		if err := p.skipSP(); err != nil {
			return cmd, err
		}
		tok, err := p.readToken()
		if err != nil {
			return cmd, err
		}
		cmd.args = []Token{tok}
		items, err := p.readFetchItems()
		if err != nil {
			return cmd, err
		}
		items, changed, err := p.finishFetch(items)
		if err != nil {
			return cmd, err
		}
		cmd.fetch = items
		cmd.changedSince = changed
		return cmd, nil
	}
	args, err := p.readTokens()
	if err != nil {
		return cmd, err
	}
	cmd.args = args
	return cmd, nil
}

func (s *session) readRawLine() (string, error) {
	line, err := s.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (s *session) on(name string) bool {
	if s.enabled == nil {
		return false
	}
	_, ok := s.enabled[strings.ToUpper(name)]
	return ok
}

// searchNow is the frozen clock when freeze-clock is set, and the wall clock otherwise.
func (s *session) searchNow() time.Time {
	if t := s.srv.frozenTime(); t != nil {
		return *t
	}
	return time.Now()
}

func (s *session) advertiseCap(name string) bool {
	for _, c := range s.caps() {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}

// appendLimit is the positive integer on this connection's capability
// list. After login that list is the post-login list. One number applies
// to every mailbox.
func (s *session) appendLimit() (uint64, bool) {
	for _, c := range s.caps() {
		if n, ok := parseAppendLimit(c); ok {
			return n, true
		}
	}
	return 0, false
}

func (s *session) appendLimitValue() uint64 {
	n, _ := s.appendLimit()
	return n
}

// messageLimit is the positive integer on this connection's capability
// list. After login that list is the post-login list.
func (s *session) messageLimit() (uint64, bool) {
	for _, c := range s.caps() {
		if n, ok := parseMessageLimit(c); ok {
			return n, true
		}
	}
	return 0, false
}

func messageLimitText(n uint64, uid uint32, withUID bool) string {
	if withUID {
		return fmt.Sprintf("[MESSAGELIMIT %d %d]", n, uid)
	}
	return fmt.Sprintf("[MESSAGELIMIT %d]", n)
}

func reverseU32(ids []uint32) []uint32 {
	out := make([]uint32, len(ids))
	for i := range ids {
		out[i] = ids[len(ids)-1-i]
	}
	return out
}

func (s *session) flush() error {
	evs := s.srv.store.drain(s)
	gone := map[uint32]bool{}
	for _, e := range evs {
		if e.kind == "expunge" {
			gone[e.uid] = true
		}
	}
	for i := 0; i < len(evs); {
		e := evs[i]
		switch e.kind {
		case "expunge":
			if s.on("QRESYNC") {
				var uids []uint32
				for i < len(evs) && evs[i].kind == "expunge" {
					uids = append(uids, evs[i].uid)
					i++
				}
				if err := s.sendLine("* VANISHED " + formatUIDSet(uids)); err != nil {
					return err
				}
				continue
			}
			if err := s.sendLine(fmt.Sprintf("* %d EXPUNGE", e.seq)); err != nil {
				return err
			}
		case "exists":
			if err := s.sendLine(fmt.Sprintf("* %d EXISTS", e.exists)); err != nil {
				return err
			}
			if err := s.sendLine(fmt.Sprintf("* %d RECENT", e.recent)); err != nil {
				return err
			}
		case "flags":
			if gone[e.uid] {
				break
			}
			if err := s.sendLine(s.flagsFetch(e.seq, e.flags, e.uid, s.on("QRESYNC"), e.modseq, s.on("CONDSTORE"))); err != nil {
				return err
			}
		}
		i++
	}
	return nil
}

func (s *session) needAuth(tag string) bool {
	if s.authed {
		return true
	}
	_ = s.no(tag, "must authenticate first")
	return false
}

func (s *session) dispatch(cmd command) error {
	switch cmd.name {
	case "CAPABILITY":
		return s.cmdCapability(cmd)
	case "ID":
		return s.cmdID(cmd)
	case "NOOP":
		return s.ok(cmd.tag, "NOOP completed")
	case "CHECK":
		if !s.srv.store.selected(s) {
			return s.no(cmd.tag, "CHECK not selected")
		}
		return s.ok(cmd.tag, "CHECK completed")
	case "LOGOUT":
		if s.pers.LogoutBye {
			if err := s.sendLine("* BYE LOGOUT"); err != nil {
				return err
			}
		}
		if err := s.ok(cmd.tag, "LOGOUT completed"); err != nil {
			return err
		}
		return errLogout
	case "STARTTLS":
		return s.cmdStartTLS(cmd)
	case "COMPRESS":
		return s.cmdCompress(cmd)
	case "LOGIN":
		return s.cmdLogin(cmd)
	case "AUTHENTICATE":
		return s.cmdAuth(cmd)
	case "ENABLE":
		return s.cmdEnable(cmd)
	case "NAMESPACE":
		return s.cmdNamespace(cmd)
	case "LIST", "XLIST":
		return s.cmdList(cmd)
	case "SCAN":
		return s.cmdScan(cmd)
	case "LSUB":
		return s.cmdLsub(cmd)
	case "SUBSCRIBE":
		return s.cmdSubscribe(cmd)
	case "UNSUBSCRIBE":
		return s.cmdUnsubscribe(cmd)
	case "STATUS":
		return s.cmdStatus(cmd)
	case "CREATE":
		return s.cmdCreate(cmd)
	case "DELETE":
		return s.cmdDelete(cmd)
	case "RENAME":
		return s.cmdRename(cmd)
	case "SELECT", "EXAMINE":
		return s.cmdSelect(cmd)
	case "FETCH":
		return s.cmdFetch(cmd)
	case "STORE":
		return s.cmdStore(cmd)
	case "SEARCH":
		return s.cmdSearch(cmd)
	case "FILTER":
		return s.cmdFilter(cmd)
	case "ESEARCH":
		return s.cmdEsearch(cmd)
	case "SORT":
		return s.cmdSort(cmd)
	case "THREAD":
		return s.cmdThread(cmd)
	case "APPEND":
		return s.cmdAppend(cmd)
	case "COPY", "MOVE", "XMOVE":
		return s.cmdCopy(cmd)
	case "EXPUNGE":
		return s.cmdExpunge(cmd)
	case "CLOSE":
		return s.cmdClose(cmd)
	case "UNSELECT":
		return s.cmdUnselect(cmd)
	case "UNAUTHENTICATE":
		return s.cmdUnauthenticate(cmd)
	case "GETJMAPACCESS":
		return s.cmdGetJMAPAccess(cmd)
	case "GETQUOTA":
		return s.cmdGetQuota(cmd)
	case "GETQUOTAROOT":
		return s.cmdGetQuotaRoot(cmd)
	case "SETQUOTA":
		return s.cmdSetQuota(cmd)
	case "IDLE":
		return s.cmdIdle(cmd)
	case "SETACL", "DELETEACL", "GETACL", "LISTRIGHTS", "MYRIGHTS":
		return s.cmdACL(cmd)
	default:
		return s.bad(cmd.tag, "unknown command")
	}
}

// aclArgCount is the number of astring arguments each ACL command takes.
// idAt is the identifier argument, or -1 when the command has none.
func aclArgCount(name string) (n int, idAt int) {
	switch name {
	case "SETACL":
		return 3, 1
	case "DELETEACL", "LISTRIGHTS":
		return 2, 1
	case "GETACL", "MYRIGHTS":
		return 1, -1
	default:
		return -1, -1
	}
}

// aclRightsOK accepts an optional leading + or -, then zero or more of the
// RFC 4314 standard rights. The empty string, "+", and "-" are valid.
// A repeated letter is valid. Any other character is not.
func aclRightsOK(s string) bool {
	i := 0
	if s != "" && (s[0] == '+' || s[0] == '-') {
		i = 1
	}
	for ; i < len(s); i++ {
		switch s[i] {
		case 'l', 'r', 's', 'w', 'i', 'p', 'k', 'x', 't', 'e', 'a', 'c', 'd':
		default:
			return false
		}
	}
	return true
}

func aclArgsOK(name string, args []Token) bool {
	n, idAt := aclArgCount(name)
	if n < 0 || len(args) != n {
		return false
	}
	for i, t := range args {
		v, ok := astring(t)
		if !ok {
			return false
		}
		if i == idAt && v == "" {
			return false
		}
		if name == "SETACL" && i == 2 && !aclRightsOK(v) {
			return false
		}
	}
	return true
}

// cmdACL recognizes the five RFC 4314 commands. Valid arguments are tagged
// NO and nothing is stored. ACL is not advertised.
func (s *session) cmdACL(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if !aclArgsOK(cmd.name, cmd.args) {
		return s.bad(cmd.tag, cmd.name+" syntax")
	}
	return s.no(cmd.tag, "valid syntax not implemented")
}

func (s *session) cmdCapability(cmd command) error {
	if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "CAPABILITY syntax")
	}
	if err := s.sendLine("* CAPABILITY " + strings.Join(s.caps(), " ")); err != nil {
		return err
	}
	return s.ok(cmd.tag, "CAPABILITY completed")
}

// cmdID answers with one canned server identity. The client field list
// is not echoed. A personality that does not advertise ID is tagged BAD.
func (s *session) cmdID(cmd command) error {
	if !s.advertiseCap("ID") {
		return s.bad(cmd.tag, "ID not supported")
	}
	if !idParamsOK(cmd.args) {
		return s.bad(cmd.tag, "ID syntax")
	}
	if err := s.sendLine(`* ID ("name" "fakeimap")`); err != nil {
		return err
	}
	return s.ok(cmd.tag, "ID completed")
}

// idParamsOK accepts NIL or one parenthesized field/value list, and
// nothing after it. Two lists, or no argument, are not that form.
func idParamsOK(args []Token) bool {
	if len(args) == 1 && args[0].Kind == "atom" && strings.EqualFold(args[0].Val, "NIL") {
		return true
	}
	if len(args) == 0 || args[0].Kind != "lparen" {
		return false
	}
	end := -1
	for i, t := range args {
		if t.Kind == "lparen" {
			if i != 0 {
				return false
			}
			continue
		}
		if t.Kind == "rparen" {
			end = i
			break
		}
	}
	if end != len(args)-1 {
		return false
	}
	inner := args[1:end]
	if len(inner)%2 != 0 {
		return false
	}
	for i, t := range inner {
		val, ok := astring(t)
		if !ok {
			return false
		}
		if i%2 == 0 && t.Kind == "atom" && strings.EqualFold(val, "NIL") {
			return false
		}
	}
	return true
}

func (s *session) cmdCompress(cmd command) error {
	if len(cmd.args) != 1 {
		return s.bad(cmd.tag, "COMPRESS syntax")
	}
	algo, ok := astring(cmd.args[0])
	if !ok || algo == "" {
		return s.bad(cmd.tag, "COMPRESS syntax")
	}
	if !strings.EqualFold(algo, "DEFLATE") {
		return s.bad(cmd.tag, "COMPRESS unknown algorithm")
	}
	if s.compressed || !s.advertiseCap("COMPRESS=DEFLATE") {
		return s.no(cmd.tag, "COMPRESS failed")
	}
	if err := s.ok(cmd.tag, "COMPRESS completed"); err != nil {
		return err
	}
	return s.startDeflate()
}

func (s *session) cmdStartTLS(cmd command) error {
	if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "STARTTLS syntax")
	}
	if s.secure || !s.srv.hasCert {
		return s.bad(cmd.tag, "STARTTLS not available")
	}
	if s.authed {
		return s.bad(cmd.tag, "STARTTLS already authenticated")
	}
	if err := s.ok(cmd.tag, "Begin TLS negotiation now"); err != nil {
		return err
	}
	var pre []byte
	if n := s.br.Buffered(); n > 0 {
		pre = make([]byte, n)
		if _, err := io.ReadFull(s.br, pre); err != nil {
			return err
		}
	}
	tc := s.srv.tlsServerFor(&prefixConn{Conn: s.conn, pre: pre}, s.id, "STARTTLS")
	if err := tc.Handshake(); err != nil {
		return err
	}
	s.wmu.Lock()
	s.conn = tc
	s.br = bufio.NewReader(tc)
	s.bw = bufio.NewWriter(tc)
	s.secure = true
	s.wmu.Unlock()
	return nil
}

func (s *session) loginOK(tag, cmd string, user, pass string) error {
	if !s.srv.store.check(user, pass) {
		return s.no(tag, "[AUTHENTICATIONFAILED] "+cmd+" failed")
	}
	s.user = user
	return s.authDone(tag, cmd)
}

func (s *session) authDone(tag, cmd string) error {
	s.authed = true
	return s.sendLine(fmt.Sprintf("%s OK [CAPABILITY %s] %s completed", tag, strings.Join(s.caps(), " "), cmd))
}

func (s *session) cmdLogin(cmd command) error {
	if s.authed {
		return s.bad(cmd.tag, "LOGIN already authenticated")
	}
	if len(cmd.args) != 2 {
		return s.bad(cmd.tag, "LOGIN syntax")
	}
	user, ok1 := astring(cmd.args[0])
	pass, ok2 := astring(cmd.args[1])
	if !ok1 || !ok2 {
		return s.bad(cmd.tag, "LOGIN syntax")
	}
	return s.loginOK(cmd.tag, "LOGIN", user, pass)
}

func (s *session) cmdAuth(cmd command) error {
	if s.authed {
		return s.bad(cmd.tag, "AUTHENTICATE already authenticated")
	}
	if len(cmd.args) < 1 || len(cmd.args) > 2 {
		return s.bad(cmd.tag, "AUTHENTICATE syntax")
	}
	mech, ok := astring(cmd.args[0])
	if !ok || !s.mechAdvertised(mech) {
		return s.no(cmd.tag, "AUTHENTICATE mechanism not available")
	}
	var ir string
	hasIR := false
	if len(cmd.args) == 2 {
		ir, ok = astring(cmd.args[1])
		if !ok {
			return s.bad(cmd.tag, "AUTHENTICATE syntax")
		}
		hasIR = true
	}
	switch strings.ToUpper(mech) {
	case "PLAIN":
		return s.authPlain(cmd.tag, hasIR, ir)
	case "LOGIN":
		return s.authLogin(cmd.tag, hasIR, ir)
	case "CRAM-MD5":
		return s.authCRAM(cmd.tag, hasIR, ir)
	case "SCRAM-SHA-1", "SCRAM-SHA-256", "SCRAM-SHA-1-PLUS", "SCRAM-SHA-256-PLUS":
		return s.authSCRAM(cmd.tag, strings.ToUpper(mech), hasIR, ir)
	case "EXTERNAL":
		return s.authExternal(cmd.tag, hasIR, ir)
	default:
		return s.no(cmd.tag, "AUTHENTICATE mechanism not available")
	}
}

func (s *session) mechAdvertised(mech string) bool {
	want := "AUTH=" + strings.ToUpper(mech)
	return capImplemented(want) && s.pers.lists(want)
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func plainCreds(b []byte) (string, string, bool) {
	parts := bytes.Split(b, []byte{0})
	if len(parts) != 3 {
		return "", "", false
	}
	zid, cid, pw := string(parts[0]), string(parts[1]), string(parts[2])
	if cid == "" || (zid != "" && zid != cid) {
		return "", "", false
	}
	return cid, pw, true
}

func (s *session) cmdEnable(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	for _, t := range cmd.args {
		if _, ok := astring(t); !ok {
			return s.bad(cmd.tag, "ENABLE syntax")
		}
	}
	recognized := false
	var newly []string
	add := func(name string) {
		name = strings.ToUpper(name)
		if !s.advertiseCap(name) {
			return
		}
		recognized = true
		if s.on(name) {
			return
		}
		if s.enabled == nil {
			s.enabled = map[string]struct{}{}
		}
		s.enabled[name] = struct{}{}
		newly = append(newly, name)
	}
	for _, t := range cmd.args {
		name, _ := astring(t)
		switch strings.ToUpper(name) {
		case "QRESYNC":
			add("CONDSTORE")
			add("QRESYNC")
		case "CONDSTORE":
			add("CONDSTORE")
		case "UTF8=ACCEPT":
			add("UTF8=ACCEPT")
		}
	}
	if recognized {
		line := "* ENABLED"
		if len(newly) > 0 {
			line += " " + strings.Join(newly, " ")
		}
		if err := s.sendLine(line); err != nil {
			return err
		}
	}
	return s.ok(cmd.tag, "ENABLE completed")
}

// boxIn turns a wire mailbox name into the Unicode name the store keeps.
// Before UTF8=ACCEPT, the wire form is modified UTF-7.
func (s *session) boxIn(name string) (string, error) {
	if s.on("UTF8=ACCEPT") {
		if name != "" && !utf8.ValidString(name) {
			return "", fmt.Errorf("bad mailbox name")
		}
		return name, nil
	}
	return decodeMUTF7(name)
}

// boxOut turns a stored mailbox name into the form this connection speaks.
func (s *session) boxOut(name string) string {
	if s.on("UTF8=ACCEPT") {
		return name
	}
	return encodeMUTF7(name)
}

func (s *session) announceCondstore(how string) error {
	if s.on("CONDSTORE") || !s.advertiseCap("CONDSTORE") {
		return nil
	}
	hi, err := s.srv.store.highestModseq(s)
	if err != nil {
		return err
	}
	if err := s.sendLine(fmt.Sprintf("* OK [HIGHESTMODSEQ %d] CONDSTORE enabled by %s", hi, how)); err != nil {
		return err
	}
	if s.enabled == nil {
		s.enabled = map[string]struct{}{}
	}
	s.enabled["CONDSTORE"] = struct{}{}
	return nil
}

func (s *session) cmdNamespace(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "NAMESPACE syntax")
	}
	if err := s.sendLine(s.pers.namespaceLine()); err != nil {
		return err
	}
	return s.ok(cmd.tag, "NAMESPACE completed")
}

// quotaLine is the * QUOTA report. STORAGE is always present. MESSAGE is
// present only when QUOTA=RES-MESSAGE is listed.
func (s *session) quotaLine() string {
	return s.quotaLineFrom(s.srv.store.quotaNow())
}

func (s *session) quotaLineFrom(q quotaSnap) string {
	line := fmt.Sprintf("* QUOTA ROOT (STORAGE %d %d", q.storageUse, q.storageLim)
	if s.advertiseCap("QUOTA=RES-MESSAGE") {
		line += fmt.Sprintf(" MESSAGE %d %d", q.messageUse, q.messageLim)
	}
	return line + ")"
}

func (s *session) cmdGetQuota(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if !s.advertiseCap("QUOTA") {
		return s.bad(cmd.tag, "GETQUOTA not supported")
	}
	if len(cmd.args) != 1 {
		return s.bad(cmd.tag, "GETQUOTA syntax")
	}
	root, ok := astring(cmd.args[0])
	if !ok {
		return s.bad(cmd.tag, "GETQUOTA syntax")
	}
	if !strings.EqualFold(root, "ROOT") {
		return s.no(cmd.tag, "GETQUOTA no such root")
	}
	if err := s.sendLine(s.quotaLine()); err != nil {
		return err
	}
	return s.ok(cmd.tag, "GETQUOTA completed")
}

func (s *session) cmdGetQuotaRoot(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if !s.advertiseCap("QUOTA") {
		return s.bad(cmd.tag, "GETQUOTAROOT not supported")
	}
	if len(cmd.args) != 1 {
		return s.bad(cmd.tag, "GETQUOTAROOT syntax")
	}
	name, ok := astring(cmd.args[0])
	if !ok {
		return s.bad(cmd.tag, "GETQUOTAROOT syntax")
	}
	name, err := s.boxIn(name)
	if err != nil {
		return s.bad(cmd.tag, "GETQUOTAROOT syntax")
	}
	cn, found := s.srv.store.canonicalBox(name)
	if !found {
		return s.no(cmd.tag, "GETQUOTAROOT no such mailbox")
	}
	if err := s.sendLine("* QUOTAROOT " + imapAtom(s.boxOut(cn)) + " ROOT"); err != nil {
		return err
	}
	if err := s.sendLine(s.quotaLine()); err != nil {
		return err
	}
	return s.ok(cmd.tag, "GETQUOTAROOT completed")
}

type quotaPair struct {
	name  string
	limit uint64
}

// parseQuotaSet accepts ROOT plus one parenthesized list of resource and
// number pairs. An empty list is not a list of pairs.
func parseQuotaSet(args []Token) (string, []quotaPair, error) {
	if len(args) < 3 {
		return "", nil, errSyntax
	}
	root, ok := astring(args[0])
	if !ok || args[1].Kind != "lparen" {
		return "", nil, errSyntax
	}
	var pairs []quotaPair
	i := 2
	for i < len(args) {
		if args[i].Kind == "rparen" {
			if i != len(args)-1 || len(pairs) == 0 {
				return "", nil, errSyntax
			}
			return root, pairs, nil
		}
		name, ok := astring(args[i])
		if !ok || i+1 >= len(args) {
			return "", nil, errSyntax
		}
		raw, ok := astring(args[i+1])
		if !ok || raw == "" {
			return "", nil, errSyntax
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return "", nil, errSyntax
		}
		pairs = append(pairs, quotaPair{name: name, limit: n})
		i += 2
	}
	return "", nil, errSyntax
}

func (s *session) cmdSetQuota(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if !s.advertiseCap("QUOTASET") {
		return s.bad(cmd.tag, "SETQUOTA not supported")
	}
	root, pairs, err := parseQuotaSet(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "SETQUOTA syntax")
	}
	if !strings.EqualFold(root, "ROOT") {
		return s.no(cmd.tag, "SETQUOTA no such root")
	}
	var storage, message uint64
	var setStorage, setMessage bool
	for _, p := range pairs {
		switch strings.ToUpper(p.name) {
		case "STORAGE":
			storage = p.limit
			setStorage = true
		case "MESSAGE":
			if !s.advertiseCap("QUOTA=RES-MESSAGE") {
				return s.bad(cmd.tag, "SETQUOTA syntax")
			}
			message = p.limit
			setMessage = true
		default:
			return s.bad(cmd.tag, "SETQUOTA syntax")
		}
	}
	if err := s.sendLine(s.quotaLineFrom(s.srv.store.applyQuota(setStorage, storage, setMessage, message))); err != nil {
		return err
	}
	return s.ok(cmd.tag, "SETQUOTA completed")
}

func (s *session) cmdList(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	word := "LIST"
	if cmd.name == "XLIST" {
		word = "XLIST"
		if !s.advertiseCap("XLIST") {
			return s.bad(cmd.tag, "XLIST not supported")
		}
		if !twoAstrings(cmd.args) {
			return s.bad(cmd.tag, "XLIST syntax")
		}
	}
	parsed, err := parseListArgs(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, word+" syntax")
	}
	if parsed.extended && !s.advertiseCap("LIST-EXTENDED") {
		return s.bad(cmd.tag, word+" syntax")
	}
	if parsed.children && !s.advertiseCap("CHILDREN") {
		return s.bad(cmd.tag, word+" syntax")
	}
	if parsed.special && !s.advertiseCap("SPECIAL-USE") {
		return s.bad(cmd.tag, word+" syntax")
	}
	if parsed.hasStatus && !s.advertiseCap("LIST-STATUS") {
		return s.bad(cmd.tag, word+" syntax")
	}
	ref, err := s.boxIn(parsed.ref)
	if err != nil {
		return s.bad(cmd.tag, word+" syntax")
	}
	pat, err := s.boxIn(parsed.pat)
	if err != nil {
		return s.bad(cmd.tag, word+" syntax")
	}
	parsed.ref, parsed.pat = ref, pat
	q := listQuery{
		extended:       parsed.extended,
		specialOnly:    parsed.specialOnly,
		showSpecial:    s.advertiseCap("SPECIAL-USE"),
		wantStatus:     parsed.hasStatus,
		status:         parsed.status,
		appendLimit:    s.appendLimitValue(),
		statusSize:     s.advertiseCap("STATUS=SIZE"),
		user:           s.user,
		subscribedOnly: parsed.subscribed,
		markSubscribed: parsed.subscribed,
	}
	entries, delim, _, err := s.srv.store.list(parsed.ref, parsed.pat, q)
	if err != nil {
		if err.Error() == "bad status item" {
			return s.bad(cmd.tag, word+" bad item")
		}
		return s.bad(cmd.tag, word+" syntax")
	}
	for _, e := range entries {
		wn := s.boxOut(e.name)
		if err := s.sendLine(fmt.Sprintf("* %s %s %s %s", word, e.flags, imapString(delim), imapAtom(wn))); err != nil {
			return err
		}
		if e.status != nil {
			if err := s.sendLine(formatStatus(wn, e.status)); err != nil {
				return err
			}
		}
	}
	return s.ok(cmd.tag, word+" completed")
}

// cmdScan returns mailbox names from the same reference and pattern as LIST
// when the name contains the string. It does not read message text.
func (s *session) cmdScan(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if !s.advertiseCap("SCAN") {
		return s.bad(cmd.tag, "SCAN not supported")
	}
	if len(cmd.args) != 3 {
		return s.bad(cmd.tag, "SCAN syntax")
	}
	ref, ok1 := astring(cmd.args[0])
	pat, ok2 := astring(cmd.args[1])
	needle, ok3 := astring(cmd.args[2])
	if !ok1 || !ok2 || !ok3 {
		return s.bad(cmd.tag, "SCAN syntax")
	}
	ref, err := s.boxIn(ref)
	if err != nil {
		return s.bad(cmd.tag, "SCAN syntax")
	}
	pat, err = s.boxIn(pat)
	if err != nil {
		return s.bad(cmd.tag, "SCAN syntax")
	}
	entries, _, _, err := s.srv.store.list(ref, pat, listQuery{})
	if err != nil {
		return s.bad(cmd.tag, "SCAN syntax")
	}
	fold := strings.ToLower(needle)
	for _, e := range entries {
		wn := s.boxOut(e.name)
		if needle != "" && !strings.Contains(strings.ToLower(wn), fold) {
			continue
		}
		if err := s.sendLine("* SCAN " + imapAtom(wn)); err != nil {
			return err
		}
	}
	return s.ok(cmd.tag, "SCAN completed")
}

func (s *session) cmdLsub(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 2 {
		return s.bad(cmd.tag, "LSUB syntax")
	}
	ref, ok1 := astring(cmd.args[0])
	pat, ok2 := astring(cmd.args[1])
	if !ok1 || !ok2 {
		return s.bad(cmd.tag, "LSUB syntax")
	}
	ref, err := s.boxIn(ref)
	if err != nil {
		return s.bad(cmd.tag, "LSUB syntax")
	}
	pat, err = s.boxIn(pat)
	if err != nil {
		return s.bad(cmd.tag, "LSUB syntax")
	}
	entries, delim, _, err := s.srv.store.list(ref, pat, listQuery{user: s.user, subscribedOnly: true})
	if err != nil {
		return s.bad(cmd.tag, "LSUB syntax")
	}
	for _, e := range entries {
		if err := s.sendLine(fmt.Sprintf("* LSUB %s %s %s", e.flags, imapString(delim), imapAtom(s.boxOut(e.name)))); err != nil {
			return err
		}
	}
	return s.ok(cmd.tag, "LSUB completed")
}

func (s *session) mailboxArg(cmd command) (string, error) {
	if len(cmd.args) != 1 {
		return "", fmt.Errorf("bad")
	}
	name, ok := astring(cmd.args[0])
	if !ok {
		return "", fmt.Errorf("bad")
	}
	return s.boxIn(name)
}

func (s *session) cmdSubscribe(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	name, err := s.mailboxArg(cmd)
	if err != nil {
		return s.bad(cmd.tag, "SUBSCRIBE syntax")
	}
	if err := s.srv.store.subscribe(s.user, name); err != nil {
		return s.no(cmd.tag, "SUBSCRIBE failed")
	}
	return s.ok(cmd.tag, "SUBSCRIBE completed")
}

func (s *session) cmdUnsubscribe(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	name, err := s.mailboxArg(cmd)
	if err != nil {
		return s.bad(cmd.tag, "UNSUBSCRIBE syntax")
	}
	if err := s.srv.store.unsubscribe(s.user, name); err != nil {
		return s.no(cmd.tag, "UNSUBSCRIBE failed")
	}
	return s.ok(cmd.tag, "UNSUBSCRIBE completed")
}

type parsedList struct {
	ref, pat    string
	extended    bool
	subscribed  bool
	specialOnly bool
	children    bool
	special     bool
	hasStatus   bool
	status      []string
}

func twoAstrings(args []Token) bool {
	if len(args) != 2 {
		return false
	}
	_, ok1 := astring(args[0])
	_, ok2 := astring(args[1])
	return ok1 && ok2
}

func parseListArgs(args []Token) (parsedList, error) {
	var p parsedList
	if len(args) == 0 {
		return p, fmt.Errorf("bad")
	}
	i := 0
	if args[0].Kind == "lparen" {
		p.extended = true
		i = 1
		for {
			if i >= len(args) {
				return p, fmt.Errorf("bad")
			}
			if args[i].Kind == "rparen" {
				i++
				break
			}
			if args[i].Kind != "atom" {
				return p, fmt.Errorf("bad")
			}
			switch strings.ToUpper(args[i].Val) {
			case "SUBSCRIBED":
				p.subscribed = true
			case "SPECIAL-USE":
				p.specialOnly = true
				p.special = true
			default:
				return p, fmt.Errorf("bad")
			}
			i++
		}
	}
	if i+1 >= len(args) {
		return p, fmt.Errorf("bad")
	}
	ref, ok1 := astring(args[i])
	pat, ok2 := astring(args[i+1])
	if !ok1 || !ok2 {
		return p, fmt.Errorf("bad")
	}
	p.ref, p.pat = ref, pat
	i += 2
	if i == len(args) {
		return p, nil
	}
	p.extended = true
	if args[i].Kind != "atom" || !strings.EqualFold(args[i].Val, "RETURN") {
		return p, fmt.Errorf("bad")
	}
	i++
	if i >= len(args) || args[i].Kind != "lparen" {
		return p, fmt.Errorf("bad")
	}
	i++
	for {
		if i >= len(args) {
			return p, fmt.Errorf("bad")
		}
		if args[i].Kind == "rparen" {
			i++
			break
		}
		if args[i].Kind != "atom" {
			return p, fmt.Errorf("bad")
		}
		switch strings.ToUpper(args[i].Val) {
		case "CHILDREN":
			p.children = true
			i++
		case "SPECIAL-USE":
			p.special = true
			i++
		case "STATUS":
			i++
			if i >= len(args) || args[i].Kind != "lparen" {
				return p, fmt.Errorf("bad")
			}
			i++
			for {
				if i >= len(args) {
					return p, fmt.Errorf("bad")
				}
				if args[i].Kind == "rparen" {
					i++
					break
				}
				if args[i].Kind != "atom" {
					return p, fmt.Errorf("bad")
				}
				p.status = append(p.status, args[i].Val)
				i++
			}
			p.hasStatus = true
		default:
			return p, fmt.Errorf("bad")
		}
	}
	if i != len(args) {
		return p, fmt.Errorf("bad")
	}
	return p, nil
}

func formatStatus(name string, pairs [][2]string) string {
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p[0])
		b.WriteByte(' ')
		b.WriteString(p[1])
	}
	return fmt.Sprintf("* STATUS %s (%s)", imapString(name), b.String())
}

func (s *session) cmdStatus(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) < 3 || cmd.args[1].Kind != "lparen" || cmd.args[len(cmd.args)-1].Kind != "rparen" {
		return s.bad(cmd.tag, "STATUS syntax")
	}
	name, ok := astring(cmd.args[0])
	if !ok {
		return s.bad(cmd.tag, "STATUS syntax")
	}
	name, err := s.boxIn(name)
	if err != nil {
		return s.no(cmd.tag, "STATUS no such mailbox")
	}
	var items []string
	for _, t := range cmd.args[2 : len(cmd.args)-1] {
		if t.Kind != "atom" {
			return s.bad(cmd.tag, "STATUS syntax")
		}
		items = append(items, t.Val)
	}
	cn, pairs, err := s.srv.store.status(name, items, s.appendLimitValue(), s.advertiseCap("STATUS=SIZE"))
	if err != nil {
		if err.Error() == "bad status item" {
			return s.bad(cmd.tag, "STATUS bad item")
		}
		return s.no(cmd.tag, "STATUS no such mailbox")
	}
	if err := s.sendLine(formatStatus(s.boxOut(cn), pairs)); err != nil {
		return err
	}
	return s.ok(cmd.tag, "STATUS completed")
}

func (s *session) cmdCreate(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) < 1 {
		return s.bad(cmd.tag, "CREATE syntax")
	}
	name, ok := astring(cmd.args[0])
	if !ok {
		return s.bad(cmd.tag, "CREATE syntax")
	}
	name, err := s.boxIn(name)
	if err != nil {
		return s.no(cmd.tag, "CREATE failed")
	}
	var uses []string
	if len(cmd.args) > 1 {
		if !s.advertiseCap("CREATE-SPECIAL-USE") {
			return s.bad(cmd.tag, "CREATE syntax")
		}
		var err error
		uses, err = parseCreateUse(cmd.args[1:])
		if err != nil {
			return s.bad(cmd.tag, "CREATE syntax")
		}
	}
	if err := s.srv.store.create(name, uses); err != nil {
		return s.no(cmd.tag, "CREATE failed")
	}
	return s.ok(cmd.tag, "CREATE completed")
}

func parseCreateUse(args []Token) ([]string, error) {
	if len(args) < 4 || args[0].Kind != "lparen" || args[len(args)-1].Kind != "rparen" {
		return nil, fmt.Errorf("bad")
	}
	inner := args[1 : len(args)-1]
	if len(inner) < 3 || inner[0].Kind != "atom" || !strings.EqualFold(inner[0].Val, "USE") || inner[1].Kind != "lparen" || inner[len(inner)-1].Kind != "rparen" {
		return nil, fmt.Errorf("bad")
	}
	var uses []string
	for _, t := range inner[2 : len(inner)-1] {
		if t.Kind != "atom" {
			return nil, fmt.Errorf("bad")
		}
		uses = append(uses, t.Val)
	}
	return uses, nil
}

func (s *session) cmdDelete(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 1 {
		return s.bad(cmd.tag, "DELETE syntax")
	}
	name, ok := astring(cmd.args[0])
	if !ok {
		return s.bad(cmd.tag, "DELETE syntax")
	}
	name, err := s.boxIn(name)
	if err != nil {
		return s.no(cmd.tag, "DELETE failed")
	}
	if err := s.srv.store.delete(s, name); err != nil {
		return s.no(cmd.tag, "DELETE failed")
	}
	return s.ok(cmd.tag, "DELETE completed")
}

func (s *session) cmdRename(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 2 {
		return s.bad(cmd.tag, "RENAME syntax")
	}
	a, ok1 := astring(cmd.args[0])
	b, ok2 := astring(cmd.args[1])
	if !ok1 || !ok2 {
		return s.bad(cmd.tag, "RENAME syntax")
	}
	a, err := s.boxIn(a)
	if err != nil {
		return s.no(cmd.tag, "RENAME failed")
	}
	b, err = s.boxIn(b)
	if err != nil {
		return s.no(cmd.tag, "RENAME failed")
	}
	if err := s.srv.store.rename(a, b); err != nil {
		return s.no(cmd.tag, "RENAME failed")
	}
	return s.ok(cmd.tag, "RENAME completed")
}

func (s *session) cmdSelect(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	name, cond, qr, err := parseSelectArgs(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "syntax")
	}
	name, err = s.boxIn(name)
	if err != nil {
		return s.no(cmd.tag, cmd.name+" no such mailbox")
	}
	if cond || qr != nil {
		if !s.advertiseCap("CONDSTORE") && !s.advertiseCap("QRESYNC") {
			return s.bad(cmd.tag, "syntax")
		}
	}
	if cond && !s.advertiseCap("CONDSTORE") {
		return s.bad(cmd.tag, "syntax")
	}
	if qr != nil && !s.advertiseCap("QRESYNC") {
		return s.bad(cmd.tag, "syntax")
	}
	if qr != nil && !s.on("QRESYNC") {
		return s.bad(cmd.tag, "QRESYNC not enabled")
	}
	if qr != nil && qr.known != nil && qr.known.dollar {
		if !s.advertiseCap("SEARCHRES") {
			return s.bad(cmd.tag, "syntax")
		}
		ids := append([]uint32(nil), s.savedUIDs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		qr.known = seqFromIDs(ids)
		if len(ids) == 0 {
			qr.knownRaw = ""
		} else {
			qr.knownRaw = formatUIDSet(ids)
		}
	}
	info, err := s.srv.store.selectBox(s, name, cmd.name == "EXAMINE", qr)
	if err != nil {
		return s.no(cmd.tag, cmd.name+" no such mailbox")
	}
	if cond {
		if s.enabled == nil {
			s.enabled = map[string]struct{}{}
		}
		s.enabled["CONDSTORE"] = struct{}{}
	}
	if info.closed && s.advertiseCap("QRESYNC") {
		if err := s.sendLine("* OK [CLOSED] Ok"); err != nil {
			return err
		}
	}
	if err := s.writeSelect(info); err != nil {
		return err
	}
	if qr != nil && info.vanished != "" {
		if err := s.sendLine("* VANISHED (EARLIER) " + info.vanished); err != nil {
			return err
		}
	}
	for _, m := range info.changed {
		if err := s.sendLine(s.flagsFetch(m.seq, m.flags, m.uid, true, m.modseq, true)); err != nil {
			return err
		}
	}
	mode := "READ-WRITE"
	if info.readOnly {
		mode = "READ-ONLY"
	}
	return s.sendLine(fmt.Sprintf("%s OK [%s] %s completed", cmd.tag, mode, cmd.name))
}

func parseSelectArgs(args []Token) (string, bool, *qresyncSel, error) {
	if len(args) == 0 {
		return "", false, nil, fmt.Errorf("bad")
	}
	name, ok := astring(args[0])
	if !ok {
		return "", false, nil, fmt.Errorf("bad")
	}
	if len(args) == 1 {
		return name, false, nil, nil
	}
	if args[1].Kind != "lparen" || args[len(args)-1].Kind != "rparen" {
		return "", false, nil, fmt.Errorf("bad")
	}
	cond := false
	var qr *qresyncSel
	i := 2
	end := len(args) - 1
	for i < end {
		if args[i].Kind != "atom" {
			return "", false, nil, fmt.Errorf("bad")
		}
		switch strings.ToUpper(args[i].Val) {
		case "CONDSTORE":
			cond = true
			i++
		case "QRESYNC":
			if i+1 >= end || args[i+1].Kind != "lparen" {
				return "", false, nil, fmt.Errorf("bad")
			}
			i += 2
			if i+1 >= end || args[i].Kind != "atom" || args[i+1].Kind != "atom" {
				return "", false, nil, fmt.Errorf("bad")
			}
			uidv, err := strconv.ParseUint(args[i].Val, 10, 32)
			if err != nil || uidv == 0 {
				return "", false, nil, fmt.Errorf("bad")
			}
			modseq, err := strconv.ParseUint(args[i+1].Val, 10, 64)
			if err != nil {
				return "", false, nil, fmt.Errorf("bad")
			}
			i += 2
			sel := &qresyncSel{uidv: uint32(uidv), modseq: modseq}
			if i < end && args[i].Kind == "atom" {
				set, err := parseSeqSet(args[i].Val)
				if err != nil {
					return "", false, nil, fmt.Errorf("bad")
				}
				sel.known = set
				sel.knownRaw = args[i].Val
				i++
			}
			if i < end && args[i].Kind == "lparen" {
				depth := 1
				i++
				for i < end && depth > 0 {
					if args[i].Kind == "lparen" {
						depth++
					}
					if args[i].Kind == "rparen" {
						depth--
					}
					i++
				}
				if depth != 0 {
					return "", false, nil, fmt.Errorf("bad")
				}
			}
			if i >= end || args[i].Kind != "rparen" {
				return "", false, nil, fmt.Errorf("bad")
			}
			i++
			qr = sel
		default:
			return "", false, nil, fmt.Errorf("bad")
		}
	}
	if i != end {
		return "", false, nil, fmt.Errorf("bad")
	}
	return name, cond, qr, nil
}

func (s *session) writeSelect(info selectInfo) error {
	order := s.pers.SelectOrder
	if len(order) == 0 {
		order = []string{"FLAGS", "PERMANENTFLAGS", "EXISTS", "RECENT", "UNSEEN", "UIDVALIDITY", "UIDNEXT"}
	}
	flags := `\Answered \Flagged \Deleted \Seen \Draft \Recent`
	if s.pers.SelectFlags != "" {
		flags = s.pers.SelectFlags
	}
	perm := `\Answered \Flagged \Deleted \Seen \Draft \*`
	if s.pers.PermanentFlags != "" {
		perm = s.pers.PermanentFlags
	}
	for _, item := range order {
		var line string
		switch item {
		case "FLAGS":
			line = "* FLAGS (" + flags + ")"
		case "PERMANENTFLAGS":
			if info.readOnly {
				line = `* OK [PERMANENTFLAGS ()] Read-only mailbox`
			} else {
				line = "* OK [PERMANENTFLAGS (" + perm + ")] Permanent flags"
			}
		case "EXISTS":
			line = fmt.Sprintf("* %d EXISTS", info.exists)
		case "RECENT":
			line = fmt.Sprintf("* %d RECENT", info.recent)
		case "UNSEEN":
			if info.unseen <= 0 {
				continue
			}
			line = fmt.Sprintf("* OK [UNSEEN %d] First unseen", info.unseen)
		case "UIDVALIDITY":
			line = fmt.Sprintf("* OK [UIDVALIDITY %d] UIDs valid", info.uidValidity)
		case "UIDNEXT":
			line = fmt.Sprintf("* OK [UIDNEXT %d] Predicted next UID", info.uidNext)
		default:
			continue
		}
		if err := s.sendLine(line); err != nil {
			return err
		}
	}
	if s.advertiseCap("CONDSTORE") {
		if err := s.sendLine(fmt.Sprintf("* OK [HIGHESTMODSEQ %d] Ok", info.highest)); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) cmdFetch(cmd command) error {
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "FETCH not selected")
	}
	if len(cmd.args) != 1 || len(cmd.fetch) == 0 {
		return s.bad(cmd.tag, "FETCH syntax")
	}
	set, err := parseSeqSet(cmd.args[0].Val)
	if err != nil || (set.dollar && !s.advertiseCap("SEARCHRES")) {
		return s.bad(cmd.tag, "FETCH syntax")
	}
	if cmd.changedSince != nil || itemListed(cmd.fetch, "MODSEQ") {
		if !s.advertiseCap("CONDSTORE") {
			return s.bad(cmd.tag, "FETCH bad item")
		}
		if err := s.announceCondstore("FETCH MODSEQ"); err != nil {
			return err
		}
	}
	limit := uint64(0)
	if n, ok := s.messageLimit(); ok {
		limit = n
	}
	snaps, items, lowest, limited, err := s.srv.store.fetch(s, cmd.uid, set, cmd.fetch, cmd.changedSince, limit)
	if err != nil {
		if err.Error() == "not selected" {
			return s.no(cmd.tag, "FETCH not selected")
		}
		if errors.Is(err, errUnknownCTE) {
			return s.no(cmd.tag, "[UNKNOWN-CTE] FETCH failed")
		}
		return s.bad(cmd.tag, "FETCH bad item")
	}
	for _, sn := range snaps {
		use := items
		if s.pers.FetchSeenFlags && sn.seenEvent != 0 && !itemListed(items, "FLAGS") {
			use = append([]string{"FLAGS"}, items...)
		}
		if err := s.sendRaw([]byte(s.formatFetch(sn, use))); err != nil {
			return err
		}
	}
	// FLAGS was not requested. One extra response carries \Seen,
	// and UID only when this is a UID command.
	if !s.pers.FetchSeenFlags && !itemListed(items, "FLAGS") {
		var last uint64
		for _, sn := range snaps {
			if sn.seenEvent == 0 {
				continue
			}
			if err := s.sendLine(s.flagsFetch(sn.seq, sn.flags, sn.uid, cmd.uid || s.on("QRESYNC"), sn.modseq, s.on("CONDSTORE"))); err != nil {
				return err
			}
			if sn.seenEvent > last {
				last = sn.seenEvent
			}
		}
		if last > 0 {
			s.srv.store.advanceSeen(s, last)
		}
	}
	if err := s.flush(); err != nil {
		return err
	}
	if limited {
		return s.ok(cmd.tag, messageLimitText(limit, lowest, true)+" FETCH completed")
	}
	return s.ok(cmd.tag, "FETCH completed")
}

func (s *session) formatFetch(sn fetchSnap, items []string) string {
	type piece struct {
		rank int
		ord  int
		text string
	}
	var pieces []piece
	for i, it := range items {
		pieces = append(pieces, piece{s.itemRank(it), i, s.fetchItemText(it, sn)})
	}
	sort.SliceStable(pieces, func(a, b int) bool { return pieces[a].rank < pieces[b].rank })
	var b strings.Builder
	fmt.Fprintf(&b, "* %d FETCH (", sn.seq)
	for i, p := range pieces {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p.text)
	}
	b.WriteString(")\r\n")
	return b.String()
}

func (s *session) fetchItemText(item string, sn fetchSnap) string {
	switch item {
	case "FLAGS":
		return "FLAGS " + sn.flags
	case "UID":
		return fmt.Sprintf("UID %d", sn.uid)
	case "INTERNALDATE":
		return "INTERNALDATE " + imapString(formatInternalDate(sn.internal))
	case "SAVEDATE":
		return "SAVEDATE " + imapString(formatInternalDate(sn.saved))
	case "RFC822.SIZE":
		return fmt.Sprintf("RFC822.SIZE %d", sn.size)
	case "ENVELOPE":
		return "ENVELOPE " + envelope(sn.raw)
	case "BODY":
		return "BODY " + parseRoot(sn.raw).format(false)
	case "BODYSTRUCTURE":
		return "BODYSTRUCTURE " + parseRoot(sn.raw).format(true)
	case "RFC822":
		return "RFC822 " + literal(sn.raw)
	case "RFC822.HEADER":
		h, _ := splitHB(sn.raw)
		return "RFC822.HEADER " + literal(h)
	case "RFC822.TEXT":
		_, body := splitHB(sn.raw)
		return "RFC822.TEXT " + literal(body)
	case "MODSEQ":
		return fmt.Sprintf("MODSEQ (%d)", sn.modseq)
	case "PREVIEW":
		return "PREVIEW " + nstring(previewSnippet(sn.raw))
	}
	if strings.HasPrefix(item, "BODY[") || strings.HasPrefix(item, "BODY.PEEK[") {
		return bodySection(item, sn.raw, s.pers.FetchPeekBody)
	}
	if isBinarySize(item) {
		return binarySizeText(item, sn.raw)
	}
	if isBinaryFetch(item) {
		return binarySectionText(item, sn.raw)
	}
	return item + " NIL"
}

func binarySizeText(item string, raw []byte) string {
	section, ok := binaryItemSection(item)
	if !ok {
		return "BINARY.SIZE NIL"
	}
	name := "BINARY.SIZE[" + section + "]"
	b, st := decodeBinaryPart(raw, section)
	if st != binaryReady {
		return name + " NIL"
	}
	return fmt.Sprintf("%s %d", name, len(b))
}

func binarySectionText(item string, raw []byte) string {
	section, partial, ok := binarySpec(item)
	if !ok {
		return "BINARY NIL"
	}
	name := "BINARY[" + section + "]"
	b, st := decodeBinaryPart(raw, section)
	if st != binaryReady {
		return name + " NIL"
	}
	if partial != "" {
		start, _, _ := splitBinaryPartial(partial)
		// Origin is <offset>. The client rejects <offset.length>.
		name += "<" + strconv.Itoa(start) + ">"
		var pok bool
		b, pok = applyPartial(b, partial)
		if !pok {
			return name + " NIL"
		}
	}
	return name + " " + literal(b)
}

func bodySection(item string, raw []byte, peekAsBody bool) string {
	respName := item
	if peekAsBody && strings.HasPrefix(item, "BODY.PEEK") {
		respName = "BODY" + item[len("BODY.PEEK"):]
	}
	specPart := item
	if strings.HasPrefix(item, "BODY.PEEK") {
		specPart = "BODY" + item[len("BODY.PEEK"):]
	}
	lb := strings.IndexByte(specPart, '[')
	rb := strings.IndexByte(specPart, ']')
	if lb < 0 || rb < lb {
		return respName + " NIL"
	}
	spec := specPart[lb+1 : rb]
	partial := ""
	if rb+1 < len(specPart) {
		partial = specPart[rb+1:]
	}
	b, ok := sectionBytes(raw, spec)
	if !ok {
		return respName + " NIL"
	}
	b, ok = applyPartial(b, partial)
	if !ok {
		return respName + " NIL"
	}
	return respName + " " + literal(b)
}

func (s *session) cmdStore(cmd command) error {
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "STORE not selected")
	}
	if len(cmd.args) < 3 {
		return s.bad(cmd.tag, "STORE syntax")
	}
	set, err := parseSeqSet(cmd.args[0].Val)
	if err != nil || (set.dollar && !s.advertiseCap("SEARCHRES")) {
		return s.bad(cmd.tag, "STORE syntax")
	}
	i := 1
	var unchanged *uint64
	if cmd.args[i].Kind == "lparen" {
		if !s.advertiseCap("CONDSTORE") {
			return s.bad(cmd.tag, "STORE syntax")
		}
		i++
		if i >= len(cmd.args) || !strings.EqualFold(cmd.args[i].Val, "UNCHANGEDSINCE") {
			return s.bad(cmd.tag, "STORE syntax")
		}
		i++
		if i >= len(cmd.args) || cmd.args[i].Kind != "atom" {
			return s.bad(cmd.tag, "STORE syntax")
		}
		n, nerr := strconv.ParseUint(cmd.args[i].Val, 10, 64)
		if nerr != nil {
			return s.bad(cmd.tag, "STORE syntax")
		}
		unchanged = &n
		i++
		if i >= len(cmd.args) || cmd.args[i].Kind != "rparen" {
			return s.bad(cmd.tag, "STORE syntax")
		}
		i++
		if err := s.announceCondstore("STORE UNCHANGEDSINCE"); err != nil {
			return err
		}
	}
	if i >= len(cmd.args) || cmd.args[i].Kind != "atom" {
		return s.bad(cmd.tag, "STORE syntax")
	}
	item := cmd.args[i].Val
	flags, next, err := parseFlagList(cmd.args, i+1)
	if err != nil || next != len(cmd.args) {
		return s.bad(cmd.tag, "STORE syntax")
	}
	limit := uint64(0)
	if n, ok := s.messageLimit(); ok {
		limit = n
	}
	snaps, modified, lowest, limited, err := s.srv.store.storeFlags(s, cmd.uid, set, item, flags, unchanged, limit)
	if err != nil {
		if err.Error() == "read-only" {
			return s.no(cmd.tag, "STORE read-only")
		}
		if err.Error() == "not selected" {
			return s.no(cmd.tag, "STORE not selected")
		}
		return s.bad(cmd.tag, "STORE syntax")
	}
	withUID := cmd.uid || s.on("QRESYNC")
	withMod := s.on("CONDSTORE")
	for _, sn := range snaps {
		if err := s.sendLine(s.flagsFetch(sn.seq, sn.flags, sn.uid, withUID, sn.modseq, withMod)); err != nil {
			return err
		}
	}
	if limited {
		return s.ok(cmd.tag, messageLimitText(limit, lowest, true)+" STORE completed")
	}
	if len(modified) > 0 {
		return s.sendLine(fmt.Sprintf("%s OK [MODIFIED %s] STORE completed", cmd.tag, formatUIDSet(modified)))
	}
	return s.ok(cmd.tag, "STORE completed")
}

func (s *session) flagsFetch(seq uint32, flags string, uid uint32, withUID bool, modseq uint64, withModseq bool) string {
	type piece struct {
		rank int
		text string
	}
	pieces := []piece{{s.itemRank("FLAGS"), "FLAGS " + flags}}
	if withUID {
		pieces = append(pieces, piece{s.itemRank("UID"), fmt.Sprintf("UID %d", uid)})
	}
	if withModseq {
		pieces = append(pieces, piece{s.itemRank("MODSEQ"), fmt.Sprintf("MODSEQ (%d)", modseq)})
	}
	sort.SliceStable(pieces, func(a, b int) bool { return pieces[a].rank < pieces[b].rank })
	var b strings.Builder
	fmt.Fprintf(&b, "* %d FETCH (", seq)
	for i, p := range pieces {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p.text)
	}
	b.WriteByte(')')
	return b.String()
}

// searchReturn is a SEARCH RETURN list. An empty list means ALL.
type searchReturn struct {
	on      bool
	min     bool
	max     bool
	count   bool
	all     bool
	partial bool
	save    bool
	a, b    int
	echo    string
}

func (r searchReturn) saveOnly() bool {
	return r.save && !r.min && !r.max && !r.count && !r.all && !r.partial
}

func parseSearchReturn(args []Token) (searchReturn, []Token, error) {
	var r searchReturn
	if len(args) == 0 || args[0].Kind != "atom" || !strings.EqualFold(args[0].Val, "RETURN") {
		return r, args, nil
	}
	r.on = true
	if len(args) < 2 || args[1].Kind != "lparen" {
		return r, nil, fmt.Errorf("bad")
	}
	i := 2
	for {
		if i >= len(args) {
			return r, nil, fmt.Errorf("bad")
		}
		if args[i].Kind == "rparen" {
			i++
			break
		}
		if args[i].Kind != "atom" {
			return r, nil, fmt.Errorf("bad")
		}
		switch strings.ToUpper(args[i].Val) {
		case "MIN":
			if r.min {
				return r, nil, fmt.Errorf("bad")
			}
			r.min = true
			i++
		case "MAX":
			if r.max {
				return r, nil, fmt.Errorf("bad")
			}
			r.max = true
			i++
		case "COUNT":
			if r.count {
				return r, nil, fmt.Errorf("bad")
			}
			r.count = true
			i++
		case "ALL":
			if r.all || r.partial {
				return r, nil, fmt.Errorf("bad")
			}
			r.all = true
			i++
		case "SAVE":
			if r.save || r.partial {
				return r, nil, fmt.Errorf("bad")
			}
			r.save = true
			i++
		case "PARTIAL":
			if r.partial || r.all || r.save {
				return r, nil, fmt.Errorf("bad")
			}
			i++
			if i >= len(args) || args[i].Kind != "atom" {
				return r, nil, fmt.Errorf("bad")
			}
			a, b, err := parsePartialRange(args[i].Val)
			if err != nil {
				return r, nil, err
			}
			r.partial = true
			r.a, r.b = a, b
			r.echo = args[i].Val
			i++
		default:
			return r, nil, fmt.Errorf("bad")
		}
	}
	if !r.min && !r.max && !r.count && !r.all && !r.partial && !r.save {
		r.all = true
	}
	return r, args[i:], nil
}

func parseNZ(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("bad")
	}
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	if s == "" || s[0] < '1' || s[0] > '9' {
		return 0, fmt.Errorf("bad")
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad")
		}
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n <= 0 {
		return 0, err
	}
	if neg {
		n = -n
	}
	return int(n), nil
}

func parsePartialRange(s string) (int, int, error) {
	loS, hiS, ok := strings.Cut(s, ":")
	if !ok || strings.Contains(hiS, ":") {
		return 0, 0, fmt.Errorf("bad")
	}
	lo, err := parseNZ(loS)
	if err != nil {
		return 0, 0, err
	}
	hi, err := parseNZ(hiS)
	if err != nil {
		return 0, 0, err
	}
	if (lo < 0) != (hi < 0) {
		return 0, 0, fmt.Errorf("bad")
	}
	return lo, hi, nil
}

// partialWindow is the 1-based inclusive slice of ids. A negative bound
// counts from the end. A window with no overlap is nil.
func partialWindow(ids []uint32, a, b int) []uint32 {
	n := len(ids)
	pos := func(v int) int {
		if v > 0 {
			return v
		}
		return n + v + 1
	}
	lo, hi := pos(a), pos(b)
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi < 1 || lo > n {
		return nil
	}
	if lo < 1 {
		lo = 1
	}
	if hi > n {
		hi = n
	}
	return ids[lo-1 : hi]
}

func formatEsearch(tag string, uid bool, ids []uint32, ret searchReturn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "* ESEARCH (TAG %s)", imapString(tag))
	if uid {
		b.WriteString(" UID")
	}
	if ret.count {
		fmt.Fprintf(&b, " COUNT %d", len(ids))
	}
	if len(ids) > 0 {
		if ret.min || ret.max {
			mn, mx := ids[0], ids[0]
			for _, id := range ids[1:] {
				if id < mn {
					mn = id
				}
				if id > mx {
					mx = id
				}
			}
			if ret.min {
				fmt.Fprintf(&b, " MIN %d", mn)
			}
			if ret.max {
				fmt.Fprintf(&b, " MAX %d", mx)
			}
		}
		if ret.all {
			b.WriteString(" ALL ")
			b.WriteString(formatUIDSet(ids))
		}
	}
	if ret.partial {
		fmt.Fprintf(&b, " PARTIAL (%s %s)", ret.echo, formatUIDSet(partialWindow(ids, ret.a, ret.b)))
	}
	return b.String()
}

// formatMultiEsearch is one mailbox of an ESEARCH IN result. The numbers are
// always UIDs. The caller skips a mailbox with no hits.
func formatMultiEsearch(tag, mbox string, uidv uint32, ids []uint32, ret searchReturn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "* ESEARCH (TAG %s MAILBOX %s UIDVALIDITY %d)", imapString(tag), imapString(mbox), uidv)
	b.WriteString(" UID")
	if ret.count {
		fmt.Fprintf(&b, " COUNT %d", len(ids))
	}
	if len(ids) > 0 {
		if ret.min || ret.max {
			mn, mx := ids[0], ids[0]
			for _, id := range ids[1:] {
				if id < mn {
					mn = id
				}
				if id > mx {
					mx = id
				}
			}
			if ret.min {
				fmt.Fprintf(&b, " MIN %d", mn)
			}
			if ret.max {
				fmt.Fprintf(&b, " MAX %d", mx)
			}
		}
		if ret.all {
			b.WriteString(" ALL ")
			b.WriteString(formatUIDSet(ids))
		}
	}
	if ret.partial {
		fmt.Fprintf(&b, " PARTIAL (%s %s)", ret.echo, formatUIDSet(partialWindow(ids, ret.a, ret.b)))
	}
	return b.String()
}

func (s *session) cmdSearch(cmd command) error {
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "SEARCH not selected")
	}
	ret, rest, err := parseSearchReturn(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "SEARCH syntax")
	}
	if ret.on && !s.advertiseCap("ESEARCH") {
		return s.bad(cmd.tag, "SEARCH syntax")
	}
	if ret.save && !s.advertiseCap("SEARCHRES") {
		return s.bad(cmd.tag, "SEARCH syntax")
	}
	if ret.partial && !s.advertiseCap("PARTIAL") {
		return s.bad(cmd.tag, "SEARCH syntax")
	}
	hasCS, cs, rest, csErr := charsetPrefix(rest)
	if csErr != nil {
		return s.bad(cmd.tag, "SEARCH syntax")
	}
	if hasCS && (s.on("UTF8=ACCEPT") || !knownCharset(cs)) {
		return s.bad(cmd.tag, "SEARCH charset")
	}
	limit, allowUID := s.messageLimit()
	keys, err := parseSearch(rest, s.advertiseCap("SEARCH=FUZZY"), s.advertiseCap("WITHIN"), s.advertiseCap("SEARCHRES"), s.advertiseCap("SAVEDATE"), allowUID, s.searchNow())
	if err != nil {
		return s.bad(cmd.tag, "SEARCH bad key")
	}
	ids, uids, lowest, limited, err := s.srv.store.search(s, cmd.uid, keys, nil, limit)
	if err != nil {
		return s.no(cmd.tag, "SEARCH not selected")
	}
	if ret.save {
		s.savedUIDs = append([]uint32(nil), uids...)
	}
	done := "SEARCH completed"
	if limited {
		done = messageLimitText(limit, lowest, true) + " SEARCH completed"
	}
	if ret.saveOnly() {
		return s.ok(cmd.tag, done)
	}
	line := formatEsearch(cmd.tag, cmd.uid, ids, ret)
	if !ret.on {
		show := ids
		if limited {
			show = reverseU32(ids)
		}
		var b strings.Builder
		b.WriteString("* SEARCH")
		for _, id := range show {
			fmt.Fprintf(&b, " %d", id)
		}
		line = b.String()
	}
	if err := s.sendLine(line); err != nil {
		return err
	}
	return s.ok(cmd.tag, done)
}

// parseFilterSpec reads one Sieve spec. A name is not looked up.
// Script text is not parsed. An empty PERSONAL or GLOBAL name is an error.
func parseFilterSpec(args []Token) ([]Token, error) {
	if len(args) < 2 || args[0].Kind != "atom" || !strings.EqualFold(args[0].Val, "SIEVE") || args[1].Kind != "atom" {
		return nil, fmt.Errorf("bad")
	}
	switch strings.ToUpper(args[1].Val) {
	case "DELIVERY":
		return args[2:], nil
	case "PERSONAL", "GLOBAL":
		if len(args) < 3 {
			return nil, fmt.Errorf("bad")
		}
		name, ok := astring(args[2])
		if !ok || name == "" {
			return nil, fmt.Errorf("bad")
		}
		return args[3:], nil
	case "SCRIPT":
		if len(args) < 3 {
			return nil, fmt.Errorf("bad")
		}
		if _, ok := astring(args[2]); !ok {
			return nil, fmt.Errorf("bad")
		}
		return args[3:], nil
	default:
		return nil, fmt.Errorf("bad")
	}
}

// cmdFilter recognizes FILTER from draft-bosch-imap-filter-sieve-00.
// Valid arguments are tagged NO. No script runs and the search is not run.
// FILTER=SIEVE is not advertised.
func (s *session) cmdFilter(cmd command) error {
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "FILTER not selected")
	}
	rest, err := parseFilterSpec(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "FILTER syntax")
	}
	if len(rest) > 0 && rest[0].Kind == "atom" && strings.EqualFold(rest[0].Val, "RETURN") {
		return s.bad(cmd.tag, "FILTER syntax")
	}
	hasCS, cs, rest, csErr := charsetPrefix(rest)
	if csErr != nil {
		return s.bad(cmd.tag, "FILTER syntax")
	}
	if hasCS && (s.on("UTF8=ACCEPT") || !knownCharset(cs)) {
		return s.bad(cmd.tag, "FILTER charset")
	}
	_, allowUID := s.messageLimit()
	if _, err := parseSearch(rest, s.advertiseCap("SEARCH=FUZZY"), s.advertiseCap("WITHIN"), s.advertiseCap("SEARCHRES"), s.advertiseCap("SAVEDATE"), allowUID, s.searchNow()); err != nil {
		return s.bad(cmd.tag, "FILTER bad key")
	}
	return s.no(cmd.tag, "not implemented")
}

// parseEsearchIn reads IN (subtree name), IN (personal), or IN (subscribed).
// mailboxes, selected, and any other source are rejected.
func parseEsearchIn(args []Token) (string, string, []Token, error) {
	if len(args) < 3 || args[0].Kind != "atom" || !strings.EqualFold(args[0].Val, "IN") || args[1].Kind != "lparen" {
		return "", "", nil, fmt.Errorf("bad")
	}
	if args[2].Kind != "atom" {
		return "", "", nil, fmt.Errorf("bad")
	}
	source := strings.ToUpper(args[2].Val)
	i := 3
	root := ""
	switch source {
	case "PERSONAL", "SUBSCRIBED":
		if i >= len(args) || args[i].Kind != "rparen" {
			return "", "", nil, fmt.Errorf("bad")
		}
		i++
	case "SUBTREE":
		if i >= len(args) {
			return "", "", nil, fmt.Errorf("bad")
		}
		name, ok := astring(args[i])
		if !ok || name == "" {
			return "", "", nil, fmt.Errorf("bad")
		}
		i++
		if i >= len(args) || args[i].Kind != "rparen" {
			return "", "", nil, fmt.Errorf("bad")
		}
		i++
		root = name
	default:
		return "", "", nil, fmt.Errorf("bad")
	}
	return source, root, args[i:], nil
}

func (s *session) cmdEsearch(cmd command) error {
	if !s.advertiseCap("MULTISEARCH") {
		return s.bad(cmd.tag, "ESEARCH unsupported")
	}
	if !s.needAuth(cmd.tag) {
		return nil
	}
	source, root, rest, err := parseEsearchIn(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "ESEARCH syntax")
	}
	if source == "SUBTREE" {
		root, err = s.boxIn(root)
		if err != nil {
			return s.bad(cmd.tag, "ESEARCH syntax")
		}
	}
	ret, rest, err := parseSearchReturn(rest)
	if err != nil || ret.save {
		return s.bad(cmd.tag, "ESEARCH syntax")
	}
	if ret.partial && !s.advertiseCap("PARTIAL") {
		return s.bad(cmd.tag, "ESEARCH syntax")
	}
	hasCS, cs, rest, csErr := charsetPrefix(rest)
	if csErr != nil {
		return s.bad(cmd.tag, "ESEARCH syntax")
	}
	if hasCS && (s.on("UTF8=ACCEPT") || !knownCharset(cs)) {
		return s.bad(cmd.tag, "ESEARCH charset")
	}
	_, allowUID := s.messageLimit()
	keys, err := parseSearch(rest, s.advertiseCap("SEARCH=FUZZY"), s.advertiseCap("WITHIN"), false, s.advertiseCap("SAVEDATE"), allowUID, s.searchNow())
	if err != nil {
		return s.bad(cmd.tag, "ESEARCH bad key")
	}
	if !ret.on {
		ret.all = true
	}
	hits, err := s.srv.store.searchSource(s.user, source, root, keys)
	if err != nil {
		return s.bad(cmd.tag, "ESEARCH syntax")
	}
	for _, h := range hits {
		if err := s.sendLine(formatMultiEsearch(cmd.tag, s.boxOut(h.name), h.uidv, h.uids, ret)); err != nil {
			return err
		}
	}
	return s.ok(cmd.tag, "ESEARCH completed")
}

// charsetPrefix reads a leading CHARSET astring. SEARCH uses it. After
// UTF8=ACCEPT the caller rejects any charset.
func charsetPrefix(toks []Token) (bool, string, []Token, error) {
	if len(toks) == 0 || toks[0].Kind != "atom" || !strings.EqualFold(toks[0].Val, "CHARSET") {
		return false, "", toks, nil
	}
	if len(toks) < 2 {
		return false, "", nil, fmt.Errorf("syntax")
	}
	cs, ok := astring(toks[1])
	if !ok || cs == "" {
		return false, "", nil, fmt.Errorf("syntax")
	}
	return true, cs, toks[2:], nil
}

func knownCharset(s string) bool {
	switch strings.ToUpper(s) {
	case "UTF-8", "US-ASCII":
		return true
	default:
		return false
	}
}

// parseSortCrit reads one parenthesized key. A second key is syntax, not a sort.
// MODSEQ is that one key only when SORT=MODSEQ is listed. UID is never a key.
func parseSortCrit(args []Token, display, modseq bool) (sortCrit, []Token, error) {
	if len(args) == 0 || args[0].Kind != "lparen" {
		return sortCrit{}, nil, fmt.Errorf("syntax")
	}
	i := 1
	rev := false
	if i < len(args) && args[i].Kind == "atom" && strings.EqualFold(args[i].Val, "REVERSE") {
		rev = true
		i++
	}
	if i >= len(args) || args[i].Kind != "atom" {
		return sortCrit{}, nil, fmt.Errorf("syntax")
	}
	key := strings.ToUpper(args[i].Val)
	switch key {
	case "ARRIVAL", "CC", "DATE", "FROM", "SIZE", "SUBJECT", "TO":
	case "DISPLAYFROM", "DISPLAYTO":
		if !display {
			return sortCrit{}, nil, fmt.Errorf("key")
		}
	case "MODSEQ":
		if !modseq {
			return sortCrit{}, nil, fmt.Errorf("key")
		}
	default:
		return sortCrit{}, nil, fmt.Errorf("key")
	}
	i++
	if i >= len(args) || args[i].Kind != "rparen" {
		return sortCrit{}, nil, fmt.Errorf("syntax")
	}
	return sortCrit{key: key, rev: rev}, args[i+1:], nil
}

func (s *session) cmdSort(cmd command) error {
	if !s.advertiseCap("SORT") {
		return s.bad(cmd.tag, "SORT unsupported")
	}
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "SORT not selected")
	}
	ret, rest, err := parseSearchReturn(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "SORT syntax")
	}
	if ret.on && !s.advertiseCap("ESORT") {
		return s.bad(cmd.tag, "SORT syntax")
	}
	if ret.save && !s.advertiseCap("SEARCHRES") {
		return s.bad(cmd.tag, "SORT syntax")
	}
	if ret.partial && !s.advertiseCap("PARTIAL") {
		return s.bad(cmd.tag, "SORT syntax")
	}
	crit, rest, err := parseSortCrit(rest, s.advertiseCap("SORT=DISPLAY"), s.advertiseCap("SORT=MODSEQ"))
	if err != nil {
		if err.Error() == "key" {
			return s.bad(cmd.tag, "SORT bad key")
		}
		return s.bad(cmd.tag, "SORT syntax")
	}
	crit.uidTie = s.advertiseCap("SORT=UID")
	if len(rest) == 0 {
		return s.bad(cmd.tag, "SORT syntax")
	}
	cs, ok := astring(rest[0])
	if !ok || cs == "" {
		return s.bad(cmd.tag, "SORT syntax")
	}
	var keys []searchKey
	if s.on("UTF8=ACCEPT") && knownCharset(cs) {
		return s.bad(cmd.tag, "SORT charset")
	}
	within := s.advertiseCap("WITHIN")
	now := s.searchNow()
	saved := s.advertiseCap("SEARCHRES")
	saveDate := s.advertiseCap("SAVEDATE")
	_, allowUID := s.messageLimit()
	if s.on("UTF8=ACCEPT") {
		keys, err = parseSearch(rest, false, within, saved, saveDate, allowUID, now)
	} else if !knownCharset(cs) {
		return s.bad(cmd.tag, "SORT bad charset")
	} else {
		keys, err = parseSearch(rest[1:], false, within, saved, saveDate, allowUID, now)
	}
	if err != nil {
		return s.bad(cmd.tag, "SORT bad key")
	}
	if n, ok := s.messageLimit(); ok {
		count, cerr := s.srv.store.selectedCount(s)
		if cerr != nil {
			return s.no(cmd.tag, "SORT not selected")
		}
		if uint64(count) > n {
			return s.no(cmd.tag, messageLimitText(n, 0, false)+" SORT exceeds limit")
		}
	}
	ids, uids, _, _, err := s.srv.store.search(s, cmd.uid, keys, &crit, 0)
	if err != nil {
		return s.no(cmd.tag, "SORT not selected")
	}
	if ret.save {
		s.savedUIDs = append([]uint32(nil), uids...)
	}
	if ret.saveOnly() {
		return s.ok(cmd.tag, "SORT completed")
	}
	line := ""
	if ret.on {
		out := ret
		if s.pers.SortPartialCountOnly {
			out.partial = false
		}
		line = formatEsearch(cmd.tag, cmd.uid, ids, out)
	} else {
		var b strings.Builder
		b.WriteString("* SORT")
		for _, id := range ids {
			fmt.Fprintf(&b, " %d", id)
		}
		line = b.String()
	}
	if err := s.sendLine(line); err != nil {
		return err
	}
	return s.ok(cmd.tag, "SORT completed")
}

func (s *session) cmdThread(cmd command) error {
	if len(cmd.args) == 0 {
		return s.bad(cmd.tag, "THREAD syntax")
	}
	alg, ok := astring(cmd.args[0])
	if !ok || alg == "" {
		return s.bad(cmd.tag, "THREAD syntax")
	}
	alg = strings.ToUpper(alg)
	switch alg {
	case "REFERENCES", "REFS", "ORDEREDSUBJECT":
	default:
		return s.bad(cmd.tag, "THREAD bad algorithm")
	}
	if !s.advertiseCap("THREAD=" + alg) {
		return s.bad(cmd.tag, "THREAD unsupported")
	}
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "THREAD not selected")
	}
	rest := cmd.args[1:]
	if len(rest) == 0 {
		return s.bad(cmd.tag, "THREAD syntax")
	}
	cs, ok := astring(rest[0])
	if !ok || cs == "" {
		return s.bad(cmd.tag, "THREAD syntax")
	}
	var keys []searchKey
	var err error
	if s.on("UTF8=ACCEPT") && knownCharset(cs) {
		return s.bad(cmd.tag, "THREAD charset")
	}
	within := s.advertiseCap("WITHIN")
	now := s.searchNow()
	saveDate := s.advertiseCap("SAVEDATE")
	_, allowUID := s.messageLimit()
	if s.on("UTF8=ACCEPT") {
		keys, err = parseSearch(rest, false, within, false, saveDate, allowUID, now)
	} else if !knownCharset(cs) {
		return s.bad(cmd.tag, "THREAD bad charset")
	} else {
		keys, err = parseSearch(rest[1:], false, within, false, saveDate, allowUID, now)
	}
	if err != nil {
		return s.bad(cmd.tag, "THREAD bad key")
	}
	if n, ok := s.messageLimit(); ok {
		count, cerr := s.srv.store.selectedCount(s)
		if cerr != nil {
			return s.no(cmd.tag, "THREAD not selected")
		}
		if uint64(count) > n {
			return s.no(cmd.tag, messageLimitText(n, 0, false)+" THREAD exceeds limit")
		}
	}
	line, err := s.srv.store.thread(s, cmd.uid, keys, alg)
	if err != nil {
		return s.no(cmd.tag, "THREAD not selected")
	}
	if err := s.sendLine(line); err != nil {
		return err
	}
	return s.ok(cmd.tag, "THREAD completed")
}

func (s *session) cmdAppend(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	mbox, parts, err := parseAppend(cmd.args, s.advertiseCap("MULTIAPPEND"))
	if err != nil {
		return s.bad(cmd.tag, "APPEND syntax")
	}
	mbox, err = s.boxIn(mbox)
	if err != nil || !s.srv.store.hasMailbox(mbox) {
		return s.no(cmd.tag, "[TRYCREATE] APPEND no such mailbox")
	}
	for _, p := range parts {
		for _, f := range p.flags {
			if _, ok := canonFlag(f); !ok {
				return s.bad(cmd.tag, "APPEND bad flag")
			}
		}
	}
	if lim, ok := s.appendLimit(); ok {
		for _, p := range parts {
			if uint64(len(p.raw)) > lim {
				return s.no(cmd.tag, "[TOOBIG] APPEND exceeds limit")
			}
		}
	}
	if n, ok := s.messageLimit(); ok && uint64(len(parts)) > n {
		return s.no(cmd.tag, messageLimitText(n, 0, false)+" APPEND exceeds limit")
	}
	uv, uids, err := s.srv.store.appendMsgs(s, mbox, parts, s.srv.frozenTime())
	if err != nil {
		if err.Error() == "bad flag" {
			return s.bad(cmd.tag, "APPEND bad flag")
		}
		return s.no(cmd.tag, "[TRYCREATE] APPEND no such mailbox")
	}
	if err := s.flush(); err != nil {
		return err
	}
	return s.sendLine(fmt.Sprintf("%s OK [APPENDUID %d %s] APPEND completed", cmd.tag, uv, formatUIDSet(uids)))
}

// parseAppend reads one mailbox and one or more messages. Without multi,
// a second message is an error and the caller stores nothing.
func parseAppend(args []Token, multi bool) (string, []appendPart, error) {
	if len(args) < 2 {
		return "", nil, fmt.Errorf("bad")
	}
	mbox, ok := astring(args[0])
	if !ok {
		return "", nil, fmt.Errorf("bad")
	}
	i := 1
	var parts []appendPart
	for {
		if i >= len(args) {
			return "", nil, fmt.Errorf("bad")
		}
		var flags []string
		var err error
		if args[i].Kind == "lparen" {
			flags, i, err = parseRawFlagList(args, i)
			if err != nil {
				return "", nil, err
			}
		}
		if i >= len(args) {
			return "", nil, fmt.Errorf("bad")
		}
		var date *time.Time
		if args[i].Kind == "quoted" {
			t, err := parseIMAPDateTime(args[i].Val)
			if err != nil {
				return "", nil, err
			}
			date = &t
			i++
		}
		if i >= len(args) || args[i].Kind != "literal" {
			return "", nil, fmt.Errorf("bad")
		}
		parts = append(parts, appendPart{flags: flags, date: date, raw: []byte(args[i].Val)})
		i++
		if i >= len(args) {
			return mbox, parts, nil
		}
		if !multi {
			return "", nil, fmt.Errorf("bad")
		}
	}
}

// parseRawFlagList reads a parenthesized flag list without checking names.
// A bad name is decided after the mailbox check.
func parseRawFlagList(toks []Token, i int) ([]string, int, error) {
	if i >= len(toks) || toks[i].Kind != "lparen" {
		return nil, i, fmt.Errorf("bad flags")
	}
	i++
	var flags []string
	for i < len(toks) && toks[i].Kind != "rparen" {
		if toks[i].Kind != "atom" && toks[i].Kind != "quoted" {
			return nil, i, fmt.Errorf("bad flags")
		}
		flags = append(flags, toks[i].Val)
		i++
	}
	if i >= len(toks) || toks[i].Kind != "rparen" {
		return nil, i, fmt.Errorf("bad flags")
	}
	return flags, i + 1, nil
}

func moveCmd(name string) bool {
	return name == "MOVE" || name == "XMOVE"
}

func (s *session) cmdCopy(cmd command) error {
	if cmd.name == "XMOVE" {
		if !s.needAuth(cmd.tag) {
			return nil
		}
		if !s.advertiseCap("XMOVE") {
			return s.bad(cmd.tag, "XMOVE not supported")
		}
	}
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, cmd.name+" not selected")
	}
	if len(cmd.args) != 2 {
		return s.bad(cmd.tag, cmd.name+" syntax")
	}
	set, err := parseSeqSet(cmd.args[0].Val)
	if err != nil || (set.dollar && !s.advertiseCap("SEARCHRES")) {
		return s.bad(cmd.tag, cmd.name+" syntax")
	}
	dest, ok := astring(cmd.args[1])
	if !ok {
		return s.bad(cmd.tag, cmd.name+" syntax")
	}
	dest, err = s.boxIn(dest)
	if err != nil {
		return s.no(cmd.tag, "[TRYCREATE] "+cmd.name+" no such mailbox")
	}
	limit := uint64(0)
	if n, ok := s.messageLimit(); ok {
		limit = n
	}
	res, err := s.srv.store.copyMsgs(s, cmd.uid, set, dest, moveCmd(cmd.name), s.srv.frozenTime(), limit)
	if err != nil {
		if err.Error() == "read-only" {
			return s.no(cmd.tag, cmd.name+" read-only")
		}
		if err.Error() == "no mailbox" {
			return s.no(cmd.tag, "[TRYCREATE] "+cmd.name+" no such mailbox")
		}
		return s.no(cmd.tag, cmd.name+" failed")
	}
	if res.refused {
		return s.no(cmd.tag, messageLimitText(limit, res.lowest, true)+" "+cmd.name+" exceeds limit")
	}
	if res.limited && moveCmd(cmd.name) && len(res.src) > 0 {
		if err := s.sendLine(fmt.Sprintf("* OK [COPYUID %d %s %s] %s",
			res.uidv, formatUIDSet(res.src), formatUIDSet(res.dst), cmd.name)); err != nil {
			return err
		}
		for _, line := range res.lines {
			if err := s.sendLine(line); err != nil {
				return err
			}
		}
		if err := s.flush(); err != nil {
			return err
		}
		return s.ok(cmd.tag, messageLimitText(limit, res.lowest, true)+" "+cmd.name+" completed")
	}
	if moveCmd(cmd.name) && s.pers.MoveCopyUIDUntagged && len(res.src) > 0 {
		if err := s.sendLine(fmt.Sprintf("* OK [COPYUID %d %s %s] %s",
			res.uidv, formatUIDSet(res.src), formatUIDSet(res.dst), cmd.name)); err != nil {
			return err
		}
		for _, line := range res.lines {
			if err := s.sendLine(line); err != nil {
				return err
			}
		}
		return s.ok(cmd.tag, cmd.name+" completed")
	}
	for _, line := range res.lines {
		if err := s.sendLine(line); err != nil {
			return err
		}
	}
	if err := s.flush(); err != nil {
		return err
	}
	if len(res.src) == 0 {
		return s.ok(cmd.tag, cmd.name+" completed")
	}
	return s.sendLine(fmt.Sprintf("%s OK [COPYUID %d %s %s] %s completed",
		cmd.tag, res.uidv, formatUIDSet(res.src), formatUIDSet(res.dst), cmd.name))
}

func (s *session) cmdExpunge(cmd command) error {
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "EXPUNGE not selected")
	}
	var set *seqSet
	if cmd.uid {
		if len(cmd.args) != 1 {
			return s.bad(cmd.tag, "UID EXPUNGE syntax")
		}
		parsed, err := parseSeqSet(cmd.args[0].Val)
		if err != nil || (parsed.dollar && !s.advertiseCap("SEARCHRES")) {
			return s.bad(cmd.tag, "UID EXPUNGE syntax")
		}
		set = parsed
	} else if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "EXPUNGE syntax")
	}
	limit := uint64(0)
	if cmd.uid {
		if n, ok := s.messageLimit(); ok {
			limit = n
		}
	}
	lines, lowest, limited, err := s.srv.store.expunge(s, set, limit)
	if err != nil {
		if err.Error() == "read-only" {
			return s.no(cmd.tag, "EXPUNGE read-only")
		}
		return s.no(cmd.tag, "EXPUNGE not selected")
	}
	for _, line := range lines {
		if err := s.sendLine(line); err != nil {
			return err
		}
	}
	if err := s.flush(); err != nil {
		return err
	}
	if limited {
		return s.ok(cmd.tag, messageLimitText(limit, lowest, true)+" EXPUNGE completed")
	}
	if s.on("CONDSTORE") {
		hi, err := s.srv.store.highestModseq(s)
		if err != nil {
			return s.no(cmd.tag, "EXPUNGE not selected")
		}
		return s.sendLine(fmt.Sprintf("%s OK [HIGHESTMODSEQ %d] EXPUNGE completed", cmd.tag, hi))
	}
	return s.ok(cmd.tag, "EXPUNGE completed")
}

func (s *session) cmdClose(cmd command) error {
	if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "CLOSE syntax")
	}
	if err := s.srv.store.closeBox(s); err != nil {
		return s.no(cmd.tag, "CLOSE not selected")
	}
	return s.ok(cmd.tag, "CLOSE completed")
}

func (s *session) cmdUnselect(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "UNSELECT syntax")
	}
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "UNSELECT not selected")
	}
	s.srv.store.deselect(s)
	return s.ok(cmd.tag, "UNSELECT completed")
}

// cmdUnauthenticate drops authentication and the enabled set. TLS and
// compression stay. A selected mailbox is closed without expunge.
func (s *session) cmdUnauthenticate(cmd command) error {
	if !s.advertiseCap("UNAUTHENTICATE") {
		return s.bad(cmd.tag, "UNAUTHENTICATE not supported")
	}
	if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "UNAUTHENTICATE syntax")
	}
	s.srv.store.deselect(s)
	s.authed = false
	s.user = ""
	s.enabled = map[string]struct{}{}
	return s.ok(cmd.tag, "UNAUTHENTICATE completed")
}

// cmdGetJMAPAccess returns the absolute JMAP session URL. The command
// exists only when this personality lists JMAPACCESS and a listener is
// bound. It requires authentication and does not require a mailbox.
func (s *session) cmdGetJMAPAccess(cmd command) error {
	if !s.jmapBound() || !s.pers.listsCap("JMAPACCESS") {
		return s.bad(cmd.tag, "GETJMAPACCESS not supported")
	}
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if !s.advertiseCap("JMAPACCESS") {
		return s.bad(cmd.tag, "GETJMAPACCESS not supported")
	}
	if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "GETJMAPACCESS syntax")
	}
	url, ok := s.srv.jmapSessionURL()
	if !ok {
		return s.bad(cmd.tag, "GETJMAPACCESS not supported")
	}
	if err := s.sendLine("* JMAPACCESS " + imapString(url)); err != nil {
		return err
	}
	return s.ok(cmd.tag, "GETJMAPACCESS completed")
}

func (s *session) cmdIdle(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "IDLE syntax")
	}
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "IDLE not selected")
	}
	if err := s.sendLine("+ idling"); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		line, err := s.readRawLine()
		if err != nil {
			done <- err
			return
		}
		if !strings.EqualFold(strings.TrimSpace(line), "DONE") {
			done <- errSyntax
			return
		}
		done <- nil
	}()
	for {
		if err := s.flush(); err != nil {
			return err
		}
		select {
		case <-s.wake:
		case err := <-done:
			if err != nil {
				return err
			}
			return s.ok(cmd.tag, "IDLE completed")
		case <-s.srv.quit:
			return io.EOF
		}
	}
}

func formatUIDSet(uids []uint32) string {
	if len(uids) == 0 {
		return "NIL"
	}
	var parts []string
	start, prev := uids[0], uids[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.FormatUint(uint64(start), 10))
			return
		}
		parts = append(parts, fmt.Sprintf("%d:%d", start, prev))
	}
	for _, u := range uids[1:] {
		if u == prev+1 {
			prev = u
			continue
		}
		flush()
		start, prev = u, u
	}
	flush()
	return strings.Join(parts, ",")
}
