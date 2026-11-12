package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestObjectIDPersonalities(t *testing.T) {
	if !capImplemented("OBJECTID") || !capImplemented("OBJECTID+") {
		t.Fatal("OBJECTID tokens are not implemented")
	}
	rfcBoth := map[string][2]string{
		"cyrus-36.json":   {"NAMESPACE", "PREVIEW"},
		"cyrus-3124.json": {"NAMESPACE", "PARTIAL"},
		"cyrus-3137.json": {"NAMESPACE", "PARTIAL"},
	}
	rfcPost := map[string][2]string{
		"sasl.json":          {"NAMESPACE", "UIDONLY"},
		"stalwart-0155.json": {"STATUS=SIZE", "PREVIEW"},
	}
	plusPost := map[string][2]string{
		"stalwart.json":       {"STATUS=SIZE", "PREVIEW"},
		"stalwart-01625.json": {"STATUS=SIZE", "PREVIEW"},
		"stalwart-dev.json":   {"STATUS=SIZE", "PREVIEW"},
	}
	greetings := map[string]string{
		"cyrus-36.json":       "localhost Cyrus IMAP 3.6.1-Debian-3.6.1-4+deb12u5 server ready",
		"cyrus-3124.json":     "localhost Cyrus IMAP 3.12.4 server ready",
		"cyrus-3137.json":     "localhost Cyrus IMAP unknown server ready",
		"sasl.json":           "localhost Cyrus IMAP unknown server ready",
		"stalwart.json":       "Stalwart IMAP4rev2 at your service.",
		"stalwart-01625.json": "Stalwart IMAP4rev2 at your service.",
		"stalwart-dev.json":   "Stalwart IMAP4rev2 at your service.",
		"stalwart-0155.json":  "Stalwart IMAP4rev2 at your service.",
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), ".gaps.") {
			continue
		}
		p, err := loadPersonality(filepath.Join("personalities", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if g, ok := greetings[e.Name()]; ok && p.Greeting != g {
			t.Fatalf("%s greeting %q", e.Name(), p.Greeting)
		}
		if listed(p.GreetingCaps, "OBJECTID") || listed(p.GreetingCaps, "OBJECTID+") {
			t.Fatalf("%s greeting lists an object id token", e.Name())
		}
		hasRFC := listed(p.PreLogin, "OBJECTID") || listed(p.PostLogin, "OBJECTID")
		hasPlus := listed(p.PreLogin, "OBJECTID+") || listed(p.PostLogin, "OBJECTID+")
		switch {
		case rfcBoth[e.Name()] != [2]string{}:
			prev, next := rfcBoth[e.Name()][0], rfcBoth[e.Name()][1]
			if listed(p.PreLogin, "OBJECTID+") || listed(p.PostLogin, "OBJECTID+") || listed(p.GreetingCaps, "OBJECTID") {
				t.Fatalf("%s token placement", e.Name())
			}
			if !betweenCaps(p.PreLogin, "OBJECTID", prev, next) || !betweenCaps(p.PostLogin, "OBJECTID", prev, next) {
				t.Fatalf("%s OBJECTID neighbors pre %v post %v", e.Name(), p.PreLogin, p.PostLogin)
			}
		case rfcPost[e.Name()] != [2]string{}:
			prev, next := rfcPost[e.Name()][0], rfcPost[e.Name()][1]
			if listed(p.PreLogin, "OBJECTID") || listed(p.PreLogin, "OBJECTID+") || listed(p.PostLogin, "OBJECTID+") {
				t.Fatalf("%s lists OBJECTID outside post-login", e.Name())
			}
			if !betweenCaps(p.PostLogin, "OBJECTID", prev, next) {
				t.Fatalf("%s OBJECTID neighbors %v", e.Name(), p.PostLogin)
			}
		case plusPost[e.Name()] != [2]string{}:
			prev, next := plusPost[e.Name()][0], plusPost[e.Name()][1]
			if hasRFC || listed(p.PreLogin, "OBJECTID+") {
				t.Fatalf("%s lists OBJECTID or pre-login OBJECTID+", e.Name())
			}
			if !betweenCaps(p.PostLogin, "OBJECTID+", prev, next) {
				t.Fatalf("%s OBJECTID+ neighbors %v", e.Name(), p.PostLogin)
			}
		default:
			if hasRFC || hasPlus {
				t.Fatalf("%s gained an object id token", e.Name())
			}
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "OBJECTID") || !strings.Contains(string(gaps), "CATENATE") {
		t.Fatal("gaps file")
	}
	facts, err := os.ReadFile("docs/facts/imap-server.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(facts)
	for _, phrase := range []string{
		"`OBJECTID`, when listed, is RFC 8474",
		"`OBJECTID+`, when listed",
		"ACCOUNTID",
		"does not require `ENABLE`",
		"A stored `THREADID` does not change",
		"JMAP `threadId` stays the email id",
		"* OK [MAILBOXID (id)] Ok",
		"* OK [OBJECTID (MAILBOXID id ACCOUNTID id)] Ok",
		"* ENABLED OBJECTID+",
		"any flag other than `\\Deleted` and `\\Recent`",
		"cyrus-36.json",
		"cyrus-3124.json",
		"cyrus-3137.json",
		"sasl.json",
		"stalwart-0155.json",
		"stalwart.json",
		"stalwart-01625.json",
		"stalwart-dev.json",
		"CATENATE` stays unimplemented",
		"not in `personalities/cyrus-3137.gaps.json`",
	} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("facts missing %s", phrase)
		}
	}
	guideRaw, err := os.ReadFile("docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := string(guideRaw)
	if strings.Contains(guide, "OBJECTID` stays unimplemented") || strings.Contains(guide, "OBJECTID stays unimplemented") {
		t.Fatal("guide still says OBJECTID is unimplemented")
	}
	if !strings.Contains(guide, "docs/facts/imap-server.md") || !strings.Contains(guide, "`OBJECTID+`") || !strings.Contains(guide, "ACCOUNTID") {
		t.Fatal("guide missing the short OBJECTID note")
	}
	off := strings.Index(guide, "## Capabilities that stay off")
	tokens := strings.Index(guide[off:], "the tokens are")
	if off < 0 || tokens < 0 || strings.Contains(guide[off+tokens:], "`OBJECTID`") {
		t.Fatal("stay-off still lists OBJECTID")
	}
	if !strings.Contains(guide[off:], "`CATENATE`") {
		t.Fatal("stay-off dropped CATENATE")
	}
	if strings.Contains(guide, "formatFetch") || strings.Contains(guide, "plusObjectOn") {
		t.Fatal("guide pastes the facts file")
	}
}

func betweenCaps(list []string, name, prev, next string) bool {
	i := indexCap(list, name)
	return i > 0 && i+1 < len(list) && strings.EqualFold(list[i-1], prev) && strings.EqualFold(list[i+1], next)
}

func TestObjectIDRFC(t *testing.T) {
	_, bound := start(t, Config{
		Listen:      "127.0.0.1:0",
		JmapListen:  "127.0.0.1:0",
		Personality: "personalities/cyrus-3137.json",
	})
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "OBJECTID") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	loginDemo(t, cl)
	sameInbox := cl.cmd("RENAME INBOX INBOX")
	sameInbox.no(t)
	if strings.Contains(sameInbox.tagLine, "MAILBOXID") || strings.Contains(sameInbox.tagLine, "OBJECTID") {
		t.Fatalf("rename inbox inbox %s", sameInbox.tagLine)
	}
	post := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	if !listed(post, "OBJECTID") || listed(post, "OBJECTID+") {
		t.Fatalf("post-login %v", post)
	}
	en := cl.cmd("ENABLE OBJECTID")
	en.mustOK(t)
	if len(en.untagged) != 0 {
		t.Fatalf("ENABLE OBJECTID %q", en.untagged)
	}
	plus := cl.cmd("ENABLE OBJECTID+")
	plus.mustOK(t)
	if len(plus.untagged) != 0 {
		t.Fatalf("ENABLE OBJECTID+ %q", plus.untagged)
	}
	badSel := cl.cmd("SELECT INBOX (OBJECTID (MAILBOXID mb1 ACCOUNTID demo))")
	if badSel.status != "BAD" {
		t.Fatalf("select param %s", badSel.status)
	}
	fail := cl.cmd("CREATE INBOX")
	fail.no(t)
	if strings.Contains(fail.tagLine, "MAILBOXID") || strings.Contains(fail.tagLine, "OBJECTID") {
		t.Fatalf("failed create %s", fail.tagLine)
	}
	made := cl.cmd("CREATE Box").mustOK(t)
	boxID := between(made.tagLine, "[MAILBOXID (", ")]")
	if !objectIDShape(boxID) || !strings.HasPrefix(boxID, "mb") {
		t.Fatalf("create id %q in %s", boxID, made.tagLine)
	}
	sel := cl.cmd("SELECT Box").mustOK(t)
	if countHas(sel.untagged, "* OK [MAILBOXID ("+boxID+")] Ok") != 1 {
		t.Fatalf("select %q", sel.untagged)
	}
	ex := cl.cmd("EXAMINE Box").mustOK(t)
	if countHas(ex.untagged, "* OK [MAILBOXID ("+boxID+")] Ok") != 1 {
		t.Fatalf("examine %q", ex.untagged)
	}
	st := cl.cmd("STATUS Box (MAILBOXID MESSAGES)").mustOK(t)
	if len(st.untagged) != 1 || !strings.Contains(st.untagged[0], "MAILBOXID ("+boxID+")") {
		t.Fatalf("status %q", st.untagged)
	}
	if cl.cmd("STATUS Box (OBJECTID)").status != "BAD" {
		t.Fatal("rfc STATUS OBJECTID")
	}
	ls := cl.cmd(`LIST "" Box RETURN (STATUS (MAILBOXID))`).mustOK(t)
	if !hasSub(ls.untagged, "MAILBOXID ("+boxID+")") {
		t.Fatalf("list status %q", ls.untagged)
	}
	parent := "From: a@example\r\nMessage-ID: <parent@example>\r\nSubject: p\r\n\r\nparent\r\n"
	child := "From: a@example\r\nMessage-ID: <child@example>\r\nReferences: <parent@example>\r\nSubject: c\r\n\r\nchild\r\n"
	cl.cmd("APPEND Box " + imapLit(child)).mustOK(t)
	cl.cmd("APPEND Box " + imapLit(parent)).mustOK(t)
	cl.cmd("APPEND Box " + imapLit(parent)).mustOK(t)
	late := "From: a@example\r\nMessage-ID: <late@example>\r\nIn-Reply-To: <parent@example>\r\nSubject: l\r\n\r\nlate\r\n"
	cl.cmd("APPEND Box " + imapLit(late)).mustOK(t)
	cl.cmd("SELECT Box").mustOK(t)
	ids := fetchIDs(t, cl, "1:4")
	if ids[0].email == ids[1].email || ids[1].email == ids[2].email {
		t.Fatalf("append reused EMAILID %+v", ids)
	}
	if ids[0].thread == ids[1].thread {
		t.Fatalf("orphan child took a later parent %+v", ids)
	}
	if ids[1].thread != ids[3].thread || ids[2].thread == ids[1].thread {
		t.Fatalf("thread parent %+v", ids)
	}
	again := fetchIDs(t, cl, "1")
	if again[0] != ids[0] {
		t.Fatalf("stored thread changed %+v", again)
	}
	for _, id := range ids {
		if id.email == id.thread || id.email == boxID || id.thread == boxID || !strings.HasPrefix(id.thread, "th") || !objectIDShape(id.email) || !objectIDShape(id.thread) {
			t.Fatalf("id shape %+v box %s", id, boxID)
		}
	}
	if cl.cmd("FETCH 1 (OBJECTID)").status != "BAD" {
		t.Fatal("rfc FETCH OBJECTID")
	}
	hit := cl.cmd("SEARCH EMAILID " + ids[1].email).mustOK(t)
	if len(hit.untagged) != 1 || hit.untagged[0] != "* SEARCH 2" {
		t.Fatalf("search %q", hit.untagged)
	}
	miss := cl.cmd("SEARCH EMAILID " + strings.ToUpper(ids[1].email)).mustOK(t)
	if len(miss.untagged) != 1 || miss.untagged[0] != "* SEARCH" {
		t.Fatalf("case %q", miss.untagged)
	}
	none := cl.cmd("UID SEARCH THREADID notreally").mustOK(t)
	if len(none.untagged) != 1 || none.untagged[0] != "* SEARCH" {
		t.Fatalf("unknown %q", none.untagged)
	}
	if cl.cmd(`SEARCH EMAILID "`+ids[1].email+`"`).status != "BAD" {
		t.Fatal("quoted EMAILID")
	}
	uidHit := cl.cmd("UID SEARCH THREADID " + ids[0].thread).mustOK(t)
	if len(uidHit.untagged) != 1 || uidHit.untagged[0] != "* SEARCH 1" {
		t.Fatalf("uid search %q", uidHit.untagged)
	}
	cl.cmd("CREATE Flags").mustOK(t)
	cl.cmd("APPEND Flags " + imapLit(parent)).mustOK(t)
	cl.cmd("SELECT Flags").mustOK(t)
	cl.cmd("COPY 1 Flags").mustOK(t)
	cl.cmd("STORE 1 +FLAGS (\\Flagged)").mustOK(t)
	cl.cmd("STORE 2 +FLAGS (\\Deleted)").mustOK(t)
	cl.cmd("STORE 1 FLAGS (\\Seen)").mustOK(t)
	flagLines := cl.cmd("FETCH 1:2 (FLAGS)").mustOK(t)
	byFlag := fetchBySeq(flagLines.untagged)
	if strings.Contains(byFlag["2"], `\Flagged`) || strings.Contains(byFlag["2"], `\Seen`) || !strings.Contains(byFlag["2"], `\Deleted`) {
		t.Fatalf("independent flags %q", flagLines.untagged)
	}
	if !strings.Contains(byFlag["1"], `\Seen`) || strings.Contains(byFlag["1"], `\Deleted`) {
		t.Fatalf("target flags %q", byFlag["1"])
	}
	cl.cmd("SELECT Box").mustOK(t)
	cl.cmd("CREATE Dest").mustOK(t)
	cl.cmd("COPY 2 Dest").mustOK(t)
	cl.cmd("SELECT Dest").mustOK(t)
	copied := fetchIDs(t, cl, "1")
	if copied[0] != ids[1] {
		t.Fatalf("copy ids %+v want %+v", copied[0], ids[1])
	}
	cl.cmd("CREATE Moved").mustOK(t)
	cl.cmd("MOVE 1 Moved").mustOK(t)
	cl.cmd("SELECT Moved").mustOK(t)
	moved := fetchIDs(t, cl, "1")
	if moved[0] != ids[1] {
		t.Fatalf("move ids %+v", moved)
	}
	cl.cmd("SELECT Dest").mustOK(t)
	if existsOf(t, cl.cmd("SEARCH ALL").mustOK(t)) != 0 {
		t.Fatal("move left the source")
	}
	cl.cmd("SELECT Moved").mustOK(t)
	cl.cmd("ENABLE UIDONLY").mustOK(t)
	uf := cl.cmd("UID FETCH 1 (EMAILID THREADID)").mustOK(t)
	if len(uf.untagged) != 1 || !strings.Contains(uf.untagged[0], "UIDFETCH") || !strings.Contains(uf.untagged[0], "EMAILID ("+moved[0].email+")") || !strings.Contains(uf.untagged[0], "THREADID ("+moved[0].thread+")") {
		t.Fatalf("uidfetch %q", uf.untagged)
	}
	cl.cmd("CLOSE").mustOK(t)
	ren := cl.cmd("RENAME Box Box2").mustOK(t)
	if !strings.Contains(ren.tagLine, "[MAILBOXID ("+boxID+")]") {
		t.Fatalf("rename %s", ren.tagLine)
	}
	if mailboxStatusID(t, cl, "Box2") != boxID {
		t.Fatal("rename dropped MAILBOXID")
	}
	same := cl.cmd("RENAME Box2 Box2").mustOK(t)
	if !strings.Contains(same.tagLine, "[MAILBOXID ("+boxID+")]") || mailboxStatusID(t, cl, "Box2") != boxID {
		t.Fatalf("same-name rename %s", same.tagLine)
	}
	inboxID := mailboxStatusID(t, cl, "INBOX")
	inboxN := statusItem(t, cl, "INBOX", "MESSAGES")
	genID := mailboxStatusID(t, cl, "INBOX/Generated")
	cl.cmd("RENAME INBOX SavedInbox").mustOK(t)
	if mailboxStatusID(t, cl, "SavedInbox") != inboxID {
		t.Fatal("RENAME INBOX dropped the id")
	}
	neu := mailboxStatusID(t, cl, "INBOX")
	if neu == inboxID || statusItem(t, cl, "SavedInbox", "MESSAGES") != inboxN || statusItem(t, cl, "INBOX", "MESSAGES") != "0" {
		t.Fatalf("inbox move old %s new %s", inboxID, neu)
	}
	if mailboxStatusID(t, cl, "INBOX/Generated") != genID {
		t.Fatal("inferior moved")
	}
	cl.cmd("DELETE Box2").mustOK(t)
	re := cl.cmd("CREATE Box2").mustOK(t)
	reID := between(re.tagLine, "[MAILBOXID (", ")]")
	if reID == boxID || reID == "" {
		t.Fatalf("deleted id reused %s", reID)
	}
	session := jmapSession(t, bound.Jmap, "demo", "secret")
	accountID := objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:mail"].(string)
	if accountID != "demo" {
		t.Fatalf("account %s", accountID)
	}
	got := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{
			"accountId":  accountID,
			"ids":        []any{moved[0].email},
			"properties": []any{"id", "threadId"},
		}, "0"},
	})
	rows := emailList(t, got)
	if len(rows) != 1 || rows[0]["id"] != moved[0].email || rows[0]["threadId"] != moved[0].email {
		t.Fatalf("jmap thread %+v", rows)
	}
}

func TestObjectIDPlus(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/stalwart.json"})
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "OBJECTID") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	loginDemo(t, cl)
	sameInbox := cl.cmd("RENAME INBOX INBOX")
	sameInbox.no(t)
	if strings.Contains(sameInbox.tagLine, "OBJECTID") || strings.Contains(sameInbox.tagLine, "MAILBOXID") {
		t.Fatalf("rename inbox inbox %s", sameInbox.tagLine)
	}
	post := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	if listed(post, "OBJECTID") || !listed(post, "OBJECTID+") {
		t.Fatalf("post %v", post)
	}
	early := cl.cmd("CREATE Early").mustOK(t)
	if strings.Contains(early.tagLine, "MAILBOXID") || strings.Contains(early.tagLine, "OBJECTID") {
		t.Fatalf("create before activation %s", early.tagLine)
	}
	sel := cl.cmd("SELECT Early").mustOK(t)
	if hasSub(sel.untagged, "MAILBOXID") || hasSub(sel.untagged, "OBJECTID") || hasSub(sel.untagged, "ENABLED") {
		t.Fatalf("select before activation %q", sel.untagged)
	}
	for _, bad := range []string{
		"STATUS Early (MAILBOXID)",
		"FETCH 1 (EMAILID)",
		"SEARCH EMAILID em1",
		"UID SEARCH THREADID th1",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s accepted", bad)
		}
	}
	unknown := cl.cmd("SELECT Early (NOTAPARAM)")
	if unknown.status != "BAD" || hasSub(unknown.untagged, "ENABLED") {
		t.Fatalf("unknown param %s %q", unknown.status, unknown.untagged)
	}
	qr := cl.cmd("SELECT Early (OBJECTID (MAILBOXID zz ACCOUNTID demo) QRESYNC (1 1))")
	if qr.status != "BAD" || hasSub(qr.untagged, "ENABLED") {
		t.Fatalf("qresync before enable %s %q", qr.status, qr.untagged)
	}
	still := cl.cmd("CREATE Still").mustOK(t)
	if strings.Contains(still.tagLine, "OBJECTID") {
		t.Fatalf("activated by a bad select %s", still.tagLine)
	}
	en := cl.cmd("ENABLE OBJECTID+").mustOK(t)
	if len(en.untagged) != 1 || en.untagged[0] != "* ENABLED OBJECTID+" {
		t.Fatalf("enable %q", en.untagged)
	}
	made := cl.cmd("CREATE Kept").mustOK(t)
	keptID := between(made.tagLine, "[OBJECTID (MAILBOXID ", " ACCOUNTID ")
	acct := between(made.tagLine, "ACCOUNTID ", ")]")
	if !objectIDShape(keptID) || acct != "demo" || strings.Contains(made.tagLine, "[MAILBOXID") {
		t.Fatalf("create %s", made.tagLine)
	}
	again := cl.cmd("ENABLE OBJECTID+").mustOK(t)
	if len(again.untagged) != 1 || again.untagged[0] != "* ENABLED" {
		t.Fatalf("second enable %q", again.untagged)
	}
	body := "From: a@example\r\nMessage-ID: <one@example>\r\nSubject: one\r\n\r\none\r\n"
	cl.cmd("APPEND Kept " + imapLit(body)).mustOK(t)
	cl.cmd("SELECT Kept").mustOK(t)
	if hasSub(cl.cmd("NOOP").mustOK(t).untagged, "ENABLED OBJECTID+") {
		t.Fatal("activation repeated")
	}
	obj := cl.cmd("FETCH 1 (OBJECTID)").mustOK(t)
	one := []idPair{{between(obj.untagged[0], "EMAILID ", " THREADID"), between(obj.untagged[0], "THREADID ", ")")}}
	if len(obj.untagged) != 1 || !strings.Contains(obj.untagged[0], "FETCH") || strings.Contains(obj.untagged[0], "UIDFETCH") || !strings.Contains(obj.untagged[0], "OBJECTID (EMAILID "+one[0].email+" THREADID "+one[0].thread+")") {
		t.Fatalf("fetch object %q", obj.untagged)
	}
	if cl.cmd("FETCH 1 (EMAILID)").status != "BAD" || cl.cmd("STATUS Kept (MAILBOXID)").status != "BAD" {
		t.Fatal("standalone accepted after activation")
	}
	st := cl.cmd("STATUS Kept (OBJECTID)").mustOK(t)
	wantStatus := "OBJECTID (MAILBOXID " + keptID + " ACCOUNTID demo)"
	if !hasSub(st.untagged, wantStatus) {
		t.Fatalf("status %q", st.untagged)
	}
	ls := cl.cmd(`LIST "" Kept RETURN (OBJECTID)`).mustOK(t)
	if !hasSub(ls.untagged, "Kept (OBJECTID (MAILBOXID "+keptID+" ACCOUNTID demo))") {
		t.Fatalf("list %q", ls.untagged)
	}
	lstat := cl.cmd(`LIST "" Kept RETURN (STATUS (OBJECTID))`).mustOK(t)
	if !hasSub(lstat.untagged, wantStatus) {
		t.Fatalf("list status %q", lstat.untagged)
	}
	ren := cl.cmd("RENAME Kept Kept2").mustOK(t)
	if !strings.Contains(ren.tagLine, "[OBJECTID (MAILBOXID "+keptID+" ACCOUNTID demo)]") || mailboxObject(t, cl, "Kept2") != keptID {
		t.Fatalf("rename %s", ren.tagLine)
	}
	cl.cmd("SELECT Kept2").mustOK(t)
	cl.cmd("COPY 1 Kept2").mustOK(t)
	cl.cmd("STORE 1 +FLAGS (\\Flagged Work)").mustOK(t)
	cl.cmd("STORE 2 +FLAGS (\\Deleted)").mustOK(t)
	cl.cmd("STORE 1 FLAGS (\\Seen)").mustOK(t)
	flags := cl.cmd("FETCH 1:2 (FLAGS)").mustOK(t)
	by := fetchBySeq(flags.untagged)
	if !strings.Contains(by["1"], `\Seen`) || strings.Contains(by["1"], `\Deleted`) || strings.Contains(by["1"], `\Flagged`) {
		t.Fatalf("target %s", by["1"])
	}
	if !strings.Contains(by["2"], `\Seen`) || !strings.Contains(by["2"], `\Deleted`) || strings.Contains(by["2"], `\Flagged`) || strings.Contains(by["2"], "Work") {
		t.Fatalf("propagated %s", by["2"])
	}
	silent := cl.cmd("STORE 1 +FLAGS.SILENT (\\Flagged)").mustOK(t)
	if hasSub(silent.untagged, "FETCH") {
		t.Fatalf("silent store %q", silent.untagged)
	}
	noop := cl.cmd("NOOP").mustOK(t)
	if hasSub(noop.untagged, "FETCH") || hasSub(noop.untagged, "FLAGS") {
		t.Fatalf("silent flush %q", noop.untagged)
	}
	after := cl.cmd("FETCH 1:2 (FLAGS)").mustOK(t)
	by = fetchBySeq(after.untagged)
	if !strings.Contains(by["1"], `\Flagged`) || !strings.Contains(by["2"], `\Flagged`) {
		t.Fatalf("silent did not apply %q", after.untagged)
	}
	cl.cmd("ENABLE UIDONLY").mustOK(t)
	uf := cl.cmd("UID FETCH 1 (OBJECTID)").mustOK(t)
	if !hasSub(uf.untagged, "UIDFETCH") || !hasSub(uf.untagged, "OBJECTID (EMAILID ") {
		t.Fatalf("uidfetch %q", uf.untagged)
	}
	cl.cmd("CLOSE").mustOK(t)
	cl.cmd("ENABLE QRESYNC").mustOK(t)
	learn := cl.cmd("SELECT INBOX").mustOK(t)
	if hasSub(learn.untagged, "* ENABLED OBJECTID+") {
		t.Fatal("select without the parameter re-announced")
	}
	_, _, uidv, _ := parseSelect(t, learn)
	if uidv == 0 || !hasSub(learn.untagged, "[HIGHESTMODSEQ ") {
		t.Fatalf("resync base %q", learn.untagged)
	}
	hi := bracketNum(joinLines(learn.untagged), "HIGHESTMODSEQ")
	both := cl.cmd("SELECT INBOX (OBJECTID (MAILBOXID nomatch ACCOUNTID demo) QRESYNC (" + strconv.FormatUint(uint64(uidv), 10) + " " + strconv.FormatUint(uint64(hi), 10) + "))").mustOK(t)
	if !hasSub(both.untagged, "* OK [OBJECTID (MAILBOXID ") {
		t.Fatalf("qresync beside object %q", both.untagged)
	}
	cl.cmd("CREATE Other").mustOK(t)
	cl.cmd("UNAUTHENTICATE").mustOK(t)
	loginDemo(t, cl)
	back := cl.cmd("CREATE After").mustOK(t)
	if strings.Contains(back.tagLine, "OBJECTID") || strings.Contains(back.tagLine, "MAILBOXID") {
		t.Fatalf("activation survived unauthenticate %s", back.tagLine)
	}
	wake := cl.cmd("STATUS Kept2 (OBJECTID)").mustOK(t)
	if len(wake.untagged) < 2 || wake.untagged[0] != "* ENABLED OBJECTID+" || !hasSub(wake.untagged, "OBJECTID (MAILBOXID "+keptID+" ACCOUNTID demo)") {
		t.Fatalf("status activation %q", wake.untagged)
	}
	cl2 := dial(t, bound.Listen)
	loginDemo(t, cl2)
	fetchAct := cl2.cmd("SELECT Kept2").mustOK(t)
	if hasSub(fetchAct.untagged, "ENABLED") {
		t.Fatalf("select name activated %q", fetchAct.untagged)
	}
	pulled := cl2.cmd("FETCH 1 (OBJECTID)").mustOK(t)
	if len(pulled.untagged) < 2 || pulled.untagged[0] != "* ENABLED OBJECTID+" {
		t.Fatalf("fetch activation %q", pulled.untagged)
	}
	cl3 := dial(t, bound.Listen)
	loginDemo(t, cl3)
	listAct := cl3.cmd(`LIST "" Kept2 RETURN (OBJECTID)`).mustOK(t)
	if len(listAct.untagged) < 2 || listAct.untagged[0] != "* ENABLED OBJECTID+" {
		t.Fatalf("list activation %q", listAct.untagged)
	}
	cl4 := dial(t, bound.Listen)
	loginDemo(t, cl4)
	cl4.cmd("CREATE Named").mustOK(t)
	cl4.cmd("APPEND Named " + imapLit(body)).mustOK(t)
	named := cl4.cmd("STATUS Kept2 (UIDVALIDITY OBJECTID)").mustOK(t)
	if named.untagged[0] != "* ENABLED OBJECTID+" {
		t.Fatalf("status list activation %q", named.untagged)
	}
	keepV := statusItem(t, cl4, "Kept2", "UIDVALIDITY")
	nameV := statusItem(t, cl4, "Named", "UIDVALIDITY")
	byName := cl4.cmd("SELECT Named (OBJECTID (MAILBOXID " + keptID + " ACCOUNTID other))").mustOK(t)
	exists, _, fallV, _ := parseSelect(t, byName)
	if strconv.FormatUint(uint64(fallV), 10) != nameV || exists != 1 {
		t.Fatalf("account mismatch exists %d uv %d want %s", exists, fallV, nameV)
	}
	pick := cl4.cmd("SELECT Named (OBJECTID (MAILBOXID " + keptID + " ACCOUNTID demo))").mustOK(t)
	gotExists, _, gotV, _ := parseSelect(t, pick)
	if strconv.FormatUint(uint64(gotV), 10) != keepV || gotExists != 1 || !hasSub(pick.untagged, "* OK [OBJECTID (MAILBOXID "+keptID+" ACCOUNTID demo)] Ok") {
		t.Fatalf("select by id exists %d uv %d want %s %q", gotExists, gotV, keepV, pick.untagged)
	}
	cl5 := dial(t, bound.Listen)
	loginDemo(t, cl5)
	gone := cl5.cmd("SELECT NoSuchBox (OBJECTID (MAILBOXID zz ACCOUNTID demo))")
	if gone.status != "NO" || len(gone.untagged) != 1 || gone.untagged[0] != "* ENABLED OBJECTID+" {
		t.Fatalf("missing mailbox activation %s %q", gone.status, gone.untagged)
	}
	now := cl5.cmd("CREATE Now").mustOK(t)
	if !strings.Contains(now.tagLine, "[OBJECTID (MAILBOXID ") || !strings.Contains(now.tagLine, "ACCOUNTID demo)") {
		t.Fatalf("create after select activation %s", now.tagLine)
	}
}

func TestObjectIDLastCopyExpunge(t *testing.T) {
	_, bound := start(t, Config{
		Listen:     "127.0.0.1:0",
		JmapListen: "127.0.0.1:0",
	})
	session := jmapSession(t, bound.Jmap, "demo", "secret")
	accountID := objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:mail"].(string)
	cl := dial(t, bound.Listen)
	loginDemo(t, cl)
	body := "From: a@example\r\nMessage-ID: <twin@example>\r\nSubject: twin\r\n\r\ntwin\r\n"
	cl.cmd("CREATE Twins").mustOK(t)
	cl.cmd("APPEND Twins " + imapLit(body)).mustOK(t)
	cl.cmd("SELECT Twins").mustOK(t)
	cl.cmd("COPY 1 Twins").mustOK(t)
	id := fetchIDs(t, cl, "1")[0].email
	if fetchIDs(t, cl, "2")[0].email != id {
		t.Fatal("copy did not keep EMAILID")
	}
	cl.cmd("STORE 1 +FLAGS (\\Deleted)").mustOK(t)
	mark := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	cl.cmd("EXPUNGE").mustOK(t)
	ch := emailChanges(t, bound.Jmap, session, accountID, mark)
	if strings.Join(stringArray(t, ch["destroyed"]), ",") != "" || strings.Join(stringArray(t, ch["updated"]), ",") != id {
		t.Fatalf("one copy left %+v", ch)
	}
	if len(emailList(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{"accountId": accountID, "ids": []any{id}, "properties": []any{"id"}}, "0"},
	}))) != 1 {
		t.Fatal("remaining copy was destroyed")
	}
	cl.cmd("STORE 1 +FLAGS (\\Deleted)").mustOK(t)
	mark = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	cl.cmd("EXPUNGE").mustOK(t)
	ch = emailChanges(t, bound.Jmap, session, accountID, mark)
	if strings.Join(stringArray(t, ch["destroyed"]), ",") != id || strings.Join(stringArray(t, ch["updated"]), ",") != "" {
		t.Fatalf("last expunge %+v", ch)
	}

	cl.cmd("CREATE Closed").mustOK(t)
	cl.cmd("APPEND Closed " + imapLit(body)).mustOK(t)
	cl.cmd("SELECT Closed").mustOK(t)
	cl.cmd("COPY 1 Closed").mustOK(t)
	closedID := fetchIDs(t, cl, "1")[0].email
	cl.cmd("STORE 1:2 +FLAGS (\\Deleted)").mustOK(t)
	mark = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	cl.cmd("CLOSE").mustOK(t)
	ch = emailChanges(t, bound.Jmap, session, accountID, mark)
	if strings.Join(stringArray(t, ch["destroyed"]), ",") != closedID || strings.Join(stringArray(t, ch["updated"]), ",") != "" {
		t.Fatalf("last close %+v", ch)
	}
}

func emailChanges(t *testing.T, addr string, session map[string]any, accountID, since string) map[string]any {
	t.Helper()
	return methodArgs(t, jmapCall(t, addr, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": since}, "0"},
	}), "Email/changes")
}

func TestObjectIDAbsent(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-22.json"})
	cl := dial(t, bound.Listen)
	loginDemo(t, cl)
	sameInbox := cl.cmd("RENAME INBOX INBOX")
	sameInbox.no(t)
	if strings.Contains(sameInbox.tagLine, "MAILBOXID") || strings.Contains(sameInbox.tagLine, "OBJECTID") {
		t.Fatalf("rename inbox inbox %s", sameInbox.tagLine)
	}
	post := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	if listed(post, "OBJECTID") || listed(post, "OBJECTID+") {
		t.Fatalf("post %v", post)
	}
	made := cl.cmd("CREATE Plain").mustOK(t)
	if strings.Contains(made.tagLine, "MAILBOXID") || strings.Contains(made.tagLine, "OBJECTID") {
		t.Fatalf("create %s", made.tagLine)
	}
	sel := cl.cmd("SELECT Plain").mustOK(t)
	if hasSub(sel.untagged, "MAILBOXID") || hasSub(sel.untagged, "OBJECTID") {
		t.Fatalf("select %q", sel.untagged)
	}
	for _, bad := range []string{
		"STATUS Plain (MAILBOXID)",
		"STATUS Plain (OBJECTID)",
		"FETCH 1 (EMAILID)",
		"FETCH 1 (THREADID)",
		"FETCH 1 (OBJECTID)",
		"SEARCH EMAILID em1",
		"UID SEARCH THREADID th1",
		"SELECT Plain (OBJECTID (MAILBOXID mb1 ACCOUNTID demo))",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s accepted", bad)
		}
	}
	for _, en := range []string{"ENABLE OBJECTID", "ENABLE OBJECTID+"} {
		rp := cl.cmd(en).mustOK(t)
		if len(rp.untagged) != 0 {
			t.Fatalf("%s %q", en, rp.untagged)
		}
	}
	ren := cl.cmd("RENAME INBOX Elsewhere")
	ren.no(t)
	if strings.Contains(ren.tagLine, "MAILBOXID") || strings.Contains(ren.tagLine, "OBJECTID") {
		t.Fatalf("rename inbox %s", ren.tagLine)
	}
}

func loginDemo(t *testing.T, cl *imapClient) {
	t.Helper()
	cl.cmd("LOGIN demo secret").mustOK(t)
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	j := strings.Index(s, b)
	if j < 0 {
		return ""
	}
	return s[:j]
}

func countHas(lines []string, sub string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func hasSub(lines []string, sub string) bool {
	return countHas(lines, sub) > 0
}

func objectIDShape(s string) bool {
	if len(s) < 1 || len(s) > 255 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

type idPair struct{ email, thread string }

func fetchIDs(t *testing.T, cl *imapClient, set string) []idPair {
	t.Helper()
	rp := cl.cmd("FETCH " + set + " (EMAILID THREADID)").mustOK(t)
	var out []idPair
	for _, l := range rp.untagged {
		if !strings.Contains(l, "EMAILID") {
			continue
		}
		out = append(out, idPair{between(l, "EMAILID (", ")"), between(l, "THREADID (", ")")})
	}
	if len(out) == 0 {
		t.Fatalf("no ids in %q", rp.untagged)
	}
	return out
}

func mailboxStatusID(t *testing.T, cl *imapClient, name string) string {
	t.Helper()
	return between(statusItemLine(t, cl, name, "MAILBOXID"), "MAILBOXID (", ")")
}

func mailboxObject(t *testing.T, cl *imapClient, name string) string {
	t.Helper()
	return between(statusItemLine(t, cl, name, "OBJECTID"), "MAILBOXID ", " ACCOUNTID")
}

func statusItem(t *testing.T, cl *imapClient, name, item string) string {
	t.Helper()
	line := statusItemLine(t, cl, name, item)
	i := strings.LastIndex(line, item+" ")
	if i < 0 {
		t.Fatalf("no %s in %s", item, line)
	}
	rest := line[i+len(item)+1:]
	if j := strings.IndexAny(rest, " )"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func statusItemLine(t *testing.T, cl *imapClient, name, item string) string {
	t.Helper()
	rp := cl.cmd("STATUS " + name + " (" + item + ")").mustOK(t)
	if len(rp.untagged) != 1 {
		t.Fatalf("status %q", rp.untagged)
	}
	return rp.untagged[0]
}

func fetchBySeq(lines []string) map[string]string {
	out := map[string]string{}
	for _, l := range lines {
		fs := strings.Fields(l)
		if len(fs) >= 3 && fs[0] == "*" {
			out[fs[1]] = l
		}
	}
	return out
}

func existsOf(t *testing.T, rp *reply) int {
	t.Helper()
	if len(rp.untagged) != 1 || !strings.HasPrefix(rp.untagged[0], "* SEARCH") {
		t.Fatalf("search %q", rp.untagged)
	}
	return len(strings.Fields(rp.untagged[0])) - 2
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}
