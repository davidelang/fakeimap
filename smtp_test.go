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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPListenReady(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	if bound.Smtp != "" {
		t.Fatalf("omitted smtp_listen bound %q", bound.Smtp)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", SmtpListen: smtpListenList{}})
	if bound.Smtp != "" {
		t.Fatalf("empty smtp_listen bound %q", bound.Smtp)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", SmtpListen: smtpListenList{{Addr: "127.0.0.1:0"}}})
	host, port, err := net.SplitHostPort(bound.Smtp)
	if err != nil || host != "127.0.0.1" || port == "" || port == "0" {
		t.Fatalf("smtp bound %q", bound.Smtp)
	}
	cl := dialSMTP(t, bound.Smtp)
	_, lines := cl.cmd("EHLO client.example")
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "AUTH PLAIN LOGIN") || !strings.Contains(text, "8BITMIME") {
		t.Fatalf("ehlo %q", text)
	}
	if strings.Contains(strings.ToUpper(text), "STARTTLS") {
		t.Fatalf("starttls advertised %q", text)
	}
	if code, _ := cl.cmd("STARTTLS"); code != 454 {
		t.Fatalf("starttls without a certificate %d", code)
	}
}

func TestSMTPStartTLS(t *testing.T) {
	cert, key := writeCert(t)
	_, bound := start(t, Config{
		Listen:     "127.0.0.1:0",
		SmtpListen: smtpListenList{{Addr: "127.0.0.1:0"}},
		CertFile:   cert,
		KeyFile:    key,
	})
	cl := dialSMTP(t, bound.Smtp)
	_, lines := cl.cmd("EHLO client.example")
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "AUTH PLAIN LOGIN") || !strings.Contains(text, "8BITMIME") || !strings.Contains(text, "STARTTLS") {
		t.Fatalf("ehlo %q", text)
	}
	if code, _ := cl.cmd("MAIL FROM:<a@b>"); code != 530 {
		t.Fatalf("mail before auth %d", code)
	}
	if code, _ := cl.cmd("STARTTLS"); code != 220 {
		t.Fatalf("starttls %d", code)
	}
	tlsConn := tls.Client(cl.c, &tls.Config{InsecureSkipVerify: true, ServerName: "localhost"})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}
	cl.c = tlsConn
	cl.r = bufio.NewReader(tlsConn)
	cl.w = bufio.NewWriter(tlsConn)
	_, lines = cl.cmd("EHLO client.example")
	text = strings.Join(lines, "\n")
	if !strings.Contains(text, "AUTH PLAIN LOGIN") || !strings.Contains(text, "8BITMIME") || strings.Contains(strings.ToUpper(text), "STARTTLS") {
		t.Fatalf("ehlo after starttls %q", text)
	}
	if code, _ := cl.cmd("STARTTLS"); code != 454 {
		t.Fatalf("second starttls %d", code)
	}
	if code, _ := cl.cmd("MAIL FROM:<a@b>"); code != 530 {
		t.Fatalf("mail before auth after tls %d", code)
	}
	cl.authPlain("demo", "secret")
	if code := cl.mailData("sender@example.com", []string{"nobody@example.com"}, "Subject: x\r\n\r\nn\r\n"); code != 250 {
		t.Fatalf("mail after tls %d", code)
	}
}

func TestSMTPAuthDeliverAndIdle(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", SmtpListen: smtpListenList{{Addr: "127.0.0.1:0"}}})
	im := dial(t, bound.Listen)
	im.cmd("LOGIN demo secret").ok(t)
	before := inboxCount(t, cc)
	names := strings.Join(im.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n")
	im.cmd("SELECT INBOX").ok(t)
	im.send("i1 IDLE\r\n")
	for {
		rec := im.readRec()
		if strings.HasPrefix(strings.TrimRight(rec, "\r\n"), "+") {
			break
		}
	}

	cl := dialSMTP(t, bound.Smtp)
	if code, _ := cl.cmd("MAIL FROM:<a@b>"); code != 530 {
		t.Fatalf("mail before auth %d", code)
	}
	if code, _ := cl.cmd("RCPT TO:<demo>"); code != 530 {
		t.Fatalf("rcpt before auth %d", code)
	}
	if code, _ := cl.cmd("AUTH PLAIN " + plainSMTP("demo", "wrong")); code != 535 {
		t.Fatalf("plain reject %d", code)
	}
	cl.send("AUTH LOGIN\r\n")
	if code, _ := cl.readReply(); code != 334 {
		t.Fatalf("login challenge %d", code)
	}
	cl.send(base64.StdEncoding.EncodeToString([]byte("demo")) + "\r\n")
	if code, _ := cl.readReply(); code != 334 {
		t.Fatalf("login password challenge %d", code)
	}
	cl.send(base64.StdEncoding.EncodeToString([]byte("wrong")) + "\r\n")
	if code, _ := cl.readReply(); code != 535 {
		t.Fatalf("login reject %d", code)
	}
	cl.authPlain("demo", "secret")

	raw := []byte("Subject: hi\r\n\r\n.hidden\r\n")
	wire := "Subject: hi\r\n\r\n..hidden\r\n"
	if code := cl.mailData("sender@example.com", []string{"demo"}, wire); code != 250 {
		t.Fatalf("bare deliver %d", code)
	}
	if code, _ := cl.cmd("DATA"); code != 503 {
		t.Fatalf("data without rcpt %d", code)
	}
	raw2 := []byte("Subject: two\r\n\r\nbody\r\n")
	if code := cl.mailData("sender@example.com", []string{"demo@example.com"}, string(raw2)); code != 250 {
		t.Fatalf("domain deliver %d", code)
	}

	deadline := time.Now().Add(5 * time.Second)
	var saw int
	for saw < 2 && time.Now().Before(deadline) {
		im.c.SetReadDeadline(time.Now().Add(time.Second))
		rec := im.readRec()
		if strings.Contains(rec, "EXISTS") {
			saw++
		}
	}
	if saw != 2 {
		t.Fatalf("idle exists %d", saw)
	}
	im.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	im.send("DONE\r\n")
	im.readTag("i1").ok(t)
	if got := strings.Join(im.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n"); got != names {
		t.Fatalf("mailboxes changed\n%s\n%s", names, got)
	}
	if inboxCount(t, cc) != before+2 {
		t.Fatalf("inbox %d want %d", inboxCount(t, cc), before+2)
	}
	msgs := cc.call(map[string]any{"op": "list-mail", "mailbox": "INBOX"}).ok(t).Messages
	got1 := fetchRFC822(t, cc, msgs[len(msgs)-2].UID)
	got2 := fetchRFC822(t, cc, msgs[len(msgs)-1].UID)
	if !bytes.Equal(got1, raw) || !bytes.Equal(got2, raw2) {
		t.Fatalf("stored %q %q", got1, got2)
	}
	list := listSMTP(t, cc)
	if len(list.Messages) != 2 || list.Messages[0].From != "sender@example.com" {
		t.Fatalf("log %+v", list.Messages)
	}
	if len(list.Messages[0].Recipients) != 1 || list.Messages[0].Recipients[0].Address != "demo" || list.Messages[0].Recipients[0].Result != "delivered" {
		t.Fatalf("rcpt0 %+v", list.Messages[0].Recipients)
	}
	if list.Messages[1].Recipients[0].Address != "demo@example.com" || list.Messages[1].Recipients[0].Result != "delivered" {
		t.Fatalf("rcpt1 %+v", list.Messages[1].Recipients)
	}
}

func TestSMTPDiscardWaitAndLimits(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", SmtpListen: smtpListenList{{Addr: "127.0.0.1:0"}}})
	before := inboxCount(t, cc)
	im := dial(t, bound.Listen)
	im.cmd("LOGIN demo secret").ok(t)
	names := strings.Join(im.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n")
	cl := dialSMTP(t, bound.Smtp)
	cl.authLogin("demo", "secret")
	if code := cl.mailData("", []string{"nobody@example.com"}, "Subject: x\r\n\r\nn\r\n"); code != 250 {
		t.Fatalf("discard %d", code)
	}
	if code := cl.mailData("a@b", []string{"demo", "stranger"}, "Subject: y\r\n\r\nmixed\r\n"); code != 250 {
		t.Fatalf("mixed %d", code)
	}
	if inboxCount(t, cc) != before+1 {
		t.Fatalf("inbox %d want %d", inboxCount(t, cc), before+1)
	}
	if got := strings.Join(im.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n"); got != names {
		t.Fatalf("mailboxes changed\n%s\n%s", names, got)
	}
	list := listSMTP(t, cc)
	if len(list.Messages) != 2 {
		t.Fatalf("log len %d", len(list.Messages))
	}
	if list.Messages[0].From != "" || list.Messages[0].Recipients[0].Result != "discarded" {
		t.Fatalf("discarded %+v", list.Messages[0])
	}
	if list.Messages[1].Recipients[0].Result != "delivered" || list.Messages[1].Recipients[1].Address != "stranger" || list.Messages[1].Recipients[1].Result != "discarded" {
		t.Fatalf("mixed log %+v", list.Messages[1].Recipients)
	}
	if !cc.call(map[string]any{"op": "wait-smtp", "count": 0}).OK {
		t.Fatal("count zero")
	}
	if cc.call(map[string]any{"op": "wait-smtp"}).OK {
		t.Fatal("missing count")
	}
	started := time.Now()
	late := cc.call(map[string]any{"op": "wait-smtp", "count": 1, "timeout_ms": 150})
	if late.OK || late.Error == "" || time.Since(started) < 100*time.Millisecond {
		t.Fatalf("timeout %+v after %s", late, time.Since(started))
	}

	cc2 := dialCtrl(t, bound.Control)
	done := make(chan ctrlReply, 1)
	go func() {
		done <- cc2.call(map[string]any{"op": "wait-smtp", "count": 1, "timeout_ms": 3000})
	}()
	time.Sleep(50 * time.Millisecond)
	if code := cl.mailData("a@b", []string{"other"}, "Subject: z\r\n\r\nwait\r\n"); code != 250 {
		t.Fatalf("wait deliver %d", code)
	}
	select {
	case rp := <-done:
		if !rp.OK {
			t.Fatalf("wait %+v", rp)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("wait-smtp did not return")
	}
	if inboxCount(t, cc) != before+1 {
		t.Fatal("discarded wait message was stored")
	}

	if code, _ := cl.cmd("RSET"); code != 250 {
		t.Fatal("rset")
	}
	if code, _ := cl.cmd("MAIL FROM:<a@b>"); code != 250 {
		t.Fatal("mail")
	}
	if code, _ := cl.cmd("RCPT TO:<demo>"); code != 250 {
		t.Fatal("rcpt")
	}
	if code, _ := cl.cmd("RSET"); code != 250 {
		t.Fatal("rset2")
	}
	if code, _ := cl.cmd("DATA"); code != 503 {
		t.Fatalf("data after rset %d", code)
	}

	if code, _ := cl.cmd("MAIL FROM:<a@b>"); code != 250 {
		t.Fatal("mail2")
	}
	if code, _ := cl.cmd("RCPT TO:<demo>"); code != 250 {
		t.Fatal("rcpt2")
	}
	cl.send("DATA\r\n")
	if code, _ := cl.readReply(); code != 354 {
		t.Fatalf("data prompt")
	}
	cl.send(strings.Repeat("A", smtpDataMax) + "\r\n.\r\n")
	if code, _ := cl.readReply(); code != 500 {
		t.Fatalf("long data line")
	}
	if inboxCount(t, cc) != before+1 {
		t.Fatal("long line delivered")
	}
	if code, _ := cl.cmd("DATA"); code != 503 {
		t.Fatalf("data after long line %d", code)
	}

	drop := dialSMTP(t, bound.Smtp)
	drop.authPlain("demo", "secret")
	if code, _ := drop.cmd("MAIL FROM:<a@b>"); code != 250 {
		t.Fatal("drop mail")
	}
	if code, _ := drop.cmd("RCPT TO:<demo>"); code != 250 {
		t.Fatal("drop rcpt")
	}
	drop.send("DATA\r\n")
	if code, _ := drop.readReply(); code != 354 {
		t.Fatal("drop data")
	}
	drop.send("partial\r\n")
	_ = drop.c.Close()
	time.Sleep(100 * time.Millisecond)
	if inboxCount(t, cc) != before+1 {
		t.Fatal("dropped data delivered")
	}
	if n := len(listSMTP(t, cc).Messages); n != 3 {
		t.Fatalf("log %d want 3", n)
	}
}

type smtpClient struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
	w *bufio.Writer
}

func dialSMTP(t *testing.T, addr string) *smtpClient {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cl := &smtpClient{t: t, c: c, r: bufio.NewReader(c), w: bufio.NewWriter(c)}
	code, _ := cl.readReply()
	if code != 220 {
		t.Fatalf("greeting %d", code)
	}
	return cl
}

func (cl *smtpClient) send(s string) {
	cl.t.Helper()
	cl.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(cl.w, s); err != nil {
		cl.t.Fatal(err)
	}
	if err := cl.w.Flush(); err != nil {
		cl.t.Fatal(err)
	}
}

func (cl *smtpClient) readReply() (int, []string) {
	cl.t.Helper()
	var lines []string
	code := 0
	for {
		cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
		line, err := cl.r.ReadString('\n')
		if err != nil {
			cl.t.Fatal(err)
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 3 {
			cl.t.Fatalf("short reply %q", line)
		}
		n, err := strconv.Atoi(line[:3])
		if err != nil {
			cl.t.Fatal(err)
		}
		if code == 0 {
			code = n
		} else if n != code {
			cl.t.Fatalf("reply code changed %d -> %d (%q)", code, n, line)
		}
		if len(line) > 3 && line[3] != ' ' && line[3] != '-' {
			cl.t.Fatalf("reply sep %q", line)
		}
		text := ""
		if len(line) > 4 {
			text = line[4:]
		}
		lines = append(lines, text)
		if len(line) == 3 || line[3] != '-' {
			return code, lines
		}
	}
}

func (cl *smtpClient) cmd(line string) (int, []string) {
	cl.t.Helper()
	cl.send(line + "\r\n")
	return cl.readReply()
}

func (cl *smtpClient) authPlain(user, pass string) {
	cl.t.Helper()
	code, lines := cl.cmd("AUTH PLAIN " + plainSMTP(user, pass))
	if code != 235 {
		cl.t.Fatalf("auth plain %d %v", code, lines)
	}
}

func (cl *smtpClient) authLogin(user, pass string) {
	cl.t.Helper()
	cl.send("AUTH LOGIN " + base64.StdEncoding.EncodeToString([]byte(user)) + "\r\n")
	code, lines := cl.readReply()
	if code != 334 {
		cl.t.Fatalf("auth login user %d %v", code, lines)
	}
	cl.send(base64.StdEncoding.EncodeToString([]byte(pass)) + "\r\n")
	code, lines = cl.readReply()
	if code != 235 {
		cl.t.Fatalf("auth login %d %v", code, lines)
	}
}

func (cl *smtpClient) mailData(from string, rcpts []string, body string) int {
	cl.t.Helper()
	code, lines := cl.cmd("MAIL FROM:<" + from + ">")
	if code != 250 {
		cl.t.Fatalf("mail %d %v", code, lines)
	}
	for _, rcpt := range rcpts {
		code, lines = cl.cmd("RCPT TO:<" + rcpt + ">")
		if code != 250 {
			cl.t.Fatalf("rcpt %s %d %v", rcpt, code, lines)
		}
	}
	cl.send("DATA\r\n")
	code, lines = cl.readReply()
	if code != 354 {
		cl.t.Fatalf("data %d %v", code, lines)
	}
	cl.send(body)
	if !strings.HasSuffix(body, "\r\n") {
		cl.send("\r\n")
	}
	cl.send(".\r\n")
	code, _ = cl.readReply()
	return code
}

func plainSMTP(user, pass string) string {
	raw := []byte{0}
	raw = append(raw, user...)
	raw = append(raw, 0)
	raw = append(raw, pass...)
	return base64.StdEncoding.EncodeToString(raw)
}

func inboxCount(t *testing.T, cc *ctrlClient) int {
	t.Helper()
	return len(cc.call(map[string]any{"op": "list-mail", "mailbox": "INBOX"}).ok(t).Messages)
}

func fetchRFC822(t *testing.T, cc *ctrlClient, uid uint32) []byte {
	t.Helper()
	rp := cc.call(map[string]any{"op": "fetch-mail", "mailbox": "INBOX", "uid": uid}).ok(t)
	b, err := base64.StdEncoding.DecodeString(rp.RFC822)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type smtpListReply struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error"`
	Messages []struct {
		From       string `json:"from"`
		Ret        string `json:"ret"`
		Envid      string `json:"envid"`
		SMTPUTF8   bool   `json:"smtputf8"`
		Size       *int   `json:"size"`
		Recipients []struct {
			Address string `json:"address"`
			Result  string `json:"result"`
			Notify  string `json:"notify"`
			Orcpt   string `json:"orcpt"`
		} `json:"recipients"`
	} `json:"messages"`
}

func listSMTP(t *testing.T, cc *ctrlClient) smtpListReply {
	t.Helper()
	b, err := json.Marshal(map[string]any{"op": "list-smtp"})
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	_ = cc.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := cc.w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := cc.w.Flush(); err != nil {
		t.Fatal(err)
	}
	_ = cc.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := cc.r.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var rp smtpListReply
	if err := json.Unmarshal(line, &rp); err != nil {
		t.Fatalf("list-smtp %s: %v", line, err)
	}
	if !rp.OK {
		t.Fatalf("list-smtp %s", rp.Error)
	}
	return rp
}

func TestSMTPCommandLineLimit(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", SmtpListen: smtpListenList{{Addr: "127.0.0.1:0"}}})
	cl := dialSMTP(t, bound.Smtp)
	cl.send(fmt.Sprintf("EHLO %s\r\n", strings.Repeat("a", smtpCmdMax)))
	code, _ := cl.readReply()
	if code != 500 {
		t.Fatalf("long command %d", code)
	}
	code, lines := cl.cmd("EHLO client")
	if code != 250 || !strings.Contains(strings.Join(lines, " "), "8BITMIME") {
		t.Fatalf("ehlo after long line %d %v", code, lines)
	}
}

func TestSMTPConfigForms(t *testing.T) {
	base := func(extra string) string {
		t.Helper()
		body := `{
			"listen":"127.0.0.1:0",
			"personality":"personalities/cyrus-3137.json",
			"dataset":"datasets/demo"`
		if extra != "" {
			body += "," + extra
		}
		body += "}"
		path := filepath.Join(t.TempDir(), "c.json")
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cfg, err := loadConfig(base(`"smtp_listen":""`))
	if err != nil || len(cfg.SmtpListen) != 0 || cfg.smtpAuthRequired() != true || cfg.smtpSizeLimit() != smtpSizeDefault {
		t.Fatalf("empty listen %+v %v", cfg.SmtpListen, err)
	}
	cfg, err = loadConfig(base(`"smtp_listen":["127.0.0.1:0",{"addr":"0.0.0.0:0","dsn":true}]`))
	if err != nil || len(cfg.SmtpListen) != 2 || cfg.SmtpListen[0].DSN || !cfg.SmtpListen[1].DSN || cfg.SmtpListen[1].Addr != "0.0.0.0:0" {
		t.Fatalf("array %+v %v", cfg.SmtpListen, err)
	}
	cfg, err = loadConfig(base(`"smtp_listen":"127.0.0.1:25","smtp_dsn":true,"smtp_auth_required":false,"smtp_size":20`))
	if err != nil || len(cfg.SmtpListen) != 1 || !cfg.SmtpListen[0].DSN || cfg.smtpAuthRequired() || cfg.smtpSizeLimit() != 20 {
		t.Fatalf("string dsn %+v auth %v size %d %v", cfg.SmtpListen, cfg.smtpAuthRequired(), cfg.smtpSizeLimit(), err)
	}
	if _, err := loadConfig(base(`"smtp_tls_listen":"127.0.0.1:0"`)); err == nil || !strings.Contains(err.Error(), "SMTP TLS") {
		t.Fatalf("tls without cert %v", err)
	}
	if _, err := loadConfig(base(`"smtp_listen":["127.0.0.1:0","127.0.0.1:0"],"smtp_dsn":true`)); err == nil {
		t.Fatal("smtp_dsn with two listeners")
	}
	if _, err := loadConfig(base(`"smtp_size":-1`)); err == nil {
		t.Fatal("negative size")
	}
	if _, err := loadConfig(base(`"smtp_listen":[{"listen":"127.0.0.1:0"}]`)); err == nil {
		t.Fatal("unknown object field")
	}
}

func TestSMTPListenersAndImplicitTLS(t *testing.T) {
	certA, keyA := writeNamedCert(t, "alpha", 1)
	certB, keyB := writeNamedCert(t, "beta", 2)
	leafA := parsePEMCert(t, mustRead(t, certA))
	_, bound, cc := startCtrl(t, Config{
		Listen:        "127.0.0.1:0",
		CertFile:      certA,
		KeyFile:       keyA,
		SmtpListen:    smtpListenList{{Addr: "127.0.0.1:0"}, {Addr: "0.0.0.0:0", DSN: true}},
		SmtpTLSListen: smtpListenList{{Addr: "127.0.0.1:0"}, {Addr: "127.0.0.1:0"}},
	})
	if len(bound.SmtpListeners) != 2 || bound.Smtp != bound.SmtpListeners[0] || bound.Smtp == "" {
		t.Fatalf("ready clear %+v %q", bound.SmtpListeners, bound.Smtp)
	}
	if len(bound.SmtpTLSListeners) != 2 || bound.SmtpTLS != bound.SmtpTLSListeners[0] || bound.SmtpTLS == "" {
		t.Fatalf("ready tls %+v %q", bound.SmtpTLSListeners, bound.SmtpTLS)
	}
	for _, addr := range append(append([]string{}, bound.SmtpListeners...), bound.SmtpTLSListeners...) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil || port == "" || port == "0" {
			t.Fatalf("bound port %q", addr)
		}
	}
	if !strings.HasPrefix(bound.SmtpListeners[0], "127.0.0.1:") {
		t.Fatalf("hosts %v", bound.SmtpListeners)
	}
	wildHost, wildPort, err := net.SplitHostPort(bound.SmtpListeners[1])
	if err != nil || (wildHost != "0.0.0.0" && wildHost != "::") || wildPort == "" || wildPort == "0" {
		t.Fatalf("wildcard %q", bound.SmtpListeners[1])
	}
	st := cc.call(map[string]any{"op": "status"}).ok(t)
	if st.Smtp != bound.Smtp || st.SmtpTLS != bound.SmtpTLS || strings.Join(st.SmtpListeners, ",") != strings.Join(bound.SmtpListeners, ",") || strings.Join(st.SmtpTLSListeners, ",") != strings.Join(bound.SmtpTLSListeners, ",") {
		t.Fatalf("status smtp %q %v tls %q %v", st.Smtp, st.SmtpListeners, st.SmtpTLS, st.SmtpTLSListeners)
	}

	plain := dialSMTP(t, bound.Smtp)
	_, lines := plain.cmd("EHLO client.example")
	text := strings.Join(lines, "\n")
	for _, want := range []string{"AUTH PLAIN LOGIN", "8BITMIME", "PIPELINING", "SMTPUTF8", "SIZE " + strconv.Itoa(smtpSizeDefault), "STARTTLS"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ehlo missing %s in %q", want, text)
		}
	}
	if strings.Contains(text, "\nDSN") || strings.HasPrefix(text, "DSN") || strings.Contains(text, " DSN") {
		t.Fatalf("dsn on plain listener %q", text)
	}
	dsn := dialSMTP(t, bound.SmtpListeners[1])
	_, lines = dsn.cmd("EHLO client.example")
	if !strings.Contains(strings.Join(lines, "\n"), "DSN") {
		t.Fatalf("dsn listener %q", lines)
	}
	sec := dialSMTPTLS(t, bound.SmtpTLS)
	_, lines = sec.cmd("EHLO client.example")
	text = strings.Join(lines, "\n")
	if strings.Contains(strings.ToUpper(text), "STARTTLS") || !strings.Contains(text, "AUTH PLAIN LOGIN") || strings.Contains(text, "DSN") {
		t.Fatalf("implicit ehlo %q", text)
	}
	if code, _ := sec.cmd("STARTTLS"); code != 454 {
		t.Fatalf("implicit starttls %d", code)
	}

	old := tls.Client(dialTCP(t, bound.SmtpTLSListeners[1]), &tls.Config{InsecureSkipVerify: true})
	if err := old.Handshake(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { old.Close() })
	if cn := old.ConnectionState().PeerCertificates[0].Subject.CommonName; cn != "alpha" {
		t.Fatalf("old cn %s", cn)
	}
	cc.call(map[string]any{"op": "reload-cert", "cert_file": certB, "key_file": keyB}).ok(t)
	if cn := old.ConnectionState().PeerCertificates[0].Subject.CommonName; cn != "alpha" {
		t.Fatalf("finished handshake changed to %s", cn)
	}
	if got := peerName(t, bound.SmtpTLS); got != "beta" {
		t.Fatalf("next smtp tls %s", got)
	}
	if got := smtpPeerName(t, bound.Smtp); got != "beta" {
		t.Fatalf("starttls after reload %s", got)
	}
	cc.arm(map[string]any{"do": "expired", "times": 1})
	if err := dialImplicitChecked(t, bound.SmtpTLS, leafA, "date"); err == nil || !strings.Contains(err.Error(), "date") {
		t.Fatalf("expired fault did not win: %v", err)
	}
}

func TestSMTPStringDSNAndSink(t *testing.T) {
	off := false
	_, bound, cc := startCtrl(t, Config{
		Listen:           "127.0.0.1:0",
		SmtpListen:       smtpListenList{{Addr: "127.0.0.1:0"}},
		SmtpDSN:          true,
		SmtpAuthRequired: &off,
		SmtpSize:         30,
	})
	cl := dialSMTP(t, bound.Smtp)
	_, lines := cl.cmd("EHLO client.example")
	text := strings.Join(lines, "\n")
	if strings.Contains(text, "AUTH") || !strings.Contains(text, "DSN") || !strings.Contains(text, "SIZE 30") || !strings.Contains(text, "PIPELINING") {
		t.Fatalf("sink ehlo %q", text)
	}
	cl.send("NOOP\r\nNOOP\r\n")
	if code, got := cl.readReply(); code != 250 || strings.Join(got, " ") != "2.0.0 OK" {
		t.Fatalf("pipe1 %d %v", code, got)
	}
	if code, got := cl.readReply(); code != 250 {
		t.Fatalf("pipe2 %d %v", code, got)
	}
	before := inboxCount(t, cc)
	if code, _ := cl.cmd("MAIL FROM:<demo@example.com>"); code != 250 {
		t.Fatalf("mail without auth %d", code)
	}
	if code, _ := cl.cmd("RCPT TO:<demo>"); code != 250 {
		t.Fatalf("rcpt %d", code)
	}
	if code, _ := cl.cmd("DATA"); code != 354 {
		t.Fatal("data prompt")
	}
	cl.send("Subject: sink\r\n\r\nbody\r\n.\r\n")
	if code, _ := cl.readReply(); code != 250 {
		t.Fatal("data")
	}
	if inboxCount(t, cc) != before {
		t.Fatal("sink delivered")
	}
	list := listSMTP(t, cc)
	if len(list.Messages) != 1 || list.Messages[0].From != "demo@example.com" || list.Messages[0].Recipients[0].Result != "discarded" {
		t.Fatalf("sink log %+v", list.Messages)
	}
	if !cc.call(map[string]any{"op": "wait-smtp", "count": 0}).OK {
		t.Fatal("wait")
	}
}

func TestSMTPParamsDSNAndFaults(t *testing.T) {
	cert, key := writeCert(t)
	_, bound, cc := startCtrl(t, Config{
		Listen:     "127.0.0.1:0",
		SmtpListen: smtpListenList{{Addr: "127.0.0.1:0"}},
		CertFile:   cert,
		KeyFile:    key,
		SmtpDSN:    true,
	})
	cl := dialSMTP(t, bound.Smtp)
	cl.authPlain("demo", "secret")
	before := inboxCount(t, cc)
	beforeLog := len(listSMTP(t, cc).Messages)

	if code, lines := cl.cmd("MAIL FROM:<demo@example.com> SIZE=10000001"); code != 552 || !strings.Contains(strings.Join(lines, " "), "5.3.4") {
		t.Fatalf("size %d %v", code, lines)
	}
	if len(listSMTP(t, cc).Messages) != beforeLog || inboxCount(t, cc) != before {
		t.Fatal("oversize stored")
	}
	if code, _ := cl.cmd("MAIL FROM:<demo@example.com> XFOO=1"); code != 501 {
		t.Fatalf("unknown mail param %d", code)
	}
	if code, _ := cl.cmd("MAIL FROM:<demo@example.com> RET=BOTH"); code != 501 {
		t.Fatalf("ret %d", code)
	}
	if code, _ := cl.cmd("MAIL FROM:<demo@example.com> BODY=8BITMIME SIZE=10 RET=HDRS ENVID=env-1 SMTPUTF8"); code != 250 {
		t.Fatal("mail params")
	}
	if code, _ := cl.cmd("RCPT TO:<demo> NOTIFY=NOPE"); code != 501 {
		t.Fatalf("notify %d", code)
	}
	if code, _ := cl.cmd("RCPT TO:<demo> NOTIFY=FAILURE,SUCCESS ORCPT=rfc822;demo@example.com"); code != 250 {
		t.Fatal("rcpt params")
	}
	if code, _ := cl.cmd("RCPT TO:<stranger>"); code != 250 {
		t.Fatal("rcpt omit")
	}
	if code, _ := cl.cmd("RCPT TO:<other@example.com> NOTIFY=SUCCESS"); code != 250 {
		t.Fatal("rcpt success")
	}
	if code := cl.finishData("Subject: p\r\n\r\nbody\r\n"); code != 250 {
		t.Fatalf("data %d", code)
	}
	if inboxCount(t, cc) != before+1 {
		t.Fatalf("stored %d", inboxCount(t, cc))
	}
	msg := listSMTP(t, cc).Messages
	last := msg[len(msg)-1]
	if last.From != "demo@example.com" || last.Ret != "HDRS" || last.Envid != "env-1" || !last.SMTPUTF8 || last.Size == nil || *last.Size != 10 {
		t.Fatalf("record %+v", last)
	}
	if len(last.Recipients) != 3 || last.Recipients[0].Notify != "FAILURE,SUCCESS" || last.Recipients[0].Orcpt != "rfc822;demo@example.com" || last.Recipients[0].Result != "delivered" {
		t.Fatalf("rcpts %+v", last.Recipients)
	}
	if last.Recipients[1].Notify != "" || last.Recipients[1].Result != "discarded" || last.Recipients[2].Notify != "SUCCESS" {
		t.Fatalf("other rcpts %+v", last.Recipients)
	}
	msgs := cc.call(map[string]any{"op": "list-mail", "mailbox": "INBOX"}).ok(t).Messages
	got := string(fetchRFC822(t, cc, msgs[len(msgs)-1].UID))
	if strings.Contains(got, "message/delivery-status") {
		t.Fatal("success report without inject-dsn")
	}

	if cc.call(map[string]any{"op": "inject-dsn"}).OK || cc.call(map[string]any{"op": "inject-dsn", "code": "nope"}).OK {
		t.Fatal("bad inject-dsn")
	}
	rawID := cc.call(map[string]any{"op": "inject-dsn", "code": "failed", "raw": "RAW-BYTES"}).ok(t).UID
	if string(fetchRFC822(t, cc, rawID)) != "RAW-BYTES" {
		t.Fatal("raw dsn")
	}
	uid := cc.call(map[string]any{"op": "inject-dsn", "code": "delivered", "recipient": "a@b", "envid": "zzz"}).ok(t).UID
	report := string(fetchRFC822(t, cc, uid))
	if !strings.Contains(report, "message/delivery-status") || !strings.Contains(report, "Action: delivered") || !strings.Contains(report, "Original-Envelope-Id: zzz") || !strings.Contains(report, "a@b") {
		t.Fatalf("delivered report %q", report)
	}
	if inboxCount(t, cc) != before+3 {
		t.Fatalf("inject count %d", inboxCount(t, cc))
	}

	cc.arm(map[string]any{"do": "bounce", "command": "DATA", "proto": "smtp", "times": 1})
	if code, _ := cl.cmd("MAIL FROM:<demo@example.com> ENVID=bounce-env"); code != 250 {
		t.Fatal("bounce mail")
	}
	if code, _ := cl.cmd("RCPT TO:<demo> NOTIFY=FAILURE"); code != 250 {
		t.Fatal("bounce rcpt")
	}
	if code, _ := cl.cmd("RCPT TO:<stranger>"); code != 250 {
		t.Fatal("bounce omit")
	}
	if code, _ := cl.cmd("RCPT TO:<other@example.com> NOTIFY=SUCCESS"); code != 250 {
		t.Fatal("bounce success")
	}
	if code := cl.finishData("Subject: b\r\n\r\nbounce\r\n"); code != 250 {
		t.Fatalf("bounce data %d", code)
	}
	if inboxCount(t, cc) != before+6 {
		t.Fatalf("bounce stored %d want %d", inboxCount(t, cc), before+6)
	}
	msgs = cc.call(map[string]any{"op": "list-mail", "mailbox": "INBOX"}).ok(t).Messages
	var reports []string
	for _, m := range msgs[len(msgs)-2:] {
		reports = append(reports, string(fetchRFC822(t, cc, m.UID)))
	}
	joined := strings.Join(reports, "\n")
	if strings.Count(joined, "message/delivery-status") != 2 || strings.Count(joined, "Action: failed") != 2 || strings.Count(joined, "Original-Envelope-Id: bounce-env") != 2 {
		t.Fatalf("bounce reports %q", joined)
	}
	if !strings.Contains(joined, "demo") || !strings.Contains(joined, "stranger") || strings.Contains(joined, "other@example.com") {
		t.Fatalf("bounce recipients %q", joined)
	}

	if cc.call(map[string]any{"op": "arm-fault", "do": "reply", "command": "EHLO", "proto": "smtp"}).OK {
		t.Fatal("smtp reply without code")
	}
	if cc.call(map[string]any{"op": "arm-fault", "do": "bounce", "command": "MAIL", "proto": "smtp"}).OK {
		t.Fatal("bounce on mail")
	}
	if cc.call(map[string]any{"op": "arm-fault", "do": "drop_after_354", "command": "DATA"}).OK {
		t.Fatal("drop without smtp proto")
	}
	if cc.call(map[string]any{"op": "arm-fault", "do": "drip", "command": "EHLO", "proto": "smtp", "delay_ms": 1}).OK {
		t.Fatal("smtp drip")
	}
	cc.arm(map[string]any{"do": "close", "command": "EHLO", "times": 1})
	if code, _ := cl.cmd("EHLO client.example"); code != 250 {
		t.Fatal("imap fault matched smtp")
	}
	cc.call(map[string]any{"op": "clear-faults"}).ok(t)

	cc.arm(map[string]any{"do": "reply", "command": "EHLO", "proto": "smtp", "code": "421", "text": "go away", "times": 1})
	if code, lines := cl.cmd("EHLO client.example"); code != 421 || !strings.Contains(strings.Join(lines, " "), "go away") {
		t.Fatalf("reply %d %v", code, lines)
	}
	cc.arm(map[string]any{"do": "silent", "command": "EHLO", "proto": "smtp", "times": 1})
	cl.send("EHLO client.example\r\nNOOP\r\n")
	if code, lines := cl.readReply(); code != 250 || strings.Contains(strings.Join(lines, " "), "8BITMIME") {
		t.Fatalf("silent %d %v", code, lines)
	}
	cc.arm(map[string]any{"do": "raw", "command": "NOOP", "proto": "smtp", "text": "250 from-raw", "times": 1})
	if code, lines := cl.cmd("NOOP"); code != 250 || strings.Join(lines, " ") != "from-raw" {
		t.Fatalf("raw %d %v", code, lines)
	}
	cc.arm(map[string]any{"do": "delay", "command": "NOOP", "proto": "smtp", "delay_ms": 200, "times": 1})
	started := time.Now()
	if code, _ := cl.cmd("NOOP"); code != 250 || time.Since(started) < 150*time.Millisecond {
		t.Fatalf("delay %s", time.Since(started))
	}
	cc.arm(map[string]any{"do": "reply", "command": "AUTH", "proto": "smtp", "code": "454", "text": "later", "times": 1})
	if code, lines := cl.cmd("AUTH PLAIN " + plainSMTP("demo", "secret")); code != 454 || !strings.Contains(strings.Join(lines, " "), "later") {
		t.Fatalf("auth fault %d %v", code, lines)
	}
	cc.arm(map[string]any{"do": "close", "command": "STARTTLS", "proto": "smtp", "times": 1})
	if _, _, err := cl.tryCmd("STARTTLS"); err == nil {
		t.Fatal("starttls stayed open")
	}

	survivor := dialSMTP(t, bound.Smtp)
	survivor.authPlain("demo", "secret")
	victim := dialSMTP(t, bound.Smtp)
	victim.authPlain("demo", "secret")
	cc.arm(map[string]any{"do": "rst", "command": "RCPT", "proto": "smtp", "conn": 3, "times": 1})
	if code, _ := survivor.cmd("NOOP"); code != 250 {
		t.Fatal("other conn")
	}
	if _, _, err := victim.tryCmd("RCPT TO:<demo>"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reset") {
		t.Fatalf("rst %v", err)
	}

	third := dialSMTP(t, bound.Smtp)
	third.authPlain("demo", "secret")
	cc.arm(map[string]any{"do": "half_close", "command": "MAIL", "proto": "smtp", "conn": 0, "times": 1})
	if _, _, err := third.tryCmd("MAIL FROM:<a@b>"); err == nil {
		t.Fatal("half close stayed open")
	}

	fourth := dialSMTP(t, bound.Smtp)
	fourth.authPlain("demo", "secret")
	nLog := len(listSMTP(t, cc).Messages)
	nBox := inboxCount(t, cc)
	cc.arm(map[string]any{"do": "reply", "command": "DATA", "proto": "smtp", "code": "550", "text": "nope", "times": 1})
	if code, _ := fourth.cmd("MAIL FROM:<demo@example.com>"); code != 250 {
		t.Fatal("mail")
	}
	if code, _ := fourth.cmd("RCPT TO:<demo>"); code != 250 {
		t.Fatal("rcpt")
	}
	if code, lines := fourth.cmd("DATA"); code != 550 || !strings.Contains(strings.Join(lines, " "), "nope") {
		t.Fatalf("data reply %d %v", code, lines)
	}
	cc.arm(map[string]any{"do": "drop_after_354", "command": "DATA", "proto": "smtp", "bytes": 4, "times": 1})
	if code, _ := fourth.cmd("DATA"); code != 354 {
		t.Fatal("drop prompt")
	}
	fourth.send("abcd")
	if _, _, err := fourth.tryRead(); err == nil {
		t.Fatal("drop stayed open")
	}
	if len(listSMTP(t, cc).Messages) != nLog || inboxCount(t, cc) != nBox {
		t.Fatal("fault stored mail")
	}
}

func (cl *smtpClient) finishData(body string) int {
	cl.t.Helper()
	cl.send("DATA\r\n")
	code, lines := cl.readReply()
	if code != 354 {
		cl.t.Fatalf("data %d %v", code, lines)
	}
	cl.send(body)
	if !strings.HasSuffix(body, "\r\n") {
		cl.send("\r\n")
	}
	cl.send(".\r\n")
	code, _ = cl.readReply()
	return code
}

func (cl *smtpClient) tryCmd(line string) (int, []string, error) {
	cl.t.Helper()
	cl.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(cl.w, line+"\r\n"); err != nil {
		return 0, nil, err
	}
	if err := cl.w.Flush(); err != nil {
		return 0, nil, err
	}
	return cl.tryRead()
}

func (cl *smtpClient) tryRead() (int, []string, error) {
	var lines []string
	code := 0
	for {
		cl.c.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := cl.r.ReadString('\n')
		if err != nil {
			return code, lines, err
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 3 {
			return code, lines, fmt.Errorf("short reply %q", line)
		}
		n, err := strconv.Atoi(line[:3])
		if err != nil {
			return code, lines, err
		}
		if code == 0 {
			code = n
		}
		text := ""
		if len(line) > 4 {
			text = line[4:]
		}
		lines = append(lines, text)
		if len(line) == 3 || line[3] != '-' {
			return code, lines, nil
		}
	}
}

func dialSMTPTLS(t *testing.T, addr string) *smtpClient {
	t.Helper()
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cl := &smtpClient{t: t, c: c, r: bufio.NewReader(c), w: bufio.NewWriter(c)}
	code, _ := cl.readReply()
	if code != 220 {
		t.Fatalf("tls greeting %d", code)
	}
	return cl
}
