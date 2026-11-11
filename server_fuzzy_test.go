package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestFuzzyPersonality(t *testing.T) {
	p, err := loadPersonality("personalities/fuzzy.json")
	if err != nil {
		t.Fatal(err)
	}
	if !listed(p.PreLogin, "SEARCH=FUZZY") || !listed(p.PostLogin, "SEARCH=FUZZY") {
		t.Fatal("fuzzy.json missing SEARCH=FUZZY")
	}
}

func TestFuzzySearch(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/fuzzy.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"SEARCH=FUZZY"})
	cl.cmd("CREATE FuzzyBox").ok(t)
	cl.cmd("SELECT FuzzyBox").ok(t)

	apple := fuzzyMsg("Apple Pie", "Ann <sender@example.com>", "Bob <demo@example.com>", "Cara <onlycc@example.com>", "Mon, 01 Jan 2024 00:00:00 +0000", "hello body")
	other := fuzzyMsg("banana", "zed@example.com", "y@example.com", "x@example.com", "Tue, 02 Jan 2024 00:00:00 +0000", "other")
	short := fuzzyMsg("cat", "a@example.com", "b@example.com", "c@example.com", "Wed, 03 Jan 2024 00:00:00 +0000", "zz")
	for _, raw := range []struct{ date, msg string }{
		{"01-Jan-2024 00:00:00 +0000", apple},
		{"02-Jan-2024 00:00:00 +0000", other},
		{"03-Jan-2024 00:00:00 +0000", short},
	} {
		cl.cmd(fmt.Sprintf("APPEND FuzzyBox \"%s\" {%d+}\r\n%s", raw.date, len(raw.msg), raw.msg)).ok(t)
	}
	cl.cmd("STORE 1 +FLAGS (\\Seen \\Answered)").ok(t)

	want := map[string]string{
		"SEARCH FUZZY SUBJECT apple":                  "1",
		"SEARCH FUZZY SUBJECT aple":                   "1",
		"SEARCH SUBJECT aple":                         "",
		"UID SEARCH FUZZY SUBJECT aple":               "1",
		"SEARCH CHARSET UTF-8 FUZZY SUBJECT aple":     "1",
		"UID SEARCH CHARSET UTF-8 FUZZY SUBJECT aple": "1",
		"SEARCH FUZZY SUBJECT APLE":                   "1",
		"SEARCH FUZZY SUBJECT ap":                     "1",
		"SEARCH FUZZY SUBJECT xy":                     "",
		"SEARCH FUZZY SUBJECT ct":                     "",
		"SEARCH FUZZY SUBJECT cot":                    "3",
		"SEARCH FUZZY SUBJECT caat":                   "3",
		"SEARCH FUZZY TEXT helo":                      "1",
		"SEARCH FUZZY BODY helo":                      "1",
		"SEARCH FUZZY BODY Appl":                      "",
		"SEARCH FUZZY FROM snder":                     "1",
		"SEARCH FUZZY TO dem":                         "1",
		"SEARCH FUZZY CC onlyc":                       "1",
		"SEARCH FUZZY CC onlcc":                       "1",
		"SEARCH FUZZY SEEN":                           "1",
		"SEARCH FUZZY UNSEEN":                         "2 3",
		"SEARCH FUZZY ANSWERED":                       "1",
		"SEARCH FUZZY UNANSWERED":                     "2 3",
		"SEARCH FUZZY LARGER 10":                      "1 2 3",
		"SEARCH FUZZY SMALLER 10":                     "",
		"SEARCH FUZZY SINCE 01-Jan-2024":              "1 2 3",
		"SEARCH FUZZY BEFORE 01-Jan-2020":             "",
		"SEARCH FUZZY ON 02-Jan-2024":                 "2",
		"SEARCH FUZZY UID 1":                          "1",
		"SEARCH FUZZY UID 2":                          "2",
		"UID SEARCH FUZZY UID 1":                      "1",
		"SEARCH FUZZY OR SUBJECT aple SUBJECT banan":  "1 2",
		"SEARCH FUZZY NOT SUBJECT aple":               "2 3",
		"SEARCH FUZZY (SUBJECT aple SEEN)":            "1",
		"SEARCH FUZZY (SUBJECT aple UNSEEN)":          "",
		"SEARCH FUZZY OR SEEN SUBJECT banan":          "1 2",
		"SEARCH NOT FUZZY SUBJECT aple":               "2 3",
		"SEARCH FUZZY SUBJECT aple UNSEEN":            "",
		"SEARCH FUZZY SUBJECT apple SEEN":             "1",
	}
	for cmd, ids := range want {
		if got := strings.Join(searchIDs(t, cl.cmd(cmd)), " "); got != ids {
			t.Fatalf("%s -> %q, want %q", cmd, got, ids)
		}
	}

	tag, line := oneEsearch(t, cl.cmd("UID SEARCH RETURN (ALL) FUZZY SUBJECT aple"))
	if line != `* ESEARCH (TAG "`+tag+`") UID ALL 1` || strings.Contains(line, "RELEVANCY") {
		t.Fatalf("esearch all %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd(`SEARCH RETURN (COUNT) CHARSET UTF-8 FUZZY SUBJECT aple`))
	if line != `* ESEARCH (TAG "`+tag+`") COUNT 1` || strings.Contains(line, "RELEVANCY") {
		t.Fatalf("esearch count %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("UID SEARCH RETURN (COUNT) CHARSET UTF-8 FUZZY TEXT helo"))
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 1` || strings.Contains(line, "RELEVANCY") {
		t.Fatalf("uid count %q", line)
	}
	if _, line = oneEsearch(t, cl.cmd("SEARCH RETURN (ALL) FUZZY SUBJECT apple")); strings.Contains(line, "RELEVANCY") {
		t.Fatalf("exact relevancy %q", line)
	}

	for _, bad := range []string{
		"SEARCH FUZZY",
		"SEARCH FUZZY OLD",
		"SEARCH FUZZY HEADER FROM x",
		"SORT (FROM) UTF-8 FUZZY ALL",
		"UID SORT (SUBJECT) UTF-8 FUZZY SUBJECT apple",
		"THREAD REFERENCES UTF-8 FUZZY ALL",
		"UID THREAD ORDEREDSUBJECT UTF-8 FUZZY ALL",
		"THREAD REFS UTF-8 FUZZY SUBJECT aple",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
}

func TestFuzzyRequiresCap(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	for _, bad := range []string{
		"SEARCH FUZZY TEXT seed",
		"SEARCH FUZZY SUBJECT apple",
		"UID SEARCH FUZZY SUBJECT aple",
		"SEARCH CHARSET UTF-8 FUZZY TEXT x",
		"UID SEARCH RETURN (COUNT) FUZZY TEXT apple",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
}

func fuzzyMsg(subject, from, to, cc, date, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nCc: %s\r\nSubject: %s\r\nDate: %s\r\n\r\n%s\r\n", from, to, cc, subject, date, body)
	return b.String()
}

func TestFuzzyOneEdit(t *testing.T) {
	if !oneEdit("aple", "apple") || oneEdit("ap", "apple") || oneEdit("apple", "apple") {
		t.Fatal("apple edits")
	}
	if !oneEdit("ct", "cat") || !oneEdit("cot", "cat") || !oneEdit("caat", "cat") {
		t.Fatal("cat edits")
	}
	if oneEdit("app", "apple") {
		t.Fatal("two inserts")
	}
}
