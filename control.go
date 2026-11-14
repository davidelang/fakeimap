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
	To           string          `json:"to"`
	Recipient    string          `json:"recipient"`
	Envid           string          `json:"envid"`
	Flags           []string        `json:"flags"`
	InternalDate    string          `json:"internal_date"`
	Seq             *int            `json:"seq"`
	NewUIDValidity  *uint32         `json:"new_uidvalidity"`
	ClientID        string          `json:"client_id"`
	Scopes          []string        `json:"scopes"`
	AccessSeconds   *int            `json:"access_seconds"`
	RefreshSeconds  *int            `json:"refresh_seconds"`
	TokenID         string          `json:"token_id"`
	Kind            string          `json:"kind"`
	URL             string          `json:"url"`
	Secret          string          `json:"secret"`
	RedirectURIs    []string        `json:"redirect_uris"`
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
		return s.ctrlInject(req)
	case "arm-inject":
		return s.ctrlArm(req)
	case "issue":
		return s.ctrlIssue(req)
	case "list-tokens":
		return okResp(map[string]any{"tokens": s.listTokens()})
	case "revoke":
		if req.TokenID == "" {
			return errResp("missing field")
		}
		if err := s.revokeToken(req.TokenID); err != nil {
			return errResp(err.Error())
		}
		return okResp(nil)
	case "auth-log":
		return okResp(map[string]any{"attempts": s.authLog()})
	case "register-client":
		if req.ClientID == "" || len(req.RedirectURIs) == 0 {
			return errResp("missing field")
		}
		s.tok.addClient(req.ClientID, req.Secret, req.RedirectURIs)
		return okResp(nil)
	case "set-referral":
		return s.ctrlReferral(req)
	case "clear-referral":
		s.mu.Lock()
		s.refs = nil
		s.mu.Unlock()
		return okResp(nil)
	case "list-mail":
		if req.Mailbox == "" {
			return errResp("missing field")
		}
		items, err := s.store.listMail(req.User, req.Mailbox)
		if err != nil {
			return errResp(err.Error())
		}
		return okResp(map[string]any{"messages": items})
	case "fetch-mail":
		if req.Mailbox == "" || req.UID == nil {
			return errResp("missing field")
		}
		b, err := s.store.fetchMail(req.User, req.Mailbox, *req.UID)
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
		if err := s.store.createRaw(req.User, s.pers, name); err != nil {
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
	case "inject-dsn":
		return s.injectDSN(req)
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
		"smtp_listeners":      boundList(s.bound.SmtpListeners),
		"smtp_tls":            s.bound.SmtpTLS,
		"smtp_tls_listeners":  boundList(s.bound.SmtpTLSListeners),
		"jmap":                s.bound.Jmap,
		"jmap_tls":            s.bound.JmapTLS,
		"sieve":               s.bound.Sieve,
		"oauth":               s.bound.OAuth,
		"oauth_tls":           s.bound.OAuthTLS,
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
	s.nextFault++
	f.id = s.nextFault
	s.faults = append(s.faults, f)
	drop := f.do == "drop_eventsource"
	id := f.id
	s.mu.Unlock()
	if drop {
		s.wakeJMAPDrops()
	}
	return id
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
		"inprogress", "drop_after_354", "bounce",
		"unauthorized", "server_fail", "drop_eventsource",
		"expired_token", "wrong_scope", "revoked_refresh", "invalid_grant", "slow_token":
		return strings.ToLower(do), true
	default:
		return "", false
	}
}

func smtpDoAllowed(do, proto string) bool {
	switch do {
	case "expired_token", "wrong_scope", "revoked_refresh", "invalid_grant", "slow_token":
		return true
	case "bounce", "drop_after_354":
		return proto == "smtp"
	case "unauthorized", "server_fail", "drop_eventsource":
		return proto == "jmap"
	}
	if proto == "sieve" {
		return do == "reply"
	}
	if proto != "smtp" {
		return true
	}
	switch do {
	case "close", "rst", "half_close", "silent", "delay", "raw", "reply", "overquota":
		return true
	default:
		return false
	}
}

func boundList(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func (s *Server) ctrlInject(req ctrlRequest) map[string]any {
	if req.Mailbox == "" || req.Raw == "" {
		return errResp("missing field")
	}
	if req.User != "" && !s.store.hasAccount(req.User) {
		return errResp("no such account")
	}
	if req.Seq != nil && req.NewUIDValidity == nil {
		return errResp("missing field")
	}
	when, err := parseInjectDate(req.InternalDate)
	if err != nil {
		return errResp(err.Error())
	}
	uid, err := s.deliverInject(req.User, req.Mailbox, req.Raw, req.Flags, when, req.Seq, req.NewUIDValidity)
	if err != nil {
		return errResp(err.Error())
	}
	return okResp(map[string]any{"uid": uid})
}

func parseInjectDate(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := parseIMAPDateTime(s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("bad time")
		}
	}
	return &t, nil
}

func (s *Server) deliverInject(user, mailbox, raw string, flags []string, date *time.Time, seq *int, uidv *uint32) (uint32, error) {
	uid, mb, renum, err := s.store.injectMsg(injectReq{
		user: user, pers: s.pers, mailbox: mailbox, raw: []byte(raw),
		flags: flags, date: date, seq: seq, uidv: uidv, frozen: s.frozenTime(),
	})
	if err != nil {
		return 0, err
	}
	if renum && mb != nil {
		s.closeUIDV(mb)
	}
	return uid, nil
}

func (s *Server) closeUIDV(mb *Mailbox) {
	s.mu.Lock()
	var hits []*session
	for _, ses := range s.sessions {
		if ses.box == mb {
			hits = append(hits, ses)
		}
	}
	s.mu.Unlock()
	for _, ses := range hits {
		s.store.deselect(ses)
		_ = ses.sendLine("* OK [CLOSED] UIDVALIDITY changed")
	}
}

func (s *Server) ctrlArm(req ctrlRequest) map[string]any {
	if req.Command == "" || req.Mailbox == "" || req.Raw == "" {
		return errResp("missing field")
	}
	if req.Seq != nil && req.NewUIDValidity == nil {
		return errResp("missing field")
	}
	when, err := parseInjectDate(req.InternalDate)
	if err != nil {
		return errResp(err.Error())
	}
	a := &armedInject{
		command: strings.ToUpper(req.Command), mailbox: req.Mailbox, raw: req.Raw,
		user: req.User, flags: append([]string(nil), req.Flags...), date: when,
		seq: req.Seq, uidv: req.NewUIDValidity,
	}
	if req.Conn != nil {
		a.conn = *req.Conn
	}
	if req.Times != nil {
		a.times = *req.Times
	} else {
		a.times = 1
	}
	s.mu.Lock()
	s.arms = append(s.arms, a)
	s.mu.Unlock()
	return okResp(nil)
}

func (s *Server) fireArmed(ses *session, command string) {
	s.mu.Lock()
	var hit *armedInject
	for _, a := range s.arms {
		if a.spent {
			continue
		}
		if a.conn != 0 && a.conn != ses.id {
			continue
		}
		if !strings.EqualFold(a.command, command) {
			continue
		}
		a.hits++
		if a.times > 0 && a.hits < a.times {
			continue
		}
		if a.times > 0 && a.hits > a.times {
			a.spent = true
			continue
		}
		if a.times > 0 {
			a.spent = true
		}
		cp := *a
		hit = &cp
		break
	}
	s.mu.Unlock()
	if hit == nil {
		return
	}
	_, _ = s.deliverInject(hit.user, hit.mailbox, hit.raw, hit.flags, hit.date, hit.seq, hit.uidv)
}

func (s *Server) ctrlIssue(req ctrlRequest) map[string]any {
	user := req.Username
	if user == "" {
		user = req.User
	}
	if user == "" {
		return errResp("missing field")
	}
	var access, refresh time.Duration
	if req.AccessSeconds != nil {
		if *req.AccessSeconds <= 0 {
			return errResp("missing field")
		}
		access = time.Duration(*req.AccessSeconds) * time.Second
	}
	if req.RefreshSeconds != nil {
		if *req.RefreshSeconds <= 0 {
			return errResp("missing field")
		}
		refresh = time.Duration(*req.RefreshSeconds) * time.Second
	}
	body, err := s.issueToken(user, req.ClientID, req.Scopes, access, refresh)
	if err != nil {
		return errResp(err.Error())
	}
	return okResp(body)
}

func (s *Server) ctrlReferral(req ctrlRequest) map[string]any {
	kind := strings.ToLower(req.Kind)
	if (kind != "login" && kind != "mailbox") || req.URL == "" {
		return errResp("missing field")
	}
	if kind == "mailbox" && req.Mailbox == "" {
		return errResp("missing field")
	}
	ref := &mailRef{kind: kind, url: req.URL, mailbox: req.Mailbox}
	if req.Conn != nil {
		ref.conn = *req.Conn
	}
	s.mu.Lock()
	s.refs = append(s.refs, ref)
	s.mu.Unlock()
	return okResp(nil)
}

func (s *Server) loginReferral(conn int) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.refs {
		if r.kind != "login" {
			continue
		}
		if r.conn != 0 && r.conn != conn {
			continue
		}
		return r.url, true
	}
	return "", false
}

func (s *Server) mailboxReferral(conn int, name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.refs {
		if r.kind != "mailbox" {
			continue
		}
		if r.conn != 0 && r.conn != conn {
			continue
		}
		if r.mailbox != "" && !strings.EqualFold(r.mailbox, name) {
			continue
		}
		return r.url, true
	}
	return "", false
}

func (s *Server) injectDSN(req ctrlRequest) map[string]any {
	action := strings.ToLower(req.Code)
	if action != "delivered" && action != "failed" {
		return errResp("missing field")
	}
	to := req.To
	if to == "" {
		last, ok := s.lastSMTP()
		if !ok || last.From == "" {
			return errResp("missing field")
		}
		to = last.From
	}
	if !s.smtpDelivers(to) {
		return errResp("no such account")
	}
	var raw []byte
	if req.Raw != "" {
		raw = []byte(req.Raw)
	} else {
		raw = deliveryReport(action, req.Recipient, req.Envid)
	}
	_, uid, err := s.store.appendMsg(nil, "INBOX", nil, nil, raw, s.frozenTime())
	if err != nil {
		return errResp(err.Error())
	}
	return okResp(map[string]any{"uid": uid})
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
			if !jmapFaultDo(do) {
				return nil, fmt.Errorf("missing field")
			}
		} else {
			f.command = strings.ToUpper(req.Command)
		}
	} else if req.Command != "" {
		f.command = strings.ToUpper(req.Command)
	}
	if !smtpDoAllowed(do, faultProto(f)) {
		return nil, fmt.Errorf("unknown fault")
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
		switch faultProto(f) {
		case "smtp":
			n, err := strconv.Atoi(req.Code)
			if err != nil || n < 200 || n > 599 {
				return nil, fmt.Errorf("missing field")
			}
			f.code = strconv.Itoa(n)
			f.text = req.Text
		case "sieve":
			switch f.command {
			case "CHECKSCRIPT", "PUTSCRIPT":
			default:
				return nil, fmt.Errorf("unknown fault")
			}
		default:
			switch strings.ToUpper(req.Status) {
			case "OK", "NO", "BAD":
				f.status = strings.ToUpper(req.Status)
			default:
				return nil, fmt.Errorf("missing field")
			}
			f.code = req.Code
			f.text = req.Text
		}
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
	case "drop_after_354":
		if f.command != "DATA" {
			return nil, fmt.Errorf("missing field")
		}
		if req.Bytes != nil {
			if *req.Bytes < 0 {
				return nil, fmt.Errorf("missing field")
			}
			f.bytes = *req.Bytes
		}
	case "bounce":
		if f.command != "DATA" {
			return nil, fmt.Errorf("missing field")
		}
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
	case "imap", "smtp", "jmap", "sieve":
		return strings.ToLower(p), true
	default:
		return "", false
	}
}
