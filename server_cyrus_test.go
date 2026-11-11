package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCyrusPersonalities(t *testing.T) {
	banned := []string{"ID", "QUOTA", "ACL", "WITHIN", "URLAUTH", "STATUS=SIZE", "SEARCHRES", "APPENDLIMIT", "IMAP4", "IMAP4rev2", "STARTTLS"}
	flagsFirst := []string{"FLAGS", "PERMANENTFLAGS", "EXISTS", "RECENT", "UNSEEN", "UIDVALIDITY", "UIDNEXT"}
	cyrusOrder := []string{"EXISTS", "RECENT", "FLAGS", "PERMANENTFLAGS", "UNSEEN", "UIDVALIDITY", "UIDNEXT", "HIGHESTMODSEQ"}
	cases := []struct {
		file     string
		greeting string
		greet    string
		pre      string
		post     string
		ns       string
		order    []string
		off      []string
	}{
		{
			file:     "personalities/cyrus-22.json",
			greeting: "* OK Cyrus IMAP4 v2.2.13-Debian-2.2.13-14+lenny6 server ready",
			pre:      "IMAP4rev1 LITERAL+ NAMESPACE UIDPLUS UNSELECT CHILDREN BINARY SORT THREAD=ORDEREDSUBJECT THREAD=REFERENCES IDLE",
			post:     "IMAP4rev1 LITERAL+ NAMESPACE UIDPLUS UNSELECT CHILDREN BINARY SORT THREAD=ORDEREDSUBJECT THREAD=REFERENCES IDLE",
			ns:       `* NAMESPACE (("INBOX." ".")) (("user." ".")) (("" "."))`,
			order:    flagsFirst,
		},
		{
			file:     "personalities/cyrus-24.json",
			greeting: "* OK [CAPABILITY IMAP4rev1 LITERAL+ ENABLE SASL-IR] Cyrus IMAP v2.4.16-Debian-2.4.16-4+deb7u2 server ready",
			greet:    "IMAP4rev1 LITERAL+ ENABLE SASL-IR",
			pre:      "BINARY CHILDREN COMPRESS=DEFLATE CONDSTORE ENABLE ESEARCH IDLE IMAP4rev1 LIST-EXTENDED LITERAL+ NAMESPACE QRESYNC SASL-IR SORT SORT=DISPLAY THREAD=ORDEREDSUBJECT THREAD=REFERENCES UIDPLUS UNSELECT",
			post:     "BINARY CHILDREN COMPRESS=DEFLATE CONDSTORE ENABLE ESEARCH IDLE IMAP4rev1 LIST-EXTENDED LITERAL+ NAMESPACE QRESYNC SORT SORT=DISPLAY THREAD=ORDEREDSUBJECT THREAD=REFERENCES UIDPLUS UNSELECT",
			ns:       `* NAMESPACE (("INBOX." ".")) (("user." ".")) (("" "."))`,
			order:    cyrusOrder,
			off:      []string{"MOVE", "SPECIAL-USE"},
		},
		{
			file:     "personalities/cyrus-25.json",
			greeting: "* OK [CAPABILITY IMAP4rev1 LITERAL+ ENABLE SASL-IR] Cyrus IMAP 2.5.10-Debian-2.5.10-3+deb9u3 server ready",
			greet:    "IMAP4rev1 LITERAL+ ENABLE SASL-IR",
			pre:      "BINARY CHILDREN COMPRESS=DEFLATE CONDSTORE CREATE-SPECIAL-USE ENABLE ESEARCH IDLE IMAP4rev1 LIST-EXTENDED LIST-STATUS LITERAL+ MOVE NAMESPACE QRESYNC SASL-IR SORT SORT=DISPLAY SPECIAL-USE THREAD=ORDEREDSUBJECT THREAD=REFERENCES UIDPLUS UNSELECT",
			post:     "BINARY CHILDREN COMPRESS=DEFLATE CONDSTORE CREATE-SPECIAL-USE ENABLE ESEARCH IDLE IMAP4rev1 LIST-EXTENDED LIST-STATUS LITERAL+ MOVE NAMESPACE QRESYNC SORT SORT=DISPLAY SPECIAL-USE THREAD=ORDEREDSUBJECT THREAD=REFERENCES UIDPLUS UNSELECT",
			ns:       `* NAMESPACE (("INBOX/" "/")) (("user/" "/")) (("" "/"))`,
			order:    cyrusOrder,
			off:      []string{"ESORT", "PREVIEW", "MULTISEARCH"},
		},
		{
			file:     "personalities/cyrus-36.json",
			greeting: "* OK [CAPABILITY IMAP4rev1 LITERAL+ ENABLE SASL-IR] Cyrus IMAP 3.6.1-Debian-3.6.1-4+deb12u5 server ready",
			greet:    "IMAP4rev1 LITERAL+ ENABLE SASL-IR",
			pre:      "BINARY CHILDREN COMPRESS=DEFLATE CONDSTORE CREATE-SPECIAL-USE ENABLE ESEARCH ESORT IDLE IMAP4rev1 LIST-EXTENDED LIST-STATUS LITERAL+ MOVE NAMESPACE PREVIEW QRESYNC SASL-IR SEARCH=FUZZY SORT SORT=DISPLAY SPECIAL-USE THREAD=ORDEREDSUBJECT THREAD=REFERENCES THREAD=REFS UIDPLUS UNSELECT",
			post:     "BINARY CHILDREN COMPRESS=DEFLATE CONDSTORE CREATE-SPECIAL-USE ENABLE ESEARCH ESORT IDLE IMAP4rev1 LIST-EXTENDED LIST-STATUS LITERAL+ MOVE NAMESPACE PREVIEW QRESYNC SEARCH=FUZZY SORT SORT=DISPLAY SPECIAL-USE THREAD=ORDEREDSUBJECT THREAD=REFERENCES THREAD=REFS UIDPLUS UNSELECT",
			ns:       `* NAMESPACE (("INBOX/" "/")) (("user/" "/")) (("" "/"))`,
			order:    cyrusOrder,
			off:      []string{"MULTISEARCH", "PARTIAL"},
		},
		{
			file:     "personalities/cyrus-3124.json",
			greeting: "* OK [CAPABILITY IMAP4rev1 ENABLE LITERAL+ SASL-IR] Cyrus IMAP 3.12.4 server ready",
			greet:    "IMAP4rev1 ENABLE LITERAL+ SASL-IR",
			pre:      "BINARY CHILDREN COMPRESS=DEFLATE CONDSTORE CREATE-SPECIAL-USE ENABLE ESEARCH ESORT IDLE IMAP4rev1 LIST-EXTENDED LIST-STATUS LITERAL+ MOVE MULTISEARCH NAMESPACE PARTIAL PREVIEW QRESYNC SASL-IR SEARCH=FUZZY SORT SORT=DISPLAY SPECIAL-USE THREAD=ORDEREDSUBJECT THREAD=REFERENCES THREAD=REFS UIDPLUS UNSELECT",
			post:     "BINARY CHILDREN COMPRESS=DEFLATE CONDSTORE CREATE-SPECIAL-USE ENABLE ESEARCH ESORT IDLE IMAP4rev1 LIST-EXTENDED LIST-STATUS LITERAL+ MOVE MULTISEARCH NAMESPACE PARTIAL PREVIEW QRESYNC SEARCH=FUZZY SORT SORT=DISPLAY SPECIAL-USE THREAD=ORDEREDSUBJECT THREAD=REFERENCES THREAD=REFS UIDPLUS UNSELECT",
			ns:       `* NAMESPACE (("" "/")) (("Other Users/" "/")) (("Shared Folders/" "/"))`,
			order:    cyrusOrder,
			off:      []string{"IMAP4rev2"},
		},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			p, err := loadPersonality(c.file)
			if err != nil {
				t.Fatal(err)
			}
			if c.greet == "" {
				if p.GreetingCaps == nil || len(p.GreetingCaps) != 0 {
					t.Fatalf("greeting list %#v", p.GreetingCaps)
				}
			} else if strings.Join(p.GreetingCaps, " ") != c.greet {
				t.Fatalf("greeting caps %v", p.GreetingCaps)
			}
			if strings.Join(p.PreLogin, " ") != c.pre || strings.Join(p.PostLogin, " ") != c.post {
				t.Fatalf("file caps pre %v post %v", p.PreLogin, p.PostLogin)
			}
			for _, name := range append(append([]string{}, banned...), c.off...) {
				if listed(p.GreetingCaps, name) || listed(p.PreLogin, name) || listed(p.PostLogin, name) {
					t.Fatalf("file advertises %s", name)
				}
			}
			_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: c.file})
			cl := dial(t, bound.Listen)
			gotGreet := strings.TrimRight(cl.greeting, "\r\n")
			if gotGreet != c.greeting {
				t.Fatalf("greeting %q", gotGreet)
			}
			if c.greet == "" && strings.Contains(gotGreet, "[CAPABILITY") {
				t.Fatalf("greeting has a capability code: %s", gotGreet)
			}
			pre := cl.cmd("CAPABILITY").mustOK(t)
			if strings.Join(capsOf(pre.untagged[0]), " ") != c.pre {
				t.Fatalf("pre-login %v", capsOf(pre.untagged[0]))
			}
			login := cl.cmd("LOGIN demo secret").mustOK(t)
			if strings.Join(capsOf(login.tagLine), " ") != c.post {
				t.Fatalf("post-login %v", capsOf(login.tagLine))
			}
			if listed(capsOf(login.tagLine), "SASL-IR") {
				t.Fatal("SASL-IR after login")
			}
			ns := cl.cmd("NAMESPACE").mustOK(t)
			if len(ns.untagged) != 1 || ns.untagged[0] != c.ns {
				t.Fatalf("namespace %q", ns.untagged)
			}
			sel := cl.cmd("SELECT INBOX").mustOK(t)
			if got := selectKinds(sel.untagged); strings.Join(got, " ") != strings.Join(c.order, " ") {
				t.Fatalf("select %v", got)
			}
		})
	}
}

func TestAbsentGreetingList(t *testing.T) {
	p, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	p.GreetingCaps = nil
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	again, err := loadPersonality(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.GreetingCaps != nil {
		t.Fatalf("omitted greeting list %#v", again.GreetingCaps)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	got := strings.TrimRight(cl.greeting, "\r\n")
	if !strings.Contains(got, "[CAPABILITY ") || !listed(capsOf(got), "IDLE") || !strings.HasSuffix(got, p.Greeting) {
		t.Fatalf("absent greeting list %q", got)
	}
}

func selectKinds(lines []string) []string {
	var kinds []string
	for _, u := range lines {
		switch {
		case strings.HasPrefix(u, "* FLAGS "):
			kinds = append(kinds, "FLAGS")
		case strings.Contains(u, "[PERMANENTFLAGS "):
			kinds = append(kinds, "PERMANENTFLAGS")
		case strings.HasPrefix(u, "* ") && strings.HasSuffix(u, " EXISTS"):
			kinds = append(kinds, "EXISTS")
		case strings.HasPrefix(u, "* ") && strings.HasSuffix(u, " RECENT"):
			kinds = append(kinds, "RECENT")
		case strings.Contains(u, "[UNSEEN "):
			kinds = append(kinds, "UNSEEN")
		case strings.Contains(u, "[UIDVALIDITY "):
			kinds = append(kinds, "UIDVALIDITY")
		case strings.Contains(u, "[UIDNEXT "):
			kinds = append(kinds, "UIDNEXT")
		case strings.Contains(u, "[HIGHESTMODSEQ "):
			kinds = append(kinds, "HIGHESTMODSEQ")
		}
	}
	return kinds
}
