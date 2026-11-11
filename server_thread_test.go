package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThreadPersonality(t *testing.T) {
	p, err := loadPersonality("personalities/thread.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"THREAD=REFERENCES", "THREAD=ORDEREDSUBJECT", "THREAD=REFS"} {
		if !listed(p.PreLogin, c) || !listed(p.PostLogin, c) {
			t.Fatalf("thread.json missing %s", c)
		}
	}
	if listed(p.PreLogin, "UTF8=ACCEPT") || listed(p.PostLogin, "UTF8=ACCEPT") {
		t.Fatal("thread.json advertises UTF8=ACCEPT")
	}
}

func TestThreadAlgorithms(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/thread.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"THREAD=REFERENCES", "THREAD=ORDEREDSUBJECT", "THREAD=REFS"})

	cl.cmd("CREATE ThreadBox").ok(t)
	cl.cmd("SELECT ThreadBox").ok(t)
	msgs := []string{
		threadMsg("apple", "Mon, 01 Jan 2020 00:00:00 +0000", "a@t", "", ""),
		threadMsg("Re: apple", "Wed, 03 Jan 2020 00:00:00 +0000", "b@t", "a@t", ""),
		threadMsg("Fwd: apple", "Tue, 02 Jan 2020 00:00:00 +0000", "c@t", "", ""),
		threadMsg("mango", "Thu, 04 Jan 2020 00:00:00 +0000", "d@t", "gone@t", ""),
		threadMsg("pear", "Fri, 05 Jan 2020 00:00:00 +0000", "e@t", "", "gone@t"),
		threadMsg("zebra", "Sat, 06 Jan 2020 00:00:00 +0000", "f@t", "", ""),
		threadMsg("other", "Sun, 07 Jan 2020 00:00:00 +0000", "g@t", "", "a@t b@t"),
	}
	for i, raw := range msgs {
		internal := fmt.Sprintf("%02d-Jan-2020 00:00:00 +0000", i+1)
		cl.cmd(fmt.Sprintf("APPEND ThreadBox \"%s\" {%d+}\r\n%s", internal, len(raw), raw)).ok(t)
	}

	refs := "(1 2 7)(3)((4)(5))(6)"
	if got := threadTree(t, cl.cmd("UID THREAD REFERENCES UTF-8 ALL")); got != refs {
		t.Fatalf("REFERENCES %q", got)
	}
	if got := threadTree(t, cl.cmd("UID THREAD REFS UTF-8 ALL")); got != refs {
		t.Fatalf("REFS %q", got)
	}
	if got := threadTree(t, cl.cmd("THREAD REFERENCES UTF-8 ALL")); got != refs {
		t.Fatalf("seq REFERENCES %q", got)
	}
	ordered := "(1 (3)(2))(4)(7)(5)(6)"
	if got := threadTree(t, cl.cmd("UID THREAD ORDEREDSUBJECT UTF-8 ALL")); got != ordered {
		t.Fatalf("ORDEREDSUBJECT %q", got)
	}
	if got := threadTree(t, cl.cmd("THREAD ORDEREDSUBJECT US-ASCII ALL")); got != ordered {
		t.Fatalf("seq ORDEREDSUBJECT %q", got)
	}
	if got := threadTree(t, cl.cmd("UID THREAD REFERENCES UTF-8 SUBJECT zebra")); got != "(6)" {
		t.Fatalf("subset %q", got)
	}
	if got := threadTree(t, cl.cmd("THREAD REFERENCES UTF-8 FROM nosuch@e.test")); got != "" {
		t.Fatalf("empty %q", got)
	}
	if cl.cmd("THREAD BOGUS UTF-8 ALL").status != "BAD" {
		t.Fatal("unknown algorithm was accepted")
	}
	if cl.cmd("UID THREAD REFERENCES KOI8-R ALL").status != "BAD" {
		t.Fatal("bad charset was accepted")
	}

	cl.cmd("CREATE ThreadSeq").ok(t)
	cl.cmd("SELECT ThreadSeq").ok(t)
	alone := threadMsg("alone", "Mon, 01 Jan 2020 00:00:00 +0000", "z@t", "", "")
	apple := threadMsg("apple", "Tue, 02 Jan 2020 00:00:00 +0000", "a@t", "", "")
	reply := threadMsg("Re: apple", "Wed, 03 Jan 2020 00:00:00 +0000", "b@t", "a@t", "")
	for _, raw := range []string{alone, apple, reply} {
		cl.cmd(fmt.Sprintf("APPEND ThreadSeq \"01-Jan-2020 00:00:00 +0000\" {%d+}\r\n%s", len(raw), raw)).ok(t)
	}
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).ok(t)
	cl.cmd("EXPUNGE").ok(t)
	if got := threadTree(t, cl.cmd("THREAD REFERENCES UTF-8 ALL")); got != "(1 2)" {
		t.Fatalf("seq after expunge %q", got)
	}
	if got := threadTree(t, cl.cmd("UID THREAD REFERENCES UTF-8 ALL")); got != "(2 3)" {
		t.Fatalf("uid after expunge %q", got)
	}
	if got := threadTree(t, cl.cmd("UID THREAD ORDEREDSUBJECT UTF-8 ALL")); got != "(2 3)" {
		t.Fatalf("ordered uid %q", got)
	}
}

func TestThreadRequiresCaps(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if cl.cmd("THREAD REFERENCES UTF-8 ALL").status != "BAD" {
		t.Fatal("THREAD was accepted")
	}
	if cl.cmd("UID THREAD ORDEREDSUBJECT UTF-8 ALL").status != "BAD" {
		t.Fatal("UID THREAD was accepted")
	}
	if cl.cmd("UID THREAD REFS UTF-8 ALL").status != "BAD" {
		t.Fatal("REFS was accepted")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "refs-only.json")
	p, err := loadPersonality("personalities/thread.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"THREAD=ORDEREDSUBJECT", "THREAD=REFS"} {
		p.PreLogin = dropCap(p.PreLogin, name)
		p.PostLogin = dropCap(p.PostLogin, name)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if cl.cmd("THREAD ORDEREDSUBJECT UTF-8 ALL").status != "BAD" {
		t.Fatal("ORDEREDSUBJECT was accepted")
	}
	if cl.cmd("UID THREAD REFS UTF-8 ALL").status != "BAD" {
		t.Fatal("REFS was accepted")
	}
	if cl.cmd("THREAD REFERENCES UTF-8 ALL").status != "OK" {
		t.Fatal("REFERENCES was rejected")
	}
}

func TestThreadUTF8Charset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thread-utf8.json")
	p, err := loadPersonality("personalities/utf8.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"THREAD=REFERENCES", "THREAD=ORDEREDSUBJECT", "THREAD=REFS"} {
		p.PreLogin = append(p.PreLogin, name)
		p.PostLogin = append(p.PostLogin, name)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE U8").ok(t)
	cl.cmd("SELECT U8").ok(t)
	raw := "From: a@e.test\r\nTo: b@e.test\r\nSubject: =?UTF-8?Q?caf=C3=A9?=\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMessage-ID: <cafe@t>\r\n\r\nx\r\n"
	reply := "From: a@e.test\r\nTo: b@e.test\r\nSubject: Re: =?UTF-8?Q?caf=C3=A9?=\r\nDate: Tue, 02 Jan 2024 00:00:00 +0000\r\nMessage-ID: <cafe-re@t>\r\nIn-Reply-To: <cafe@t>\r\n\r\ny\r\n"
	cl.cmd(fmt.Sprintf("APPEND U8 \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n%s", len(raw), raw)).ok(t)
	cl.cmd(fmt.Sprintf("APPEND U8 \"02-Jan-2024 00:00:00 +0000\" {%d+}\r\n%s", len(reply), reply)).ok(t)

	plain := dial(t, bound.Listen)
	plain.cmd("LOGIN demo secret").ok(t)
	plain.cmd("SELECT U8").ok(t)
	if got := threadTree(t, plain.cmd("UID THREAD REFERENCES UTF-8 ALL")); got != "(1 2)" {
		t.Fatalf("before enable %q", got)
	}
	if got := threadTree(t, plain.cmd("UID THREAD ORDEREDSUBJECT UTF-8 ALL")); got != "(1 2)" {
		t.Fatalf("ordered before enable %q", got)
	}

	cl.cmd("ENABLE UTF8=ACCEPT").ok(t)
	if cl.cmd("THREAD REFERENCES UTF-8 ALL").status != "BAD" {
		t.Fatal("charset was accepted")
	}
	if cl.cmd("UID THREAD ORDEREDSUBJECT US-ASCII ALL").status != "BAD" {
		t.Fatal("us-ascii was accepted")
	}
	if cl.cmd("THREAD REFERENCES CHARSET UTF-8 ALL").status != "BAD" {
		t.Fatal("CHARSET was accepted")
	}
	needle := "café"
	if got := threadTree(t, cl.cmd(fmt.Sprintf("UID THREAD ORDEREDSUBJECT SUBJECT {%d+}\r\n%s", len(needle), needle))); got != "(1 2)" {
		t.Fatalf("utf-8 criteria %q", got)
	}
	if got := threadTree(t, cl.cmd("UID THREAD REFS ALL")); got != "(1 2)" {
		t.Fatalf("refs after enable %q", got)
	}
}

func threadMsg(subj, date, id, inReply, refs string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: a@e.test\r\nTo: b@e.test\r\nSubject: %s\r\nDate: %s\r\n", subj, date)
	if id != "" {
		fmt.Fprintf(&b, "Message-ID: <%s>\r\n", id)
	}
	if inReply != "" {
		fmt.Fprintf(&b, "In-Reply-To: <%s>\r\n", inReply)
	}
	if refs != "" {
		var ids []string
		for _, part := range strings.Fields(refs) {
			ids = append(ids, "<"+part+">")
		}
		fmt.Fprintf(&b, "References: %s\r\n", strings.Join(ids, " "))
	}
	b.WriteString("\r\nx\r\n")
	return b.String()
}

func threadTree(t *testing.T, rp *reply) string {
	t.Helper()
	if rp.status != "OK" || len(rp.untagged) != 1 || !strings.HasPrefix(rp.untagged[0], "* THREAD") {
		t.Fatalf("thread reply %+v", rp)
	}
	return strings.TrimSpace(strings.TrimPrefix(rp.untagged[0], "* THREAD"))
}
