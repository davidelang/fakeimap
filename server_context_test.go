package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContextSearchLists(t *testing.T) {
	if _, ok := implementedCaps["CONTEXT=SEARCH"]; !ok || !capImplemented("CONTEXT=SEARCH") {
		t.Fatal("CONTEXT=SEARCH is not implemented")
	}
	if _, ok := implementedCaps["CONTEXT=SORT"]; ok || capImplemented("CONTEXT=SORT") {
		t.Fatal("CONTEXT=SORT is implemented")
	}
	both := map[string]bool{"dovecot-12.json": true}
	postOnly := map[string]bool{
		"dovecot-22.json":  true,
		"dovecot-245.json": true,
		"dovecot-dev.json": true,
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), "gaps") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("personalities", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		want := both[e.Name()] || postOnly[e.Name()]
		if !want {
			if strings.Contains(string(raw), "CONTEXT=SEARCH") || strings.Contains(string(raw), "CONTEXT=SORT") {
				t.Fatalf("%s gained a CONTEXT token", e.Name())
			}
			continue
		}
		p, err := loadPersonality(filepath.Join("personalities", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "CONTEXT=SEARCH") || listed(p.GreetingCaps, "CONTEXT=SORT") {
			t.Fatalf("%s greeting lists CONTEXT", e.Name())
		}
		if both[e.Name()] {
			if !contextBetween(p.PreLogin) || !contextBetween(p.PostLogin) {
				t.Fatalf("%s lists %v %v", e.Name(), p.PreLogin, p.PostLogin)
			}
			continue
		}
		if listed(p.PreLogin, "CONTEXT=SEARCH") || !contextBetween(p.PostLogin) {
			t.Fatalf("%s lists %v %v", e.Name(), p.PreLogin, p.PostLogin)
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "CONTEXT=SEARCH") || strings.Contains(string(gaps), "CONTEXT=SORT") {
		t.Fatal("gaps file names a CONTEXT token")
	}
	facts, err := os.ReadFile("docs/facts/imap-server.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(facts)
	for _, phrase := range []string{
		"CONTEXT=SEARCH",
		"ADDTO",
		"REMOVEFROM",
		"CANCELUPDATE",
		"[NOUPDATE]",
		"RETURN (UPDATE)",
		"RETURN (CONTEXT)",
		"dovecot-12.json",
		"dovecot-22.json",
		"dovecot-245.json",
		"dovecot-dev.json",
		"UNAUTHENTICATE",
		"UIDONLY",
		"CONTEXT=SORT",
		"must authenticate first",
	} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("facts missing %q", phrase)
		}
	}
	guideRaw, err := os.ReadFile("docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := string(guideRaw)
	off := strings.Index(guide, "## Capabilities that stay off")
	if off < 0 {
		t.Fatal("guide missing the stay-off heading")
	}
	if strings.Contains(guide[off:], "CONTEXT=SEARCH") {
		t.Fatal("guide stay-off lists CONTEXT=SEARCH")
	}
	if strings.Contains(guide, "[NOUPDATE]") || strings.Contains(guide, `* ESEARCH (TAG "tag")`) {
		t.Fatal("guide pastes the facts file")
	}
	para := guide[:off]
	if !strings.Contains(para, "CONTEXT=SEARCH") || !strings.Contains(para, "ADDTO") || !strings.Contains(para, "REMOVEFROM") || !strings.Contains(para, "CANCELUPDATE") || !strings.Contains(para, "CONTEXT=SORT") || !strings.Contains(para, "docs/facts/imap-server.md") {
		t.Fatal("guide missing the CONTEXT=SEARCH note")
	}
}

func contextBetween(list []string) bool {
	i := capAt(list, "CONTEXT=SEARCH")
	return i > 0 && list[i-1] == "WITHIN" && i+1 < len(list) && list[i+1] == "LIST-STATUS"
}

func TestContextSearchDenied(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/dovecot-12.json"})
	pre := dial(t, bound.Listen)
	if strings.Contains(pre.greeting, "CONTEXT=SEARCH") {
		t.Fatalf("greeting %q", pre.greeting)
	}
	if !listed(capsOf(pre.cmd("CAPABILITY").mustOK(t).untagged[0]), "CONTEXT=SEARCH") {
		t.Fatal("dovecot-12 pre-login missing CONTEXT=SEARCH")
	}
	for _, line := range []string{`CANCELUPDATE`, `CANCELUPDATE "t"`} {
		rp := pre.cmd(line)
		if rp.status != "NO" || !strings.Contains(rp.text, "must authenticate first") {
			t.Fatalf("before login %s %+v", line, rp)
		}
	}

	_, cyrusBound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	cyrus := dial(t, cyrusBound.Listen)
	cyrus.cmd("LOGIN demo secret").mustOK(t)
	cyrus.cmd("SELECT INBOX").mustOK(t)
	for _, line := range []string{
		"CANCELUPDATE",
		`CANCELUPDATE "t"`,
		"SEARCH RETURN (UPDATE) ALL",
		"UID SEARCH RETURN (UPDATE) ALL",
		"SEARCH RETURN (CONTEXT) ALL",
		"UID SEARCH RETURN (CONTEXT) ALL",
		"SORT RETURN (UPDATE) (ARRIVAL) UTF-8 ALL",
		"SORT RETURN (CONTEXT) (ARRIVAL) UTF-8 ALL",
		"ESEARCH IN (personal) RETURN (UPDATE) ALL",
		"FILTER SIEVE DELIVERY RETURN (UPDATE) ALL",
	} {
		wantContextBad(t, cyrus.cmd(line))
	}
}

func TestContextSearchReturn(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/dovecot-245.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("SELECT INBOX").mustOK(t)

	plain := cl.cmd("SEARCH RETURN (ALL) ALL").mustOK(t)
	if joined(plain.untagged) != esearch(cl, "ALL 1:2") {
		t.Fatalf("all %q", plain.untagged)
	}
	ctxOnly := cl.cmd("SEARCH RETURN (CONTEXT) ALL").mustOK(t)
	if joined(ctxOnly.untagged) != esearch(cl, "ALL 1:2") {
		t.Fatalf("context %q", ctxOnly.untagged)
	}
	ctxEmpty := cl.cmd("SEARCH RETURN (CONTEXT) ALL").mustOK(t)
	if !strings.Contains(joined(ctxEmpty.untagged), "ALL 1:2") {
		t.Fatalf("context again %q", ctxEmpty.untagged)
	}
	minOnly := cl.cmd("SEARCH RETURN (MIN) ALL").mustOK(t)
	ctxMin := cl.cmd("SEARCH RETURN (CONTEXT MIN) ALL").mustOK(t)
	if !strings.Contains(joined(minOnly.untagged), "MIN 1") || strings.Contains(joined(minOnly.untagged), "ALL") || joined(ctxMin.untagged) == "" || !strings.Contains(joined(ctxMin.untagged), "MIN 1") || strings.Contains(joined(ctxMin.untagged), "ALL") {
		t.Fatalf("min %q context %q", minOnly.untagged, ctxMin.untagged)
	}

	for _, line := range []string{
		"SEARCH RETURN (UPDATE UPDATE) ALL",
		"SEARCH RETURN (CONTEXT CONTEXT) ALL",
		"UID SEARCH RETURN (UPDATE UPDATE) ALL",
		"SORT RETURN (UPDATE) (ARRIVAL) UTF-8 ALL",
		"UID SORT RETURN (UPDATE) (ARRIVAL) UTF-8 ALL",
		"SORT RETURN (CONTEXT) (ARRIVAL) UTF-8 ALL",
		"UID SORT RETURN (CONTEXT) (ARRIVAL) UTF-8 ALL",
		"ESEARCH IN (personal) RETURN (UPDATE) ALL",
		"FILTER SIEVE DELIVERY RETURN (UPDATE) ALL",
		"UID FILTER SIEVE DELIVERY RETURN (UPDATE) ALL",
	} {
		wantContextBad(t, cl.cmd(line))
	}

	upd := cl.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
	if joined(upd.untagged) != esearch(cl, "ALL 1:2") {
		t.Fatalf("update alone %q", upd.untagged)
	}
	withAll := cl.cmd("SEARCH RETURN (UPDATE ALL) ALL").mustOK(t)
	if joined(withAll.untagged) != esearch(cl, "ALL 1:2") {
		t.Fatalf("update all %q", withAll.untagged)
	}
	withMin := cl.cmd("SEARCH RETURN (UPDATE MIN) ALL").mustOK(t)
	if !strings.Contains(joined(withMin.untagged), "MIN 1") || strings.Contains(joined(withMin.untagged), "ALL") {
		t.Fatalf("update min %q", withMin.untagged)
	}
	withMax := cl.cmd("SEARCH RETURN (MAX UPDATE) ALL").mustOK(t)
	if !strings.Contains(joined(withMax.untagged), "MAX 2") || strings.Contains(joined(withMax.untagged), "ALL") {
		t.Fatalf("update max %q", withMax.untagged)
	}
	withCount := cl.cmd("SEARCH RETURN (COUNT UPDATE) ALL").mustOK(t)
	if !strings.Contains(joined(withCount.untagged), "COUNT 2") || strings.Contains(joined(withCount.untagged), "ALL") {
		t.Fatalf("update count %q", withCount.untagged)
	}
	uidUpd := cl.cmd("UID SEARCH RETURN (UPDATE CONTEXT) ALL").mustOK(t)
	if joined(uidUpd.untagged) != esearch(cl, "UID ALL 1:2") {
		t.Fatalf("uid update %q", uidUpd.untagged)
	}
	saved := cl.cmd("SEARCH RETURN (SAVE UPDATE) ALL").mustOK(t)
	if len(saved.untagged) != 0 {
		t.Fatalf("save update %q", saved.untagged)
	}
	saveTag := fmt.Sprintf("a%d", cl.n)

	cl.send("s1 SEARCH RETURN (UPDATE) UNSEEN\r\n")
	first := cl.readTag("s1").mustOK(t)
	if joined(first.untagged) != `* ESEARCH (TAG "s1") ALL 2` {
		t.Fatalf("unseen %q", first.untagged)
	}
	cl.send("s1 SEARCH RETURN (UPDATE) ALL\r\n")
	wantContextBad(t, cl.readTag("s1"))
	seen := cl.cmd(`STORE 2 +FLAGS (\Seen)`).mustOK(t)
	if !strings.Contains(joined(seen.untagged), `* ESEARCH (TAG "s1") REMOVEFROM (0 2)`) {
		t.Fatalf("old context %q", seen.untagged)
	}
	if strings.Contains(joined(seen.untagged), "ADDTO") || strings.Contains(joined(seen.untagged), "NOUPDATE") {
		t.Fatalf("replaced context %q", seen.untagged)
	}

	for _, line := range []string{"CANCELUPDATE", "CANCELUPDATE s1", `CANCELUPDATE s1 "s1"`} {
		wantContextBad(t, cl.cmd(line))
	}
	if rp := cl.cmd(`UID CANCELUPDATE "s1"`); rp.status != "BAD" {
		t.Fatalf("uid cancel %+v", rp)
	}
	if strings.Contains(joined(cl.cmd(`STORE 2 -FLAGS (\Seen)`).mustOK(t).untagged), `TAG "s1"`) == false {
		t.Fatal("unquoted cancel dropped the context")
	}
	cl.cmd(`CANCELUPDATE "missing"`).mustOK(t)
	cl.cmd(`CANCELUPDATE "s1" "missing"`).mustOK(t)
	quiet := cl.cmd(`STORE 2 +FLAGS (\Seen)`).mustOK(t)
	if strings.Contains(joined(quiet.untagged), "ESEARCH") {
		t.Fatalf("cancelled %q", quiet.untagged)
	}

	// The SAVE from before the flag changes is still the original match.
	fetch := cl.cmd("FETCH $ (UID)").mustOK(t)
	got := joined(fetch.untagged)
	if !strings.Contains(got, "UID 1") || !strings.Contains(got, "UID 2") || strings.Contains(got, "UID 3") {
		t.Fatalf("saved set %q tag %s", fetch.untagged, saveTag)
	}
}

func TestContextSearchPush(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/dovecot-245.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("CREATE Box").mustOK(t)
	cl.cmd("SELECT Box").mustOK(t)
	cl.cmd("APPEND Box " + imapLit("one")).mustOK(t)
	cl.cmd("APPEND Box " + imapLit("two")).mustOK(t)

	same := cl.cmd("STORE 1 FLAGS (\\Seen)").mustOK(t)
	// No context yet.
	if strings.Contains(joined(same.untagged), "ESEARCH") {
		t.Fatalf("early esearch %q", same.untagged)
	}
	reg := cl.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
	tag := fmt.Sprintf("a%d", cl.n)
	if joined(reg.untagged) != esearch(cl, "ALL 1:2") {
		t.Fatalf("register %q", reg.untagged)
	}
	held := cl.cmd("STORE 1 FLAGS (\\Seen)").mustOK(t)
	if strings.Contains(joined(held.untagged), "ESEARCH") {
		t.Fatalf("unchanged %q", held.untagged)
	}

	other := dial(t, bound.Listen)
	other.cmd("LOGIN demo secret").mustOK(t)
	other.cmd("APPEND Box " + imapLit("three")).mustOK(t)
	pushed := pullUntagged(t, cl, 2*time.Second)
	assertOrder(t, pushed, "* 3 EXISTS", esearchTag(tag, "ADDTO (0 3)"))
	if strings.Contains(joined(pushed), "UID ADDTO") || strings.Contains(joined(pushed), "NOUPDATE") {
		t.Fatalf("seq add %q", pushed)
	}

	cl.cmd(`CANCELUPDATE "` + tag + `"`).mustOK(t)
	uidReg := cl.cmd("UID SEARCH RETURN (UPDATE) ALL").mustOK(t)
	uidTag := fmt.Sprintf("a%d", cl.n)
	if joined(uidReg.untagged) != esearch(cl, "UID ALL 1:3") {
		t.Fatalf("uid register %q", uidReg.untagged)
	}
	other.cmd("APPEND Box " + imapLit("four")).mustOK(t)
	uidPushed := pullUntagged(t, cl, 2*time.Second)
	assertOrder(t, uidPushed, "* 4 EXISTS", esearchTag(uidTag, "UID ADDTO (0 4)"))

	cl.cmd(`CANCELUPDATE "` + uidTag + `"`).mustOK(t)
	unseen := cl.cmd("SEARCH RETURN (UPDATE) UNSEEN").mustOK(t)
	unseenTag := fmt.Sprintf("a%d", cl.n)
	// one was \Seen, two three four were not.
	if !strings.Contains(joined(unseen.untagged), "ALL 2:4") {
		t.Fatalf("unseen %q", unseen.untagged)
	}
	gone := cl.cmd(`STORE 2 +FLAGS (\Seen)`).mustOK(t)
	assertOrder(t, gone.untagged, esearchTag(unseenTag, "REMOVEFROM (0 2)"), "FETCH")
	if strings.Contains(joined(gone.untagged), "ADDTO") {
		t.Fatalf("remove also added %q", gone.untagged)
	}

	cl.cmd(`CANCELUPDATE "` + unseenTag + `"`).mustOK(t)
	cl.cmd("CREATE Orbox").mustOK(t)
	cl.cmd("SELECT Orbox").mustOK(t)
	foo := "Subject: foo\r\n\r\n"
	bar := "Subject: bar\r\n\r\n"
	cl.cmd("APPEND Orbox " + imapLit(foo)).mustOK(t)
	cl.cmd("APPEND Orbox " + imapLit(bar)).mustOK(t)
	or := cl.cmd("SEARCH RETURN (UPDATE) OR (SUBJECT foo UNSEEN) (SUBJECT bar FLAGGED)").mustOK(t)
	orTag := fmt.Sprintf("a%d", cl.n)
	if joined(or.untagged) != esearch(cl, "ALL 1") {
		t.Fatalf("or %q", or.untagged)
	}
	both := cl.cmd(`STORE 1:2 FLAGS (\Seen \Flagged)`).mustOK(t)
	assertOrder(t, both.untagged, esearchTag(orTag, "REMOVEFROM (0 1)"), esearchTag(orTag, "ADDTO (0 2)"), "FETCH")

	cl.cmd("CREATE Exp").mustOK(t)
	cl.cmd("SELECT Exp").mustOK(t)
	cl.cmd("APPEND Exp " + imapLit("a")).mustOK(t)
	cl.cmd("APPEND Exp " + imapLit("b")).mustOK(t)
	cl.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
	expTag := fmt.Sprintf("a%d", cl.n)
	ex := cl.cmd(`STORE 1 +FLAGS (\Deleted)`).mustOK(t)
	if strings.Contains(joined(ex.untagged), "ESEARCH") {
		t.Fatalf("deleted flag %q", ex.untagged)
	}
	exp := cl.cmd("EXPUNGE").mustOK(t)
	assertOrder(t, exp.untagged, esearchTag(expTag, "REMOVEFROM (0 1)"), "* 1 EXPUNGE")
	if strings.Contains(joined(exp.untagged), "VANISHED") || strings.Contains(joined(exp.untagged), "ADDTO") {
		t.Fatalf("expunge %q", exp.untagged)
	}

	// A second context on the surviving message, then one UID expunge.
	cl.cmd("UID SEARCH RETURN (UPDATE) ALL").mustOK(t)
	uidExp := fmt.Sprintf("a%d", cl.n)
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).mustOK(t)
	uidGone := cl.cmd("EXPUNGE").mustOK(t)
	assertOrder(t, uidGone.untagged, esearchTag(expTag, "REMOVEFROM (0 1)"), esearchTag(uidExp, "UID REMOVEFROM (0 2)"), "* 1 EXPUNGE")

	cl.cmd("CREATE Rep").mustOK(t)
	cl.cmd("SELECT Rep").mustOK(t)
	cl.cmd("APPEND Rep " + imapLit("old-a")).mustOK(t)
	cl.cmd("APPEND Rep " + imapLit("old-b")).mustOK(t)
	cl.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
	repTag := fmt.Sprintf("a%d", cl.n)
	sameBox := cl.cmd("REPLACE 1 Rep " + imapLit("new-a")).mustOK(t)
	assertOrder(t, sameBox.untagged,
		esearchTag(repTag, "REMOVEFROM (0 1)"),
		"* 1 EXPUNGE",
		"* 2 EXISTS",
		esearchTag(repTag, "ADDTO (0 2)"),
	)
	if strings.Contains(joined(sameBox.untagged), "UID ") {
		t.Fatalf("replace uid marker %q", sameBox.untagged)
	}
	cl.cmd("CREATE Other").mustOK(t)
	away := cl.cmd("REPLACE 1 Other " + imapLit("away")).mustOK(t)
	if joined(away.untagged) != esearchTag(repTag, "REMOVEFROM (0 1)")+"\n* 1 EXPUNGE" {
		t.Fatalf("away %q", away.untagged)
	}

	cl.cmd("CREATE Saved").mustOK(t)
	cl.cmd("SELECT Saved").mustOK(t)
	cl.cmd("APPEND Saved " + imapLit("s1")).mustOK(t)
	cl.cmd("APPEND Saved " + imapLit("s2")).mustOK(t)
	keep := cl.cmd("SEARCH RETURN (SAVE UPDATE) ALL").mustOK(t)
	if len(keep.untagged) != 0 {
		t.Fatalf("save %q", keep.untagged)
	}
	saveTag := fmt.Sprintf("a%d", cl.n)
	other.cmd("APPEND Saved " + imapLit("s3")).mustOK(t)
	savePush := pullUntagged(t, cl, 2*time.Second)
	if joined(savePush) == "" || !strings.Contains(joined(savePush), esearchTag(saveTag, "ADDTO (0 3)")) {
		t.Fatalf("save push %q", savePush)
	}
	fetched := cl.cmd("FETCH $ (UID)").mustOK(t)
	body := joined(fetched.untagged)
	if !strings.Contains(body, "UID 1") || !strings.Contains(body, "UID 2") || strings.Contains(body, "UID 3") {
		t.Fatalf("save set stayed %q", fetched.untagged)
	}

	cl.cmd("CREATE Dol").mustOK(t)
	cl.cmd("SELECT Dol").mustOK(t)
	cl.cmd("APPEND Dol " + imapLit("d1")).mustOK(t)
	cl.cmd("APPEND Dol " + imapLit("d2")).mustOK(t)
	cl.cmd("SEARCH RETURN (SAVE) UID 1").mustOK(t)
	frozen := cl.cmd("SEARCH RETURN (UPDATE) $").mustOK(t)
	if joined(frozen.untagged) != esearch(cl, "ALL 1") {
		t.Fatalf("frozen %q", frozen.untagged)
	}
	cl.cmd("SEARCH RETURN (SAVE) ALL").mustOK(t)
	cl.cmd(`STORE 2 +FLAGS (\Deleted)`).mustOK(t)
	left := cl.cmd("EXPUNGE").mustOK(t)
	if strings.Contains(joined(left.untagged), "ESEARCH") {
		t.Fatalf("rebound $ %q", left.untagged)
	}

	// Idle and a run of expunges observed by another session.
	watch := dial(t, bound.Listen)
	watch.cmd("LOGIN demo secret").mustOK(t)
	watch.cmd("ENABLE QRESYNC").mustOK(t)
	watch.cmd("SELECT Saved").mustOK(t)
	watch.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
	wtag := fmt.Sprintf("a%d", watch.n)
	watch.n++
	idleTag := fmt.Sprintf("a%d", watch.n)
	watch.send(idleTag + " IDLE\r\n")
	for {
		rec := strings.TrimRight(watch.readRec(), "\r\n")
		if strings.HasPrefix(rec, "+") {
			break
		}
	}
	cl.cmd("SELECT Saved").mustOK(t)
	cl.cmd(`STORE 1:* +FLAGS (\Deleted)`).mustOK(t)
	cl.cmd("EXPUNGE").mustOK(t)
	idleLines := pullUntagged(t, watch, 2*time.Second)
	assertOrder(t, idleLines, esearchTag(wtag, "REMOVEFROM"), "* VANISHED ")
	if strings.Contains(joined(idleLines), "EXPUNGE") {
		t.Fatalf("idle vanished %q", idleLines)
	}
	rem := esearchLine(idleLines, "REMOVEFROM")
	van := esearchLine(idleLines, "VANISHED")
	if strings.Count(joined(idleLines), "REMOVEFROM") != 1 || strings.Count(joined(idleLines), "VANISHED") != 1 {
		t.Fatalf("run %q", idleLines)
	}
	if !strings.Contains(rem, "(0 ") || !strings.HasPrefix(van, "* VANISHED ") {
		t.Fatalf("shapes %q %q", rem, van)
	}
	watch.send("DONE\r\n")
	watch.readTag(idleTag).mustOK(t)
}

func TestContextSearchLifetime(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/dovecot-245.json"})
	keep := dial(t, bound.Listen)
	keep.cmd("LOGIN demo secret").mustOK(t)
	keep.cmd("CREATE Life").mustOK(t)
	keep.cmd("SELECT Life").mustOK(t)
	keep.cmd("APPEND Life " + imapLit("L")).mustOK(t)
	keep.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
	keepTag := fmt.Sprintf("a%d", keep.n)

	drop := dial(t, bound.Listen)
	drop.cmd("LOGIN demo secret").mustOK(t)
	actor := dial(t, bound.Listen)
	actor.cmd("LOGIN demo secret").mustOK(t)

	check := func(label, dropCmd string) {
		t.Helper()
		drop.cmd("SELECT Life").mustOK(t)
		drop.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
		dtag := fmt.Sprintf("a%d", drop.n)
		drop.cmd(dropCmd).mustOK(t)
		actor.cmd("APPEND Life " + imapLit(label)).mustOK(t)
		got := pullUntagged(t, keep, 2*time.Second)
		if !strings.Contains(joined(got), esearchTag(keepTag, "ADDTO")) {
			t.Fatalf("%s keep %q", label, got)
		}
		if strings.Contains(joined(got), `TAG "`+dtag+`"`) {
			t.Fatalf("%s leaked drop tag %q", label, got)
		}
		quiet := pullUntagged(t, drop, 300*time.Millisecond)
		if strings.Contains(joined(quiet), "ESEARCH") || strings.Contains(joined(quiet), "EXISTS") {
			t.Fatalf("%s drop still watching %q", label, quiet)
		}
	}
	check("select", "SELECT INBOX")
	check("examine", "EXAMINE INBOX")
	check("close", "CLOSE")
	check("unselect", "UNSELECT")

	path := personalityWithPostCap(t, "personalities/stalwart.json", "CONTEXT=SEARCH")
	_, ubound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	ukeep := dial(t, ubound.Listen)
	ukeep.cmd("LOGIN demo secret").mustOK(t)
	ukeep.cmd("CREATE Ulife").mustOK(t)
	ukeep.cmd("SELECT Ulife").mustOK(t)
	ukeep.cmd("APPEND Ulife " + imapLit("u")).mustOK(t)
	ukeep.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
	utag := fmt.Sprintf("a%d", ukeep.n)
	udrop := dial(t, ubound.Listen)
	udrop.cmd("LOGIN demo secret").mustOK(t)
	udrop.cmd("SELECT Ulife").mustOK(t)
	udrop.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
	udrop.cmd("UNAUTHENTICATE").mustOK(t)
	uactor := dial(t, ubound.Listen)
	uactor.cmd("LOGIN demo secret").mustOK(t)
	uactor.cmd("APPEND Ulife " + imapLit("more")).mustOK(t)
	ugot := pullUntagged(t, ukeep, 2*time.Second)
	if !strings.Contains(joined(ugot), esearchTag(utag, "ADDTO")) {
		t.Fatalf("unauth keep %q", ugot)
	}
	if quiet := pullUntagged(t, udrop, 300*time.Millisecond); strings.Contains(joined(quiet), "ESEARCH") {
		t.Fatalf("unauth drop %q", quiet)
	}
}

func TestContextSearchNotifyAndPartial(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/dovecot-245.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("SELECT INBOX").mustOK(t)
	cl.cmd("SEARCH RETURN (UPDATE) ALL").mustOK(t)
	tag := fmt.Sprintf("a%d", cl.n)
	cl.cmd("NOTIFY SET (personal (MessageNew MessageExpunge))").mustOK(t)
	actor := dial(t, bound.Listen)
	actor.cmd("LOGIN demo secret").mustOK(t)
	actor.cmd("APPEND INBOX/Generated {5+}\r\nhello").mustOK(t)
	other := pullUntagged(t, cl, 2*time.Second)
	if len(other) != 1 || !strings.HasPrefix(other[0], `* STATUS "INBOX/Generated" `) {
		t.Fatalf("other mailbox %q", other)
	}
	cl.cmd("NOTIFY SET (selected (MessageNew (FLAGS) MessageExpunge))").mustOK(t)
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	got := pullUntagged(t, cl, 2*time.Second)
	assertOrder(t, got, "* 3 EXISTS", "FETCH", esearchTag(tag, "ADDTO (0 3)"))
	for _, line := range got {
		if strings.HasPrefix(line, "* STATUS ") {
			t.Fatalf("selected status %q", got)
		}
	}

	path := personalityWithPostCap(t, "personalities/cyrus-3137.json", "CONTEXT=SEARCH")
	_, pbound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	part := dial(t, pbound.Listen)
	part.cmd("LOGIN demo secret").mustOK(t)
	part.cmd("SELECT INBOX").mustOK(t)
	win := part.cmd("SEARCH RETURN (UPDATE PARTIAL 1:1) ALL").mustOK(t)
	ptag := fmt.Sprintf("a%d", part.n)
	if !strings.Contains(joined(win.untagged), "PARTIAL (1:1 1)") || strings.Contains(joined(win.untagged), "ALL") {
		t.Fatalf("partial %q", win.untagged)
	}
	pactor := dial(t, pbound.Listen)
	pactor.cmd("LOGIN demo secret").mustOK(t)
	pactor.cmd("APPEND INBOX " + imapLit("p")).mustOK(t)
	ppush := pullUntagged(t, part, 2*time.Second)
	if !strings.Contains(joined(ppush), esearchTag(ptag, "ADDTO (0 3)")) || strings.Contains(joined(ppush), "PARTIAL") {
		t.Fatalf("partial push %q", ppush)
	}

	uid := dial(t, pbound.Listen)
	uid.cmd("LOGIN demo secret").mustOK(t)
	uid.cmd("ENABLE UIDONLY").mustOK(t)
	uid.cmd("CREATE Ubox").mustOK(t)
	uid.cmd("SELECT Ubox").mustOK(t)
	uid.cmd("APPEND Ubox " + imapLit("u")).mustOK(t)
	wantUIDRequired(t, uid.cmd("SEARCH RETURN (UPDATE) ALL"))
	pactor.cmd("APPEND Ubox " + imapLit("v")).mustOK(t)
	noop := uid.cmd("NOOP").mustOK(t)
	if !strings.Contains(joined(noop.untagged), "EXISTS") || strings.Contains(joined(noop.untagged), "ESEARCH") {
		t.Fatalf("uidonly search registered %q", noop.untagged)
	}
	ok := uid.cmd("UID SEARCH RETURN (UPDATE) ALL").mustOK(t)
	if !strings.Contains(joined(ok.untagged), "UID ALL") {
		t.Fatalf("uid search %q", ok.untagged)
	}
	utag := fmt.Sprintf("a%d", uid.n)
	pactor.cmd("APPEND Ubox " + imapLit("w")).mustOK(t)
	upush := pullUntagged(t, uid, 2*time.Second)
	if !strings.Contains(joined(upush), esearchTag(utag, "UID ADDTO")) {
		t.Fatalf("uidonly push %q", upush)
	}

	es := dial(t, pbound.Listen)
	es.cmd("LOGIN demo secret").mustOK(t)
	es.cmd("SELECT INBOX").mustOK(t)
	wantContextBad(t, es.cmd("ESEARCH IN (personal) RETURN (UPDATE) ALL"))
	wantContextBad(t, es.cmd("ESEARCH IN (personal) RETURN (CONTEXT) ALL"))
	wantContextBad(t, es.cmd("FILTER SIEVE DELIVERY RETURN (UPDATE) ALL"))
	wantContextBad(t, es.cmd("SORT RETURN (UPDATE) (ARRIVAL) UTF-8 ALL"))
	wantContextBad(t, es.cmd("SORT RETURN (CONTEXT) (ARRIVAL) UTF-8 ALL"))
}

func TestContextSearchEveryPersonality(t *testing.T) {
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), "gaps") {
			continue
		}
		path := filepath.Join("personalities", e.Name())
		p, err := loadPersonality(path)
		if err != nil {
			t.Fatal(err)
		}
		_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
		cl := dial(t, bound.Listen)
		if strings.Contains(cl.greeting, "CONTEXT=SEARCH") || strings.Contains(cl.greeting, "CONTEXT=SORT") {
			t.Fatalf("%s greeting %q", e.Name(), cl.greeting)
		}
		pre := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
		if listed(pre, "CONTEXT=SEARCH") != listed(p.PreLogin, "CONTEXT=SEARCH") || listed(pre, "CONTEXT=SORT") {
			t.Fatalf("%s pre %v", e.Name(), pre)
		}
		cl.cmd("LOGIN demo secret").mustOK(t)
		post := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
		if listed(post, "CONTEXT=SEARCH") != listed(p.PostLogin, "CONTEXT=SEARCH") || listed(post, "CONTEXT=SORT") {
			t.Fatalf("%s post %v", e.Name(), post)
		}
		cl.cmd("SELECT INBOX").mustOK(t)
		for _, line := range []string{
			"SORT RETURN (UPDATE) (ARRIVAL) UTF-8 ALL",
			"SORT RETURN (CONTEXT) (ARRIVAL) UTF-8 ALL",
			"UID SORT RETURN (UPDATE) (ARRIVAL) UTF-8 ALL",
			"UID SORT RETURN (CONTEXT) (ARRIVAL) UTF-8 ALL",
		} {
			wantContextBad(t, cl.cmd(line))
		}
		rp := cl.cmd(`CANCELUPDATE "nope"`)
		if listed(p.PostLogin, "CONTEXT=SEARCH") {
			if rp.status != "OK" || strings.Contains(rp.tagLine, "NOUPDATE") {
				t.Fatalf("%s cancel %+v", e.Name(), rp)
			}
			continue
		}
		wantContextBad(t, rp)
	}
}

func personalityWithPostCap(t *testing.T, src, token string) string {
	t.Helper()
	p, err := loadPersonality(src)
	if err != nil {
		t.Fatal(err)
	}
	p.PostLogin = append([]string{}, p.PostLogin...)
	p.PostLogin = append(p.PostLogin, token)
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func esearch(cl *imapClient, tail string) string {
	return fmt.Sprintf("* ESEARCH (TAG \"a%d\") %s", cl.n, tail)
}

func esearchTag(tag, tail string) string {
	return fmt.Sprintf("* ESEARCH (TAG \"%s\") %s", tag, tail)
}

func esearchLine(lines []string, part string) string {
	for _, line := range lines {
		if strings.Contains(line, part) {
			return line
		}
	}
	return ""
}

func assertOrder(t *testing.T, lines []string, parts ...string) {
	t.Helper()
	last := -1
	for _, part := range parts {
		at := -1
		for i, line := range lines {
			if strings.Contains(line, part) {
				at = i
				break
			}
		}
		if at < 0 || at <= last {
			t.Fatalf("order %q in %q", parts, lines)
		}
		last = at
	}
}

func wantContextBad(t *testing.T, rp *reply) {
	t.Helper()
	if rp.status != "BAD" || strings.Contains(rp.tagLine, "NOUPDATE") || strings.Contains(joined(rp.untagged), "NOUPDATE") {
		t.Fatalf("want BAD, got %+v", rp)
	}
}
