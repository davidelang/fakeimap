package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

func (s *Server) listenControl() error {
	path := s.cfg.Control
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSocket != 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return err
	}
	s.ctrlLn = ln
	s.bound.Control = path
	return nil
}

func (s *Server) acceptControl() {
	for {
		c, err := s.ctrlLn.Accept()
		if err != nil {
			return
		}
		go s.serveControl(c)
	}
}

func (s *Server) serveControl(c net.Conn) {
	defer c.Close()
	dec := json.NewDecoder(c)
	enc := json.NewEncoder(c)
	enc.SetEscapeHTML(false)
	for {
		var req ctrlRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		if err := enc.Encode(s.ctrl(req)); err != nil {
			return
		}
	}
}

type ctrlRequest struct {
	Op           string          `json:"op"`
	Path         string          `json:"path"`
	Conn         *int            `json:"conn"`
	Mailbox      string          `json:"mailbox"`
	User         string          `json:"user"`
	Username     string          `json:"username"`
	Password     string          `json:"password"`
	Raw          string          `json:"raw"`
	UID          *uint32         `json:"uid"`
	Time         string          `json:"time"`
	ID           *int            `json:"id"`
	Command      string          `json:"command"`
	Times        *int            `json:"times"`
	Do           string          `json:"do"`
	DelayMs      *int            `json:"delay_ms"`
	Bytes        *int            `json:"bytes"`
	Text         string          `json:"text"`
	Count        *int            `json:"count"`
	Offset       *int            `json:"offset"`
	Tag          string          `json:"tag"`
	Order        []string        `json:"order"`
	TimeoutMs    *int            `json:"timeout_ms"`
	Status       string          `json:"status"`
	Code         string          `json:"code"`
	Proto        string          `json:"proto"`
	Size         *int            `json:"size"`
	After        *int            `json:"after"`
	Value        json.RawMessage `json:"value"`
	Done         json.RawMessage `json:"done"`
	Total        json.RawMessage `json:"total"`
	Name         string          `json:"name"`
	NameB64      string          `json:"name_b64"`
	FailDelayMs  *int            `json:"fail_delay_ms"`
	LockoutAfter *int            `json:"lockout_after"`
	LockoutMs    *int            `json:"lockout_ms"`
	CertFile     string          `json:"cert_file"`
	KeyFile      string          `json:"key_file"`
}

func okResp(extra map[string]any) map[string]any {
	m := map[string]any{"ok": true}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func errResp(msg string) map[string]any {
	return map[string]any{"ok": false, "error": msg}
}

func (s *Server) ctrl(req ctrlRequest) any {
	switch req.Op {
	case "status":
		return s.ctrlStatus()
	case "reset":
		if err := s.resetDataset(); err != nil {
			return errResp(err.Error())
		}
		return okResp(nil)
	case "set-personality":
		if req.Path == "" {
			return errResp("missing field")
		}
		if err := s.setPersonality(req.Path); err != nil {
			return errResp(err.Error())
		}
		return okResp(nil)
	case "set-dataset":
		if req.Path == "" {
			return errResp("missing field")
		}
		if err := s.reloadDataset(req.Path); err != nil {
			return errResp(err.Error())
		}
		return okResp(nil)
	case "disconnect":
		if req.Conn == nil {
			return errResp("missing field")
		}
		if err := s.disconnectID(*req.Conn); err != nil {
			return errResp(err.Error())
		}
		return okResp(nil)
	case "inject":
		if req.Mailbox == "" || req.Raw == "" {
			return errResp("missing field")
		}
		if req.User != "" && !s.store.hasAccount(req.User) {
			return errResp("no such account")
		}
		_, uid, err := s.store.appendMsg(nil, req.Mailbox, nil, nil, []byte(req.Raw), s.frozenTime())
		if err != nil {
			return errResp(err.Error())
		}
		return okResp(map[string]any{"uid": uid})
	case "list-mail":
		if req.Mailbox == "" {
			return errResp("missing field")
		}
		items, err := s.store.listMail(req.Mailbox)
		if err != nil {
			return errResp(err.Error())
		}
		return okResp(map[string]any{"messages": items})
	case "fetch-mail":
		if req.Mailbox == "" || req.UID == nil {
			return errResp("missing field")
		}
		b, err := s.store.fetchMail(req.Mailbox, *req.UID)
		if err != nil {
			return errResp(err.Error())
		}
		return okResp(map[string]any{"rfc822": base64.StdEncoding.EncodeToString(b)})
	case "accounts":
		if req.Username == "" && req.Password == "" {
			return okResp(map[string]any{"usernames": s.store.usernames()})
		}
		if req.Username == "" || req.Password == "" {
			return errResp("missing field")
		}
		if err := s.store.addAccount(req.Username, req.Password); err != nil {
			return errResp(err.Error())
		}
		return okResp(map[string]any{"usernames": s.store.usernames()})
	case "freeze-clock":
		if req.Time == "" {
			return errResp("missing field")
		}
		t, err := time.Parse(time.RFC3339, req.Time)
		if err != nil {
			return errResp("bad time")
		}
		s.mu.Lock()
		s.frozen = &t
		s.mu.Unlock()
		return okResp(nil)
	case "thaw-clock":
		s.mu.Lock()
		s.frozen = nil
		s.mu.Unlock()
		return okResp(nil)
	case "bye":
		text := req.Text
		if text == "" {
			text = "Server logging out"
		}
		if err := s.byeConns(req.Conn, text); err != nil {
			return errResp(err.Error())
		}
		return okResp(nil)
	case "create-mailbox":
		if req.Name == "" && req.NameB64 == "" {
			return errResp("missing field")
		}
		if req.Name != "" && req.NameB64 != "" {
			return errResp("bad name")
		}
		name := req.Name
		if req.NameB64 != "" {
			b, err := base64.StdEncoding.DecodeString(req.NameB64)
			if err != nil {
				b, err = base64.RawStdEncoding.DecodeString(req.NameB64)
				if err != nil {
					return errResp("bad name")
				}
			}
			name = string(b)
		}
		if req.User != "" && !s.store.hasAccount(req.User) {
			return errResp("no such account")
		}
		if err := s.store.createRaw(name); err != nil {
			return errResp(err.Error())
		}
		return okResp(nil)
	case "set-auth-policy":
		delay, after, lock, err := authPolicyNums(req.FailDelayMs, req.LockoutAfter, req.LockoutMs)
		if err != nil {
			return errResp(err.Error())
		}
		s.setAuthPolicy(delay, after, lock)
		return okResp(nil)
	case "clear-auth-policy":
		s.setAuthPolicy(0, 0, 0)
		return okResp(nil)
	case "reload-cert":
		if (req.CertFile == "") != (req.KeyFile == "") {
			return errResp("missing field")
		}
		certFile, keyFile := req.CertFile, req.KeyFile
		if certFile == "" {
			s.mu.Lock()
			certFile, keyFile = s.cfg.CertFile, s.cfg.KeyFile
			s.mu.Unlock()
		}
		if err := s.installCert(certFile, keyFile); err != nil {
			return errResp(err.Error())
		}
		return okResp(nil)
	case "set-login-disabled":
		if len(bytes.TrimSpace(req.Value)) == 0 || string(req.Value) == "null" {
			return errResp("missing field")
		}
		var v bool
		if err := json.Unmarshal(req.Value, &v); err != nil {
			return errResp("missing field")
		}
		s.setLoginDisabled(v)
		return okResp(nil)
	case "arm-fault":
		f, err := buildFault(req)
		if err != nil {
			return errResp(err.Error())
		}
		if f.do == "expunge_mid" && !s.store.hasUID(f.uid) {
			return errResp("no such message")
		}
		id := s.armFault(f)
		return okResp(map[string]any{"id": id})
	case "clear-faults":
		if err := s.clearFaults(req.ID); err != nil {
			return errResp(err.Error())
		}
		return okResp(nil)
	case "list-smtp":
		return okResp(map[string]any{"messages": s.listSMTP()})
	case "wait-smtp":
		if req.Count == nil || *req.Count < 0 {
			return errResp("missing field")
		}
		if *req.Count == 0 {
			return okResp(nil)
		}
		if req.TimeoutMs == nil || *req.TimeoutMs < 0 {
			return errResp("missing field")
		}
		if !s.waitSMTP(*req.Count, time.Duration(*req.TimeoutMs)*time.Millisecond) {
			return errResp("timeout")
		}
		return okResp(nil)
	case "":
		return errResp("missing field")
	default:
		return errResp("unknown op")
	}
}

func (s *Server) ctrlStatus() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := make([]faultView, 0, len(s.faults))
	for _, f := range s.faults {
		views = append(views, f.view())
	}
	return map[string]any{
		"ok":                  true,
		"listen":              s.bound.Listen,
		"implicit_tls_listen": s.bound.ImplicitTLSListen,
		"control":             s.bound.Control,
		"smtp":                s.bound.Smtp,
		"jmap":                s.bound.Jmap,
		"jmap_tls":            s.bound.JmapTLS,
		"sieve":               s.bound.Sieve,
		"personality":         s.cfg.Personality,
		"dataset":             s.cfg.Dataset,
		"connections":         len(s.imap),
		"clock_frozen":        s.frozen != nil,
		"faults":              views,
	}
}

type authPolicy struct {
	delay time.Duration
	after int
	lock  time.Duration
}

type authFailState struct {
	n     int
	until time.Time
}

func authPolicyNums(delay, after, lock *int) (int, int, int, error) {
	d, err := nonNegInt(delay)
	if err != nil {
		return 0, 0, 0, err
	}
	a, err := nonNegInt(after)
	if err != nil {
		return 0, 0, 0, err
	}
	l, err := nonNegInt(lock)
	if err != nil {
		return 0, 0, 0, err
	}
	return d, a, l, nil
}

func nonNegInt(p *int) (int, error) {
	if p == nil {
		return 0, nil
	}
	if *p < 0 {
		return 0, fmt.Errorf("missing field")
	}
	return *p, nil
}

func authKey(conn int, user string) string {
	if user == "" {
		return "\x00" + strconv.Itoa(conn)
	}
	return user
}

func (s *Server) setAuthPolicy(delayMs, after, lockMs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authPol = authPolicy{
		delay: time.Duration(delayMs) * time.Millisecond,
		after: after,
		lock:  time.Duration(lockMs) * time.Millisecond,
	}
	s.authFails = nil
}

func (s *Server) authNowLocked() time.Time {
	if s.frozen != nil {
		return *s.frozen
	}
	return time.Now()
}

func (s *Server) authBlocked(conn int, user string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.authFails[authKey(conn, user)]
	if st == nil || st.until.IsZero() {
		return false
	}
	if !s.authNowLocked().Before(st.until) {
		st.until = time.Time{}
		st.n = 0
		return false
	}
	return true
}

func (s *Server) noteAuthFailure(conn int, user string) {
	s.mu.Lock()
	delay := s.authPol.delay
	after := s.authPol.after
	lockFor := s.authPol.lock
	if after > 0 && lockFor > 0 {
		key := authKey(conn, user)
		st := s.authFails[key]
		if st == nil {
			st = &authFailState{}
			if s.authFails == nil {
				s.authFails = map[string]*authFailState{}
			}
			s.authFails[key] = st
		}
		st.n++
		if st.n >= after {
			st.until = s.authNowLocked().Add(lockFor)
			st.n = 0
		}
	}
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
}

func (s *Server) clearAuthFailure(conn int, user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.authFails, authKey(conn, user))
}

func (s *Server) frozenTime() *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frozen == nil {
		return nil
	}
	t := *s.frozen
	return &t
}

func (s *Server) resetDataset() error {
	s.mu.Lock()
	dir := s.cfg.Dataset
	s.mu.Unlock()
	return s.reloadDataset(dir)
}

func (s *Server) reloadDataset(dir string) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	s.mu.Lock()
	delim := s.pers.Delimiter
	s.mu.Unlock()
	neu, err := loadStore(dir, delim)
	if err != nil {
		return err
	}
	s.dropIMAP()
	s.store.become(neu)
	s.mu.Lock()
	s.cfg.Dataset = dir
	s.faults = nil
	s.pausing = false
	s.mu.Unlock()
	s.clearSieve()
	return nil
}

func (s *Server) dropIMAP() {
	s.mu.Lock()
	s.pausing = true
	cs := make([]net.Conn, 0, len(s.imap))
	for _, c := range s.imap {
		cs = append(cs, c)
	}
	s.mu.Unlock()
	for _, c := range cs {
		_ = c.Close()
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		s.mu.Lock()
		n := len(s.imap)
		s.mu.Unlock()
		if n == 0 || time.Now().After(deadline) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *Server) setPersonality(path string) error {
	p, err := loadPersonality(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.pers = p
	s.cfg.Personality = path
	s.mu.Unlock()
	return nil
}

func (s *Server) disconnectID(id int) error {
	s.mu.Lock()
	c := s.imap[id]
	s.mu.Unlock()
	if c == nil {
		return fmt.Errorf("no such connection")
	}
	return c.Close()
}

// byeConns writes * BYE to each matched IMAP session and closes it.
// A nil conn, or conn 0, matches every IMAP connection.
func (s *Server) byeConns(conn *int, text string) error {
	s.mu.Lock()
	var ss []*session
	if conn != nil && *conn != 0 {
		sess := s.sessions[*conn]
		s.mu.Unlock()
		if sess == nil {
			return fmt.Errorf("no such connection")
		}
		ss = []*session{sess}
	} else {
		for _, sess := range s.sessions {
			ss = append(ss, sess)
		}
		s.mu.Unlock()
	}
	for _, sess := range ss {
		_ = sess.byeClose(text)
	}
	return nil
}

func (s *Server) armFault(f *fault) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextFault++
	f.id = s.nextFault
	s.faults = append(s.faults, f)
	return f.id
}

func (s *Server) setLoginDisabled(v bool) {
	s.mu.Lock()
	s.loginDisabled = &v
	s.mu.Unlock()
}

// loginDisabledOverride is the set-login-disabled value. Nil means the
// personality list and the certificate filter decide.
func (s *Server) loginDisabledOverride() *bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loginDisabled == nil {
		return nil
	}
	v := *s.loginDisabled
	return &v
}

func optNumber(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return "NIL", nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil || n.String() == "" {
		return "", fmt.Errorf("missing field")
	}
	return n.String(), nil
}

func (s *Server) clearFaults(id *int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == nil {
		s.faults = nil
		return nil
	}
	for i, f := range s.faults {
		if f.id == *id {
			s.faults = append(s.faults[:i], s.faults[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("no such fault")
}

func canonDo(do string) (string, bool) {
	switch strings.ToLower(do) {
	case "close", "rst", "half_close", "silent", "delay", "drip", "truncate", "raw",
		"bad_literal", "flood", "split", "coalesce", "wrong_tag", "unsolicited",
		"reorder", "mid", "fetch_order", "tag_first", "mutate", "wrong_name",
		"expired", "self_signed",
		"reply", "bye", "idle_burst", "drop_literal", "rst_literal", "overquota", "expunge_mid",
		"inprogress":
		return strings.ToLower(do), true
	default:
		return "", false
	}
}

func buildFault(req ctrlRequest) (*fault, error) {
	do, ok := canonDo(req.Do)
	if !ok {
		if req.Do == "" {
			return nil, fmt.Errorf("missing field")
		}
		return nil, fmt.Errorf("unknown fault")
	}
	f := &fault{do: do}
	if req.Proto != "" {
		p, ok := canonProto(req.Proto)
		if !ok {
			return nil, fmt.Errorf("unknown fault")
		}
		if p != "imap" {
			f.proto = p
		}
	}
	if req.Conn != nil {
		if *req.Conn < 0 {
			return nil, fmt.Errorf("missing field")
		}
		f.conn = *req.Conn
	}
	if req.Times != nil {
		if *req.Times < 0 {
			return nil, fmt.Errorf("missing field")
		}
		f.times = *req.Times
	}
	if !isCertDo(do) {
		if req.Command == "" {
			return nil, fmt.Errorf("missing field")
		}
		f.command = strings.ToUpper(req.Command)
	} else if req.Command != "" {
		f.command = strings.ToUpper(req.Command)
	}
	switch do {
	case "delay", "drip":
		if req.DelayMs == nil || *req.DelayMs < 0 {
			return nil, fmt.Errorf("missing field")
		}
		f.delayMs = *req.DelayMs
	case "truncate":
		if req.Bytes == nil || *req.Bytes < 0 {
			return nil, fmt.Errorf("missing field")
		}
		f.bytes = *req.Bytes
	case "raw", "unsolicited":
		if req.Text == "" {
			return nil, fmt.Errorf("missing field")
		}
		f.text = req.Text
	case "flood":
		if req.Text == "" || req.Count == nil || *req.Count < 1 {
			return nil, fmt.Errorf("missing field")
		}
		f.text = req.Text
		f.count = *req.Count
	case "split":
		if req.Offset == nil || *req.Offset < 1 {
			return nil, fmt.Errorf("missing field")
		}
		f.offset = *req.Offset
	case "mid":
		if req.Text == "" || req.Offset == nil || *req.Offset < 0 {
			return nil, fmt.Errorf("missing field")
		}
		f.text = req.Text
		f.offset = *req.Offset
	case "wrong_tag":
		if req.Tag == "" {
			return nil, fmt.Errorf("missing field")
		}
		f.tag = req.Tag
	case "fetch_order":
		if len(req.Order) == 0 {
			return nil, fmt.Errorf("missing field")
		}
		f.order = append([]string(nil), req.Order...)
	case "reply":
		switch strings.ToUpper(req.Status) {
		case "OK", "NO", "BAD":
			f.status = strings.ToUpper(req.Status)
		default:
			return nil, fmt.Errorf("missing field")
		}
		f.code = req.Code
		f.text = req.Text
	case "bye":
		f.text = req.Text
		if f.text == "" {
			f.text = "Server logging out"
		}
	case "idle_burst":
		if f.command != "IDLE" || req.Text == "" {
			return nil, fmt.Errorf("missing field")
		}
		f.text = req.Text
	case "drop_literal", "rst_literal":
		if req.Bytes == nil || *req.Bytes < 0 {
			return nil, fmt.Errorf("missing field")
		}
		f.bytes = *req.Bytes
	case "bad_literal":
		if req.Size != nil {
			if *req.Size < 0 {
				return nil, fmt.Errorf("missing field")
			}
			f.size = *req.Size
			f.sizeSet = true
		}
	case "inprogress":
		switch f.command {
		case "SEARCH", "ESEARCH", "SORT":
		default:
			return nil, fmt.Errorf("missing field")
		}
		f.count = 2
		if req.Count != nil {
			if *req.Count < 0 {
				return nil, fmt.Errorf("missing field")
			}
			f.count = *req.Count
		}
		if req.DelayMs != nil {
			if *req.DelayMs < 0 {
				return nil, fmt.Errorf("missing field")
			}
			f.delayMs = *req.DelayMs
		}
		done, err := optNumber(req.Done)
		if err != nil {
			return nil, err
		}
		total, err := optNumber(req.Total)
		if err != nil {
			return nil, err
		}
		f.doneText = done
		f.totalText = total
	case "expunge_mid":
		if req.UID == nil {
			return nil, fmt.Errorf("missing field")
		}
		if req.After != nil && *req.After < 0 {
			return nil, fmt.Errorf("missing field")
		}
		f.uid = *req.UID
		f.after = 1
		if req.After != nil {
			f.after = *req.After
			f.afterSet = true
		}
	}
	return f, nil
}

func canonProto(p string) (string, bool) {
	switch strings.ToLower(p) {
	case "imap", "smtp", "jmap":
		return strings.ToLower(p), true
	default:
		return "", false
	}
}
