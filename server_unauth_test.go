package main

import (
	"crypto/tls"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUnauthenticate(t *testing.T) {
	if !capImplemented("UNAUTHENTICATE") {
		t.Fatal("UNAUTHENTICATE is not implemented")
	}
	stalwartFiles := map[string]bool{
		"stalwart.json":       true,
		"stalwart-0155.json":  true,
		"stalwart-01625.json": true,
		"stalwart-dev.json":   true,
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join("personalities", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		has := strings.Contains(string(b), "UNAUTHENTICATE")
		if stalwartFiles[e.Name()] {
			if !has {
				t.Fatalf("%s missing UNAUTHENTICATE", e.Name())
			}
			p, err := loadPersonality(filepath.Join("personalities", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if listed(p.GreetingCaps, "UNAUTHENTICATE") || listed(p.PreLogin, "UNAUTHENTICATE") {
				t.Fatalf("%s lists UNAUTHENTICATE before login", e.Name())
			}
			q := indexCap(p.PostLogin, "QRESYNC")
			u := indexCap(p.PostLogin, "UNAUTHENTICATE")
			sz := indexCap(p.PostLogin, "STATUS=SIZE")
			obj := indexCap(p.PostLogin, "OBJECTID")
			if obj < 0 {
				obj = indexCap(p.PostLogin, "OBJECTID+")
			}
			pr := indexCap(p.PostLogin, "PREVIEW")
			if q < 0 || u != q+1 || sz != u+1 || obj != sz+1 || pr != obj+1 || p.PostLogin[len(p.PostLogin)-1] != "STARTTLS" {
				t.Fatalf("%s post list %v", e.Name(), p.PostLogin)
			}
			continue
		}
		if has {
			t.Fatalf("%s gained UNAUTHENTICATE", e.Name())
		}
	}

	_, cyrusBound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	cyrus := dial(t, cyrusBound.Listen)
	cyrus.cmd("LOGIN demo secret").mustOK(t)
	if rp := cyrus.cmd("UNAUTHENTICATE"); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("cyrus UNAUTHENTICATE %+v", rp)
	}
	cyrus.cmd("NOOP").ok(t)

	p, err := loadPersonality("personalities/stalwart.json")
	if err != nil {
		t.Fatal(err)
	}
	wantPre := strings.Join(p.advertise(false, false, false, false), " ")
	wantPost := strings.Join(p.advertise(true, false, false, false), " ")

	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/stalwart.json"})
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "UNAUTHENTICATE") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	pre := cl.cmd("CAPABILITY").mustOK(t)
	if strings.Join(capsOf(pre.untagged[0]), " ") != wantPre {
		t.Fatalf("pre-login %v", capsOf(pre.untagged[0]))
	}
	if rp := cl.cmd("UNAUTHENTICATE"); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("before login %+v", rp)
	}
	login := cl.cmd("LOGIN demo secret").mustOK(t)
	if strings.Join(capsOf(login.tagLine), " ") != wantPost || len(login.untagged) != 0 {
		t.Fatalf("login caps %q untagged %q", login.tagLine, login.untagged)
	}
	if rp := cl.cmd("UNAUTHENTICATE extra"); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("argument %+v", rp)
	}
	en := cl.cmd("ENABLE QRESYNC UTF8=ACCEPT").mustOK(t)
	if strings.Join(en.untagged, " ") != "* ENABLED CONDSTORE QRESYNC UTF8=ACCEPT" {
		t.Fatalf("enable %q", en.untagged)
	}
	sel := cl.cmd("SELECT INBOX").mustOK(t)
	exists, _, uidv, _ := parseSelect(t, sel)
	if exists < 1 {
		t.Fatal("empty inbox")
	}
	if cl.cmd("SORT (SUBJECT) UTF-8 ALL").status != "BAD" {
		t.Fatal("UTF8=ACCEPT did not reject SORT charset")
	}
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).ok(t)
	gone := cl.cmd("UNAUTHENTICATE")
	if gone.status != "OK" || len(gone.untagged) != 0 || strings.Contains(gone.tagLine, "CAPABILITY") {
		t.Fatalf("unauthenticate %+v", gone)
	}
	again := cl.cmd("CAPABILITY").mustOK(t)
	if strings.Join(capsOf(again.untagged[0]), " ") != wantPre {
		t.Fatalf("after unauth caps %v", capsOf(again.untagged[0]))
	}
	if cl.cmd("SELECT INBOX").status != "NO" {
		t.Fatal("SELECT was not rejected")
	}
	second := cl.cmd("LOGIN demo secret").mustOK(t)
	if strings.Join(capsOf(second.tagLine), " ") != wantPost {
		t.Fatalf("second login %q", second.tagLine)
	}
	qr := cl.cmd("SELECT INBOX (QRESYNC (" + strconv.FormatUint(uint64(uidv), 10) + " 0 1:2))")
	if qr.status != "BAD" {
		t.Fatalf("qresync still enabled %s %v", qr.tagLine, qr.untagged)
	}
	back := cl.cmd("SELECT INBOX").mustOK(t)
	gotExists, _, _, _ := parseSelect(t, back)
	if gotExists != exists {
		t.Fatalf("exists %d want %d", gotExists, exists)
	}
	flags := fetchLine(t, cl.cmd("FETCH 1 (FLAGS)").mustOK(t))
	if !strings.Contains(flags, `\Deleted`) {
		t.Fatalf("deleted flag gone: %s", flags)
	}
	if cl.cmd("SORT (SUBJECT) UTF-8 ALL").status != "OK" {
		t.Fatal("UTF8=ACCEPT still enabled")
	}
	reen := cl.cmd("ENABLE QRESYNC").mustOK(t)
	if strings.Join(reen.untagged, " ") != "* ENABLED CONDSTORE QRESYNC" {
		t.Fatalf("re-enable %q", reen.untagged)
	}
}

func TestUnauthenticateKeepsTLS(t *testing.T) {
	cert, key := writeCert(t)
	p, err := loadPersonality("personalities/stalwart.json")
	if err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{
		ImplicitTLSListen: "127.0.0.1:0",
		CertFile:          cert,
		KeyFile:           key,
		Personality:       "personalities/stalwart.json",
	})
	cl := dialTLS(t, bound.ImplicitTLSListen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("UNAUTHENTICATE").mustOK(t)
	tc, ok := cl.c.(*tls.Conn)
	if !ok || !tc.ConnectionState().HandshakeComplete {
		t.Fatal("TLS did not stay up")
	}
	pre := cl.cmd("CAPABILITY").mustOK(t)
	if strings.Join(capsOf(pre.untagged[0]), " ") != strings.Join(p.advertise(false, true, true, false), " ") {
		t.Fatalf("tls pre %v", capsOf(pre.untagged[0]))
	}
	login := cl.cmd("LOGIN demo secret").mustOK(t)
	if strings.Join(capsOf(login.tagLine), " ") != strings.Join(p.advertise(true, true, true, false), " ") {
		t.Fatalf("tls login %q", login.tagLine)
	}
	cl.cmd("NOOP").ok(t)
}

func TestUnauthenticateKeepsDeflate(t *testing.T) {
	p, err := loadPersonality("personalities/stalwart.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PostLogin = append(append([]string{}, p.PostLogin...), "COMPRESS=DEFLATE")
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "compress-unauth.json")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("ENABLE QRESYNC UTF8=ACCEPT").mustOK(t)
	cl.cmd("COMPRESS DEFLATE").mustOK(t)
	cl.enableDeflate()
	gone := cl.cmd("UNAUTHENTICATE")
	if gone.status != "OK" || strings.Contains(gone.tagLine, "CAPABILITY") {
		t.Fatalf("compressed unauth %+v", gone)
	}
	cl.cmd("CAPABILITY").mustOK(t)
	cl.cmd("LOGIN demo secret").mustOK(t)
	if cl.cmd("COMPRESS DEFLATE").status != "NO" {
		t.Fatal("compression did not stay up")
	}
	reen := cl.cmd("ENABLE QRESYNC UTF8=ACCEPT").mustOK(t)
	if strings.Join(reen.untagged, " ") != "* ENABLED CONDSTORE QRESYNC UTF8=ACCEPT" {
		t.Fatalf("extensions still enabled %q", reen.untagged)
	}
	cl.cmd("NOOP").ok(t)
}

func indexCap(list []string, name string) int {
	for i, c := range list {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}
