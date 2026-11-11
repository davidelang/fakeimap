package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
)

// Bound is the one JSON object written to stdout before accept.
type Bound struct {
	Listen            string `json:"listen,omitempty"`
	ImplicitTLSListen string `json:"implicit_tls_listen,omitempty"`
}

// Server is the loopback IMAP listener.
type Server struct {
	cfg      Config
	pers     *Personality
	store    *Store
	hasCert  bool
	tlsCfg   *tls.Config
	ln       net.Listener
	tlsLn    net.Listener
	stdout   io.Writer
	logf     *os.File
	logMu    sync.Mutex
	mu       sync.Mutex
	conns    []net.Conn
	ready    chan struct{}
	quit     chan struct{}
	startErr error
	once     sync.Once
	connSeq  atomic.Int64
	bound    Bound
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
		cfg:    cfg,
		pers:   pers,
		store:  st,
		stdout: stdout,
		ready:  make(chan struct{}),
		quit:   make(chan struct{}),
	}
	if cfg.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
		srv.hasCert = true
		srv.tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
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

func (s *Server) tlsServer(conn net.Conn) *tls.Conn {
	return tls.Server(conn, s.tlsCfg)
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
	return nil
}

func (s *Server) accept(ln net.Listener, implicit bool) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, c)
		s.mu.Unlock()
		go s.handle(c, implicit)
	}
}

func (s *Server) handle(conn net.Conn, implicit bool) {
	defer conn.Close()
	id := int(s.connSeq.Add(1))
	c := conn
	if implicit {
		tc := tls.Server(conn, s.tlsCfg)
		if err := tc.Handshake(); err != nil {
			return
		}
		c = tc
	}
	sess := &session{
		srv:     s,
		conn:    c,
		br:      bufio.NewReader(c),
		bw:      bufio.NewWriter(c),
		id:      id,
		secure:  implicit,
		enabled: map[string]struct{}{},
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
		s.mu.Lock()
		for _, c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
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

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fakeimap config.json")
		os.Exit(2)
	}
	srv, err := newServer(os.Args[1], os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := srv.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
