package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEsearchReturn(t *testing.T) {
	p, err := loadPersonality("personalities/esearch.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"ESEARCH", "PARTIAL"} {
		if !listed(p.PreLogin, c) || !listed(p.PostLogin, c) {
			t.Fatalf("esearch.json missing %s", c)
		}
	}

	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/esearch.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"ESEARCH", "PARTIAL"})

	cl.cmd("SELECT INBOX").ok(t)
	plain := cl.cmd("SEARCH ALL")
	if strings.Join(searchIDs(t, plain), " ") != "1 2" || len(plain.untagged) != 1 || !strings.HasPrefix(plain.untagged[0], "* SEARCH") {
		t.Fatalf("plain search %+v", plain)
	}
	uidPlain := cl.cmd("UID SEARCH ALL")
	if strings.Join(searchIDs(t, uidPlain), " ") != "1 2" || !strings.HasPrefix(uidPlain.untagged[0], "* SEARCH") {
		t.Fatalf("uid search %+v", uidPlain)
	}

	seq := cl.cmd("SEARCH RETURN (MIN MAX COUNT ALL) ALL")
	tag, line := oneEsearch(t, seq)
	if line != `* ESEARCH (TAG "`+tag+`") COUNT 2 MIN 1 MAX 2 ALL 1:2` {
		t.Fatalf("seq esearch %q", line)
	}
	uid := cl.cmd("UID SEARCH RETURN (MIN MAX COUNT ALL) ALL")
	tag, line = oneEsearch(t, uid)
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 2 MIN 1 MAX 2 ALL 1:2` {
		t.Fatalf("uid esearch %q", line)
	}
	empty := cl.cmd(`SEARCH RETURN (MIN MAX COUNT ALL) FROM nosuch@no.example`)
	tag, line = oneEsearch(t, empty)
	if line != `* ESEARCH (TAG "`+tag+`") COUNT 0` {
		t.Fatalf("empty esearch %q", line)
	}
	swapped := cl.cmd("UID SEARCH RETURN (PARTIAL 2:1 COUNT) ALL")
	tag, line = oneEsearch(t, swapped)
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 2 PARTIAL (2:1 1:2)` {
		t.Fatalf("swapped %q", line)
	}
	last := cl.cmd("UID SEARCH RETURN (PARTIAL -1:-1 COUNT) ALL")
	tag, line = oneEsearch(t, last)
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 2 PARTIAL (-1:-1 2)` {
		t.Fatalf("last %q", line)
	}
	over := cl.cmd("UID SEARCH RETURN (PARTIAL -3:-1 COUNT) ALL")
	tag, line = oneEsearch(t, over)
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 2 PARTIAL (-3:-1 1:2)` {
		t.Fatalf("overlap %q", line)
	}
	none := cl.cmd("UID SEARCH RETURN (PARTIAL -5:-4 COUNT) ALL")
	tag, line = oneEsearch(t, none)
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 2 PARTIAL (-5:-4 NIL)` {
		t.Fatalf("negative miss %q", line)
	}

	cl.cmd("SELECT INBOX/Generated").ok(t)
	part := cl.cmd("UID SEARCH RETURN (PARTIAL 1:2 COUNT) ALL")
	tag, line = oneEsearch(t, part)
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 3 PARTIAL (1:2 1:2)` {
		t.Fatalf("partial %q", line)
	}
	past := cl.cmd("UID SEARCH RETURN (PARTIAL 4:9 COUNT) ALL")
	tag, line = oneEsearch(t, past)
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 3 PARTIAL (4:9 NIL)` {
		t.Fatalf("past end %q", line)
	}
	tail := cl.cmd("UID SEARCH RETURN (PARTIAL -1:-2 COUNT) ALL")
	tag, line = oneEsearch(t, tail)
	if line != `* ESEARCH (TAG "`+tag+`") UID COUNT 3 PARTIAL (-1:-2 2:3)` {
		t.Fatalf("tail %q", line)
	}
	seqPart := cl.cmd("SEARCH RETURN (PARTIAL 1:1 COUNT) ALL")
	tag, line = oneEsearch(t, seqPart)
	if line != `* ESEARCH (TAG "`+tag+`") COUNT 3 PARTIAL (1:1 1)` {
		t.Fatalf("seq partial %q", line)
	}
	bare := cl.cmd("SEARCH RETURN () ALL")
	tag, line = oneEsearch(t, bare)
	if line != `* ESEARCH (TAG "`+tag+`") ALL 1:3` {
		t.Fatalf("empty return %q", line)
	}
}

func TestEsearchRequiresCaps(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	if cl.cmd("SEARCH RETURN (MIN MAX COUNT ALL) ALL").status != "BAD" {
		t.Fatal("RETURN was accepted")
	}
	if cl.cmd("UID SEARCH RETURN (PARTIAL 1:1 COUNT) ALL").status != "BAD" {
		t.Fatal("PARTIAL was accepted")
	}
	plain := cl.cmd("SEARCH ALL")
	if strings.Join(searchIDs(t, plain), " ") != "1 2" {
		t.Fatalf("plain %+v", plain)
	}
	uid := cl.cmd("UID SEARCH ALL")
	if strings.Join(searchIDs(t, uid), " ") != "1 2" {
		t.Fatalf("uid %+v", uid)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "esearch-only.json")
	p, err := loadPersonality("personalities/esearch.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PreLogin = dropCap(p.PreLogin, "PARTIAL")
	p.PostLogin = dropCap(p.PostLogin, "PARTIAL")
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
	if cl.cmd("UID SEARCH RETURN (PARTIAL 1:1 COUNT) ALL").status != "BAD" {
		t.Fatal("PARTIAL without the capability was accepted")
	}
	min := cl.cmd("SEARCH RETURN (MIN) ALL")
	tag, line := oneEsearch(t, min)
	if line != `* ESEARCH (TAG "`+tag+`") MIN 1` {
		t.Fatalf("min only %q", line)
	}
}

func oneEsearch(t *testing.T, rp *reply) (string, string) {
	t.Helper()
	if rp.status != "OK" || len(rp.untagged) != 1 || !strings.HasPrefix(rp.untagged[0], "* ESEARCH ") {
		t.Fatalf("esearch reply %+v", rp)
	}
	tag := strings.SplitN(rp.tagLine, " ", 2)[0]
	return tag, rp.untagged[0]
}

func dropCap(caps []string, name string) []string {
	var out []string
	for _, c := range caps {
		if !strings.EqualFold(c, name) {
			out = append(out, c)
		}
	}
	return out
}
