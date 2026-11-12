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
		"SEARCH OLD",
		"SEARCH HEADER FROM x",
		"SEARCH TEXT",
	} {
		rp := cl.cmd(bad)
		if rp.status != "BAD" {
			t.Fatalf("%s -> %s", bad, rp.status)
		}
	}
}

func TestSearchBool(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)

	want := map[string]string{
		"SEARCH OR SEEN UNSEEN":                             "1 2",
		"SEARCH NOT SEEN":                                   "2",
		"SEARCH (SEEN)":                                     "1",
		"SEARCH NOT (SEEN)":                                 "2",
		"SEARCH SEEN UNSEEN":                                "",
		"SEARCH FROM sender@example.com SEEN":               "1",
		"SEARCH (FROM sender@example.com) (UNSEEN)":         "2",
		"SEARCH (SEEN UNSEEN)":                              "",
		`SEARCH OR (SUBJECT "Naïve café") (TEXT overlay-2)`: "1 2",
		`SEARCH OR (FROM nosuch@example.com SUBJECT "Naïve café") (TEXT overlay-2)`: "2",
		"SEARCH NOT (TEXT overlay-2)":                                   "1",
		`SEARCH OR NOT (SEEN) (SUBJECT "Naïve café")`:                   "1 2",
		"SEARCH NOT (OR SEEN (TEXT overlay-2))":                         "",
		`SEARCH (OR (SEEN) (NOT (TEXT "in the body")))`:                 "1 2",
		"SEARCH ((SEEN))":                                               "1",
		"SEARCH OR (SEEN FROM nosuch@example.com) NOT (TEXT overlay-2)": "1",
		"SEARCH NOT NOT UNSEEN":                                         "2",
		`SEARCH CHARSET UTF-8 OR SUBJECT "Naïve café" TEXT overlay-2`:   "1 2",
		"UID SEARCH CHARSET UTF-8 NOT SEEN":                             "2",
		"UID SEARCH OR SEEN UNSEEN":                                     "1 2",
		"UID SEARCH (SEEN)":                                             "1",
	}
	for cmd, ids := range want {
		if got := strings.Join(searchIDs(t, cl.cmd(cmd)), " "); got != ids {
			t.Fatalf("%s -> %q, want %q", cmd, got, ids)
		}
	}

	tag, line := oneEsearch(t, cl.cmd(`UID SEARCH RETURN (ALL) CHARSET UTF-8 OR TEXT "in the body" TEXT overlay-2`))
	if line != `* ESEARCH (TAG "`+tag+`") UID ALL 1:2` {
		t.Fatalf("uid esearch %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("SEARCH RETURN (ALL) NOT SEEN"))
	if line != `* ESEARCH (TAG "`+tag+`") ALL 2` {
		t.Fatalf("esearch not %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("UID SEARCH RETURN (ALL) (SEEN)"))
	if line != `* ESEARCH (TAG "`+tag+`") UID ALL 1` {
		t.Fatalf("esearch paren %q", line)
	}

	for _, bad := range []string{
		"SEARCH FUZZY",
		"SEARCH OLD",
		"SEARCH HEADER FROM x",
		"SEARCH 1",
		"SEARCH OR",
		"SEARCH OR SEEN",
		"SEARCH OR SEEN NOT",
		"SEARCH NOT",
		"SEARCH NOT OR",
		"UID SEARCH NOT",
		"SEARCH ()",
		"SEARCH (SEEN",
		"SEARCH (OR SEEN",
		"SEARCH OR (SEEN UNSEEN",
		"SEARCH NOT (SEEN",
		"SEARCH SEEN)",
		"SEARCH ((SEEN)",
	} {
		rp := cl.cmd(bad)
		if rp.status != "BAD" {
			t.Fatalf("%s -> %s", bad, rp.status)
		}
	}
}
