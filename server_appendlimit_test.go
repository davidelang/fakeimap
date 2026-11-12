package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePersonality(t *testing.T, pre, post []string) string {
	t.Helper()
	p, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PreLogin = pre
	p.PostLogin = post
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

func statusField(t *testing.T, cl *imapClient, box, item string) string {
	t.Helper()
	rp := cl.cmd("STATUS " + box + " (" + item + ")").mustOK(t)
	if len(rp.untagged) != 1 {
		t.Fatalf("status lines %q", rp.untagged)
	}
	mark := item + " "
	i := strings.Index(rp.untagged[0], mark)
	if i < 0 {
		t.Fatalf("no %s in %q", item, rp.untagged[0])
	}
	rest := strings.TrimRight(rp.untagged[0][i+len(mark):], ")")
	fs := strings.Fields(rest)
	if len(fs) == 0 {
		t.Fatalf("empty %s in %q", item, rp.untagged[0])
	}
	return fs[0]
}

func TestAppendLimitLoopback(t *testing.T) {
	pre := []string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "LIST-EXTENDED", "LIST-STATUS", "AUTH=PLAIN", "APPENDLIMIT=10"}
	post := []string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "LIST-EXTENDED", "LIST-STATUS", "APPENDLIMIT=10"}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: writePersonality(t, pre, post)})
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "APPENDLIMIT") {
		t.Fatalf("greeting lists APPENDLIMIT: %q", cl.greeting)
	}
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "APPENDLIMIT=10") {
		t.Fatal("pre-login missing APPENDLIMIT=10")
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "APPENDLIMIT=10") {
		t.Fatal("post-login missing APPENDLIMIT=10")
	}
	cl.cmd("CREATE Lim").mustOK(t)
	if got := statusField(t, cl, "Lim", "MESSAGES"); got != "0" {
		t.Fatalf("messages before %s", got)
	}
	if got := statusField(t, cl, "INBOX", "APPENDLIMIT"); got != "10" {
		t.Fatalf("inbox limit %s", got)
	}
	if got := statusField(t, cl, "Lim", "APPENDLIMIT"); got != "10" {
		t.Fatalf("lim limit %s", got)
	}
	rp := cl.cmd("APPEND Lim {10+}\r\n0123456789").mustOK(t)
	if !strings.Contains(rp.tagLine, "[APPENDUID ") {
		t.Fatalf("exact append %s", rp.tagLine)
	}
	if got := statusField(t, cl, "Lim", "MESSAGES"); got != "1" {
		t.Fatalf("messages after exact %s", got)
	}
	big := cl.cmd("APPEND Lim {11+}\r\n0123456789X")
	big.no(t)
	if !strings.Contains(big.tagLine, "[TOOBIG]") {
		t.Fatalf("oversize %s", big.tagLine)
	}
	if got := statusField(t, cl, "Lim", "MESSAGES"); got != "1" {
		t.Fatalf("messages after reject %s", got)
	}
	lst := cl.cmd(`LIST "" Lim RETURN (STATUS (APPENDLIMIT))`).mustOK(t)
	saw := false
	for _, u := range lst.untagged {
		if strings.Contains(u, "APPENDLIMIT 10") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("list status %q", lst.untagged)
	}
}

func TestAppendLimitAbsent(t *testing.T) {
	pre := []string{"IMAP4rev1", "LITERAL+", "AUTH=PLAIN", "UIDPLUS", "LIST-EXTENDED", "LIST-STATUS"}
	post := []string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "LIST-EXTENDED", "LIST-STATUS"}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: writePersonality(t, pre, post)})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	rp := cl.cmd("STATUS INBOX (APPENDLIMIT)")
	if rp.status != "BAD" {
		t.Fatalf("status without capability %s %s", rp.status, rp.tagLine)
	}
	cl.cmd("CREATE Lim").mustOK(t)
	cl.cmd("APPEND Lim {11+}\r\n0123456789X").mustOK(t)
	if got := statusField(t, cl, "Lim", "MESSAGES"); got != "1" {
		t.Fatalf("stored %s", got)
	}
}

func TestAppendLimitTokenForm(t *testing.T) {
	if capImplemented("APPENDLIMIT") || capImplemented("APPENDLIMIT=0") || capImplemented("APPENDLIMIT=10x") || capImplemented("APPENDLIMIT=") {
		t.Fatal("bad APPENDLIMIT token is implemented")
	}
	if _, ok := parseAppendLimit("APPENDLIMIT=18446744073709551616"); ok {
		t.Fatal("overflow is implemented")
	}
	n, ok := parseAppendLimit("APPENDLIMIT=10")
	if !ok || n != 10 {
		t.Fatalf("parse 10: %d %v", n, ok)
	}
	pre := []string{"IMAP4rev1", "APPENDLIMIT", "APPENDLIMIT=0", "APPENDLIMIT=12abc", "APPENDLIMIT=10", "AUTH=PLAIN"}
	post := []string{"IMAP4rev1", "APPENDLIMIT", "APPENDLIMIT=0", "APPENDLIMIT=12abc", "APPENDLIMIT=10"}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: writePersonality(t, pre, post)})
	cl := dial(t, bound.Listen)
	got := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	for _, c := range got {
		u := strings.ToUpper(c)
		if u == "APPENDLIMIT" || u == "APPENDLIMIT=0" || strings.Contains(u, "ABC") {
			t.Fatalf("advertised %s in %v", c, got)
		}
	}
	if !listed(got, "APPENDLIMIT=10") {
		t.Fatalf("missing number token %v", got)
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	if got := statusField(t, cl, "INBOX", "APPENDLIMIT"); got != "10" {
		t.Fatalf("status %s", got)
	}
}

func TestAppendLimitUsesPostLogin(t *testing.T) {
	pre := []string{"IMAP4rev1", "LITERAL+", "AUTH=PLAIN", "APPENDLIMIT=10"}
	post := []string{"IMAP4rev1", "LITERAL+", "APPENDLIMIT=20"}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: writePersonality(t, pre, post)})
	cl := dial(t, bound.Listen)
	before := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	if !listed(before, "APPENDLIMIT=10") || listed(before, "APPENDLIMIT=20") {
		t.Fatalf("pre %v", before)
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	after := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	if !listed(after, "APPENDLIMIT=20") || listed(after, "APPENDLIMIT=10") {
		t.Fatalf("post %v", after)
	}
	if got := statusField(t, cl, "INBOX", "APPENDLIMIT"); got != "20" {
		t.Fatalf("status %s", got)
	}
	cl.cmd("CREATE Lim").mustOK(t)
	rp := cl.cmd("APPEND Lim {20+}\r\n" + strings.Repeat("a", 20)).mustOK(t)
	if !strings.Contains(rp.tagLine, "[APPENDUID ") {
		t.Fatalf("exact %s", rp.tagLine)
	}
	big := cl.cmd("APPEND Lim {21+}\r\n" + strings.Repeat("b", 21))
	big.no(t)
	if !strings.Contains(big.tagLine, "[TOOBIG]") {
		t.Fatalf("oversize %s", big.tagLine)
	}
	if got := statusField(t, cl, "Lim", "MESSAGES"); got != "1" {
		t.Fatalf("messages %s", got)
	}
}

func TestAppendLimitDroppedAfterLogin(t *testing.T) {
	pre := []string{"IMAP4rev1", "LITERAL+", "AUTH=PLAIN", "APPENDLIMIT=10"}
	post := []string{"IMAP4rev1", "LITERAL+"}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: writePersonality(t, pre, post)})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	after := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	for _, c := range after {
		if strings.HasPrefix(strings.ToUpper(c), "APPENDLIMIT") {
			t.Fatalf("post still lists %s", c)
		}
	}
	rp := cl.cmd("STATUS INBOX (APPENDLIMIT)")
	if rp.status != "BAD" {
		t.Fatalf("status %s %s", rp.status, rp.tagLine)
	}
	cl.cmd("CREATE Lim").mustOK(t)
	cl.cmd("APPEND Lim {11+}\r\n0123456789X").mustOK(t)
}

func TestRecordedAppendLimit(t *testing.T) {
	cases := []struct {
		file string
		num  string
	}{
		{"personalities/cyrus-3137.json", "2147483647"},
		{"personalities/cyrus-36.json", "4294967295"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: c.file})
			cl := dial(t, bound.Listen)
			if strings.Contains(cl.greeting, "APPENDLIMIT") {
				t.Fatalf("greeting %q", cl.greeting)
			}
			token := "APPENDLIMIT=" + c.num
			if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), token) {
				t.Fatal("pre-login missing token")
			}
			login := cl.cmd("LOGIN demo secret").mustOK(t)
			if !listed(capsOf(login.tagLine), token) {
				t.Fatalf("login %s", login.tagLine)
			}
			if got := statusField(t, cl, "INBOX", "APPENDLIMIT"); got != c.num {
				t.Fatalf("status %s", got)
			}
		})
	}
}

func TestAppendLimitOnlyOnNamedPersonalities(t *testing.T) {
	allowPost := map[string]bool{
		"cyrus-3124.json": true,
		"cyrus-3137.json": true,
		"cyrus-36.json":   true,
		"sasl.json":       true,
	}
	allowPre := map[string]bool{
		"cyrus-3124.json": true,
		"cyrus-3137.json": true,
		"cyrus-36.json":   true,
	}
	matches, err := filepath.Glob("personalities/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no personalities")
	}
	for _, f := range matches {
		base := filepath.Base(f)
		if strings.HasSuffix(base, ".gaps.json") {
			continue
		}
		p, err := loadPersonality(f)
		if err != nil {
			t.Fatal(err)
		}
		check := func(list []string, allow bool, where string) {
			t.Helper()
			found := false
			for _, c := range list {
				if !strings.HasPrefix(strings.ToUpper(c), "APPENDLIMIT") {
					continue
				}
				found = true
				if !allow {
					t.Fatalf("%s %s lists %s", base, where, c)
				}
			}
			if allow && !found {
				t.Fatalf("%s %s missing APPENDLIMIT", base, where)
			}
		}
		check(p.GreetingCaps, false, "greeting")
		check(p.PreLogin, allowPre[base], "pre")
		check(p.PostLogin, allowPost[base], "post")
	}
}
