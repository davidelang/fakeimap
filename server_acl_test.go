package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wantACLStub(t *testing.T, cl *imapClient, line string) {
	t.Helper()
	rp := cl.cmd(line)
	if rp.status != "NO" || rp.text != "valid syntax not implemented" || len(rp.untagged) != 0 || strings.Contains(rp.tagLine, "[") {
		t.Fatalf("%q -> %s %q untagged %q", line, rp.status, rp.text, rp.untagged)
	}
	if strings.Contains(rp.tagLine, "TRYCREATE") || strings.Contains(strings.ToUpper(rp.tagLine), "ACL ") {
		t.Fatalf("%q leaked %s", line, rp.tagLine)
	}
}

func wantACLBad(t *testing.T, cl *imapClient, line string) {
	t.Helper()
	rp := cl.cmd(line)
	if rp.status != "BAD" || strings.Contains(rp.text, "valid syntax not implemented") || len(rp.untagged) != 0 {
		t.Fatalf("%q -> %s %q untagged %q", line, rp.status, rp.text, rp.untagged)
	}
}

func wantAuthFirst(t *testing.T, cl *imapClient, line string) {
	t.Helper()
	rp := cl.cmd(line)
	if rp.status != "NO" || rp.text != "must authenticate first" || len(rp.untagged) != 0 {
		t.Fatalf("before login %q -> %s %q untagged %q", line, rp.status, rp.text, rp.untagged)
	}
}

func assertNoACLToken(t *testing.T, caps []string, where string) {
	t.Helper()
	for _, tok := range []string{"ACL", "RIGHTS=kxten", "LIST-MYRIGHTS"} {
		if listed(caps, tok) {
			t.Fatalf("%s lists %s: %q", where, tok, caps)
		}
	}
}

func TestACLSyntaxStub(t *testing.T) {
	if capImplemented("ACL") || capImplemented("RIGHTS=kxten") || capImplemented("LIST-MYRIGHTS") {
		t.Fatal("ACL family is implemented")
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"ACL", "RIGHTS=kxten", "LIST-MYRIGHTS"} {
		if !strings.Contains(string(gaps), `"`+tok+`"`) {
			t.Fatalf("gaps dropped %s", tok)
		}
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), "gaps") {
			continue
		}
		path := filepath.Join("personalities", e.Name())
		p, err := loadPersonality(path)
		if err != nil {
			t.Fatal(err)
		}
		if p.listsCap("ACL") || p.listsCap("RIGHTS=kxten") || p.listsCap("LIST-MYRIGHTS") {
			t.Fatalf("%s lists an ACL token", path)
		}
		files = append(files, path)
	}
	if len(files) < 2 {
		t.Fatal("expected personality files")
	}

	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	pre := dial(t, bound.Listen)
	assertNoACLToken(t, capsOf(pre.greeting), "greeting")
	greetCap := pre.cmd("CAPABILITY").mustOK(t)
	if len(greetCap.untagged) != 1 {
		t.Fatalf("capability %q", greetCap.untagged)
	}
	assertNoACLToken(t, capsOf(greetCap.untagged[0]), "pre-login")
	for _, line := range []string{
		"SETACL INBOX anyone lr",
		"SETACL",
		"SETACL INBOX",
		"DELETEACL INBOX anyone",
		"DELETEACL",
		"GETACL INBOX",
		"GETACL",
		"GETACL INBOX extra",
		"LISTRIGHTS INBOX anyone",
		"LISTRIGHTS",
		"MYRIGHTS INBOX",
		"MYRIGHTS",
		"MYRIGHTS INBOX extra",
		"SETACL (INBOX) anyone lr",
		`SETACL INBOX "" lr`,
	} {
		wantAuthFirst(t, pre, line)
	}

	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	post := cl.cmd("CAPABILITY").mustOK(t)
	if len(post.untagged) != 1 {
		t.Fatalf("post capability %q", post.untagged)
	}
	assertNoACLToken(t, capsOf(post.untagged[0]), "post-login")

	for _, line := range []string{
		"SETACL INBOX anyone lr",
		"SETACL INBOX anyone \"\"",
		"SETACL INBOX anyone +",
		"SETACL INBOX anyone -",
		"SETACL INBOX anyone ll",
		"SETACL INBOX anyone lrswipkxteacd",
		"SETACL INBOX anyone +lrswipkxteacd",
		"SETACL INBOX anyone -llrr",
		"SETACL INBOX -fred l",
		"SETACL INBOX anyone {0+}\r\n",
		"SETACL \"No Such Box\" anyone l",
		"SETACL Missing anyone lr",
		`SETACL "" anyone lr`,
		"DELETEACL INBOX anyone",
		"DELETEACL INBOX -anyone",
		"DELETEACL Missing -x",
		"GETACL INBOX",
		"GETACL Missing",
		`GETACL "No Such"`,
		`GETACL ""`,
		"LISTRIGHTS INBOX anyone",
		"LISTRIGHTS INBOX -x",
		"LISTRIGHTS Missing anyone",
		"MYRIGHTS INBOX",
		"MYRIGHTS Missing",
		"setacl INBOX anyone l",
		"deleteacl INBOX anyone",
		"getacl INBOX",
		"listrights INBOX anyone",
		"myrights INBOX",
		"SETACL INBOX {6+}\r\nanyone lr",
	} {
		wantACLStub(t, cl, line)
	}
	for _, line := range []string{
		"SETACL",
		"SETACL INBOX",
		"SETACL INBOX anyone lr extra",
		"SETACL (INBOX) anyone lr",
		"SETACL INBOX anyone (lr)",
		"DELETEACL",
		"DELETEACL INBOX",
		"DELETEACL INBOX anyone extra",
		"GETACL",
		"GETACL INBOX extra",
		"GETACL (INBOX)",
		"LISTRIGHTS",
		"LISTRIGHTS INBOX",
		"LISTRIGHTS INBOX anyone extra",
		"LISTRIGHTS (INBOX) anyone",
		"MYRIGHTS",
		"MYRIGHTS INBOX extra",
		"MYRIGHTS (INBOX)",
		`SETACL INBOX "" lr`,
		"SETACL INBOX {0+}\r\nlr",
		`DELETEACL INBOX ""`,
		"DELETEACL INBOX {0+}\r\n",
		`LISTRIGHTS INBOX ""`,
		"LISTRIGHTS INBOX {0+}\r\n",
		"SETACL INBOX anyone n",
		"SETACL INBOX anyone N",
		"SETACL INBOX anyone L",
		"SETACL INBOX anyone lR",
		"SETACL INBOX anyone 1",
		"SETACL INBOX anyone l+",
		"SETACL INBOX anyone +-",
		"SETACL INBOX anyone +l-",
		`SETACL INBOX anyone "l r"`,
		"SETACL INBOX anyone ?",
	} {
		wantACLBad(t, cl, line)
	}

	listBefore := append([]string(nil), cl.cmd(`LIST "" "*"`).mustOK(t).untagged...)
	cl.cmd("SELECT INBOX").mustOK(t)
	flagsBefore := append([]string(nil), cl.cmd("FETCH 1:* (UID FLAGS)").mustOK(t).untagged...)
	statusBefore := append([]string(nil), cl.cmd("STATUS INBOX (MESSAGES UIDNEXT UNSEEN)").mustOK(t).untagged...)
	for _, line := range []string{
		"SETACL INBOX anyone lrswipkxteacd",
		"DELETEACL INBOX anyone",
		"GETACL NoSuchACLBox",
		"LISTRIGHTS INBOX -anyone",
		"MYRIGHTS INBOX",
		`SETACL "No Such Box" anyone ""`,
	} {
		wantACLStub(t, cl, line)
	}
	if rp := cl.cmd("GETACL NoSuchACLBox"); rp.status != "NO" || rp.text != "valid syntax not implemented" || strings.Contains(rp.tagLine, "TRYCREATE") || len(rp.untagged) != 0 {
		t.Fatalf("missing mailbox %s %q %q", rp.status, rp.text, rp.untagged)
	}
	flagsAfter := cl.cmd("FETCH 1:* (UID FLAGS)").mustOK(t).untagged
	statusAfter := cl.cmd("STATUS INBOX (MESSAGES UIDNEXT UNSEEN)").mustOK(t).untagged
	listAfter := cl.cmd(`LIST "" "*"`).mustOK(t).untagged
	if strings.Join(flagsAfter, "\n") != strings.Join(flagsBefore, "\n") {
		t.Fatalf("flags changed %q -> %q", flagsBefore, flagsAfter)
	}
	if strings.Join(statusAfter, "\n") != strings.Join(statusBefore, "\n") {
		t.Fatalf("status changed %q -> %q", statusBefore, statusAfter)
	}
	if strings.Join(listAfter, "\n") != strings.Join(listBefore, "\n") {
		t.Fatalf("mailboxes changed %q -> %q", listBefore, listAfter)
	}
	for _, ln := range listAfter {
		if strings.Contains(ln, "NoSuchACLBox") || strings.Contains(ln, "No Such Box") {
			t.Fatalf("lookup created %s", ln)
		}
	}

	for _, line := range []string{
		"UID SETACL INBOX anyone lr",
		"UID DELETEACL INBOX anyone",
		"UID GETACL INBOX",
		"UID LISTRIGHTS INBOX anyone",
		"UID MYRIGHTS INBOX",
	} {
		uid := dial(t, bound.Listen)
		uid.cmd("LOGIN demo secret").mustOK(t)
		rp := uid.cmd(line)
		if rp.status != "BAD" || rp.text != "syntax error" || strings.Contains(rp.text, "valid syntax not implemented") {
			t.Fatalf("%s -> %s %q", line, rp.status, rp.text)
		}
	}

	for _, path := range files {
		_, one := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
		c := dial(t, one.Listen)
		assertNoACLToken(t, capsOf(c.greeting), path+" greeting")
		before := c.cmd("CAPABILITY").mustOK(t)
		if len(before.untagged) != 1 {
			t.Fatalf("%s pre %q", path, before.untagged)
		}
		assertNoACLToken(t, capsOf(before.untagged[0]), path+" pre")
		wantAuthFirst(t, c, "MYRIGHTS")
		wantAuthFirst(t, c, "SETACL INBOX anyone lr")
		c.cmd("LOGIN demo secret").mustOK(t)
		after := c.cmd("CAPABILITY").mustOK(t)
		if len(after.untagged) != 1 {
			t.Fatalf("%s post %q", path, after.untagged)
		}
		assertNoACLToken(t, capsOf(after.untagged[0]), path+" post")
		wantACLStub(t, c, "MYRIGHTS INBOX")
		wantACLBad(t, c, "GETACL")
	}
}
