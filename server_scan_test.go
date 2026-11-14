package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScan(t *testing.T) {
	if !capImplemented("SCAN") {
		t.Fatal("SCAN is not implemented")
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/uwimap.json"})
	early := dial(t, bound.Listen)
	if strings.Contains(early.greeting, "SCAN") {
		t.Fatal("greeting lists SCAN")
	}
	pre := early.cmd(`SCAN "" "*" ""`)
	if pre.status != "NO" || len(pre.untagged) != 0 {
		t.Fatalf("before login %s %q", pre.status, pre.untagged)
	}
	if !listed(capsOf(early.cmd("CAPABILITY").mustOK(t).untagged[0]), "SCAN") {
		t.Fatal("pre-login missing SCAN")
	}

	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "SCAN") {
		t.Fatal("post-login missing SCAN")
	}

	one := cl.cmd(`SCAN "" "*" "generated"`).mustOK(t)
	if strings.Join(one.untagged, "\n") != "* SCAN INBOX/Generated\n* SCAN ~demo/INBOX/Generated" {
		t.Fatalf("name filter %q", one.untagged)
	}
	same := cl.cmd(`SCAN "" "*" "GENERATED"`).mustOK(t)
	if strings.Join(same.untagged, "\n") != "* SCAN INBOX/Generated\n* SCAN ~demo/INBOX/Generated" {
		t.Fatalf("case %q", same.untagged)
	}
	text := cl.cmd(`SCAN "" "*" "Alice"`).mustOK(t)
	if len(text.untagged) != 0 {
		t.Fatalf("message text %q", text.untagged)
	}
	cafe := "café"
	bodyHit := cl.cmd(fmt.Sprintf("SCAN \"\" \"*\" {%d+}\r\n%s", len(cafe), cafe)).mustOK(t)
	if len(bodyHit.untagged) != 0 {
		t.Fatalf("body text %q", bodyHit.untagged)
	}
	if strings.Join(cl.cmd(`SCAN "INBOX/" "%" "gen"`).mustOK(t).untagged, "\n") != "* SCAN INBOX/Generated" {
		t.Fatal("reference pattern")
	}
	if len(cl.cmd(`SCAN "" "INBOX" "gen"`).mustOK(t).untagged) != 0 {
		t.Fatal("pattern did not limit the name filter")
	}

	cl.cmd("SELECT INBOX").ok(t)
	before := cl.cmd("FETCH 1:2 (FLAGS UID)").mustOK(t)
	again := cl.cmd(`SCAN "" "*" "Alice"`).mustOK(t)
	if len(again.untagged) != 0 {
		t.Fatal("selected scan searched text")
	}
	after := cl.cmd("FETCH 1:2 (FLAGS UID)").mustOK(t)
	if strings.Join(before.untagged, "\n") != strings.Join(after.untagged, "\n") || after.status != "OK" {
		t.Fatalf("flags or selection changed %q", after.untagged)
	}

	cl.cmd(`CREATE "Scan Me"`).ok(t)
	cl.cmd("CREATE scan-low").ok(t)
	cl.cmd("CREATE Notes").ok(t)
	raw := "From: a@e.test\r\nTo: b@e.test\r\nSubject: scan-low\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\nscan-low is here\r\n"
	cl.cmd(fmt.Sprintf("APPEND Notes {%d+}\r\n%s", len(raw), raw)).ok(t)
	if len(cl.cmd(`SCAN "" "Notes" "scan-low"`).mustOK(t).untagged) != 0 {
		t.Fatal("mailbox name missed and message text matched")
	}
	wantScan := "* SCAN \"Scan Me\"\n* SCAN scan-low\n* SCAN \"~demo/Scan Me\"\n* SCAN ~demo/scan-low"
	if strings.Join(cl.cmd(`SCAN "" "*" "scan"`).mustOK(t).untagged, "\n") != wantScan {
		t.Fatal("order or atom form")
	}
	all := "* SCAN INBOX\n* SCAN INBOX/Generated\n* SCAN \"Scan Me\"\n* SCAN scan-low\n* SCAN Notes\n* SCAN ~demo\n* SCAN ~demo/INBOX\n* SCAN ~demo/INBOX/Generated\n* SCAN \"~demo/Scan Me\"\n* SCAN ~demo/scan-low\n* SCAN ~demo/Notes"
	if strings.Join(cl.cmd(`SCAN "" "*" ""`).mustOK(t).untagged, "\n") != all {
		t.Fatalf("empty string %q", cl.cmd(`SCAN "" "*" ""`).untagged)
	}
	if strings.Join(cl.cmd(`SCAN "INBOX/" "" ""`).mustOK(t).untagged, "\n") != "* SCAN INBOX" {
		t.Fatal("hierarchy row")
	}
	if strings.Join(cl.cmd(`SCAN "" "" ""`).mustOK(t).untagged, "\n") != `* SCAN ""` {
		t.Fatal("empty hierarchy row")
	}
	none := cl.cmd(`SCAN "INBOX/" "" "no-such"`).mustOK(t)
	if len(none.untagged) != 0 {
		t.Fatalf("no match %q", none.untagged)
	}
	still := cl.cmd("FETCH 1 (UID)").mustOK(t)
	if len(still.untagged) != 1 || !strings.Contains(still.untagged[0], "UID") {
		t.Fatal("selected mailbox changed")
	}

	for _, bad := range []string{
		"SCAN",
		`SCAN ""`,
		`SCAN "" "*"`,
		`SCAN "" "*" a b`,
		`SCAN "" "*" RETURN (SUBSCRIBED)`,
		`SCAN RETURN (SUBSCRIBED) "" "*"`,
		`SCAN (SUBSCRIBED) "" "*" "a"`,
		`SCAN "" "*" ()`,
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
}

func TestScanUTF8(t *testing.T) {
	p, err := loadPersonality("personalities/utf8.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PreLogin = append(append([]string{}, p.PreLogin...), "SCAN")
	p.PostLogin = append(append([]string{}, p.PostLogin...), "SCAN")
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	wire := "u-caf&AOk-"
	uni := "u-café"
	cl.cmd("CREATE " + wire).ok(t)
	if strings.Join(cl.cmd(`SCAN "" "u-*" "caf"`).mustOK(t).untagged, "\n") != "* SCAN "+wire {
		t.Fatal("modified UTF-7 name")
	}
	if strings.Join(cl.cmd(`SCAN "" "`+wire+`" ""`).mustOK(t).untagged, "\n") != "* SCAN "+wire {
		t.Fatal("modified UTF-7 pattern")
	}
	if strings.Join(cl.cmd(`SCAN "`+wire+`" "" ""`).mustOK(t).untagged, "\n") != "* SCAN "+wire {
		t.Fatal("modified UTF-7 reference")
	}
	if len(cl.cmd(fmt.Sprintf("SCAN \"\" \"u-*\" {%d+}\r\n%s", len(uni), uni)).mustOK(t).untagged) != 0 {
		t.Fatal("UTF-8 string matched modified UTF-7")
	}
	if cl.cmd(fmt.Sprintf("SCAN \"\" {%d+}\r\n%s \"\"", len(uni), uni)).status != "BAD" {
		t.Fatal("raw UTF-8 pattern accepted before enable")
	}
	cl.cmd("ENABLE UTF8=ACCEPT").ok(t)
	uniLine := fmt.Sprintf("* SCAN {%d}\r\n%s", len(uni), uni)
	if strings.Join(cl.cmd(`SCAN "" "u-*" "caf"`).mustOK(t).untagged, "\n") != uniLine {
		t.Fatal("UTF-8 name after enable")
	}
	if strings.Join(cl.cmd(fmt.Sprintf("SCAN \"\" {%d+}\r\n%s \"\"", len(uni), uni)).mustOK(t).untagged, "\n") != uniLine {
		t.Fatal("UTF-8 pattern after enable")
	}
	if strings.Join(cl.cmd(fmt.Sprintf("SCAN {%d+}\r\n%s \"\" \"\"", len(uni), uni)).mustOK(t).untagged, "\n") != uniLine {
		t.Fatal("UTF-8 reference after enable")
	}
	if len(cl.cmd(`SCAN "" "`+wire+`" ""`).mustOK(t).untagged) != 0 {
		t.Fatal("modified UTF-7 pattern matched after enable")
	}
	if strings.Join(cl.cmd(fmt.Sprintf("SCAN \"\" \"u-*\" {%d+}\r\n%s", len(uni), uni)).mustOK(t).untagged, "\n") != uniLine {
		t.Fatal("UTF-8 string after enable")
	}
}

func TestScanNotListed(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	cl := dial(t, bound.Listen)
	if cl.cmd(`SCAN "" "*" ""`).status != "NO" {
		t.Fatal("unlisted scan before login was not NO")
	}
	cl.cmd("LOGIN demo secret").ok(t)
	caps := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	if listed(caps, "SCAN") {
		t.Fatal("cyrus-3137 lists SCAN")
	}
	if cl.cmd(`SCAN "" "*" ""`).status != "BAD" {
		t.Fatal("unlisted scan was accepted")
	}
}

func TestScanLists(t *testing.T) {
	type spot struct {
		file     string
		prePrev  string
		preNext  string
		postPrev string
		postNext string
	}
	for _, c := range []spot{
		{"personalities/cyrus-24.json", "QRESYNC", "XLIST", "QRESYNC", "XLIST"},
		{"personalities/cyrus-25.json", "QRESYNC", "XLIST", "QRESYNC", "XLIST"},
		{"personalities/cyrus-36.json", "WITHIN", "SORT=MODSEQ", "WITHIN", "SORT=MODSEQ"},
		{"personalities/uwimap.json", "WITHIN", "SORT", "WITHIN", "SORT"},
	} {
		p, err := loadPersonality(c.file)
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "SCAN") {
			t.Fatalf("%s greeting lists SCAN", c.file)
		}
		if capAt(p.PreLogin, "SCAN") != capAt(p.PreLogin, c.prePrev)+1 || capAt(p.PreLogin, c.preNext) != capAt(p.PreLogin, "SCAN")+1 {
			t.Fatalf("%s pre-login SCAN not between %s and %s", c.file, c.prePrev, c.preNext)
		}
		if capAt(p.PostLogin, "SCAN") != capAt(p.PostLogin, c.postPrev)+1 || capAt(p.PostLogin, c.postNext) != capAt(p.PostLogin, "SCAN")+1 {
			t.Fatalf("%s post-login SCAN not between %s and %s", c.file, c.postPrev, c.postNext)
		}
	}
	allow := map[string]bool{
		"cyrus-24.json": true, "cyrus-25.json": true, "cyrus-36.json": true, "uwimap.json": true,
	}
	matches, err := filepath.Glob("personalities/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range matches {
		base := filepath.Base(f)
		if strings.HasSuffix(base, ".gaps.json") || allow[base] {
			continue
		}
		p, err := loadPersonality(f)
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "SCAN") || listed(p.PreLogin, "SCAN") || listed(p.PostLogin, "SCAN") {
			t.Fatalf("%s gained SCAN", base)
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), `"SCAN"`) {
		t.Fatal("gaps lists SCAN")
	}
}
