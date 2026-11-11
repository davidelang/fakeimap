package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestMUTF7RoundTrip(t *testing.T) {
	for _, s := range []string{"INBOX", "INBOX/Generated", "a&b", "café", "Entwürfe", "~peter/mail/台北/日本語"} {
		enc := encodeMUTF7(s)
		dec, err := decodeMUTF7(enc)
		if err != nil || dec != s {
			t.Fatalf("%q -> %q -> %q %v", s, enc, dec, err)
		}
	}
	if encodeMUTF7("Entwürfe") != "Entw&APw-rfe" {
		t.Fatalf("entwurfe %q", encodeMUTF7("Entwürfe"))
	}
	if encodeMUTF7("~peter/mail/台北/日本語") != "~peter/mail/&U,BTFw-/&ZeVnLIqe-" {
		t.Fatalf("peter %q", encodeMUTF7("~peter/mail/台北/日本語"))
	}
	if _, err := decodeMUTF7("caf\xc3\xa9"); err == nil {
		t.Fatal("raw utf-8 accepted as modified utf-7")
	}
}

func TestUTF8Accept(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/utf8.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"UTF8=ACCEPT", "ENABLE"})

	wire := "u-caf&AOk-"
	uni := "u-café"
	cl.cmd("CREATE " + wire).ok(t)
	before := cl.cmd(`LIST "" "u-*"`)
	before.ok(t)
	if !listHas(before, wire) || listHas(before, uni) {
		t.Fatalf("before enable %q", before.untagged)
	}

	en := cl.cmd("ENABLE UTF8=ACCEPT")
	en.ok(t)
	if strings.Join(en.untagged, " ") != "* ENABLED UTF8=ACCEPT" {
		t.Fatalf("enable %q", en.untagged)
	}
	after := cl.cmd(`LIST "" "u-*"`)
	after.ok(t)
	if !listHas(after, uni) || listHas(after, wire) {
		t.Fatalf("after enable %q", after.untagged)
	}

	other := dial(t, bound.Listen)
	other.cmd("LOGIN demo secret").ok(t)
	still := other.cmd(`LIST "" "u-*"`)
	still.ok(t)
	if !listHas(still, wire) || listHas(still, uni) {
		t.Fatalf("other connection %q", still.untagged)
	}

	neu := "n-é"
	cl.cmd(fmt.Sprintf("CREATE {%d+}\r\n%s", len(neu), neu)).ok(t)
	created := cl.cmd(`LIST "" "n-*"`)
	created.ok(t)
	if !listHas(created, neu) {
		t.Fatalf("utf-8 create %q", created.untagged)
	}
	otherList := other.cmd(`LIST "" "n-*"`)
	otherList.ok(t)
	if !listHas(otherList, "n-&AOk-") || listHas(otherList, neu) {
		t.Fatalf("other sees %q", otherList.untagged)
	}

	cl.cmd("SELECT " + wire).no(t)
	cl.cmd("SELECT " + uni).ok(t)
	raw := "From: a@e.test\r\nTo: b@e.test\r\nSubject: =?UTF-8?Q?caf=C3=A9?=\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\nx\r\n"
	cl.cmd(fmt.Sprintf("APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n%s", uni, len(raw), raw)).ok(t)
	if cl.cmd("SELECT "+uni).status != "OK" {
		t.Fatal("reselect")
	}
	needle := "café"
	if cl.cmd(fmt.Sprintf("SEARCH CHARSET UTF-8 SUBJECT {%d+}\r\n%s", len(needle), needle)).status != "BAD" {
		t.Fatal("search charset was accepted")
	}
	if cl.cmd("SORT (SUBJECT) UTF-8 ALL").status != "BAD" {
		t.Fatal("sort charset was accepted")
	}
	if cl.cmd("SORT (SUBJECT) US-ASCII ALL").status != "BAD" {
		t.Fatal("sort us-ascii was accepted")
	}
	if got := searchIDs(t, cl.cmd(fmt.Sprintf("SEARCH SUBJECT {%d+}\r\n%s", len(needle), needle))); strings.Join(got, " ") != "1" {
		t.Fatalf("utf-8 search %v", got)
	}

	plain := dial(t, bound.Listen)
	plain.cmd("LOGIN demo secret").ok(t)
	plain.cmd("SELECT " + wire).ok(t)
	if got := searchIDs(t, plain.cmd(fmt.Sprintf("SEARCH CHARSET UTF-8 SUBJECT {%d+}\r\n%s", len(needle), needle))); strings.Join(got, " ") != "1" {
		t.Fatalf("charset before enable %v", got)
	}
	if plain.cmd("SORT (SUBJECT) UTF-8 ALL").status != "OK" {
		t.Fatal("sort before enable")
	}
}

func TestUTF8NotAdvertised(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	en := cl.cmd("ENABLE UTF8=ACCEPT")
	en.ok(t)
	for _, u := range en.untagged {
		if strings.Contains(strings.ToUpper(u), "UTF8=ACCEPT") {
			t.Fatalf("enabled %q", u)
		}
	}
	p, err := loadPersonality("personalities/utf8.json")
	if err != nil {
		t.Fatal(err)
	}
	if !listed(p.PreLogin, "UTF8=ACCEPT") || !listed(p.PostLogin, "UTF8=ACCEPT") {
		t.Fatal("utf8.json does not advertise UTF8=ACCEPT")
	}
}

func listHas(rp *reply, name string) bool {
	for _, u := range rp.untagged {
		if strings.Contains(u, name) && strings.Contains(u, "LIST") {
			return true
		}
	}
	return false
}
