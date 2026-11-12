package main

import (
	"strings"
	"testing"
)

func TestOtherServerPersonalities(t *testing.T) {
	banned := []string{"IMAP4rev2", "LOGIN-REFERRALS", "JMAPACCESS", "QUOTA"}
	dovFlags := `\Answered \Flagged \Deleted \Seen \Draft`
	dovPerm := dovFlags + ` \*`
	dovOrder := []string{"FLAGS", "PERMANENTFLAGS", "EXISTS", "RECENT", "UNSEEN", "UIDVALIDITY", "UIDNEXT"}
	devGreet := "IMAP4rev1 ID ENABLE IDLE SASL-IR LITERAL+"
	devPost := "IMAP4rev1 SASL-IR ID ENABLE IDLE SORT SORT=DISPLAY THREAD=REFERENCES THREAD=REFS THREAD=ORDEREDSUBJECT UNSELECT CHILDREN NAMESPACE UIDPLUS LIST-EXTENDED CONDSTORE QRESYNC ESEARCH ESORT WITHIN LIST-STATUS BINARY MOVE PREVIEW SPECIAL-USE COMPRESS=DEFLATE LITERAL+"
	stalGreet := "IMAP4rev1 ENABLE SASL-IR LITERAL+ ID UTF8=ACCEPT"
	stalPost := "IMAP4rev1 ENABLE SASL-IR LITERAL+ ID UTF8=ACCEPT IDLE NAMESPACE CHILDREN BINARY UNSELECT UIDPLUS ESEARCH WITHIN SORT THREAD=REFERENCES LIST-EXTENDED LIST-STATUS ESORT SORT=DISPLAY SPECIAL-USE CREATE-SPECIAL-USE MOVE CONDSTORE QRESYNC UNAUTHENTICATE PREVIEW"
	stalOrder := []string{"EXISTS", "FLAGS", "RECENT", "PERMANENTFLAGS", "UIDVALIDITY", "UIDNEXT"}
	stalPerm := `\Deleted \Seen \Answered \Flagged \Draft \*`
	zimFlags := `\Answered \Deleted \Draft \Flagged \Seen $Forwarded $MDNSent Forwarded $Junk $NotJunk Junk JunkRecorded NonJunk NotJunk`
	zimPerm := `\Answered \Deleted \Draft \Flagged \Seen $Forwarded $MDNSent Forwarded \*`
	zimOrder := []string{"EXISTS", "RECENT", "UNSEEN", "UIDVALIDITY", "UIDNEXT", "FLAGS", "PERMANENTFLAGS"}
	cases := []struct {
		file     string
		greeting string
		greet    string
		pre      string
		post     string
		delim    string
		ns       string
		flags    string
		perm     string
		order    []string
		other    string
	}{
		{
			file:     "personalities/dovecot-10.json",
			greeting: "* OK Dovecot ready.",
			pre:      "IMAP4rev1 SASL-IR SORT THREAD=REFERENCES UNSELECT LITERAL+ IDLE CHILDREN NAMESPACE AUTH=PLAIN AUTH=LOGIN",
			post:     "IMAP4rev1 SASL-IR SORT THREAD=REFERENCES UNSELECT LITERAL+ IDLE CHILDREN NAMESPACE",
			delim:    ".",
			ns:       `* NAMESPACE (("" ".")) NIL NIL`,
			flags:    dovFlags,
			perm:     dovPerm,
			order:    dovOrder,
		},
		{
			file:     "personalities/dovecot-12.json",
			greeting: "* OK [CAPABILITY IMAP4rev1 LITERAL+ SASL-IR ID ENABLE AUTH=PLAIN AUTH=LOGIN] Dovecot ready.",
			greet:    "IMAP4rev1 LITERAL+ SASL-IR ID ENABLE AUTH=PLAIN AUTH=LOGIN",
			pre:      "IMAP4rev1 LITERAL+ SASL-IR ID ENABLE SORT SORT=DISPLAY THREAD=REFERENCES THREAD=REFS UNSELECT IDLE CHILDREN NAMESPACE UIDPLUS LIST-EXTENDED CONDSTORE QRESYNC ESEARCH ESORT WITHIN LIST-STATUS AUTH=PLAIN AUTH=LOGIN",
			post:     "IMAP4rev1 LITERAL+ SASL-IR ID ENABLE SORT SORT=DISPLAY THREAD=REFERENCES THREAD=REFS UNSELECT IDLE CHILDREN NAMESPACE UIDPLUS LIST-EXTENDED CONDSTORE QRESYNC ESEARCH ESORT WITHIN LIST-STATUS",
			delim:    ".",
			ns:       `* NAMESPACE (("" ".")) NIL NIL`,
			flags:    dovFlags,
			perm:     dovPerm,
			order:    dovOrder,
		},
		{
			file:     "personalities/dovecot-22.json",
			greeting: "* OK [CAPABILITY IMAP4rev1 LITERAL+ SASL-IR ID ENABLE IDLE AUTH=PLAIN] Dovecot ready.",
			greet:    "IMAP4rev1 LITERAL+ SASL-IR ID ENABLE IDLE AUTH=PLAIN",
			pre:      "IMAP4rev1 LITERAL+ SASL-IR ID ENABLE IDLE AUTH=PLAIN",
			post:     "IMAP4rev1 LITERAL+ SASL-IR ID ENABLE IDLE SORT SORT=DISPLAY THREAD=REFERENCES THREAD=REFS THREAD=ORDEREDSUBJECT UNSELECT CHILDREN NAMESPACE UIDPLUS LIST-EXTENDED CONDSTORE QRESYNC ESEARCH ESORT WITHIN LIST-STATUS SPECIAL-USE BINARY MOVE",
			delim:    ".",
			ns:       `* NAMESPACE (("" ".")) NIL NIL`,
			flags:    dovFlags,
			perm:     dovPerm,
			order:    dovOrder,
		},
		{
			file:     "personalities/dovecot-dev.json",
			greeting: "* OK [CAPABILITY " + devGreet + " AUTH=PLAIN] Dovecot ready.",
			greet:    devGreet + " AUTH=PLAIN",
			pre:      devGreet + " AUTH=PLAIN",
			post:     devPost,
			delim:    "/",
			ns:       `* NAMESPACE (("" "/")) NIL NIL`,
			flags:    dovFlags,
			perm:     dovPerm,
			order:    dovOrder,
		},
		{
			file:     "personalities/dovecot-245.json",
			greeting: "* OK [CAPABILITY " + devGreet + " AUTH=PLAIN AUTH=EXTERNAL] Dovecot ready.",
			greet:    devGreet + " AUTH=PLAIN AUTH=EXTERNAL",
			pre:      devGreet + " AUTH=PLAIN AUTH=EXTERNAL",
			post:     devPost + " UTF8=ACCEPT",
			delim:    "/",
			ns:       `* NAMESPACE (("" "/")) NIL NIL`,
			flags:    dovFlags,
			perm:     dovPerm,
			order:    dovOrder,
		},
	}
	liveStalGreet := stalGreet + " AUTH=PLAIN"
	liveStalPost := stalPost + " STARTTLS"
	for _, name := range []string{
		"personalities/stalwart.json",
		"personalities/stalwart-01625.json",
		"personalities/stalwart-dev.json",
		"personalities/stalwart-0155.json",
	} {
		cases = append(cases, struct {
			file     string
			greeting string
			greet    string
			pre      string
			post     string
			delim    string
			ns       string
			flags    string
			perm     string
			order    []string
			other    string
		}{
			file:     name,
			greeting: "* OK [CAPABILITY " + liveStalGreet + "] Stalwart IMAP4rev2 at your service.",
			greet:    liveStalGreet,
			pre:      liveStalGreet,
			post:     liveStalPost,
			delim:    "/",
			ns:       `* NAMESPACE (("" "/")) NIL NIL`,
			flags:    dovFlags,
			perm:     stalPerm,
			order:    stalOrder,
		})
	}
	cases = append(cases, struct {
		file     string
		greeting string
		greet    string
		pre      string
		post     string
		delim    string
		ns       string
		flags    string
		perm     string
		order    []string
		other    string
	}{
		file:     "personalities/zimbra-883.json",
		greeting: "* OK IMAP4rev1 proxy server ready",
		pre:      "BINARY CHILDREN CONDSTORE ENABLE ESEARCH ESORT ID IDLE IMAP4rev1 LIST-EXTENDED LIST-STATUS LITERAL+ NAMESPACE QRESYNC SASL-IR SORT THREAD=ORDEREDSUBJECT UIDPLUS UNSELECT WITHIN AUTH=PLAIN STARTTLS",
		post:     "IMAP4rev1 BINARY CHILDREN CONDSTORE ENABLE ESEARCH ESORT ID IDLE LIST-EXTENDED LIST-STATUS LITERAL+ NAMESPACE QRESYNC SASL-IR SORT THREAD=ORDEREDSUBJECT UIDPLUS UNSELECT WITHIN",
		delim:    "/",
		ns:       `* NAMESPACE (("" "/")) (("/home/" "/")) NIL`,
		flags:    zimFlags,
		perm:     zimPerm,
		order:    zimOrder,
		other:    "/home/",
	})
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			p, err := loadPersonality(c.file)
			if err != nil {
				t.Fatal(err)
			}
			if p.Delimiter != c.delim || p.PersonalPrefix != "" || p.OtherUsers != c.other || p.SharedFolders != nil {
				t.Fatalf("namespace fields delim %q prefix %q other %q shared %v", p.Delimiter, p.PersonalPrefix, p.OtherUsers, p.SharedFolders)
			}
			if !p.LogoutBye || !p.FetchPeekBody || !p.FetchSeenFlags || p.SortPartialCountOnly {
				t.Fatalf("quirks bye %v peek %v seen %v partial %v", p.LogoutBye, p.FetchPeekBody, p.FetchSeenFlags, p.SortPartialCountOnly)
			}
			if p.SelectFlags != c.flags || p.PermanentFlags != c.perm || strings.Join(p.SelectOrder, " ") != strings.Join(c.order, " ") {
				t.Fatalf("select fields flags %q perm %q order %v", p.SelectFlags, p.PermanentFlags, p.SelectOrder)
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
			for _, name := range banned {
				if listed(p.GreetingCaps, name) || listed(p.PreLogin, name) || listed(p.PostLogin, name) {
					t.Fatalf("file advertises %s", name)
				}
			}
			if c.file == "personalities/dovecot-10.json" && (listed(p.GreetingCaps, "ID") || listed(p.PreLogin, "ID") || listed(p.PostLogin, "ID")) {
				t.Fatal("dovecot-10 lists ID")
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
			wantPre := c.pre
			if strings.Contains(c.pre, "STARTTLS") {
				var kept []string
				for _, tok := range strings.Fields(c.pre) {
					if tok != "STARTTLS" {
						kept = append(kept, tok)
					}
				}
				wantPre = strings.Join(kept, " ")
			}
			if strings.Join(capsOf(pre.untagged[0]), " ") != wantPre {
				t.Fatalf("pre-login %v", capsOf(pre.untagged[0]))
			}
			login := cl.cmd("LOGIN demo secret").mustOK(t)
			wantPost := c.post
			if strings.Contains(c.post, "STARTTLS") {
				var kept []string
				for _, tok := range strings.Fields(c.post) {
					if tok != "STARTTLS" {
						kept = append(kept, tok)
					}
				}
				wantPost = strings.Join(kept, " ")
			}
			if strings.Join(capsOf(login.tagLine), " ") != wantPost {
				t.Fatalf("post-login %v", capsOf(login.tagLine))
			}
			for _, name := range banned {
				if listed(capsOf(login.tagLine), name) || listed(capsOf(pre.untagged[0]), name) {
					t.Fatalf("wire advertises %s", name)
				}
			}
			ns := cl.cmd("NAMESPACE").mustOK(t)
			if len(ns.untagged) != 1 || ns.untagged[0] != c.ns {
				t.Fatalf("namespace %q", ns.untagged)
			}
			sel := cl.cmd("SELECT INBOX").mustOK(t)
			wantOrder := append([]string{}, c.order...)
			if listed(strings.Fields(c.post), "CONDSTORE") {
				wantOrder = append(wantOrder, "HIGHESTMODSEQ")
			}
			if got := selectKinds(sel.untagged); strings.Join(got, " ") != strings.Join(wantOrder, " ") {
				t.Fatalf("select %v", got)
			}
			flags, perm := selectFlagLists(sel.untagged)
			if flags != c.flags || perm != c.perm {
				t.Fatalf("select flags %q perm %q", flags, perm)
			}
			out := cl.cmd("LOGOUT")
			if len(out.untagged) != 1 || out.untagged[0] != "* BYE LOGOUT" {
				t.Fatalf("logout %q", out.untagged)
			}
		})
	}
}

func selectFlagLists(lines []string) (flags, perm string) {
	for _, u := range lines {
		if strings.HasPrefix(u, "* FLAGS (") && strings.HasSuffix(u, ")") {
			flags = strings.TrimSuffix(strings.TrimPrefix(u, "* FLAGS ("), ")")
		}
		const mark = "[PERMANENTFLAGS ("
		if i := strings.Index(u, mark); i >= 0 {
			rest := u[i+len(mark):]
			if j := strings.Index(rest, ")]"); j >= 0 {
				perm = rest[:j]
			}
		}
	}
	return flags, perm
}

func TestCourierAndUW(t *testing.T) {
	caps := "IMAP4rev1 UIDPLUS CHILDREN NAMESPACE THREAD=ORDEREDSUBJECT THREAD=REFERENCES SORT IDLE ENABLE UTF8=ACCEPT"
	courierNS := `* NAMESPACE (("INBOX." ".")) NIL (("#shared." ".")("shared." "."))`
	courierFlags := `\Draft \Answered \Flagged \Deleted \Seen \Recent`
	courierPerm := `\* \Draft \Answered \Flagged \Deleted \Seen`
	courierOrder := []string{"FLAGS", "PERMANENTFLAGS", "EXISTS", "RECENT", "UIDVALIDITY"}
	uwCaps := "IMAP4REV1 LITERAL+ IDLE UIDPLUS NAMESPACE CHILDREN BINARY UNSELECT ESEARCH WITHIN SORT THREAD=REFERENCES THREAD=ORDEREDSUBJECT SASL-IR"
	uwPost := uwCaps + " STARTTLS"
	uwNS := `* NAMESPACE (("" "/")("#mhinbox" NIL)("#mh/" "/")) (("~" "/")) (("#shared/" "/")("#ftp/" "/")("#news." ".")("#public/" "/"))`
	uwFlags := `\Answered \Flagged \Deleted \Draft \Seen`
	uwPerm := `\* \Answered \Flagged \Deleted \Draft \Seen`
	uwOrder := []string{"EXISTS", "RECENT", "UIDVALIDITY", "UIDNEXT", "FLAGS", "PERMANENTFLAGS", "UNSEEN"}
	banned := []string{"QUOTA", "ACL", "ACL2=UNION", "I18NLEVEL=1", "LOGIN-REFERRALS", "ID"}
	cases := []struct {
		file     string
		greeting string
		greet    string
		pre      string
		post     string
		delim    string
		ns       string
		flags    string
		perm     string
		order    []string
	}{
		{
			file:     "personalities/courier-606.json",
			greeting: "* OK [CAPABILITY " + caps + "] Courier-IMAP ready. Copyright 1998-2022 S. Varshavchik.  See COPYING for distribution information.",
			greet:    caps,
			pre:      caps,
			post:     caps,
			delim:    ".",
			ns:       courierNS,
			flags:    courierFlags,
			perm:     courierPerm,
			order:    courierOrder,
		},
		{
			file:     "personalities/courier-50.json",
			greeting: "* OK [CAPABILITY " + caps + "] Courier-IMAP ready. Copyright 1998-2019 Double Precision, Inc.  See COPYING for distribution information.",
			greet:    caps,
			pre:      caps,
			post:     caps,
			delim:    ".",
			ns:       courierNS,
			flags:    courierFlags,
			perm:     courierPerm,
			order:    courierOrder,
		},
		{
			file:     "personalities/uwimap.json",
			greeting: "* OK [CAPABILITY IMAP4REV1 LITERAL+ SASL-IR] IMAP4rev1 2007e.404",
			greet:    "IMAP4REV1 LITERAL+ SASL-IR",
			pre:      uwCaps,
			post:     uwPost,
			delim:    "/",
			ns:       uwNS,
			flags:    uwFlags,
			perm:     uwPerm,
			order:    uwOrder,
		},
	}
	old := []struct {
		file string
		ns   string
	}{
		{"personalities/cyrus-3137.json", `* NAMESPACE (("" "/")) (("Other Users/" "/")) (("Shared Folders/" "/"))`},
		{"personalities/dovecot-245.json", `* NAMESPACE (("" "/")) NIL NIL`},
	}
	for _, o := range old {
		p, err := loadPersonality(o.file)
		if err != nil {
			t.Fatal(err)
		}
		if p.PersonalNS != nil || p.OtherNS != nil || p.SharedNS != nil {
			t.Fatalf("%s has namespace lists", o.file)
		}
		if p.namespaceLine() != o.ns {
			t.Fatalf("%s namespace %q", o.file, p.namespaceLine())
		}
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			p, err := loadPersonality(c.file)
			if err != nil {
				t.Fatal(err)
			}
			if p.Delimiter != c.delim || p.namespaceLine() != c.ns {
				t.Fatalf("delim %q namespace %q", p.Delimiter, p.namespaceLine())
			}
			wantSeen := c.file != "personalities/uwimap.json"
			if !p.LogoutBye || !p.FetchPeekBody || p.FetchSeenFlags != wantSeen {
				t.Fatalf("quirks bye %v peek %v seen %v", p.LogoutBye, p.FetchPeekBody, p.FetchSeenFlags)
			}
			if strings.Join(p.GreetingCaps, " ") != c.greet || strings.Join(p.PreLogin, " ") != c.pre || strings.Join(p.PostLogin, " ") != c.post {
				t.Fatalf("caps greet %v pre %v post %v", p.GreetingCaps, p.PreLogin, p.PostLogin)
			}
			for _, name := range banned {
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
			pre := cl.cmd("CAPABILITY").mustOK(t)
			if strings.Join(capsOf(pre.untagged[0]), " ") != c.pre {
				t.Fatalf("pre-login %v", capsOf(pre.untagged[0]))
			}
			login := cl.cmd("LOGIN demo secret").mustOK(t)
			wantPost := c.post
			if strings.Contains(c.post, "STARTTLS") {
				var kept []string
				for _, tok := range strings.Fields(c.post) {
					if tok != "STARTTLS" {
						kept = append(kept, tok)
					}
				}
				wantPost = strings.Join(kept, " ")
			}
			if strings.Join(capsOf(login.tagLine), " ") != wantPost {
				t.Fatalf("post-login %v", capsOf(login.tagLine))
			}
			for _, name := range banned {
				if listed(capsOf(login.tagLine), name) || listed(capsOf(pre.untagged[0]), name) || listed(capsOf(gotGreet), name) {
					t.Fatalf("wire advertises %s", name)
				}
			}
			ns := cl.cmd("NAMESPACE").mustOK(t)
			if len(ns.untagged) != 1 || ns.untagged[0] != c.ns {
				t.Fatalf("namespace %q", ns.untagged)
			}
			root := cl.cmd(`LIST "" ""`).mustOK(t)
			if len(root.untagged) != 1 || !strings.Contains(root.untagged[0], imapString(c.delim)) {
				t.Fatalf("list delim %q", root.untagged)
			}
			sel := cl.cmd("SELECT INBOX").mustOK(t)
			if got := selectKinds(sel.untagged); strings.Join(got, " ") != strings.Join(c.order, " ") {
				t.Fatalf("select %v", got)
			}
			flags, perm := selectFlagLists(sel.untagged)
			if flags != c.flags || perm != c.perm {
				t.Fatalf("select flags %q perm %q", flags, perm)
			}
			out := cl.cmd("LOGOUT")
			if len(out.untagged) != 1 || out.untagged[0] != "* BYE LOGOUT" {
				t.Fatalf("logout %q", out.untagged)
			}
		})
	}
}
