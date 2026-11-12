package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestStatusSizeLoopback(t *testing.T) {
	if !capImplemented("STATUS=SIZE") {
		t.Fatal("STATUS=SIZE is not implemented")
	}
	pre := []string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "LIST-EXTENDED", "LIST-STATUS", "AUTH=PLAIN", "STATUS=SIZE"}
	post := []string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "LIST-EXTENDED", "LIST-STATUS", "STATUS=SIZE"}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: writePersonality(t, pre, post)})
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "STATUS=SIZE") {
		t.Fatalf("greeting lists STATUS=SIZE: %q", cl.greeting)
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "STATUS=SIZE") {
		t.Fatal("post-login missing STATUS=SIZE")
	}
	cl.cmd("CREATE Sz").mustOK(t)
	if got := statusField(t, cl, "Sz", "SIZE"); got != "0" {
		t.Fatalf("empty size %s", got)
	}
	cl.cmd("APPEND Sz {5+}\r\nhello").mustOK(t)
	cl.cmd("APPEND Sz {8+}\r\nhelloXXX").mustOK(t)
	cl.cmd("SELECT Sz").mustOK(t)
	a := fetchSize(t, cl.cmd("FETCH 1 (RFC822.SIZE)"))
	b := fetchSize(t, cl.cmd("FETCH 2 (RFC822.SIZE)"))
	if a == b {
		t.Fatalf("sizes not distinct: %d", a)
	}
	sum := strconv.Itoa(a + b)
	if got := statusField(t, cl, "Sz", "SIZE"); got != sum {
		t.Fatalf("size %s want %s", got, sum)
	}
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).mustOK(t)
	if got := statusField(t, cl, "Sz", "SIZE"); got != sum {
		t.Fatalf("deleted still present: %s want %s", got, sum)
	}
	cl.cmd("EXPUNGE").mustOK(t)
	left := strconv.Itoa(b)
	if got := statusField(t, cl, "Sz", "SIZE"); got != left {
		t.Fatalf("after expunge %s want %s", got, left)
	}
	lst := cl.cmd(`LIST "" "Sz" RETURN (STATUS (SIZE))`).mustOK(t)
	saw := false
	for _, u := range lst.untagged {
		if strings.Contains(u, "SIZE "+left) {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("list status %q", lst.untagged)
	}
}

func TestStatusSizeAbsent(t *testing.T) {
	pre := []string{"IMAP4rev1", "LITERAL+", "AUTH=PLAIN", "UIDPLUS", "LIST-EXTENDED", "LIST-STATUS"}
	post := []string{"IMAP4rev1", "LITERAL+", "UIDPLUS", "LIST-EXTENDED", "LIST-STATUS"}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: writePersonality(t, pre, post)})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	rp := cl.cmd("STATUS INBOX (SIZE)")
	if rp.status != "BAD" {
		t.Fatalf("status without capability %s %s", rp.status, rp.tagLine)
	}
}

func TestStatusSizeLists(t *testing.T) {
	check := func(file, after string, pre, post bool) {
		t.Helper()
		p, err := loadPersonality(file)
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "STATUS=SIZE") {
			t.Fatalf("%s greeting lists STATUS=SIZE", file)
		}
		if pre {
			if i, j := capAt(p.PreLogin, "STATUS=SIZE"), capAt(p.PreLogin, after); i < 0 || j < 0 || i != j+1 {
				t.Fatalf("%s pre-login STATUS=SIZE not after %s", file, after)
			}
		} else if listed(p.PreLogin, "STATUS=SIZE") {
			t.Fatalf("%s pre-login lists STATUS=SIZE", file)
		}
		if post {
			if i, j := capAt(p.PostLogin, "STATUS=SIZE"), capAt(p.PostLogin, after); i < 0 || j < 0 || i != j+1 {
				t.Fatalf("%s post-login STATUS=SIZE not after %s", file, after)
			}
		} else if listed(p.PostLogin, "STATUS=SIZE") {
			t.Fatalf("%s post-login lists STATUS=SIZE", file)
		}
	}
	for _, file := range []string{
		"personalities/cyrus-36.json",
		"personalities/cyrus-3124.json",
		"personalities/cyrus-3137.json",
	} {
		check(file, "SPECIAL-USE", true, true)
	}
	check("personalities/dovecot-245.json", "SPECIAL-USE", false, true)
	check("personalities/dovecot-dev.json", "SPECIAL-USE", false, true)
	check("personalities/sasl.json", "SPECIAL-USE", false, true)
	for _, file := range []string{
		"personalities/stalwart.json",
		"personalities/stalwart-01625.json",
		"personalities/stalwart-dev.json",
		"personalities/stalwart-0155.json",
	} {
		check(file, "UNAUTHENTICATE", false, true)
	}
	for _, file := range []string{
		"personalities/cyrus-22.json",
		"personalities/cyrus-24.json",
		"personalities/cyrus-25.json",
		"personalities/dovecot-10.json",
		"personalities/dovecot-12.json",
		"personalities/dovecot-22.json",
		"personalities/courier-50.json",
		"personalities/courier-606.json",
		"personalities/uwimap.json",
		"personalities/zimbra-883.json",
	} {
		check(file, "", false, false)
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "STATUS=SIZE") {
		t.Fatal("gaps still lists STATUS=SIZE")
	}
	for _, tok := range []string{"ACL", "QUOTA", "URLAUTH"} {
		if !strings.Contains(string(gaps), `"`+tok+`"`) {
			t.Fatalf("gaps dropped %s", tok)
		}
	}
}
