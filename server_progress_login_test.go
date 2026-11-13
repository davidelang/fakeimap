package main

import (
	"bufio"
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInProgressAndLoginDisabledLists(t *testing.T) {
	if _, ok := implementedCaps["INPROGRESS"]; !ok || !capImplemented("INPROGRESS") {
		t.Fatal("INPROGRESS is not implemented")
	}
	if _, ok := implementedCaps["LOGINDISABLED"]; !ok || !capImplemented("LOGINDISABLED") {
		t.Fatal("LOGINDISABLED is not implemented")
	}
	inprogress := map[string]string{
		"cyrus-3124.json":  "both",
		"cyrus-3137.json":  "both",
		"dovecot-245.json": "post",
		"dovecot-dev.json": "post",
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".gaps.json") {
			continue
		}
		p, err := loadPersonality(filepath.Join("personalities", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		where := inprogress[e.Name()]
		gotBoth := listed(p.PreLogin, "INPROGRESS") || listed(p.PostLogin, "INPROGRESS")
		if where == "" {
			if gotBoth || listed(p.GreetingCaps, "INPROGRESS") {
				t.Fatalf("%s gained INPROGRESS", e.Name())
			}
		} else if listed(p.GreetingCaps, "INPROGRESS") {
			t.Fatalf("%s greeting lists INPROGRESS", e.Name())
		}
		switch where {
		case "both":
			if !betweenCaps(p.PreLogin, "INPROGRESS", "IDLE", "LIST-EXTENDED") || !betweenCaps(p.PostLogin, "INPROGRESS", "IDLE", "LIST-EXTENDED") {
				t.Fatalf("%s INPROGRESS placement pre %v post %v", e.Name(), p.PreLogin, p.PostLogin)
			}
		case "post":
			if listed(p.PreLogin, "INPROGRESS") {
				t.Fatalf("%s lists INPROGRESS before login", e.Name())
			}
			prev := "NOTIFY"
			if !listed(p.PostLogin, "NOTIFY") {
				prev = "LITERAL+"
			}
			if capNext(p.PostLogin, "INPROGRESS") != prev && !(prev == "LITERAL+" && capNext(p.PostLogin, "INPROGRESS") == "LITERAL+") {
				t.Fatalf("%s post INPROGRESS before %s: %v", e.Name(), prev, p.PostLogin)
			}
			if listed(p.PostLogin, "NOTIFY") && capNext(p.PostLogin, "INPROGRESS") != "NOTIFY" {
				t.Fatalf("%s INPROGRESS is not before NOTIFY", e.Name())
			}
			if !listed(p.PostLogin, "NOTIFY") && capNext(p.PostLogin, "INPROGRESS") != "LITERAL+" {
				t.Fatalf("%s INPROGRESS is not before LITERAL+", e.Name())
			}
		}
		loginDisabled := false // no lab personality lists LOGINDISABLED; docker does not advertise it
		if listed(p.GreetingCaps, "LOGINDISABLED") != loginDisabled || listed(p.PreLogin, "LOGINDISABLED") != loginDisabled || listed(p.PostLogin, "LOGINDISABLED") {
			t.Fatalf("%s LOGINDISABLED greet %v pre %v post %v", e.Name(), p.GreetingCaps, listed(p.PreLogin, "LOGINDISABLED"), listed(p.PostLogin, "LOGINDISABLED"))
		}
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), "INPROGRESS") || strings.Contains(string(gaps), "LOGINDISABLED") {
		t.Fatal("gaps file still names a landed token")
	}
	facts := string(mustRead(t, "docs/facts/imap-server.md"))
	guide := string(mustRead(t, "docs/guide.md"))
	for _, phrase := range []string{"INPROGRESS", "LOGINDISABLED", "set-login-disabled", "PRIVACYREQUIRED", "inprogress"} {
		if !strings.Contains(facts, phrase) {
			t.Fatalf("facts missing %s", phrase)
		}
	}
	if !strings.Contains(guide, "`INPROGRESS`") || !strings.Contains(guide, "`LOGINDISABLED`") || !strings.Contains(guide, "docs/facts/imap-server.md") {
		t.Fatal("guide missing the short notes")
	}
	off := strings.Index(guide, "## Capabilities that stay off")
	if off < 0 {
		t.Fatal("stay-off section missing")
	}
	tokens := strings.Index(guide[off:], "the tokens are")
	if tokens < 0 || strings.Contains(guide[off+tokens:], "`INPROGRESS`") || strings.Contains(guide[off+tokens:], "`LOGINDISABLED`") {
		t.Fatal("stay-off still lists a landed token")
	}
	if strings.Contains(guide, "LOGIN disabled") || strings.Contains(guide, "[INPROGRESS (") {
		t.Fatal("guide pastes the facts file")
	}

	base, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	src := []string{"IMAP4rev1", "LOGINDISABLED", "AUTH=PLAIN"}
	if got := base.advertiseList(src, false, false, false, false, nil); listed(got, "LOGINDISABLED") {
		t.Fatalf("no certificate kept %v", got)
	}
	if got := base.advertiseList(src, false, true, true, false, nil); listed(got, "LOGINDISABLED") {
		t.Fatalf("secure kept %v", got)
	}
	if got := base.advertiseList(src, false, false, true, false, nil); !listed(got, "LOGINDISABLED") || listed(got, "STARTTLS") == false {
		t.Fatalf("cleartext cert %v", got)
	}
	if got := base.advertiseList(src, true, false, true, false, nil); !listed(got, "LOGINDISABLED") {
		t.Fatalf("post-login cleartext %v", got)
	}
	offForce := false
	if got := base.advertiseList(src, false, false, true, false, &offForce); listed(got, "LOGINDISABLED") {
		t.Fatalf("force off %v", got)
	}
	on := true
	if got := base.advertiseList([]string{"IMAP4rev1"}, false, false, false, false, &on); !listed(got, "LOGINDISABLED") {
		t.Fatalf("force on without certificate %v", got)
	}
	if got := base.advertiseList([]string{"IMAP4rev1"}, false, true, true, false, &on); listed(got, "LOGINDISABLED") {
		t.Fatalf("force on over TLS %v", got)
	}
	if got := base.advertiseList([]string{"IMAP4rev1"}, true, false, false, false, &on); listed(got, "LOGINDISABLED") {
		t.Fatalf("force on post-login %v", got)
	}
}

func TestInProgressFault(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	for _, req := range []map[string]any{
		{"op": "arm-fault", "do": "inprogress"},
		{"op": "arm-fault", "do": "inprogress", "command": "NOOP"},
		{"op": "arm-fault", "do": "inprogress", "command": "UID SEARCH"},
		{"op": "arm-fault", "do": "inprogress", "command": "SEARCH", "count": -1},
		{"op": "arm-fault", "do": "inprogress", "command": "SEARCH", "delay_ms": -1},
		{"op": "arm-fault", "do": "inprogress", "command": "SEARCH", "done": "nope"},
		{"op": "arm-fault", "do": "inprogress", "command": "SEARCH", "total": true},
	} {
		rp := cc.call(req)
		if rp.OK || rp.Error != "missing field" {
			t.Fatalf("missing field %+v -> %+v", req, rp)
		}
	}
	if cc.call(map[string]any{"op": "set-login-disabled"}).Error != "missing field" {
		t.Fatal("set-login-disabled without value")
	}
	if cc.call(map[string]any{"op": "set-login-disabled", "value": "true"}).Error != "missing field" {
		t.Fatal("set-login-disabled string value")
	}

	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "INPROGRESS") || strings.Contains(cl.greeting, "LOGINDISABLED") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	pre := cl.cmd("CAPABILITY").mustOK(t)
	preCaps := capsOf(pre.untagged[0])
	if !listed(preCaps, "INPROGRESS") || listed(preCaps, "LOGINDISABLED") {
		t.Fatalf("pre-login %v", preCaps)
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("SELECT INBOX").mustOK(t)
	quiet := []string{"SEARCH ALL", "UID SEARCH ALL", "ESEARCH IN (personal) ALL", "SORT (ARRIVAL) UTF-8 ALL", "UID SORT (ARRIVAL) UTF-8 ALL"}
	var baseline []*reply
	for _, line := range quiet {
		rp := cl.cmd(line).mustOK(t)
		if strings.Contains(strings.Join(rp.untagged, "\n"), "INPROGRESS") {
			t.Fatalf("quiet %s %q", line, rp.untagged)
		}
		baseline = append(baseline, rp)
	}

	cc.arm(map[string]any{"do": "inprogress", "command": "SEARCH", "times": 1})
	got := cl.cmd("SEARCH ALL").mustOK(t)
	tag := strings.Fields(got.tagLine)[0]
	if strings.Join(progressOf(got.untagged), "\n") != "* OK [INPROGRESS ("+tag+" NIL NIL)]\n* OK [INPROGRESS ("+tag+" NIL NIL)]" {
		t.Fatalf("default progress %q", got.untagged)
	}
	if strings.Join(dropProgress(got.untagged), "\n") != strings.Join(baseline[0].untagged, "\n") || got.tagLine == "" || !strings.Contains(got.tagLine, " OK ") {
		t.Fatalf("search result changed %q vs %q", got.untagged, baseline[0].untagged)
	}
	again := cl.cmd("UID SEARCH ALL").mustOK(t)
	if strings.Contains(strings.Join(again.untagged, "\n"), "INPROGRESS") {
		t.Fatal("spent fault still fired")
	}

	cc.arm(map[string]any{"do": "inprogress", "command": "SEARCH", "times": 1, "count": 1, "done": 3, "total": 9})
	uid := cl.cmd("UID SEARCH ALL").mustOK(t)
	tag = strings.Fields(uid.tagLine)[0]
	if strings.Join(progressOf(uid.untagged), "\n") != "* OK [INPROGRESS ("+tag+" 3 9)]" {
		t.Fatalf("uid search progress %q", uid.untagged)
	}
	if strings.Join(dropProgress(uid.untagged), "\n") != strings.Join(baseline[1].untagged, "\n") {
		t.Fatalf("uid search result %q", uid.untagged)
	}

	cc.arm(map[string]any{"do": "inprogress", "command": "ESEARCH", "times": 1, "count": 0})
	es := cl.cmd("ESEARCH IN (personal) ALL").mustOK(t)
	if len(progressOf(es.untagged)) != 0 || !strings.Contains(strings.Join(es.untagged, "\n"), "ESEARCH") {
		t.Fatalf("count 0 %q", es.untagged)
	}

	start := time.Now()
	cc.arm(map[string]any{"do": "inprogress", "command": "SORT", "times": 1, "delay_ms": 80, "count": 2, "done": 0, "total": 1})
	sortRP := cl.cmd("SORT (ARRIVAL) UTF-8 ALL").mustOK(t)
	if time.Since(start) < 70*time.Millisecond {
		t.Fatalf("delay %s", time.Since(start))
	}
	tag = strings.Fields(sortRP.tagLine)[0]
	if strings.Join(progressOf(sortRP.untagged), "\n") != "* OK [INPROGRESS ("+tag+" 0 1)]\n* OK [INPROGRESS ("+tag+" 0 1)]" {
		t.Fatalf("sort progress %q", sortRP.untagged)
	}
	if strings.Join(dropProgress(sortRP.untagged), "\n") != strings.Join(baseline[3].untagged, "\n") {
		t.Fatalf("sort result %q", sortRP.untagged)
	}
	cc.arm(map[string]any{"do": "inprogress", "command": "SORT", "times": 1, "count": 2, "done": 0, "total": 1})
	uidSort := cl.cmd("UID SORT (ARRIVAL) UTF-8 ALL").mustOK(t)
	tag = strings.Fields(uidSort.tagLine)[0]
	if strings.Join(progressOf(uidSort.untagged), "\n") != "* OK [INPROGRESS ("+tag+" 0 1)]\n* OK [INPROGRESS ("+tag+" 0 1)]" {
		t.Fatalf("uid sort progress %q", uidSort.untagged)
	}
	if strings.Join(dropProgress(uidSort.untagged), "\n") != strings.Join(baseline[4].untagged, "\n") {
		t.Fatalf("uid sort result %q", uidSort.untagged)
	}

	_, bareBound, bareCC := startCtrl(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/sasl.json"})
	bare := dial(t, bareBound.Listen)
	bare.cmd("LOGIN demo secret").mustOK(t)
	bare.cmd("SELECT INBOX").mustOK(t)
	bareCC.arm(map[string]any{"do": "inprogress", "command": "SEARCH", "count": 3, "done": 1, "total": 1})
	quietSearch := bare.cmd("SEARCH ALL").mustOK(t)
	if strings.Contains(strings.Join(quietSearch.untagged, "\n"), "INPROGRESS") || quietSearch.status != "OK" {
		t.Fatalf("unlisted token %q", quietSearch.untagged)
	}
	bareCC.arm(map[string]any{"do": "inprogress", "command": "ESEARCH"})
	quietES := bare.cmd("ESEARCH IN (personal) ALL").mustOK(t)
	if strings.Contains(strings.Join(quietES.untagged, "\n"), "INPROGRESS") {
		t.Fatalf("unlisted esearch %q", quietES.untagged)
	}
	bareCC.arm(map[string]any{"do": "inprogress", "command": "SORT"})
	quietSort := bare.cmd("SORT (ARRIVAL) UTF-8 ALL").mustOK(t)
	if strings.Contains(strings.Join(quietSort.untagged, "\n"), "INPROGRESS") {
		t.Fatalf("unlisted sort %q", quietSort.untagged)
	}
}

func TestLoginDisabledOnTheWire(t *testing.T) {
	cert, key := writeCert(t)
	_, bound, cc := startCtrl(t, Config{
		Listen:            "127.0.0.1:0",
		ImplicitTLSListen: "127.0.0.1:0",
		CertFile:          cert,
		KeyFile:           key,
		Personality:       "personalities/cyrus-3137.json",
	})
	cc.call(map[string]any{"op": "set-login-disabled", "value": true}).ok(t)
	cl := dial(t, bound.Listen)
	if !listed(capsOf(cl.greeting), "LOGINDISABLED") || listed(capsOf(cl.greeting), "INPROGRESS") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	pre := cl.cmd("CAPABILITY").mustOK(t)
	if !listed(capsOf(pre.untagged[0]), "LOGINDISABLED") || !listed(capsOf(pre.untagged[0]), "INPROGRESS") {
		t.Fatalf("pre %v", capsOf(pre.untagged[0]))
	}
	refused := cl.cmd("LOGIN demo secret")
	if refused.status != "NO" || !strings.Contains(refused.tagLine, "[PRIVACYREQUIRED]") || strings.Contains(refused.tagLine, "AUTHENTICATIONFAILED") {
		t.Fatalf("login %q", refused.tagLine)
	}
	if rp := cl.cmd("SELECT INBOX"); rp.status != "NO" {
		t.Fatalf("authenticated %q", rp.tagLine)
	}
	cl.cmd("AUTHENTICATE PLAIN " + plainToken("demo", "secret")).mustOK(t)
	post := cl.cmd("CAPABILITY").mustOK(t)
	if listed(capsOf(post.untagged[0]), "LOGINDISABLED") || !listed(capsOf(post.untagged[0]), "INPROGRESS") {
		t.Fatalf("post %v", capsOf(post.untagged[0]))
	}

	plain := dial(t, bound.Listen)
	plain.cmd("STARTTLS").mustOK(t)
	tc := tls.Client(plain.c, &tls.Config{InsecureSkipVerify: true})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	plain.c = tc
	plain.r = bufio.NewReader(tc)
	plain.w = bufio.NewWriter(tc)
	after := plain.cmd("CAPABILITY").mustOK(t)
	if listed(capsOf(after.untagged[0]), "LOGINDISABLED") {
		t.Fatalf("after STARTTLS %v", capsOf(after.untagged[0]))
	}
	plain.cmd("LOGIN demo secret").mustOK(t)

	implicit := dialTLS(t, bound.ImplicitTLSListen)
	if listed(capsOf(implicit.greeting), "LOGINDISABLED") {
		t.Fatalf("implicit greeting %q", implicit.greeting)
	}
	impPre := implicit.cmd("CAPABILITY").mustOK(t)
	if listed(capsOf(impPre.untagged[0]), "LOGINDISABLED") {
		t.Fatalf("implicit pre %v", capsOf(impPre.untagged[0]))
	}
	implicit.cmd("LOGIN demo secret").mustOK(t)

	cc.call(map[string]any{"op": "set-login-disabled", "value": false}).ok(t)
	forcedOff := dial(t, bound.Listen)
	if listed(capsOf(forcedOff.greeting), "LOGINDISABLED") {
		t.Fatalf("forced off greeting %q", forcedOff.greeting)
	}
	offPre := forcedOff.cmd("CAPABILITY").mustOK(t)
	if listed(capsOf(offPre.untagged[0]), "LOGINDISABLED") {
		t.Fatalf("forced off pre %v", capsOf(offPre.untagged[0]))
	}
	forcedOff.cmd("LOGIN demo secret").mustOK(t)

	_, otherBound, otherCC := startCtrl(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3124.json"})
	otherCC.call(map[string]any{"op": "set-login-disabled", "value": true}).ok(t)
	forced := dial(t, otherBound.Listen)
	if !listed(capsOf(forced.greeting), "LOGINDISABLED") {
		t.Fatalf("forced greeting %q", forced.greeting)
	}
	forcedPre := forced.cmd("CAPABILITY").mustOK(t)
	if !listed(capsOf(forcedPre.untagged[0]), "LOGINDISABLED") {
		t.Fatalf("forced pre %v", capsOf(forcedPre.untagged[0]))
	}
	no := forced.cmd("LOGIN demo secret")
	if no.status != "NO" || !strings.Contains(no.tagLine, "[PRIVACYREQUIRED]") {
		t.Fatalf("forced login %q", no.tagLine)
	}
	forced.cmd("AUTHENTICATE PLAIN " + plainToken("demo", "secret")).mustOK(t)
}

func progressOf(lines []string) []string {
	var out []string
	for _, line := range lines {
		if strings.Contains(line, "[INPROGRESS ") {
			out = append(out, line)
		}
	}
	return out
}

func dropProgress(lines []string) []string {
	var out []string
	for _, line := range lines {
		if strings.Contains(line, "[INPROGRESS ") {
			continue
		}
		out = append(out, line)
	}
	return out
}
