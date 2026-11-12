package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestXList(t *testing.T) {
	if !capImplemented("XLIST") {
		t.Fatal("XLIST is not implemented")
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-25.json"})
	early := dial(t, bound.Listen)
	if strings.Contains(early.greeting, "XLIST") {
		t.Fatal("greeting lists XLIST")
	}
	if early.cmd(`XLIST "" "*"`).status != "NO" {
		t.Fatal("XLIST before login was not NO")
	}
	early.cmd("LOGIN demo secret").ok(t)
	if early.cmd("SELECT INBOX").status != "OK" {
		t.Fatal("select")
	}
	// No selected mailbox is required: a second connection never selects.
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "XLIST") {
		t.Fatal("post-login missing XLIST")
	}
	sameLines := func(listCmd, xlistCmd string) {
		t.Helper()
		list := cl.cmd(listCmd).mustOK(t)
		xlist := cl.cmd(xlistCmd).mustOK(t)
		if strings.Join(xlist.untagged, "\n") != swapWord(strings.Join(list.untagged, "\n"), "LIST", "XLIST") {
			t.Fatalf("%s\nlist %q\nxlist %q", listCmd, list.untagged, xlist.untagged)
		}
		for _, ln := range xlist.untagged {
			if strings.Contains(ln, "* LIST ") {
				t.Fatalf("XLIST line used LIST: %s", ln)
			}
		}
	}
	sameLines(`LIST "" "*"`, `XLIST "" "*"`)
	sameLines(`LIST "INBOX/" ""`, `XLIST "INBOX/" ""`)
	sameLines(`LIST "" INBOX`, `XLIST "" INBOX`)
	cl.cmd(`CREATE "Odd Name"`).ok(t)
	sameLines(`LIST "" "Odd*"`, `XLIST "" "Odd*"`)
	cl.cmd(`CREATE SentBox (USE (\Sent))`).ok(t)
	sent := cl.cmd(`LIST "" SentBox`).mustOK(t)
	if len(sent.untagged) != 1 || !strings.Contains(sent.untagged[0], `\Sent`) {
		t.Fatalf("LIST special-use %q", sent.untagged)
	}
	sameLines(`LIST "" SentBox`, `XLIST "" SentBox`)

	for _, bad := range []string{
		"XLIST",
		`XLIST ""`,
		`XLIST "" "*" extra`,
		`XLIST (SUBSCRIBED) "" "*"`,
		`XLIST "" "*" RETURN (SUBSCRIBED)`,
		`XLIST RETURN (CHILDREN) "" "*"`,
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was not BAD", bad)
		}
	}

	dir := t.TempDir()
	raw := []byte("{\n  \"accounts\": [{\"username\": \"demo\", \"password\": \"secret\"}],\n  \"mailboxes\": [{\"name\": \"INBOX\", \"special_use\": [\"\\\\Trash\"]}]\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "dataset.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	_, offBound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-24.json", Dataset: dir})
	off := dial(t, offBound.Listen)
	off.cmd("LOGIN demo secret").ok(t)
	plain := off.cmd(`LIST "" INBOX`).mustOK(t)
	if strings.Contains(strings.Join(plain.untagged, "\n"), `\Trash`) {
		t.Fatal("LIST included special-use without SPECIAL-USE")
	}
	got := off.cmd(`XLIST "" INBOX`).mustOK(t)
	if strings.Join(got.untagged, "\n") != swapWord(strings.Join(plain.untagged, "\n"), "LIST", "XLIST") {
		t.Fatalf("XLIST flags %q list %q", got.untagged, plain.untagged)
	}
	_, onBound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-25.json", Dataset: dir})
	on := dial(t, onBound.Listen)
	on.cmd("LOGIN demo secret").ok(t)
	onList := on.cmd(`LIST "" INBOX`).mustOK(t)
	if !strings.Contains(strings.Join(onList.untagged, "\n"), `\Trash`) {
		t.Fatalf("LIST omitted special-use %q", onList.untagged)
	}
	onX := on.cmd(`XLIST "" INBOX`).mustOK(t)
	if strings.Join(onX.untagged, "\n") != swapWord(strings.Join(onList.untagged, "\n"), "LIST", "XLIST") {
		t.Fatalf("XLIST special-use %q list %q", onX.untagged, onList.untagged)
	}

	_, bareBound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-22.json"})
	bare := dial(t, bareBound.Listen)
	if bare.cmd(`XLIST "" "*"`).status != "NO" {
		t.Fatal("unlisted XLIST before login was not NO")
	}
	bare.cmd("LOGIN demo secret").ok(t)
	if bare.cmd(`XLIST "" "*"`).status != "BAD" {
		t.Fatal("unlisted XLIST was not BAD")
	}
	uid := dial(t, bound.Listen)
	uid.cmd("LOGIN demo secret").ok(t)
	if uid.cmd(`UID XLIST "" "*"`).status != "BAD" {
		t.Fatal("UID XLIST was not BAD")
	}
}

func TestXMove(t *testing.T) {
	if !capImplemented("XMOVE") {
		t.Fatal("XMOVE is not implemented")
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-25.json"})
	early := dial(t, bound.Listen)
	if strings.Contains(early.greeting, "XMOVE") {
		t.Fatal("greeting lists XMOVE")
	}
	if early.cmd("XMOVE 1 INBOX").status != "NO" {
		t.Fatal("XMOVE before login was not NO")
	}
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if !listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "XMOVE") {
		t.Fatal("post-login missing XMOVE")
	}
	nosel := cl.cmd("XMOVE 1 Dest")
	noselMove := cl.cmd("MOVE 1 Dest")
	if nosel.status != "NO" || noselMove.status != "NO" || swapWord(nosel.text, "XMOVE", "MOVE") != noselMove.text {
		t.Fatalf("not selected XMOVE %q MOVE %q", nosel.tagLine, noselMove.tagLine)
	}
	cl.cmd("SELECT INBOX").ok(t)
	for _, bad := range []string{"XMOVE", "XMOVE 1", "XMOVE 1 Dest extra"} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was not BAD", bad)
		}
	}
	missing := cl.cmd("XMOVE 1 NoSuchBox")
	missingMove := cl.cmd("MOVE 1 NoSuchBox")
	if missing.status != "NO" || !strings.Contains(missing.text, "[TRYCREATE]") || swapWord(missing.text, "XMOVE", "MOVE") != missingMove.text {
		t.Fatalf("missing XMOVE %q MOVE %q", missing.tagLine, missingMove.tagLine)
	}
	cl.cmd("EXAMINE INBOX").ok(t)
	ro := cl.cmd("XMOVE 1 NoSuchBox")
	roMove := cl.cmd("MOVE 1 NoSuchBox")
	if ro.status != "NO" || !strings.Contains(ro.text, "read-only") || swapWord(ro.text, "XMOVE", "MOVE") != roMove.text {
		t.Fatalf("read-only XMOVE %q MOVE %q", ro.tagLine, roMove.tagLine)
	}

	moveOnce := func(p *Personality, cmdName string) (untagged, text string) {
		t.Helper()
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "p.json")
		if err := os.WriteFile(path, b, 0644); err != nil {
			t.Fatal(err)
		}
		_, bnd := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
		c := dial(t, bnd.Listen)
		c.cmd("LOGIN demo secret").ok(t)
		c.cmd("CREATE Dest").ok(t)
		c.cmd("SELECT INBOX").ok(t)
		uid := fetchUID(t, c.cmd("FETCH 1 (UID)"))
		rp := c.cmd(cmdName + " 1 Dest").mustOK(t)
		if strings.Contains(strings.Join(searchIDs(t, c.cmd("UID SEARCH ALL")), " "), strconv.Itoa(int(uid))) {
			t.Fatal("moved uid still in source")
		}
		c.cmd("SELECT Dest").ok(t)
		if len(searchIDs(t, c.cmd("UID SEARCH ALL"))) != 1 {
			t.Fatal("destination missing the message")
		}
		return strings.Join(rp.untagged, "\n"), rp.text
	}
	base, err := loadPersonality("personalities/cyrus-25.json")
	if err != nil {
		t.Fatal(err)
	}
	xu, xt := moveOnce(base, "XMOVE")
	mu, mt := moveOnce(base, "MOVE")
	if !strings.Contains(xt, "XMOVE") || strings.Contains(swapWord(xt, "XMOVE", ""), "MOVE") {
		t.Fatalf("COPYUID text %q", xt)
	}
	if swapWord(xu, "XMOVE", "MOVE") != mu || swapWord(xt, "XMOVE", "MOVE") != mt {
		t.Fatalf("XMOVE lines %q %q\nMOVE lines %q %q", xu, xt, mu, mt)
	}
	base.MoveCopyUIDUntagged = true
	xu, xt = moveOnce(base, "XMOVE")
	mu, mt = moveOnce(base, "MOVE")
	if !strings.Contains(xu, "[COPYUID ") || !strings.Contains(xu, "XMOVE") || strings.Contains(swapWord(xu, "XMOVE", ""), "MOVE") {
		t.Fatalf("untagged COPYUID %q", xu)
	}
	if swapWord(xu, "XMOVE", "MOVE") != mu || swapWord(xt, "XMOVE", "MOVE") != mt {
		t.Fatalf("untagged XMOVE %q %q\nMOVE %q %q", xu, xt, mu, mt)
	}

	uidP, err := loadPersonality("personalities/cyrus-25.json")
	if err != nil {
		t.Fatal(err)
	}
	_, uidText := moveOnce(uidP, "UID XMOVE")
	if !strings.Contains(uidText, "XMOVE") {
		t.Fatalf("UID XMOVE text %q", uidText)
	}

	bareP, err := loadPersonality("personalities/cyrus-25.json")
	if err != nil {
		t.Fatal(err)
	}
	bareP.PreLogin = dropCap(bareP.PreLogin, "MOVE")
	bareP.PostLogin = dropCap(bareP.PostLogin, "MOVE")
	b, err := json.Marshal(bareP)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nomove.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, noMoveBound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	noMove := dial(t, noMoveBound.Listen)
	noMove.cmd("LOGIN demo secret").ok(t)
	post := capsOf(noMove.cmd("CAPABILITY").mustOK(t).untagged[0])
	if listed(post, "MOVE") || !listed(post, "XMOVE") {
		t.Fatalf("caps %v", post)
	}
	noMove.cmd("CREATE Dest").ok(t)
	noMove.cmd("SELECT INBOX").ok(t)
	noMove.cmd("XMOVE 1 Dest").mustOK(t)

	_, oldBound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-22.json"})
	old := dial(t, oldBound.Listen)
	if old.cmd("XMOVE 1 INBOX").status != "NO" {
		t.Fatal("unlisted XMOVE before login was not NO")
	}
	old.cmd("LOGIN demo secret").ok(t)
	if old.cmd("XMOVE 1 INBOX").status != "BAD" {
		t.Fatal("unlisted XMOVE was not BAD")
	}
}

func TestXListXMoveAdvertised(t *testing.T) {
	type row struct {
		file     string
		xlist    bool
		xmove    bool
		prePrev  string
		preNext  string
		postPrev string
		postNext string
	}
	for _, c := range []row{
		{"personalities/cyrus-24.json", true, false, "SCAN", "SASL-IR", "SCAN", "COMPRESS=DEFLATE"},
		{"personalities/cyrus-25.json", true, true, "SCAN", "XMOVE", "SCAN", "XMOVE"},
		{"personalities/cyrus-36.json", true, true, "THREAD=REFS", "XMOVE", "THREAD=REFS", "XMOVE"},
		{"personalities/cyrus-3124.json", true, false, "THREAD=REFS", "AUTH=PLAIN", "THREAD=REFS", ""},
		{"personalities/cyrus-3137.json", true, false, "THREAD=REFS", "AUTH=PLAIN", "THREAD=REFS", ""},
		{"personalities/zimbra-883.json", true, false, "WITHIN", "AUTH=PLAIN", "WITHIN", ""},
	} {
		p, err := loadPersonality(c.file)
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "XLIST") || listed(p.GreetingCaps, "XMOVE") {
			t.Fatalf("%s greeting lists a token", c.file)
		}
		if capAt(p.PreLogin, "XLIST") != capAt(p.PreLogin, c.prePrev)+1 {
			t.Fatalf("%s pre-login XLIST not after %s", c.file, c.prePrev)
		}
		if c.preNext == "" {
			t.Fatalf("%s pre-login XLIST has no following token", c.file)
		}
		if capAt(p.PreLogin, c.preNext) != capAt(p.PreLogin, "XLIST")+1 {
			t.Fatalf("%s pre-login %s not after XLIST", c.file, c.preNext)
		}
		if c.postNext == "" {
			if capAt(p.PostLogin, "XLIST") != len(p.PostLogin)-1 || capAt(p.PostLogin, c.postPrev) != len(p.PostLogin)-2 {
				t.Fatalf("%s post-login XLIST not at end after %s", c.file, c.postPrev)
			}
		} else if capAt(p.PostLogin, "XLIST") != capAt(p.PostLogin, c.postPrev)+1 || capAt(p.PostLogin, c.postNext) != capAt(p.PostLogin, "XLIST")+1 {
			t.Fatalf("%s post-login XLIST not between %s and %s", c.file, c.postPrev, c.postNext)
		}
		if c.xmove {
			if capAt(p.PreLogin, "XMOVE") != capAt(p.PreLogin, "XLIST")+1 || capAt(p.PostLogin, "XMOVE") != capAt(p.PostLogin, "XLIST")+1 {
				t.Fatalf("%s XMOVE not after XLIST", c.file)
			}
		} else if listed(p.PreLogin, "XMOVE") || listed(p.PostLogin, "XMOVE") {
			t.Fatalf("%s lists XMOVE", c.file)
		}
		if !c.xlist {
			t.Fatalf("%s missing XLIST", c.file)
		}
	}
	sasl, err := loadPersonality("personalities/sasl.json")
	if err != nil {
		t.Fatal(err)
	}
	if listed(sasl.GreetingCaps, "XLIST") || listed(sasl.PreLogin, "XLIST") || !listed(sasl.PostLogin, "XLIST") {
		t.Fatal("sasl XLIST is not post-login only")
	}
	if listed(sasl.GreetingCaps, "XMOVE") || listed(sasl.PreLogin, "XMOVE") || listed(sasl.PostLogin, "XMOVE") {
		t.Fatal("sasl lists XMOVE")
	}
	cyrus, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	if !sameCapSet(dropCap(cyrus.PostLogin, "SEARCH=FUZZY"), sasl.PostLogin) {
		t.Fatal("sasl post-login set drifted from cyrus-3137")
	}
	allowXList := map[string]bool{
		"cyrus-24.json": true, "cyrus-25.json": true, "cyrus-36.json": true,
		"cyrus-3124.json": true, "cyrus-3137.json": true, "zimbra-883.json": true, "sasl.json": true,
	}
	allowXMove := map[string]bool{"cyrus-25.json": true, "cyrus-36.json": true}
	matches, err := filepath.Glob("personalities/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range matches {
		base := filepath.Base(f)
		if strings.HasSuffix(base, ".gaps.json") {
			continue
		}
		p, err := loadPersonality(f)
		if err != nil {
			t.Fatal(err)
		}
		if !allowXList[base] && (listed(p.GreetingCaps, "XLIST") || listed(p.PreLogin, "XLIST") || listed(p.PostLogin, "XLIST")) {
			t.Fatalf("%s gained XLIST", base)
		}
		if !allowXMove[base] && (listed(p.GreetingCaps, "XMOVE") || listed(p.PreLogin, "XMOVE") || listed(p.PostLogin, "XMOVE")) {
			t.Fatalf("%s gained XMOVE", base)
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), `"XLIST"`) || strings.Contains(string(gaps), `"XMOVE"`) {
		t.Fatal("gaps lists XLIST or XMOVE")
	}
	for _, tok := range []string{"ACL", "QUOTA", "URLAUTH=BINARY", "IMAP4rev2"} {
		if !strings.Contains(string(gaps), `"`+tok+`"`) {
			t.Fatalf("gaps dropped %s", tok)
		}
	}
}

func swapWord(s, from, to string) string {
	return strings.ReplaceAll(s, from, to)
}
