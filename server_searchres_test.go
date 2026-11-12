package main

import (
	"os"
	"strings"
	"testing"
)

func TestSearchResLists(t *testing.T) {
	if !capImplemented("SEARCHRES") {
		t.Fatal("SEARCHRES is not implemented")
	}
	check := func(file, preAfter, postAfter string, pre, post bool) {
		t.Helper()
		p, err := loadPersonality(file)
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "SEARCHRES") {
			t.Fatalf("%s greeting lists SEARCHRES", file)
		}
		if pre {
			if i, j := capAt(p.PreLogin, "SEARCHRES"), capAt(p.PreLogin, preAfter); i < 0 || j < 0 || i != j+1 {
				t.Fatalf("%s pre-login SEARCHRES not after %s: %v", file, preAfter, p.PreLogin)
			}
		} else if listed(p.PreLogin, "SEARCHRES") {
			t.Fatalf("%s pre-login lists SEARCHRES", file)
		}
		if post {
			if i, j := capAt(p.PostLogin, "SEARCHRES"), capAt(p.PostLogin, postAfter); i < 0 || j < 0 || i != j+1 {
				t.Fatalf("%s post-login SEARCHRES not after %s: %v", file, postAfter, p.PostLogin)
			}
		} else if listed(p.PostLogin, "SEARCHRES") {
			t.Fatalf("%s post-login lists SEARCHRES", file)
		}
	}
	check("personalities/cyrus-3124.json", "SEARCH=FUZZY", "SEARCH=FUZZY", true, true)
	check("personalities/cyrus-3137.json", "SEARCH=FUZZY", "SEARCH=FUZZY", true, true)
	check("personalities/dovecot-12.json", "ESORT", "ESORT", true, true)
	check("personalities/dovecot-22.json", "", "ESORT", false, true)
	check("personalities/dovecot-245.json", "", "ESORT", false, true)
	check("personalities/dovecot-dev.json", "", "ESORT", false, true)
	for _, file := range []string{
		"personalities/stalwart.json",
		"personalities/stalwart-01625.json",
		"personalities/stalwart-dev.json",
		"personalities/stalwart-0155.json",
	} {
		check(file, "", "WITHIN", false, true)
	}
	// Zimbra lists SEARCHRES immediately before SORT, so the previous token differs by list.
	z, err := loadPersonality("personalities/zimbra-883.json")
	if err != nil {
		t.Fatal(err)
	}
	if listed(z.GreetingCaps, "SEARCHRES") {
		t.Fatal("zimbra greeting lists SEARCHRES")
	}
	for _, caps := range [][]string{z.PreLogin, z.PostLogin} {
		i, j := capAt(caps, "SEARCHRES"), capAt(caps, "SORT")
		if i < 0 || j < 0 || i+1 != j {
			t.Fatalf("zimbra SEARCHRES not before SORT: %v", caps)
		}
	}
	sasl, err := loadPersonality("personalities/sasl.json")
	if err != nil {
		t.Fatal(err)
	}
	if listed(sasl.GreetingCaps, "SEARCHRES") || listed(sasl.PreLogin, "SEARCHRES") || !listed(sasl.PostLogin, "SEARCHRES") {
		t.Fatal("sasl SEARCHRES is not post-login only")
	}
	for _, file := range []string{
		"personalities/cyrus-22.json",
		"personalities/cyrus-24.json",
		"personalities/cyrus-25.json",
		"personalities/cyrus-36.json",
		"personalities/courier-50.json",
		"personalities/courier-606.json",
		"personalities/dovecot-10.json",
		"personalities/uwimap.json",
	} {
		check(file, "", "", false, false)
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "SEARCHRES") {
		t.Fatal("gaps still lists SEARCHRES")
	}
	for _, tok := range []string{"ACL", "QUOTA", "URLAUTH=BINARY", "XLIST"} {
		if !strings.Contains(string(gaps), `"`+tok+`"`) {
			t.Fatalf("gaps dropped %s", tok)
		}
	}
}

func capAt(caps []string, name string) int {
	for i, c := range caps {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}

func TestSearchResRequiresCaps(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/esearch.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if cl.cmd("SEARCH RETURN (SAVE) ALL").status != "BAD" {
		t.Fatal("SAVE without SEARCHRES was accepted")
	}
	if cl.cmd("SEARCH RETURN (SAVE ALL) ALL").status != "BAD" {
		t.Fatal("SAVE ALL without SEARCHRES was accepted")
	}
	if cl.cmd("FETCH $ (UID)").status != "BAD" {
		t.Fatal("FETCH $ without SEARCHRES was accepted")
	}
	if cl.cmd("SEARCH $").status != "BAD" {
		t.Fatal("SEARCH $ without SEARCHRES was accepted")
	}
	all := cl.cmd("SEARCH RETURN (ALL) ALL")
	tag, line := oneEsearch(t, all)
	if line != `* ESEARCH (TAG "`+tag+`") ALL 1:2` {
		t.Fatalf("return all %q", line)
	}

	_, bound = start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if cl.cmd("SEARCH RETURN (SAVE) ALL").status != "BAD" {
		t.Fatal("SAVE without ESEARCH was accepted")
	}
	if cl.cmd("SEARCH RETURN (ALL) ALL").status != "BAD" {
		t.Fatal("RETURN without ESEARCH was accepted")
	}
	if cl.cmd("FETCH $ (UID)").status != "BAD" {
		t.Fatal("FETCH $ without SEARCHRES was accepted")
	}
}

func TestSearchResSave(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"SEARCHRES", "ESEARCH", "ESORT", "SORT"})
	cl.cmd("SELECT INBOX").ok(t)

	saved := cl.cmd("SEARCH RETURN (SAVE) ALL")
	if saved.status != "OK" || len(saved.untagged) != 0 {
		t.Fatalf("save only %+v", saved)
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "1 2" {
		t.Fatalf("fetch saved %q", got)
	}

	tag, line := oneEsearch(t, cl.cmd("SEARCH RETURN (SAVE ALL) ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") ALL 1:2` {
		t.Fatalf("save all %q", line)
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "1 2" {
		t.Fatalf("after save all %q", got)
	}
	tag, line = oneEsearch(t, cl.cmd("SEARCH RETURN (SAVE COUNT) ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") COUNT 2` {
		t.Fatalf("save count %q", line)
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "1 2" {
		t.Fatalf("count saved the full match, got %q", got)
	}
	if ids := strings.Join(searchIDs(t, cl.cmd("SEARCH ALL")), " "); ids != "1 2" {
		t.Fatalf("plain search %s", ids)
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "1 2" {
		t.Fatalf("search without SAVE changed the set to %q", got)
	}

	sortSave := cl.cmd("SORT RETURN (SAVE) (ARRIVAL) UTF-8 ALL")
	if sortSave.status != "OK" || len(sortSave.untagged) != 0 {
		t.Fatalf("sort save %+v", sortSave)
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "1 2" {
		t.Fatalf("sort save %q", got)
	}
	plainSort := cl.cmd("SORT (ARRIVAL) UTF-8 ALL")
	plainSort.ok(t)
	if len(plainSort.untagged) != 1 || !strings.HasPrefix(plainSort.untagged[0], "* SORT") {
		t.Fatalf("sort %+v", plainSort)
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "1 2" {
		t.Fatalf("sort without SAVE changed the set to %q", got)
	}
	tag, line = oneEsearch(t, cl.cmd("SORT RETURN (SAVE COUNT) (ARRIVAL) UTF-8 ALL"))
	if line != `* ESEARCH (TAG "`+tag+`") COUNT 2` {
		t.Fatalf("sort save count %q", line)
	}
	if cl.cmd("SEARCH RETURN (SAVE PARTIAL 1:1) ALL").status != "BAD" {
		t.Fatal("SAVE with PARTIAL was accepted")
	}
	if cl.cmd("SORT RETURN (PARTIAL 1:1 SAVE) (ARRIVAL) UTF-8 ALL").status != "BAD" {
		t.Fatal("SORT SAVE with PARTIAL was accepted")
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "1 2" {
		t.Fatalf("bad partial changed the set to %q", got)
	}
	if cl.cmd("THREAD RETURN (SAVE) REFERENCES UTF-8 ALL").status != "BAD" {
		t.Fatal("THREAD parsed RETURN")
	}
	th := cl.cmd("THREAD REFERENCES UTF-8 ALL")
	th.ok(t)
	if len(th.untagged) != 1 || !strings.HasPrefix(th.untagged[0], "* THREAD") {
		t.Fatalf("thread %+v", th)
	}
	if cl.cmd("ESEARCH IN (personal) RETURN (SAVE) ALL").status != "BAD" {
		t.Fatal("ESEARCH accepted SAVE")
	}

	// Two messages, then one UID, and a search that does not save leaves that UID.
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "1 2" {
		t.Fatalf("before narrowing %q", got)
	}
	one := cl.cmd("UID SEARCH RETURN (SAVE) UID 2")
	if one.status != "OK" || len(one.untagged) != 0 {
		t.Fatalf("uid save %+v", one)
	}
	if got := fetchUIDs(t, cl.cmd("UID FETCH $ (UID)")); got != "2" {
		t.Fatalf("uid fetch $ %q", got)
	}
	if ids := strings.Join(searchIDs(t, cl.cmd("SEARCH ALL")), " "); ids != "1 2" {
		t.Fatalf("search all %s", ids)
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "2" {
		t.Fatalf("search all cleared the uid save, got %q", got)
	}
	if cl.cmd("SEARCH RETURN (SAVE) BOGUS").status != "BAD" {
		t.Fatal("bogus save was not BAD")
	}
	if cl.cmd("SORT RETURN (SAVE) (ARRIVAL) UTF-8 BOGUS").status != "BAD" {
		t.Fatal("bogus sort save was not BAD")
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "2" {
		t.Fatalf("bad search cleared the set, got %q", got)
	}
	if cl.cmd("FETCH 1,$ (UID)").status != "BAD" || cl.cmd("FETCH 1:$ (UID)").status != "BAD" {
		t.Fatal("$ was accepted inside a sequence set")
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "2" {
		t.Fatalf("bad sequence changed the set to %q", got)
	}
	cl.cmd(`STORE $ +FLAGS (\Deleted)`).ok(t)
	cl.cmd("EXPUNGE").ok(t)
	gone := cl.cmd("FETCH $ (UID)")
	gone.ok(t)
	if fetchUIDs(t, gone) != "" {
		t.Fatalf("expunged message still fetched %q", gone.untagged)
	}
	cl.cmd("SELECT INBOX").ok(t)
	after := cl.cmd("FETCH $ (UID)")
	after.ok(t)
	if fetchUIDs(t, after) != "" {
		t.Fatalf("select left a saved set %q", after.untagged)
	}

	cl.cmd("SELECT INBOX/Generated").ok(t)
	if ids := strings.Join(searchIDs(t, cl.cmd("UID SEARCH ALL")), " "); ids != "1 2 3" {
		t.Fatalf("generated uids %s", ids)
	}
	gen := cl.cmd("SEARCH RETURN (SAVE) ALL")
	if gen.status != "OK" || len(gen.untagged) != 0 {
		t.Fatalf("generated save %+v", gen)
	}
	if got := fetchUIDs(t, cl.cmd("FETCH $ (UID)")); got != "1 2 3" {
		t.Fatalf("generated fetch %q", got)
	}
	cl.cmd("EXAMINE INBOX/Generated").ok(t)
	cleared := cl.cmd("FETCH $ (UID)")
	cleared.ok(t)
	if fetchUIDs(t, cleared) != "" {
		t.Fatalf("examine left a saved set %q", cleared.untagged)
	}
	if ids := strings.Join(searchIDs(t, cl.cmd("SEARCH ALL")), " "); ids != "1 2 3" {
		t.Fatalf("examine dropped messages %s", ids)
	}

	cl.cmd("SELECT INBOX/Generated").ok(t)
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).ok(t)
	cl.cmd("EXPUNGE").ok(t)
	// Sequence 1 is now UID 2. SAVE must store the UID, not the sequence number.
	seqSave := cl.cmd("SEARCH RETURN (SAVE) UID 2")
	if seqSave.status != "OK" || len(seqSave.untagged) != 0 {
		t.Fatalf("seq save %+v", seqSave)
	}
	uidHit := cl.cmd("UID FETCH $ (UID)")
	if fetchUIDs(t, uidHit) != "2" {
		t.Fatalf("saved value was not the UID: %q", uidHit.untagged)
	}
	seqHit := cl.cmd("FETCH $ (UID)")
	if fetchUIDs(t, seqHit) != "2" || len(seqHit.untagged) != 1 || !strings.HasPrefix(seqHit.untagged[0], "* 1 FETCH ") {
		t.Fatalf("non-uid $ %q", seqHit.untagged)
	}
	if ids := strings.Join(searchIDs(t, cl.cmd("SEARCH $")), " "); ids != "1" {
		t.Fatalf("search $ %s", ids)
	}
	if ids := strings.Join(searchIDs(t, cl.cmd("UID SEARCH $")), " "); ids != "2" {
		t.Fatalf("uid search $ %s", ids)
	}

	empty := cl.cmd("SEARCH RETURN (SAVE) FROM nosuch@no.example")
	if empty.status != "OK" || len(empty.untagged) != 0 {
		t.Fatalf("empty save %+v", empty)
	}
	none := cl.cmd("FETCH $ (UID)")
	none.ok(t)
	if fetchUIDs(t, none) != "" {
		t.Fatalf("empty $ fetched %q", none.untagged)
	}
	if ids := searchIDs(t, cl.cmd("SEARCH $")); len(ids) != 0 {
		t.Fatalf("empty search $ %v", ids)
	}
}

func fetchUIDs(t *testing.T, rp *reply) string {
	t.Helper()
	if rp.status != "OK" {
		t.Fatalf("fetch %s %s", rp.status, rp.tagLine)
	}
	var ids []string
	for _, line := range rp.untagged {
		if !strings.Contains(line, " FETCH ") {
			continue
		}
		const mark = "UID "
		i := strings.Index(line, mark)
		if i < 0 {
			t.Fatalf("fetch line %q", line)
		}
		rest := line[i+len(mark):]
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end == 0 {
			t.Fatalf("fetch line %q", line)
		}
		ids = append(ids, rest[:end])
	}
	return strings.Join(ids, " ")
}
