package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func oauthPers(t *testing.T, saslIR bool) string {
	t.Helper()
	p, err := loadPersonality("personalities/cyrus-22.json")
	if err != nil {
		t.Fatal(err)
	}
	caps := []string{"IMAP4rev1", "AUTH=PLAIN", "AUTH=OAUTHBEARER", "AUTH=XOAUTH2"}
	if saslIR {
		caps = []string{"IMAP4rev1", "SASL-IR", "AUTH=PLAIN", "AUTH=OAUTHBEARER", "AUTH=XOAUTH2"}
	}
	p.GreetingCaps = append([]string{}, caps...)
	p.PreLogin = append([]string{}, caps...)
	p.PostLogin = []string{"IMAP4rev1"}
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

func callMap(t *testing.T, cc *ctrlClient, req any) map[string]any {
	t.Helper()
	b, err := json.Marshal(req)
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
	var rp map[string]any
	if err := json.Unmarshal(line, &rp); err != nil {
		t.Fatal(err)
	}
	if rp["ok"] != true {
		t.Fatalf("control %v", rp["error"])
	}
	return rp
}

func issuedToken(t *testing.T, cc *ctrlClient, scopes []string) (id, token string) {
	t.Helper()
	req := map[string]any{"op": "issue", "username": "demo"}
	if scopes != nil {
		req["scopes"] = scopes
	}
	rp := callMap(t, cc, req)
	id, _ = rp["id"].(string)
	token, _ = rp["access_token"].(string)
	if id == "" || token == "" {
		t.Fatal("issue reply missing token")
	}
	return id, token
}

func xoauth2Line(user, token string) string {
	raw := "user=" + user + "\x01auth=Bearer " + token + "\x01\x01"
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func oauthBearerLine(user, token string) string {
	raw := "n,a=" + user + ",\x01host=localhost\x01port=143\x01auth=Bearer " + token + "\x01\x01"
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func expectJSONContinue(t *testing.T, line, status string) {
	t.Helper()
	payload := ""
	switch {
	case strings.HasPrefix(line, "+ "):
		payload = strings.TrimPrefix(line, "+ ")
	case strings.HasPrefix(line, "334 "):
		payload = strings.TrimPrefix(line, "334 ")
	default:
		t.Fatalf("continue %q", line)
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("continue %q", line)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("continue json %q", line)
	}
	if obj["status"] != status {
		t.Fatalf("status %v", obj["status"])
	}
}

func finishXOAuth2(t *testing.T, cl *imapClient, tag, token, status string) {
	t.Helper()
	cl.send(tag + " AUTHENTICATE XOAUTH2 " + xoauth2Line("demo", token) + "\r\n")
	expectJSONContinue(t, strings.TrimRight(cl.readRec(), "\r\n"), status)
	cl.send("\r\n")
	if rp := cl.readTag(tag); rp.status != "NO" {
		t.Fatalf("%s %s", tag, rp.status)
	}
}

func TestXOAuth2JSONContinue(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{
		Listen:      "127.0.0.1:0",
		SmtpListen:  smtpListenList{{Addr: "127.0.0.1:0"}},
		Personality: oauthPers(t, true),
	})
	cl := dial(t, bound.Listen)
	finishXOAuth2(t, cl, "a1", "nope", "invalid_token")

	id, revoked := issuedToken(t, cc, nil)
	callMap(t, cc, map[string]any{"op": "revoke", "token_id": id})
	finishXOAuth2(t, cl, "a2", revoked, "invalid_token")

	_, scoped := issuedToken(t, cc, []string{"smtp"})
	finishXOAuth2(t, cl, "a3", scoped, "insufficient_scope")

	_, expiring := issuedToken(t, cc, nil)
	cc.arm(map[string]any{"do": "expired_token", "command": "AUTHENTICATE"})
	finishXOAuth2(t, cl, "a4", expiring, "invalid_token")

	smtp := dialSMTP(t, bound.Smtp)
	code, lines := smtp.cmd("AUTH XOAUTH2 " + xoauth2Line("demo", "nope"))
	if code != 334 || len(lines) == 0 {
		t.Fatalf("smtp continue %d %v", code, lines)
	}
	expectJSONContinue(t, "334 "+lines[0], "invalid_token")
	smtp.send("\r\n")
	code, lines = smtp.readReply()
	if code != 535 {
		t.Fatalf("smtp fail %d %v", code, lines)
	}
}

func TestBearerSASLIR(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{
		Listen:      "127.0.0.1:0",
		Personality: oauthPers(t, false),
	})
	_, tok := issuedToken(t, cc, nil)
	ir := oauthBearerLine("demo", tok)
	cl := dial(t, bound.Listen)
	cl.send("a1 AUTHENTICATE OAUTHBEARER " + ir + "\r\n")
	if got := strings.TrimRight(cl.readRec(), "\r\n"); got != "+" {
		t.Fatalf("challenge first %q", got)
	}
	cl.send(ir + "\r\n")
	cl.readTag("a1").mustOK(t)

	cl2 := dial(t, bound.Listen)
	cl2.send("a1 AUTHENTICATE OAUTHBEARER =\r\n")
	if got := strings.TrimRight(cl2.readRec(), "\r\n"); got != "+" {
		t.Fatalf("equals still challenges %q", got)
	}
	cl2.send(ir + "\r\n")
	cl2.readTag("a1").mustOK(t)

	_, boundIR, ccIR := startCtrl(t, Config{
		Listen:      "127.0.0.1:0",
		Personality: oauthPers(t, true),
	})
	_, tokIR := issuedToken(t, ccIR, nil)
	okIR := oauthBearerLine("demo", tokIR)
	with := dial(t, boundIR.Listen)
	with.send("b1 AUTHENTICATE OAUTHBEARER\r\n")
	if got := strings.TrimRight(with.readRec(), "\r\n"); got != "+" {
		t.Fatalf("missing IR %q", got)
	}
	with.send(okIR + "\r\n")
	with.readTag("b1").mustOK(t)

	direct := dial(t, boundIR.Listen)
	direct.send("b1 AUTHENTICATE OAUTHBEARER " + okIR + "\r\n")
	direct.readTag("b1").mustOK(t)

	empty := dial(t, boundIR.Listen)
	empty.send("b1 AUTHENTICATE OAUTHBEARER =\r\n")
	expectJSONContinue(t, strings.TrimRight(empty.readRec(), "\r\n"), "invalid_token")
	empty.send("AQ==\r\n")
	if rp := empty.readTag("b1"); rp.status != "NO" {
		t.Fatalf("empty IR %s", rp.status)
	}
}
