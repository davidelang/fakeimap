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
	_, bound = start(t, Config{Listen: "127.0.0.1:0", SmtpListen: ""})
	if bound.Smtp != "" {
		t.Fatalf("empty smtp_listen bound %q", bound.Smtp)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", SmtpListen: "127.0.0.1:0"})
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
		SmtpListen: "127.0.0.1:0",
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
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", SmtpListen: "127.0.0.1:0"})
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
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", SmtpListen: "127.0.0.1:0"})
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
		Recipients []struct {
			Address string `json:"address"`
			Result  string `json:"result"`
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
	_, bound := start(t, Config{Listen: "127.0.0.1:0", SmtpListen: "127.0.0.1:0"})
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
