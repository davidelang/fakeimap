package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCondstoreAndQResync(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	post := capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])
	requireCaps(t, post, []string{"CONDSTORE", "QRESYNC"})

	cl.cmd("CREATE csbox").ok(t)
	sel := cl.cmd("SELECT csbox").mustOK(t)
	if !strings.Contains(strings.Join(sel.untagged, "\n"), "[HIGHESTMODSEQ ") {
		t.Fatalf("select missing HIGHESTMODSEQ: %v", sel.untagged)
	}
	if got := highestModseq(sel); got != 0 {
		t.Fatalf("empty highest %d", got)
	}
	ex := cl.cmd("EXAMINE csbox").mustOK(t)
	if !strings.Contains(strings.Join(ex.untagged, "\n"), "[HIGHESTMODSEQ ") {
		t.Fatalf("examine missing HIGHESTMODSEQ: %v", ex.untagged)
	}
	cl.cmd("SELECT csbox").ok(t)
	msg := "From: a@b\r\n\r\nbody\r\n"
	cl.cmd("APPEND csbox {" + strconv.Itoa(len(msg)) + "+}\r\n" + msg).ok(t)
	cl.cmd("APPEND csbox {" + strconv.Itoa(len(msg)) + "+}\r\n" + msg).ok(t)
	again := cl.cmd("SELECT csbox").mustOK(t)
	if got := highestModseq(again); got != 2 {
		t.Fatalf("highest after append %d", got)
	}
	one := cl.cmd("FETCH 1 (MODSEQ)").mustOK(t)
	if !strings.Contains(fetchLine(t, one), "MODSEQ (1)") {
		t.Fatalf("modseq %s", fetchLine(t, one))
	}
	two := cl.cmd("FETCH 1:* (FLAGS) (CHANGEDSINCE 1)").mustOK(t)
	if n := fetchCount(two); n != 1 || !strings.Contains(fetchLine(t, two), "MODSEQ (2)") {
		t.Fatalf("changedsince %v", two.untagged)
	}
	below := cl.cmd(`STORE 1 (UNCHANGEDSINCE 0) +FLAGS (\Flagged)`)
	below.ok(t)
	if !strings.Contains(below.tagLine, "[MODIFIED 1]") {
		t.Fatalf("modified %s", below.tagLine)
	}
	if strings.Contains(strings.Join(below.untagged, "\n"), "FETCH") {
		t.Fatalf("modified store changed flags: %v", below.untagged)
	}
	same := cl.cmd(`STORE 1 (UNCHANGEDSINCE 1) +FLAGS (\Flagged)`)
	same.ok(t)
	if strings.Contains(same.tagLine, "MODIFIED") {
		t.Fatalf("equal modseq was modified: %s", same.tagLine)
	}
	if !strings.Contains(fetchLine(t, same), `\Flagged`) || !strings.Contains(fetchLine(t, same), "MODSEQ (3)") {
		t.Fatalf("store result %s", fetchLine(t, same))
	}
	bumped := cl.cmd("SELECT csbox").mustOK(t)
	if got := highestModseq(bumped); got != 3 {
		t.Fatalf("highest after flag change %d", got)
	}
	flags := cl.cmd("FETCH 1 (FLAGS)").mustOK(t)
	if strings.Contains(fetchLine(t, flags), `\Seen`) {
		t.Fatalf("rejected store set seen: %s", fetchLine(t, flags))
	}

	other := dial(t, bound.Listen)
	other.cmd("LOGIN demo secret").ok(t)
	en := other.cmd("ENABLE CONDSTORE")
	en.ok(t)
	if strings.Join(en.untagged, " ") != "* ENABLED CONDSTORE" {
		t.Fatalf("enable %q", en.untagged)
	}
	other.cmd("SELECT csbox").ok(t)
	cl.cmd(`STORE 2 +FLAGS (\Answered)`).ok(t)
	noop := other.cmd("NOOP").mustOK(t)
	if n := fetchCount(noop); n != 1 {
		t.Fatalf("unsolicited count %d %v", n, noop.untagged)
	}
	line := fetchLine(t, noop)
	if !strings.Contains(line, "FLAGS ") || !strings.Contains(line, "MODSEQ (") {
		t.Fatalf("unsolicited %s", line)
	}

	fresh := dial(t, bound.Listen)
	fresh.cmd("LOGIN demo secret").ok(t)
	both := fresh.cmd("ENABLE QRESYNC")
	both.ok(t)
	if strings.Join(both.untagged, " ") != "* ENABLED CONDSTORE QRESYNC" {
		t.Fatalf("qresync enable %q", both.untagged)
	}
	fresh.cmd("SELECT csbox").ok(t)
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).ok(t)
	expu := fresh.cmd("EXPUNGE")
	expu.ok(t)
	joined := strings.Join(expu.untagged, "\n")
	if !strings.Contains(joined, "* VANISHED ") || strings.Contains(joined, "EXPUNGE") {
		t.Fatalf("qresync expunge %v", expu.untagged)
	}
	if !strings.Contains(expu.tagLine, "[HIGHESTMODSEQ ") {
		t.Fatalf("expunge tag %s", expu.tagLine)
	}
	seen := other.cmd("NOOP").mustOK(t)
	sjoin := strings.Join(seen.untagged, "\n")
	if !strings.Contains(sjoin, "EXPUNGE") || strings.Contains(sjoin, "VANISHED") {
		t.Fatalf("condstore expunge report %v", seen.untagged)
	}

	uidv := bracketNum(strings.Join(bumped.untagged, "\n"), "UIDVALIDITY")
	bad := other.cmd("SELECT csbox (QRESYNC (" + strconv.FormatUint(uint64(uidv), 10) + " 0 1:2))")
	if bad.status != "BAD" {
		t.Fatalf("qresync without enable %s %v", bad.tagLine, bad.untagged)
	}
	sync := fresh.cmd("SELECT csbox (QRESYNC (" + strconv.FormatUint(uint64(uidv), 10) + " 0 1:2))")
	sync.ok(t)
	sjoined := strings.Join(sync.untagged, "\n")
	if !strings.Contains(sjoined, "VANISHED (EARLIER)") || !strings.Contains(sjoined, "[HIGHESTMODSEQ ") {
		t.Fatalf("resync %v", sync.untagged)
	}
}

func TestCondstoreNotAdvertised(t *testing.T) {
	p, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PreLogin = stripCaps(p.PreLogin)
	p.PostLogin = stripCaps(p.PostLogin)
	p.GreetingCaps = stripCaps(p.GreetingCaps)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "plain.json")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	for _, c := range capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]) {
		if strings.EqualFold(c, "CONDSTORE") || strings.EqualFold(c, "QRESYNC") {
			t.Fatalf("advertised %s", c)
		}
	}
	sel := cl.cmd("SELECT INBOX").mustOK(t)
	if strings.Contains(strings.Join(sel.untagged, "\n"), "HIGHESTMODSEQ") {
		t.Fatalf("unexpected highest %v", sel.untagged)
	}
	if cl.cmd("FETCH 1 (MODSEQ)").status != "BAD" {
		t.Fatal("MODSEQ was accepted")
	}
	if cl.cmd("FETCH 1 (FLAGS) (CHANGEDSINCE 0)").status != "BAD" {
		t.Fatal("CHANGEDSINCE was accepted")
	}
	if cl.cmd(`STORE 1 (UNCHANGEDSINCE 0) +FLAGS (\Flagged)`).status != "BAD" {
		t.Fatal("UNCHANGEDSINCE was accepted")
	}
	if cl.cmd("SELECT INBOX (QRESYNC (1 0 1:2))").status != "BAD" {
		t.Fatal("QRESYNC select was accepted")
	}
	en := cl.cmd("ENABLE CONDSTORE QRESYNC")
	en.ok(t)
	for _, u := range en.untagged {
		if strings.Contains(strings.ToUpper(u), "ENABLED") {
			t.Fatalf("enabled %q", u)
		}
	}
	cl.cmd("NOOP").ok(t)
}

func stripCaps(in []string) []string {
	var out []string
	for _, c := range in {
		if strings.EqualFold(c, "CONDSTORE") || strings.EqualFold(c, "QRESYNC") {
			continue
		}
		out = append(out, c)
	}
	return out
}

func highestModseq(rp *reply) uint64 {
	for _, u := range rp.untagged {
		if strings.Contains(u, "[HIGHESTMODSEQ ") {
			s := strings.Split(u, "[HIGHESTMODSEQ ")[1]
			s = strings.TrimRight(strings.Fields(s)[0], "]")
			n, _ := strconv.ParseUint(s, 10, 64)
			return n
		}
	}
	return 0
}

func fetchCount(rp *reply) int {
	n := 0
	for _, u := range rp.untagged {
		if strings.Contains(u, " FETCH (") {
			n++
		}
	}
	return n
}
