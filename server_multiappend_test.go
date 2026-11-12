package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFormatUIDSetRanges(t *testing.T) {
	if got := formatUIDSet([]uint32{7}); got != "7" {
		t.Fatalf("one %s", got)
	}
	if got := formatUIDSet([]uint32{2, 3, 4}); got != "2:4" {
		t.Fatalf("run %s", got)
	}
	if got := formatUIDSet([]uint32{1, 2, 4, 5, 7}); got != "1:2,4:5,7" {
		t.Fatalf("mixed %s", got)
	}
}

func TestMultiappend(t *testing.T) {
	if !capImplemented("MULTIAPPEND") {
		t.Fatal("MULTIAPPEND is not implemented")
	}
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	cc.call(map[string]any{"op": "freeze-clock", "time": "2020-01-02T03:04:05Z"}).ok(t)
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "MULTIAPPEND") {
		t.Fatalf("greeting lists MULTIAPPEND: %q", cl.greeting)
	}
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "MULTIAPPEND") {
		t.Fatal("pre-login missing MULTIAPPEND")
	}
	if rp := cl.cmd("APPEND INBOX " + imapLit("nope")); rp.status != "NO" {
		t.Fatalf("pre-login one %s %s", rp.status, rp.tagLine)
	}
	if rp := cl.cmd("APPEND INBOX " + imapLit("one") + " " + imapLit("two")); rp.status != "NO" {
		t.Fatalf("pre-login two %s %s", rp.status, rp.tagLine)
	}
	cl.cmd("LOGIN demo secret").ok(t)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "MULTIAPPEND") {
		t.Fatal("post-login missing MULTIAPPEND")
	}
	if got := statusField(t, cl, "INBOX", "MESSAGES"); got != "2" {
		t.Fatalf("pre-login append stored, messages %s", got)
	}
	if got := statusField(t, cl, "INBOX", "UIDNEXT"); got != "3" {
		t.Fatalf("pre-login append stored, uidnext %s", got)
	}

	cl.cmd("CREATE Box").ok(t)
	cl.cmd("SELECT Box").ok(t)
	one := cl.cmd(`APPEND Box (\Seen) "01-Jan-1990 00:00:00 +0000" ` + imapLit("solo"))
	one.ok(t)
	if got := strings.Join(one.untagged, "\n"); got != "* 1 EXISTS\n* 1 RECENT" {
		t.Fatalf("one untagged %q", one.untagged)
	}
	_, set := appendUIDSet(t, one.tagLine)
	if strings.ContainsAny(set, ":,") {
		t.Fatalf("one uid set %s", set)
	}
	if body := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); body != "solo" {
		t.Fatalf("one body %q", body)
	}
	if got := fetchQuoted(t, fetchLine(t, cl.cmd("FETCH 1 (INTERNALDATE SAVEDATE)").mustOK(t)), "INTERNALDATE"); got != "01-Jan-1990 00:00:00 +0000" {
		t.Fatalf("one internal %q", got)
	}
	if got := fetchQuoted(t, fetchLine(t, cl.cmd("FETCH 1 (SAVEDATE)").mustOK(t)), "SAVEDATE"); got != "02-Jan-2020 03:04:05 +0000" {
		t.Fatalf("one save %q", got)
	}
	if flags := fetchItem(t, cl, 1, "FLAGS"); !strings.Contains(flags, `\Seen`) || strings.Contains(flags, `\Flagged`) {
		t.Fatalf("one flags %s", flags)
	}

	three := cl.cmd(`APPEND Box (\Flagged) "02-Feb-1991 03:04:05 -0700" ` + imapLit("one") + " " + imapLit("two") + ` (\Seen) ` + imapLit("three"))
	three.ok(t)
	wantUntagged := "* 2 EXISTS\n* 2 RECENT\n* 3 EXISTS\n* 3 RECENT\n* 4 EXISTS\n* 4 RECENT"
	if got := strings.Join(three.untagged, "\n"); got != wantUntagged {
		t.Fatalf("three untagged %q", three.untagged)
	}
	_, set = appendUIDSet(t, three.tagLine)
	if set != "2:4" {
		t.Fatalf("three uid set %s", set)
	}
	for i, body := range []string{"one", "two", "three"} {
		got := string(fetchLiteral(t, fetchLine(t, cl.cmd(fmt.Sprintf("FETCH %d (BODY.PEEK[])", i+2)).mustOK(t))))
		if got != body {
			t.Fatalf("seq %d body %q", i+2, got)
		}
	}
	if flags := fetchItem(t, cl, 2, "FLAGS"); !strings.Contains(flags, `\Flagged`) || strings.Contains(flags, `\Seen`) {
		t.Fatalf("second flags %s", flags)
	}
	if flags := fetchItem(t, cl, 3, "FLAGS"); strings.Contains(flags, `\Flagged`) || strings.Contains(flags, `\Seen`) {
		t.Fatalf("third flags %s", flags)
	}
	if flags := fetchItem(t, cl, 4, "FLAGS"); !strings.Contains(flags, `\Seen`) || strings.Contains(flags, `\Flagged`) {
		t.Fatalf("fourth flags %s", flags)
	}
	if got := fetchQuoted(t, fetchLine(t, cl.cmd("FETCH 2 (INTERNALDATE)").mustOK(t)), "INTERNALDATE"); got != "02-Feb-1991 03:04:05 -0700" {
		t.Fatalf("second internal %q", got)
	}
	if got := fetchQuoted(t, fetchLine(t, cl.cmd("FETCH 3 (INTERNALDATE)").mustOK(t)), "INTERNALDATE"); got != "02-Jan-2020 03:04:05 +0000" {
		t.Fatalf("third internal %q", got)
	}
	if got := fetchQuoted(t, fetchLine(t, cl.cmd("FETCH 4 (INTERNALDATE)").mustOK(t)), "INTERNALDATE"); got != "02-Jan-2020 03:04:05 +0000" {
		t.Fatalf("fourth internal %q", got)
	}
	for _, seq := range []int{2, 3, 4} {
		if got := fetchQuoted(t, fetchLine(t, cl.cmd(fmt.Sprintf("FETCH %d (SAVEDATE)", seq)).mustOK(t)), "SAVEDATE"); got != "02-Jan-2020 03:04:05 +0000" {
			t.Fatalf("seq %d save %q", seq, got)
		}
	}

	// Destination does not have to be selected. No EXISTS for that mailbox.
	away := cl.cmd("APPEND Other " + imapLit("aa") + " " + imapLit("bbb"))
	if away.status != "NO" || !strings.Contains(away.tagLine, "[TRYCREATE]") {
		t.Fatalf("missing %s %s", away.status, away.tagLine)
	}
	if len(away.untagged) != 0 {
		t.Fatalf("missing stored %q", away.untagged)
	}
	cl.cmd("CREATE Other").ok(t)
	away = cl.cmd("APPEND Other " + imapLit("aa") + " " + imapLit("bbb"))
	away.ok(t)
	if len(away.untagged) != 0 {
		t.Fatalf("unselected untagged %q", away.untagged)
	}
	_, set = appendUIDSet(t, away.tagLine)
	if set != "1:2" {
		t.Fatalf("unselected set %s", set)
	}
	cl.cmd("SELECT Other").ok(t)
	if got := statusField(t, cl, "Other", "MESSAGES"); got != "2" {
		t.Fatalf("other messages %s", got)
	}
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[])").mustOK(t)))); got != "aa" {
		t.Fatalf("other first %q", got)
	}
	if got := string(fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 2 (BODY.PEEK[])").mustOK(t)))); got != "bbb" {
		t.Fatalf("other second %q", got)
	}

	before := statusField(t, cl, "Other", "MESSAGES")
	badFlag := cl.cmd("APPEND Other " + imapLit("ok") + ` (\NoSuch) ` + imapLit("nn"))
	if badFlag.status != "BAD" || len(badFlag.untagged) != 0 {
		t.Fatalf("bad flag %s %q", badFlag.tagLine, badFlag.untagged)
	}
	if got := statusField(t, cl, "Other", "MESSAGES"); got != before {
		t.Fatalf("bad flag stored %s", got)
	}
	miss := cl.cmd("APPEND Missing " + imapLit("a") + " " + imapLit("b"))
	if miss.status != "NO" || !strings.Contains(miss.tagLine, "[TRYCREATE]") {
		t.Fatalf("trycreate %s %s", miss.status, miss.tagLine)
	}
	if st := cl.cmd("STATUS Missing (MESSAGES)"); st.status != "NO" {
		t.Fatalf("missing mailbox created %s", st.tagLine)
	}
	cat := cl.cmd("APPEND Other CATENATE (TEXT " + imapLit("body") + ")")
	if cat.status != "BAD" || len(cat.untagged) != 0 {
		t.Fatalf("catenate %s %q", cat.tagLine, cat.untagged)
	}
	cat = cl.cmd(`APPEND Other (\Seen) CATENATE (TEXT ` + imapLit("body") + ")")
	if cat.status != "BAD" {
		t.Fatalf("catenate flags %s", cat.tagLine)
	}
	if got := statusField(t, cl, "Other", "MESSAGES"); got != before {
		t.Fatalf("catenate stored %s", got)
	}

	cc.call(map[string]any{"op": "thaw-clock"}).ok(t)
	wall := cl.cmd("APPEND Other " + imapLit("wall") + " " + imapLit("clock"))
	wall.ok(t)
	if len(wall.untagged) != 4 {
		t.Fatalf("wall untagged %q", wall.untagged)
	}
	now := time.Now()
	for _, seq := range []int{3, 4} {
		line := fetchLine(t, cl.cmd(fmt.Sprintf("FETCH %d (INTERNALDATE SAVEDATE)", seq)).mustOK(t))
		internal := fetchQuoted(t, line, "INTERNALDATE")
		saved := fetchQuoted(t, line, "SAVEDATE")
		if internal != saved {
			t.Fatalf("seq %d wall internal %q save %q", seq, internal, saved)
		}
		when := mustIMAPTime(t, internal)
		if when.Before(now.Add(-2*time.Minute)) || when.After(now.Add(2*time.Minute)) {
			t.Fatalf("seq %d wall time %s", seq, internal)
		}
		if internal == "02-Jan-2020 03:04:05 +0000" {
			t.Fatalf("seq %d still frozen", seq)
		}
	}

	uid := cl.cmd("UID APPEND Other " + imapLit("z"))
	if uid.status != "BAD" {
		t.Fatalf("uid append %s %s", uid.status, uid.tagLine)
	}
}

func TestMultiappendWithoutToken(t *testing.T) {
	path := writePersonality(t,
		[]string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "AUTH=PLAIN"},
		[]string{"IMAP4rev1", "LITERAL+", "UIDPLUS"},
	)
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "MULTIAPPEND") {
		t.Fatal("token listed")
	}
	cl.cmd("CREATE Box").ok(t)
	cl.cmd("SELECT Box").ok(t)
	one := cl.cmd("APPEND Box " + imapLit("solo")).mustOK(t)
	if got := strings.Join(one.untagged, "\n"); got != "* 1 EXISTS\n* 1 RECENT" {
		t.Fatalf("one untagged %q", one.untagged)
	}
	_, set := appendUIDSet(t, one.tagLine)
	if strings.ContainsAny(set, ":,") {
		t.Fatalf("one uid set %s", set)
	}
	two := cl.cmd("APPEND Box " + imapLit("aa") + " " + imapLit("bb"))
	if two.status != "BAD" || len(two.untagged) != 0 {
		t.Fatalf("second %s %q", two.tagLine, two.untagged)
	}
	if got := statusField(t, cl, "Box", "MESSAGES"); got != "1" {
		t.Fatalf("second stored %s", got)
	}
	if got := statusField(t, cl, "Box", "UIDNEXT"); got != "2" {
		t.Fatalf("uidnext %s", got)
	}
}

func TestMultiappendLimit(t *testing.T) {
	path := writePersonality(t,
		[]string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "AUTH=PLAIN", "MULTIAPPEND", "APPENDLIMIT=4"},
		[]string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "MULTIAPPEND", "APPENDLIMIT=4"},
	)
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE Lim").ok(t)
	cl.cmd("SELECT Lim").ok(t)
	exact := cl.cmd("APPEND Lim " + imapLit("abcd")).mustOK(t)
	_, set := appendUIDSet(t, exact.tagLine)
	if strings.ContainsAny(set, ":,") {
		t.Fatalf("exact set %s", set)
	}
	if got := strings.Join(exact.untagged, "\n"); got != "* 1 EXISTS\n* 1 RECENT" {
		t.Fatalf("exact untagged %q", exact.untagged)
	}
	both := cl.cmd("APPEND Lim " + imapLit("abcd") + " " + imapLit("wxyz")).mustOK(t)
	_, set = appendUIDSet(t, both.tagLine)
	if set != "2:3" {
		t.Fatalf("both set %s", set)
	}
	if got := strings.Join(both.untagged, "\n"); got != "* 2 EXISTS\n* 2 RECENT\n* 3 EXISTS\n* 3 RECENT" {
		t.Fatalf("both untagged %q", both.untagged)
	}
	over := cl.cmd("APPEND Lim " + imapLit("abcde"))
	if over.status != "NO" || !strings.Contains(over.tagLine, "[TOOBIG]") || len(over.untagged) != 0 {
		t.Fatalf("over %s %q", over.tagLine, over.untagged)
	}
	second := cl.cmd("APPEND Lim " + imapLit("abcd") + " " + imapLit("abcde"))
	if second.status != "NO" || !strings.Contains(second.tagLine, "[TOOBIG]") || len(second.untagged) != 0 {
		t.Fatalf("second over %s %q", second.tagLine, second.untagged)
	}
	first := cl.cmd("APPEND Lim " + imapLit("abcde") + " " + imapLit("abcd"))
	if first.status != "NO" || !strings.Contains(first.tagLine, "[TOOBIG]") || len(first.untagged) != 0 {
		t.Fatalf("first over %s %q", first.tagLine, first.untagged)
	}
	if got := statusField(t, cl, "Lim", "MESSAGES"); got != "3" {
		t.Fatalf("limit stored %s", got)
	}
	if got := statusField(t, cl, "Lim", "UIDNEXT"); got != "4" {
		t.Fatalf("limit uidnext %s", got)
	}
}

func imapLit(s string) string {
	return fmt.Sprintf("{%d+}\r\n%s", len(s), s)
}

func appendUIDSet(t *testing.T, line string) (string, string) {
	t.Helper()
	const mark = "[APPENDUID "
	i := strings.Index(line, mark)
	if i < 0 {
		t.Fatalf("no APPENDUID in %s", line)
	}
	rest := line[i+len(mark):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		t.Fatalf("unclosed APPENDUID in %s", line)
	}
	fs := strings.Fields(rest[:j])
	if len(fs) != 2 {
		t.Fatalf("APPENDUID %q", rest[:j])
	}
	return fs[0], fs[1]
}

func fetchItem(t *testing.T, cl *imapClient, seq int, item string) string {
	t.Helper()
	line := fetchLine(t, cl.cmd(fmt.Sprintf("FETCH %d (%s)", seq, item)).mustOK(t))
	for _, it := range parseFetchItems(line) {
		if it[0] == item {
			return it[1]
		}
	}
	t.Fatalf("no %s in %s", item, line)
	return ""
}
