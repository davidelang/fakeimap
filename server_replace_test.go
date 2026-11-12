package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplaceLists(t *testing.T) {
	if _, ok := implementedCaps["REPLACE"]; !ok || !capImplemented("REPLACE") {
		t.Fatal("REPLACE is not implemented")
	}
	allow := map[string]bool{
		"cyrus-3124.json":  true,
		"cyrus-3137.json":  true,
		"sasl.json":        true,
		"dovecot-245.json": true,
		"dovecot-dev.json": true,
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
		got := listed(p.GreetingCaps, "REPLACE") || listed(p.PreLogin, "REPLACE") || listed(p.PostLogin, "REPLACE")
		if got != allow[e.Name()] {
			t.Fatalf("%s REPLACE=%v", e.Name(), got)
		}
		if listed(p.GreetingCaps, "REPLACE") {
			t.Fatalf("%s greeting lists REPLACE", e.Name())
		}
	}
	c3124, err := loadPersonality("personalities/cyrus-3124.json")
	if err != nil {
		t.Fatal(err)
	}
	c3137, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	sasl, err := loadPersonality("personalities/sasl.json")
	if err != nil {
		t.Fatal(err)
	}
	if listed(sasl.PreLogin, "REPLACE") || !betweenCaps(c3124.PreLogin, "REPLACE", "QUOTASET", "SASL-IR") || !betweenCaps(c3137.PreLogin, "REPLACE", "QUOTASET", "SASL-IR") {
		t.Fatal("pre-login REPLACE position")
	}
	if !betweenCaps(c3124.PostLogin, "REPLACE", "QUOTASET", "SAVEDATE") || !betweenCaps(c3137.PostLogin, "REPLACE", "QUOTASET", "SAVEDATE") || !betweenCaps(sasl.PostLogin, "REPLACE", "QUOTASET", "SAVEDATE") {
		t.Fatal("post-login REPLACE position")
	}
	for _, file := range []string{"personalities/dovecot-245.json", "personalities/dovecot-dev.json"} {
		p, err := loadPersonality(file)
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.PreLogin, "REPLACE") || !betweenCaps(p.PostLogin, "REPLACE", "MOVE", "PREVIEW") {
			t.Fatalf("%s REPLACE position", file)
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "REPLACE") || !strings.Contains(string(gaps), "CATENATE") {
		t.Fatal("gaps file")
	}
	facts, err := os.ReadFile("docs/facts/imap-server.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(facts)
	for _, phrase := range []string{
		"`REPLACE`, when listed",
		"must authenticate first",
		"or does neither",
		"new `EMAILID`",
		"[UIDREQUIRED] Message numbers are not allowed once UIDONLY is enabled",
		"cyrus-3124.json",
		"cyrus-3137.json",
		"sasl.json",
		"dovecot-245.json",
		"dovecot-dev.json",
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
	off := strings.Index(guide, "## Capabilities that stay off")
	if off < 0 {
		t.Fatal("stay-off section missing")
	}
	if !strings.Contains(guide[:off], "`REPLACE`") || !strings.Contains(guide[:off], "docs/facts/imap-server.md") || !strings.Contains(guide[:off], "new `EMAILID`") {
		t.Fatal("guide missing the short REPLACE note")
	}
	tokens := strings.Index(guide[off:], "the tokens are")
	if tokens < 0 || strings.Contains(guide[off+tokens:], "`REPLACE`") {
		t.Fatal("stay-off still lists REPLACE")
	}
	if !strings.Contains(guide[off:], "`CATENATE`") {
		t.Fatal("stay-off dropped CATENATE")
	}
	if strings.Contains(guide, "noteExpungedIDs") || strings.Contains(guide, "appendLocked") || strings.Contains(guide, "formatUIDSet") {
		t.Fatal("guide pastes the facts file")
	}
}

func TestReplace(t *testing.T) {
	_, bound := start(t, Config{
		Listen:     "127.0.0.1:0",
		JmapListen: "127.0.0.1:0",
	})
	session := jmapSession(t, bound.Jmap, "demo", "secret")
	accountID := objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:mail"].(string)
	early := dial(t, bound.Listen)
	if strings.Contains(early.greeting, "REPLACE") {
		t.Fatalf("greeting lists REPLACE: %q", early.greeting)
	}
	if !listed(capsOf(early.cmd("CAPABILITY").mustOK(t).untagged[0]), "REPLACE") {
		t.Fatal("pre-login missing REPLACE")
	}
	pre := early.cmd("REPLACE 1 INBOX " + imapLit("nope"))
	if pre.status != "NO" || !strings.Contains(pre.text, "must authenticate first") {
		t.Fatalf("pre-login %s %s", pre.status, pre.tagLine)
	}
	preUID := early.cmd("UID REPLACE 1 INBOX " + imapLit("nope"))
	if preUID.status != "NO" || !strings.Contains(preUID.text, "must authenticate first") {
		t.Fatalf("pre-login uid %s %s", preUID.status, preUID.tagLine)
	}
	loginDemo(t, early)
	if !listed(capsOf(early.cmd("CAPABILITY").mustOK(t).untagged[0]), "REPLACE") {
		t.Fatal("post-login missing REPLACE")
	}
	if rp := early.cmd("REPLACE 1 INBOX " + imapLit("nope")); rp.status != "NO" || !strings.Contains(rp.text, "not selected") {
		t.Fatalf("not selected %s %s", rp.status, rp.tagLine)
	}

	cl := early
	cl.cmd("CREATE Box").mustOK(t)
	cl.cmd("SELECT Box").mustOK(t)
	cl.cmd("APPEND Box " + imapLit("old-a")).mustOK(t)
	cl.cmd("APPEND Box " + imapLit("old-b")).mustOK(t)
	cl.cmd(`STORE 1 +FLAGS (\Flagged \Seen)`).mustOK(t)
	old := fetchIDs(t, cl, "1:2")
	oldUID := fetchItem(t, cl, 1, "UID")
	if old[0].email == old[1].email || old[0].thread == "" {
		t.Fatalf("seed ids %+v", old)
	}
	uv := statusField(t, cl, "Box", "UIDVALIDITY")
	if statusField(t, cl, "Box", "MESSAGES") != "2" || statusField(t, cl, "Box", "UIDNEXT") != "3" {
		t.Fatal("seed counts")
	}

	other := dial(t, bound.Listen)
	loginDemo(t, other)
	other.cmd("SELECT Box").mustOK(t)
	mark := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	same := cl.cmd("REPLACE 1 Box " + imapLit("new-a"))
	same.mustOK(t)
	if !strings.Contains(same.tagLine, "[APPENDUID "+uv+" ") || !strings.HasSuffix(same.text, "REPLACE completed") {
		t.Fatalf("ok %s", same.tagLine)
	}
	_, newUID := appendUIDSet(t, same.tagLine)
	if newUID == oldUID || strings.ContainsAny(newUID, ":,") {
		t.Fatalf("new uid %s old %s", newUID, oldUID)
	}
	if strings.Join(same.untagged, "\n") != "* 1 EXPUNGE\n* 2 EXISTS\n* 1 RECENT" {
		t.Fatalf("same untagged %q", same.untagged)
	}
	if statusField(t, cl, "Box", "MESSAGES") != "2" || statusField(t, cl, "Box", "UIDNEXT") != "4" {
		t.Fatal("same-mailbox count changed")
	}
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); got != "old-b" {
		t.Fatalf("kept %q", got)
	}
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 2 (BODY.PEEK[])").mustOK(t)))); got != "new-a" {
		t.Fatalf("new body %q", got)
	}
	if flags := fetchItem(t, cl, 2, "FLAGS"); strings.Contains(flags, `\Flagged`) || strings.Contains(flags, `\Seen`) || strings.Contains(flags, `\Draft`) {
		t.Fatalf("omitted flags %s", flags)
	}
	ids := fetchIDs(t, cl, "1:2")
	if ids[0] != old[1] || ids[1].email == old[0].email || ids[1].thread == old[0].thread || ids[1].email == ids[0].email {
		t.Fatalf("ids old %+v now %+v", old, ids)
	}
	ch := emailChanges(t, bound.Jmap, session, accountID, mark)
	if strings.Join(stringArray(t, ch["created"]), ",") != ids[1].email || strings.Join(stringArray(t, ch["destroyed"]), ",") != old[0].email || strings.Join(stringArray(t, ch["updated"]), ",") != "" {
		t.Fatalf("last copy %+v", ch)
	}
	seen := other.cmd("NOOP").mustOK(t)
	if strings.Join(seen.untagged, "\n") != "* 1 EXPUNGE\n* 2 EXISTS\n* 1 RECENT" {
		t.Fatalf("other session %q", seen.untagged)
	}

	cl.cmd("COPY 1 Box").mustOK(t)
	copied := fetchIDs(t, cl, "1")[0]
	if fetchIDs(t, cl, "3")[0].email != copied.email {
		t.Fatal("copy did not keep EMAILID")
	}
	mark = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	kept := cl.cmd(`REPLACE 1 Box (\Draft) "01-Jan-1990 00:00:00 +0000" ` + imapLit("new-b"))
	kept.mustOK(t)
	if statusField(t, cl, "Box", "MESSAGES") != "3" {
		t.Fatal("copy replace changed the count")
	}
	now := fetchIDs(t, cl, "1:3")
	if now[1].email != copied.email || now[2].email == copied.email || now[2].thread == copied.thread {
		t.Fatalf("remaining copy %+v want %s", now, copied.email)
	}
	if flags := fetchItem(t, cl, 3, "FLAGS"); !strings.Contains(flags, `\Draft`) || strings.Contains(flags, `\Flagged`) {
		t.Fatalf("new flags %s", flags)
	}
	line := fetchLine(t, cl.cmd("FETCH 3 (INTERNALDATE)").mustOK(t))
	if got := fetchQuoted(t, line, "INTERNALDATE"); got != "01-Jan-1990 00:00:00 +0000" {
		t.Fatalf("internal %q", got)
	}
	ch = emailChanges(t, bound.Jmap, session, accountID, mark)
	if strings.Join(stringArray(t, ch["created"]), ",") != now[2].email || strings.Join(stringArray(t, ch["updated"]), ",") != copied.email || strings.Join(stringArray(t, ch["destroyed"]), ",") != "" {
		t.Fatalf("remaining %+v", ch)
	}

	cl.cmd("CREATE Other").mustOK(t)
	watch := dial(t, bound.Listen)
	loginDemo(t, watch)
	watch.cmd("SELECT Other").mustOK(t)
	srcWatch := dial(t, bound.Listen)
	loginDemo(t, srcWatch)
	srcWatch.cmd("SELECT Box").mustOK(t)
	beforeSrc := statusField(t, cl, "Box", "MESSAGES")
	away := cl.cmd("REPLACE 1 Other " + imapLit("away"))
	away.mustOK(t)
	if len(away.untagged) != 1 || away.untagged[0] != "* 1 EXPUNGE" {
		t.Fatalf("away untagged %q", away.untagged)
	}
	if statusField(t, cl, "Box", "MESSAGES") != "2" || statusField(t, cl, "Other", "MESSAGES") != "1" {
		t.Fatal("different destination counts")
	}
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); got == "new-a" {
		t.Fatal("source still has the replaced body")
	}
	cl.cmd("SELECT Other").mustOK(t)
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); got != "away" {
		t.Fatalf("dest body %q", got)
	}
	if statusField(t, cl, "Box", "MESSAGES") != "2" || beforeSrc != "3" {
		t.Fatal("source count")
	}
	srcSeen := srcWatch.cmd("NOOP").mustOK(t)
	if len(srcSeen.untagged) != 1 || srcSeen.untagged[0] != "* 1 EXPUNGE" {
		t.Fatalf("source session %q", srcSeen.untagged)
	}
	dstSeen := watch.cmd("NOOP").mustOK(t)
	if strings.Join(dstSeen.untagged, "\n") != "* 1 EXISTS\n* 1 RECENT" {
		t.Fatalf("dest session %q", dstSeen.untagged)
	}

	cl.cmd("SELECT Box").mustOK(t)
	snap := statusField(t, cl, "Box", "UIDNEXT")
	body := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t))))
	for _, line := range []string{
		"REPLACE 1,2 Box " + imapLit("no"),
		"REPLACE 1:2 Box " + imapLit("no"),
		"REPLACE 1 Box " + imapLit("aa") + " " + imapLit("bb"),
		"REPLACE 1 Box CATENATE (TEXT " + imapLit("body") + ")",
		`REPLACE 1 Box (\Seen) CATENATE (TEXT ` + imapLit("body") + ")",
		"UID REPLACE 1:2 Box " + imapLit("no"),
		"UID REPLACE 99 Box " + imapLit("no"),
		"REPLACE 9 Box " + imapLit("no"),
	} {
		rp := cl.cmd(line)
		if line == "UID REPLACE 99 Box "+imapLit("no") || line == "REPLACE 9 Box "+imapLit("no") {
			if rp.status != "NO" || len(rp.untagged) != 0 {
				t.Fatalf("missing %s %q", rp.tagLine, rp.untagged)
			}
		} else if rp.status != "BAD" || len(rp.untagged) != 0 {
			t.Fatalf("syntax %s %q", rp.tagLine, rp.untagged)
		}
		if got := statusField(t, cl, "Box", "UIDNEXT"); got != snap {
			t.Fatalf("after %s uidnext %s", line, got)
		}
		if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); got != body {
			t.Fatalf("after %s body %q", line, got)
		}
	}
	miss := cl.cmd("REPLACE 1 Missing " + imapLit("no"))
	if miss.status != "NO" || !strings.Contains(miss.tagLine, "[TRYCREATE]") || len(miss.untagged) != 0 {
		t.Fatalf("trycreate %s %q", miss.tagLine, miss.untagged)
	}
	if st := cl.cmd("STATUS Missing (MESSAGES)"); st.status != "NO" {
		t.Fatalf("missing mailbox created %s", st.tagLine)
	}
	badFlag := cl.cmd(`REPLACE 1 Box (\NoSuch) ` + imapLit("no"))
	if badFlag.status != "BAD" || len(badFlag.untagged) != 0 {
		t.Fatalf("bad flag %s %q", badFlag.tagLine, badFlag.untagged)
	}
	cl.cmd("CREATE Empty").mustOK(t)
	cl.cmd("SELECT Empty").mustOK(t)
	star := cl.cmd("REPLACE * Empty " + imapLit("no"))
	if star.status != "NO" || len(star.untagged) != 0 || statusField(t, cl, "Empty", "MESSAGES") != "0" || statusField(t, cl, "Empty", "UIDNEXT") != "1" {
		t.Fatalf("empty star %s %s messages changed", star.status, star.tagLine)
	}
	uidStar := cl.cmd("UID REPLACE * Empty " + imapLit("no"))
	if uidStar.status != "NO" || statusField(t, cl, "Empty", "UIDNEXT") != "1" {
		t.Fatalf("empty uid star %s %s", uidStar.status, uidStar.tagLine)
	}

	cl.cmd("SELECT Box").mustOK(t)
	roBody := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t))))
	roNext := statusField(t, cl, "Box", "UIDNEXT")
	cl.cmd("EXAMINE Box").mustOK(t)
	ro := cl.cmd("REPLACE 1 Box " + imapLit("ro"))
	if ro.status != "NO" || !strings.Contains(ro.text, "read-only") || len(ro.untagged) != 0 {
		t.Fatalf("examine %s %q", ro.tagLine, ro.untagged)
	}
	cl.cmd("SELECT Box").mustOK(t)
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); got != roBody || statusField(t, cl, "Box", "UIDNEXT") != roNext {
		t.Fatal("examine stored or expunged")
	}

	uid := fetchItem(t, cl, 1, "UID")
	cl.cmd("ENABLE UIDONLY").mustOK(t)
	banned := cl.cmd("REPLACE 1 Box " + imapLit("seq"))
	if banned.status != "BAD" || banned.text != "[UIDREQUIRED] Message numbers are not allowed once UIDONLY is enabled" || len(banned.untagged) != 0 {
		t.Fatalf("uidonly %s %q", banned.tagLine, banned.untagged)
	}
	if statusField(t, cl, "Box", "UIDNEXT") != roNext {
		t.Fatal("uidonly sequence stored")
	}
	van := cl.cmd("UID REPLACE " + uid + " Box " + imapLit("uid-new"))
	van.mustOK(t)
	if len(van.untagged) < 2 || van.untagged[0] != "* VANISHED "+uid || strings.Join(van.untagged, "\n") == "" {
		t.Fatalf("vanished %q", van.untagged)
	}
	for _, line := range van.untagged {
		if strings.Contains(line, "EXPUNGE") {
			t.Fatalf("expunge under uidonly %q", van.untagged)
		}
	}
	if !strings.Contains(strings.Join(van.untagged, "\n"), " EXISTS") {
		t.Fatalf("uidonly exists %q", van.untagged)
	}
}

func TestReplaceAbsent(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-22.json"})
	cl := dial(t, bound.Listen)
	loginDemo(t, cl)
	if listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "REPLACE") {
		t.Fatal("cyrus-22 lists REPLACE")
	}
	cl.cmd("CREATE Box").mustOK(t)
	cl.cmd("APPEND Box " + imapLit("keep")).mustOK(t)
	cl.cmd("SELECT Box").mustOK(t)
	rp := cl.cmd("REPLACE 1 Box " + imapLit("no"))
	if rp.status != "BAD" || !strings.Contains(rp.text, "unsupported") || len(rp.untagged) != 0 {
		t.Fatalf("absent %s %q", rp.tagLine, rp.untagged)
	}
	uid := cl.cmd("UID REPLACE 1 Box " + imapLit("no"))
	if uid.status != "BAD" || !strings.Contains(uid.text, "unsupported") {
		t.Fatalf("absent uid %s %s", uid.status, uid.tagLine)
	}
	if statusField(t, cl, "Box", "MESSAGES") != "1" || statusField(t, cl, "Box", "UIDNEXT") != "2" {
		t.Fatal("absent stored")
	}
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); got != "keep" {
		t.Fatalf("absent body %q", got)
	}
}

func TestReplaceLimit(t *testing.T) {
	path := writePersonality(t,
		[]string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "AUTH=PLAIN", "REPLACE", "APPENDLIMIT=4"},
		[]string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "REPLACE", "APPENDLIMIT=4"},
	)
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE Lim").mustOK(t)
	cl.cmd("SELECT Lim").mustOK(t)
	cl.cmd("APPEND Lim " + imapLit("abcd")).mustOK(t)
	over := cl.cmd("REPLACE 1 Lim " + imapLit("abcde"))
	if over.status != "NO" || !strings.Contains(over.tagLine, "[TOOBIG]") || len(over.untagged) != 0 {
		t.Fatalf("toobig %s %q", over.tagLine, over.untagged)
	}
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); got != "abcd" {
		t.Fatalf("toobig stored %q", got)
	}
	if statusField(t, cl, "Lim", "MESSAGES") != "1" || statusField(t, cl, "Lim", "UIDNEXT") != "2" {
		t.Fatal("toobig counts")
	}
	exact := cl.cmd("REPLACE 1 Lim " + imapLit("wxyz")).mustOK(t)
	if _, set := appendUIDSet(t, exact.tagLine); set != "2" {
		t.Fatalf("exact uid %s", exact.tagLine)
	}
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); got != "wxyz" {
		t.Fatalf("exact body %q", got)
	}
	if statusField(t, cl, "Lim", "MESSAGES") != "1" {
		t.Fatal("exact count")
	}
}

func TestReplaceNotify(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/dovecot-245.json"})
	cl := dial(t, bound.Listen)
	actor := dial(t, bound.Listen)
	loginDemo(t, cl)
	loginDemo(t, actor)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "REPLACE") {
		t.Fatal("dovecot post-login missing REPLACE")
	}
	actor.cmd("CREATE Src").mustOK(t)
	actor.cmd("CREATE Dest").mustOK(t)
	actor.cmd("APPEND Src " + imapLit("src")).mustOK(t)
	actor.cmd("SELECT Src").mustOK(t)
	cl.cmd("NOTIFY SET (mailboxes (Dest) (MessageNew MessageExpunge))").mustOK(t)
	actor.cmd("APPEND Dest " + imapLit("appended")).mustOK(t)
	got := pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.Contains(got[0], "STATUS") || !strings.Contains(got[0], "Dest") {
		t.Fatalf("append notify %q", got)
	}
	actor.cmd("REPLACE 1 Dest " + imapLit("replaced")).mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.Contains(got[0], "STATUS") || !strings.Contains(got[0], "Dest") {
		t.Fatalf("replace notify %q", got)
	}
	actor.cmd("SELECT Dest").mustOK(t)
	if got := string(fetchLiteral(t, fetchLine(t, actor.cmd("FETCH 2 (BODY.PEEK[])").mustOK(t)))); got != "replaced" {
		t.Fatalf("notified body %q", got)
	}
}
