package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestSearchTextBodyCC(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)

	if got := searchIDs(t, cl.cmd(`SEARCH TEXT "in the body"`)); strings.Join(got, " ") != "1" {
		t.Fatalf("TEXT body %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH TEXT "IN THE BODY"`)); strings.Join(got, " ") != "1" {
		t.Fatalf("TEXT case %v", got)
	}
	if got := searchIDs(t, cl.cmd(`UID SEARCH TEXT "in the body"`)); strings.Join(got, " ") != "1" {
		t.Fatalf("UID TEXT %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH CHARSET UTF-8 TEXT "in the body"`)); strings.Join(got, " ") != "1" {
		t.Fatalf("CHARSET TEXT %v", got)
	}
	if got := searchIDs(t, cl.cmd(`UID SEARCH CHARSET UTF-8 TEXT "CAFÉ"`)); strings.Join(got, " ") != "1" {
		t.Fatalf("UID CHARSET TEXT %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH TEXT overlay-2")); strings.Join(got, " ") != "2" {
		t.Fatalf("TEXT header %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BODY overlay-2")); len(got) != 0 {
		t.Fatalf("BODY saw header %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH BODY "plain part"`)); strings.Join(got, " ") != "2" {
		t.Fatalf("BODY %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH BODY "PLAIN PART"`)); strings.Join(got, " ") != "2" {
		t.Fatalf("BODY case %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BODY Subject")); len(got) != 0 {
		t.Fatalf("BODY saw Subject %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH FROM sender@example.com")); strings.Join(got, " ") != "1 2" {
		t.Fatalf("FROM %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH TO demo@example.com")); strings.Join(got, " ") != "1 2" {
		t.Fatalf("TO %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH SUBJECT "Naïve café"`)); strings.Join(got, " ") != "1" {
		t.Fatalf("SUBJECT %v", got)
	}

	tag, line := oneEsearch(t, cl.cmd(`UID SEARCH RETURN (ALL) CHARSET UTF-8 TEXT "in the body"`))
	if line != `* ESEARCH (TAG "`+tag+`") UID ALL 1` {
		t.Fatalf("esearch all %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd(`SEARCH RETURN (COUNT) TEXT "in the body"`))
	if line != `* ESEARCH (TAG "`+tag+`") COUNT 1` {
		t.Fatalf("esearch count %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd(`SEARCH RETURN (ALL) CHARSET UTF-8 TEXT overlay-2`))
	if line != `* ESEARCH (TAG "`+tag+`") ALL 2` {
		t.Fatalf("esearch header %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd(`UID SEARCH RETURN (COUNT) CHARSET UTF-8 TEXT nosuch-text-word`))
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 0` {
		t.Fatalf("esearch zero %q", line)
	}

	raw := "From: Only From <onlyfrom@example.com>\r\nTo: Only To <onlyto@example.com>\r\nCc: Only Cc <onlycc@example.com>\r\nSubject: onlysubject\r\n\r\nonlybodyword\r\n"
	cl.cmd("CREATE SearchText").ok(t)
	cl.cmd(fmt.Sprintf("APPEND SearchText {%d+}\r\n%s", len(raw), raw)).ok(t)
	cl.cmd("SELECT SearchText").ok(t)
	if got := searchIDs(t, cl.cmd("SEARCH CC onlycc")); strings.Join(got, " ") != "1" {
		t.Fatalf("CC %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH CC ONLYCC")); strings.Join(got, " ") != "1" {
		t.Fatalf("CC case %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH CC onlyfrom")); len(got) != 0 {
		t.Fatalf("CC saw From %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH CC onlyto")); len(got) != 0 {
		t.Fatalf("CC saw To %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH FROM onlyfrom")); strings.Join(got, " ") != "1" {
		t.Fatalf("FROM only %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH FROM onlycc")); len(got) != 0 {
		t.Fatalf("FROM saw Cc %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH TO onlyto")); strings.Join(got, " ") != "1" {
		t.Fatalf("TO only %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH TO onlycc")); len(got) != 0 {
		t.Fatalf("TO saw Cc %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH SUBJECT onlysubject")); strings.Join(got, " ") != "1" {
		t.Fatalf("SUBJECT only %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH SUBJECT onlybodyword")); len(got) != 0 {
		t.Fatalf("SUBJECT saw body %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BODY onlybodyword")); strings.Join(got, " ") != "1" {
		t.Fatalf("BODY word %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BODY ONLYBODYWORD")); strings.Join(got, " ") != "1" {
		t.Fatalf("BODY word case %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BODY onlysubject")); len(got) != 0 {
		t.Fatalf("BODY saw subject %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BODY onlycc")); len(got) != 0 {
		t.Fatalf("BODY saw Cc %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH TEXT onlybodyword")); strings.Join(got, " ") != "1" {
		t.Fatalf("TEXT body word %v", got)
	}
	if got := searchIDs(t, cl.cmd(`UID SEARCH CHARSET UTF-8 TEXT onlysubject`)); strings.Join(got, " ") != "1" {
		t.Fatalf("UID CHARSET header %v", got)
	}

	cl.cmd("SELECT INBOX/Generated").ok(t)
	if got := searchIDs(t, cl.cmd("SEARCH TEXT seed=")); strings.Join(got, " ") != "1 2 3" {
		t.Fatalf("gen TEXT %v", got)
	}
	if got := searchIDs(t, cl.cmd(`UID SEARCH CHARSET UTF-8 TEXT seed=`)); strings.Join(got, " ") != "1 2 3" {
		t.Fatalf("gen UID CHARSET %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BODY seed=")); strings.Join(got, " ") != "1 2 3" {
		t.Fatalf("gen BODY %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BODY Generated")); len(got) != 0 {
		t.Fatalf("gen BODY saw subject %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH TEXT "Generated 2"`)); strings.Join(got, " ") != "2" {
		t.Fatalf("gen TEXT subject %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH CC alice")); len(got) != 0 {
		t.Fatalf("gen CC %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH FROM alice")); strings.Join(got, " ") != "1 2 3" {
		t.Fatalf("gen FROM %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH SUBJECT "Generated 2"`)); strings.Join(got, " ") != "2" {
		t.Fatalf("gen SUBJECT %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH TO demo@example.com")); strings.Join(got, " ") != "1 2 3" {
		t.Fatalf("gen TO %v", got)
	}

	for _, bad := range []string{
		"SEARCH OR SEEN UNSEEN",
		"SEARCH NOT SEEN",
		"SEARCH (SEEN)",
		"SEARCH FUZZY TEXT seed",
		"SEARCH NEW",
		"SEARCH HEADER FROM x",
		"UID SEARCH OR TEXT a TEXT b",
		"SEARCH TEXT",
	} {
		rp := cl.cmd(bad)
		if rp.status != "BAD" {
			t.Fatalf("%s -> %s", bad, rp.status)
		}
	}
}
