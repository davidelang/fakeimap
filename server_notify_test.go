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

func TestNotifyPersonalities(t *testing.T) {
	if _, ok := implementedCaps["NOTIFY"]; !ok || !capImplemented("NOTIFY") {
		t.Fatal("NOTIFY is not implemented")
	}
	want := map[string]bool{"dovecot-245.json": true, "dovecot-dev.json": true}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join("personalities", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		has := strings.Contains(string(b), "NOTIFY")
		if !want[e.Name()] {
			if has {
				t.Fatalf("%s gained NOTIFY", e.Name())
			}
			continue
		}
		if !has {
			t.Fatalf("%s missing NOTIFY", e.Name())
		}
		p, err := loadPersonality(filepath.Join("personalities", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "NOTIFY") || listed(p.PreLogin, "NOTIFY") {
			t.Fatalf("%s lists NOTIFY before login", e.Name())
		}
		c := indexCap(p.PostLogin, "COMPRESS=DEFLATE")
		n := indexCap(p.PostLogin, "NOTIFY")
		l := indexCap(p.PostLogin, "LITERAL+")
		if c < 0 || n != c+1 || l != n+1 {
			t.Fatalf("%s post list %v", e.Name(), p.PostLogin)
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "NOTIFY") {
		t.Fatal("gaps file names NOTIFY")
	}
}

func TestNotify(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/dovecot-245.json"})
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "NOTIFY") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	pre := cl.cmd("CAPABILITY").mustOK(t)
	if strings.Contains(strings.Join(capsOf(pre.untagged[0]), " "), "NOTIFY") {
		t.Fatalf("pre-login %v", capsOf(pre.untagged[0]))
	}
	if rp := cl.cmd("NOTIFY NONE"); rp.status != "NO" || !strings.Contains(rp.text, "must authenticate first") {
		t.Fatalf("before login %+v", rp)
	}

	_, cyrusBound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	cyrus := dial(t, cyrusBound.Listen)
	cyrus.cmd("LOGIN demo secret").mustOK(t)
	if rp := cyrus.cmd("NOTIFY SET (personal (MessageNew MessageExpunge))"); rp.status != "BAD" {
		t.Fatalf("cyrus NOTIFY %+v", rp)
	}

	cl.cmd("LOGIN demo secret").mustOK(t)
	post := cl.cmd("CAPABILITY").mustOK(t)
	if !listed(capsOf(post.untagged[0]), "NOTIFY") {
		t.Fatalf("post-login %v", capsOf(post.untagged[0]))
	}
	// No selected mailbox is required.
	cl.cmd("NOTIFY NONE").mustOK(t)
	cl.cmd("NOTIFY SET (inboxes (MessageNew MessageExpunge))").mustOK(t)
	if rp := cl.cmd("NOTIFY SET (personal (FlagChange))"); rp.status != "BAD" {
		t.Fatalf("broken pair %+v", rp)
	}
	if rp := cl.cmd("NOTIFY SET (personal (MessageNew))"); rp.status != "BAD" {
		t.Fatalf("half pair %+v", rp)
	}
	if rp := cl.cmd("NOTIFY SET (personal (MessageNew (FLAGS) MessageExpunge))"); rp.status != "BAD" {
		t.Fatalf("fetch on personal %+v", rp)
	}
	if rp := cl.cmd("NOTIFY SET (personal (NoSuch))"); rp.status != "BAD" {
		t.Fatalf("bad event %+v", rp)
	}
	if rp := cl.cmd("NOTIFY SET (HIGHESTMODSEQ) (personal (MessageNew MessageExpunge))"); rp.status != "BAD" {
		t.Fatalf("bad status %+v", rp)
	}
	if rp := cl.cmd("NOTIFY SET (selected (MessageNew (BOGUS) MessageExpunge))"); rp.status != "BAD" {
		t.Fatalf("bad fetch %+v", rp)
	}

	// Syntax errors left the inboxes registration in place.
	actor := dial(t, bound.Listen)
	actor.cmd("LOGIN demo secret").mustOK(t)
	actor.cmd("APPEND INBOX/Generated {5+}\r\nhello").mustOK(t)
	if got := pullUntagged(t, cl, 400*time.Millisecond); len(got) != 0 {
		t.Fatalf("inboxes saw generated %q", got)
	}
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	got := pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.HasPrefix(got[0], `* STATUS "INBOX" `) {
		t.Fatalf("inboxes status %q", got)
	}
	if !strings.Contains(got[0], "MESSAGES ") || !strings.Contains(got[0], "UIDNEXT ") || !strings.Contains(got[0], "UNSEEN ") {
		t.Fatalf("default items %q", got[0])
	}

	cl.cmd("NOTIFY SET (UIDVALIDITY) (subscribed (MessageNew MessageExpunge))").mustOK(t)
	actor.cmd("APPEND INBOX/Generated {5+}\r\nhello").mustOK(t)
	if got = pullUntagged(t, cl, 400*time.Millisecond); len(got) != 0 {
		t.Fatalf("unsubscribed %q", got)
	}
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.Contains(got[0], "UIDVALIDITY ") || strings.Contains(got[0], "MESSAGES ") {
		t.Fatalf("uidvalidity status %q", got)
	}

	actor.cmd("SUBSCRIBE INBOX/Generated").mustOK(t)
	cl.cmd("NOTIFY SET (subscribed (MessageNew MessageExpunge))").mustOK(t)
	actor.cmd("APPEND INBOX/Generated {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.HasPrefix(got[0], `* STATUS "INBOX/Generated" `) {
		t.Fatalf("subscribed %q", got)
	}

	cl.cmd("NOTIFY SET (subtree INBOX/Generated (MessageNew MessageExpunge))").mustOK(t)
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	if got = pullUntagged(t, cl, 400*time.Millisecond); len(got) != 0 {
		t.Fatalf("subtree saw INBOX %q", got)
	}
	actor.cmd("CREATE INBOX/Generated/Child").mustOK(t)
	actor.cmd("APPEND INBOX/Generated/Child {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.HasPrefix(got[0], `* STATUS "INBOX/Generated/Child" `) {
		t.Fatalf("subtree child %q", got)
	}

	cl.cmd("NOTIFY SET (mailboxes (INBOX/Generated) NONE) (personal (MessageNew MessageExpunge))").mustOK(t)
	actor.cmd("APPEND INBOX/Generated {5+}\r\nhello").mustOK(t)
	if got = pullUntagged(t, cl, 400*time.Millisecond); len(got) != 0 {
		t.Fatalf("none group %q", got)
	}
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.HasPrefix(got[0], `* STATUS "INBOX" `) {
		t.Fatalf("group after none %q", got)
	}

	cl.cmd("NOTIFY SET (mailboxes (INBOX) (MessageNew MessageExpunge)) (personal (MessageNew MessageExpunge))").mustOK(t)
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.HasPrefix(got[0], `* STATUS "INBOX" `) {
		t.Fatalf("two groups %q", got)
	}

	// Selected mailbox keeps EXISTS and adds one FETCH. No STATUS, and not twice.
	cl.cmd("SELECT INBOX").mustOK(t)
	cl.cmd("NOTIFY SET (selected (MessageNew (FLAGS) MessageExpunge))").mustOK(t)
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	nExists, nFetch, nStatus := 0, 0, 0
	for _, line := range got {
		if strings.Contains(line, " EXISTS") {
			nExists++
		}
		if strings.Contains(line, " FETCH ") {
			nFetch++
		}
		if strings.HasPrefix(line, "* STATUS ") {
			nStatus++
		}
	}
	if nExists != 1 || nFetch != 1 || nStatus != 0 {
		t.Fatalf("selected fetch lines %q", got)
	}

	cl.cmd("NOTIFY SET (selected-delayed (MessageNew MessageExpunge))").mustOK(t)
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	nExists, nFetch = 0, 0
	for _, line := range got {
		if strings.Contains(line, " EXISTS") {
			nExists++
		}
		if strings.Contains(line, " FETCH ") || strings.HasPrefix(line, "* STATUS ") {
			nFetch++
		}
	}
	if nExists != 1 || nFetch != 0 {
		t.Fatalf("selected-delayed %q", got)
	}

	// A different mailbox while this session is selected, including IDLE.
	cl.cmd("NOTIFY SET (personal (MessageNew MessageExpunge FlagChange))").mustOK(t)
	actor.cmd("APPEND INBOX/Generated {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.HasPrefix(got[0], `* STATUS "INBOX/Generated" `) {
		t.Fatalf("other while selected %q", got)
	}
	actor.cmd("SELECT INBOX/Generated").mustOK(t)
	actor.cmd("STORE 1 +FLAGS (\\Seen)").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.HasPrefix(got[0], `* STATUS "INBOX/Generated" `) {
		t.Fatalf("flag change %q", got)
	}
	cl.cmd("NOTIFY SET (personal (MessageNew MessageExpunge))").mustOK(t)
	actor.cmd("STORE 1 -FLAGS (\\Seen)").mustOK(t)
	if got = pullUntagged(t, cl, 400*time.Millisecond); len(got) != 0 {
		t.Fatalf("flag without FlagChange %q", got)
	}

	cl.cmd("UNSELECT").mustOK(t)
	cl.cmd("SELECT INBOX/Generated").mustOK(t)
	cl.cmd("NOTIFY SET (inboxes (MessageNew MessageExpunge))").mustOK(t)
	cl.n++
	idleTag := fmt.Sprintf("a%d", cl.n)
	cl.send(idleTag + " IDLE\r\n")
	for {
		rec := strings.TrimRight(cl.readRec(), "\r\n")
		if strings.HasPrefix(rec, "+") {
			break
		}
	}
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.HasPrefix(got[0], `* STATUS "INBOX" `) {
		t.Fatalf("idle %q", got)
	}
	cl.send("DONE\r\n")
	cl.readTag(idleTag).mustOK(t)

	// MailboxName and SubscriptionChange use the LIST line.
	cl.cmd("NOTIFY SET (personal (MailboxName SubscriptionChange))").mustOK(t)
	actor.cmd("CREATE INBOX/Zed").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	listed := actor.cmd(`LIST "" INBOX/Zed`).mustOK(t)
	if len(got) != 1 || len(listed.untagged) != 1 || got[0] != listed.untagged[0] {
		t.Fatalf("create list %q vs %q", got, listed.untagged)
	}
	actor.cmd("RENAME INBOX/Zed INBOX/Zed2").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	renamed := actor.cmd(`LIST "" INBOX/Zed2`).mustOK(t)
	if len(got) != 2 || len(renamed.untagged) != 1 {
		t.Fatalf("rename %q list %q", got, renamed.untagged)
	}
	if !strings.Contains(got[0], `\NonExistent`) || !strings.Contains(got[0], "INBOX/Zed") || strings.Contains(got[0], "INBOX/Zed2") {
		t.Fatalf("rename old %q", got[0])
	}
	if got[1] != renamed.untagged[0] {
		t.Fatalf("rename new %q vs %q", got[1], renamed.untagged[0])
	}
	actor.cmd("DELETE INBOX/Zed2").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	if len(got) != 1 || !strings.Contains(got[0], `\NonExistent`) || !strings.Contains(got[0], "INBOX/Zed2") {
		t.Fatalf("delete %q", got)
	}
	actor.cmd("SUBSCRIBE INBOX/Generated/Child").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	sub := actor.cmd(`LIST "" INBOX/Generated/Child`).mustOK(t)
	if len(got) != 1 || len(sub.untagged) != 1 || got[0] != sub.untagged[0] {
		t.Fatalf("subscribe %q vs %q", got, sub.untagged)
	}
	actor.cmd("UNSUBSCRIBE INBOX/Generated/Child").mustOK(t)
	got = pullUntagged(t, cl, 2*time.Second)
	unsub := actor.cmd(`LIST "" INBOX/Generated/Child`).mustOK(t)
	if len(got) != 1 || len(unsub.untagged) != 1 || got[0] != unsub.untagged[0] {
		t.Fatalf("unsubscribe %q vs %q", got, unsub.untagged)
	}

	// No registration: the next command still carries the selected line, and nothing arrives early.
	quiet := dial(t, bound.Listen)
	quiet.cmd("LOGIN demo secret").mustOK(t)
	quiet.cmd("SELECT INBOX").mustOK(t)
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	if got = pullUntagged(t, quiet, 400*time.Millisecond); len(got) != 0 {
		t.Fatalf("no registration pushed %q", got)
	}
	noop := quiet.cmd("NOOP").mustOK(t)
	if !hasPrefix(noop.untagged, "* ") || !strings.Contains(strings.Join(noop.untagged, "\n"), " EXISTS") {
		t.Fatalf("noop %q", noop.untagged)
	}
}

func TestNotifyUnauthenticateAndUIDONLY(t *testing.T) {
	p, err := loadPersonality("personalities/dovecot-245.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PostLogin = append(append([]string{}, p.PostLogin...), "UIDONLY", "UNAUTHENTICATE")
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	a := dial(t, bound.Listen)
	bcl := dial(t, bound.Listen)
	a.cmd("LOGIN demo secret").mustOK(t)
	bcl.cmd("LOGIN demo secret").mustOK(t)
	a.cmd("NOTIFY SET (personal (MessageNew MessageExpunge))").mustOK(t)
	bcl.cmd("NOTIFY SET (personal (MessageNew MessageExpunge))").mustOK(t)
	a.cmd("UNAUTHENTICATE").mustOK(t)
	a.cmd("LOGIN demo secret").mustOK(t)
	actor := dial(t, bound.Listen)
	actor.cmd("LOGIN demo secret").mustOK(t)
	actor.cmd("APPEND INBOX/Generated {5+}\r\nhello").mustOK(t)
	if got := pullUntagged(t, a, 400*time.Millisecond); len(got) != 0 {
		t.Fatalf("cleared registration %q", got)
	}
	got := pullUntagged(t, bcl, 2*time.Second)
	if len(got) != 1 || !strings.HasPrefix(got[0], `* STATUS "INBOX/Generated" `) {
		t.Fatalf("other connection %q", got)
	}

	a.cmd("SELECT INBOX").mustOK(t)
	a.cmd("ENABLE UIDONLY").mustOK(t)
	a.cmd("NOTIFY SET (selected (MessageNew (FLAGS UID) MessageExpunge))").mustOK(t)
	actor.cmd("APPEND INBOX {5+}\r\nhello").mustOK(t)
	got = pullUntagged(t, a, 2*time.Second)
	nUID, nFetch := 0, 0
	for _, line := range got {
		if strings.Contains(line, " UIDFETCH ") {
			nUID++
		}
		if strings.Contains(line, " FETCH ") {
			nFetch++
		}
	}
	if nUID != 1 || nFetch != 0 {
		t.Fatalf("uidfetch %q", got)
	}
	actor.cmd("SELECT INBOX").mustOK(t)
	actor.cmd("STORE 1 +FLAGS (\\Deleted)").mustOK(t)
	actor.cmd("EXPUNGE").mustOK(t)
	got = pullUntagged(t, a, 2*time.Second)
	vanished, expunge := false, false
	for _, line := range got {
		if strings.HasPrefix(line, "* VANISHED ") {
			vanished = true
		}
		if strings.Contains(line, " EXPUNGE") {
			expunge = true
		}
	}
	if !vanished || expunge {
		t.Fatalf("expunge lines %q", got)
	}
}

func pullUntagged(t *testing.T, cl *imapClient, wait time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(wait)
	var out []string
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return out
		}
		slice := remain
		if len(out) > 0 && slice > 200*time.Millisecond {
			slice = 200 * time.Millisecond
		}
		if err := cl.c.SetReadDeadline(time.Now().Add(slice)); err != nil {
			t.Fatal(err)
		}
		line, err := cl.r.ReadString('\n')
		if err != nil {
			return out
		}
		out = append(out, strings.TrimRight(line, "\r\n"))
		if len(out) > 20 {
			return out
		}
	}
}
