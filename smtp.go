package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// RFC 5321 limits, counting the trailing CRLF.
// smtpSizeDefault is the EHLO SIZE limit when smtp_size is omitted.
const (
	smtpCmdMax      = 512
	smtpDataMax     = 1000
	smtpSizeDefault = 10000000
)

type smtpLn struct {
	ln       net.Listener
	dsn      bool
	implicit bool
}

type smtpRcptView struct {
	Address string `json:"address"`
	Result  string `json:"result"`
	Notify  string `json:"notify,omitempty"`
	Orcpt   string `json:"orcpt,omitempty"`
}

type smtpRecord struct {
	From       string         `json:"from"`
	Ret        string         `json:"ret,omitempty"`
	Envid      string         `json:"envid,omitempty"`
	SMTPUTF8   bool           `json:"smtputf8,omitempty"`
	Size       *int           `json:"size,omitempty"`
	Recipients []smtpRcptView `json:"recipients"`
}

type smtpWaiter struct {
	goal int
	ch   chan struct{}
}

type smtpEnv struct {
	from     string
	hasFrom  bool
	ret      string
	envid    string
	smtputf8 bool
	size     *int
	rcpts    []smtpRcpt
}

type smtpRcpt struct {
	addr    string
	deliver bool
	notify  string
	orcpt   string
}

type smtpConn struct {
	srv      *Server
	c        net.Conn
	br       *bufio.Reader
	bw       *bufio.Writer
	id       int
	authed   bool
	user     string
	secure   bool
	dsn      bool
	suppress bool
	capture  *bytes.Buffer
	env      smtpEnv
}

func (s *Server) acceptSMTP(spec smtpLn) {
	for {
		c, err := spec.ln.Accept()
		if err != nil {
			return
		}
		go s.serveSMTP(c, spec.dsn, spec.implicit)
	}
}

func (s *Server) serveSMTP(c net.Conn, dsn, implicit bool) {
	defer c.Close()
	sc := &smtpConn{
		srv: s, c: c, br: bufio.NewReader(c), bw: bufio.NewWriter(c),
		id: int(s.smtpSeq.Add(1)), dsn: dsn,
	}
	if implicit {
		tc := s.tlsServerFor(c, sc.id, "TLS")
		if err := tc.Handshake(); err != nil {
			return
		}
		sc.c = tc
		sc.br = bufio.NewReader(tc)
		sc.bw = bufio.NewWriter(tc)
		sc.secure = true
	}
	sc.serve()
}

func (s *Server) acceptSMTPMsg(rec smtpRecord) {
	s.smtpMu.Lock()
	s.smtpLog = append(s.smtpLog, rec)
	s.smtpN++
	n := s.smtpN
	var wake []chan struct{}
	keep := s.smtpWaiters[:0]
	for _, w := range s.smtpWaiters {
		if n >= w.goal {
			wake = append(wake, w.ch)
		} else {
			keep = append(keep, w)
		}
	}
	s.smtpWaiters = keep
	s.smtpMu.Unlock()
	for _, ch := range wake {
		close(ch)
	}
}

func (s *Server) listSMTP() []smtpRecord {
	s.smtpMu.Lock()
	defer s.smtpMu.Unlock()
	out := make([]smtpRecord, len(s.smtpLog))
	for i, rec := range s.smtpLog {
		out[i] = rec
		out[i].Recipients = append([]smtpRcptView(nil), rec.Recipients...)
		if rec.Size != nil {
			n := *rec.Size
			out[i].Size = &n
		}
	}
	return out
}

func (s *Server) lastSMTP() (smtpRecord, bool) {
	s.smtpMu.Lock()
	defer s.smtpMu.Unlock()
	if len(s.smtpLog) == 0 {
		return smtpRecord{}, false
	}
	rec := s.smtpLog[len(s.smtpLog)-1]
	rec.Recipients = append([]smtpRcptView(nil), rec.Recipients...)
	if rec.Size != nil {
		n := *rec.Size
		rec.Size = &n
	}
	return rec, true
}

// waitSMTP waits until count messages have been accepted since the call.
func (s *Server) waitSMTP(count int, timeout time.Duration) bool {
	if count <= 0 {
		return true
	}
	s.smtpMu.Lock()
	goal := s.smtpN + count
	ch := make(chan struct{})
	w := &smtpWaiter{goal: goal, ch: ch}
	s.smtpWaiters = append(s.smtpWaiters, w)
	s.smtpMu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-timer.C:
		s.smtpMu.Lock()
		ok := s.smtpN >= goal
		s.removeSMTPWaiter(w)
		s.smtpMu.Unlock()
		return ok
	case <-s.quit:
		s.smtpMu.Lock()
		s.removeSMTPWaiter(w)
		s.smtpMu.Unlock()
		return false
	}
}

func (s *Server) removeSMTPWaiter(w *smtpWaiter) {
	for i, x := range s.smtpWaiters {
		if x == w {
			s.smtpWaiters = append(s.smtpWaiters[:i], s.smtpWaiters[i+1:]...)
			return
		}
	}
}

func (s *Server) smtpDelivers(addr string) bool {
	local := addr
	if i := strings.LastIndex(addr, "@"); i >= 0 {
		local = addr[:i]
	}
	if local == "" {
		return false
	}
	return s.store.hasAccount(local)
}

func (sc *smtpConn) serve() {
	if err := sc.reply(220, "fakeimap ESMTP ready"); err != nil {
		return
	}
	for {
		line, tooLong, err := readSMTPLine(sc.br, smtpCmdMax)
		if err != nil {
			return
		}
		if tooLong {
			if err := sc.reply(500, "5.5.2 line too long"); err != nil {
				return
			}
			continue
		}
		if err := sc.command(line); err != nil {
			return
		}
	}
}

func (sc *smtpConn) reply(code int, text string) error {
	return sc.writeReply(fmt.Sprintf("%d %s\r\n", code, text))
}

func (sc *smtpConn) replyLines(code int, lines []string) error {
	var b strings.Builder
	for i, line := range lines {
		sep := byte(' ')
		if i < len(lines)-1 {
			sep = '-'
		}
		fmt.Fprintf(&b, "%d%c%s\r\n", code, sep, line)
	}
	return sc.writeReply(b.String())
}

func (sc *smtpConn) cont(b64 string) error {
	line := "334\r\n"
	if b64 != "" {
		line = "334 " + b64 + "\r\n"
	}
	return sc.writeReply(line)
}

func (sc *smtpConn) writeReply(line string) error {
	if sc.suppress {
		return nil
	}
	if sc.capture != nil {
		_, err := sc.capture.WriteString(line)
		return err
	}
	if _, err := io.WriteString(sc.bw, line); err != nil {
		return err
	}
	return sc.bw.Flush()
}

func (sc *smtpConn) command(line string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return sc.reply(500, "5.5.2 empty command")
	}
	verb, arg, _ := strings.Cut(line, " ")
	verb = strings.ToUpper(verb)
	arg = strings.TrimSpace(arg)
	if verb == "MAIL" || verb == "RCPT" || verb == "DATA" {
		if sc.srv.cfg.smtpAuthRequired() && !sc.authed {
			return sc.reply(530, "5.7.0 authentication required")
		}
	}
	if f := sc.srv.matchSMTPFault(sc.id, verb); f != nil {
		return sc.applySMTPFault(verb, arg, f)
	}
	return sc.dispatch(verb, arg)
}

func (sc *smtpConn) dispatch(verb, arg string) error {
	switch verb {
	case "EHLO":
		return sc.replyLines(250, sc.ehloLines())
	case "HELO":
		return sc.reply(250, "fakeimap")
	case "STARTTLS":
		return sc.cmdStartTLS(arg)
	case "NOOP":
		return sc.reply(250, "2.0.0 OK")
	case "RSET":
		sc.env = smtpEnv{}
		return sc.reply(250, "2.0.0 OK")
	case "QUIT":
		_ = sc.reply(221, "2.0.0 bye")
		return io.EOF
	case "AUTH":
		return sc.cmdAuth(arg)
	case "MAIL":
		return sc.cmdMail(arg)
	case "RCPT":
		return sc.cmdRcpt(arg)
	case "DATA":
		return sc.cmdData(false)
	default:
		return sc.reply(502, "5.5.1 command not implemented")
	}
}

func (sc *smtpConn) applySMTPFault(verb, arg string, f *fault) error {
	switch f.do {
	case "overquota":
		return sc.reply(552, "5.2.2 mailbox full")
	case "close":
		return io.EOF
	case "rst":
		if tc := tcpConnOf(sc.c); tc != nil {
			_ = tc.SetLinger(0)
		}
		return io.EOF
	case "half_close":
		_ = closeWrite(sc.c)
		return io.EOF
	case "reply":
		code, err := strconv.Atoi(f.code)
		if err != nil {
			return sc.reply(550, "5.0.0 fault")
		}
		text := f.text
		if text == "" {
			text = "fault"
		}
		return sc.reply(code, text)
	case "silent":
		sc.suppress = true
		err := sc.dispatch(verb, arg)
		sc.suppress = false
		return err
	case "delay":
		if f.delayMs > 0 {
			time.Sleep(time.Duration(f.delayMs) * time.Millisecond)
		}
		return sc.dispatch(verb, arg)
	case "raw":
		sc.suppress = true
		err := sc.dispatch(verb, arg)
		sc.suppress = false
		if werr := sc.writeReply(string(lineCRLF(f.text))); werr != nil {
			return werr
		}
		return err
	case "drop_after_354":
		if err := sc.reply(354, "start mail input"); err != nil {
			return err
		}
		if f.bytes > 0 {
			buf := make([]byte, f.bytes)
			if _, err := io.ReadFull(sc.br, buf); err != nil {
				return err
			}
		}
		return io.EOF
	case "bounce":
		return sc.cmdData(true)
	default:
		return sc.dispatch(verb, arg)
	}
}

func (sc *smtpConn) ehloLines() []string {
	lines := []string{"fakeimap"}
	if sc.srv.cfg.smtpAuthRequired() {
		lines = append(lines, "AUTH "+sc.authMechList())
	}
	lines = append(lines,
		"8BITMIME",
		"PIPELINING",
		"SMTPUTF8",
		fmt.Sprintf("SIZE %d", sc.srv.cfg.smtpSizeLimit()),
	)
	if sc.dsn {
		lines = append(lines, "DSN")
	}
	if sc.srv.certLoaded() && !sc.secure {
		lines = append(lines, "STARTTLS")
	}
	return lines
}

func (sc *smtpConn) cmdStartTLS(arg string) error {
	if arg != "" {
		return sc.reply(501, "5.5.4 syntax error")
	}
	if sc.secure || !sc.srv.certLoaded() {
		return sc.reply(454, "4.7.0 TLS not available")
	}
	if err := sc.reply(220, "2.0.0 Ready to start TLS"); err != nil {
		return err
	}
	var pre []byte
	if n := sc.br.Buffered(); n > 0 {
		pre = make([]byte, n)
		if _, err := io.ReadFull(sc.br, pre); err != nil {
			return err
		}
	}
	tc := sc.srv.tlsServerFor(&prefixConn{Conn: sc.c, pre: pre}, 0, "STARTTLS")
	if err := tc.Handshake(); err != nil {
		return err
	}
	sc.c = tc
	sc.br = bufio.NewReader(tc)
	sc.bw = bufio.NewWriter(tc)
	sc.secure = true
	return nil
}

func (sc *smtpConn) authMechList() string {
	mechs := []string{"PLAIN", "LOGIN"}
	if sc.srv.pers != nil && sc.srv.pers.lists("AUTH=OAUTHBEARER") {
		mechs = append(mechs, "OAUTHBEARER")
	}
	if sc.srv.pers != nil && sc.srv.pers.lists("AUTH=XOAUTH2") {
		mechs = append(mechs, "XOAUTH2")
	}
	return strings.Join(mechs, " ")
}

func (sc *smtpConn) cmdAuth(arg string) error {
	if sc.authed {
		return sc.reply(503, "5.5.1 already authenticated")
	}
	mech, rest, hasRest := strings.Cut(arg, " ")
	mech = strings.ToUpper(strings.TrimSpace(mech))
	rest = strings.TrimSpace(rest)
	switch mech {
	case "PLAIN":
		return sc.authPlain(hasRest, rest)
	case "LOGIN":
		return sc.authLogin(hasRest, rest)
	case "OAUTHBEARER", "XOAUTH2":
		if sc.srv.pers == nil || !sc.srv.pers.lists("AUTH="+mech) {
			return sc.reply(504, "5.5.4 unrecognized authentication type")
		}
		return sc.authBearer(mech, hasRest, rest)
	case "":
		return sc.reply(501, "5.5.4 mechanism required")
	default:
		return sc.reply(504, "5.5.4 unrecognized authentication type")
	}
}

func (sc *smtpConn) authBearer(mech string, hasRest bool, rest string) error {
	var raw []byte
	if hasRest {
		var err error
		raw, err = decodeSASL(rest)
		if err != nil {
			return sc.reply(535, "5.7.8 authentication failed")
		}
	} else {
		if err := sc.cont(""); err != nil {
			return err
		}
		line, tooLong, err := readSMTPLine(sc.br, smtpCmdMax)
		if err != nil {
			return err
		}
		if tooLong {
			return sc.reply(500, "5.5.2 line too long")
		}
		raw, err = decodeSASL(strings.TrimSpace(line))
		if err != nil {
			return sc.reply(535, "5.7.8 authentication failed")
		}
	}
	var user, token string
	var ok bool
	if mech == "OAUTHBEARER" {
		user, token, ok = parseOAuthBearer(raw)
	} else {
		user, token, ok = parseXOAuth2(raw)
	}
	fail := "invalid"
	var scopes []string
	bound := ""
	if ok {
		res := sc.srv.checkBearer(token, "smtp", sc.id)
		bound = res.user
		scopes = res.scopes
		fail = res.fail
		if fail == "" && ((mech == "XOAUTH2" && user != bound) || (mech == "OAUTHBEARER" && user != "" && user != bound)) {
			fail = "invalid"
		}
		if fail == "" {
			sc.user = bound
			sc.authed = true
			sc.srv.logAttempt("smtp", mech, bound, "ok")
			return sc.reply(235, "2.7.0 authentication successful")
		}
	}
	who := user
	if who == "" {
		who = bound
	}
	sc.srv.logAttempt("smtp", mech, who, fail)
	if mech == "XOAUTH2" {
		if err := sc.cont(""); err != nil {
			return err
		}
	} else {
		st := "invalid_token"
		if fail == "scope" {
			st = "insufficient_scope"
		}
		if err := sc.cont(sc.srv.bearerContinue(st, scopes)); err != nil {
			return err
		}
	}
	_, tooLong, err := readSMTPLine(sc.br, smtpCmdMax)
	if err != nil {
		return err
	}
	if tooLong {
		return sc.reply(500, "5.5.2 line too long")
	}
	return sc.reply(535, "5.7.8 authentication failed")
}

func (sc *smtpConn) authPlain(hasRest bool, rest string) error {
	var raw []byte
	if hasRest {
		var err error
		raw, err = decodeB64(rest)
		if err != nil {
			return sc.reply(535, "5.7.8 authentication failed")
		}
	} else {
		if err := sc.cont(""); err != nil {
			return err
		}
		line, tooLong, err := readSMTPLine(sc.br, smtpCmdMax)
		if err != nil {
			return err
		}
		if tooLong {
			return sc.reply(500, "5.5.2 line too long")
		}
		if line == "*" {
			return sc.reply(501, "5.7.0 authentication cancelled")
		}
		raw, err = decodeB64(strings.TrimSpace(line))
		if err != nil {
			return sc.reply(535, "5.7.8 authentication failed")
		}
	}
	user, pass, ok := plainCreds(raw)
	if !ok || !sc.srv.store.check(user, pass) {
		return sc.reply(535, "5.7.8 authentication failed")
	}
	sc.user = user
	sc.authed = true
	return sc.reply(235, "2.7.0 authentication successful")
}

func (sc *smtpConn) authLogin(hasRest bool, rest string) error {
	var user string
	if hasRest {
		raw, err := decodeB64(rest)
		if err != nil || len(raw) == 0 {
			return sc.reply(535, "5.7.8 authentication failed")
		}
		user = string(raw)
	} else {
		if err := sc.cont(base64.StdEncoding.EncodeToString([]byte("Username:"))); err != nil {
			return err
		}
		line, tooLong, err := readSMTPLine(sc.br, smtpCmdMax)
		if err != nil {
			return err
		}
		if tooLong {
			return sc.reply(500, "5.5.2 line too long")
		}
		if line == "*" {
			return sc.reply(501, "5.7.0 authentication cancelled")
		}
		raw, err := decodeB64(strings.TrimSpace(line))
		if err != nil || len(raw) == 0 {
			return sc.reply(535, "5.7.8 authentication failed")
		}
		user = string(raw)
	}
	if err := sc.cont(base64.StdEncoding.EncodeToString([]byte("Password:"))); err != nil {
		return err
	}
	line, tooLong, err := readSMTPLine(sc.br, smtpCmdMax)
	if err != nil {
		return err
	}
	if tooLong {
		return sc.reply(500, "5.5.2 line too long")
	}
	if line == "*" {
		return sc.reply(501, "5.7.0 authentication cancelled")
	}
	raw, err := decodeB64(strings.TrimSpace(line))
	if err != nil {
		return sc.reply(535, "5.7.8 authentication failed")
	}
	if !sc.srv.store.check(user, string(raw)) {
		return sc.reply(535, "5.7.8 authentication failed")
	}
	sc.user = user
	sc.authed = true
	return sc.reply(235, "2.7.0 authentication successful")
}

func (sc *smtpConn) cmdMail(arg string) error {
	if sc.env.hasFrom {
		return sc.reply(503, "5.5.1 sender already set")
	}
	addr, params, ok := parseSMTPPath(arg, "FROM:")
	if !ok {
		return sc.reply(501, "5.5.4 bad sender")
	}
	ret, envid, utf8, size, code, text, ok := sc.mailParams(params)
	if !ok {
		return sc.reply(code, text)
	}
	sc.env.from = addr
	sc.env.hasFrom = true
	sc.env.ret = ret
	sc.env.envid = envid
	sc.env.smtputf8 = utf8
	sc.env.size = size
	return sc.reply(250, "2.1.0 OK")
}

func (sc *smtpConn) mailParams(params []smtpParam) (string, string, bool, *int, int, string, bool) {
	seen := map[string]bool{}
	var ret, envid string
	var utf8 bool
	var size *int
	for _, p := range params {
		if seen[p.key] {
			return "", "", false, nil, 501, "5.5.4 bad parameter", false
		}
		seen[p.key] = true
		switch p.key {
		case "BODY":
			if !p.hasVal {
				return "", "", false, nil, 501, "5.5.4 bad parameter", false
			}
			switch strings.ToUpper(p.val) {
			case "7BIT", "8BITMIME", "BINARYMIME":
			default:
				return "", "", false, nil, 501, "5.5.4 bad parameter", false
			}
		case "SIZE":
			if !p.hasVal {
				return "", "", false, nil, 501, "5.5.4 bad parameter", false
			}
			n, err := strconv.ParseInt(p.val, 10, 64)
			if err != nil || n < 0 {
				return "", "", false, nil, 501, "5.5.4 bad parameter", false
			}
			if n > int64(sc.srv.cfg.smtpSizeLimit()) {
				return "", "", false, nil, 552, "5.3.4 message too big", false
			}
			v := int(n)
			size = &v
		case "RET":
			if !p.hasVal {
				return "", "", false, nil, 501, "5.5.4 bad parameter", false
			}
			switch strings.ToUpper(p.val) {
			case "HDRS", "FULL":
				ret = strings.ToUpper(p.val)
			default:
				return "", "", false, nil, 501, "5.5.4 bad parameter", false
			}
		case "ENVID":
			if !p.hasVal || p.val == "" {
				return "", "", false, nil, 501, "5.5.4 bad parameter", false
			}
			envid = p.val
		case "SMTPUTF8":
			if p.hasVal {
				return "", "", false, nil, 501, "5.5.4 bad parameter", false
			}
			utf8 = true
		default:
			return "", "", false, nil, 501, "5.5.4 bad parameter", false
		}
	}
	return ret, envid, utf8, size, 0, "", true
}

func (sc *smtpConn) cmdRcpt(arg string) error {
	if !sc.env.hasFrom {
		return sc.reply(503, "5.5.1 need sender")
	}
	addr, params, ok := parseSMTPPath(arg, "TO:")
	if !ok {
		return sc.reply(501, "5.5.4 bad recipient")
	}
	notify, orcpt, code, text, ok := rcptParams(params)
	if !ok {
		return sc.reply(code, text)
	}
	deliver := sc.srv.cfg.smtpAuthRequired() && sc.srv.smtpDelivers(addr)
	sc.env.rcpts = append(sc.env.rcpts, smtpRcpt{addr: addr, deliver: deliver, notify: notify, orcpt: orcpt})
	return sc.reply(250, "2.1.5 OK")
}

func rcptParams(params []smtpParam) (string, string, int, string, bool) {
	seen := map[string]bool{}
	var notify, orcpt string
	for _, p := range params {
		if seen[p.key] {
			return "", "", 501, "5.5.4 bad parameter", false
		}
		seen[p.key] = true
		switch p.key {
		case "NOTIFY":
			if !p.hasVal || !validNotify(p.val) {
				return "", "", 501, "5.5.4 bad parameter", false
			}
			notify = strings.ToUpper(p.val)
		case "ORCPT":
			if !p.hasVal || p.val == "" {
				return "", "", 501, "5.5.4 bad parameter", false
			}
			orcpt = p.val
		default:
			return "", "", 501, "5.5.4 bad parameter", false
		}
	}
	return notify, orcpt, 0, "", true
}

func validNotify(v string) bool {
	parts := strings.Split(v, ",")
	if len(parts) == 0 {
		return false
	}
	never := false
	for _, p := range parts {
		switch strings.ToUpper(strings.TrimSpace(p)) {
		case "NEVER":
			never = true
		case "SUCCESS", "FAILURE", "DELAY":
		default:
			return false
		}
	}
	return !never || len(parts) == 1
}

func (sc *smtpConn) cmdData(bounce bool) error {
	if !sc.env.hasFrom || len(sc.env.rcpts) == 0 {
		return sc.reply(503, "5.5.1 no recipient")
	}
	if err := sc.replyLive(354, "start mail input"); err != nil {
		return err
	}
	raw, tooLong, err := readSMTPData(sc.br)
	if err != nil {
		return err
	}
	recips := append([]smtpRcpt(nil), sc.env.rcpts...)
	from := sc.env.from
	meta := smtpMeta{ret: sc.env.ret, envid: sc.env.envid, smtputf8: sc.env.smtputf8, size: sc.env.size}
	sc.env = smtpEnv{}
	if tooLong {
		return sc.reply(500, "5.5.2 line too long")
	}
	if err := sc.srv.acceptMail(sc.user, from, recips, raw, meta); err != nil {
		return sc.reply(451, "4.3.0 delivery failed")
	}
	if bounce {
		if err := sc.srv.deliverFailureReports(from, meta.envid, recips); err != nil {
			return sc.reply(451, "4.3.0 delivery failed")
		}
	}
	return sc.reply(250, "2.0.0 OK")
}

// replyLive writes a reply even while a fault is holding the final reply.
// DATA's 354 is the prompt the client waits on before the body.
func (sc *smtpConn) replyLive(code int, text string) error {
	prevS, prevC := sc.suppress, sc.capture
	sc.suppress = false
	sc.capture = nil
	err := sc.reply(code, text)
	sc.suppress, sc.capture = prevS, prevC
	return err
}

type smtpMeta struct {
	ret      string
	envid    string
	smtputf8 bool
	size     *int
}

// acceptMail is the SMTP DATA accept path. A dataset recipient appends the
// message to INBOX once. Any other recipient is accepted and not stored.
// list-smtp and wait-smtp see the same record either caller produces.
// A caller that cleared deliver on every recipient stores nothing.
func (s *Server) acceptMail(user, from string, rcpts []smtpRcpt, raw []byte, meta smtpMeta) error {
	views := make([]smtpRcptView, len(rcpts))
	deliver := false
	for i, r := range rcpts {
		result := "discarded"
		if r.deliver {
			result = "delivered"
			deliver = true
		}
		views[i] = smtpRcptView{Address: r.addr, Result: result, Notify: r.notify, Orcpt: r.orcpt}
	}
	if deliver {
		if _, _, err := s.store.appendAs(user, s.pers, "INBOX", nil, nil, raw, s.frozenTime()); err != nil {
			return err
		}
	}
	var size *int
	if meta.size != nil {
		n := *meta.size
		size = &n
	}
	s.acceptSMTPMsg(smtpRecord{
		From: from, Ret: meta.ret, Envid: meta.envid, SMTPUTF8: meta.smtputf8,
		Size: size, Recipients: views,
	})
	return nil
}

// deliverFailureReports stores one failure DSN per recipient that asked for
// one, in the envelope sender's INBOX when that sender is a dataset account.
func (s *Server) deliverFailureReports(from, envid string, rcpts []smtpRcpt) error {
	if !s.smtpDelivers(from) {
		return nil
	}
	for _, r := range rcpts {
		if !notifyWantsFailure(r.notify) {
			continue
		}
		raw := deliveryReport("failed", r.addr, envid)
		local := from
		if i := strings.LastIndex(from, "@"); i >= 0 {
			local = from[:i]
		}
		if _, _, err := s.store.appendAs(local, s.pers, "INBOX", nil, nil, raw, s.frozenTime()); err != nil {
			return err
		}
	}
	return nil
}

func notifyWantsFailure(notify string) bool {
	if notify == "" {
		return true
	}
	for _, p := range strings.Split(notify, ",") {
		if strings.EqualFold(strings.TrimSpace(p), "FAILURE") {
			return true
		}
	}
	return false
}

// deliveryReport is a multipart/report whose second part is message/delivery-status.
func deliveryReport(action, recipient, envid string) []byte {
	status := "5.0.0"
	if action == "delivered" {
		status = "2.0.0"
	}
	var b strings.Builder
	const boundary = "fakeimap-dsn"
	fmt.Fprintf(&b, "Content-Type: multipart/report; report-type=delivery-status; boundary=%s\r\n", boundary)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Subject: Delivery Status Notification\r\n")
	b.WriteString("\r\n")
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/plain\r\n\r\n")
	fmt.Fprintf(&b, "%s\r\n\r\n", action)
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: message/delivery-status\r\n\r\n")
	b.WriteString("Reporting-MTA: dns; fakeimap\r\n")
	if envid != "" {
		fmt.Fprintf(&b, "Original-Envelope-Id: %s\r\n", envid)
	}
	b.WriteString("\r\n")
	if recipient != "" {
		fmt.Fprintf(&b, "Final-Recipient: rfc822;%s\r\n", recipient)
	}
	fmt.Fprintf(&b, "Action: %s\r\n", action)
	fmt.Fprintf(&b, "Status: %s\r\n", status)
	fmt.Fprintf(&b, "\r\n--%s--\r\n", boundary)
	return []byte(b.String())
}

type smtpParam struct {
	key    string
	val    string
	hasVal bool
}

func parseSMTPPath(arg, keyword string) (string, []smtpParam, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) < len(keyword) || !strings.EqualFold(arg[:len(keyword)], keyword) {
		return "", nil, false
	}
	rest := strings.TrimSpace(arg[len(keyword):])
	if rest == "" {
		return "", nil, false
	}
	var addr string
	if rest[0] == '<' {
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			return "", nil, false
		}
		addr = rest[1:end]
		rest = strings.TrimSpace(rest[end+1:])
	} else {
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			addr = rest[:i]
			rest = strings.TrimSpace(rest[i+1:])
		} else {
			addr = rest
			rest = ""
		}
		if addr == "" {
			return "", nil, false
		}
	}
	params, ok := parseESMTPParams(rest)
	if !ok {
		return "", nil, false
	}
	return addr, params, true
}

func parseESMTPParams(rest string) ([]smtpParam, bool) {
	var out []smtpParam
	rest = strings.TrimSpace(rest)
	for rest != "" {
		i := strings.IndexByte(rest, ' ')
		tok := rest
		if i >= 0 {
			tok = rest[:i]
			rest = strings.TrimSpace(rest[i+1:])
		} else {
			rest = ""
		}
		if tok == "" {
			return nil, false
		}
		key, val, has := tok, "", false
		if eq := strings.IndexByte(tok, '='); eq >= 0 {
			key = tok[:eq]
			val = tok[eq+1:]
			has = true
			if val == "" {
				return nil, false
			}
		}
		if key == "" {
			return nil, false
		}
		for _, c := range key {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return nil, false
			}
		}
		out = append(out, smtpParam{key: strings.ToUpper(key), val: val, hasVal: has})
	}
	return out, true
}

func readSMTPLine(r *bufio.Reader, max int) (string, bool, error) {
	var buf []byte
	tooLong := false
	for {
		b, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && len(buf) == 0 && !tooLong {
				return "", false, io.EOF
			}
			return "", tooLong, err
		}
		if !tooLong {
			buf = append(buf, b)
			if len(buf) > max {
				tooLong = true
				buf = nil
			}
		}
		if b == '\n' {
			break
		}
	}
	if tooLong {
		return "", true, nil
	}
	if len(buf) > 0 && buf[len(buf)-1] == '\n' {
		buf = buf[:len(buf)-1]
	}
	if len(buf) > 0 && buf[len(buf)-1] == '\r' {
		buf = buf[:len(buf)-1]
	}
	return string(buf), false, nil
}

func readSMTPData(r *bufio.Reader) ([]byte, bool, error) {
	var buf []byte
	tooLong := false
	for {
		line, long, err := readSMTPLine(r, smtpDataMax)
		if err != nil {
			return nil, tooLong || long, err
		}
		if long {
			tooLong = true
			buf = nil
			continue
		}
		if line == "." {
			if tooLong {
				return nil, true, nil
			}
			return buf, false, nil
		}
		if tooLong {
			continue
		}
		if strings.HasPrefix(line, ".") {
			line = line[1:]
		}
		buf = append(buf, line...)
		buf = append(buf, "\r\n"...)
	}
}
