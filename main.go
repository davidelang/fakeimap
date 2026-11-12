package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Bound is the one JSON object written to stdout before accept.
type Bound struct {
	Listen            string `json:"listen,omitempty"`
	ImplicitTLSListen string `json:"implicit_tls_listen,omitempty"`
	Control           string `json:"control,omitempty"`
	Smtp              string `json:"smtp,omitempty"`
	Jmap              string `json:"jmap,omitempty"`
	JmapTLS           string `json:"jmap_tls,omitempty"`
	Sieve             string `json:"sieve,omitempty"`
}

// Server is the loopback IMAP listener.
type Server struct {
	cfg         Config
	pers        *Personality
	store       *Store
	hasCert     bool
	tlsCfg      *tls.Config
	ln          net.Listener
	tlsLn       net.Listener
	ctrlLn      net.Listener
	smtpLn      net.Listener
	jmapLn      net.Listener
	jmapTLSLn   net.Listener
	sieveLn     net.Listener
	stdout      io.Writer
	logf        *os.File
	logMu       sync.Mutex
	mu          sync.Mutex
	reloadMu    sync.Mutex
	imap        map[int]net.Conn
	pausing     bool
	faults      []*fault
	nextFault   int
	frozen      *time.Time
	ready       chan struct{}
	quit        chan struct{}
	startErr    error
	once        sync.Once
	connSeq     atomic.Int64
	bound       Bound
	smtpMu      sync.Mutex
	smtpLog     []smtpRecord
	smtpN       int
	smtpWaiters []*smtpWaiter
	subMu       sync.Mutex
	subN        uint64
	subs        map[string][]jmapSub
	sieveMu     sync.Mutex
	sieveAccts  map[string]*sieveAcct
}

func newServer(path string, stdout io.Writer) (*Server, error) {
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, err
	}
	return newServerFrom(cfg, stdout)
}

func newServerFrom(cfg Config, stdout io.Writer) (*Server, error) {
	pers, err := loadPersonality(cfg.Personality)
	if err != nil {
		return nil, err
	}
	st, err := loadStore(cfg.Dataset, pers.Delimiter)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		cfg:        cfg,
		pers:       pers,
		store:      st,
		stdout:     stdout,
		ready:      make(chan struct{}),
		quit:       make(chan struct{}),
		sieveAccts: map[string]*sieveAcct{},
	}
	if cfg.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
		srv.hasCert = true
		srv.tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}
	if cfg.ClientCAFile != "" {
		b, err := os.ReadFile(cfg.ClientCAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(b) {
			return nil, fmt.Errorf("client_ca_file: no certificates")
		}
		if srv.tlsCfg != nil {
			srv.tlsCfg.ClientCAs = pool
		}
	}
	if srv.tlsCfg != nil && pers.lists("AUTH=EXTERNAL") {
		if srv.tlsCfg.ClientCAs != nil {
			srv.tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
		} else {
			srv.tlsCfg.ClientAuth = tls.RequestClientCert
		}
	}
	if cfg.LogPath != "" {
		f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		srv.logf = f
	}
	return srv, nil
}

func (s *Server) tlsServerFor(conn net.Conn, id int, command string) *tls.Conn {
	cfg := s.tlsCfg
	if cert := s.handshakeCert(id, command); cert != nil && cfg != nil {
		clone := cfg.Clone()
		clone.Certificates = []tls.Certificate{*cert}
		cfg = clone
	}
	return tls.Server(conn, cfg)
}

func (s *Server) logLine(kind string, conn int, text string) {
	if s.logf == nil {
		return
	}
	rec, err := json.Marshal(struct {
		Kind string `json:"kind"`
		Conn int    `json:"conn"`
		Text string `json:"text"`
	}{kind, conn, trimCRLF(text)})
	if err != nil {
		return
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	_, _ = s.logf.Write(append(rec, '\n'))
}

func trimCRLF(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// Serve binds, writes the bound addresses, then accepts until Close.
func (s *Server) Serve() error {
	if err := s.listen(); err != nil {
		s.startErr = err
		close(s.ready)
		return err
	}
	enc := json.NewEncoder(s.stdout)
	if err := enc.Encode(s.bound); err != nil {
		s.startErr = err
		close(s.ready)
		return err
	}
	if f, ok := s.stdout.(*os.File); ok {
		_ = f.Sync()
	}
	close(s.ready)
	var wg sync.WaitGroup
	if s.ln != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.accept(s.ln, false)
		}()
	}
	if s.tlsLn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.accept(s.tlsLn, true)
		}()
	}
	if s.ctrlLn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.acceptControl()
		}()
	}
	if s.smtpLn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.acceptSMTP()
		}()
	}
	if s.jmapLn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.acceptJMAP(s.jmapLn)
		}()
	}
	if s.jmapTLSLn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.acceptJMAP(tls.NewListener(s.jmapTLSLn, s.tlsCfg))
		}()
	}
	if s.sieveLn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.acceptSieve()
		}()
	}
	wg.Wait()
	return nil
}

func (s *Server) listen() error {
	if s.cfg.Listen != "" {
		ln, err := net.Listen("tcp", s.cfg.Listen)
		if err != nil {
			return err
		}
		s.ln = ln
		s.bound.Listen = ln.Addr().String()
	}
	if s.cfg.ImplicitTLSListen != "" {
		ln, err := net.Listen("tcp", s.cfg.ImplicitTLSListen)
		if err != nil {
			return err
		}
		s.tlsLn = ln
		s.bound.ImplicitTLSListen = ln.Addr().String()
	}
	if s.cfg.Control != "" {
		if err := s.listenControl(); err != nil {
			return err
		}
	}
	if s.cfg.SmtpListen != "" {
		ln, err := net.Listen("tcp", s.cfg.SmtpListen)
		if err != nil {
			return err
		}
		s.smtpLn = ln
		s.bound.Smtp = ln.Addr().String()
	}
	if s.cfg.JmapListen != "" {
		ln, err := net.Listen("tcp", s.cfg.JmapListen)
		if err != nil {
			return err
		}
		s.jmapLn = ln
		s.bound.Jmap = ln.Addr().String()
	}
	if s.cfg.JmapTLSListen != "" {
		ln, err := net.Listen("tcp", s.cfg.JmapTLSListen)
		if err != nil {
			return err
		}
		s.jmapTLSLn = ln
		s.bound.JmapTLS = ln.Addr().String()
	}
	if s.cfg.SieveListen != "" {
		ln, err := net.Listen("tcp", s.cfg.SieveListen)
		if err != nil {
			return err
		}
		s.sieveLn = ln
		s.bound.Sieve = ln.Addr().String()
	}
	return nil
}

func (s *Server) accept(ln net.Listener, implicit bool) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c, implicit)
	}
}

func (s *Server) track(id int, conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pausing {
		return false
	}
	if s.imap == nil {
		s.imap = map[int]net.Conn{}
	}
	s.imap[id] = conn
	return true
}

func (s *Server) untrack(id int) {
	s.mu.Lock()
	delete(s.imap, id)
	s.mu.Unlock()
}

func (s *Server) handle(conn net.Conn, implicit bool) {
	id := int(s.connSeq.Add(1))
	if !s.track(id, conn) {
		_ = conn.Close()
		return
	}
	s.finish(conn, implicit, id)
}

func (s *Server) finish(conn net.Conn, implicit bool, id int) {
	defer conn.Close()
	defer s.untrack(id)
	c := conn
	if implicit {
		tc := s.tlsServerFor(conn, id, "TLS")
		if err := tc.Handshake(); err != nil {
			return
		}
		c = tc
	}
	s.mu.Lock()
	pers := s.pers
	s.mu.Unlock()
	sess := &session{
		srv:      s,
		conn:     c,
		br:       bufio.NewReader(c),
		bw:       bufio.NewWriter(c),
		id:       id,
		secure:   implicit,
		notifyCh: make(chan struct{}, 1),
		enabled:  map[string]struct{}{},
		pers:     pers,
	}
	sess.serve()
}

func (s *Server) Close() {
	s.once.Do(func() {
		close(s.quit)
		if s.ln != nil {
			_ = s.ln.Close()
		}
		if s.tlsLn != nil {
			_ = s.tlsLn.Close()
		}
		if s.ctrlLn != nil {
			_ = s.ctrlLn.Close()
		}
		if s.smtpLn != nil {
			_ = s.smtpLn.Close()
		}
		if s.jmapLn != nil {
			_ = s.jmapLn.Close()
		}
		if s.jmapTLSLn != nil {
			_ = s.jmapTLSLn.Close()
		}
		if s.sieveLn != nil {
			_ = s.sieveLn.Close()
		}
		s.mu.Lock()
		for _, c := range s.imap {
			_ = c.Close()
		}
		s.mu.Unlock()
		if s.bound.Control != "" {
			_ = os.Remove(s.bound.Control)
		}
		if s.logf != nil {
			_ = s.logf.Close()
		}
	})
}

type prefixConn struct {
	net.Conn
	pre []byte
}

func (p *prefixConn) Read(b []byte) (int, error) {
	if len(p.pre) > 0 {
		n := copy(b, p.pre)
		p.pre = p.pre[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}
