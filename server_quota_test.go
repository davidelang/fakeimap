package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func emptyQuotaDataset(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	raw := []byte(`{"accounts":[{"username":"demo","password":"secret"}],"mailboxes":[{"name":"INBOX","uid_validity":1}]}`)
	if err := os.WriteFile(filepath.Join(dir, "dataset.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func quotaPers(t *testing.T, caps ...string) string {
	t.Helper()
	pre := append([]string{"IMAP4rev1", "LITERAL+", "AUTH=PLAIN"}, caps...)
	post := append([]string{"IMAP4rev1", "LITERAL+"}, caps...)
	return writePersonality(t, pre, post)
}

func wantQuota(use, slim uint64, msg bool, muse, mlim uint64) string {
	line := fmt.Sprintf("* QUOTA ROOT (STORAGE %d %d", use, slim)
	if msg {
		line += fmt.Sprintf(" MESSAGE %d %d", muse, mlim)
	}
	return line + ")"
}

func TestQuotaReport(t *testing.T) {
	for _, c := range []string{"QUOTA", "QUOTA=RES-STORAGE", "QUOTA=RES-MESSAGE", "QUOTASET"} {
		if !capImplemented(c) {
			t.Fatalf("%s is not implemented", c)
		}
	}
	if capImplemented("QUOTA=RES-ANNOTATION-STORAGE") || capImplemented("QUOTA=RES-MAILBOX") {
		t.Fatal("annotation storage or mailbox count is implemented")
	}
	dir := emptyQuotaDataset(t)
	pers := quotaPers(t, "QUOTA", "QUOTA=RES-STORAGE", "QUOTA=RES-MESSAGE", "QUOTASET")
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: pers, Dataset: dir})
	cl := dial(t, bound.Listen)
	for _, cmd := range []string{"GETQUOTA ROOT", "GETQUOTAROOT INBOX", "SETQUOTA ROOT (STORAGE 1)"} {
		rp := cl.cmd(cmd)
		if rp.status != "NO" || len(rp.untagged) != 0 {
			t.Fatalf("before login %s -> %s %q", cmd, rp.status, rp.untagged)
		}
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	startLine := wantQuota(0, 1048576, true, 0, 1000000)
	got := cl.cmd("GETQUOTA ROOT").mustOK(t)
	if len(got.untagged) != 1 || got.untagged[0] != startLine {
		t.Fatalf("empty %q", got.untagged)
	}
	if cl.cmd("GETQUOTA").status != "BAD" || cl.cmd("GETQUOTA ROOT EXTRA").status != "BAD" {
		t.Fatal("bad GETQUOTA arity was accepted")
	}
	if rp := cl.cmd("GETQUOTA INBOX"); rp.status != "NO" || len(rp.untagged) != 0 {
		t.Fatalf("other root %s %q", rp.status, rp.untagged)
	}
	if cl.cmd("GETQUOTAROOT").status != "BAD" || cl.cmd("GETQUOTAROOT INBOX EXTRA").status != "BAD" {
		t.Fatal("bad GETQUOTAROOT arity was accepted")
	}
	if rp := cl.cmd("GETQUOTAROOT Missing"); rp.status != "NO" || len(rp.untagged) != 0 {
		t.Fatalf("missing mailbox %s %q", rp.status, rp.untagged)
	}
	root := cl.cmd("GETQUOTAROOT inbox").mustOK(t)
	if len(root.untagged) != 2 || root.untagged[0] != "* QUOTAROOT INBOX ROOT" || root.untagged[1] != startLine {
		t.Fatalf("quotaroot %q", root.untagged)
	}
	body := strings.Repeat("a", 1024)
	cl.cmd("APPEND INBOX {" + fmt.Sprint(len(body)) + "+}\r\n" + body).mustOK(t)
	one := wantQuota(1, 1048576, true, 1, 1000000)
	if got := cl.cmd("GETQUOTA root").mustOK(t); len(got.untagged) != 1 || got.untagged[0] != one {
		t.Fatalf("1024 octets %q", got.untagged)
	}
	cl.cmd("APPEND INBOX {1+}\r\nX").mustOK(t)
	two := wantQuota(2, 1048576, true, 2, 1000000)
	if got := cl.cmd("GETQUOTA ROOT").mustOK(t); got.untagged[0] != two {
		t.Fatalf("1025 octets %q", got.untagged)
	}
	cl.cmd("SELECT INBOX").mustOK(t)
	cl.cmd(`STORE 2 +FLAGS (\Deleted)`).mustOK(t)
	if got := cl.cmd("GETQUOTA ROOT").mustOK(t); got.untagged[0] != two {
		t.Fatalf("deleted still counts %q", got.untagged)
	}
	cl.cmd("EXPUNGE").mustOK(t)
	if got := cl.cmd("GETQUOTA ROOT").mustOK(t); got.untagged[0] != one {
		t.Fatalf("after expunge %q", got.untagged)
	}
	cl.cmd("CREATE Other").mustOK(t)
	cl.cmd("COPY 1 Other").mustOK(t)
	copied := wantQuota(2, 1048576, true, 2, 1000000)
	if got := cl.cmd("GETQUOTA ROOT").mustOK(t); got.untagged[0] != copied {
		t.Fatalf("copy counts %q", got.untagged)
	}
	cl.cmd(`CREATE "A B"`).mustOK(t)
	list := cl.cmd(`LIST "" "A B"`).mustOK(t)
	qr := cl.cmd(`GETQUOTAROOT "A B"`).mustOK(t)
	if len(list.untagged) != 1 || !strings.Contains(list.untagged[0], `"A B"`) {
		t.Fatalf("list spelling %q", list.untagged)
	}
	if len(qr.untagged) != 2 || qr.untagged[0] != `* QUOTAROOT "A B" ROOT` || strings.Contains(qr.untagged[1], "ANNOTATION") || strings.Contains(qr.untagged[1], "MAILBOX") {
		t.Fatalf("quoted root %q", qr.untagged)
	}

	both := cl.cmd("SETQUOTA root (message 12 storage 8)").mustOK(t)
	wantBoth := wantQuota(2, 8, true, 2, 12)
	if len(both.untagged) != 1 || both.untagged[0] != wantBoth {
		t.Fatalf("set both %q", both.untagged)
	}
	only := cl.cmd("SETQUOTA ROOT (STORAGE 9)").mustOK(t)
	if only.untagged[0] != wantQuota(2, 9, true, 2, 12) {
		t.Fatalf("storage only %q", only.untagged)
	}
	for _, bad := range []string{
		"SETQUOTA ROOT ()",
		"SETQUOTA ROOT (STORAGE nope)",
		"SETQUOTA ROOT (ANNOTATION-STORAGE 1)",
		"SETQUOTA ROOT (STORAGE 4 MESSAGE 5 EXTRA)",
		"SETQUOTA",
	} {
		before := cl.cmd("GETQUOTA ROOT").mustOK(t).untagged[0]
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was not BAD", bad)
		}
		if got := cl.cmd("GETQUOTA ROOT").mustOK(t).untagged[0]; got != before {
			t.Fatalf("%s changed %q -> %q", bad, before, got)
		}
	}
	if rp := cl.cmd("SETQUOTA OTHER (STORAGE 1)"); rp.status != "NO" || len(rp.untagged) != 0 {
		t.Fatalf("other root %s %q", rp.status, rp.untagged)
	}
	if got := cl.cmd("GETQUOTA ROOT").mustOK(t).untagged[0]; got != wantQuota(2, 9, true, 2, 12) {
		t.Fatalf("other root changed %q", got)
	}
}

func TestQuotaWithoutMessage(t *testing.T) {
	dir := emptyQuotaDataset(t)
	pers := quotaPers(t, "QUOTA", "QUOTA=RES-STORAGE", "QUOTASET")
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: pers, Dataset: dir})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	line := wantQuota(0, 1048576, false, 0, 0)
	if got := cl.cmd("GETQUOTA ROOT").mustOK(t).untagged[0]; got != line || strings.Contains(got, "MESSAGE") {
		t.Fatalf("no message resource %q", got)
	}
	before := cl.cmd("GETQUOTA ROOT").mustOK(t).untagged[0]
	for _, bad := range []string{"SETQUOTA ROOT (MESSAGE 4)", "SETQUOTA ROOT (STORAGE 3 MESSAGE 4)"} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was not BAD", bad)
		}
		if got := cl.cmd("GETQUOTA ROOT").mustOK(t).untagged[0]; got != before {
			t.Fatalf("%s changed the limit", bad)
		}
	}
	set := cl.cmd("SETQUOTA ROOT (STORAGE 3)").mustOK(t)
	if set.untagged[0] != wantQuota(0, 3, false, 0, 0) {
		t.Fatalf("set storage %q", set.untagged)
	}
}

func TestQuotaCapabilityRequired(t *testing.T) {
	dir := emptyQuotaDataset(t)
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: quotaPers(t), Dataset: dir})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	for _, cmd := range []string{"GETQUOTA ROOT", "GETQUOTAROOT INBOX", "SETQUOTA ROOT (STORAGE 1)"} {
		if cl.cmd(cmd).status != "BAD" {
			t.Fatalf("%s without the token was not BAD", cmd)
		}
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", Personality: quotaPers(t, "QUOTA"), Dataset: dir})
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	if cl.cmd("GETQUOTA ROOT").status != "OK" {
		t.Fatal("GETQUOTA needs QUOTASET")
	}
	if cl.cmd("SETQUOTA ROOT (STORAGE 1)").status != "BAD" {
		t.Fatal("SETQUOTA without QUOTASET was not BAD")
	}
}

func TestQuotaSharedLimits(t *testing.T) {
	dir := emptyQuotaDataset(t)
	pers := quotaPers(t, "QUOTA", "QUOTA=RES-MESSAGE", "QUOTASET")
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", Personality: pers, Dataset: dir})
	one := dial(t, bound.Listen)
	one.cmd("LOGIN demo secret").mustOK(t)
	one.cmd("APPEND INBOX {1+}\r\nZ").mustOK(t)
	set := one.cmd("SETQUOTA ROOT (STORAGE 40 MESSAGE 7)").mustOK(t)
	want := wantQuota(1, 40, true, 1, 7)
	if set.untagged[0] != want {
		t.Fatalf("set %q", set.untagged)
	}
	two := dial(t, bound.Listen)
	two.cmd("LOGIN demo secret").mustOK(t)
	if got := two.cmd("GETQUOTA ROOT").mustOK(t).untagged[0]; got != want {
		t.Fatalf("second connection %q", got)
	}
	one.cmd("LOGOUT").ok(t)
	two.cmd("LOGOUT").ok(t)
	cc.call(map[string]any{"op": "reset"}).ok(t)
	again := dial(t, bound.Listen)
	again.cmd("LOGIN demo secret").mustOK(t)
	fresh := wantQuota(0, 1048576, true, 0, 1000000)
	if got := again.cmd("GETQUOTA ROOT").mustOK(t).untagged[0]; got != fresh {
		t.Fatalf("after reset %q", got)
	}
	again.cmd("SETQUOTA ROOT (STORAGE 2 MESSAGE 3)").mustOK(t)
	again.cmd("LOGOUT").ok(t)
	cc.call(map[string]any{"op": "set-dataset", "path": dir}).ok(t)
	back := dial(t, bound.Listen)
	back.cmd("LOGIN demo secret").mustOK(t)
	if got := back.cmd("GETQUOTA ROOT").mustOK(t).untagged[0]; got != fresh {
		t.Fatalf("after set-dataset %q", got)
	}
}

func TestQuotaLists(t *testing.T) {
	type place struct {
		file  string
		lists []string // greeting, pre, post; empty means absent
		toks  []string
		prev  []string
		next  []string
	}
	four := []string{"QUOTA", "QUOTA=RES-STORAGE", "QUOTA=RES-MESSAGE", "QUOTASET"}
	one := []string{"QUOTA"}
	two := []string{"QUOTA", "QUOTA=RES-STORAGE"}
	cases := []place{
		{"personalities/cyrus-22.json", []string{"", "pre", "post"}, one, []string{"", "IMAP4rev1", "IMAP4rev1"}, []string{"", "LITERAL+", "LITERAL+"}},
		{"personalities/cyrus-24.json", []string{"", "pre", "post"}, one, []string{"", "ENABLE", "ENABLE"}, []string{"", "NAMESPACE", "NAMESPACE"}},
		{"personalities/cyrus-25.json", []string{"", "pre", "post"}, one, []string{"", "ENABLE", "ENABLE"}, []string{"", "NAMESPACE", "NAMESPACE"}},
		{"personalities/cyrus-36.json", []string{"", "pre", "post"}, one, []string{"", "QRESYNC", "QRESYNC"}, []string{"", "SAVEDATE", "SAVEDATE"}},
		{"personalities/cyrus-3124.json", []string{"", "pre", "post"}, four, []string{"", "QRESYNC", "QRESYNC"}, []string{"", "SASL-IR", "SAVEDATE"}},
		{"personalities/cyrus-3137.json", []string{"", "pre", "post"}, four, []string{"", "QRESYNC", "QRESYNC"}, []string{"", "SASL-IR", "SAVEDATE"}},
		{"personalities/sasl.json", []string{"", "", "post"}, four, []string{"", "", "QRESYNC"}, []string{"", "", "SAVEDATE"}},
		{"personalities/courier-50.json", []string{"greet", "pre", "post"}, one, []string{"SORT", "SORT", "SORT"}, []string{"IDLE", "IDLE", "IDLE"}},
		{"personalities/courier-606.json", []string{"greet", "pre", "post"}, one, []string{"SORT", "SORT", "SORT"}, []string{"IDLE", "IDLE", "IDLE"}},
		{"personalities/stalwart.json", []string{"", "", "post"}, two, []string{"", "", "PREVIEW"}, []string{"", "", "STARTTLS"}},
		{"personalities/stalwart-01625.json", []string{"", "", "post"}, two, []string{"", "", "PREVIEW"}, []string{"", "", "STARTTLS"}},
		{"personalities/stalwart-dev.json", []string{"", "", "post"}, two, []string{"", "", "PREVIEW"}, []string{"", "", "STARTTLS"}},
		{"personalities/stalwart-0155.json", []string{"", "", "post"}, two, []string{"", "", "PREVIEW"}, []string{"", "", "STARTTLS"}},
		{"personalities/zimbra-883.json", []string{"", "pre", "post"}, one, []string{"", "QRESYNC", "QRESYNC"}, []string{"", "SASL-IR", "SASL-IR"}},
	}
	allow := map[string]bool{}
	for _, c := range cases {
		allow[filepath.Base(c.file)] = true
		p, err := loadPersonality(c.file)
		if err != nil {
			t.Fatal(err)
		}
		lists := [][]string{p.GreetingCaps, p.PreLogin, p.PostLogin}
		names := []string{"greeting", "pre-login", "post-login"}
		for _, tok := range []string{"QUOTA", "QUOTA=RES-STORAGE", "QUOTA=RES-MESSAGE", "QUOTASET", "QUOTA=RES-ANNOTATION-STORAGE", "QUOTA=RES-MAILBOX"} {
			seen := listed(p.GreetingCaps, tok) || listed(p.PreLogin, tok) || listed(p.PostLogin, tok)
			if seen != containsStr(c.toks, tok) {
				t.Fatalf("%s token %s present=%v", c.file, tok, seen)
			}
		}
		for i, list := range lists {
			if c.lists[i] == "" {
				for _, tok := range []string{"QUOTA", "QUOTA=RES-STORAGE", "QUOTA=RES-MESSAGE", "QUOTASET", "QUOTA=RES-ANNOTATION-STORAGE", "QUOTA=RES-MAILBOX"} {
					if listed(list, tok) {
						t.Fatalf("%s %s lists %s", c.file, names[i], tok)
					}
				}
				continue
			}
			at := capAt(list, c.prev[i])
			for n, tok := range c.toks {
				if capAt(list, tok) != at+1+n {
					t.Fatalf("%s %s %s not at %d after %s", c.file, names[i], tok, n, c.prev[i])
				}
			}
			if capAt(list, c.next[i]) != at+1+len(c.toks) {
				t.Fatalf("%s %s next is not %s", c.file, names[i], c.next[i])
			}
		}
	}
	cyrus, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	sasl, err := loadPersonality("personalities/sasl.json")
	if err != nil {
		t.Fatal(err)
	}
	if !sameCapSet(dropCap(cyrus.PostLogin, "SEARCH=FUZZY"), sasl.PostLogin) {
		t.Fatal("sasl post-login set drifted from cyrus-3137")
	}
	matches, err := filepath.Glob("personalities/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range matches {
		base := filepath.Base(f)
		if strings.HasSuffix(base, ".gaps.json") || allow[base] {
			continue
		}
		p, err := loadPersonality(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, tok := range []string{"QUOTA", "QUOTA=RES-STORAGE", "QUOTA=RES-MESSAGE", "QUOTASET", "QUOTA=RES-ANNOTATION-STORAGE", "QUOTA=RES-MAILBOX"} {
			if listed(p.GreetingCaps, tok) || listed(p.PreLogin, tok) || listed(p.PostLogin, tok) {
				t.Fatalf("%s gained %s", base, tok)
			}
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	text := string(gaps)
	for _, tok := range []string{"QUOTA", "QUOTA=RES-STORAGE", "QUOTA=RES-MESSAGE", "QUOTASET"} {
		if strings.Contains(text, `"`+tok+`"`) {
			t.Fatalf("gaps still lists %s", tok)
		}
	}
	for _, tok := range []string{"QUOTA=RES-ANNOTATION-STORAGE", "QUOTA=RES-MAILBOX", "ACL", "IMAP4rev2"} {
		if !strings.Contains(text, `"`+tok+`"`) {
			t.Fatalf("gaps dropped %s", tok)
		}
	}
}
