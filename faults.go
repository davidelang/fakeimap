package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fault is one armed control-socket fault. hits and spent move only
// while Server.mu is held. A session keeps the pointer for one command.
type fault struct {
	id       int
	conn     int
	command  string
	times    int
	do       string
	delayMs  int
	bytes    int
	text     string
	count    int
	offset   int
	tag      string
	order    []string
	hits     int
	spent    bool
	certOnce sync.Once
	cert     *tls.Certificate
}

type faultView struct {
	ID      int      `json:"id"`
	Conn    int      `json:"conn"`
	Command string   `json:"command,omitempty"`
	Times   int      `json:"times"`
	Do      string   `json:"do"`
	DelayMs int      `json:"delay_ms,omitempty"`
	Bytes   int      `json:"bytes,omitempty"`
	Text    string   `json:"text,omitempty"`
	Count   int      `json:"count,omitempty"`
	Offset  int      `json:"offset,omitempty"`
	Tag     string   `json:"tag,omitempty"`
	Order   []string `json:"order,omitempty"`
	Spent   bool     `json:"spent,omitempty"`
}

func (f *fault) view() faultView {
	return faultView{
		ID: f.id, Conn: f.conn, Command: f.command, Times: f.times, Do: f.do,
		DelayMs: f.delayMs, Bytes: f.bytes, Text: f.text, Count: f.count,
		Offset: f.offset, Tag: f.tag, Order: f.order, Spent: f.spent,
	}
}

func isCertDo(do string) bool {
	switch do {
	case "wrong_name", "expired", "self_signed":
		return true
	default:
		return false
	}
}

// takeHit counts one match. times 0 fires every time. times N fires on
// the Nth match only, then the fault is spent.
func takeHit(f *fault) *fault {
	f.hits++
	if f.times > 0 && f.hits < f.times {
		return nil
	}
	if f.times > 0 && f.hits > f.times {
		f.spent = true
		return nil
	}
	if f.times > 0 {
		f.spent = true
	}
	return f
}

func (s *Server) matchReplyFault(id int, command string) *fault {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.faults {
		if f.spent || isCertDo(f.do) || f.command == "" {
			continue
		}
		if f.conn != 0 && f.conn != id {
			continue
		}
		if !strings.EqualFold(f.command, command) {
			continue
		}
		return takeHit(f)
	}
	return nil
}

func (s *Server) handshakeCert(id int, command string) *tls.Certificate {
	s.mu.Lock()
	var picked *fault
	for _, cand := range s.faults {
		if cand.spent || !isCertDo(cand.do) {
			continue
		}
		if cand.conn != 0 && cand.conn != id {
			continue
		}
		if cand.command != "" && !strings.EqualFold(cand.command, command) {
			continue
		}
		picked = takeHit(cand)
		break
	}
	s.mu.Unlock()
	if picked == nil {
		return nil
	}
	return picked.ensureCert()
}

func (f *fault) ensureCert() *tls.Certificate {
	f.certOnce.Do(func() {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return
		}
		now := time.Now()
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(now.UnixNano()),
			Subject:      pkix.Name{CommonName: "localhost"},
			NotBefore:    now.Add(-time.Hour),
			NotAfter:     now.Add(24 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			DNSNames:     []string{"localhost"},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		}
		switch f.do {
		case "wrong_name":
			tmpl.Subject = pkix.Name{CommonName: "wrong.example"}
			tmpl.DNSNames = []string{"wrong.example"}
			tmpl.IPAddresses = nil
		case "expired":
			tmpl.NotBefore = now.Add(-48 * time.Hour)
			tmpl.NotAfter = now.Add(-time.Hour)
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			return
		}
		leaf, err := x509.ParseCertificate(der)
		if err != nil {
			return
		}
		f.cert = &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
	})
	return f.cert
}

func (s *session) itemRank(it string) int {
	if s.fault != nil && s.fault.do == "fetch_order" {
		for i, name := range s.fault.order {
			if strings.EqualFold(name, it) {
				return i
			}
		}
		return len(s.fault.order) + s.pers.fetchRank(it)
	}
	return s.pers.fetchRank(it)
}

func (s *session) runCommand(cmd command) error {
	s.srv.logLine("command", s.id, joinCmd(cmd.tag, cmd.name, cmd.uid, cmd.args, cmd.fetch))
	if err := s.flush(); err != nil {
		return err
	}
	f := s.srv.matchReplyFault(s.id, cmd.name)
	if f == nil {
		return s.dispatch(cmd)
	}
	return s.applyFault(cmd, f)
}

func (s *session) applyFault(cmd command, f *fault) error {
	s.fault = f
	defer func() { s.fault = nil }()
	switch f.do {
	case "close", "rst", "half_close":
		return s.emitFault(f, cmd.tag, nil)
	case "reorder":
		return s.applyReorder(cmd)
	case "coalesce":
		return s.applyCoalesce(cmd)
	default:
		buf, err := s.captureDispatch(cmd)
		if sendErr := s.emitFault(f, cmd.tag, buf); sendErr != nil {
			return sendErr
		}
		return err
	}
}

// applyReorder finishes the next pipelined command before this reply.
func (s *session) applyReorder(cmd command) error {
	buf, err1 := s.captureDispatch(cmd)
	cmd2, err := s.readCommand()
	if err != nil {
		if len(buf) > 0 {
			_ = s.writeParts(buf)
		}
		return err
	}
	if err := s.runCommand(cmd2); err != nil {
		if len(buf) > 0 {
			_ = s.writeParts(buf)
		}
		return err
	}
	if err := s.writeParts(buf); err != nil {
		return err
	}
	return err1
}

// applyCoalesce writes this reply and the next command's reply in one socket write.
func (s *session) applyCoalesce(cmd command) error {
	buf1, err1 := s.captureDispatch(cmd)
	cmd2, err := s.readCommand()
	if err != nil {
		if len(buf1) > 0 {
			_ = s.writeParts(buf1)
		}
		return err
	}
	s.srv.logLine("command", s.id, joinCmd(cmd2.tag, cmd2.name, cmd2.uid, cmd2.args, cmd2.fetch))
	buf2, err2 := s.captureSecond(cmd2)
	out := append(append([]byte{}, buf1...), buf2...)
	if err := s.writeParts(out); err != nil {
		return err
	}
	if err1 != nil {
		return err1
	}
	return err2
}

func (s *session) captureDispatch(cmd command) ([]byte, error) {
	buf := &bytes.Buffer{}
	s.wmu.Lock()
	prev := s.capture
	s.capture = buf
	s.wmu.Unlock()
	err := s.dispatch(cmd)
	s.wmu.Lock()
	s.capture = prev
	out := append([]byte(nil), buf.Bytes()...)
	s.wmu.Unlock()
	return out, err
}

func (s *session) captureSecond(cmd command) ([]byte, error) {
	f := s.srv.matchReplyFault(s.id, cmd.name)
	if f != nil && (f.do == "reorder" || f.do == "coalesce" || f.do == "close" || f.do == "rst" || f.do == "half_close") {
		f = nil
	}
	outer := s.fault
	s.fault = f
	buf := &bytes.Buffer{}
	s.wmu.Lock()
	prev := s.capture
	s.capture = buf
	s.wmu.Unlock()
	err := s.flush()
	if err == nil {
		err = s.dispatch(cmd)
	}
	s.wmu.Lock()
	s.capture = prev
	out := append([]byte(nil), buf.Bytes()...)
	s.wmu.Unlock()
	s.fault = outer
	if f == nil {
		return out, err
	}
	if f.do == "delay" && f.delayMs > 0 {
		time.Sleep(time.Duration(f.delayMs) * time.Millisecond)
	}
	var joined []byte
	for _, p := range faultParts(f, cmd.tag, out) {
		joined = append(joined, p...)
	}
	return joined, err
}

func (s *session) emitFault(f *fault, tag string, reply []byte) error {
	switch f.do {
	case "silent":
		return nil
	case "close":
		return io.EOF
	case "rst":
		if tc := tcpConnOf(s.conn); tc != nil {
			_ = tc.SetLinger(0)
		}
		return io.EOF
	case "half_close":
		_ = closeWrite(s.conn)
		return io.EOF
	case "delay":
		if f.delayMs > 0 {
			time.Sleep(time.Duration(f.delayMs) * time.Millisecond)
		}
		return s.writeParts(reply)
	case "drip":
		setNoDelay(s.conn)
		parts := dripParts(reply)
		if len(parts) == 0 {
			return nil
		}
		if err := s.writeParts(parts[0]); err != nil {
			return err
		}
		if len(parts) == 1 {
			return nil
		}
		if f.delayMs > 0 {
			time.Sleep(time.Duration(f.delayMs) * time.Millisecond)
		}
		return s.writeParts(parts[1])
	default:
		return s.writeParts(faultParts(f, tag, reply)...)
	}
}

func (s *session) writeParts(parts ...[]byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := s.bw.Flush(); err != nil {
		return err
	}
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		if _, err := s.conn.Write(p); err != nil {
			return err
		}
		s.srv.logLine("reply", s.id, string(p))
	}
	return nil
}

func tcpConnOf(c net.Conn) *net.TCPConn {
	for i := 0; i < 8 && c != nil; i++ {
		switch t := c.(type) {
		case *net.TCPConn:
			return t
		case *tls.Conn:
			c = t.NetConn()
		case *prefixConn:
			c = t.Conn
		default:
			return nil
		}
	}
	return nil
}

func setNoDelay(c net.Conn) {
	if tc := tcpConnOf(c); tc != nil {
		_ = tc.SetNoDelay(true)
	}
}

func closeWrite(c net.Conn) error {
	if tc := tcpConnOf(c); tc != nil {
		return tc.CloseWrite()
	}
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

func lineCRLF(text string) []byte {
	if strings.HasSuffix(text, "\r\n") {
		return []byte(text)
	}
	if strings.HasSuffix(text, "\n") {
		return []byte(strings.TrimSuffix(text, "\n") + "\r\n")
	}
	return []byte(text + "\r\n")
}

func dripParts(reply []byte) [][]byte {
	if len(reply) < 2 {
		if len(reply) == 0 {
			return nil
		}
		return [][]byte{reply}
	}
	mid := len(reply) / 2
	return [][]byte{reply[:mid], reply[mid:]}
}

func faultParts(f *fault, tag string, reply []byte) [][]byte {
	switch f.do {
	case "silent", "close", "rst", "half_close":
		return nil
	case "truncate":
		n := f.bytes
		if n < 0 {
			n = 0
		}
		if n > len(reply) {
			n = len(reply)
		}
		return [][]byte{reply[:n]}
	case "raw":
		return [][]byte{lineCRLF(f.text)}
	case "bad_literal":
		return [][]byte{badLiteral(reply)}
	case "flood":
		line := lineCRLF(f.text)
		parts := make([][]byte, 0, f.count+1)
		for i := 0; i < f.count; i++ {
			parts = append(parts, line)
		}
		parts = append(parts, reply)
		return parts
	case "split":
		off := f.offset
		if off < 1 || off >= len(reply) {
			if len(reply) >= 2 {
				off = 1
			} else {
				return [][]byte{reply}
			}
		}
		return [][]byte{reply[:off], reply[off:]}
	case "wrong_tag":
		return [][]byte{rewriteTag(reply, tag, f.tag)}
	case "unsolicited":
		return [][]byte{append(lineCRLF(f.text), reply...)}
	case "mid":
		return [][]byte{insertAt(reply, f.offset, f.text)}
	case "tag_first":
		return [][]byte{moveTagFirst(reply, tag)}
	default:
		return [][]byte{reply}
	}
}

func insertAt(reply []byte, off int, text string) []byte {
	if off < 0 {
		off = 0
	}
	if off > len(reply) {
		off = len(reply)
	}
	out := make([]byte, 0, len(reply)+len(text))
	out = append(out, reply[:off]...)
	out = append(out, text...)
	out = append(out, reply[off:]...)
	return out
}

func imapLiteralLen(line []byte) (int, bool) {
	trim := bytes.TrimRight(line, "\r\n")
	i := bytes.LastIndexByte(trim, '{')
	if i < 0 || !bytes.HasSuffix(trim, []byte("}")) {
		return 0, false
	}
	inner := trim[i+1 : len(trim)-1]
	if bytes.HasSuffix(inner, []byte("+")) {
		inner = bytes.TrimSuffix(inner, []byte("+"))
	}
	if len(inner) == 0 {
		return 0, false
	}
	n := 0
	for _, c := range inner {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func splitIMAPRecords(reply []byte) [][]byte {
	var recs [][]byte
	for i := 0; i < len(reply); {
		nl := bytes.IndexByte(reply[i:], '\n')
		if nl < 0 {
			recs = append(recs, reply[i:])
			break
		}
		end := i + nl + 1
		line := reply[i:end]
		if n, ok := imapLiteralLen(line); ok {
			take := end + n
			if take > len(reply) {
				take = len(reply)
			}
			recs = append(recs, reply[i:take])
			i = take
			continue
		}
		recs = append(recs, line)
		i = end
	}
	return recs
}

func rewriteTag(reply []byte, oldTag, newTag string) []byte {
	if oldTag == "" || newTag == "" || oldTag == newTag {
		return reply
	}
	recs := splitIMAPRecords(reply)
	pref := []byte(oldTag + " ")
	for i, rec := range recs {
		if bytes.HasPrefix(rec, pref) {
			recs[i] = append([]byte(newTag+" "), rec[len(pref):]...)
			break
		}
	}
	return bytes.Join(recs, nil)
}

func moveTagFirst(reply []byte, tag string) []byte {
	recs := splitIMAPRecords(reply)
	pref := []byte(tag + " ")
	idx := -1
	for i, rec := range recs {
		if bytes.HasPrefix(rec, pref) {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return reply
	}
	out := append([]byte(nil), recs[idx]...)
	for i, rec := range recs {
		if i == idx {
			continue
		}
		out = append(out, rec...)
	}
	return out
}

func badLiteral(reply []byte) []byte {
	fallback := []byte("* 1 FETCH (RFC822 {4}\r\nZZ)\r\n")
	i := bytes.IndexByte(reply, '{')
	if i < 0 {
		return fallback
	}
	rel := bytes.IndexByte(reply[i:], '}')
	if rel < 0 {
		return fallback
	}
	j := i + rel
	num := reply[i+1 : j]
	plus := bytes.HasSuffix(num, []byte("+"))
	if plus {
		num = num[:len(num)-1]
	}
	n, err := strconv.Atoi(string(num))
	if err != nil {
		return fallback
	}
	repl := strconv.Itoa(n + 1)
	if plus {
		repl += "+"
	}
	var b bytes.Buffer
	b.Write(reply[:i+1])
	b.WriteString(repl)
	b.Write(reply[j:])
	return b.Bytes()
}
