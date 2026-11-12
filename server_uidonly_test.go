package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func wantUIDRequired(t *testing.T, rp *reply) {
	t.Helper()
	const text = "[UIDREQUIRED] Message numbers are not allowed once UIDONLY is enabled"
	if rp.status != "BAD" || rp.text != text {
		t.Fatalf("got %s %q untagged %q", rp.status, rp.tagLine, rp.untagged)
	}
}

func uidFetchLine(t *testing.T, rp *reply) string {
	t.Helper()
	for _, u := range rp.untagged {
		if strings.Contains(u, " UIDFETCH (") {
			return u
		}
	}
	t.Fatalf("no UIDFETCH in %q", rp.untagged)
	return ""
}

func joined(lines []string) string {
	return strings.Join(lines, "\n")
}

func capNext(list []string, name string) string {
	for i, c := range list {
		if strings.EqualFold(c, name) && i+1 < len(list) {
			return list[i+1]
		}
	}
	return ""
}

func capPrev(list []string, name string) string {
	for i, c := range list {
		if strings.EqualFold(c, name) && i > 0 {
			return list[i-1]
		}
	}
	return ""
}

func TestUIDONLYLists(t *testing.T) {
	if _, ok := implementedCaps["UIDONLY"]; !ok || !capImplemented("UIDONLY") {
		t.Fatal("UIDONLY is not implemented")
	}
	both := []string{"cyrus-3124.json", "cyrus-3137.json"}
	postOnly := []string{"sasl.json"}
	stal := []string{"stalwart.json", "stalwart-01625.json", "stalwart-dev.json"}
	allow := map[string]bool{}
	for _, name := range append(append(append([]string{}, both...), postOnly...), stal...) {
		allow[name] = true
	}
	for _, name := range both {
		p, err := loadPersonality(filepath.Join("personalities", name))
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "UIDONLY") || capNext(p.PreLogin, "UIDONLY") != "UIDPLUS" || capNext(p.PostLogin, "UIDONLY") != "UIDPLUS" {
			t.Fatalf("%s placement greet %v pre %v post %v", name, p.GreetingCaps, p.PreLogin, p.PostLogin)
		}
	}
	sasl, err := loadPersonality("personalities/sasl.json")
	if err != nil {
		t.Fatal(err)
	}
	if listed(sasl.GreetingCaps, "UIDONLY") || listed(sasl.PreLogin, "UIDONLY") || capNext(sasl.PostLogin, "UIDONLY") != "UIDPLUS" {
		t.Fatal("sasl UIDONLY placement")
	}
	cyrus, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	if !sameCapSet(dropCap(cyrus.PostLogin, "SEARCH=FUZZY"), sasl.PostLogin) {
		t.Fatal("sasl post-login set drifted from cyrus-3137")
	}
	for _, name := range stal {
		p, err := loadPersonality(filepath.Join("personalities", name))
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "UIDONLY") || listed(p.PreLogin, "UIDONLY") || capPrev(p.PostLogin, "UIDONLY") != "QUOTA=RES-STORAGE" || capNext(p.PostLogin, "UIDONLY") != "MESSAGELIMIT=1000000" {
			t.Fatalf("%s placement", name)
		}
	}
	old, err := loadPersonality("personalities/stalwart-0155.json")
	if err != nil {
		t.Fatal(err)
	}
	if listed(old.GreetingCaps, "UIDONLY") || listed(old.PreLogin, "UIDONLY") || listed(old.PostLogin, "UIDONLY") {
		t.Fatal("stalwart-0155 gained UIDONLY")
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".gaps.json") {
			continue
		}
		p, err := loadPersonality(filepath.Join("personalities", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got := listed(p.GreetingCaps, "UIDONLY") || listed(p.PreLogin, "UIDONLY") || listed(p.PostLogin, "UIDONLY")
		if got != allow[e.Name()] {
			t.Fatalf("%s UIDONLY=%v", e.Name(), got)
		}
		if strings.Contains(p.Greeting, "UIDONLY") {
			t.Fatalf("%s greeting text changed", e.Name())
		}
	}
	gapsRaw, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	gapsText := string(gapsRaw)
	if strings.Contains(gapsText, "UIDONLY") {
		t.Fatal("gaps still lists UIDONLY")
	}
	for _, keep := range []string{"ACL", "UIDBATCHES", "IMAP4rev2", "URLAUTH", "X-SIEVE-MAILBOX"} {
		if !strings.Contains(gapsText, `"`+keep+`"`) {
			t.Fatalf("gaps dropped %s", keep)
		}
	}
	facts, err := os.ReadFile("docs/facts/imap-server.md")
	if err != nil {
		t.Fatal(err)
	}
	factText := string(facts)
	for _, phrase := range []string{
		"ENABLE UIDONLY",
		"[UIDREQUIRED] Message numbers are not allowed once UIDONLY is enabled",
		"UIDFETCH",
		"VANISHED",
		"immediately before `UIDPLUS`",
		"stalwart-0155.json` does not list it",
		"never sends `UIDNOTSTICKY`",
	} {
		if !strings.Contains(factText, phrase) {
			t.Fatalf("facts missing %s", phrase)
		}
	}
	guideRaw, err := os.ReadFile("docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := string(guideRaw)
	if !strings.Contains(guide, "ENABLE UIDONLY") || !strings.Contains(guide, "UIDFETCH") || !strings.Contains(guide, "VANISHED") || !strings.Contains(guide, "docs/facts/imap-server.md") {
		t.Fatal("guide missing the short UIDONLY note")
	}
	if strings.Contains(guide, "A `*` anywhere in a message set") || strings.Contains(guide, "formatUIDSet") {
		t.Fatal("guide pastes the facts file")
	}
	off := strings.Index(guide, "## Capabilities that stay off")
	if off < 0 {
		t.Fatal("stay-off section missing")
	}
	stay := guide[off:]
	tokens := strings.Index(stay, "the tokens are")
	if tokens < 0 || strings.Contains(stay[tokens:], "`UIDONLY`") {
		t.Fatal("stay-off still lists UIDONLY")
	}
}

func TestUIDONLYBeforeEnable(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	if listed(capsOf(cl.greeting), "UIDONLY") {
		t.Fatalf("greeting lists UIDONLY: %s", cl.greeting)
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	post := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	if !listed(post, "UIDONLY") {
		t.Fatal("post-login missing UIDONLY")
	}
	appendBodies(t, cl, "Pre", []string{"one", "two"})
	cl.cmd("SELECT Pre").mustOK(t)
	got := cl.cmd("FETCH 1 (FLAGS)").mustOK(t)
	line := fetchLine(t, got)
	if strings.Contains(line, "UIDFETCH") || !strings.Contains(line, " FETCH (") {
		t.Fatalf("fetch before enable %s", line)
	}
	stored := cl.cmd(`STORE 1 +FLAGS (\Flagged)`).mustOK(t)
	if strings.Contains(joined(stored.untagged), "UIDFETCH") {
		t.Fatalf("store before enable %v", stored.untagged)
	}
	if ids := strings.Join(searchIDs(t, cl.cmd("SEARCH ALL")), " "); ids != "1 2" {
		t.Fatalf("search before enable %s", ids)
	}
	cl.cmd("CREATE PreCopy").mustOK(t)
	copied := cl.cmd("COPY 1 PreCopy").mustOK(t)
	if !strings.Contains(copied.tagLine, "[COPYUID ") || strings.Contains(joined(copied.untagged), "VANISHED") {
		t.Fatalf("copy before enable %s %v", copied.tagLine, copied.untagged)
	}
	moved := cl.cmd("MOVE 2 PreCopy").mustOK(t)
	moveText := joined(moved.untagged)
	if !strings.Contains(moveText, "EXPUNGE") || strings.Contains(moveText, "VANISHED") || !strings.Contains(moveText, "* OK [COPYUID ") || strings.Contains(moved.tagLine, "COPYUID") {
		t.Fatalf("move before enable %s %v", moved.tagLine, moved.untagged)
	}
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).mustOK(t)
	ex := cl.cmd("EXPUNGE").mustOK(t)
	if !strings.Contains(joined(ex.untagged), "EXPUNGE") || strings.Contains(joined(ex.untagged), "VANISHED") {
		t.Fatalf("expunge before enable %v", ex.untagged)
	}
	bare := cl.cmd("UID SEARCH 1")
	if bare.status != "BAD" || strings.Contains(bare.tagLine, "UIDREQUIRED") {
		t.Fatalf("bare seq before enable %s %s", bare.status, bare.tagLine)
	}
	uidEs := cl.cmd("UID ESEARCH IN (personal) RETURN (COUNT) ALL").mustOK(t)
	plain := cl.cmd("ESEARCH IN (personal) RETURN (COUNT) ALL").mustOK(t)
	if !strings.Contains(joined(uidEs.untagged), " UID ") || !strings.Contains(joined(plain.untagged), " UID ") {
		t.Fatalf("esearch before enable uid %v plain %v", uidEs.untagged, plain.untagged)
	}
}

func TestUIDONLYNotAdvertised(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-22.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	en := cl.cmd("ENABLE UIDONLY").mustOK(t)
	if len(en.untagged) != 0 {
		t.Fatalf("enabled nothing %v", en.untagged)
	}
	cl.cmd("SELECT INBOX").mustOK(t)
	got := cl.cmd("FETCH 1 (FLAGS)").mustOK(t)
	if strings.Contains(joined(got.untagged), "UIDFETCH") || strings.Contains(got.tagLine, "UIDREQUIRED") {
		t.Fatalf("fetch after ignored enable %s %v", got.tagLine, got.untagged)
	}
}

func TestUIDONLYSession(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	en := cl.cmd("ENABLE UIDONLY").mustOK(t)
	if strings.Join(en.untagged, " ") != "* ENABLED UIDONLY" {
		t.Fatalf("enable %v", en.untagged)
	}
	other := dial(t, bound.Listen)
	other.cmd("LOGIN demo secret").mustOK(t)
	appendBodies(t, cl, "Box", []string{"a", "b", "c"})
	sel := cl.cmd("SELECT Box").mustOK(t)
	selText := joined(sel.untagged)
	if !strings.Contains(selText, "* 3 EXISTS") || !strings.Contains(selText, " RECENT") || strings.Contains(selText, "UIDFETCH") || strings.Contains(selText, "UIDNOTSTICKY") {
		t.Fatalf("select %v", sel.untagged)
	}
	added := cl.cmd("APPEND Box " + imapLit("d")).mustOK(t)
	addText := joined(added.untagged)
	if !strings.Contains(addText, "* 4 EXISTS") || !strings.Contains(addText, " RECENT") {
		t.Fatalf("exists %v", added.untagged)
	}
	for _, line := range []string{
		"FETCH 1 (FLAGS)",
		`STORE 1 +FLAGS (\Seen)`,
		"SEARCH ALL",
		"COPY 1 Box",
		"MOVE 1 Box",
		"XMOVE 1 Box",
		"SORT (ARRIVAL) UTF-8 ALL",
		"THREAD REFERENCES UTF-8 ALL",
		"ESEARCH IN (personal) RETURN (ALL) ALL",
	} {
		wantUIDRequired(t, cl.cmd(line))
	}
	for _, line := range []string{
		"UID FETCH 1:* (FLAGS)",
		"UID FETCH * (FLAGS)",
		`UID STORE 1:* +FLAGS (\Seen)`,
		"UID COPY 1:* Box",
		"UID MOVE 2:* Box",
		"UID EXPUNGE 1:*",
		"UID SEARCH *",
		"UID SEARCH UID 1:*",
		"UID SEARCH 1",
		"UID SEARCH (1:2)",
		"UID SEARCH OR 1 ALL",
		"UID SORT (ARRIVAL) UTF-8 1",
		"UID THREAD REFERENCES UTF-8 1:2",
		"UID ESEARCH IN (personal) RETURN (ALL) 1",
		"FILTER SIEVE DELIVERY 1:2",
		"UID FILTER SIEVE DELIVERY 1",
	} {
		wantUIDRequired(t, cl.cmd(line))
	}
	flags := cl.cmd("UID FETCH 4 (FLAGS)").mustOK(t)
	line := uidFetchLine(t, flags)
	if !strings.HasPrefix(line, "* 4 UIDFETCH (") || listed(fetchKeys(t, line), "UID") {
		t.Fatalf("uid fetch %s", line)
	}
	if strings.Contains(joined(flags.untagged), " FETCH (") {
		t.Fatalf("seq fetch leaked %v", flags.untagged)
	}
	asked := cl.cmd("UID FETCH 4 (UID FLAGS)").mustOK(t)
	askedLine := uidFetchLine(t, asked)
	if !listed(fetchKeys(t, askedLine), "UID") || !strings.Contains(askedLine, "UID 4") {
		t.Fatalf("asked uid %s", askedLine)
	}
	kept := cl.cmd("UID SEARCH ALL").mustOK(t)
	if got := strings.Join(searchIDs(t, kept), " "); got != "1 2 3 4" {
		t.Fatalf("uid search all %s", got)
	}
	one := cl.cmd("UID SEARCH UID 4").mustOK(t)
	if got := strings.Join(searchIDs(t, one), " "); got != "4" {
		t.Fatalf("uid key %s", got)
	}
	filt := cl.cmd("FILTER SIEVE DELIVERY ALL")
	if filt.status != "NO" || filt.text != "not implemented" || len(filt.untagged) != 0 {
		t.Fatalf("filter all %s %q %v", filt.status, filt.text, filt.untagged)
	}
	uidFilt := cl.cmd("UID FILTER SIEVE DELIVERY UID 4")
	if uidFilt.status != "NO" || uidFilt.text != "not implemented" {
		t.Fatalf("uid filter %s %q", uidFilt.status, uidFilt.text)
	}
	sorted := cl.cmd("UID SORT (ARRIVAL) UTF-8 ALL").mustOK(t)
	if len(sorted.untagged) != 1 || !strings.HasPrefix(sorted.untagged[0], "* SORT ") {
		t.Fatalf("sort %v", sorted.untagged)
	}
	threaded := cl.cmd("UID THREAD REFERENCES UTF-8 ALL").mustOK(t)
	if len(threaded.untagged) != 1 || !strings.HasPrefix(threaded.untagged[0], "* THREAD ") {
		t.Fatalf("thread %v", threaded.untagged)
	}
	es := cl.cmd("UID ESEARCH IN (personal) RETURN (COUNT) ALL").mustOK(t)
	if !strings.Contains(joined(es.untagged), " UID ") {
		t.Fatalf("uid esearch %v", es.untagged)
	}
	cl.cmd("CREATE Dest").mustOK(t)
	copied := cl.cmd("UID COPY 4 Dest").mustOK(t)
	if !strings.Contains(copied.tagLine, "[COPYUID ") || strings.Contains(joined(copied.untagged), "VANISHED") || strings.Contains(joined(copied.untagged), "EXPUNGE") {
		t.Fatalf("uid copy %s %v", copied.tagLine, copied.untagged)
	}
	cl.cmd(`UID STORE 1 +FLAGS (\Deleted)`).mustOK(t)
	ex := cl.cmd("UID EXPUNGE 1").mustOK(t)
	if joined(ex.untagged) != "* VANISHED 1" {
		t.Fatalf("uid expunge %v", ex.untagged)
	}
	stored := cl.cmd(`UID STORE 4 +FLAGS (\Flagged)`).mustOK(t)
	storeLine := uidFetchLine(t, stored)
	if !strings.HasPrefix(storeLine, "* 4 UIDFETCH (") || listed(fetchKeys(t, storeLine), "UID") {
		t.Fatalf("uid store %s", storeLine)
	}
	other.cmd("SELECT Box").mustOK(t)
	otherFetch := other.cmd("FETCH 1 (FLAGS)").mustOK(t)
	if strings.Contains(joined(otherFetch.untagged), "UIDFETCH") || !strings.Contains(fetchLine(t, otherFetch), " FETCH (") {
		t.Fatalf("other fetch %v", otherFetch.untagged)
	}
	other.cmd(`STORE 1 +FLAGS (\Answered)`).mustOK(t)
	unsol := cl.cmd("NOOP").mustOK(t)
	unsolLine := uidFetchLine(t, unsol)
	if !strings.HasPrefix(unsolLine, "* 2 UIDFETCH (") || listed(fetchKeys(t, unsolLine), "UID") {
		t.Fatalf("unsolicited %s", unsolLine)
	}
	cl.cmd(`UID STORE 2 +FLAGS (\Deleted)`).mustOK(t)
	cl.cmd(`UID STORE 4 +FLAGS (\Deleted)`).mustOK(t)
	both := cl.cmd("EXPUNGE").mustOK(t)
	if joined(both.untagged) != "* VANISHED 2,4" {
		t.Fatalf("expunge runs %v", both.untagged)
	}
	seen := other.cmd("NOOP").mustOK(t)
	seenText := joined(seen.untagged)
	if !strings.Contains(seenText, "EXPUNGE") || strings.Contains(seenText, "VANISHED") {
		t.Fatalf("other expunge %v", seen.untagged)
	}
	moved := cl.cmd("UID MOVE 3 Dest").mustOK(t)
	moveText := joined(moved.untagged)
	copyAt := strings.Index(moveText, "* OK [COPYUID ")
	vanAt := strings.Index(moveText, "* VANISHED 3")
	if copyAt < 0 || vanAt < copyAt || strings.Contains(moved.tagLine, "COPYUID") || strings.Contains(moveText, "EXPUNGE") || strings.Contains(moveText, "EARLIER") {
		t.Fatalf("uid move %s %v", moved.tagLine, moved.untagged)
	}

	appendBodies(t, cl, "Qbox", []string{"q"})
	qsel := cl.cmd("SELECT Qbox").mustOK(t)
	uidv := bracketNum(joined(qsel.untagged), "UIDVALIDITY")
	if uidv == 0 {
		t.Fatal("no uidvalidity")
	}
	cl.cmd("ENABLE QRESYNC").mustOK(t)
	qr := strconv.FormatUint(uint64(uidv), 10)
	plainQ := cl.cmd("SELECT Qbox (QRESYNC (" + qr + " 0 1:2))").mustOK(t)
	if strings.Contains(joined(plainQ.untagged)+" "+plainQ.tagLine, "UIDNOTSTICKY") {
		t.Fatalf("uidnotsticky %v %s", plainQ.untagged, plainQ.tagLine)
	}
	starQ := cl.cmd("SELECT Qbox (QRESYNC (" + qr + " 0 1:*))").mustOK(t)
	if strings.Contains(starQ.tagLine, "UIDREQUIRED") {
		t.Fatalf("known uid star %s", starQ.tagLine)
	}
	wantUIDRequired(t, cl.cmd("SELECT Qbox (QRESYNC ("+qr+" 0 1:2 (1 1)))"))
	wantUIDRequired(t, cl.cmd("EXAMINE Qbox (QRESYNC ("+qr+" 0 (1 1)))"))
	forced := cl.cmd("UID FETCH 1 (FLAGS)").mustOK(t)
	forcedLine := uidFetchLine(t, forced)
	if !listed(fetchKeys(t, forcedLine), "UID") {
		t.Fatalf("qresync did not force UID %s", forcedLine)
	}

	appendBodies(t, cl, "IdleBox", []string{"i"})
	cl.cmd("SELECT IdleBox").mustOK(t)
	other.cmd("SELECT IdleBox").mustOK(t)
	cl.send("i1 IDLE\r\n")
	for {
		rec := strings.TrimRight(cl.readRec(), "\r\n")
		if strings.HasPrefix(rec, "+") {
			break
		}
	}
	other.cmd(`STORE 1 +FLAGS (\Deleted)`).mustOK(t)
	otherEx := other.cmd("EXPUNGE").mustOK(t)
	if !strings.Contains(joined(otherEx.untagged), "EXPUNGE") || strings.Contains(joined(otherEx.untagged), "VANISHED") {
		t.Fatalf("other idle expunge %v", otherEx.untagged)
	}
	sawVanished := false
	for i := 0; i < 6 && !sawVanished; i++ {
		rec := strings.TrimRight(cl.readRec(), "\r\n")
		if strings.Contains(rec, "EXPUNGE") || strings.Contains(rec, "EARLIER") {
			t.Fatalf("idle %s", rec)
		}
		if rec == "* VANISHED 1" {
			sawVanished = true
		}
	}
	if !sawVanished {
		t.Fatal("idle did not report VANISHED")
	}
	cl.send("DONE\r\n")
	if done := cl.readTag("i1"); done.status != "OK" {
		t.Fatalf("idle done %s", done.tagLine)
	}

	appendBodies(t, cl, "CloseBox", []string{"c1", "c2"})
	cl.cmd("SELECT CloseBox").mustOK(t)
	cl.cmd(`UID STORE 1 +FLAGS (\Deleted)`).mustOK(t)
	cl.cmd(`UID STORE 2 +FLAGS (\Deleted)`).mustOK(t)
	closed := cl.cmd("CLOSE").mustOK(t)
	if joined(closed.untagged) != "* VANISHED 1:2" {
		t.Fatalf("close %v", closed.untagged)
	}
}

func TestUIDONLYUnauthenticate(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/stalwart.json"})
	cl := dial(t, bound.Listen)
	if listed(capsOf(cl.greeting), "UIDONLY") || !strings.Contains(cl.greeting, "Stalwart IMAP4rev2 at your service.") {
		t.Fatalf("greeting %s", cl.greeting)
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "UIDONLY") {
		t.Fatal("stalwart post-login missing UIDONLY")
	}
	en := cl.cmd("ENABLE UIDONLY").mustOK(t)
	if strings.Join(en.untagged, " ") != "* ENABLED UIDONLY" {
		t.Fatalf("enable %v", en.untagged)
	}
	appendBodies(t, cl, "Sbox", []string{"s"})
	cl.cmd("CREATE Sdest").mustOK(t)
	cl.cmd("SELECT Sbox").mustOK(t)
	moved := cl.cmd("UID MOVE 1 Sdest").mustOK(t)
	text := joined(moved.untagged)
	copyAt := strings.Index(text, "* OK [COPYUID ")
	vanAt := strings.Index(text, "* VANISHED 1")
	if copyAt < 0 || vanAt < copyAt || strings.Contains(moved.tagLine, "COPYUID") || strings.Contains(text, "EXPUNGE") || strings.Contains(text, "EARLIER") {
		t.Fatalf("stalwart move %s %v", moved.tagLine, moved.untagged)
	}
	cl.cmd("SELECT Sdest").mustOK(t)
	cl.cmd("UNAUTHENTICATE").mustOK(t)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("SELECT Sdest").mustOK(t)
	got := cl.cmd("FETCH 1 (FLAGS)").mustOK(t)
	if strings.Contains(joined(got.untagged), "UIDFETCH") {
		t.Fatalf("still enabled %v", got.untagged)
	}
}
