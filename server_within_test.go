package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestWithin(t *testing.T) {
	if !capImplemented("WITHIN") {
		t.Fatal("WITHIN is not implemented")
	}
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	cc.call(map[string]any{"op": "freeze-clock", "time": "2020-01-02T03:04:05Z"}).ok(t)
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "WITHIN") {
		t.Fatal("post-login missing WITHIN")
	}
	cl.cmd("CREATE Within").ok(t)
	cl.cmd("SELECT Within").ok(t)
	body := "hello"
	for _, when := range []string{
		"02-Jan-2020 03:03:55 +0000",
		"02-Jan-2020 03:04:00 +0000",
		"02-Jan-2020 03:04:05 +0000",
	} {
		cl.cmd(fmt.Sprintf("APPEND Within \"%s\" {%d+}\r\n%s", when, len(body), body)).ok(t)
	}
	want := map[string]string{
		"SEARCH YOUNGER 10":               "1 2 3",
		"SEARCH OLDER 10":                 "1",
		"SEARCH YOUNGER 5":                "2 3",
		"SEARCH OLDER 5":                  "1 2",
		"UID SEARCH YOUNGER 10":           "1 2 3",
		"UID SEARCH OLDER 10":             "1",
		"UID SEARCH YOUNGER 5":            "2 3",
		"UID SEARCH OLDER 5":              "1 2",
		"SORT (ARRIVAL) UTF-8 YOUNGER 10": "1 2 3",
	}
	for cmd, ids := range want {
		got := ""
		if strings.HasPrefix(cmd, "SORT") {
			got = sortIDs(t, cl.cmd(cmd))
		} else {
			got = strings.Join(searchIDs(t, cl.cmd(cmd)), " ")
		}
		if got != ids {
			t.Fatalf("%s -> %q, want %q", cmd, got, ids)
		}
	}
	if got := threadTree(t, cl.cmd("THREAD REFERENCES UTF-8 YOUNGER 10")); got != "(1)(2)(3)" {
		t.Fatalf("THREAD YOUNGER 10 %q", got)
	}
	if got := threadTree(t, cl.cmd("UID THREAD REFERENCES UTF-8 OLDER 10")); got != "(1)" {
		t.Fatalf("UID THREAD OLDER 10 %q", got)
	}
	for _, bad := range []string{
		"SEARCH OLDER 0",
		"SEARCH YOUNGER 0",
		"SEARCH OLDER",
		"SEARCH YOUNGER",
		"SEARCH OLDER abc",
		"SEARCH YOUNGER -1",
		"SEARCH OLDER +10",
		"UID SEARCH OLDER 0",
		"SORT (ARRIVAL) UTF-8 YOUNGER x",
		"THREAD REFERENCES UTF-8 OLDER",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
}

func TestWithinWallClock(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE WithinWall").ok(t)
	cl.cmd("SELECT WithinWall").ok(t)
	body := "x"
	for _, when := range []string{
		"01-Jan-1990 00:00:00 +0000",
		"01-Jan-2090 00:00:00 +0000",
	} {
		cl.cmd(fmt.Sprintf("APPEND WithinWall \"%s\" {%d+}\r\n%s", when, len(body), body)).ok(t)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH OLDER 1")), " "); got != "1" {
		t.Fatalf("wall OLDER %q", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH YOUNGER 1")), " "); got != "2" {
		t.Fatalf("wall YOUNGER %q", got)
	}
}

func TestWithinRequiresCap(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-22.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "WITHIN") {
		t.Fatal("cyrus-22 lists WITHIN")
	}
	for _, bad := range []string{
		"SEARCH OLDER 1",
		"SEARCH YOUNGER 1",
		"UID SEARCH OLDER 1",
		"SORT (ARRIVAL) UTF-8 YOUNGER 1",
		"THREAD REFERENCES UTF-8 OLDER 1",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
}
