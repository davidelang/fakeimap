package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListExtended(t *testing.T) {
	p, err := loadPersonality("personalities/list.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"LIST-EXTENDED", "CHILDREN", "SPECIAL-USE", "LIST-STATUS", "CREATE-SPECIAL-USE"} {
		if !listed(p.PreLogin, c) || !listed(p.PostLogin, c) {
			t.Fatalf("list.json missing %s", c)
		}
	}

	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/list.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)

	all := cl.cmd(`LIST "" "*"`)
	all.ok(t)
	joined := strings.Join(all.untagged, "\n")
	if !strings.Contains(joined, " INBOX") || !strings.Contains(joined, " INBOX/Generated") {
		t.Fatalf("list all %s", joined)
	}
	kids := cl.cmd(`LIST "" "*" RETURN (CHILDREN)`)
	kids.ok(t)
	kj := strings.Join(kids.untagged, "\n")
	if !strings.Contains(kj, `\HasChildren`) || !strings.Contains(kj, `\HasNoChildren`) {
		t.Fatalf("children %s", kj)
	}

	for i, flag := range []string{`\All`, `\Archive`, `\Drafts`, `\Flagged`, `\Junk`, `\Sent`, `\Trash`} {
		name := "use" + string(rune('a'+i))
		cl.cmd("CREATE " + name + " (USE (" + flag + "))").ok(t)
		line := listLine(cl.cmd(`LIST "" "`+name+`" RETURN (CHILDREN SPECIAL-USE)`).mustOK(t).untagged, name)
		if !strings.Contains(line, `\HasNoChildren`) || !strings.Contains(line, flag) {
			t.Fatalf("%s line %q", flag, line)
		}
	}
	again := cl.cmd(`CREATE useSent2 (USE (\Sent))`)
	if again.status != "NO" {
		t.Fatalf("duplicate use %s", again.status)
	}
	if len(cl.cmd(`LIST "" "useSent2"`).mustOK(t).untagged) != 0 {
		t.Fatal("conflict create left a mailbox")
	}
	badUse := cl.cmd(`CREATE baduse (USE (\Answered))`)
	if badUse.status != "NO" {
		t.Fatalf("bad use %s", badUse.status)
	}
	if len(cl.cmd(`LIST "" "baduse"`).mustOK(t).untagged) != 0 {
		t.Fatal("bad use left a mailbox")
	}

	st := cl.cmd("STATUS INBOX (MESSAGES UNSEEN)")
	st.ok(t)
	lst := cl.cmd(`LIST "" "INBOX" RETURN (STATUS (MESSAGES UNSEEN))`)
	lst.ok(t)
	if len(lst.untagged) != 2 || statusBody(lst.untagged[1]) != statusBody(st.untagged[0]) {
		t.Fatalf("list status %q status %q", lst.untagged, st.untagged)
	}
	gen := cl.cmd("STATUS INBOX/Generated (MESSAGES UNSEEN)")
	gen.ok(t)
	both := cl.cmd(`LIST "" "*" RETURN (STATUS (MESSAGES UNSEEN))`)
	both.ok(t)
	if statusBody(statusFor(both.untagged, "INBOX")) != statusBody(st.untagged[0]) || statusBody(statusFor(both.untagged, "INBOX/Generated")) != statusBody(gen.untagged[0]) {
		t.Fatalf("counts %q", both.untagged)
	}
}

func TestDatasetSpecialUse(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{
	  "accounts": [{"username": "demo", "password": "secret"}],
	  "mailboxes": [
	    {"name": "INBOX", "special_use": ["\\Trash"]},
	    {"name": "INBOX/Generated", "special_use": ["\\Archive"]}
	  ]
	}`)
	if err := os.WriteFile(filepath.Join(dir, "dataset.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/list.json", Dataset: dir})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	lines := cl.cmd(`LIST "" "*" RETURN (CHILDREN SPECIAL-USE)`).mustOK(t).untagged
	inbox := listLine(lines, "INBOX")
	gen := listLine(lines, "INBOX/Generated")
	if !strings.Contains(inbox, `\HasChildren`) || !strings.Contains(inbox, `\Trash`) {
		t.Fatalf("inbox %q", inbox)
	}
	if !strings.Contains(gen, `\HasNoChildren`) || !strings.Contains(gen, `\Archive`) {
		t.Fatalf("generated %q", gen)
	}
	if cl.cmd(`CREATE other (USE (\Trash))`).status != "NO" {
		t.Fatal("dataset flag was not reserved")
	}
}

func TestListExtendedNotAdvertised(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if cl.cmd(`LIST "" "*" RETURN (CHILDREN)`).status != "BAD" {
		t.Fatal("RETURN was accepted")
	}
	if cl.cmd(`LIST (SPECIAL-USE) "" "*"`).status != "BAD" {
		t.Fatal("selection was accepted")
	}
	plain := cl.cmd(`LIST "" "*"`)
	plain.ok(t)
	joined := strings.Join(plain.untagged, "\n")
	if !strings.Contains(joined, " INBOX") || !strings.Contains(joined, `\HasChildren`) || strings.Contains(joined, "RETURN") {
		t.Fatalf("plain list %s", joined)
	}
	if cl.cmd(`CREATE Z (USE (\Sent))`).status != "BAD" {
		t.Fatal("USE was accepted")
	}
	if len(cl.cmd(`LIST "" "Z"`).mustOK(t).untagged) != 0 {
		t.Fatal("rejected CREATE left a mailbox")
	}
}

func listed(caps []string, name string) bool {
	for _, c := range caps {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}

func listLine(lines []string, name string) string {
	for _, ln := range lines {
		if strings.HasSuffix(ln, " "+name) || strings.HasSuffix(ln, ` "`+name+`"`) {
			return ln
		}
	}
	return ""
}

func statusFor(lines []string, name string) string {
	for _, ln := range lines {
		if strings.HasPrefix(ln, "* STATUS ") && (strings.Contains(ln, " "+name+" ") || strings.Contains(ln, ` "`+name+`" `)) {
			return ln
		}
	}
	return ""
}

func statusBody(line string) string {
	i := strings.LastIndex(line, "(")
	j := strings.LastIndex(line, ")")
	if i < 0 || j < i {
		return line
	}
	return line[i : j+1]
}
