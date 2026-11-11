package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// RFC 5321 limits, counting the trailing CRLF.
const (
	smtpCmdMax  = 512
	smtpDataMax = 1000
)

type smtpRcptView struct {
	Address string `json:"address"`
	Result  string `json:"result"`
}

type smtpRecord struct {
	From       string         `json:"from"`
	Recipients []smtpRcptView `json:"recipients"`
}

type smtpWaiter struct {
	goal int
	ch   chan struct{}
}

type smtpEnv struct {
	from    string
	hasFrom bool
	rcpts   []smtpRcpt
}

type smtpRcpt struct {
	addr    string
	deliver bool
}

type smtpConn struct {
	srv    *Server
	c      net.Conn
	br     *bufio.Reader
	bw     *bufio.Writer
	authed bool
	env    smtpEnv
}

func (s *Server) acceptSMTP() {
	for {
		c, err := s.smtpLn.Accept()
		if err != nil {
			return
		}
		go s.serveSMTP(c)
	}
}

func (s *Server) serveSMTP(c net.Conn) {
	defer c.Close()
	sc := &smtpConn{srv: s, c: c, br: bufio.NewReader(c), bw: bufio.NewWriter(c)}
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
	}
	return out
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
	if _, err := fmt.Fprintf(sc.bw, "%d %s\r\n", code, text); err != nil {
		return err
	}
	return sc.bw.Flush()
}

func (sc *smtpConn) replyLines(code int, lines []string) error {
	for i, line := range lines {
		sep := byte(' ')
		if i < len(lines)-1 {
			sep = '-'
		}
		if _, err := fmt.Fprintf(sc.bw, "%d%c%s\r\n", code, sep, line); err != nil {
			return err
		}
	}
	return sc.bw.Flush()
}

func (sc *smtpConn) cont(b64 string) error {
	line := "334\r\n"
	if b64 != "" {
		line = "334 " + b64 + "\r\n"
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
	switch verb {
	case "EHLO":
		return sc.replyLines(250, []string{"fakeimap", "AUTH PLAIN LOGIN", "8BITMIME"})
	case "HELO":
		return sc.reply(250, "fakeimap")
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
		if !sc.authed {
			return sc.reply(530, "5.7.0 authentication required")
		}
		return sc.cmdMail(arg)
	case "RCPT":
		if !sc.authed {
			return sc.reply(530, "5.7.0 authentication required")
		}
		return sc.cmdRcpt(arg)
	case "DATA":
		if !sc.authed {
			return sc.reply(530, "5.7.0 authentication required")
		}
		return sc.cmdData()
	default:
		return sc.reply(502, "5.5.1 command not implemented")
	}
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
	case "":
		return sc.reply(501, "5.5.4 mechanism required")
	default:
		return sc.reply(504, "5.5.4 unrecognized authentication type")
	}
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
	sc.authed = true
	return sc.reply(235, "2.7.0 authentication successful")
}

func (sc *smtpConn) cmdMail(arg string) error {
	if sc.env.hasFrom {
		return sc.reply(503, "5.5.1 sender already set")
	}
	addr, ok := parseSMTPPath(arg, "FROM:")
	if !ok {
		return sc.reply(501, "5.5.4 bad sender")
	}
	sc.env.from = addr
	sc.env.hasFrom = true
	return sc.reply(250, "2.1.0 OK")
}

func (sc *smtpConn) cmdRcpt(arg string) error {
	if !sc.env.hasFrom {
		return sc.reply(503, "5.5.1 need sender")
	}
	addr, ok := parseSMTPPath(arg, "TO:")
	if !ok {
		return sc.reply(501, "5.5.4 bad recipient")
	}
	sc.env.rcpts = append(sc.env.rcpts, smtpRcpt{addr: addr, deliver: sc.srv.smtpDelivers(addr)})
	return sc.reply(250, "2.1.5 OK")
}

func (sc *smtpConn) cmdData() error {
	if !sc.env.hasFrom || len(sc.env.rcpts) == 0 {
		return sc.reply(503, "5.5.1 no recipient")
	}
	if err := sc.reply(354, "start mail input"); err != nil {
		return err
	}
	raw, tooLong, err := readSMTPData(sc.br)
	if err != nil {
		return err
	}
	recips := sc.env.rcpts
	from := sc.env.from
	sc.env = smtpEnv{}
	if tooLong {
		return sc.reply(500, "5.5.2 line too long")
	}
	if err := sc.srv.acceptMail(from, recips, raw); err != nil {
		return sc.reply(451, "4.3.0 delivery failed")
	}
	return sc.reply(250, "2.0.0 OK")
}

// acceptMail is the SMTP DATA accept path. A dataset recipient appends the
// message to INBOX once. Any other recipient is accepted and not stored.
// list-smtp and wait-smtp see the same record either caller produces.
func (s *Server) acceptMail(from string, rcpts []smtpRcpt, raw []byte) error {
	views := make([]smtpRcptView, len(rcpts))
	deliver := false
	for i, r := range rcpts {
		result := "discarded"
		if r.deliver {
			result = "delivered"
			deliver = true
		}
		views[i] = smtpRcptView{Address: r.addr, Result: result}
	}
	if deliver {
		if _, _, err := s.store.appendMsg(nil, "INBOX", nil, nil, raw, s.frozenTime()); err != nil {
			return err
		}
	}
	s.acceptSMTPMsg(smtpRecord{From: from, Recipients: views})
	return nil
}

func parseSMTPPath(arg, keyword string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) < len(keyword) || !strings.EqualFold(arg[:len(keyword)], keyword) {
		return "", false
	}
	rest := strings.TrimSpace(arg[len(keyword):])
	if rest == "" {
		return "", false
	}
	if rest[0] == '<' {
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			return "", false
		}
		return rest[1:end], true
	}
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	if rest == "" {
		return "", false
	}
	return rest, true
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
