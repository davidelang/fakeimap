package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestSortPersonality(t *testing.T) {
	p, err := loadPersonality("personalities/sort.json")
	if err != nil {
		t.Fatal(err)
	}
	if p.SortPartialCountOnly {
		t.Fatal("sort.json returns no window")
	}
	for _, c := range []string{"SORT", "ESORT", "SORT=DISPLAY"} {
		if !listed(p.PreLogin, c) || !listed(p.PostLogin, c) {
			t.Fatalf("sort.json missing %s", c)
		}
	}
	cyrus, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	if !cyrus.SortPartialCountOnly {
		t.Fatal("cyrus personality should omit the PARTIAL window")
	}
	for _, c := range []string{"SORT", "ESORT", "SORT=DISPLAY", "SORT=MODSEQ", "SORT=UID"} {
		if !listed(cyrus.PreLogin, c) || !listed(cyrus.PostLogin, c) {
			t.Fatalf("cyrus personality missing %s", c)
		}
	}
	for _, c := range []string{"SORT=DISPLAY", "SORT=MODSEQ", "SORT=UID"} {
		if !capImplemented(c) {
			t.Fatalf("%s is not implemented", c)
		}
	}
}

func TestSortKeys(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/sort.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"SORT", "ESORT", "SORT=DISPLAY"})

	cl.cmd("CREATE SortBox").ok(t)
	cl.cmd("SELECT SortBox").ok(t)
	raws := []string{
		sortMsg("mid@e.com", "z@e.com", "b@e.com", "mango", "Mon, 01 Jan 2020 00:00:00 +0000", "aaaa"),
		sortMsg("zed@e.com", "a@e.com", "", "apple", "Wed, 03 Jan 2020 00:00:00 +0000", "a"),
		sortMsg("amy@e.com", "m@e.com", "a@e.com", "cherry", "Tue, 02 Jan 2020 00:00:00 +0000", strings.Repeat("b", 20)),
	}
	internals := []string{
		"03-Mar-2020 00:00:00 +0000",
		"01-Mar-2020 00:00:00 +0000",
		"02-Mar-2020 00:00:00 +0000",
	}
	for i, raw := range raws {
		cl.cmd(fmt.Sprintf("APPEND SortBox \"%s\" {%d+}\r\n%s", internals[i], len(raw), raw)).ok(t)
	}

	if got := sortIDs(t, cl.cmd("SORT (FROM) UTF-8 ALL")); got != "3 1 2" {
		t.Fatalf("FROM %q", got)
	}
	if got := sortIDs(t, cl.cmd("UID SORT (FROM) US-ASCII ALL")); got != "3 1 2" {
		t.Fatalf("UID FROM %q", got)
	}
	if got := sortIDs(t, cl.cmd(`SORT (REVERSE FROM) "UTF-8" ALL`)); got != "2 1 3" {
		t.Fatalf("REVERSE FROM %q", got)
	}
	if got := sortIDs(t, cl.cmd("SORT (ARRIVAL) UTF-8 ALL")); got != "2 3 1" {
		t.Fatalf("ARRIVAL %q", got)
	}
	if got := sortIDs(t, cl.cmd("SORT (DATE) UTF-8 ALL")); got != "1 3 2" {
		t.Fatalf("DATE %q", got)
	}
	if got := sortIDs(t, cl.cmd("SORT (SUBJECT) UTF-8 ALL")); got != "2 3 1" {
		t.Fatalf("SUBJECT %q", got)
	}
	if got := sortIDs(t, cl.cmd("SORT (TO) UTF-8 ALL")); got != "2 3 1" {
		t.Fatalf("TO %q", got)
	}
	if got := sortIDs(t, cl.cmd("SORT (CC) UTF-8 ALL")); got != "2 3 1" {
		t.Fatalf("CC %q", got)
	}
	sizeOrder := []int{0, 1, 2}
	sort.SliceStable(sizeOrder, func(i, j int) bool {
		return len(raws[sizeOrder[i]]) < len(raws[sizeOrder[j]])
	})
	var sizeWant []string
	for _, i := range sizeOrder {
		sizeWant = append(sizeWant, fmt.Sprintf("%d", i+1))
	}
	if got := sortIDs(t, cl.cmd("UID SORT (SIZE) UTF-8 ALL")); got != strings.Join(sizeWant, " ") {
		t.Fatalf("SIZE %q want %s", got, strings.Join(sizeWant, " "))
	}
	if got := sortIDs(t, cl.cmd("SORT (FROM) UTF-8 FROM amy@e.com")); got != "3" {
		t.Fatalf("filtered %q", got)
	}

	tag, line := oneEsearch(t, cl.cmd("UID SORT RETURN (ALL COUNT) (FROM) UTF-8 ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 3 ALL 3,1:2` {
		t.Fatalf("all %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("SORT RETURN (ALL COUNT) (FROM) UTF-8 ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") COUNT 3 ALL 3,1:2` {
		t.Fatalf("seq all %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("UID SORT RETURN (PARTIAL 1:2 COUNT) (FROM) UTF-8 ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 3 PARTIAL (1:2 3,1)` {
		t.Fatalf("partial %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("UID SORT RETURN (PARTIAL -1:-1 COUNT) (FROM) UTF-8 ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 3 PARTIAL (-1:-1 2)` {
		t.Fatalf("negative %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("UID SORT RETURN (PARTIAL 4:9 COUNT) (FROM) UTF-8 ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 3 PARTIAL (4:9 NIL)` {
		t.Fatalf("miss %q", line)
	}
	tag, line = oneEsearch(t, cl.cmd("UID SORT RETURN (MIN MAX COUNT) (FROM) UTF-8 ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 3 MIN 1 MAX 3` {
		t.Fatalf("min max %q", line)
	}
	empty := cl.cmd("SORT RETURN (COUNT) (FROM) UTF-8 FROM nosuch@e.com")
	tag, line = oneEsearch(t, empty)
	if line != `* ESEARCH (TAG "`+tag+`") COUNT 0` {
		t.Fatalf("empty esort %q", line)
	}
	if got := sortIDs(t, cl.cmd("SORT (SUBJECT) UTF-8 FROM nosuch@e.com")); got != "" {
		t.Fatalf("empty sort %q", got)
	}

	tie := sortMsg("Amy <amy@e.com>", "z@e.com", "", "zed", "Mon, 01 Jan 2020 00:00:00 +0000", "z")
	cl.cmd(fmt.Sprintf("APPEND SortBox \"04-Mar-2020 00:00:00 +0000\" {%d+}\r\n%s", len(tie), tie)).ok(t)
	if got := sortIDs(t, cl.cmd("SORT (FROM) UTF-8 ALL")); got != "3 4 1 2" {
		t.Fatalf("tie %q", got)
	}
	if got := sortIDs(t, cl.cmd("SORT (REVERSE FROM) UTF-8 ALL")); got != "2 1 3 4" {
		t.Fatalf("reverse tie %q", got)
	}

	for _, line := range []string{
		"SORT (NOTAKEY) UTF-8 ALL",
		"SORT (FROM FROM) UTF-8 ALL",
		"SORT (FROM) KOI8-R ALL",
		"SORT (FROM) UTF-8 FUZZY ALL",
		"SORT FROM UTF-8 ALL",
	} {
		if cl.cmd(line).status != "BAD" {
			t.Fatalf("%s was accepted", line)
		}
	}

	cl.cmd("CREATE SortUid").ok(t)
	cl.cmd("SELECT SortUid").ok(t)
	one := sortMsg("only@e.com", "a@e.com", "", "one", "Mon, 01 Jan 2020 00:00:00 +0000", "z")
	cl.cmd(fmt.Sprintf("APPEND SortUid \"01-Jan-2020 00:00:00 +0000\" {%d+}\r\n%s", len(one), one)).ok(t)
	cl.cmd(fmt.Sprintf("APPEND SortUid \"02-Jan-2020 00:00:00 +0000\" {%d+}\r\n%s", len(one), one)).ok(t)
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).ok(t)
	cl.cmd("EXPUNGE").ok(t)
	if got := sortIDs(t, cl.cmd("SORT (ARRIVAL) UTF-8 ALL")); got != "1" {
		t.Fatalf("seq after expunge %q", got)
	}
	if got := sortIDs(t, cl.cmd("UID SORT (ARRIVAL) UTF-8 ALL")); got != "2" {
		t.Fatalf("uid after expunge %q", got)
	}
}

func TestSortPartialCountOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "count-only.json")
	p, err := loadPersonality("personalities/sort.json")
	if err != nil {
		t.Fatal(err)
	}
	p.SortPartialCountOnly = true
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	tag, line := oneEsearch(t, cl.cmd("UID SORT RETURN (PARTIAL 1:1 COUNT) (ARRIVAL) UTF-8 ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 2` {
		t.Fatalf("count only %q", line)
	}
	if strings.Contains(line, "PARTIAL") {
		t.Fatalf("window returned %q", line)
	}
}

func TestSortRequiresCaps(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if cl.cmd("SORT (FROM) UTF-8 ALL").status != "BAD" {
		t.Fatal("SORT was accepted")
	}
	if cl.cmd("UID SORT (DATE) UTF-8 ALL").status != "BAD" {
		t.Fatal("UID SORT was accepted")
	}
	plain := cl.cmd("SEARCH ALL")
	if strings.Join(searchIDs(t, plain), " ") != "1 2" {
		t.Fatalf("search after rejected SORT %+v", plain)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sort-only.json")
	p, err := loadPersonality("personalities/sort.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PreLogin = dropCap(p.PreLogin, "ESORT")
	p.PostLogin = dropCap(p.PostLogin, "ESORT")
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if cl.cmd("UID SORT RETURN (ALL COUNT) (ARRIVAL) UTF-8 ALL").status != "BAD" {
		t.Fatal("RETURN was accepted without ESORT")
	}
	if got := sortIDs(t, cl.cmd("SORT (ARRIVAL) UTF-8 ALL")); got != "1 2" {
		t.Fatalf("plain sort without ESORT %q", got)
	}
}

func TestSortDisplay(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/sort.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE DispBox").ok(t)
	cl.cmd("SELECT DispBox").ok(t)
	raws := []string{
		sortMsg("Zed Person <amy@e.com>", "Amy Person <zed@e.com>", "", "a", "Mon, 01 Jan 2020 00:00:00 +0000", "a"),
		sortMsg("=?utf-8?Q?amy_person?= <zed@e.com>", "zed person <amy@e.com>", "", "b", "Tue, 02 Jan 2020 00:00:00 +0000", "b"),
		sortMsg("Mid <mid@e.com>", "mid@e.com", "", "c", "Wed, 03 Jan 2020 00:00:00 +0000", "c"),
		sortMsg("mid <bbb@e.com>", "Mid <aaa@e.com>", "", "d", "Thu, 04 Jan 2020 00:00:00 +0000", "d"),
	}
	for i, raw := range raws {
		cl.cmd(fmt.Sprintf("APPEND DispBox \"0%d-Mar-2020 00:00:00 +0000\" {%d+}\r\n%s", i+1, len(raw), raw)).ok(t)
	}
	if got := sortIDs(t, cl.cmd("SORT (FROM) UTF-8 ALL")); got != "1 4 3 2" {
		t.Fatalf("FROM %q", got)
	}
	if got := sortIDs(t, cl.cmd("SORT (TO) UTF-8 ALL")); got != "4 2 3 1" {
		t.Fatalf("TO %q", got)
	}
	if got := sortIDs(t, cl.cmd("UID SORT (DISPLAYFROM) UTF-8 ALL")); got != "2 3 4 1" {
		t.Fatalf("DISPLAYFROM %q", got)
	}
	if got := sortIDs(t, cl.cmd("UID SORT (REVERSE DISPLAYFROM) UTF-8 ALL")); got != "1 3 4 2" {
		t.Fatalf("REVERSE DISPLAYFROM %q", got)
	}
	if got := sortIDs(t, cl.cmd("UID SORT (DISPLAYTO) US-ASCII ALL")); got != "1 4 3 2" {
		t.Fatalf("DISPLAYTO %q", got)
	}
	if got := sortIDs(t, cl.cmd("SORT (REVERSE DISPLAYTO) UTF-8 ALL")); got != "2 3 4 1" {
		t.Fatalf("REVERSE DISPLAYTO %q", got)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "no-display.json")
	p, err := loadPersonality("personalities/sort.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PreLogin = dropCap(p.PreLogin, "SORT=DISPLAY")
	p.PostLogin = dropCap(p.PostLogin, "SORT=DISPLAY")
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE PlainBox").ok(t)
	cl.cmd("SELECT PlainBox").ok(t)
	one := sortMsg("Amy <amy@e.com>", "Zed <zed@e.com>", "", "e", "Mon, 01 Jan 2020 00:00:00 +0000", "e")
	cl.cmd(fmt.Sprintf("APPEND PlainBox \"05-Mar-2020 00:00:00 +0000\" {%d+}\r\n%s", len(one), one)).ok(t)
	if cl.cmd("UID SORT (DISPLAYFROM) UTF-8 ALL").status != "BAD" {
		t.Fatal("DISPLAYFROM was accepted")
	}
	if cl.cmd("SORT (REVERSE DISPLAYTO) UTF-8 ALL").status != "BAD" {
		t.Fatal("DISPLAYTO was accepted")
	}
	if got := sortIDs(t, cl.cmd("SORT (FROM) UTF-8 ALL")); got != "1" {
		t.Fatalf("FROM without SORT=DISPLAY %q", got)
	}
}

func TestSortModseq(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/sort.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if cl.cmd("SORT (MODSEQ) UTF-8 ALL").status != "BAD" {
		t.Fatal("MODSEQ was accepted")
	}
	if cl.cmd("UID SORT (REVERSE MODSEQ) UTF-8 ALL").status != "BAD" {
		t.Fatal("UID MODSEQ was accepted")
	}

	_, bound = start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE ModBox").ok(t)
	cl.cmd("SELECT ModBox").ok(t)
	raw := sortMsg("a@e.com", "b@e.com", "", "m", "Mon, 01 Jan 2020 00:00:00 +0000", "x")
	cl.cmd(fmt.Sprintf("APPEND ModBox \"01-Jan-2020 00:00:00 +0000\" {%d+}\r\n%s", len(raw), raw)).ok(t)
	cl.cmd(fmt.Sprintf("APPEND ModBox \"02-Jan-2020 00:00:00 +0000\" {%d+}\r\n%s", len(raw), raw)).ok(t)
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).ok(t)
	cl.cmd("EXPUNGE").ok(t)
	cl.cmd(fmt.Sprintf("APPEND ModBox \"03-Jan-2020 00:00:00 +0000\" {%d+}\r\n%s", len(raw), raw)).ok(t)
	if got := sortIDs(t, cl.cmd("SORT (MODSEQ) UTF-8 ALL")); got != "1 2" {
		t.Fatalf("before store %q", got)
	}
	if got := sortIDs(t, cl.cmd("UID SORT (MODSEQ) UTF-8 ALL")); got != "2 3" {
		t.Fatalf("uid before store %q", got)
	}
	cl.cmd(`STORE 1 +FLAGS (\Seen)`).ok(t)
	if got := sortIDs(t, cl.cmd("SORT (MODSEQ) UTF-8 ALL")); got != "2 1" {
		t.Fatalf("after store %q", got)
	}
	if got := sortIDs(t, cl.cmd("SORT (REVERSE MODSEQ) UTF-8 ALL")); got != "1 2" {
		t.Fatalf("reverse %q", got)
	}
	if got := sortIDs(t, cl.cmd("UID SORT (MODSEQ) UTF-8 ALL")); got != "3 2" {
		t.Fatalf("uid after store %q", got)
	}
	if got := sortIDs(t, cl.cmd("UID SORT (REVERSE MODSEQ) UTF-8 ALL")); got != "2 3" {
		t.Fatalf("uid reverse %q", got)
	}
	for _, line := range []string{
		"SORT (MODSEQ FROM) UTF-8 ALL",
		"SORT (UID) UTF-8 ALL",
		"SORT (REVERSE UID) UTF-8 ALL",
	} {
		if cl.cmd(line).status != "BAD" {
			t.Fatalf("%s was accepted", line)
		}
	}
}

func TestSortUIDTie(t *testing.T) {
	check := func(pers, box string) {
		t.Helper()
		_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: pers})
		cl := dial(t, bound.Listen)
		cl.cmd("LOGIN demo secret").ok(t)
		cl.cmd("CREATE " + box).ok(t)
		cl.cmd("SELECT " + box).ok(t)
		raw := sortMsg("a@e.com", "b@e.com", "", "m", "Mon, 01 Jan 2020 00:00:00 +0000", "x")
		for i := 0; i < 2; i++ {
			cl.cmd(fmt.Sprintf("APPEND %s \"01-Jan-2020 00:00:00 +0000\" {%d+}\r\n%s", box, len(raw), raw)).ok(t)
		}
		if got := sortIDs(t, cl.cmd("SORT (ARRIVAL) UTF-8 ALL")); got != "1 2" {
			t.Fatalf("%s arrival %q", pers, got)
		}
		if got := sortIDs(t, cl.cmd("SORT (REVERSE ARRIVAL) UTF-8 ALL")); got != "1 2" {
			t.Fatalf("%s reverse arrival %q", pers, got)
		}
		if got := sortIDs(t, cl.cmd("UID SORT (REVERSE ARRIVAL) UTF-8 ALL")); got != "1 2" {
			t.Fatalf("%s uid reverse %q", pers, got)
		}
	}
	check("personalities/sort.json", "PlainArr")
	check("personalities/cyrus-3137.json", "UidArr")
}

func TestSortExtensionFiles(t *testing.T) {
	both := func(file, modAfter, uidAfter string) {
		t.Helper()
		p, err := loadPersonality(file)
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "SORT=MODSEQ") || listed(p.GreetingCaps, "SORT=UID") {
			t.Fatalf("%s greeting lists a sort extension", file)
		}
		for _, list := range [][]string{p.PreLogin, p.PostLogin} {
			if capAt(list, "SORT=MODSEQ") != capAt(list, modAfter)+1 {
				t.Fatalf("%s SORT=MODSEQ not after %s", file, modAfter)
			}
			if uidAfter == "" {
				if listed(list, "SORT=UID") {
					t.Fatalf("%s lists SORT=UID", file)
				}
				continue
			}
			if capAt(list, "SORT=UID") != capAt(list, uidAfter)+1 {
				t.Fatalf("%s SORT=UID not after %s", file, uidAfter)
			}
		}
	}
	both("personalities/cyrus-24.json", "SORT", "")
	both("personalities/cyrus-25.json", "SORT", "SORT=DISPLAY")
	both("personalities/cyrus-36.json", "SCAN", "SORT=MODSEQ")
	for _, file := range []string{
		"personalities/cyrus-3124.json",
		"personalities/cyrus-3137.json",
	} {
		both(file, "WITHIN", "SORT=MODSEQ")
	}
	sasl, err := loadPersonality("personalities/sasl.json")
	if err != nil {
		t.Fatal(err)
	}
	if listed(sasl.GreetingCaps, "SORT=MODSEQ") || listed(sasl.GreetingCaps, "SORT=UID") ||
		listed(sasl.PreLogin, "SORT=MODSEQ") || listed(sasl.PreLogin, "SORT=UID") {
		t.Fatal("sasl lists a sort extension before login")
	}
	if capAt(sasl.PostLogin, "SORT=UID") != capAt(sasl.PostLogin, "SORT=MODSEQ")+1 {
		t.Fatal("sasl post-login order")
	}
	cyrus, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	if !sameCapSet(dropCyrusLab(cyrus.PostLogin), sasl.PostLogin) {
		t.Fatal("sasl post-login set drifted from cyrus-3137")
	}
	allow := map[string]bool{
		"cyrus-24.json": true, "cyrus-25.json": true, "cyrus-36.json": true,
		"cyrus-3124.json": true, "cyrus-3137.json": true, "sasl.json": true,
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
		if listed(p.GreetingCaps, "SORT=MODSEQ") || listed(p.PreLogin, "SORT=MODSEQ") || listed(p.PostLogin, "SORT=MODSEQ") ||
			listed(p.GreetingCaps, "SORT=UID") || listed(p.PreLogin, "SORT=UID") || listed(p.PostLogin, "SORT=UID") {
			t.Fatalf("%s gained a sort extension", base)
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "SORT=MODSEQ") || strings.Contains(string(gaps), "SORT=UID") {
		t.Fatal("gaps still lists a sort extension")
	}
	for _, tok := range []string{"ACL", "URL-PARTIAL", "CATENATE"} {
		if !strings.Contains(string(gaps), `"`+tok+`"`) {
			t.Fatalf("gaps dropped %s", tok)
		}
	}
}

func sortMsg(from, to, cc, subj, date, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\n", from, to)
	if cc != "" {
		fmt.Fprintf(&b, "Cc: %s\r\n", cc)
	}
	fmt.Fprintf(&b, "Subject: %s\r\nDate: %s\r\n\r\n%s", subj, date, body)
	return b.String()
}

func sortIDs(t *testing.T, rp *reply) string {
	t.Helper()
	if rp.status != "OK" || len(rp.untagged) != 1 || !strings.HasPrefix(rp.untagged[0], "* SORT") {
		t.Fatalf("sort reply %+v", rp)
	}
	return strings.TrimSpace(strings.TrimPrefix(rp.untagged[0], "* SORT"))
}
