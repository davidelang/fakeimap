package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
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
	Op        string   `json:"op"`
	Path      string   `json:"path"`
	Conn      *int     `json:"conn"`
	Mailbox   string   `json:"mailbox"`
	User      string   `json:"user"`
	Username  string   `json:"username"`
	Password  string   `json:"password"`
	Raw       string   `json:"raw"`
	UID       *uint32  `json:"uid"`
	Time      string   `json:"time"`
	ID        *int     `json:"id"`
	Command   string   `json:"command"`
	Times     *int     `json:"times"`
	Do        string   `json:"do"`
	DelayMs   *int     `json:"delay_ms"`
	Bytes     *int     `json:"bytes"`
	Text      string   `json:"text"`
	Count     *int     `json:"count"`
	Offset    *int     `json:"offset"`
	Tag       string   `json:"tag"`
	Order     []string `json:"order"`
	TimeoutMs *int     `json:"timeout_ms"`
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
	case "arm-fault":
		f, err := buildFault(req)
		if err != nil {
			return errResp(err.Error())
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
		"sieve":               s.bound.Sieve,
		"personality":         s.cfg.Personality,
		"dataset":             s.cfg.Dataset,
		"connections":         len(s.imap),
		"clock_frozen":        s.frozen != nil,
		"faults":              views,
	}
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

func (s *Server) armFault(f *fault) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextFault++
	f.id = s.nextFault
	s.faults = append(s.faults, f)
	return f.id
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
		"expired", "self_signed":
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
	}
	return f, nil
}
