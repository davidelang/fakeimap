package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wantFilterNO(t *testing.T, rp *reply, line string) {
	t.Helper()
	if rp.status != "NO" || rp.text != "not implemented" || len(rp.untagged) != 0 || strings.Contains(rp.tagLine, "[") {
		t.Fatalf("%q -> %s %q untagged %q", line, rp.status, rp.text, rp.untagged)
	}
	upper := strings.ToUpper(rp.tagLine)
	if strings.Contains(upper, "FILTER") || strings.Contains(upper, "SEARCH") || strings.Contains(upper, "UID") {
		t.Fatalf("%q leaked %s", line, rp.tagLine)
	}
}

func wantFilterBad(t *testing.T, rp *reply, line string) {
	t.Helper()
	if rp.status != "BAD" || strings.Contains(rp.text, "not implemented") || len(rp.untagged) != 0 {
		t.Fatalf("%q -> %s %q untagged %q", line, rp.status, rp.text, rp.untagged)
	}
}

func wantFilterNotSelected(t *testing.T, cl *imapClient, line string) {
	t.Helper()
	rp := cl.cmd(line)
	if rp.status != "NO" || rp.text != "FILTER not selected" || len(rp.untagged) != 0 || strings.Contains(rp.tagLine, "[") {
		t.Fatalf("%q -> %s %q untagged %q", line, rp.status, rp.text, rp.untagged)
	}
}

func assertNoFilterCap(t *testing.T, caps []string, where string) {
	t.Helper()
	for _, tok := range []string{"FILTER=SIEVE", "FILTERS"} {
		if listed(caps, tok) {
			t.Fatalf("%s lists %s: %q", where, tok, caps)
		}
	}
}

func filterState(t *testing.T, cl *imapClient) string {
	t.Helper()
	var parts []string
	for _, line := range []string{
		`LIST "" "*"`,
		"STATUS INBOX (MESSAGES RECENT UIDNEXT UIDVALIDITY UNSEEN)",
		"FETCH 1:* (UID FLAGS)",
	} {
		rp := cl.cmd(line).mustOK(t)
		parts = append(parts, strings.Join(rp.untagged, "\n"))
	}
	return strings.Join(parts, "\n---\n")
}

func sameFilterGate(t *testing.T, cl *imapClient, criteria string) {
	t.Helper()
	search := cl.cmd("SEARCH " + criteria)
	filter := cl.cmd("FILTER SIEVE DELIVERY " + criteria)
	uidf := cl.cmd("UID FILTER SIEVE DELIVERY " + criteria)
	switch search.status {
	case "OK":
		wantFilterNO(t, filter, "FILTER SIEVE DELIVERY "+criteria)
		wantFilterNO(t, uidf, "UID FILTER SIEVE DELIVERY "+criteria)
	case "BAD":
		wantFilterBad(t, filter, "FILTER SIEVE DELIVERY "+criteria)
		wantFilterBad(t, uidf, "UID FILTER SIEVE DELIVERY "+criteria)
	default:
		t.Fatalf("SEARCH %s -> %s %q", criteria, search.status, search.text)
	}
}

func authSieveDemo(t *testing.T, cl *sieveClient) {
	t.Helper()
	cl.greeting()
	plain := base64.StdEncoding.EncodeToString([]byte("\x00demo\x00secret"))
	if cl.transact(`AUTHENTICATE "PLAIN" "`+plain+`"`) != "OK" {
		t.Fatal("sieve auth")
	}
}

func TestFilterSieveStub(t *testing.T) {
	if capImplemented("FILTER=SIEVE") || capImplemented("FILTERS") {
		t.Fatal("FILTER capability is implemented")
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "FILTER=SIEVE") || strings.Contains(string(gaps), `"FILTERS"`) {
		t.Fatal("gaps gained a FILTER token")
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), "gaps") {
			continue
		}
		path := filepath.Join("personalities", e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "FILTER=SIEVE") || strings.Contains(string(raw), `"FILTERS"`) {
			t.Fatalf("%s lists a FILTER token", path)
		}
		p, err := loadPersonality(path)
		if err != nil {
			t.Fatal(err)
		}
		if p.listsCap("FILTER=SIEVE") || p.listsCap("FILTERS") {
			t.Fatalf("%s lists a FILTER token", path)
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
		"FILTER",
		"UID FILTER",
		"FILTER not selected",
		"not implemented",
		"SIEVE DELIVERY",
		"SIEVE PERSONAL",
		"SIEVE GLOBAL",
		"SIEVE SCRIPT",
		"FILTER=SIEVE",
		"No script runs",
		"The search is not run",
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
	if strings.Contains(guide, "draft-bosch-imap-filter-sieve-00") {
		t.Fatal("guide pastes the facts file")
	}
	off := strings.Index(guide, "## Capabilities that stay off")
	if off < 0 || strings.Contains(guide[off:], "FILTER=SIEVE") {
		t.Fatal("guide stay-off lists FILTER=SIEVE")
	}
	para := guide[:off]
	if !strings.Contains(para, "FILTER not selected") || !strings.Contains(para, "not implemented") || !strings.Contains(para, "docs/facts/imap-server.md") {
		t.Fatal("guide missing the FILTER stub")
	}

	_, bound := start(t, Config{Listen: "127.0.0.1:0", SieveListen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	unchecked := []string{
		"FILTER",
		"FILTER SIEVE DELIVERY ALL",
		"FILTER NOT A SPEC",
		`FILTER SIEVE PERSONAL "" ALL`,
		"FILTER SIEVE",
		"UID FILTER",
		"UID FILTER GARBAGE",
		"UID FILTER SIEVE DELIVERY ALL",
		"UID FILTER SIEVE SCRIPT",
		"filter sieve delivery all",
		"FILTER SIEVE PERSONAL {0+}\r\n ALL",
	}
	pre := dial(t, bound.Listen)
	assertNoFilterCap(t, capsOf(pre.greeting), "greeting")
	greetCap := pre.cmd("CAPABILITY").mustOK(t)
	if len(greetCap.untagged) != 1 {
		t.Fatalf("capability %q", greetCap.untagged)
	}
	assertNoFilterCap(t, capsOf(greetCap.untagged[0]), "pre-login")
	for _, line := range unchecked {
		wantFilterNotSelected(t, pre, line)
	}

	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	post := cl.cmd("CAPABILITY").mustOK(t)
	if len(post.untagged) != 1 {
		t.Fatalf("post capability %q", post.untagged)
	}
	assertNoFilterCap(t, capsOf(post.untagged[0]), "post-login")
	for _, line := range unchecked {
		wantFilterNotSelected(t, cl, line)
	}

	sv := dialSieve(t, bound.Sieve)
	authSieveDemo(t, sv)
	body := []byte("not a sieve script {{\n")
	if sv.scriptCmd(`PUTSCRIPT "vacation"`, body) != "OK" {
		t.Fatal("put")
	}

	cl.cmd("SELECT INBOX").mustOK(t)
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "" {
		t.Fatalf("initial saved set %q", got)
	}
	before := filterState(t, cl)

	for _, line := range []string{
		"FILTER SIEVE DELIVERY ALL",
		"filter sieve delivery all",
		"UID FILTER SIEVE DELIVERY ALL",
		"uid filter sieve delivery seen",
		`FILTER SIEVE PERSONAL vacation ALL`,
		`FILTER SIEVE PERSONAL "no such script" UNSEEN`,
		`FILTER SIEVE PERSONAL NIL ALL`,
		"FILTER SIEVE GLOBAL shared ALL",
		`FILTER sieve Global "shared name" ALL`,
		`FILTER SIEVE SCRIPT "" ALL`,
		`FILTER SIEVE SCRIPT "this is not a sieve script {{{" ALL`,
		"FILTER SIEVE SCRIPT NIL ALL",
		"FILTER SIEVE SCRIPT {16+}\r\nnot sieve at all ALL",
		"FILTER SIEVE SCRIPT {0+}\r\n ALL",
		"FILTER SIEVE DELIVERY CHARSET UTF-8 ALL",
		"FILTER SIEVE DELIVERY charset us-ascii SEEN",
		"UID FILTER SIEVE DELIVERY OR SEEN UNSEEN",
		"FILTER SIEVE DELIVERY (SEEN)",
		"FILTER SIEVE DELIVERY FUZZY SUBJECT cafe",
		"FILTER SIEVE DELIVERY OLDER 1",
		"FILTER SIEVE DELIVERY $",
		"FILTER SIEVE DELIVERY SAVEDSINCE 01-Jan-2020",
	} {
		wantFilterNO(t, cl.cmd(line), line)
	}

	for _, line := range []string{
		"FILTER",
		"FILTER ALL",
		"FILTER SIEVE",
		"FILTER SIEVE DELIVERY",
		"FILTER SIEVE BOGUS ALL",
		`FILTER SIEVE "DELIVERY" ALL`,
		`FILTER "SIEVE" DELIVERY ALL`,
		"FILTER (SIEVE DELIVERY) ALL",
		"FILTER SIEVE PERSONAL",
		`FILTER SIEVE PERSONAL "" ALL`,
		"FILTER SIEVE PERSONAL {0+}\r\n ALL",
		"FILTER SIEVE GLOBAL",
		`FILTER SIEVE GLOBAL "" SEEN`,
		"FILTER SIEVE SCRIPT",
		"FILTER SIEVE SCRIPT ALL",
		"FILTER SIEVE DELIVERY CHARSET",
		`FILTER SIEVE DELIVERY CHARSET "" ALL`,
		"FILTER SIEVE DELIVERY CHARSET LATIN1 ALL",
		"FILTER SIEVE DELIVERY RETURN (ALL) ALL",
		"FILTER SIEVE DELIVERY RETURN (MIN MAX COUNT) ALL",
		"FILTER SIEVE DELIVERY RETURN () ALL",
		"FILTER SIEVE DELIVERY RETURN (SAVE) ALL",
		"FILTER SIEVE DELIVERY RETURN (PARTIAL 1:1) ALL",
		"UID FILTER SIEVE DELIVERY RETURN (ALL) CHARSET UTF-8 ALL",
		"FILTER SIEVE DELIVERY CHARSET UTF-8",
		"FILTER SIEVE DELIVERY ()",
		"FILTER SIEVE DELIVERY 1",
		"FILTER SIEVE DELIVERY HEADER FROM x",
		"FILTER SIEVE DELIVERY UIDAFTER 1",
		"UID FILTER",
		"UID FILTER SIEVE",
		"SEARCH FILTER",
	} {
		wantFilterBad(t, cl.cmd(line), line)
	}

	after := filterState(t, cl)
	if after != before {
		t.Fatalf("filter changed state\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "" {
		t.Fatalf("filter ran a search and saved %q", got)
	}
	okLine, gotBody := sv.get("vacation")
	if okLine != "OK" || string(gotBody) != string(body) {
		t.Fatalf("script %q %q", okLine, gotBody)
	}
	if lines := sv.list(); strings.Join(lines, "\n") != "\"vacation\"\nOK" {
		t.Fatalf("scripts %q", lines)
	}

	for _, criteria := range []string{
		"ALL",
		"SEEN",
		"CHARSET UTF-8 ALL",
		"CHARSET US-ASCII SUBJECT x",
		"charset utf-8 ALL",
		"FUZZY SUBJECT cafe",
		"FUZZY SEEN",
		"OLDER 1",
		"YOUNGER 1",
		"$",
		"UID $",
		"SAVEDSINCE 01-Jan-2020",
		"SAVEDBEFORE 01-Jan-2020",
		"SAVEDON 10-Nov-2025",
		"OR SEEN UNSEEN",
		"NOT SEEN",
		"(SEEN)",
		"FROM sender@example.com SEEN",
		"LARGER 1",
		"UID 1:*",
		"ALL ALL",
		"CHARSET UTF-8",
		"CHARSET",
		`CHARSET "" ALL`,
		"CHARSET LATIN1 ALL",
		"CHARSET UTF-8 RETURN (ALL) ALL",
		"FUZZY",
		"OLDER",
		"YOUNGER -1",
		"UIDAFTER 1",
		"UIDBEFORE 0",
		"UIDAFTER abc",
		"HEADER FROM x",
		"1",
		"1:*",
		"OR",
		"NOT",
		"()",
		"(SEEN",
		"SEEN)",
		"SINCE not-a-date",
		"MODSEQ 1",
	} {
		sameFilterGate(t, cl, criteria)
	}
	for _, criteria := range []string{
		"RETURN (ALL) ALL",
		"RETURN (MIN) ALL",
		"RETURN () ALL",
		"RETURN (PARTIAL 1:1) ALL",
	} {
		search := cl.cmd("SEARCH " + criteria)
		if search.status != "OK" && search.status != "BAD" {
			t.Fatalf("SEARCH %s -> %s %q", criteria, search.status, search.text)
		}
		wantFilterBad(t, cl.cmd("FILTER SIEVE DELIVERY "+criteria), criteria)
		wantFilterBad(t, cl.cmd("UID FILTER SIEVE DELIVERY "+criteria), "UID "+criteria)
	}
	searchReturn := cl.cmd("SEARCH RETURN (ALL) ALL")
	if searchReturn.status != "OK" || len(searchReturn.untagged) == 0 {
		t.Fatalf("SEARCH RETURN should be accepted, got %s %q untagged %q", searchReturn.status, searchReturn.text, searchReturn.untagged)
	}
	wantFilterBad(t, cl.cmd("FILTER SIEVE DELIVERY RETURN (ALL) ALL"), "return after search accepted it")
	if again := filterState(t, cl); again != before {
		t.Fatalf("gated filter changed state")
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "" {
		t.Fatalf("saved set after gates %q", got)
	}

	cl.cmd("UNSELECT").mustOK(t)
	for _, line := range []string{
		"FILTER SIEVE DELIVERY ALL",
		"UID FILTER SIEVE DELIVERY ALL",
		"FILTER NOT A SPEC",
		`FILTER SIEVE PERSONAL "" ALL`,
	} {
		wantFilterNotSelected(t, cl, line)
	}
}

func TestFilterSearchGates(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-22.json"})
	cl := dial(t, bound.Listen)
	assertNoFilterCap(t, capsOf(cl.greeting), "cyrus-22 greeting")
	cl.cmd("LOGIN demo secret").mustOK(t)
	assertNoFilterCap(t, capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "cyrus-22")
	for _, line := range []string{"FILTER SIEVE DELIVERY ALL", "UID FILTER BOGUS"} {
		wantFilterNotSelected(t, cl, line)
	}
	cl.cmd("SELECT INBOX").mustOK(t)
	for _, criteria := range []string{
		"ALL",
		"CHARSET UTF-8 ALL",
		"FUZZY SUBJECT a",
		"OLDER 10",
		"YOUNGER 10",
		"$",
		"UID $",
		"SAVEDON 01-Jan-2020",
		"SAVEDSINCE 01-Jan-2020",
		"UIDAFTER 1",
		"UIDBEFORE 0",
		"RETURN (ALL) ALL",
		"RETURN (PARTIAL 1:1) ALL",
	} {
		sameFilterGate(t, cl, criteria)
	}

	_, bound = start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/utf8.json"})
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("SELECT INBOX").mustOK(t)
	sameFilterGate(t, cl, "CHARSET UTF-8 ALL")
	sameFilterGate(t, cl, "CHARSET US-ASCII SEEN")
	cl.cmd("ENABLE UTF8=ACCEPT").mustOK(t)
	for _, criteria := range []string{
		"ALL",
		"CHARSET UTF-8 ALL",
		"CHARSET US-ASCII SEEN",
		"FUZZY SUBJECT a",
		"OLDER 1",
		"$",
		"SAVEDSINCE 01-Jan-2020",
		"UIDAFTER 1",
	} {
		sameFilterGate(t, cl, criteria)
	}
	for _, criteria := range []string{
		"RETURN (ALL) ALL",
		"RETURN (PARTIAL 1:1 COUNT) ALL",
	} {
		search := cl.cmd("SEARCH " + criteria)
		if search.status != "OK" {
			t.Fatalf("utf8 SEARCH %s -> %s %q", criteria, search.status, search.text)
		}
		wantFilterBad(t, cl.cmd("FILTER SIEVE DELIVERY "+criteria), criteria)
	}
	partial := cl.cmd("SEARCH RETURN (PARTIAL 1:1) ALL")
	if partial.status != "OK" {
		t.Fatalf("utf8 PARTIAL SEARCH %s %q", partial.status, partial.text)
	}
	wantFilterBad(t, cl.cmd("FILTER SIEVE DELIVERY RETURN (PARTIAL 1:1) ALL"), "partial return")

	_, bound = start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/stalwart.json"})
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("SELECT INBOX").mustOK(t)
	for _, criteria := range []string{
		"UIDAFTER 1",
		"UIDBEFORE 0",
		"CHARSET UTF-8 UIDAFTER 1",
		"OLDER 1",
		"$",
		"FUZZY SUBJECT a",
		"SAVEDSINCE 01-Jan-2020",
	} {
		sameFilterGate(t, cl, criteria)
	}
	ret := cl.cmd("SEARCH RETURN (ALL) ALL")
	if ret.status != "OK" {
		t.Fatalf("stalwart SEARCH RETURN -> %s %q", ret.status, ret.text)
	}
	wantFilterBad(t, cl.cmd("FILTER SIEVE DELIVERY RETURN (ALL) ALL"), "stalwart return")
	cl.cmd("ENABLE UTF8=ACCEPT").mustOK(t)
	sameFilterGate(t, cl, "CHARSET UTF-8 UIDAFTER 1")
	sameFilterGate(t, cl, "UIDAFTER 1")
	sameFilterGate(t, cl, "CHARSET US-ASCII ALL")
}
