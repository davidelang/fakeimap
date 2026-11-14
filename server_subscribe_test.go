package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSubscribe(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)

	plain := cl.cmd(`LIST "" "*"`).mustOK(t)
	ret := cl.cmd(`LIST "" "*" RETURN (CHILDREN)`).mustOK(t)
	if !reflect.DeepEqual(listNames(plain.untagged), listNames(ret.untagged)) {
		t.Fatalf("return changed names %v %v", listNames(plain.untagged), listNames(ret.untagged))
	}
	joinedPlain := strings.Join(append(append([]string{}, plain.untagged...), ret.untagged...), "\n")
	if strings.Contains(joinedPlain, `\Subscribed`) {
		t.Fatalf("plain list gained \\Subscribed %s", joinedPlain)
	}
	if !strings.Contains(joinedPlain, " INBOX") || !strings.Contains(joinedPlain, " INBOX/Generated") {
		t.Fatalf("plain list %s", joinedPlain)
	}

	sub := cl.cmd(`LIST (SUBSCRIBED) "" "*"`).mustOK(t)
	if got := listNames(sub.untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("initial subscribed %v", got)
	}
	plainInbox := listLine(plain.untagged, "INBOX")
	if listLine(sub.untagged, "INBOX") != withSubscribed(plainInbox) {
		t.Fatalf("subscribed flags %q plain %q", listLine(sub.untagged, "INBOX"), plainInbox)
	}
	if got := listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("lsub %v", got)
	}

	cl.cmd("CREATE Fresh").ok(t)
	if got := listNames(cl.cmd(`LIST (SUBSCRIBED) "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("create subscribed %v", got)
	}
	if cl.cmd("SUBSCRIBE MissingQx").status != "NO" {
		t.Fatal("missing subscribe was not NO")
	}
	if got := listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("missing subscribe changed the set %v", got)
	}
	cl.cmd("SUBSCRIBE Fresh").ok(t)
	cl.cmd("SUBSCRIBE Fresh").ok(t)
	cl.cmd("SUBSCRIBE inbox").ok(t)
	want := []string{"INBOX", "Fresh"}
	if got := listNames(cl.cmd(`LIST (SUBSCRIBED) "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, want) {
		t.Fatalf("after subscribe %v", got)
	}
	if got := listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, want) {
		t.Fatalf("lsub names %v", got)
	}
	freshPlain := listLine(cl.cmd(`LIST "" "Fresh"`).mustOK(t).untagged, "Fresh")
	freshSub := listLine(cl.cmd(`LIST (SUBSCRIBED) "" "Fresh"`).mustOK(t).untagged, "Fresh")
	if freshSub != withSubscribed(freshPlain) {
		t.Fatalf("fresh flags %q plain %q", freshSub, freshPlain)
	}
	if got := listNames(cl.cmd(`LIST (SUBSCRIBED) "" "INBOX"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("pattern inbox %v", got)
	}
	if len(cl.cmd(`LIST (SUBSCRIBED) "" "NoSuch*"`).mustOK(t).untagged) != 0 {
		t.Fatal("pattern matched a missing name")
	}

	cl.cmd("SUBSCRIBE INBOX/Generated").ok(t)
	if got := listNames(cl.cmd(`LIST (SUBSCRIBED) "INBOX/" "%"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX/Generated"}) {
		t.Fatalf("reference %v", got)
	}
	cl.cmd("UNSUBSCRIBE INBOX/Generated").ok(t)

	cl.cmd("CREATE Other").ok(t)
	cl.cmd("UNSUBSCRIBE Other").ok(t)
	cl.cmd("UNSUBSCRIBE MissingQx").ok(t)
	if strings.Contains(strings.Join(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged, "\n"), "Other") {
		t.Fatal("unsubscribe of a new mailbox subscribed it")
	}
	cl.cmd("UNSUBSCRIBE Fresh").ok(t)
	cl.cmd("UNSUBSCRIBE Fresh").ok(t)
	if got := listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("after unsubscribe %v", got)
	}

	cl.cmd("SUBSCRIBE Other").ok(t)
	cl.cmd("DELETE Other").ok(t)
	if strings.Contains(strings.Join(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged, "\n"), "Other") {
		t.Fatal("delete left the subscription")
	}

	cl.cmd("CREATE Parent").ok(t)
	cl.cmd("CREATE Parent/Kid").ok(t)
	cl.cmd("SUBSCRIBE Parent").ok(t)
	cl.cmd("SUBSCRIBE Parent/Kid").ok(t)
	cl.cmd("RENAME Parent Renamed").ok(t)
	renamed := listNames(cl.cmd(`LIST (SUBSCRIBED) "" "*"`).mustOK(t).untagged)
	if !reflect.DeepEqual(renamed, []string{"INBOX", "Renamed", "Renamed/Kid"}) {
		t.Fatalf("rename %v", renamed)
	}
	if got := listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, renamed) {
		t.Fatalf("lsub after rename %v", got)
	}

	word := "qsubscribex"
	raw := "From: a@example.com\r\nTo: b@example.com\r\nSubject: plain\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\n" + word + "\r\n"
	appendTo := func(name string) {
		t.Helper()
		cl.cmd(fmt.Sprintf("APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n%s", name, len(raw), raw)).ok(t)
	}
	cl.cmd("CREATE Unsub").ok(t)
	appendTo("Renamed")
	appendTo("Unsub")
	uv := map[string]uint32{
		"INBOX":   statusUIDValidity(t, cl, "INBOX"),
		"Renamed": statusUIDValidity(t, cl, "Renamed"),
		"Unsub":   statusUIDValidity(t, cl, "Unsub"),
	}
	tag, lines := multiLines(t, cl.cmd("ESEARCH IN (subscribed) RETURN (ALL) TEXT "+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, []string{"Renamed"}, uv, "ALL", map[string]string{"Renamed": "1"}), "\n") {
		t.Fatalf("subscribed search\n%s", strings.Join(lines, "\n"))
	}
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (subscribed) RETURN (COUNT) ALL"))
	// Renamed/Kid is subscribed and has no hits, so it produces no line.
	wantCount := multiWant(tag, []string{"INBOX", "Renamed"}, uv, "COUNT", map[string]string{
		"INBOX":   "2",
		"Renamed": "1",
	})
	if strings.Join(lines, "\n") != strings.Join(wantCount, "\n") {
		t.Fatalf("subscribed count\n%s", strings.Join(lines, "\n"))
	}
	cl.cmd("UNSUBSCRIBE INBOX").ok(t)
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (subscribed) RETURN (COUNT) ALL"))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, []string{"Renamed"}, uv, "COUNT", map[string]string{"Renamed": "1"}), "\n") {
		t.Fatalf("after unsub inbox\n%s", strings.Join(lines, "\n"))
	}
	cl.cmd("SUBSCRIBE Unsub").ok(t)
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (subscribed) RETURN (ALL) TEXT "+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, []string{"Renamed", "Unsub"}, uv, "ALL", map[string]string{"Renamed": "1", "Unsub": "1"}), "\n") {
		t.Fatalf("two boxes\n%s", strings.Join(lines, "\n"))
	}

	if !cc.call(map[string]any{"op": "accounts", "username": "ada", "password": "secret"}).ok(t).OK {
		t.Fatal("add account")
	}
	ada := dial(t, bound.Listen)
	ada.cmd("LOGIN ada secret").ok(t)
	if got := listNames(ada.cmd(`LIST (SUBSCRIBED) "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("ada initial %v", got)
	}
	ada.cmd("CREATE Renamed").ok(t)
	ada.cmd("SUBSCRIBE Renamed").ok(t)
	if got := listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged); reflect.DeepEqual(got, []string{"INBOX"}) || !containsName(got, "Renamed") {
		t.Fatalf("demo set changed by ada %v", got)
	}
	ada.cmd("UNSUBSCRIBE INBOX").ok(t)
	if got := listNames(ada.cmd(`LSUB "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"Renamed"}) {
		t.Fatalf("ada set %v", got)
	}
	if !containsName(listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged), "Unsub") {
		t.Fatal("demo lost Unsub")
	}

	cl.cmd("LOGOUT").ok(t)
	ada.cmd("LOGOUT").ok(t)
	cc.call(map[string]any{"op": "reset"}).ok(t)
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if got := listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("after reset %v", got)
	}
	ada = dial(t, bound.Listen)
	ada.cmd("LOGIN ada secret").no(t)

	cl.cmd("CREATE Zbox").ok(t)
	cl.cmd("SUBSCRIBE Zbox").ok(t)
	cl.cmd("LOGOUT").ok(t)
	cc.call(map[string]any{"op": "set-dataset", "path": "datasets/demo"}).ok(t)
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if got := listNames(cl.cmd(`LIST (SUBSCRIBED) "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("after set-dataset %v", got)
	}
	if len(cl.cmd(`LIST "" "Zbox"`).mustOK(t).untagged) != 0 {
		t.Fatal("set-dataset kept Zbox")
	}
}

func TestSubscribeWithoutListExtended(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if cl.cmd(`LIST (SUBSCRIBED) "" "*"`).status != "BAD" {
		t.Fatal("SUBSCRIBED was accepted")
	}
	plain := cl.cmd(`LIST "" "*"`).mustOK(t)
	if strings.Contains(strings.Join(plain.untagged, "\n"), `\Subscribed`) {
		t.Fatal("plain list has \\Subscribed")
	}
	if got := listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX"}) {
		t.Fatalf("lsub %v", got)
	}
	cl.cmd("CREATE Fresh").ok(t)
	cl.cmd("SUBSCRIBE Fresh").ok(t)
	if got := listNames(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged); !reflect.DeepEqual(got, []string{"INBOX", "Fresh"}) {
		t.Fatalf("lsub %v", got)
	}
	if strings.Contains(strings.Join(cl.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n"), `\Subscribed`) {
		t.Fatal("subscribe changed plain LIST")
	}
}

func withSubscribed(line string) string {
	i := strings.Index(line, "(")
	j := strings.Index(line, ")")
	if i < 0 || j < i {
		return line
	}
	return line[:j] + ` \Subscribed` + line[j:]
}

func listNames(lines []string) []string {
	var out []string
	for _, ln := range lines {
		if strings.HasPrefix(ln, "* STATUS ") {
			continue
		}
		fs := strings.Fields(ln)
		if len(fs) == 0 {
			continue
		}
		out = append(out, strings.Trim(fs[len(fs)-1], `"`))
	}
	return out
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
