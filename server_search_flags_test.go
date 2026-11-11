package main

import (
	"strings"
	"testing"
)

func TestSearchFlags(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)

	cl.cmd("EXAMINE INBOX").ok(t)
	if got := searchIDs(t, cl.cmd("SEARCH NEW")); strings.Join(got, " ") != "2" {
		t.Fatalf("NEW while recent %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH KEYWORD \Recent`)); strings.Join(got, " ") != "1 2" {
		t.Fatalf("Recent while examined %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH UNKEYWORD \Recent`)); len(got) != 0 {
		t.Fatalf("UNKEYWORD Recent %v", got)
	}

	cl.cmd("SELECT INBOX").ok(t)
	if got := searchIDs(t, cl.cmd("SEARCH NEW")); len(got) != 0 {
		t.Fatalf("NEW after select %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH KEYWORD \Recent`)); len(got) != 0 {
		t.Fatalf("Recent cleared %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH UNKEYWORD \Recent`)); strings.Join(got, " ") != "1 2" {
		t.Fatalf("not Recent %v", got)
	}

	cl.cmd(`STORE 1 +FLAGS (\Answered)`).ok(t)
	cl.cmd(`STORE 2 +FLAGS ($Forwarded)`).ok(t)
	want := map[string]string{
		"SEARCH ANSWERED":                       "1",
		"SEARCH UNANSWERED":                     "2",
		"SEARCH NOT ANSWERED":                   "2",
		"SEARCH OR ANSWERED UNANSWERED":         "1 2",
		"SEARCH (ANSWERED)":                     "1",
		`SEARCH KEYWORD \Answered`:              "1",
		`SEARCH KEYWORD \answered`:              "1",
		`SEARCH UNKEYWORD \Answered`:            "2",
		"SEARCH KEYWORD $Forwarded":             "2",
		"SEARCH KEYWORD $forwarded":             "2",
		"SEARCH UNKEYWORD $Forwarded":           "1",
		"SEARCH UNKEYWORD $forwarded":           "1",
		"SEARCH NOT KEYWORD $Forwarded":         "1",
		"SEARCH OR ANSWERED KEYWORD $Forwarded": "1 2",
		"UID SEARCH ANSWERED":                   "1",
		"UID SEARCH KEYWORD $Forwarded":         "2",
		"UID SEARCH NOT UNANSWERED":             "1",
	}
	for cmd, ids := range want {
		if got := strings.Join(searchIDs(t, cl.cmd(cmd)), " "); got != ids {
			t.Fatalf("%s -> %q, want %q", cmd, got, ids)
		}
	}

	cl.cmd("APPEND INBOX {5+}\r\nhello").ok(t)
	cl.cmd(`APPEND INBOX (\Seen) {5+}` + "\r\nhello").ok(t)
	if got := searchIDs(t, cl.cmd("SEARCH NEW")); strings.Join(got, " ") != "3" {
		t.Fatalf("NEW append %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH KEYWORD \Recent`)); strings.Join(got, " ") != "3 4" {
		t.Fatalf("Recent append %v", got)
	}
	if got := searchIDs(t, cl.cmd("UID SEARCH NEW")); strings.Join(got, " ") != "3" {
		t.Fatalf("UID NEW %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH NOT NEW")); strings.Join(got, " ") != "1 2 4" {
		t.Fatalf("NOT NEW %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH OR NEW ANSWERED")); strings.Join(got, " ") != "1 3" {
		t.Fatalf("OR NEW %v", got)
	}

	tag, line := oneEsearch(t, cl.cmd("SEARCH RETURN (ALL) NEW"))
	if line != `* ESEARCH (TAG "`+tag+`") ALL 3` {
		t.Fatalf("esearch NEW %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("UID SEARCH RETURN (ALL) KEYWORD $Forwarded"))
	if line != `* ESEARCH (TAG "`+tag+`") UID ALL 2` {
		t.Fatalf("esearch keyword %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("UID SEARCH RETURN (ALL) NOT ANSWERED"))
	if line != `* ESEARCH (TAG "`+tag+`") UID ALL 2:4` {
		t.Fatalf("esearch not answered %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("SEARCH RETURN (ALL) OR ANSWERED NEW"))
	if line != `* ESEARCH (TAG "`+tag+`") ALL 1,3` {
		t.Fatalf("esearch or %q", line)
	}

	other := dial(t, bound.Listen)
	other.cmd("LOGIN demo secret").ok(t)
	other.cmd("SELECT INBOX").ok(t)
	if got := searchIDs(t, cl.cmd(`SEARCH KEYWORD \Recent`)); len(got) != 0 {
		t.Fatalf("Recent after another select %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH NEW")); len(got) != 0 {
		t.Fatalf("NEW after another select %v", got)
	}

	for _, bad := range []string{
		"SEARCH FUZZY",
		"SEARCH OLD",
		"SEARCH RECENT",
		"SEARCH DRAFT",
		"SEARCH KEYWORD",
		"SEARCH UNKEYWORD",
		`SEARCH KEYWORD \Nope`,
		"SEARCH ANSWERED KEYWORD",
		"UID SEARCH UNKEYWORD",
		"SEARCH NOT KEYWORD",
		"SEARCH OR NEW KEYWORD",
	} {
		rp := cl.cmd(bad)
		if rp.status != "BAD" {
			t.Fatalf("%s -> %s", bad, rp.status)
		}
	}
}
