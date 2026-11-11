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
	secure     bool
	box        *Mailbox
	readonly   bool
	seenID     uint64
	wake       chan struct{}
	enabled    map[string]struct{}
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

func (s *session) caps() []string {
	return s.pers.advertise(s.authed, s.secure, s.srv.hasCert)
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
	greetCaps := s.caps()
	if len(s.pers.GreetingCaps) > 0 {
		greetCaps = s.pers.advertiseList(s.pers.GreetingCaps, false, s.secure, s.srv.hasCert)
	}
	greet := fmt.Sprintf("* OK [CAPABILITY %s] %s", strings.Join(greetCaps, " "), s.pers.Greeting)
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
		case "FETCH", "STORE", "SEARCH", "COPY", "MOVE", "EXPUNGE", "SORT", "THREAD":
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

func (s *session) advertiseCap(name string) bool {
	for _, c := range s.caps() {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
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
	case "LIST":
		return s.cmdList(cmd)
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
	case "SORT":
		return s.cmdSort(cmd)
	case "THREAD":
		return s.cmdThread(cmd)
	case "APPEND":
		return s.cmdAppend(cmd)
	case "COPY", "MOVE":
		return s.cmdCopy(cmd)
	case "EXPUNGE":
		return s.cmdExpunge(cmd)
	case "CLOSE":
		return s.cmdClose(cmd)
	case "UNSELECT":
		return s.cmdUnselect(cmd)
	case "IDLE":
		return s.cmdIdle(cmd)
	default:
		return s.bad(cmd.tag, "unknown command")
	}
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
	d := imapString(s.pers.Delimiter)
	other, shared := "NIL", "NIL"
	if s.pers.OtherUsers != "" {
		other = fmt.Sprintf(`((%s %s))`, imapString(s.pers.OtherUsers), d)
	}
	if s.pers.SharedFolders != "" {
		shared = fmt.Sprintf(`((%s %s))`, imapString(s.pers.SharedFolders), d)
	}
	if err := s.sendLine(fmt.Sprintf(`* NAMESPACE (("" %s)) %s %s`, d, other, shared)); err != nil {
		return err
	}
	return s.ok(cmd.tag, "NAMESPACE completed")
}

func (s *session) cmdList(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	parsed, err := parseListArgs(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "LIST syntax")
	}
	if parsed.extended && !s.advertiseCap("LIST-EXTENDED") {
		return s.bad(cmd.tag, "LIST syntax")
	}
	if parsed.children && !s.advertiseCap("CHILDREN") {
		return s.bad(cmd.tag, "LIST syntax")
	}
	if parsed.special && !s.advertiseCap("SPECIAL-USE") {
		return s.bad(cmd.tag, "LIST syntax")
	}
	if parsed.hasStatus && !s.advertiseCap("LIST-STATUS") {
		return s.bad(cmd.tag, "LIST syntax")
	}
	ref, err := s.boxIn(parsed.ref)
	if err != nil {
		return s.bad(cmd.tag, "LIST syntax")
	}
	pat, err := s.boxIn(parsed.pat)
	if err != nil {
		return s.bad(cmd.tag, "LIST syntax")
	}
	parsed.ref, parsed.pat = ref, pat
	q := listQuery{
		extended:    parsed.extended,
		specialOnly: parsed.specialOnly,
		showSpecial: s.advertiseCap("SPECIAL-USE"),
		wantStatus:  parsed.hasStatus,
		status:      parsed.status,
	}
	entries, delim, _, err := s.srv.store.list(parsed.ref, parsed.pat, q)
	if err != nil {
		if err.Error() == "bad status item" {
			return s.bad(cmd.tag, "LIST bad item")
		}
		return s.bad(cmd.tag, "LIST syntax")
	}
	for _, e := range entries {
		wn := s.boxOut(e.name)
		if err := s.sendLine(fmt.Sprintf("* LIST %s %s %s", e.flags, imapString(delim), imapAtom(wn))); err != nil {
			return err
		}
		if e.status != nil {
			if err := s.sendLine(formatStatus(wn, e.status)); err != nil {
				return err
			}
		}
	}
	return s.ok(cmd.tag, "LIST completed")
}

type parsedList struct {
	ref, pat    string
	extended    bool
	specialOnly bool
	children    bool
	special     bool
	hasStatus   bool
	status      []string
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
	cn, pairs, err := s.srv.store.status(name, items)
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
	if err != nil {
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
	snaps, items, err := s.srv.store.fetch(s, cmd.uid, set, cmd.fetch, cmd.changedSince)
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
	}
	if strings.HasPrefix(item, "BODY[") || strings.HasPrefix(item, "BODY.PEEK[") {
		return bodySection(item, sn.raw, s.pers.FetchPeekBody)
	}
	if isBinaryFetch(item) {
		return binarySectionText(item, sn.raw)
	}
	return item + " NIL"
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
	if err != nil {
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
	snaps, modified, err := s.srv.store.storeFlags(s, cmd.uid, set, item, flags, unchanged)
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
	a, b    int
	echo    string
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
		case "PARTIAL":
			if r.partial || r.all {
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
	if !r.min && !r.max && !r.count && !r.all && !r.partial {
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
	keys, err := parseSearch(rest)
	if err != nil {
		return s.bad(cmd.tag, "SEARCH bad key")
	}
	ids, err := s.srv.store.search(s, cmd.uid, keys, nil)
	if err != nil {
		return s.no(cmd.tag, "SEARCH not selected")
	}
	line := formatEsearch(cmd.tag, cmd.uid, ids, ret)
	if !ret.on {
		var b strings.Builder
		b.WriteString("* SEARCH")
		for _, id := range ids {
			fmt.Fprintf(&b, " %d", id)
		}
		line = b.String()
	}
	if err := s.sendLine(line); err != nil {
		return err
	}
	return s.ok(cmd.tag, "SEARCH completed")
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
func parseSortCrit(args []Token, display bool) (sortCrit, []Token, error) {
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
	if ret.partial && !s.advertiseCap("PARTIAL") {
		return s.bad(cmd.tag, "SORT syntax")
	}
	crit, rest, err := parseSortCrit(rest, s.advertiseCap("SORT=DISPLAY"))
	if err != nil {
		if err.Error() == "key" {
			return s.bad(cmd.tag, "SORT bad key")
		}
		return s.bad(cmd.tag, "SORT syntax")
	}
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
	if s.on("UTF8=ACCEPT") {
		keys, err = parseSearch(rest)
	} else if !knownCharset(cs) {
		return s.bad(cmd.tag, "SORT bad charset")
	} else {
		keys, err = parseSearch(rest[1:])
	}
	if err != nil {
		return s.bad(cmd.tag, "SORT bad key")
	}
	ids, err := s.srv.store.search(s, cmd.uid, keys, &crit)
	if err != nil {
		return s.no(cmd.tag, "SORT not selected")
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
	if s.on("UTF8=ACCEPT") {
		keys, err = parseSearch(rest)
	} else if !knownCharset(cs) {
		return s.bad(cmd.tag, "THREAD bad charset")
	} else {
		keys, err = parseSearch(rest[1:])
	}
	if err != nil {
		return s.bad(cmd.tag, "THREAD bad key")
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
	mbox, flags, date, body, err := parseAppend(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "APPEND syntax")
	}
	mbox, err = s.boxIn(mbox)
	if err != nil {
		return s.no(cmd.tag, "[TRYCREATE] APPEND no such mailbox")
	}
	uv, uid, err := s.srv.store.appendMsg(s, mbox, flags, date, body, s.srv.frozenTime())
	if err != nil {
		if err.Error() == "bad flag" {
			return s.bad(cmd.tag, "APPEND bad flag")
		}
		return s.no(cmd.tag, "[TRYCREATE] APPEND no such mailbox")
	}
	if err := s.flush(); err != nil {
		return err
	}
	return s.sendLine(fmt.Sprintf("%s OK [APPENDUID %d %d] APPEND completed", cmd.tag, uv, uid))
}

func parseAppend(args []Token) (string, []string, *time.Time, []byte, error) {
	if len(args) < 2 {
		return "", nil, nil, nil, fmt.Errorf("bad")
	}
	mbox, ok := astring(args[0])
	if !ok {
		return "", nil, nil, nil, fmt.Errorf("bad")
	}
	i := 1
	var flags []string
	var err error
	if args[i].Kind == "lparen" {
		flags, i, err = parseFlagList(args, i)
		if err != nil {
			return "", nil, nil, nil, err
		}
	}
	if i >= len(args) {
		return "", nil, nil, nil, fmt.Errorf("bad")
	}
	var date *time.Time
	if args[i].Kind == "quoted" {
		t, err := parseIMAPDateTime(args[i].Val)
		if err != nil {
			return "", nil, nil, nil, err
		}
		date = &t
		i++
	}
	if i != len(args)-1 || args[i].Kind != "literal" {
		return "", nil, nil, nil, fmt.Errorf("bad")
	}
	return mbox, flags, date, []byte(args[i].Val), nil
}

func (s *session) cmdCopy(cmd command) error {
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, cmd.name+" not selected")
	}
	if len(cmd.args) != 2 {
		return s.bad(cmd.tag, cmd.name+" syntax")
	}
	set, err := parseSeqSet(cmd.args[0].Val)
	if err != nil {
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
	res, err := s.srv.store.copyMsgs(s, cmd.uid, set, dest, cmd.name == "MOVE")
	if err != nil {
		if err.Error() == "read-only" {
			return s.no(cmd.tag, cmd.name+" read-only")
		}
		if err.Error() == "no mailbox" {
			return s.no(cmd.tag, "[TRYCREATE] "+cmd.name+" no such mailbox")
		}
		return s.no(cmd.tag, cmd.name+" failed")
	}
	if cmd.name == "MOVE" && s.pers.MoveCopyUIDUntagged && len(res.src) > 0 {
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
		if err != nil {
			return s.bad(cmd.tag, "UID EXPUNGE syntax")
		}
		set = parsed
	} else if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "EXPUNGE syntax")
	}
	lines, err := s.srv.store.expunge(s, set)
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
