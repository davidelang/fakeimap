package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wantNotImpl(t *testing.T, cl *imapClient, line string) {
	t.Helper()
	rp := cl.cmd(line)
	if rp.status != "NO" || rp.text != "not implemented" || len(rp.untagged) != 0 || strings.Contains(rp.tagLine, "[") || strings.Contains(rp.tagLine, "{") {
		t.Fatalf("%q -> %s %q untagged %q", line, rp.status, rp.text, rp.untagged)
	}
}

func wantBadNotImpl(t *testing.T, cl *imapClient, line string) {
	t.Helper()
	rp := cl.cmd(line)
	if rp.status != "BAD" || strings.Contains(rp.text, "not implemented") || len(rp.untagged) != 0 || strings.Contains(rp.tagLine, "{") {
		t.Fatalf("%q -> %s %q untagged %q", line, rp.status, rp.text, rp.untagged)
	}
}

func TestI18NURLAUTH(t *testing.T) {
	if !capImplemented("URLAUTH") || !capImplemented("URLAUTH=BINARY") || capImplemented("I18NLEVEL=1") || capImplemented("URL-PARTIAL") {
		t.Fatal("URLAUTH capability map")
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gaps), `"URL-PARTIAL"`) || !strings.Contains(string(gaps), `"I18NLEVEL=1"`) && !strings.Contains(string(gaps), "I18NLEVEL") {
		if !strings.Contains(string(gaps), `"URL-PARTIAL"`) {
			t.Fatal("gaps dropped URL-PARTIAL")
		}
	}
	if !strings.Contains(string(gaps), `"URL-PARTIAL"`) {
		t.Fatal("gaps dropped URL-PARTIAL")
	}
	for _, tok := range []string{"URLAUTH", "URLAUTH=BINARY"} {
		if strings.Contains(string(gaps), `"`+tok+`"`) {
			t.Fatalf("gaps still lists %s", tok)
		}
	}
	urlPost := map[string][2]string{
		"cyrus-24.json":   {"XLIST", "URLAUTH=BINARY"},
		"cyrus-25.json":   {"CREATE-SPECIAL-USE", "URLAUTH=BINARY"},
		"cyrus-36.json":   {"UNSELECT", "URLAUTH=BINARY"},
		"cyrus-3124.json": {"UNSELECT", "URLAUTH=BINARY"},
		"cyrus-3137.json": {"UNSELECT", "URLAUTH=BINARY"},
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), "gaps") {
			continue
		}
		path := filepath.Join("personalities", e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "I18NLEVEL=1") || strings.Contains(string(raw), "URL-PARTIAL") {
			t.Fatalf("%s gained a dropped token", path)
		}
		p, err := loadPersonality(path)
		if err != nil {
			t.Fatal(err)
		}
		if p.listsCap("I18NLEVEL=1") || p.listsCap("URL-PARTIAL") {
			t.Fatalf("%s lists a filtered token", path)
		}
		neighbors, want := urlPost[e.Name()]
		has := listed(p.PostLogin, "URLAUTH") && listed(p.PostLogin, "URLAUTH=BINARY")
		if listed(p.GreetingCaps, "URLAUTH") || listed(p.PreLogin, "URLAUTH") || listed(p.GreetingCaps, "URLAUTH=BINARY") || listed(p.PreLogin, "URLAUTH=BINARY") || has != want {
			t.Fatalf("%s URLAUTH lists", path)
		}
		if want && (!betweenCaps(p.PostLogin, "URLAUTH", neighbors[0], "URLAUTH=BINARY") || capNext(p.PostLogin, "URLAUTH=BINARY") != neighbors[1] && neighbors[1] != "URLAUTH=BINARY") {
			if !betweenCaps(p.PostLogin, "URLAUTH", neighbors[0], "URLAUTH=BINARY") {
				t.Fatalf("%s URLAUTH neighbors %v", path, p.PostLogin)
			}
		}
	}

	base, err := loadPersonality("personalities/cyrus-22.json")
	if err != nil {
		t.Fatal(err)
	}
	src := append(append([]string{}, base.PostLogin...), "I18NLEVEL=1", "URL-PARTIAL", "URLAUTH")
	got := base.advertiseList(src, true, false, false, false, nil)
	if listed(got, "I18NLEVEL=1") || listed(got, "URL-PARTIAL") || !listed(got, "URLAUTH") {
		t.Fatalf("advertiseList %v", got)
	}

	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	early := dial(t, bound.Listen)
	if listed(capsOf(early.greeting), "URLAUTH") || listed(capsOf(early.cmd("CAPABILITY").mustOK(t).untagged[0]), "URLAUTH") {
		t.Fatal("URLAUTH before login")
	}
	rump := "imap://demo@localhost/INBOX/;UID=1"
	for _, line := range []string{
		"COMPARATOR i;octet",
		"GENURLAUTH " + rump + " INTERNAL",
		"GENURLAUTH " + rump + " ANONYMOUS",
		"RESETKEY",
	} {
		wantAuthFirst(t, early, line)
	}
	unknown := `URLFETCH "imap://demo@localhost/INBOX/;UID=1;URLAUTH=submit+demo"`
	earlyFetch := early.cmd(unknown).mustOK(t)
	if len(earlyFetch.untagged) != 1 || !strings.Contains(earlyFetch.untagged[0], " NIL") {
		t.Fatalf("URLFETCH before login %q", earlyFetch.untagged)
	}
	wantBadNotImpl(t, early, `URLFETCH "imap://demo@localhost/INBOX/;UID=1"`)

	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	post := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	if !listed(post, "URLAUTH") || !listed(post, "URLAUTH=BINARY") || listed(post, "I18NLEVEL=1") || listed(post, "URL-PARTIAL") {
		t.Fatalf("post %v", post)
	}
	cl.cmd("SELECT INBOX").mustOK(t)
	gen := cl.cmd("GENURLAUTH " + rump + " ANONYMOUS").mustOK(t)
	if len(gen.untagged) != 1 || !strings.Contains(gen.untagged[0], "* GENURLAUTH ") || !strings.Contains(strings.ToLower(gen.untagged[0]), ";urlauth=anonymous:") {
		t.Fatalf("GENURLAUTH %q", gen.untagged)
	}
	url := strings.Trim(gen.untagged[0][len("* GENURLAUTH "):], `"`)
	fetched := cl.cmd("URLFETCH \"" + url + "\"").mustOK(t)
	if len(fetched.untagged) != 1 || strings.HasSuffix(strings.TrimSpace(fetched.untagged[0]), " NIL") {
		t.Fatalf("URLFETCH %q", fetched.untagged)
	}
	missing := cl.cmd(`URLFETCH "imap://h/box/;UID=7;URLAUTH=user"`).mustOK(t)
	if len(missing.untagged) != 1 || !strings.Contains(missing.untagged[0], " NIL") {
		t.Fatalf("unknown URL %q", missing.untagged)
	}
	cl.cmd("RESETKEY").mustOK(t)
	afterReset := cl.cmd("URLFETCH \"" + url + "\"").mustOK(t)
	if len(afterReset.untagged) != 1 || !strings.Contains(afterReset.untagged[0], " NIL") {
		t.Fatalf("after RESETKEY %q", afterReset.untagged)
	}
	wantNotImpl(t, cl, "COMPARATOR i;octet")
	for _, line := range []string{
		"GENURLAUTH",
		"GENURLAUTH " + rump,
		`GENURLAUTH "` + rump + `" "INTERNAL"`,
		`GENURLAUTH "nope" INTERNAL`,
		"URLFETCH",
		"RESETKEY INBOX extra",
	} {
		wantBadNotImpl(t, cl, line)
	}
	uid := dial(t, bound.Listen)
	uid.cmd("LOGIN demo secret").mustOK(t)
	rp := uid.cmd("UID GENURLAUTH " + rump + " INTERNAL")
	if rp.status != "BAD" || rp.text != "syntax error" {
		t.Fatalf("UID GENURLAUTH %s %q", rp.status, rp.text)
	}

	facts := string(mustRead(t, "docs/facts/imap-server.md"))
	for _, phrase := range []string{
		"COMPARATOR",
		"GENURLAUTH",
		"URLFETCH",
		"RESETKEY",
		"URLFETCH` does not require login",
		"`NO` `not implemented`",
		"must authenticate first",
		"Invalid arguments are tagged `BAD`",
		"I18NLEVEL=1",
		"URLAUTH=BINARY",
		"advertiseList",
		"/;UID=",
		";URLAUTH=",
		"URL-PARTIAL` stays unimplemented",
		"cyrus-3137.gaps.json",
	} {
		if !strings.Contains(facts, phrase) {
			t.Fatalf("facts missing %q", phrase)
		}
	}
	guide := string(mustRead(t, "docs/guide.md"))
	off := strings.Index(guide, "## Capabilities that stay off")
	if off < 0 {
		t.Fatal("guide missing stay-off section")
	}
	stay := guide[off:]
	if !strings.Contains(stay, "I18NLEVEL=1") || !strings.Contains(stay, "URL-PARTIAL") {
		t.Fatal("stay-off dropped a filtered token")
	}
	if !strings.Contains(guide[:off], "`COMPARATOR`") || !strings.Contains(guide[:off], "`GENURLAUTH`") || !strings.Contains(guide[:off], "`URLFETCH`") || !strings.Contains(guide[:off], "`RESETKEY`") || !strings.Contains(guide[:off], "not implemented") || !strings.Contains(guide[:off], "docs/facts/imap-server.md") {
		t.Fatal("guide missing the short URLAUTH note")
	}
	if strings.Contains(guide, "/;UID=") || strings.Contains(guide, "url-rump") || strings.Contains(guide, ";URLAUTH=") {
		t.Fatal("guide pastes the facts file")
	}
}
