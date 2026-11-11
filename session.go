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
)

var (
	errSyntax = errors.New("syntax")
	errLogout = errors.New("logout")
)

// session is one IMAP connection. box, readonly, seenID, and wake are
// guarded by Store.mu.
type session struct {
	srv      *Server
	conn     net.Conn
	br       *bufio.Reader
	bw       *bufio.Writer
	wmu      sync.Mutex
	id       int
	authed   bool
	secure   bool
	box      *Mailbox
	readonly bool
	seenID   uint64
	wake     chan struct{}
	enabled  map[string]struct{}
}

type command struct {
	tag   string
	name  string
	uid   bool
	args  []Token
	fetch []string
}

func (s *session) caps() []string {
	return s.srv.pers.advertise(s.authed, s.secure, s.srv.hasCert)
}

func (s *session) sendLine(line string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
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
	greet := fmt.Sprintf("* OK [CAPABILITY %s] %s", strings.Join(s.caps(), " "), s.srv.pers.Greeting)
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
		s.srv.logLine("command", s.id, joinCmd(cmd.tag, cmd.name, cmd.uid, cmd.args, cmd.fetch))
		if err := s.flush(); err != nil {
			return
		}
		if err := s.dispatch(cmd); err != nil {
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
		case "FETCH", "STORE", "SEARCH", "COPY", "MOVE", "EXPUNGE":
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
		cmd.fetch = items
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

func (s *session) flush() error {
	for _, e := range s.srv.store.drain(s) {
		switch e.kind {
		case "expunge":
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
			if err := s.sendLine(fmt.Sprintf("* %d FETCH (FLAGS %s)", e.seq, e.flags)); err != nil {
				return err
			}
		}
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
		if s.srv.pers.LogoutBye {
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
	tc := s.srv.tlsServer(&prefixConn{Conn: s.conn, pre: pre})
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
	if !ok || !strings.EqualFold(mech, "PLAIN") {
		return s.no(cmd.tag, "AUTHENTICATE mechanism not available")
	}
	var b64 string
	if len(cmd.args) == 2 {
		b64, ok = astring(cmd.args[1])
		if !ok {
			return s.bad(cmd.tag, "AUTHENTICATE syntax")
		}
	} else {
		if err := s.sendLine("+"); err != nil {
			return err
		}
		line, err := s.readRawLine()
		if err != nil {
			return err
		}
		if line == "*" {
			return s.bad(cmd.tag, "AUTHENTICATE cancelled")
		}
		b64 = line
	}
	raw, err := decodeB64(b64)
	if err != nil {
		return s.no(cmd.tag, "[AUTHENTICATIONFAILED] AUTHENTICATE failed")
	}
	user, pass, ok := plainCreds(raw)
	if !ok {
		return s.no(cmd.tag, "[AUTHENTICATIONFAILED] AUTHENTICATE failed")
	}
	return s.loginOK(cmd.tag, "AUTHENTICATE", user, pass)
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
	// No capability in this server is turned on by ENABLE.
	// Unadvertised names, and advertised names that are not extensions
	// to enable, enable nothing: there is no ENABLED response.
	return s.ok(cmd.tag, "ENABLE completed")
}

func (s *session) cmdNamespace(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 0 {
		return s.bad(cmd.tag, "NAMESPACE syntax")
	}
	d := imapString(s.srv.pers.Delimiter)
	if err := s.sendLine(fmt.Sprintf(`* NAMESPACE (("" %s)) NIL NIL`, d)); err != nil {
		return err
	}
	return s.ok(cmd.tag, "NAMESPACE completed")
}

func (s *session) cmdList(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 2 {
		return s.bad(cmd.tag, "LIST syntax")
	}
	ref, ok1 := astring(cmd.args[0])
	pat, ok2 := astring(cmd.args[1])
	if !ok1 || !ok2 {
		return s.bad(cmd.tag, "LIST syntax")
	}
	entries, delim, _, err := s.srv.store.list(ref, pat)
	if err != nil {
		return s.bad(cmd.tag, "LIST syntax")
	}
	for _, e := range entries {
		if err := s.sendLine(fmt.Sprintf("* LIST %s %s %s", e.flags, imapString(delim), imapString(e.name))); err != nil {
			return err
		}
	}
	return s.ok(cmd.tag, "LIST completed")
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
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p[0])
		b.WriteByte(' ')
		b.WriteString(p[1])
	}
	if err := s.sendLine(fmt.Sprintf("* STATUS %s (%s)", imapString(cn), b.String())); err != nil {
		return err
	}
	return s.ok(cmd.tag, "STATUS completed")
}

func (s *session) cmdCreate(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 1 {
		return s.bad(cmd.tag, "CREATE syntax")
	}
	name, ok := astring(cmd.args[0])
	if !ok {
		return s.bad(cmd.tag, "CREATE syntax")
	}
	if err := s.srv.store.create(name); err != nil {
		return s.no(cmd.tag, "CREATE failed")
	}
	return s.ok(cmd.tag, "CREATE completed")
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
	if err := s.srv.store.rename(a, b); err != nil {
		return s.no(cmd.tag, "RENAME failed")
	}
	return s.ok(cmd.tag, "RENAME completed")
}

func (s *session) cmdSelect(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if len(cmd.args) != 1 {
		return s.bad(cmd.tag, "syntax")
	}
	name, ok := astring(cmd.args[0])
	if !ok {
		return s.bad(cmd.tag, "syntax")
	}
	info, err := s.srv.store.selectBox(s, name, cmd.name == "EXAMINE")
	if err != nil {
		return s.no(cmd.tag, cmd.name+" no such mailbox")
	}
	if err := s.sendLine(`* FLAGS (\Answered \Flagged \Deleted \Seen \Draft \Recent)`); err != nil {
		return err
	}
	if info.readOnly {
		if err := s.sendLine(`* OK [PERMANENTFLAGS ()] Read-only mailbox`); err != nil {
			return err
		}
	} else {
		if err := s.sendLine(`* OK [PERMANENTFLAGS (\Answered \Flagged \Deleted \Seen \Draft \*)] Permanent flags`); err != nil {
			return err
		}
	}
	if err := s.sendLine(fmt.Sprintf("* %d EXISTS", info.exists)); err != nil {
		return err
	}
	if err := s.sendLine(fmt.Sprintf("* %d RECENT", info.recent)); err != nil {
		return err
	}
	if info.unseen > 0 {
		if err := s.sendLine(fmt.Sprintf("* OK [UNSEEN %d] First unseen", info.unseen)); err != nil {
			return err
		}
	}
	if err := s.sendLine(fmt.Sprintf("* OK [UIDVALIDITY %d] UIDs valid", info.uidValidity)); err != nil {
		return err
	}
	if err := s.sendLine(fmt.Sprintf("* OK [UIDNEXT %d] Predicted next UID", info.uidNext)); err != nil {
		return err
	}
	mode := "READ-WRITE"
	if info.readOnly {
		mode = "READ-ONLY"
	}
	return s.sendLine(fmt.Sprintf("%s OK [%s] %s completed", cmd.tag, mode, cmd.name))
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
	snaps, items, err := s.srv.store.fetch(s, cmd.uid, set, cmd.fetch)
	if err != nil {
		if err.Error() == "not selected" {
			return s.no(cmd.tag, "FETCH not selected")
		}
		return s.bad(cmd.tag, "FETCH bad item")
	}
	for _, sn := range snaps {
		if err := s.sendRaw([]byte(s.formatFetch(sn, items))); err != nil {
			return err
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
		pieces = append(pieces, piece{s.srv.pers.fetchRank(it), i, fetchItemText(it, sn)})
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

func fetchItemText(item string, sn fetchSnap) string {
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
	}
	if strings.HasPrefix(item, "BODY[") || strings.HasPrefix(item, "BODY.PEEK[") {
		return bodySection(item, sn.raw)
	}
	return item + " NIL"
}

func bodySection(item string, raw []byte) string {
	specPart := item
	if strings.HasPrefix(item, "BODY.PEEK") {
		specPart = "BODY" + item[len("BODY.PEEK"):]
	}
	lb := strings.IndexByte(specPart, '[')
	rb := strings.IndexByte(specPart, ']')
	if lb < 0 || rb < lb {
		return item + " NIL"
	}
	spec := specPart[lb+1 : rb]
	partial := ""
	if rb+1 < len(specPart) {
		partial = specPart[rb+1:]
	}
	b, ok := sectionBytes(raw, spec)
	if !ok {
		return item + " NIL"
	}
	b, ok = applyPartial(b, partial)
	if !ok {
		return item + " NIL"
	}
	return item + " " + literal(b)
}

func (s *session) cmdStore(cmd command) error {
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "STORE not selected")
	}
	if len(cmd.args) < 3 {
		return s.bad(cmd.tag, "STORE syntax")
	}
	set, err := parseSeqSet(cmd.args[0].Val)
	if err != nil || cmd.args[1].Kind != "atom" {
		return s.bad(cmd.tag, "STORE syntax")
	}
	flags, next, err := parseFlagList(cmd.args, 2)
	if err != nil || next != len(cmd.args) {
		return s.bad(cmd.tag, "STORE syntax")
	}
	snaps, withUID, err := s.srv.store.storeFlags(s, cmd.uid, set, cmd.args[1].Val, flags)
	if err != nil {
		if err.Error() == "read-only" {
			return s.no(cmd.tag, "STORE read-only")
		}
		if err.Error() == "not selected" {
			return s.no(cmd.tag, "STORE not selected")
		}
		return s.bad(cmd.tag, "STORE syntax")
	}
	for _, sn := range snaps {
		if err := s.sendLine(s.flagsFetch(sn.seq, sn.flags, sn.uid, withUID)); err != nil {
			return err
		}
	}
	return s.ok(cmd.tag, "STORE completed")
}

func (s *session) flagsFetch(seq uint32, flags string, uid uint32, withUID bool) string {
	type piece struct {
		rank int
		text string
	}
	pieces := []piece{{s.srv.pers.fetchRank("FLAGS"), "FLAGS " + flags}}
	if withUID {
		pieces = append(pieces, piece{s.srv.pers.fetchRank("UID"), fmt.Sprintf("UID %d", uid)})
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

func (s *session) cmdSearch(cmd command) error {
	if !s.srv.store.selected(s) {
		return s.no(cmd.tag, "SEARCH not selected")
	}
	keys, err := parseSearch(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "SEARCH bad key")
	}
	ids, err := s.srv.store.search(s, cmd.uid, keys)
	if err != nil {
		return s.no(cmd.tag, "SEARCH not selected")
	}
	var b strings.Builder
	b.WriteString("* SEARCH")
	for _, id := range ids {
		fmt.Fprintf(&b, " %d", id)
	}
	if err := s.sendLine(b.String()); err != nil {
		return err
	}
	return s.ok(cmd.tag, "SEARCH completed")
}

func (s *session) cmdAppend(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	mbox, flags, date, body, err := parseAppend(cmd.args)
	if err != nil {
		return s.bad(cmd.tag, "APPEND syntax")
	}
	uv, uid, err := s.srv.store.appendMsg(s, mbox, flags, date, body)
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
