package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func messageLimitPersonality(t *testing.T, n string) string {
	t.Helper()
	p, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PostLogin = append(p.PostLogin, "MESSAGELIMIT="+n, "XMOVE")
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendBodies(t *testing.T, cl *imapClient, box string, bodies []string) {
	t.Helper()
	cl.cmd("CREATE " + box).mustOK(t)
	for _, b := range bodies {
		cl.cmd("APPEND " + box + " " + imapLit(b)).mustOK(t)
	}
}

func limitCode(tag, n, uid string, withUID bool) bool {
	want := "[MESSAGELIMIT " + n
	if withUID {
		want += " " + uid
	}
	want += "]"
	if !strings.Contains(tag, want) {
		return false
	}
	if !withUID && strings.Contains(tag, "[MESSAGELIMIT "+n+" ") {
		return false
	}
	return true
}

func requireLimit(t *testing.T, rp *reply, n, uid string, withUID bool) {
	t.Helper()
	if !limitCode(rp.tagLine, n, uid, withUID) {
		t.Fatalf("limit code n=%s uid=%q with=%v in %s", n, uid, withUID, rp.tagLine)
	}
}

func rejectLimit(t *testing.T, rp *reply) {
	t.Helper()
	if strings.Contains(rp.tagLine, "MESSAGELIMIT") {
		t.Fatalf("unexpected limit %s", rp.tagLine)
	}
}

func fetchSeqs(lines []string) []string {
	var out []string
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) >= 3 && f[0] == "*" && f[2] == "FETCH" {
			out = append(out, f[1])
		}
	}
	return out
}

func expungeSeqs(lines []string) []string {
	var out []string
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) == 3 && f[0] == "*" && f[2] == "EXPUNGE" {
			out = append(out, f[1])
		}
	}
	return out
}

func TestMessageLimitToken(t *testing.T) {
	if !capImplemented("MESSAGELIMIT=2") || !capImplemented("MESSAGELIMIT=1000000") {
		t.Fatal("positive MESSAGELIMIT is not implemented")
	}
	for _, bad := range []string{"MESSAGELIMIT", "MESSAGELIMIT=0", "MESSAGELIMIT=10x", "MESSAGELIMIT=", "MESSAGELIMIT=12abc", "SAVELIMIT", "SAVELIMIT=2", "SAVELIMIT=1000000"} {
		if capImplemented(bad) {
			t.Fatalf("%s is implemented", bad)
		}
	}
	if _, ok := parseMessageLimit("MESSAGELIMIT=18446744073709551616"); ok {
		t.Fatal("overflow MESSAGELIMIT is implemented")
	}
	n, ok := parseMessageLimit("MESSAGELIMIT=2")
	if !ok || n != 2 {
		t.Fatalf("parse %d %v", n, ok)
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "SAVELIMIT") || strings.Contains(string(gaps), "MESSAGELIMIT") {
		t.Fatal("gaps lists SAVELIMIT or MESSAGELIMIT")
	}
}

func TestMessageLimitFiles(t *testing.T) {
	matches, err := filepath.Glob("personalities/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range matches {
		if strings.HasSuffix(f, ".gaps.json") {
			continue
		}
		p, err := loadPersonality(f)
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Base(f)
		wantPost := base == "stalwart.json" || base == "stalwart-01625.json" || base == "stalwart-dev.json"
		if listed(p.PostLogin, "MESSAGELIMIT=1000000") != wantPost {
			t.Fatalf("%s post-login MESSAGELIMIT=%v", base, !wantPost)
		}
		if listed(p.GreetingCaps, "MESSAGELIMIT=1000000") || listed(p.PreLogin, "MESSAGELIMIT=1000000") {
			t.Fatalf("%s lists MESSAGELIMIT before login", base)
		}
		for _, c := range append(append([]string{}, p.GreetingCaps...), append(p.PreLogin, p.PostLogin...)...) {
			if strings.HasPrefix(strings.ToUpper(c), "MESSAGELIMIT") && c != "MESSAGELIMIT=1000000" {
				t.Fatalf("%s lists %s", base, c)
			}
			if strings.HasPrefix(strings.ToUpper(c), "SAVELIMIT") {
				t.Fatalf("%s lists %s", base, c)
			}
		}
	}
}

func TestMessageLimitDocs(t *testing.T) {
	facts, err := os.ReadFile("docs/facts/imap-server.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(facts)
	for _, s := range []string{
		"COPY` and `UID COPY",
		"highest UIDs",
		"[MESSAGELIMIT N UID]",
		"[MESSAGELIMIT N]",
		"SAVELIMIT` stays unimplemented",
		"stalwart.json",
		"stalwart-0155.json",
		"UIDAFTER",
		"UIDBEFORE",
		"ESEARCH IN` is not limited",
		"FETCH` `PARTIAL` is not added",
		"EXPUNGE`, `CLOSE`, and `STATUS` are not limited",
	} {
		if !strings.Contains(text, s) {
			t.Fatalf("facts missing %s", s)
		}
	}
	guide, err := os.ReadFile("docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	g := string(guide)
	if !strings.Contains(g, "MESSAGELIMIT=<positive integer>") || !strings.Contains(g, "docs/facts/imap-server.md") || !strings.Contains(g, "SAVELIMIT` stays unimplemented") {
		t.Fatal("guide missing the short MESSAGELIMIT note")
	}
	offAt := strings.Index(g, "## Capabilities that stay off")
	next := strings.Index(g[offAt+1:], "\n## ")
	if offAt < 0 || next < 0 {
		t.Fatal("stay-off section missing")
	}
	off := g[offAt : offAt+1+next]
	if strings.Contains(off, "MESSAGELIMIT") {
		t.Fatal("stay-off names MESSAGELIMIT")
	}
}

func TestMessageLimitWithoutCap(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	if listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "MESSAGELIMIT=2") || strings.Contains(cl.greeting, "MESSAGELIMIT") {
		t.Fatal("default personality lists MESSAGELIMIT")
	}
	appendBodies(t, cl, "Plain", []string{"a", "b", "c", "d"})
	cl.cmd("SELECT Plain").mustOK(t)
	for _, bad := range []string{
		"SEARCH UIDAFTER 1",
		"SEARCH UIDBEFORE 1",
		"UID SEARCH UIDAFTER 1",
		"SORT (ARRIVAL) UTF-8 UIDAFTER 1",
		"THREAD REFERENCES UTF-8 UIDBEFORE 1",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
	rp := cl.cmd("FETCH 1:* (UID)").mustOK(t)
	rejectLimit(t, rp)
	if got := fetchUIDs(t, rp); got != "1 2 3 4" {
		t.Fatalf("fetch %s", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH ALL")), " "); got != "1 2 3 4" {
		t.Fatalf("search %s", got)
	}
	rejectLimit(t, cl.cmd("SEARCH ALL"))
	cl.cmd("CREATE PlainDest").mustOK(t)
	copied := cl.cmd("COPY 1:* PlainDest").mustOK(t)
	rejectLimit(t, copied)
	if got := statusField(t, cl, "PlainDest", "MESSAGES"); got != "4" {
		t.Fatalf("copied %s", got)
	}
	sorted := cl.cmd("SORT (ARRIVAL) UTF-8 ALL").mustOK(t)
	rejectLimit(t, sorted)
	if len(sorted.untagged) != 1 || !strings.HasPrefix(sorted.untagged[0], "* SORT") {
		t.Fatalf("sort %q", sorted.untagged)
	}
}

func TestMessageLimit(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: messageLimitPersonality(t, "2")})
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "MESSAGELIMIT") {
		t.Fatalf("greeting lists MESSAGELIMIT: %q", cl.greeting)
	}
	pre := cl.cmd("CAPABILITY").mustOK(t)
	if listed(capsOf(pre.untagged[0]), "MESSAGELIMIT=2") {
		t.Fatal("pre-login lists MESSAGELIMIT")
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	post := cl.cmd("CAPABILITY").mustOK(t)
	if !listed(capsOf(post.untagged[0]), "MESSAGELIMIT=2") {
		t.Fatal("post-login missing MESSAGELIMIT=2")
	}

	appendBodies(t, cl, "Small", []string{"a", "b"})
	cl.cmd("SELECT Small").mustOK(t)
	exact := cl.cmd("FETCH 1:* (UID)").mustOK(t)
	rejectLimit(t, exact)
	if got := fetchUIDs(t, exact); got != "1 2" {
		t.Fatalf("exact fetch %s", got)
	}
	wide := cl.cmd("FETCH 1:1000 (UID)").mustOK(t)
	rejectLimit(t, wide)
	if got := fetchUIDs(t, wide); got != "1 2" {
		t.Fatalf("sparse width counted as messages: %s", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH UIDAFTER 1")), " "); got != "2" {
		t.Fatalf("UIDAFTER %s", got)
	}
	rejectLimit(t, cl.cmd("SEARCH UIDAFTER 1"))
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH UIDBEFORE 1")), " "); got != "" {
		t.Fatalf("UIDBEFORE 1 %q", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH UIDAFTER 0")), " "); got != "1 2" {
		t.Fatalf("UIDAFTER 0 %s", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH UIDBEFORE 2")), " "); got != "1" {
		t.Fatalf("UIDBEFORE 2 %s", got)
	}
	for _, bad := range []string{
		"SEARCH UIDAFTER",
		"SEARCH UIDBEFORE",
		"SEARCH UIDAFTER abc",
		"SEARCH UIDBEFORE 1x",
		"SEARCH UIDAFTER -1",
		"SEARCH UIDAFTER +1",
		"UID SEARCH UIDBEFORE",
		"SORT (ARRIVAL) UTF-8 UIDAFTER x",
		"THREAD REFERENCES UTF-8 UIDBEFORE",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
	if got := sortIDs(t, cl.cmd("SORT (ARRIVAL) UTF-8 UIDAFTER 1")); got != "2" {
		t.Fatalf("sort UIDAFTER %s", got)
	}
	if got := threadTree(t, cl.cmd("THREAD REFERENCES UTF-8 UIDBEFORE 1")); got != "" {
		t.Fatalf("thread UIDBEFORE 1 %q", got)
	}
	one := cl.cmd("APPEND Small " + imapLit("z")).mustOK(t)
	rejectLimit(t, one)
	if !strings.Contains(one.tagLine, "[APPENDUID ") {
		t.Fatalf("one append %s", one.tagLine)
	}

	appendBodies(t, cl, "Lim", []string{"a", "b", "c", "d"})
	cl.cmd("SELECT Lim").mustOK(t)
	if got := statusField(t, cl, "Lim", "MESSAGES"); got != "4" {
		t.Fatalf("messages %s", got)
	}
	st := cl.cmd("STATUS Lim (MESSAGES)").mustOK(t)
	rejectLimit(t, st)

	sparse := cl.cmd("UID FETCH 1,100,3 (UID)").mustOK(t)
	rejectLimit(t, sparse)
	if got := fetchUIDs(t, sparse); got != "1 3" {
		t.Fatalf("sparse set %s", got)
	}
	over := cl.cmd("FETCH 1:1000 (UID)").mustOK(t)
	requireLimit(t, over, "2", "3", true)
	if got := fetchUIDs(t, over); got != "4 3" {
		t.Fatalf("fetch highest %s", got)
	}
	uidOver := cl.cmd("UID FETCH 1:* (UID)").mustOK(t)
	requireLimit(t, uidOver, "2", "3", true)
	if got := fetchUIDs(t, uidOver); got != "4 3" {
		t.Fatalf("uid fetch %s", got)
	}

	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH UID 1:2")), " "); got != "1 2" {
		t.Fatalf("small uid set %s", got)
	}
	rejectLimit(t, cl.cmd("SEARCH UID 1:2"))
	all := cl.cmd("SEARCH ALL").mustOK(t)
	requireLimit(t, all, "2", "3", true)
	if got := strings.Join(searchIDs(t, all), " "); got != "4 3" {
		t.Fatalf("search order %s", got)
	}
	uidAll := cl.cmd("UID SEARCH ALL").mustOK(t)
	requireLimit(t, uidAll, "2", "3", true)
	if got := strings.Join(searchIDs(t, uidAll), " "); got != "4 3" {
		t.Fatalf("uid search order %s", got)
	}
	es := cl.cmd("SEARCH RETURN (ALL) ALL").mustOK(t)
	requireLimit(t, es, "2", "3", true)
	if len(es.untagged) != 1 || !strings.Contains(es.untagged[0], "ALL 3:4") {
		t.Fatalf("esearch all %q", es.untagged)
	}
	stats := cl.cmd("UID SEARCH RETURN (COUNT MIN MAX) ALL").mustOK(t)
	requireLimit(t, stats, "2", "3", true)
	if len(stats.untagged) != 1 || !strings.Contains(stats.untagged[0], "COUNT 2") || !strings.Contains(stats.untagged[0], "MIN 3") || !strings.Contains(stats.untagged[0], "MAX 4") {
		t.Fatalf("count %q", stats.untagged)
	}
	saved := cl.cmd("UID SEARCH RETURN (SAVE) ALL").mustOK(t)
	requireLimit(t, saved, "2", "3", true)
	for _, u := range saved.untagged {
		if strings.Contains(u, "SEARCH") {
			t.Fatalf("save sent a result %q", saved.untagged)
		}
	}
	gotSaved := cl.cmd("UID SEARCH $").mustOK(t)
	rejectLimit(t, gotSaved)
	if got := strings.Join(searchIDs(t, gotSaved), " "); got != "3 4" {
		t.Fatalf("saved %s", got)
	}

	cl.cmd("UID STORE 1 +FLAGS (\\Flagged)").mustOK(t)
	low := cl.cmd("UID SEARCH FLAGGED").mustOK(t)
	requireLimit(t, low, "2", "3", true)
	if got := strings.Join(searchIDs(t, low), " "); got != "" {
		t.Fatalf("low flag was examined %s", got)
	}
	stored := cl.cmd("UID STORE 1:* +FLAGS (\\Flagged)").mustOK(t)
	requireLimit(t, stored, "2", "3", true)
	if got := fetchUIDs(t, stored); got != "4 3" {
		t.Fatalf("store order %s", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH UID 1:2 FLAGGED")), " "); got != "1" {
		t.Fatalf("low flags %s", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH UID 3:4 FLAGGED")), " "); got != "3 4" {
		t.Fatalf("high flags %s", got)
	}
	seqStore := cl.cmd("STORE 1:* +FLAGS (\\Seen)").mustOK(t)
	requireLimit(t, seqStore, "2", "3", true)
	if got := strings.Join(fetchSeqs(seqStore.untagged), " "); got != "4 3" {
		t.Fatalf("seq store %s", got)
	}

	sortNo := cl.cmd("SORT (ARRIVAL) UTF-8 ALL")
	if sortNo.status != "NO" || len(sortNo.untagged) != 0 {
		t.Fatalf("sort %s %q", sortNo.tagLine, sortNo.untagged)
	}
	requireLimit(t, sortNo, "2", "", false)
	threadNo := cl.cmd("THREAD REFERENCES UTF-8 ALL")
	if threadNo.status != "NO" || len(threadNo.untagged) != 0 {
		t.Fatalf("thread %s %q", threadNo.tagLine, threadNo.untagged)
	}
	requireLimit(t, threadNo, "2", "", false)

	cl.cmd("CREATE CopyDest").mustOK(t)
	cp := cl.cmd("COPY 1:* CopyDest")
	if cp.status != "NO" {
		t.Fatalf("copy %s", cp.tagLine)
	}
	requireLimit(t, cp, "2", "3", true)
	if got := statusField(t, cl, "CopyDest", "MESSAGES"); got != "0" {
		t.Fatalf("copy stored %s", got)
	}
	uc := cl.cmd("UID COPY 1:* CopyDest")
	if uc.status != "NO" {
		t.Fatalf("uid copy %s", uc.tagLine)
	}
	requireLimit(t, uc, "2", "3", true)
	if got := statusField(t, cl, "CopyDest", "MESSAGES"); got != "0" {
		t.Fatalf("uid copy stored %s", got)
	}

	cl.cmd("CREATE MoveDest").mustOK(t)
	mv := cl.cmd("MOVE 1:* MoveDest").mustOK(t)
	requireLimit(t, mv, "2", "3", true)
	if got := strings.Join(expungeSeqs(mv.untagged), " "); got != "4 3" {
		t.Fatalf("move expunge %s in %q", got, mv.untagged)
	}
	if !hasPrefix(mv.untagged, "* OK [COPYUID ") {
		t.Fatalf("move copyuid %q", mv.untagged)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH ALL")), " "); got != "1 2" {
		t.Fatalf("after move %s", got)
	}
	cl.cmd("SELECT MoveDest").mustOK(t)
	if body := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); body != "d" {
		t.Fatalf("moved first %q", body)
	}
	if body := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 2 (BODY.PEEK[])").mustOK(t)))); body != "c" {
		t.Fatalf("moved second %q", body)
	}

	appendBodies(t, cl, "XSrc", []string{"a", "b", "c", "d"})
	cl.cmd("SELECT XSrc").mustOK(t)
	cl.cmd("CREATE XDest").mustOK(t)
	xm := cl.cmd("XMOVE 1:* XDest").mustOK(t)
	requireLimit(t, xm, "2", "3", true)
	if got := strings.Join(expungeSeqs(xm.untagged), " "); got != "4 3" {
		t.Fatalf("xmove expunge %s", got)
	}
	cl.cmd("SELECT XDest").mustOK(t)
	if got := statusField(t, cl, "XDest", "MESSAGES"); got != "2" {
		t.Fatalf("xmove dest %s", got)
	}

	appendBodies(t, cl, "USrc", []string{"a", "b", "c", "d"})
	cl.cmd("SELECT USrc").mustOK(t)
	cl.cmd("CREATE UDest").mustOK(t)
	um := cl.cmd("UID MOVE 1:* UDest").mustOK(t)
	requireLimit(t, um, "2", "3", true)
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH ALL")), " "); got != "1 2" {
		t.Fatalf("uid move left %s", got)
	}

	esin := cl.cmd("ESEARCH IN (subtree Lim) RETURN (ALL) UIDAFTER 0").mustOK(t)
	rejectLimit(t, esin)
	found := false
	for _, u := range esin.untagged {
		if strings.Contains(u, "Lim") && strings.Contains(u, "ALL 1:2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("esearch in was limited %q", esin.untagged)
	}

	appendBodies(t, cl, "Exp", []string{"a", "b", "c", "d"})
	cl.cmd("SELECT Exp").mustOK(t)
	cl.cmd("STORE 1:2 +FLAGS.SILENT (\\Deleted)").mustOK(t)
	cl.cmd("STORE 3:4 +FLAGS.SILENT (\\Deleted)").mustOK(t)
	plain := cl.cmd("EXPUNGE").mustOK(t)
	rejectLimit(t, plain)
	if got := statusField(t, cl, "Exp", "MESSAGES"); got != "0" {
		t.Fatalf("plain expunge left %s", got)
	}

	appendBodies(t, cl, "UExp", []string{"a", "b", "c", "d"})
	cl.cmd("SELECT UExp").mustOK(t)
	cl.cmd("STORE 1:2 +FLAGS.SILENT (\\Deleted)").mustOK(t)
	cl.cmd("STORE 3:4 +FLAGS.SILENT (\\Deleted)").mustOK(t)
	ue := cl.cmd("UID EXPUNGE 1:*").mustOK(t)
	requireLimit(t, ue, "2", "3", true)
	if got := strings.Join(expungeSeqs(ue.untagged), " "); got != "4 3" {
		t.Fatalf("uid expunge %s in %q", got, ue.untagged)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH ALL")), " "); got != "1 2" {
		t.Fatalf("uid expunge left %s", got)
	}

	appendBodies(t, cl, "Del", []string{"a", "b", "c", "d"})
	cl.cmd("SELECT Del").mustOK(t)
	cl.cmd("UID STORE 1,2 +FLAGS.SILENT (\\Deleted)").mustOK(t)
	cl.cmd("UID STORE 4 +FLAGS.SILENT (\\Deleted)").mustOK(t)
	de := cl.cmd("UID EXPUNGE 1:*").mustOK(t)
	requireLimit(t, de, "2", "2", true)
	if got := strings.Join(expungeSeqs(de.untagged), " "); got != "4 2" {
		t.Fatalf("partial deleted expunge %s", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH ALL")), " "); got != "1 3" {
		t.Fatalf("partial deleted left %s", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH DELETED")), " "); got != "1" {
		t.Fatalf("deleted left %s", got)
	}

	appendBodies(t, cl, "Closing", []string{"a", "b", "c", "d"})
	cl.cmd("SELECT Closing").mustOK(t)
	cl.cmd("STORE 1:2 +FLAGS.SILENT (\\Deleted)").mustOK(t)
	cl.cmd("STORE 3:4 +FLAGS.SILENT (\\Deleted)").mustOK(t)
	closed := cl.cmd("CLOSE").mustOK(t)
	rejectLimit(t, closed)
	cl.cmd("SELECT Closing").mustOK(t)
	if got := statusField(t, cl, "Closing", "MESSAGES"); got != "0" {
		t.Fatalf("close left %s", got)
	}

	cl.cmd("CREATE App").mustOK(t)
	if got := statusField(t, cl, "App", "UIDNEXT"); got != "1" {
		t.Fatalf("uidnext %s", got)
	}
	tooMany := cl.cmd("APPEND App " + imapLit("a") + " " + imapLit("b") + " " + imapLit("c"))
	if tooMany.status != "NO" {
		t.Fatalf("multiappend %s", tooMany.tagLine)
	}
	requireLimit(t, tooMany, "2", "", false)
	if got := statusField(t, cl, "App", "MESSAGES"); got != "0" {
		t.Fatalf("multiappend stored %s", got)
	}
	if got := statusField(t, cl, "App", "UIDNEXT"); got != "1" {
		t.Fatalf("multiappend uidnext %s", got)
	}
	two := cl.cmd("APPEND App " + imapLit("a") + " " + imapLit("b")).mustOK(t)
	rejectLimit(t, two)
	if got := statusField(t, cl, "App", "MESSAGES"); got != "2" {
		t.Fatalf("two append %s", got)
	}
	part := cl.cmd("FETCH 1 (UID) (PARTIAL -1:-5)")
	if part.status != "BAD" {
		t.Fatalf("FETCH PARTIAL %s %s", part.status, part.tagLine)
	}
}
