package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// ManageSieve (RFC 5804) stores scripts per account. It does not run them.
// CHECKSCRIPT does not parse Sieve.

const sieveLitMax = 1 << 20

var (
	errSieveClose  = errors.New("sieve close")
	errSieveSyntax = errors.New("sieve syntax")
	errSieveCancel = errors.New("sieve cancel")
)

type sieveAcct struct {
	order  []string
	body   map[string][]byte
	active string
}

type sieveArg struct {
	text string
	n    int
	plus bool
	lit  bool
}

type sieveVal struct {
	text  string
	raw   []byte
	isRaw bool
}

type sieveConn struct {
	srv    *Server
	c      net.Conn
	br     *bufio.Reader
	bw     *bufio.Writer
	id     int
	user   string
	secure bool
}

func (s *Server) acceptSieve() {
	for {
		c, err := s.sieveLn.Accept()
		if err != nil {
			return
		}
		go s.serveSieve(c)
	}
}

func (s *Server) serveSieve(c net.Conn) {
	defer c.Close()
	sc := &sieveConn{
		srv: s,
		c:   c,
		br:  bufio.NewReader(c),
		bw:  bufio.NewWriter(c),
		id:  int(s.connSeq.Add(1)),
	}
	sc.serve()
}

func (s *Server) sieveGreeting(secure bool) []string {
	lines := []string{
		sieveQuote("IMPLEMENTATION") + " " + sieveQuote("fakeimap"),
		sieveQuote("VERSION") + " " + sieveQuote("1.0"),
		sieveQuote("SASL") + " " + sieveQuote("PLAIN CRAM-MD5"),
		sieveQuote("SIEVE") + " " + sieveQuote("fileinto include"),
	}
	if s.hasCert && !secure {
		lines = append(lines, sieveQuote("STARTTLS"))
	}
	return lines
}

func (s *Server) clearSieve() {
	s.sieveMu.Lock()
	s.sieveAccts = map[string]*sieveAcct{}
	s.sieveMu.Unlock()
}

func (s *Server) sieveAcct(user string) *sieveAcct {
	a := s.sieveAccts[user]
	if a == nil {
		a = &sieveAcct{body: map[string][]byte{}}
		s.sieveAccts[user] = a
	}
	return a
}

func (s *Server) sievePut(user, name string, body []byte) {
	s.sieveMu.Lock()
	defer s.sieveMu.Unlock()
	a := s.sieveAcct(user)
	if _, ok := a.body[name]; !ok {
		a.order = append(a.order, name)
	}
	a.body[name] = append([]byte(nil), body...)
}

func (s *Server) sieveGet(user, name string) ([]byte, bool) {
	s.sieveMu.Lock()
	defer s.sieveMu.Unlock()
	a := s.sieveAccts[user]
	if a == nil {
		return nil, false
	}
	b, ok := a.body[name]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), b...), true
}

func (s *Server) sieveList(user string) ([]string, string) {
	s.sieveMu.Lock()
	defer s.sieveMu.Unlock()
	a := s.sieveAccts[user]
	if a == nil {
		return nil, ""
	}
	return append([]string(nil), a.order...), a.active
}

func (s *Server) sieveSetActive(user, name string) bool {
	s.sieveMu.Lock()
	defer s.sieveMu.Unlock()
	a := s.sieveAcct(user)
	if name == "" {
		a.active = ""
		return true
	}
	if _, ok := a.body[name]; !ok {
		return false
	}
	a.active = name
	return true
}

func (sc *sieveConn) serve() {
	if err := sc.greet(); err != nil {
		return
	}
	for {
		if err := sc.command(); err != nil {
			return
		}
	}
}

func (sc *sieveConn) greet() error {
	for _, line := range sc.srv.sieveGreeting(sc.secure) {
		if err := sc.send(line); err != nil {
			return err
		}
	}
	return sc.ok("")
}

func (sc *sieveConn) command() error {
	line, err := sc.readLine()
	if err != nil {
		return err
	}
	args, err := splitSieve(line)
	if err != nil {
		return sc.no("syntax")
	}
	vals := make([]sieveVal, 0, len(args))
	for i, a := range args {
		if a.lit {
			if i != len(args)-1 {
				return sc.no("syntax")
			}
			b, err := sc.readLit(a.n, a.plus)
			if errors.Is(err, errSieveSyntax) {
				return sc.no("syntax")
			}
			if err != nil {
				return err
			}
			vals = append(vals, sieveVal{raw: b, isRaw: true})
			continue
		}
		vals = append(vals, sieveVal{text: a.text})
	}
	if len(vals) == 0 || vals[0].isRaw || vals[0].text == "" {
		return sc.no("syntax")
	}
	cmd := strings.ToUpper(vals[0].text)
	rest := vals[1:]
	switch cmd {
	case "LOGOUT":
		if len(rest) != 0 {
			return sc.no("syntax")
		}
		if err := sc.ok(""); err != nil {
			return err
		}
		return errSieveClose
	case "STARTTLS":
		return sc.cmdStartTLS(rest)
	case "AUTHENTICATE":
		return sc.cmdAuth(rest)
	case "LISTSCRIPTS":
		return sc.cmdList(rest)
	case "GETSCRIPT":
		return sc.cmdGet(rest)
	case "PUTSCRIPT":
		return sc.cmdPut(rest)
	case "CHECKSCRIPT":
		return sc.cmdCheck(rest)
	case "SETACTIVE":
		return sc.cmdSetActive(rest)
	default:
		return sc.no("unknown command")
	}
}

func (sc *sieveConn) cmdStartTLS(rest []sieveVal) error {
	if len(rest) != 0 {
		return sc.no("syntax")
	}
	if sc.secure || !sc.srv.hasCert || sc.user != "" {
		return sc.no("STARTTLS not available")
	}
	if err := sc.ok(""); err != nil {
		return err
	}
	var pre []byte
	if n := sc.br.Buffered(); n > 0 {
		pre = make([]byte, n)
		if _, err := io.ReadFull(sc.br, pre); err != nil {
			return err
		}
	}
	tc := sc.srv.tlsServerFor(&prefixConn{Conn: sc.c, pre: pre}, sc.id, "STARTTLS")
	if err := tc.Handshake(); err != nil {
		return err
	}
	sc.c = tc
	sc.br = bufio.NewReader(tc)
	sc.bw = bufio.NewWriter(tc)
	sc.secure = true
	return sc.greet()
}

func (sc *sieveConn) cmdAuth(rest []sieveVal) error {
	if sc.user != "" {
		return sc.no("already authenticated")
	}
	if len(rest) < 1 || len(rest) > 2 || rest[0].isRaw || rest[0].text == "" {
		return sc.no("syntax")
	}
	switch strings.ToUpper(rest[0].text) {
	case "PLAIN":
		return sc.authPlain(rest)
	case "CRAM-MD5":
		return sc.authCRAM(rest)
	default:
		return sc.no("mechanism not available")
	}
}

func (sc *sieveConn) authPlain(rest []sieveVal) error {
	var b64 string
	if len(rest) == 2 {
		if rest[1].isRaw {
			b64 = string(rest[1].raw)
		} else {
			b64 = rest[1].text
		}
	} else {
		s, err := sc.readOneString()
		if errors.Is(err, errSieveCancel) {
			return sc.no("cancelled")
		}
		if errors.Is(err, errSieveSyntax) {
			return sc.no("syntax")
		}
		if err != nil {
			return err
		}
		b64 = s
	}
	raw, err := decodeB64(b64)
	if err != nil {
		return sc.no("authentication failed")
	}
	user, pass, ok := plainCreds(raw)
	if !ok || !sc.srv.store.check(user, pass) {
		return sc.no("authentication failed")
	}
	sc.user = user
	return sc.ok("")
}

func (sc *sieveConn) authCRAM(rest []sieveVal) error {
	if len(rest) != 1 {
		return sc.no("authentication failed")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	challenge := fmt.Sprintf("<%d.%x@fakeimap>", time.Now().UnixNano(), nonce)
	if err := sc.send(sieveQuote(b64std([]byte(challenge)))); err != nil {
		return err
	}
	line, err := sc.readOneString()
	if errors.Is(err, errSieveCancel) {
		return sc.no("cancelled")
	}
	if errors.Is(err, errSieveSyntax) {
		return sc.no("syntax")
	}
	if err != nil {
		return err
	}
	raw, err := decodeB64(line)
	if err != nil {
		return sc.no("authentication failed")
	}
	user, digest, ok := strings.Cut(string(raw), " ")
	if !ok || user == "" || strings.Contains(digest, " ") {
		return sc.no("authentication failed")
	}
	pass, found := sc.srv.store.secret(user)
	if !found {
		pass = "secret"
	}
	sum, decErr := hex.DecodeString(digest)
	want, _ := hex.DecodeString(cramHex(pass, challenge))
	if decErr != nil || !found || !hmac.Equal(sum, want) {
		return sc.no("authentication failed")
	}
	sc.user = user
	return sc.ok("")
}

func (sc *sieveConn) cmdList(rest []sieveVal) error {
	if len(rest) != 0 {
		return sc.no("syntax")
	}
	if sc.user == "" {
		return sc.no("authenticate first")
	}
	names, active := sc.srv.sieveList(sc.user)
	for _, name := range names {
		line := sieveQuote(name)
		if name == active {
			line += " ACTIVE"
		}
		if err := sc.send(line); err != nil {
			return err
		}
	}
	return sc.ok("")
}

func (sc *sieveConn) cmdGet(rest []sieveVal) error {
	if len(rest) != 1 || rest[0].isRaw || rest[0].text == "" {
		return sc.no("syntax")
	}
	if sc.user == "" {
		return sc.no("authenticate first")
	}
	b, ok := sc.srv.sieveGet(sc.user, rest[0].text)
	if !ok {
		return sc.no("no such script")
	}
	if err := sc.sendLiteral(b); err != nil {
		return err
	}
	return sc.ok("")
}

func (sc *sieveConn) cmdPut(rest []sieveVal) error {
	if len(rest) != 2 || rest[0].isRaw || rest[0].text == "" || !rest[1].isRaw {
		return sc.no("syntax")
	}
	if sc.user == "" {
		return sc.no("authenticate first")
	}
	if len(rest[1].raw) == 0 {
		return sc.no("empty script")
	}
	sc.srv.sievePut(sc.user, rest[0].text, rest[1].raw)
	return sc.ok("")
}

func (sc *sieveConn) cmdCheck(rest []sieveVal) error {
	if len(rest) != 1 || !rest[0].isRaw {
		return sc.no("syntax")
	}
	if sc.user == "" {
		return sc.no("authenticate first")
	}
	if len(rest[0].raw) == 0 {
		return sc.no("empty script")
	}
	return sc.ok("")
}

func (sc *sieveConn) cmdSetActive(rest []sieveVal) error {
	if len(rest) != 1 || rest[0].isRaw {
		return sc.no("syntax")
	}
	if sc.user == "" {
		return sc.no("authenticate first")
	}
	if !sc.srv.sieveSetActive(sc.user, rest[0].text) {
		return sc.no("no such script")
	}
	return sc.ok("")
}

func (sc *sieveConn) send(line string) error {
	if _, err := sc.bw.WriteString(line); err != nil {
		return err
	}
	if _, err := sc.bw.WriteString("\r\n"); err != nil {
		return err
	}
	return sc.bw.Flush()
}

func (sc *sieveConn) ok(text string) error {
	if text == "" {
		return sc.send("OK")
	}
	return sc.send("OK " + sieveQuote(text))
}

func (sc *sieveConn) no(text string) error {
	return sc.send("NO " + sieveQuote(text))
}

// The octet count is the script only. A following CRLF is the literal tail
// the client reads before the OK line.
func (sc *sieveConn) sendLiteral(b []byte) error {
	if _, err := fmt.Fprintf(sc.bw, "{%d}\r\n", len(b)); err != nil {
		return err
	}
	if _, err := sc.bw.Write(b); err != nil {
		return err
	}
	if _, err := sc.bw.WriteString("\r\n"); err != nil {
		return err
	}
	return sc.bw.Flush()
}

func (sc *sieveConn) readLine() (string, error) {
	line, err := sc.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > 65536 {
		return "", errSieveClose
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

// A non-synchronizing literal is followed by CRLF. The liveimap client sends
// that CRLF after the octets and waits for it to be consumed.
func (sc *sieveConn) readLit(n int, plus bool) ([]byte, error) {
	if n < 0 || n > sieveLitMax {
		return nil, errSieveClose
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(sc.br, buf); err != nil {
		return nil, err
	}
	if plus {
		line, err := sc.readLine()
		if err != nil {
			return nil, err
		}
		if line != "" {
			return nil, errSieveSyntax
		}
	}
	return buf, nil
}

func (sc *sieveConn) readOneString() (string, error) {
	line, err := sc.readLine()
	if err != nil {
		return "", err
	}
	if line == "*" {
		return "", errSieveCancel
	}
	args, err := splitSieve(line)
	if err != nil || len(args) != 1 {
		return "", errSieveSyntax
	}
	a := args[0]
	if a.lit {
		b, err := sc.readLit(a.n, a.plus)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return a.text, nil
}

func splitSieve(line string) ([]sieveArg, error) {
	var args []sieveArg
	i := 0
	for i < len(line) {
		if line[i] == ' ' || line[i] == '\t' {
			i++
			continue
		}
		if line[i] == '"' {
			s, next, err := unquoteSieve(line, i)
			if err != nil {
				return nil, err
			}
			args = append(args, sieveArg{text: s})
			i = next
			continue
		}
		j := i
		for j < len(line) && line[j] != ' ' && line[j] != '\t' {
			j++
		}
		tok := line[i:j]
		if strings.HasPrefix(tok, "{") {
			if j != len(line) {
				return nil, errSieveSyntax
			}
			n, plus, ok := parseSieveLit(tok)
			if !ok {
				return nil, errSieveSyntax
			}
			return append(args, sieveArg{lit: true, n: n, plus: plus}), nil
		}
		args = append(args, sieveArg{text: tok})
		i = j
	}
	return args, nil
}

func parseSieveLit(tok string) (int, bool, bool) {
	if len(tok) < 3 || tok[0] != '{' || tok[len(tok)-1] != '}' {
		return 0, false, false
	}
	body := tok[1 : len(tok)-1]
	plus := false
	if strings.HasSuffix(body, "+") {
		plus = true
		body = body[:len(body)-1]
	}
	if body == "" {
		return 0, false, false
	}
	n, err := strconv.Atoi(body)
	if err != nil || n < 0 {
		return 0, false, false
	}
	return n, plus, true
}

func unquoteSieve(line string, open int) (string, int, error) {
	var b strings.Builder
	i := open + 1
	for i < len(line) {
		c := line[i]
		if c == '\\' {
			if i+1 >= len(line) {
				return "", 0, errSieveSyntax
			}
			n := line[i+1]
			if n == '\\' || n == '"' {
				b.WriteByte(n)
				i += 2
				continue
			}
			b.WriteByte(c)
			i++
			continue
		}
		if c == '"' {
			return b.String(), i + 1, nil
		}
		b.WriteByte(c)
		i++
	}
	return "", 0, errSieveSyntax
}

func sieveQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '"' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}
