package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSavedate(t *testing.T) {
	if !capImplemented("SAVEDATE") {
		t.Fatal("SAVEDATE is not implemented")
	}
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	cc.call(map[string]any{"op": "freeze-clock", "time": "2020-01-02T03:04:05Z"}).ok(t)
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "SAVEDATE") {
		t.Fatalf("greeting lists SAVEDATE: %q", cl.greeting)
	}
	cl.cmd("LOGIN demo secret").ok(t)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "SAVEDATE") {
		t.Fatal("post-login missing SAVEDATE")
	}

	cl.cmd("SELECT INBOX").ok(t)
	over := fetchLine(t, cl.cmd("FETCH 1 (INTERNALDATE SAVEDATE)").mustOK(t))
	if got, want := fetchQuoted(t, over, "INTERNALDATE"), "10-Nov-2025 12:00:00 +0000"; got != want {
		t.Fatalf("dataset internal %q", got)
	}
	if a, b := fetchQuoted(t, over, "INTERNALDATE"), fetchQuoted(t, over, "SAVEDATE"); a != b {
		t.Fatalf("dataset save %q internal %q", b, a)
	}
	flags := fetchLine(t, cl.cmd("FETCH 2 (FLAGS)").mustOK(t))
	cl.cmd("FETCH 2 (SAVEDATE)").mustOK(t)
	again := fetchLine(t, cl.cmd("FETCH 2 (FLAGS)").mustOK(t))
	if flags != again || strings.Contains(again, `\Seen`) {
		t.Fatalf("SAVEDATE changed flags %q -> %q", flags, again)
	}

	cl.cmd("SELECT INBOX/Generated").ok(t)
	gen1 := fetchLine(t, cl.cmd("FETCH 1 (INTERNALDATE SAVEDATE)").mustOK(t))
	if a, b := fetchQuoted(t, gen1, "INTERNALDATE"), fetchQuoted(t, gen1, "SAVEDATE"); a != b || a != "01-Jan-2020 00:00:00 +0000" {
		t.Fatalf("generated 1 internal %q save %q", a, b)
	}
	gen2 := fetchLine(t, cl.cmd("FETCH 2 (INTERNALDATE SAVEDATE)").mustOK(t))
	if a, b := fetchQuoted(t, gen2, "INTERNALDATE"), fetchQuoted(t, gen2, "SAVEDATE"); a != b || a != "02-Jan-2020 00:00:00 +0000" {
		t.Fatalf("generated 2 internal %q save %q", a, b)
	}

	body := "hello"
	clientDate := "01-Jan-1990 00:00:00 +0000"
	firstSave := "02-Jan-2020 03:04:05 +0000"
	cl.cmd("CREATE Box").ok(t)
	cl.cmd("SELECT Box").ok(t)
	cl.cmd("APPEND Box \"" + clientDate + "\" {" + strconv.Itoa(len(body)) + "+}\r\n" + body).ok(t)
	held := fetchLine(t, cl.cmd("FETCH 1 (FLAGS)").mustOK(t))
	one := fetchLine(t, cl.cmd("FETCH 1 (SAVEDATE INTERNALDATE)").mustOK(t))
	if got := fetchQuoted(t, one, "INTERNALDATE"); got != clientDate {
		t.Fatalf("append internal %q", got)
	}
	if got := fetchQuoted(t, one, "SAVEDATE"); got != firstSave {
		t.Fatalf("append save %q", got)
	}
	heldAfter := fetchLine(t, cl.cmd("FETCH 1 (FLAGS)").mustOK(t))
	if held != heldAfter || strings.Contains(heldAfter, `\Seen`) {
		t.Fatalf("append SAVEDATE set \\Seen %q -> %q", held, heldAfter)
	}
	cl.cmd("APPEND Box {" + strconv.Itoa(len(body)) + "+}\r\n" + body).ok(t)
	two := fetchLine(t, cl.cmd("FETCH 2 (INTERNALDATE SAVEDATE)").mustOK(t))
	if a, b := fetchQuoted(t, two, "INTERNALDATE"), fetchQuoted(t, two, "SAVEDATE"); a != firstSave || b != firstSave {
		t.Fatalf("undated append internal %q save %q", a, b)
	}

	cc.call(map[string]any{"op": "freeze-clock", "time": "2020-01-03T04:05:06Z"}).ok(t)
	cl.cmd("COPY 1 Box").ok(t)
	if got := fetchQuoted(t, fetchLine(t, cl.cmd("FETCH 1 (SAVEDATE)").mustOK(t)), "SAVEDATE"); got != firstSave {
		t.Fatalf("source save changed %q", got)
	}
	cp := fetchLine(t, cl.cmd("FETCH 3 (INTERNALDATE SAVEDATE)").mustOK(t))
	if got := fetchQuoted(t, cp, "INTERNALDATE"); got != clientDate {
		t.Fatalf("copy internal %q", got)
	}
	if got := fetchQuoted(t, cp, "SAVEDATE"); got != "03-Jan-2020 04:05:06 +0000" {
		t.Fatalf("copy save %q", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH SAVEDON 02-Jan-2020")), " "); got != "1 2" {
		t.Fatalf("SAVEDON first day %q", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH SAVEDON 02-Jan-2020")), " "); got != "1 2" {
		t.Fatalf("UID SAVEDON first day %q", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH SAVEDON 03-Jan-2020")), " "); got != "3" {
		t.Fatalf("SAVEDON copy day %q", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH SAVEDBEFORE 03-Jan-2020")), " "); got != "1 2" {
		t.Fatalf("SAVEDBEFORE %q", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH SAVEDSINCE 03-Jan-2020")), " "); got != "3" {
		t.Fatalf("SAVEDSINCE %q", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH ON 01-Jan-1990")), " "); got != "1 3" {
		t.Fatalf("ON client date %q", got)
	}

	cc.call(map[string]any{"op": "freeze-clock", "time": "2020-01-04T05:06:07Z"}).ok(t)
	cl.cmd("CREATE Other").ok(t)
	cl.cmd("MOVE 3 Other").ok(t)
	cl.cmd("SELECT Other").ok(t)
	mv := fetchLine(t, cl.cmd("FETCH 1 (INTERNALDATE SAVEDATE)").mustOK(t))
	if got := fetchQuoted(t, mv, "INTERNALDATE"); got != clientDate {
		t.Fatalf("move internal %q", got)
	}
	if got := fetchQuoted(t, mv, "SAVEDATE"); got != "04-Jan-2020 05:06:07 +0000" {
		t.Fatalf("move save %q", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH SAVEDON 02-Jan-2020")); len(got) != 0 {
		t.Fatalf("move dest matched first day %v", got)
	}
	cl.cmd("SELECT Box").ok(t)
	if got := fetchQuoted(t, fetchLine(t, cl.cmd("FETCH 1 (SAVEDATE)").mustOK(t)), "SAVEDATE"); got != firstSave {
		t.Fatalf("source save after move %q", got)
	}

	for _, bad := range []string{
		"SEARCH SAVEDON",
		"SEARCH SAVEDBEFORE",
		"SEARCH SAVEDSINCE",
		"SEARCH SAVEDON not-a-date",
		"SEARCH SAVEDBEFORE 32-Jan-2020",
		"UID SEARCH SAVEDON",
		"UID SEARCH SAVEDSINCE",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
}

func TestSavedateWallClock(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE Wall").ok(t)
	cl.cmd("SELECT Wall").ok(t)
	body := "x"
	before := time.Now().Add(-2 * time.Second)
	cl.cmd("APPEND Wall \"01-Jan-1990 00:00:00 +0000\" {" + strconv.Itoa(len(body)) + "+}\r\n" + body).ok(t)
	after := time.Now().Add(2 * time.Second)
	one := fetchLine(t, cl.cmd("FETCH 1 (INTERNALDATE SAVEDATE)").mustOK(t))
	if got := fetchQuoted(t, one, "INTERNALDATE"); got != "01-Jan-1990 00:00:00 +0000" {
		t.Fatalf("wall internal %q", got)
	}
	saved := mustIMAPTime(t, fetchQuoted(t, one, "SAVEDATE"))
	if saved.Before(before) || saved.After(after) {
		t.Fatalf("wall save %s not in [%s, %s]", saved, before, after)
	}
	before = time.Now().Add(-2 * time.Second)
	cl.cmd("APPEND Wall {" + strconv.Itoa(len(body)) + "+}\r\n" + body).ok(t)
	after = time.Now().Add(2 * time.Second)
	two := fetchLine(t, cl.cmd("FETCH 2 (INTERNALDATE SAVEDATE)").mustOK(t))
	a, b := fetchQuoted(t, two, "INTERNALDATE"), fetchQuoted(t, two, "SAVEDATE")
	if a != b {
		t.Fatalf("undated wall internal %q save %q", a, b)
	}
	when := mustIMAPTime(t, a)
	if when.Before(before) || when.After(after) {
		t.Fatalf("undated wall %s not in [%s, %s]", when, before, after)
	}
}

func TestSavedateRequiresCap(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-22.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "SAVEDATE") {
		t.Fatal("cyrus-22 lists SAVEDATE")
	}
	for _, bad := range []string{
		"FETCH 1 (SAVEDATE)",
		"UID FETCH 1 (SAVEDATE)",
		"SEARCH SAVEDON 10-Nov-2025",
		"UID SEARCH SAVEDON 10-Nov-2025",
		"SEARCH SAVEDBEFORE 10-Nov-2025",
		"SEARCH SAVEDSINCE 10-Nov-2025",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH ON 10-Nov-2025")), " "); got != "1 2" {
		t.Fatalf("ON still works %q", got)
	}
}

func TestSavedateLists(t *testing.T) {
	if !capImplemented("SAVEDATE") {
		t.Fatal("SAVEDATE is not implemented")
	}
	allow := map[string]bool{}
	note := func(file string) *Personality {
		t.Helper()
		allow[filepath.Base(file)] = true
		p := mustPers(t, file)
		if listed(p.GreetingCaps, "SAVEDATE") {
			t.Fatalf("%s greeting lists SAVEDATE", file)
		}
		return p
	}
	after := func(file, where string, list []string, prev string, on bool) {
		t.Helper()
		i, j := capAt(list, "SAVEDATE"), capAt(list, prev)
		if on {
			if i < 0 || j < 0 || i != j+1 {
				t.Fatalf("%s %s SAVEDATE not after %s", file, where, prev)
			}
			return
		}
		if listed(list, "SAVEDATE") {
			t.Fatalf("%s %s lists SAVEDATE", file, where)
		}
	}
	before := func(file, where string, list []string, next string) {
		t.Helper()
		i, j := capAt(list, "SAVEDATE"), capAt(list, next)
		if i < 0 || j < 0 || i+1 != j {
			t.Fatalf("%s %s SAVEDATE not before %s", file, where, next)
		}
	}
	c36 := note("personalities/cyrus-36.json")
	before("personalities/cyrus-36.json", "pre-login", c36.PreLogin, "SEARCH=FUZZY")
	before("personalities/cyrus-36.json", "post-login", c36.PostLogin, "SEARCH=FUZZY")
	c3124 := note("personalities/cyrus-3124.json")
	after("personalities/cyrus-3124.json", "pre-login", c3124.PreLogin, "SASL-IR", true)
	after("personalities/cyrus-3124.json", "post-login", c3124.PostLogin, "REPLACE", true)
	c3137 := note("personalities/cyrus-3137.json")
	after("personalities/cyrus-3137.json", "pre-login", c3137.PreLogin, "SASL-IR", true)
	after("personalities/cyrus-3137.json", "post-login", c3137.PostLogin, "REPLACE", true)
	for _, file := range []string{"personalities/dovecot-245.json", "personalities/dovecot-dev.json"} {
		p := note(file)
		after(file, "pre-login", p.PreLogin, "STATUS=SIZE", false)
		after(file, "post-login", p.PostLogin, "STATUS=SIZE", true)
	}
	saslPers := note("personalities/sasl.json")
	after("personalities/sasl.json", "pre-login", saslPers.PreLogin, "QRESYNC", false)
	after("personalities/sasl.json", "post-login", saslPers.PostLogin, "REPLACE", true)

	if !sameCapSet(dropCyrusLab(c3137.PostLogin), saslPers.PostLogin) {
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
		if listed(p.GreetingCaps, "SAVEDATE") || listed(p.PreLogin, "SAVEDATE") || listed(p.PostLogin, "SAVEDATE") {
			t.Fatalf("%s gained SAVEDATE", base)
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "SAVEDATE") {
		t.Fatal("gaps still lists SAVEDATE")
	}
	for _, tok := range []string{"ACL", "URL-PARTIAL"} {
		if !strings.Contains(string(gaps), `"`+tok+`"`) {
			t.Fatalf("gaps dropped %s", tok)
		}
	}
}

func mustPers(t *testing.T, file string) *Personality {
	t.Helper()
	p, err := loadPersonality(file)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func fetchQuoted(t *testing.T, line, item string) string {
	t.Helper()
	mark := item + ` "`
	i := strings.Index(line, mark)
	if i < 0 {
		t.Fatalf("no %s in %s", item, line)
	}
	rest := line[i+len(mark):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatalf("unclosed %s in %s", item, line)
	}
	return rest[:j]
}

func mustIMAPTime(t *testing.T, s string) time.Time {
	t.Helper()
	when, err := time.Parse("02-Jan-2006 15:04:05 -0700", s)
	if err != nil {
		t.Fatal(err)
	}
	return when
}
