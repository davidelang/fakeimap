package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIMAP4rev2Advertised(t *testing.T) {
	if !capImplemented("IMAP4rev2") {
		t.Fatal("IMAP4rev2 is not implemented")
	}
	const stalSentence = "Stalwart IMAP4rev2 at your service."
	stalwarts := map[string]bool{
		"stalwart.json":       true,
		"stalwart-0155.json":  true,
		"stalwart-01625.json": true,
		"stalwart-dev.json":   true,
		"stalwart-oauth.json": true,
	}
	if len(stalwarts) != 5 {
		t.Fatal("stalwart filename map")
	}
	cyrusRev2 := map[string]bool{"cyrus-3124.json": true, "cyrus-3137.json": true}
	dovecotRev2 := map[string]bool{"dovecot-245.json": true}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	var nPers int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), "gaps") {
			continue
		}
		nPers++
		path := filepath.Join("personalities", e.Name())
		p, err := loadPersonality(path)
		if err != nil {
			t.Fatal(err)
		}
		g := listed(p.GreetingCaps, "IMAP4rev2")
		pre := listed(p.PreLogin, "IMAP4rev2")
		post := listed(p.PostLogin, "IMAP4REV2")
		switch {
		case stalwarts[e.Name()]:
			if p.Greeting != stalSentence || !g || !pre || !post {
				t.Fatalf("%s IMAP4rev2 lists", path)
			}
			if p.GreetingCaps[0] != "IMAP4rev2" || p.PreLogin[0] != "IMAP4rev2" || p.PostLogin[0] != "IMAP4REV2" {
				t.Fatalf("%s IMAP4rev2 is not first", path)
			}
			if e.Name() == "stalwart-oauth.json" {
				if !listed(p.GreetingCaps, "AUTH=OAUTHBEARER") || !listed(p.GreetingCaps, "AUTH=XOAUTH2") || listed(p.PostLogin, "AUTH=OAUTHBEARER") {
					t.Fatalf("%s AUTH placement", path)
				}
			} else if listed(p.GreetingCaps, "AUTH=OAUTHBEARER") || listed(p.PreLogin, "AUTH=XOAUTH2") {
				t.Fatalf("%s gained a bearer AUTH token", path)
			}
		case cyrusRev2[e.Name()]:
			if !g || !pre || !post || capNext(p.GreetingCaps, "IMAP4rev1") != "IMAP4rev2" || capNext(p.PostLogin, "IMAP4rev1") != "IMAP4REV2" {
				t.Fatalf("%s IMAP4rev2 placement", path)
			}
		case dovecotRev2[e.Name()]:
			if !g || !pre || !post || capNext(p.GreetingCaps, "IMAP4rev1") != "IMAP4rev2" || capNext(p.PostLogin, "IMAP4rev1") != "IMAP4REV2" {
				t.Fatalf("%s IMAP4rev2 placement", path)
			}
		default:
			if g || pre || post || listed(p.PostLogin, "IMAP4rev2") || listed(p.GreetingCaps, "IMAP4REV2") {
				t.Fatalf("%s lists IMAP4rev2", path)
			}
		}
	}
	if nPers < 5 {
		t.Fatal("expected personality files")
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "IMAP4rev2") {
		t.Fatal("gaps still lists IMAP4rev2")
	}
	base, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	src := []string{"IMAP4rev1", "IMAP4rev2", "I18NLEVEL=1", "URL-PARTIAL", "AUTH=PLAIN"}
	got := base.advertiseList(src, false, false, false, false, nil)
	if strings.Join(got, " ") != "IMAP4rev1 IMAP4rev2 AUTH=PLAIN" {
		t.Fatalf("advertiseList %v", got)
	}

	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	plain := dial(t, bound.Listen)
	plain.cmd("LOGIN demo secret").mustOK(t)
	sel := plain.cmd("SELECT INBOX").mustOK(t)
	if !hasWordLine(sel.untagged, "RECENT") {
		t.Fatalf("SELECT without ENABLE %q", sel.untagged)
	}
	search := plain.cmd("SEARCH ALL").mustOK(t)
	if len(search.untagged) != 1 || !strings.HasPrefix(search.untagged[0], "* SEARCH") {
		t.Fatalf("SEARCH without ENABLE %q", search.untagged)
	}

	cl := dial(t, bound.Listen)
	if !listed(capsOf(cl.greeting), "IMAP4rev2") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	only := cl.cmd("ENABLE IMAP4rev2").mustOK(t)
	if len(only.untagged) != 1 || only.untagged[0] != "* ENABLED IMAP4REV2" {
		t.Fatalf("ENABLE %q", only.untagged)
	}
	mixed := cl.cmd("ENABLE IMAP4rev2 CONDSTORE").mustOK(t)
	if len(mixed.untagged) != 1 || mixed.untagged[0] != "* ENABLED CONDSTORE" {
		t.Fatalf("mixed ENABLE %q", mixed.untagged)
	}
	rev := cl.cmd("SELECT INBOX").mustOK(t)
	if hasWordLine(rev.untagged, "RECENT") || hasWordLine(rev.untagged, "UNSEEN") {
		t.Fatalf("SELECT after ENABLE %q", rev.untagged)
	}
	foundList := false
	for _, ln := range rev.untagged {
		if strings.HasPrefix(ln, "* LIST ") && strings.Contains(ln, "INBOX") {
			foundList = true
		}
		if strings.Contains(ln, `\Recent`) {
			t.Fatalf("Recent flag %s", ln)
		}
	}
	if !foundList {
		t.Fatalf("SELECT missing LIST %q", rev.untagged)
	}
	es := cl.cmd("SEARCH ALL").mustOK(t)
	if len(es.untagged) != 1 || strings.HasPrefix(es.untagged[0], "* SEARCH") || !strings.Contains(es.untagged[0], "ESEARCH") {
		t.Fatalf("SEARCH after ENABLE %q", es.untagged)
	}
	if rp := cl.cmd("SEARCH NEW"); rp.status != "BAD" || !strings.Contains(rp.text, "bad key") {
		t.Fatalf("SEARCH NEW %s %q", rp.status, rp.text)
	}
	if rp := cl.cmd("CHECK"); rp.status != "BAD" || !strings.Contains(rp.text, "unknown command") {
		t.Fatalf("CHECK %s %q", rp.status, rp.text)
	}
	if before := plain.cmd("STATUS INBOX (DELETED)"); before.status != "BAD" || !strings.Contains(before.text, "bad item") {
		t.Fatalf("STATUS DELETED before ENABLE %s %q", before.status, before.text)
	}
	cl.cmd(`STORE 1:* +FLAGS (\Deleted)`).mustOK(t)
	deleted := cl.cmd("STATUS INBOX (DELETED)").mustOK(t)
	if len(deleted.untagged) != 1 || !strings.Contains(deleted.untagged[0], "DELETED 2") {
		t.Fatalf("STATUS DELETED %q", deleted.untagged)
	}
	if rp := cl.cmd("STATUS INBOX (RECENT)"); rp.status != "BAD" || !strings.Contains(rp.text, "bad item") {
		t.Fatalf("STATUS RECENT after ENABLE %s %q", rp.status, rp.text)
	}
	lst := cl.cmd(`LIST "" "INBOX" RETURN (STATUS (DELETED))`).mustOK(t)
	sawDeleted := false
	for _, u := range lst.untagged {
		if strings.Contains(u, "DELETED 2") {
			sawDeleted = true
		}
	}
	if !sawDeleted {
		t.Fatalf("LIST STATUS DELETED %q", lst.untagged)
	}

	facts, err := os.ReadFile("docs/facts/imap-server.md")
	if err != nil {
		t.Fatal(err)
	}
	factText := string(facts)
	for _, phrase := range []string{
		"ENABLE IMAP4rev2",
		"* ENABLED IMAP4REV2",
		"stalwart-oauth.json",
		stalSentence,
		"I18NLEVEL=1",
		"advertiseList",
	} {
		if !strings.Contains(factText, phrase) {
			t.Fatalf("facts missing %q", phrase)
		}
	}
	guide := string(mustRead(t, "docs/guide.md"))
	off := strings.Index(guide, "## Capabilities that stay off")
	if off < 0 {
		t.Fatal("guide missing stay-off section")
	}
	if !strings.Contains(guide[:off], "IMAP4rev2") || !strings.Contains(guide[:off], stalSentence) || !strings.Contains(guide[:off], "docs/facts/imap-server.md") {
		t.Fatal("guide missing the IMAP4rev2 note")
	}
	stay := guide[off:]
	if strings.Contains(stay, "never advertised") || strings.Contains(stay, "drops that token by name") || strings.Contains(stay, "even if the token were added") {
		t.Fatal("stay-off still describes the IMAP4rev2 filter")
	}
}

func hasWordLine(lines []string, word string) bool {
	for _, ln := range lines {
		fields := strings.Fields(ln)
		for _, f := range fields {
			if f == word {
				return true
			}
		}
	}
	return false
}
