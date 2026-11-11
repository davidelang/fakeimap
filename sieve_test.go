package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSieveListenAndStatus(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	if bound.Sieve != "" {
		t.Fatalf("omitted sieve_listen bound %q", bound.Sieve)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", SieveListen: ""})
	if bound.Sieve != "" {
		t.Fatalf("empty sieve_listen bound %q", bound.Sieve)
	}
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	raw := ctrlRaw(t, bound.Control, map[string]any{"op": "status"})
	if raw["sieve"] != "" {
		t.Fatalf("unbound status sieve %#v", raw["sieve"])
	}
	if cc.call(map[string]any{"op": "status"}).ok(t).Sieve != "" {
		t.Fatal("status sieve not empty")
	}

	_, bound, cc = startCtrl(t, Config{Listen: "127.0.0.1:0", SieveListen: "127.0.0.1:0"})
	host, port, err := net.SplitHostPort(bound.Sieve)
	if err != nil || host != "127.0.0.1" || port == "" || port == "0" {
		t.Fatalf("sieve bound %q", bound.Sieve)
	}
	raw = ctrlRaw(t, bound.Control, map[string]any{"op": "status"})
	if raw["sieve"] != bound.Sieve {
		t.Fatalf("status sieve %#v want %s", raw["sieve"], bound.Sieve)
	}
	if cc.call(map[string]any{"op": "status"}).ok(t).Sieve != bound.Sieve {
		t.Fatal("status sieve mismatch")
	}
}

func TestSieveGreetingAuthAndScripts(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", SieveListen: "127.0.0.1:0"})
	cl := dialSieve(t, bound.Sieve)
	greet := cl.greeting()
	if strings.Contains(greet, `"STARTTLS"`) {
		t.Fatalf("starttls without a certificate:\n%s", greet)
	}
	requireSieveCaps(t, greet, false)
	if !strings.HasPrefix(cl.transact("STARTTLS"), "NO") {
		t.Fatal("starttls without a certificate")
	}

	for _, cmd := range []string{
		"LISTSCRIPTS",
		`GETSCRIPT "x"`,
		`SETACTIVE "x"`,
		`SETACTIVE ""`,
	} {
		if !strings.HasPrefix(cl.transact(cmd), "NO") {
			t.Fatalf("%s before auth", cmd)
		}
	}
	if got := cl.scriptCmd("CHECKSCRIPT", []byte("keep;\n")); !strings.HasPrefix(got, "NO") {
		t.Fatalf("check before auth %q", got)
	}
	if got := cl.scriptCmd(`PUTSCRIPT "x"`, []byte("keep;\n")); !strings.HasPrefix(got, "NO") {
		t.Fatalf("put before auth %q", got)
	}

	plain := base64.StdEncoding.EncodeToString([]byte("\x00demo\x00wrong"))
	if !strings.HasPrefix(cl.transact(`AUTHENTICATE "PLAIN" "`+plain+`"`), "NO") {
		t.Fatal("plain wrong password")
	}
	cl.cram("demo", "wrong", false)
	plain = base64.StdEncoding.EncodeToString([]byte("\x00demo\x00secret"))
	if cl.transact(`AUTHENTICATE "PLAIN" "`+plain+`"`) != "OK" {
		t.Fatal("plain auth")
	}

	body := []byte("not sieve {{\n\x00")
	if cl.scriptCmd("CHECKSCRIPT", body) != "OK" {
		t.Fatal("checkscript")
	}
	if lines := cl.list(); len(lines) != 1 || lines[0] != "OK" {
		t.Fatalf("check stored %q", lines)
	}
	if cl.scriptCmd("CHECKSCRIPT", nil) == "OK" {
		t.Fatal("empty check stored or accepted")
	}
	if lines := cl.list(); len(lines) != 1 || lines[0] != "OK" {
		t.Fatalf("empty check stored %q", lines)
	}

	if cl.scriptCmd(`PUTSCRIPT "vacation"`, body) != "OK" {
		t.Fatal("put")
	}
	if cl.scriptCmd(`PUTSCRIPT "other"`, []byte("keep;\n")) != "OK" {
		t.Fatal("put other")
	}
	okLine, got := cl.get("vacation")
	if okLine != "OK" || !bytes.Equal(got, body) {
		t.Fatalf("get %q %q", okLine, got)
	}
	if no := cl.transact(`GETSCRIPT "missing"`); !strings.HasPrefix(no, "NO") {
		t.Fatalf("missing get %q", no)
	}
	lines := cl.list()
	if strings.Join(lines, "\n") != "\"vacation\"\n\"other\"\nOK" {
		t.Fatalf("list %q", lines)
	}
	if cl.transact(`SETACTIVE "vacation"`) != "OK" {
		t.Fatal("setactive")
	}
	lines = cl.list()
	if strings.Join(lines, "\n") != "\"vacation\" ACTIVE\n\"other\"\nOK" {
		t.Fatalf("active list %q", lines)
	}
	if !strings.HasPrefix(cl.transact(`SETACTIVE "missing"`), "NO") {
		t.Fatal("setactive unknown")
	}
	lines = cl.list()
	if strings.Join(lines, "\n") != "\"vacation\" ACTIVE\n\"other\"\nOK" {
		t.Fatalf("active changed %q", lines)
	}
	if cl.transact(`SETACTIVE ""`) != "OK" {
		t.Fatal("clear active")
	}
	lines = cl.list()
	if strings.Join(lines, "\n") != "\"vacation\"\n\"other\"\nOK" {
		t.Fatalf("cleared %q", lines)
	}
	if !strings.HasPrefix(cl.transact(`SETACTIVE "missing"`), "NO") {
		t.Fatal("setactive unknown after clear")
	}
	lines = cl.list()
	if strings.Join(lines, "\n") != "\"vacation\"\n\"other\"\nOK" {
		t.Fatalf("clear changed %q", lines)
	}

	cc.call(map[string]any{"op": "accounts", "username": "ada", "password": "pw"}).ok(t)
	ada := dialSieve(t, bound.Sieve)
	_ = ada.greeting()
	adaPlain := base64.StdEncoding.EncodeToString([]byte("\x00ada\x00pw"))
	if ada.transact(`AUTHENTICATE "PLAIN" "`+adaPlain+`"`) != "OK" {
		t.Fatal("ada auth")
	}
	if lines := ada.list(); len(lines) != 1 || lines[0] != "OK" {
		t.Fatalf("ada saw demo scripts %q", lines)
	}

	cc.call(map[string]any{"op": "reset"}).ok(t)
	if lines := cl.list(); len(lines) != 1 || lines[0] != "OK" {
		t.Fatalf("reset kept scripts %q", lines)
	}
	if cl.scriptCmd(`PUTSCRIPT "again"`, []byte("keep;\n")) != "OK" {
		t.Fatal("put after reset")
	}
	cc.call(map[string]any{"op": "set-dataset", "path": "datasets/demo"}).ok(t)
	if lines := cl.list(); len(lines) != 1 || lines[0] != "OK" {
		t.Fatalf("set-dataset kept scripts %q", lines)
	}
	if cl.transact("LOGOUT") != "OK" {
		t.Fatal("logout")
	}
}

func TestSieveCRAMAndStartTLS(t *testing.T) {
	cert, key := writeCert(t)
	_, bound := start(t, Config{
		Listen:      "127.0.0.1:0",
		SieveListen: "127.0.0.1:0",
		CertFile:    cert,
		KeyFile:     key,
	})
	cl := dialSieve(t, bound.Sieve)
	greet := cl.greeting()
	requireSieveCaps(t, greet, true)
	if cl.transact("STARTTLS") != "OK" {
		t.Fatal("starttls")
	}
	tlsConn := tls.Client(cl.c, &tls.Config{InsecureSkipVerify: true, ServerName: "localhost"})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	cl.c = tlsConn
	cl.r = bufio.NewReader(tlsConn)
	cl.w = bufio.NewWriter(tlsConn)
	again := cl.greeting()
	requireSieveCaps(t, again, false)
	if strings.Contains(again, `"STARTTLS"`) {
		t.Fatalf("starttls still advertised:\n%s", again)
	}
	plain := base64.StdEncoding.EncodeToString([]byte("\x00demo\x00secret"))
	if cl.transact(`AUTHENTICATE "PLAIN" "`+plain+`"`) != "OK" {
		t.Fatal("plain after starttls")
	}
	if cl.transact("LOGOUT") != "OK" {
		t.Fatal("logout")
	}

	cl = dialSieve(t, bound.Sieve)
	_ = cl.greeting()
	cl.cram("demo", "secret", true)
	if cl.scriptCmd(`PUTSCRIPT "liveimap"`, []byte("keep;\n")) != "OK" {
		t.Fatal("put after cram")
	}
	okLine, got := cl.get("liveimap")
	if okLine != "OK" || !bytes.Equal(got, []byte("keep;\n")) {
		t.Fatalf("get after cram %q %q", okLine, got)
	}
}

func requireSieveCaps(t *testing.T, greet string, starttls bool) {
	t.Helper()
	for _, want := range []string{
		`"IMPLEMENTATION"`,
		`"VERSION" "1.0"`,
		`"SASL" "PLAIN CRAM-MD5"`,
		`"SIEVE" "fileinto include"`,
	} {
		if !strings.Contains(greet, want) {
			t.Fatalf("greeting missing %s\n%s", want, greet)
		}
	}
	if starttls && !strings.Contains(greet, `"STARTTLS"`) {
		t.Fatalf("greeting missing STARTTLS\n%s", greet)
	}
	if !strings.HasSuffix(strings.TrimSpace(greet), "OK") {
		t.Fatalf("greeting did not end OK\n%s", greet)
	}
}

func ctrlRaw(t *testing.T, path string, req any) map[string]any {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("status %s: %v", line, err)
	}
	return m
}

type sieveClient struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
	w *bufio.Writer
}

func dialSieve(t *testing.T, addr string) *sieveClient {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &sieveClient{t: t, c: c, r: bufio.NewReader(c), w: bufio.NewWriter(c)}
}

func (cl *sieveClient) readLine() string {
	cl.t.Helper()
	_ = cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := cl.r.ReadString('\n')
	if err != nil {
		cl.t.Fatalf("read: %v", err)
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
}

func (cl *sieveClient) writeLine(line string) {
	cl.t.Helper()
	_ = cl.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := cl.w.WriteString(line + "\r\n"); err != nil {
		cl.t.Fatal(err)
	}
	if err := cl.w.Flush(); err != nil {
		cl.t.Fatal(err)
	}
}

func (cl *sieveClient) greeting() string {
	cl.t.Helper()
	var lines []string
	for {
		line := cl.readLine()
		lines = append(lines, line)
		if line == "OK" || strings.HasPrefix(line, "OK ") {
			return strings.Join(lines, "\n")
		}
		if strings.HasPrefix(line, "NO") || strings.HasPrefix(line, "BYE") || len(lines) > 20 {
			cl.t.Fatalf("greeting %q", lines)
		}
	}
}

func (cl *sieveClient) transact(line string) string {
	cl.t.Helper()
	cl.writeLine(line)
	return cl.readLine()
}

func (cl *sieveClient) scriptCmd(command string, body []byte) string {
	cl.t.Helper()
	cl.writeLine(fmt.Sprintf("%s {%d+}", command, len(body)))
	_ = cl.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := cl.w.Write(body); err != nil {
		cl.t.Fatal(err)
	}
	if _, err := cl.w.WriteString("\r\n"); err != nil {
		cl.t.Fatal(err)
	}
	if err := cl.w.Flush(); err != nil {
		cl.t.Fatal(err)
	}
	return cl.readLine()
}

func (cl *sieveClient) list() []string {
	cl.t.Helper()
	cl.writeLine("LISTSCRIPTS")
	var lines []string
	for {
		line := cl.readLine()
		lines = append(lines, line)
		if line == "OK" || strings.HasPrefix(line, "OK ") || strings.HasPrefix(line, "NO") {
			return lines
		}
		if len(lines) > 50 {
			cl.t.Fatalf("list %q", lines)
		}
	}
}

func (cl *sieveClient) get(name string) (string, []byte) {
	cl.t.Helper()
	cl.writeLine("GETSCRIPT " + sieveQuote(name))
	header := cl.readLine()
	if strings.HasPrefix(header, "NO") || strings.HasPrefix(header, "OK") {
		return header, nil
	}
	n, plus, ok := parseSieveLit(header)
	if !ok || plus {
		cl.t.Fatalf("get header %q", header)
	}
	buf := make([]byte, n)
	_ = cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(cl.r, buf); err != nil {
		cl.t.Fatal(err)
	}
	if tail := cl.readLine(); tail != "" {
		cl.t.Fatalf("literal tail %q", tail)
	}
	return cl.readLine(), buf
}

func (cl *sieveClient) cram(user, pass string, ok bool) {
	cl.t.Helper()
	cl.writeLine(`AUTHENTICATE "CRAM-MD5"`)
	line := cl.readLine()
	if strings.HasPrefix(line, "NO") || strings.HasPrefix(line, "OK") {
		cl.t.Fatalf("cram challenge %q", line)
	}
	args, err := splitSieve(line)
	if err != nil || len(args) != 1 || args[0].lit {
		cl.t.Fatalf("cram challenge %q", line)
	}
	raw, err := base64.StdEncoding.DecodeString(args[0].text)
	if err != nil {
		cl.t.Fatal(err)
	}
	msg := base64.StdEncoding.EncodeToString([]byte(user + " " + cramHex(pass, string(raw))))
	got := cl.transact(sieveQuote(msg))
	if ok && got != "OK" {
		cl.t.Fatalf("cram %q", got)
	}
	if !ok && !strings.HasPrefix(got, "NO") {
		cl.t.Fatalf("cram reject %q", got)
	}
}
