package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func startCtrl(t *testing.T, cfg Config) (*Server, Bound, *ctrlClient) {
	t.Helper()
	if cfg.Control == "" {
		cfg.Control = filepath.Join(t.TempDir(), "ctrl.sock")
	}
	srv, bound := start(t, cfg)
	if bound.Control != cfg.Control {
		t.Fatalf("ready control %q", bound.Control)
	}
	return srv, bound, dialCtrl(t, bound.Control)
}

type ctrlClient struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
	w *bufio.Writer
}

func dialCtrl(t *testing.T, path string) *ctrlClient {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &ctrlClient{t: t, c: c, r: bufio.NewReader(c), w: bufio.NewWriter(c)}
}

type ctrlReply struct {
	OK                bool           `json:"ok"`
	Error             string         `json:"error"`
	ID                int            `json:"id"`
	UID               uint32         `json:"uid"`
	Usernames         []string       `json:"usernames"`
	Connections       int            `json:"connections"`
	ClockFrozen       bool           `json:"clock_frozen"`
	Listen            string         `json:"listen"`
	ImplicitTLSListen string         `json:"implicit_tls_listen"`
	Control           string         `json:"control"`
	Personality       string         `json:"personality"`
	Dataset           string         `json:"dataset"`
	Faults            []faultView    `json:"faults"`
	Messages          []mailListItem `json:"messages"`
	RFC822            string         `json:"rfc822"`
}

func (c *ctrlClient) call(req any) ctrlReply {
	c.t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		c.t.Fatal(err)
	}
	b = append(b, '\n')
	_ = c.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.w.Write(b); err != nil {
		c.t.Fatal(err)
	}
	if err := c.w.Flush(); err != nil {
		c.t.Fatal(err)
	}
	_ = c.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("control read: %v", err)
	}
	var rp ctrlReply
	if err := json.Unmarshal(line, &rp); err != nil {
		c.t.Fatalf("control json %s: %v", line, err)
	}
	return rp
}

func (rp ctrlReply) ok(t *testing.T) ctrlReply {
	t.Helper()
	if !rp.OK {
		t.Fatalf("control error: %s", rp.Error)
	}
	return rp
}

func (c *ctrlClient) arm(fields map[string]any) ctrlReply {
	c.t.Helper()
	fields["op"] = "arm-fault"
	rp := c.call(fields)
	rp.ok(c.t)
	if rp.ID < 1 {
		c.t.Fatalf("fault id %d", rp.ID)
	}
	return rp
}

func TestControlSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctrl.sock")
	// net.Listen unlinks on Close. A raw bind leaves the stale socket file.
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		t.Fatal(err)
	}
	_ = syscall.Close(fd)
	if st, err := os.Stat(path); err != nil || st.Mode()&os.ModeSocket == 0 {
		t.Fatalf("stale socket: %v", err)
	}
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", Control: path})
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	if bound.Control != path {
		t.Fatalf("ready %q", bound.Control)
	}
	stt := cc.call(map[string]any{"op": "status"}).ok(t)
	if stt.Listen != bound.Listen || stt.Control != path || stt.Personality == "" || stt.Dataset == "" {
		t.Fatalf("status %+v", stt)
	}
	if stt.Connections != 0 || stt.ClockFrozen || len(stt.Faults) != 0 {
		t.Fatalf("status live %+v", stt)
	}
	if cc.call(map[string]any{"op": "nope"}).OK {
		t.Fatal("unknown op")
	}
	for _, req := range []map[string]any{
		{},
		{"op": "set-personality"},
		{"op": "set-dataset"},
		{"op": "disconnect"},
		{"op": "arm-fault"},
		{"op": "arm-fault", "do": "close"},
		{"op": "inject"},
		{"op": "freeze-clock"},
	} {
		if cc.call(req).OK {
			t.Fatalf("missing field accepted: %#v", req)
		}
	}
	if cc.call(map[string]any{"op": "set-personality", "path": filepath.Join(dir, "missing.json")}).OK {
		t.Fatal("bad personality")
	}

	c, err := net.Dial("tcp", bound.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = readGreeting(t, c)
	if got := cc.call(map[string]any{"op": "status"}).ok(t); got.Connections != 1 {
		t.Fatalf("connections %d", got.Connections)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := readSome(c, time.Second)
		errc <- err
	}()
	cc.call(map[string]any{"op": "disconnect", "conn": 1}).ok(t)
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("disconnect did not fail the read")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect timed out")
	}
	if got := cc.call(map[string]any{"op": "status"}).ok(t); got.Connections != 0 {
		t.Fatalf("connections after disconnect %d", got.Connections)
	}
}

func TestControlAccountsMailAndClock(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	names := cc.call(map[string]any{"op": "accounts"}).ok(t).Usernames
	if strings.Join(names, " ") != "demo" {
		t.Fatalf("usernames %v", names)
	}
	added := cc.call(map[string]any{"op": "accounts", "username": "ada", "password": "lovelace"}).ok(t)
	if strings.Join(added.Usernames, " ") != "ada demo" {
		t.Fatalf("added %v", added.Usernames)
	}
	dup := cc.call(map[string]any{"op": "accounts", "username": "demo", "password": "other"})
	if dup.OK || !strings.Contains(dup.Error, "duplicate") {
		t.Fatalf("dup %+v", dup)
	}
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN ada lovelace").ok(t)
	clBad := dial(t, bound.Listen)
	clBad.cmd("LOGIN demo other").no(t)
	cl2 := dial(t, bound.Listen)
	cl2.cmd("LOGIN demo secret").ok(t)

	listed := cc.call(map[string]any{"op": "list-mail", "mailbox": "INBOX"}).ok(t)
	rawList, _ := json.Marshal(listed.Messages)
	if bytes.Contains(bytes.ToLower(rawList), []byte("rfc822")) || bytes.Contains(bytes.ToLower(rawList), []byte("\"body\"")) {
		t.Fatalf("list-mail has a body: %s", rawList)
	}
	var sawSeen bool
	for _, m := range listed.Messages {
		if m.UID == 0 || m.Size <= 0 || m.Flags == nil {
			t.Fatalf("mail item %+v", m)
		}
		if m.UID == 1 {
			for _, f := range m.Flags {
				if f == `\Seen` {
					sawSeen = true
				}
			}
		}
	}
	if !sawSeen {
		t.Fatalf("uid 1 flags %+v", listed.Messages)
	}
	one := cc.call(map[string]any{"op": "fetch-mail", "mailbox": "INBOX", "uid": 1}).ok(t)
	body, err := base64.StdEncoding.DecodeString(one.RFC822)
	if err != nil || !bytes.Contains(body, []byte("overlay-1@fakeimap.example")) {
		t.Fatalf("fetch-mail %q err %v", one.RFC822, err)
	}
	if cc.call(map[string]any{"op": "fetch-mail", "mailbox": "INBOX"}).OK {
		t.Fatal("fetch-mail without uid")
	}

	cl2.cmd("SELECT INBOX").ok(t)
	cc.call(map[string]any{"op": "freeze-clock", "time": "2020-01-02T03:04:05Z"}).ok(t)
	if !cc.call(map[string]any{"op": "status"}).ok(t).ClockFrozen {
		t.Fatal("clock not frozen")
	}
	cl2.cmd("APPEND INBOX {5+}\r\nhello").ok(t)
	got := fetchLine(t, cl2.cmd("FETCH 3 (INTERNALDATE)").mustOK(t))
	if !strings.Contains(got, "02-Jan-2020 03:04:05 +0000") {
		t.Fatalf("frozen date %s", got)
	}
	cc.call(map[string]any{"op": "thaw-clock"}).ok(t)
	if cc.call(map[string]any{"op": "status"}).ok(t).ClockFrozen {
		t.Fatal("clock still frozen")
	}
	cl2.cmd("APPEND INBOX {5+}\r\nhello").ok(t)
	thawed := fetchLine(t, cl2.cmd("FETCH 4 (INTERNALDATE)").mustOK(t))
	if strings.Contains(thawed, "02-Jan-2020") || !strings.Contains(thawed, strconv.Itoa(time.Now().Year())) {
		t.Fatalf("thawed date %s", thawed)
	}
}

func TestControlInjectIdle(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	before, _, _, _ := parseSelect(t, cl.cmd("SELECT INBOX").mustOK(t))
	cl.send("i1 IDLE\r\n")
	for {
		rec := cl.readRec()
		if strings.HasPrefix(strings.TrimRight(rec, "\r\n"), "+") {
			break
		}
	}
	raw := "From: a@b\r\nTo: c@d\r\nSubject: injected\r\nMessage-ID: <inj@x>\r\n\r\nbody\r\n"
	inj := cc.call(map[string]any{"op": "inject", "mailbox": "INBOX", "raw": raw}).ok(t)
	if inj.UID == 0 {
		t.Fatal("no uid")
	}
	deadline := time.Now().Add(3 * time.Second)
	var saw bool
	for time.Now().Before(deadline) {
		cl.c.SetReadDeadline(time.Now().Add(time.Second))
		rec := cl.readRec()
		if strings.Contains(rec, "EXISTS") {
			fields := strings.Fields(rec)
			n, _ := strconv.Atoi(fields[1])
			if n != before+1 {
				t.Fatalf("exists %d want %d", n, before+1)
			}
			saw = true
			break
		}
	}
	if !saw {
		t.Fatal("IDLE did not see EXISTS")
	}
	got := cc.call(map[string]any{"op": "fetch-mail", "mailbox": "INBOX", "uid": inj.UID}).ok(t)
	body, err := base64.StdEncoding.DecodeString(got.RFC822)
	if err != nil || !bytes.Contains(body, []byte("<inj@x>")) {
		t.Fatalf("injected bytes %q", body)
	}
	var found bool
	for _, m := range cc.call(map[string]any{"op": "list-mail", "mailbox": "INBOX"}).ok(t).Messages {
		if m.UID == inj.UID && m.Size == len(body) {
			found = true
		}
	}
	if !found {
		t.Fatal("list-mail missing inject")
	}
}

func TestControlResetAndDataset(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("APPEND INBOX {5+}\r\nhello").ok(t)
	cl.cmd("LOGOUT").ok(t)
	cc.arm(map[string]any{"do": "silent", "command": "NOOP", "times": 0})
	live, err := net.Dial("tcp", bound.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	_ = readGreeting(t, live)
	errc := make(chan error, 1)
	go func() {
		_, err := readSome(live, 2*time.Second)
		errc <- err
	}()
	cc.call(map[string]any{"op": "reset"}).ok(t)
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("reset did not drop the connection")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reset drop timed out")
	}
	if n := len(cc.call(map[string]any{"op": "status"}).ok(t).Faults); n != 0 {
		t.Fatalf("faults after reset %d", n)
	}
	cl2 := dial(t, bound.Listen)
	cl2.cmd("NOOP").ok(t)
	cl2.cmd("LOGIN demo secret").ok(t)
	exists, _, _, _ := parseSelect(t, cl2.cmd("SELECT INBOX").mustOK(t))
	if exists != 2 {
		t.Fatalf("exists after reset %d", exists)
	}

	ds := filepath.Join(t.TempDir(), "ds")
	if err := os.Mkdir(ds, 0755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"accounts":[{"username":"ada","password":"lovelace"}],"mailboxes":[{"name":"INBOX","uid_validity":7}]}`)
	if err := os.WriteFile(filepath.Join(ds, "dataset.json"), body, 0644); err != nil {
		t.Fatal(err)
	}
	held := dial(t, bound.Listen)
	cc.call(map[string]any{"op": "set-dataset", "path": ds}).ok(t)
	held.c.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := held.r.ReadByte(); err == nil {
		t.Fatal("set-dataset left the connection up")
	}
	if cc.call(map[string]any{"op": "status"}).ok(t).Dataset != ds {
		t.Fatal("dataset path")
	}
	cl3 := dial(t, bound.Listen)
	cl3.cmd("LOGIN demo secret").no(t)
	cl3.cmd("LOGIN ada lovelace").ok(t)
	_, _, uidv, _ := parseSelect(t, cl3.cmd("SELECT INBOX").mustOK(t))
	if uidv != 7 {
		t.Fatalf("uidvalidity %d", uidv)
	}
}

func TestControlPersonality(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	path := writeAltPersonality(t)
	old := dial(t, bound.Listen)
	if !strings.Contains(old.greeting, "Cyrus IMAP") {
		t.Fatalf("greeting %q", old.greeting)
	}
	old.cmd("LOGIN demo secret").ok(t)
	old.cmd("SELECT INBOX.Generated").ok(t)
	before := strings.Join(fetchKeys(t, fetchLine(t, old.cmd("FETCH 1 (RFC822.SIZE UID FLAGS INTERNALDATE)").mustOK(t))), " ")
	if before != "FLAGS UID INTERNALDATE RFC822.SIZE" {
		t.Fatalf("order before %s", before)
	}
	old.send("i1 IDLE\r\n")
	for {
		rec := old.readRec()
		if strings.HasPrefix(strings.TrimRight(rec, "\r\n"), "+") {
			break
		}
	}
	cc.call(map[string]any{"op": "set-personality", "path": path}).ok(t)
	old.send("DONE\r\n")
	old.readTag("i1").ok(t)
	after := strings.Join(fetchKeys(t, fetchLine(t, old.cmd("FETCH 1 (RFC822.SIZE UID FLAGS INTERNALDATE)").mustOK(t))), " ")
	if after != before {
		t.Fatalf("in-flight connection changed order %s", after)
	}
	bye := old.cmd("LOGOUT")
	bye.ok(t)
	if !hasPrefix(bye.untagged, "* BYE") {
		t.Fatal("old connection lost BYE")
	}
	neu := dial(t, bound.Listen)
	if !strings.Contains(neu.greeting, "NEWPERSON") {
		t.Fatalf("new greeting %q", neu.greeting)
	}
	neu.cmd("LOGIN demo secret").ok(t)
	neu.cmd("SELECT INBOX.Generated").ok(t)
	ord := strings.Join(fetchKeys(t, fetchLine(t, neu.cmd("FETCH 1 (RFC822.SIZE UID FLAGS INTERNALDATE)").mustOK(t))), " ")
	if ord != "RFC822.SIZE FLAGS UID INTERNALDATE" {
		t.Fatalf("new order %s", ord)
	}
	nb := neu.cmd("LOGOUT")
	nb.ok(t)
	if hasPrefix(nb.untagged, "* BYE") {
		t.Fatal("new personality sent BYE")
	}
}

func writeAltPersonality(t *testing.T) string {
	t.Helper()
	p, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	p.Greeting = "NEWPERSON"
	p.LogoutBye = false
	order := []string{"RFC822.SIZE"}
	for _, it := range p.FetchItemOrder {
		if strings.EqualFold(it, "RFC822.SIZE") {
			continue
		}
		order = append(order, it)
	}
	p.FetchItemOrder = order
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFaultMatchingAndReplies(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	id1 := cc.arm(map[string]any{"do": "silent", "command": "NOOP", "conn": 1, "times": 1})
	id2 := cc.arm(map[string]any{"do": "silent", "command": "NOOP", "conn": 2, "times": 1})
	st := cc.call(map[string]any{"op": "status"}).ok(t)
	if len(st.Faults) != 2 || st.Faults[0].ID != id1.ID || st.Faults[1].Do != "silent" {
		t.Fatalf("faults %+v", st.Faults)
	}
	cc.call(map[string]any{"op": "clear-faults", "id": id2.ID}).ok(t)
	st = cc.call(map[string]any{"op": "status"}).ok(t)
	if len(st.Faults) != 1 || st.Faults[0].ID != id1.ID {
		t.Fatalf("after clear one %+v", st.Faults)
	}
	if cc.call(map[string]any{"op": "clear-faults", "id": 99}).OK {
		t.Fatal("missing fault id")
	}
	cc.call(map[string]any{"op": "clear-faults"}).ok(t)
	if n := len(cc.call(map[string]any{"op": "status"}).ok(t).Faults); n != 0 {
		t.Fatalf("faults left %d", n)
	}

	cc.arm(map[string]any{"do": "silent", "command": "NOOP", "conn": 1, "times": 1})
	cl1 := dial(t, bound.Listen)
	cl2 := dial(t, bound.Listen)
	cl2.cmd("NOOP").ok(t)
	if err := expectSilence(cl1, "z1 NOOP\r\n"); err != nil {
		t.Fatal(err)
	}
	cl1.cmd("NOOP").ok(t)

	cc.arm(map[string]any{"do": "silent", "command": "NOOP", "times": 0})
	cl := dial(t, bound.Listen)
	if err := expectSilence(cl, "z2 NOOP\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := expectSilence(cl, "z3 NOOP\r\n"); err != nil {
		t.Fatal(err)
	}
	cl.cmd("CAPABILITY").ok(t)
	cc.call(map[string]any{"op": "clear-faults"}).ok(t)

	start := time.Now()
	cc.arm(map[string]any{"do": "delay", "command": "NOOP", "times": 1, "delay_ms": 400})
	cl.cmd("NOOP").ok(t)
	if time.Since(start) < 350*time.Millisecond {
		t.Fatalf("delay too short: %s", time.Since(start))
	}

	cc.arm(map[string]any{"do": "raw", "command": "NOOP", "times": 1, "text": "* RAWLINE"})
	cl.send("r1 NOOP\r\n")
	raw := strings.TrimRight(cl.readRec(), "\r\n")
	if raw != "* RAWLINE" {
		t.Fatalf("raw %q", raw)
	}
	cl.cmd("NOOP").ok(t)

	cc.arm(map[string]any{"do": "flood", "command": "NOOP", "times": 1, "text": "* FLOOD", "count": 3})
	fp := cl.cmd("NOOP")
	fp.ok(t)
	if strings.Join(fp.untagged, "|") != "* FLOOD|* FLOOD|* FLOOD" {
		t.Fatalf("flood %q", fp.untagged)
	}

	cc.arm(map[string]any{"do": "unsolicited", "command": "NOOP", "times": 1, "text": "* PREFACE"})
	up := cl.cmd("NOOP")
	up.ok(t)
	if len(up.untagged) != 1 || up.untagged[0] != "* PREFACE" {
		t.Fatalf("unsolicited %q", up.untagged)
	}

	cc.arm(map[string]any{"do": "wrong_tag", "command": "NOOP", "times": 1, "tag": "zzz"})
	cl.send("w1 NOOP\r\n")
	wt := strings.TrimRight(cl.readRec(), "\r\n")
	if !strings.HasPrefix(wt, "zzz ") || strings.Contains(wt, "w1 ") {
		t.Fatalf("wrong tag %q", wt)
	}

	cc.arm(map[string]any{"do": "mid", "command": "NOOP", "times": 1, "text": "QQ", "offset": 4})
	cl.send("m1 NOOP\r\n")
	mid := cl.readRec()
	if !strings.Contains(mid, "m1 OQQK") || strings.HasPrefix(strings.TrimRight(mid, "\r\n"), "QQ") {
		t.Fatalf("mid %q", mid)
	}

	cc.arm(map[string]any{"do": "tag_first", "command": "CAPABILITY", "times": 1})
	cl.send("c1 CAPABILITY\r\n")
	first := strings.TrimRight(cl.readRec(), "\r\n")
	second := strings.TrimRight(cl.readRec(), "\r\n")
	if !strings.HasPrefix(first, "c1 OK") || !strings.HasPrefix(second, "* CAPABILITY") {
		t.Fatalf("tag_first %q then %q", first, second)
	}

	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	plain := fetchLine(t, cl.cmd("FETCH 2 (FLAGS)").mustOK(t))
	if strings.Contains(plain, `\Flagged`) {
		t.Fatalf("already flagged %s", plain)
	}
	cc.arm(map[string]any{"do": "mutate", "command": "FETCH", "times": 1})
	mp := cl.cmd("FETCH 2 (FLAGS)")
	mp.ok(t)
	ml := fetchLine(t, mp)
	if strings.Count(ml, "FLAGS") != 1 || !strings.Contains(ml, `\Flagged`) {
		t.Fatalf("mutate %q lines %q", ml, mp.untagged)
	}

	ord := strings.Join(fetchKeys(t, fetchLine(t, cl.cmd("FETCH 1 (RFC822.SIZE UID FLAGS INTERNALDATE)").mustOK(t))), " ")
	if ord != "FLAGS UID INTERNALDATE RFC822.SIZE" {
		t.Fatalf("personality order %s", ord)
	}
	cc.arm(map[string]any{
		"do": "fetch_order", "command": "FETCH", "times": 1,
		"order": []string{"RFC822.SIZE", "FLAGS", "UID", "INTERNALDATE"},
	})
	got := strings.Join(fetchKeys(t, fetchLine(t, cl.cmd("FETCH 1 (RFC822.SIZE UID FLAGS INTERNALDATE)").mustOK(t))), " ")
	if got != "RFC822.SIZE FLAGS UID INTERNALDATE" {
		t.Fatalf("fetch_order %s", got)
	}
	again := strings.Join(fetchKeys(t, fetchLine(t, cl.cmd("FETCH 1 (RFC822.SIZE UID FLAGS INTERNALDATE)").mustOK(t))), " ")
	if again != ord {
		t.Fatalf("order after one shot %s", again)
	}
}

func TestFaultBytesAndClose(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})

	cc.arm(map[string]any{"do": "truncate", "command": "NOOP", "times": 1, "bytes": 4})
	c := dialRaw(t, bound.Listen)
	_, _ = c.Write([]byte("a1 NOOP\r\n"))
	got, err := readSome(c, time.Second)
	if err != nil || string(got) != "a1 O" {
		t.Fatalf("truncate %q %v", got, err)
	}
	_, _ = c.Write([]byte("a2 CAPABILITY\r\n"))
	rest := readUntil(t, c, 2*time.Second, []byte("a2 OK"))
	if !bytes.Contains(rest, []byte("a2 OK")) || bytes.Contains(rest, []byte("NOOP")) {
		t.Fatalf("after truncate %q", rest)
	}

	cc.arm(map[string]any{"do": "drip", "command": "NOOP", "times": 1, "delay_ms": 900})
	d := dialRaw(t, bound.Listen)
	if tc, ok := d.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	_, _ = d.Write([]byte("d1 NOOP\r\n"))
	full := []byte("d1 OK NOOP completed\r\n")
	part, err := readSome(d, 500*time.Millisecond)
	if err != nil && len(part) == 0 {
		t.Fatalf("drip read %v", err)
	}
	if len(part) == 0 || len(part) >= len(full) || !bytes.HasPrefix(full, part) {
		t.Fatalf("drip prefix %q", part)
	}
	tail := readUntil(t, d, 2*time.Second, full[len(part):])
	gotAll := append(append([]byte{}, part...), tail...)
	if !bytes.Equal(gotAll, full) {
		t.Fatalf("drip all %q + %q", part, tail)
	}

	cc.arm(map[string]any{"do": "bad_literal", "command": "FETCH", "times": 1})
	b := dialRaw(t, bound.Listen)
	_, _ = b.Write([]byte("b1 LOGIN demo secret\r\nb2 SELECT INBOX\r\nb3 FETCH 1 (BODY.PEEK[TEXT])\r\n"))
	blob := readUntil(t, b, 2*time.Second, []byte("b3 "))
	if !bytes.Contains(blob, []byte("b3 ")) {
		t.Fatalf("bad literal reply %q", blob)
	}
	i := bytes.LastIndex(blob, []byte("{"))
	j := bytes.IndexByte(blob[i:], '}')
	num := blob[i+1 : i+j]
	n, err := strconv.Atoi(string(bytes.TrimSuffix(num, []byte("+"))))
	if err != nil {
		t.Fatal(err)
	}
	restLit := blob[i+j+1:]
	if bytes.HasPrefix(restLit, []byte("\r\n")) {
		restLit = restLit[2:]
	}
	if len(restLit) == n {
		t.Fatalf("literal count %d matches the %d octets that follow", n, len(restLit))
	}
	_, _ = b.Write([]byte("b4 NOOP\r\n"))
	if !bytes.Contains(readUntil(t, b, 2*time.Second, []byte("b4 OK")), []byte("b4 OK")) {
		t.Fatal("connection died after bad literal")
	}

	cc.arm(map[string]any{"do": "close", "command": "NOOP", "times": 1})
	cl := dialRaw(t, bound.Listen)
	_, _ = cl.Write([]byte("x1 NOOP\r\n"))
	buf, err := readSome(cl, time.Second)
	if err == nil || bytes.Contains(buf, []byte(" OK ")) {
		t.Fatalf("close %q %v", buf, err)
	}

	cc.arm(map[string]any{"do": "half_close", "command": "NOOP", "times": 1})
	hc := dialRaw(t, bound.Listen)
	_, _ = hc.Write([]byte("h1 NOOP\r\n"))
	hbuf, herr := readSome(hc, time.Second)
	if !errors.Is(herr, io.EOF) || bytes.Contains(hbuf, []byte("OK")) {
		t.Fatalf("half_close %q %v", hbuf, herr)
	}

	cc.arm(map[string]any{"do": "rst", "command": "NOOP", "times": 1})
	rc := dialRaw(t, bound.Listen)
	_, _ = rc.Write([]byte("s1 NOOP\r\n"))
	rbuf, rerr := readSome(rc, time.Second)
	if rerr == nil || errors.Is(rerr, io.EOF) || bytes.Contains(rbuf, []byte(" OK ")) || !strings.Contains(strings.ToLower(rerr.Error()), "reset") {
		t.Fatalf("rst %q %v", rbuf, rerr)
	}
}

func TestReorderAndSocketWrites(t *testing.T) {
	srv, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.send("a1 NOOP\r\na2 CAPABILITY\r\n")
	r1 := cl.readTag("a1")
	r2 := cl.readTag("a2")
	r1.ok(t)
	r2.ok(t)
	if len(r1.untagged) != 0 {
		t.Fatalf("unfaulted noop waited: %q", r1.untagged)
	}

	cc.arm(map[string]any{"do": "reorder", "command": "NOOP", "times": 1})
	cl.send("b1 NOOP\r\nb2 CAPABILITY\r\n")
	var lines []string
	for i := 0; i < 3; i++ {
		lines = append(lines, strings.TrimRight(cl.readRec(), "\r\n"))
	}
	b1, b2 := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(line, "b1 ") {
			b1 = i
		}
		if strings.HasPrefix(line, "b2 ") {
			b2 = i
		}
	}
	if b2 < 0 || b1 < 0 || b2 > b1 {
		t.Fatalf("reorder lines %q", lines)
	}

	cc.arm(map[string]any{"do": "coalesce", "command": "NOOP", "times": 1})
	base, writes := standInCommand(t, srv, "p1 NOOP\r\np2 CAPABILITY\r\n", 1)
	if len(writes) != base+1 {
		t.Fatalf("coalesce writes %d base %d", len(writes), base)
	}
	blob := writes[base]
	if !bytes.Contains(blob, []byte("p1 OK")) || !bytes.Contains(blob, []byte("p2 OK")) {
		t.Fatalf("coalesce blob %q", blob)
	}

	cc.arm(map[string]any{"do": "split", "command": "NOOP", "times": 1, "offset": 4})
	base, writes = standInCommand(t, srv, "p1 NOOP\r\n", 2)
	if len(writes) != base+2 {
		t.Fatalf("split writes %d base %d", len(writes), base)
	}
	parts := writes[base:]
	if len(parts[0]) != 4 {
		t.Fatalf("split lens %d %d", len(parts[0]), len(parts[1]))
	}
	if string(append(append([]byte{}, parts[0]...), parts[1]...)) != "p1 OK NOOP completed\r\n" {
		t.Fatalf("split bytes %q %q", parts[0], parts[1])
	}
}

func TestCertificateFaults(t *testing.T) {
	certPath, keyPath := writeCert(t)
	beforeC, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeK, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	leaf := parsePEMCert(t, beforeC)
	_, bound, cc := startCtrl(t, Config{
		Listen:            "127.0.0.1:0",
		ImplicitTLSListen: "127.0.0.1:0",
		CertFile:          certPath,
		KeyFile:           keyPath,
	})
	st := cc.call(map[string]any{"op": "status"}).ok(t)
	if st.Listen != bound.Listen || st.ImplicitTLSListen != bound.ImplicitTLSListen {
		t.Fatalf("listeners %+v", st)
	}
	cl := dial(t, bound.Listen)
	if err := startTLSChecked(t, cl, leaf, ""); err != nil {
		t.Fatal(err)
	}
	cc.arm(map[string]any{"do": "wrong_name", "times": 1})
	if err := dialImplicitChecked(t, bound.ImplicitTLSListen, leaf, "name"); err == nil {
		t.Fatal("implicit wrong_name was accepted")
	}
	cc.call(map[string]any{"op": "clear-faults"}).ok(t)
	for _, tc := range []struct{ do, want string }{
		{"wrong_name", "name"},
		{"expired", "date"},
		{"self_signed", "not-configured"},
	} {
		cc.arm(map[string]any{"do": tc.do, "times": 1})
		cl = dial(t, bound.Listen)
		if err := startTLSChecked(t, cl, leaf, tc.want); err == nil {
			t.Fatalf("%s was accepted", tc.do)
		}
		cc.call(map[string]any{"op": "clear-faults"}).ok(t)
	}
	cl = dial(t, bound.Listen)
	if err := startTLSChecked(t, cl, leaf, ""); err != nil {
		t.Fatalf("configured cert rejected after clear: %v", err)
	}
	afterC, _ := os.ReadFile(certPath)
	afterK, _ := os.ReadFile(keyPath)
	if !bytes.Equal(beforeC, afterC) || !bytes.Equal(beforeK, afterK) {
		t.Fatal("configured cert files changed")
	}
}

func expectSilence(cl *imapClient, line string) error {
	cl.send(line)
	_ = cl.c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, err := cl.r.ReadByte()
	_ = cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err == nil {
		return errors.New("silent fault sent a byte")
	}
	return nil
}

func readGreeting(t *testing.T, c net.Conn) string {
	t.Helper()
	buf := readUntil(t, c, 2*time.Second, []byte("\n"))
	if !bytes.HasPrefix(buf, []byte("* OK ")) {
		t.Fatalf("greeting %q", buf)
	}
	return string(buf)
}

func readSome(c net.Conn, d time.Duration) ([]byte, error) {
	_ = c.SetReadDeadline(time.Now().Add(d))
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if n < 0 {
		n = 0
	}
	return buf[:n], err
}

func readUntil(t *testing.T, c net.Conn, d time.Duration, needle []byte) []byte {
	t.Helper()
	deadline := time.Now().Add(d)
	var buf bytes.Buffer
	tmp := make([]byte, 1024)
	for time.Now().Before(deadline) {
		if needle != nil && bytes.Contains(buf.Bytes(), needle) {
			return buf.Bytes()
		}
		_ = c.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		n, err := c.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if needle == nil || bytes.Contains(buf.Bytes(), needle) {
				return buf.Bytes()
			}
			t.Fatalf("read until %q: %v after %q", needle, err, buf.Bytes())
		}
	}
	t.Fatalf("timeout waiting for %q after %q", needle, buf.Bytes())
	return nil
}

func dialRaw(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	_ = readGreeting(t, c)
	return c
}

type countConn struct {
	net.Conn
	mu     sync.Mutex
	writes [][]byte
}

func (c *countConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes = append(c.writes, append([]byte(nil), p...))
	c.mu.Unlock()
	return c.Conn.Write(p)
}

func (c *countConn) snapshot() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.writes))
	copy(out, c.writes)
	return out
}

func awaitWrites(t *testing.T, c *countConn, n int) [][]byte {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := c.snapshot()
		if len(got) >= n {
			time.Sleep(120 * time.Millisecond)
			return c.snapshot()
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("writes %d want >= %d", len(c.snapshot()), n)
	return nil
}

func standInCommand(t *testing.T, srv *Server, line string, extra int) (int, [][]byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	srvConn := <-accepted
	wrapped := &countConn{Conn: srvConn}
	go srv.handle(wrapped, false)
	baseSnap := awaitWrites(t, wrapped, 1)
	base := len(baseSnap)
	if _, err := client.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	return base, awaitWrites(t, wrapped, base+extra)
}

func parsePEMCert(t *testing.T, der []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(der)
	if block == nil {
		t.Fatal("no pem")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func certProblem(configured, got *x509.Certificate, now time.Time) string {
	if now.Before(got.NotBefore) || !now.Before(got.NotAfter) {
		return "date"
	}
	if err := got.VerifyHostname("localhost"); err != nil {
		return "name"
	}
	if !bytes.Equal(got.Raw, configured.Raw) {
		if err := got.CheckSignatureFrom(got); err != nil {
			return "not-self-signed"
		}
		return "not-configured"
	}
	return ""
}

func verifyConfig(configured *x509.Certificate, want string) *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "localhost",
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			got, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}
			gotProblem := certProblem(configured, got, time.Now())
			if gotProblem != want {
				return fmt.Errorf("problem %q want %q", gotProblem, want)
			}
			if want != "" {
				return errors.New(want)
			}
			return nil
		},
	}
}

func startTLSChecked(t *testing.T, cl *imapClient, configured *x509.Certificate, want string) error {
	t.Helper()
	cl.cmd("STARTTLS").ok(t)
	tc := tls.Client(cl.c, verifyConfig(configured, want))
	return tc.Handshake()
}

func dialImplicitChecked(t *testing.T, addr string, configured *x509.Certificate, want string) error {
	t.Helper()
	c, err := tls.Dial("tcp", addr, verifyConfig(configured, want))
	if c != nil {
		c.Close()
	}
	return err
}
