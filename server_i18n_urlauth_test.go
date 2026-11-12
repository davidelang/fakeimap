package main

import (
	"encoding/json"
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
	upper := strings.ToUpper(strings.Join(rp.untagged, " ") + " " + rp.tagLine)
	for _, word := range []string{"COMPARATOR", "GENURLAUTH", "URLFETCH", "RESETKEY"} {
		if strings.Contains(upper, word) {
			t.Fatalf("%q leaked %s in %s", line, word, rp.tagLine)
		}
	}
}

func wantBadNotImpl(t *testing.T, cl *imapClient, line string) {
	t.Helper()
	rp := cl.cmd(line)
	if rp.status != "BAD" || strings.Contains(rp.text, "not implemented") || len(rp.untagged) != 0 || strings.Contains(rp.tagLine, "{") {
		t.Fatalf("%q -> %s %q untagged %q", line, rp.status, rp.text, rp.untagged)
	}
}

func assertNoURLAuthTokens(t *testing.T, caps []string, where string) {
	t.Helper()
	for _, tok := range []string{"I18NLEVEL=1", "URLAUTH", "URLAUTH=BINARY", "URL-PARTIAL"} {
		if listed(caps, tok) {
			t.Fatalf("%s lists %s: %q", where, tok, caps)
		}
	}
}

func TestI18NURLAUTHStub(t *testing.T) {
	for _, name := range []string{"I18NLEVEL=1", "URLAUTH", "URLAUTH=BINARY", "URL-PARTIAL"} {
		if _, ok := implementedCaps[strings.ToUpper(name)]; ok || capImplemented(name) {
			t.Fatalf("%s is implemented", name)
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"URL-PARTIAL", "URLAUTH", "URLAUTH=BINARY"} {
		if !strings.Contains(string(gaps), `"`+tok+`"`) {
			t.Fatalf("gaps dropped %s", tok)
		}
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join("personalities", e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() == "cyrus-3137.gaps.json" {
			continue
		}
		for _, tok := range []string{"I18NLEVEL=1", "URLAUTH", "URLAUTH=BINARY"} {
			if strings.Contains(string(raw), tok) {
				t.Fatalf("%s gained %s", path, tok)
			}
		}
		p, err := loadPersonality(path)
		if err != nil {
			t.Fatal(err)
		}
		if p.listsCap("I18NLEVEL=1") || p.listsCap("URLAUTH") || p.listsCap("URLAUTH=BINARY") || p.listsCap("URL-PARTIAL") {
			t.Fatalf("%s lists a filtered token", path)
		}
		files = append(files, path)
	}
	if len(files) < 2 {
		t.Fatal("expected personality files")
	}

	facts, err := os.ReadFile("docs/facts/imap-server.md")
	if err != nil {
		t.Fatal(err)
	}
	factText := string(facts)
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
		if !strings.Contains(factText, phrase) {
			t.Fatalf("facts missing %q", phrase)
		}
	}
	guideRaw, err := os.ReadFile("docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := string(guideRaw)
	off := strings.Index(guide, "## Capabilities that stay off")
	if off < 0 {
		t.Fatal("guide missing stay-off section")
	}
	stay := guide[off:]
	if !strings.Contains(stay, "URLAUTH") || !strings.Contains(stay, "URLAUTH=BINARY") || !strings.Contains(stay, "URL-PARTIAL") {
		t.Fatal("stay-off dropped URLAUTH")
	}
	if !strings.Contains(guide[:off], "`COMPARATOR`") || !strings.Contains(guide[:off], "`GENURLAUTH`") || !strings.Contains(guide[:off], "`URLFETCH`") || !strings.Contains(guide[:off], "`RESETKEY`") || !strings.Contains(guide[:off], "not implemented") || !strings.Contains(guide[:off], "docs/facts/imap-server.md") {
		t.Fatal("guide missing the short URLAUTH note")
	}
	if strings.Contains(guide, "/;UID=") || strings.Contains(guide, "url-rump") || strings.Contains(guide, ";URLAUTH=") {
		t.Fatal("guide pastes the facts file")
	}

	for _, name := range []string{"I18NLEVEL=1", "URLAUTH", "URLAUTH=BINARY"} {
		implementedCaps[strings.ToUpper(name)] = struct{}{}
	}
	t.Cleanup(func() {
		for _, name := range []string{"I18NLEVEL=1", "URLAUTH", "URLAUTH=BINARY"} {
			delete(implementedCaps, strings.ToUpper(name))
		}
	})
	if !capImplemented("URLAUTH") || capImplemented("URL-PARTIAL") {
		t.Fatal("capability map setup")
	}
	base, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	greetSrc := append([]string{}, base.GreetingCaps...)
	preSrc := append([]string{}, base.PreLogin...)
	postSrc := append([]string{}, base.PostLogin...)
	wantGreet := strings.Join(base.advertiseList(greetSrc, false, false, false, false, nil), " ")
	wantPre := strings.Join(base.advertiseList(preSrc, false, false, false, false, nil), " ")
	wantPost := strings.Join(base.advertiseList(postSrc, true, false, false, false, nil), " ")
	base.GreetingCaps = append(append([]string{}, greetSrc...), "I18NLEVEL=1", "URLAUTH", "URLAUTH=BINARY", "URL-PARTIAL")
	base.PreLogin = append(append([]string{}, preSrc...), "i18nlevel=1", "urlauth", "URLAUTH=BINARY")
	base.PostLogin = append(append([]string{}, postSrc...), "I18NLEVEL=1", "URLAUTH", "Urlauth=Binary", "URL-PARTIAL")
	for _, spec := range []struct {
		src       []string
		postLogin bool
		want      string
	}{
		{base.GreetingCaps, false, wantGreet},
		{base.PreLogin, false, wantPre},
		{base.PostLogin, true, wantPost},
	} {
		got := base.advertiseList(spec.src, spec.postLogin, false, false, false, nil)
		assertNoURLAuthTokens(t, got, "advertiseList")
		if strings.Join(got, " ") != spec.want {
			t.Fatalf("advertiseList %v -> %v want %s", spec.src, got, spec.want)
		}
	}
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, filtered := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	fcl := dial(t, filtered.Listen)
	assertNoURLAuthTokens(t, capsOf(fcl.greeting), "greeting")
	if strings.Join(capsOf(fcl.greeting), " ") != wantGreet {
		t.Fatalf("greeting %q", fcl.greeting)
	}
	pre := fcl.cmd("CAPABILITY").mustOK(t)
	if len(pre.untagged) != 1 {
		t.Fatalf("pre-login %q", pre.untagged)
	}
	assertNoURLAuthTokens(t, capsOf(pre.untagged[0]), "pre-login")
	if strings.Join(capsOf(pre.untagged[0]), " ") != wantPre {
		t.Fatalf("pre-login %q", pre.untagged[0])
	}
	login := fcl.cmd("LOGIN demo secret").mustOK(t)
	assertNoURLAuthTokens(t, capsOf(login.tagLine), "login")
	post := fcl.cmd("CAPABILITY").mustOK(t)
	if len(post.untagged) != 1 {
		t.Fatalf("post-login %q", post.untagged)
	}
	assertNoURLAuthTokens(t, capsOf(post.untagged[0]), "post-login")
	if strings.Join(capsOf(post.untagged[0]), " ") != wantPost {
		t.Fatalf("post-login %q", post.untagged[0])
	}
	for _, line := range []string{"ENABLE I18NLEVEL=1", "ENABLE URLAUTH", "ENABLE URLAUTH=BINARY"} {
		rp := fcl.cmd(line).mustOK(t)
		joined := strings.ToUpper(strings.Join(rp.untagged, " "))
		if len(rp.untagged) != 0 || strings.Contains(joined, "I18NLEVEL") || strings.Contains(joined, "URLAUTH") {
			t.Fatalf("%s %q %q", line, rp.tagLine, rp.untagged)
		}
	}
	mixed := fcl.cmd("ENABLE I18NLEVEL=1 URLAUTH CONDSTORE").mustOK(t)
	joined := strings.ToUpper(strings.Join(mixed.untagged, " "))
	if len(mixed.untagged) != 1 || joined != "* ENABLED CONDSTORE" || strings.Contains(joined, "I18NLEVEL") || strings.Contains(joined, "URLAUTH") {
		t.Fatalf("mixed ENABLE %q", mixed.untagged)
	}

	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	early := dial(t, bound.Listen)
	assertNoURLAuthTokens(t, capsOf(early.greeting), "cyrus greeting")
	assertNoURLAuthTokens(t, capsOf(early.cmd("CAPABILITY").mustOK(t).untagged[0]), "cyrus pre")
	for _, line := range []string{
		"COMPARATOR",
		"COMPARATOR ()",
		"COMPARATOR i;octet",
		"GENURLAUTH",
		`GENURLAUTH "imap://h/box/;UID=1" ANONYMOUS`,
		"RESETKEY",
		"RESETKEY INBOX extra",
	} {
		wantAuthFirst(t, early, line)
	}
	wantNotImpl(t, early, `URLFETCH "imap://demo@localhost/INBOX/;UID=1;URLAUTH=submit+demo"`)
	wantNotImpl(t, early, "urlfetch imap://h/box/;UID=7;URLAUTH=user")
	wantBadNotImpl(t, early, `URLFETCH "imap://demo@localhost/INBOX/;UID=1"`)
	wantBadNotImpl(t, early, "URLFETCH")
	early.cmd("LOGIN demo secret").mustOK(t)
	assertNoURLAuthTokens(t, capsOf(early.cmd("CAPABILITY").mustOK(t).untagged[0]), "cyrus post")

	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("SELECT INBOX").mustOK(t)
	searchBefore := append([]string(nil), cl.cmd("SEARCH ALL").mustOK(t).untagged...)
	sortBefore := append([]string(nil), cl.cmd("SORT (ARRIVAL) UTF-8 ALL").mustOK(t).untagged...)
	flagsBefore := append([]string(nil), cl.cmd("FETCH 1:* (UID FLAGS)").mustOK(t).untagged...)
	statusBefore := append([]string(nil), cl.cmd("STATUS INBOX (MESSAGES UIDNEXT UNSEEN)").mustOK(t).untagged...)
	listBefore := append([]string(nil), cl.cmd(`LIST "" "*"`).mustOK(t).untagged...)

	rump := "imap://demo@localhost/INBOX/;UID=1"
	rump2 := "imap://demo@localhost/Sent/;UID=42;UIDVALIDITY=9"
	fetch1 := rump + ";URLAUTH=submit+demo"
	fetch2 := "imap://h/box/;UID=7;URLAUTH=user"
	for _, line := range []string{
		"COMPARATOR i;octet",
		`COMPARATOR "i;unicode-casemap"`,
		"COMPARATOR (i;octet \"i;ascii-casemap\")",
		"COMPARATOR (" + imapLit("i;octet") + ")",
		"comparator (i;octet)",
		"GENURLAUTH " + rump + " INTERNAL",
		"GENURLAUTH " + imapLit(rump) + " INTERNAL",
		`GENURLAUTH "` + rump + `" INTERNAL "` + rump2 + `" internal`,
		"URLFETCH " + fetch1,
		"URLFETCH " + imapLit(fetch1) + " " + fetch2,
		"RESETKEY",
		"RESETKEY NoSuchURLBox",
		`RESETKEY "No Such Box"`,
		"RESETKEY " + imapLit("MissingLitBox"),
	} {
		wantNotImpl(t, cl, line)
	}
	for _, line := range []string{
		"COMPARATOR",
		"COMPARATOR ()",
		"COMPARATOR (",
		"COMPARATOR (i;octet",
		"COMPARATOR i;octet i;ascii-casemap",
		"COMPARATOR ((i;octet))",
		`COMPARATOR ("i;octet" (b))`,
		"GENURLAUTH",
		"GENURLAUTH " + rump,
		"GENURLAUTH " + rump + " ANONYMOUS",
		`GENURLAUTH "` + rump + `" "INTERNAL"`,
		`GENURLAUTH "nope" INTERNAL`,
		`GENURLAUTH "imap://h/box/;UID=0" INTERNAL`,
		`GENURLAUTH "imap://h/box" INTERNAL`,
		`GENURLAUTH "IMAP://h/box/;UID=1" INTERNAL`,
		`GENURLAUTH "imap://h/box/;uid=1" INTERNAL`,
		"URLFETCH",
		`URLFETCH "imap://h/box/;UID=1"`,
		`URLFETCH "` + fetch1 + `" "http://h/;UID=1;URLAUTH=a"`,
		`URLFETCH "imap://h/b/;UID=0;URLAUTH=a"`,
		`URLFETCH "imap://h/b/;UID=1;URLAUTH="`,
		`URLFETCH "imap://h/b/;UID=1;URLAUTH=]"`,
		"RESETKEY INBOX extra",
		"RESETKEY (INBOX)",
	} {
		wantBadNotImpl(t, cl, line)
	}
	searchAfter := cl.cmd("SEARCH ALL").mustOK(t).untagged
	sortAfter := cl.cmd("SORT (ARRIVAL) UTF-8 ALL").mustOK(t).untagged
	flagsAfter := cl.cmd("FETCH 1:* (UID FLAGS)").mustOK(t).untagged
	statusAfter := cl.cmd("STATUS INBOX (MESSAGES UIDNEXT UNSEEN)").mustOK(t).untagged
	listAfter := cl.cmd(`LIST "" "*"`).mustOK(t).untagged
	if strings.Join(searchAfter, "\n") != strings.Join(searchBefore, "\n") {
		t.Fatalf("SEARCH changed %q -> %q", searchBefore, searchAfter)
	}
	if strings.Join(sortAfter, "\n") != strings.Join(sortBefore, "\n") {
		t.Fatalf("SORT changed %q -> %q", sortBefore, sortAfter)
	}
	if strings.Join(flagsAfter, "\n") != strings.Join(flagsBefore, "\n") {
		t.Fatalf("flags changed %q -> %q", flagsBefore, flagsAfter)
	}
	if strings.Join(statusAfter, "\n") != strings.Join(statusBefore, "\n") {
		t.Fatalf("status changed %q -> %q", statusBefore, statusAfter)
	}
	if strings.Join(listAfter, "\n") != strings.Join(listBefore, "\n") {
		t.Fatalf("mailboxes changed %q -> %q", listBefore, listAfter)
	}
	for _, ln := range listAfter {
		if strings.Contains(ln, "NoSuchURLBox") || strings.Contains(ln, "No Such Box") || strings.Contains(ln, "MissingLitBox") {
			t.Fatalf("lookup created %s", ln)
		}
	}

	for _, line := range []string{
		"UID COMPARATOR i;octet",
		"UID GENURLAUTH " + rump + " INTERNAL",
		"UID URLFETCH " + fetch1,
		"UID RESETKEY",
	} {
		uid := dial(t, bound.Listen)
		uid.cmd("LOGIN demo secret").mustOK(t)
		rp := uid.cmd(line)
		if rp.status != "BAD" || rp.text != "syntax error" || strings.Contains(rp.text, "not implemented") {
			t.Fatalf("%s -> %s %q", line, rp.status, rp.text)
		}
	}

	for _, path := range files {
		_, one := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
		c := dial(t, one.Listen)
		assertNoURLAuthTokens(t, capsOf(c.greeting), path+" greeting")
		before := c.cmd("CAPABILITY").mustOK(t)
		if len(before.untagged) != 1 {
			t.Fatalf("%s pre %q", path, before.untagged)
		}
		assertNoURLAuthTokens(t, capsOf(before.untagged[0]), path+" pre")
		wantAuthFirst(t, c, "COMPARATOR i;octet")
		wantAuthFirst(t, c, "GENURLAUTH "+rump+" INTERNAL")
		wantAuthFirst(t, c, "RESETKEY")
		wantNotImpl(t, c, "URLFETCH "+fetch1)
		c.cmd("LOGIN demo secret").mustOK(t)
		after := c.cmd("CAPABILITY").mustOK(t)
		if len(after.untagged) != 1 {
			t.Fatalf("%s post %q", path, after.untagged)
		}
		assertNoURLAuthTokens(t, capsOf(after.untagged[0]), path+" post")
		wantNotImpl(t, c, "COMPARATOR (i;octet)")
		wantNotImpl(t, c, "RESETKEY MissingBox")
	}
}
