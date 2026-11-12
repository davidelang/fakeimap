package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJMAPAccessLists(t *testing.T) {
	if !capImplemented("JMAPACCESS") || !capImplemented("OBJECTID") || !capImplemented("OBJECTID+") {
		t.Fatal("JMAPACCESS, OBJECTID, or OBJECTID+ is not implemented")
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gaps), `"OBJECTID"`) || strings.Contains(string(gaps), "JMAPACCESS") {
		t.Fatal("gaps file changed")
	}
	postOnly := map[string]bool{
		"stalwart.json":       true,
		"stalwart-01625.json": true,
		"stalwart-dev.json":   true,
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), ".gaps.") {
			continue
		}
		p, err := loadPersonality(filepath.Join("personalities", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		g := listed(p.GreetingCaps, "JMAPACCESS")
		pre := listed(p.PreLogin, "JMAPACCESS")
		post := listed(p.PostLogin, "JMAPACCESS")
		switch {
		case e.Name() == "stalwart-0155.json":
			if !g || !pre || !post {
				t.Fatalf("%s lists greeting %v pre %v post %v", e.Name(), g, pre, post)
			}
			if !jmapBetween(p.GreetingCaps, "AUTH=PLAIN") || !jmapBetween(p.PreLogin, "AUTH=PLAIN") || !jmapBetween(p.PostLogin, "IDLE") {
				t.Fatalf("%s position greet %v pre %v post %v", e.Name(), p.GreetingCaps, p.PreLogin, p.PostLogin)
			}
		case postOnly[e.Name()]:
			if g || pre || !post || !jmapBetween(p.PostLogin, "IDLE") {
				t.Fatalf("%s lists greeting %v pre %v post %v %v", e.Name(), g, pre, post, p.PostLogin)
			}
		default:
			if g || pre || post {
				t.Fatalf("%s gained JMAPACCESS", e.Name())
			}
		}
	}
}

func jmapBetween(list []string, next string) bool {
	i := indexCap(list, "JMAPACCESS")
	u := indexCap(list, "UTF8=ACCEPT")
	n := indexCap(list, next)
	return i > 0 && u >= 0 && n >= 0 && i == u+1 && n == i+1
}

func TestJMAPAccessCommand(t *testing.T) {
	_, cyrusBound := start(t, Config{
		Listen:      "127.0.0.1:0",
		JmapListen:  "127.0.0.1:0",
		Personality: "personalities/cyrus-3137.json",
	})
	cyrus := dial(t, cyrusBound.Listen)
	if strings.Contains(cyrus.greeting, "JMAPACCESS") {
		t.Fatalf("cyrus greeting %q", cyrus.greeting)
	}
	if rp := cyrus.cmd("GETJMAPACCESS"); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("cyrus before login %+v", rp)
	}
	cyrus.cmd("LOGIN demo secret").mustOK(t)
	post := capsOf(cyrus.cmd("CAPABILITY").mustOK(t).untagged[0])
	if listed(post, "JMAPACCESS") {
		t.Fatalf("cyrus advertises JMAPACCESS %v", post)
	}
	if rp := cyrus.cmd("GETJMAPACCESS"); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("cyrus GETJMAPACCESS %+v", rp)
	}

	p, err := loadPersonality("personalities/stalwart.json")
	if err != nil {
		t.Fatal(err)
	}
	_, bare := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/stalwart.json"})
	off := dial(t, bare.Listen)
	if strings.Contains(off.greeting, "JMAPACCESS") || listed(capsOf(off.cmd("CAPABILITY").mustOK(t).untagged[0]), "JMAPACCESS") {
		t.Fatal("unbound server advertises JMAPACCESS")
	}
	if rp := off.cmd("GETJMAPACCESS"); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("unbound before login %+v", rp)
	}
	login := off.cmd("LOGIN demo secret").mustOK(t)
	if listed(capsOf(login.tagLine), "JMAPACCESS") {
		t.Fatalf("unbound post %v", capsOf(login.tagLine))
	}
	if got := strings.Join(capsOf(login.tagLine), " "); got != strings.Join(p.advertise(true, false, false, false), " ") {
		t.Fatalf("unbound post %s", got)
	}
	if rp := off.cmd("GETJMAPACCESS"); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("unbound GETJMAPACCESS %+v", rp)
	}

	_, bound := start(t, Config{
		Listen:      "127.0.0.1:0",
		JmapListen:  "127.0.0.1:0",
		Personality: "personalities/stalwart.json",
	})
	cl := dial(t, bound.Listen)
	if strings.Contains(cl.greeting, "JMAPACCESS") || listed(capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0]), "JMAPACCESS") {
		t.Fatal("stalwart advertises JMAPACCESS before login")
	}
	if rp := cl.cmd("GETJMAPACCESS"); rp.status != "NO" || len(rp.untagged) != 0 {
		t.Fatalf("before login %+v", rp)
	}
	in := cl.cmd("LOGIN demo secret").mustOK(t)
	if !jmapBetween(capsOf(in.tagLine), "IDLE") {
		t.Fatalf("post caps %v", capsOf(in.tagLine))
	}
	if got := strings.Join(capsOf(in.tagLine), " "); got != strings.Join(p.advertise(true, false, false, true), " ") {
		t.Fatalf("http post %s", got)
	}
	want := fmt.Sprintf(`* JMAPACCESS "http://127.0.0.1:%s/.well-known/jmap"`, mustPort(t, bound.Jmap))
	got := cl.cmd("GETJMAPACCESS").mustOK(t)
	if len(got.untagged) != 1 || got.untagged[0] != want {
		t.Fatalf("url %q want %s", got.untagged, want)
	}
	if rp := cl.cmd("GETJMAPACCESS extra"); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("argument %+v", rp)
	}

	old, err := loadPersonality("personalities/stalwart-0155.json")
	if err != nil {
		t.Fatal(err)
	}
	_, oldBound := start(t, Config{
		Listen:      "127.0.0.1:0",
		JmapListen:  "127.0.0.1:0",
		Personality: "personalities/stalwart-0155.json",
	})
	early := dial(t, oldBound.Listen)
	if !jmapBetween(capsOf(early.greeting), "AUTH=PLAIN") {
		t.Fatalf("0155 greeting %q", early.greeting)
	}
	pre := early.cmd("CAPABILITY").mustOK(t)
	if !jmapBetween(capsOf(pre.untagged[0]), "AUTH=PLAIN") {
		t.Fatalf("0155 pre %v", capsOf(pre.untagged[0]))
	}
	if rp := early.cmd("GETJMAPACCESS"); rp.status != "NO" || len(rp.untagged) != 0 {
		t.Fatalf("0155 before login %+v", rp)
	}
	oldIn := early.cmd("LOGIN demo secret").mustOK(t)
	if !jmapBetween(capsOf(oldIn.tagLine), "IDLE") {
		t.Fatalf("0155 post %v", capsOf(oldIn.tagLine))
	}
	if got := strings.Join(capsOf(oldIn.tagLine), " "); got != strings.Join(old.advertise(true, false, false, true), " ") {
		t.Fatalf("0155 post %s", got)
	}
	oldURL := early.cmd("GETJMAPACCESS").mustOK(t)
	want = fmt.Sprintf(`* JMAPACCESS "http://127.0.0.1:%s/.well-known/jmap"`, mustPort(t, oldBound.Jmap))
	if len(oldURL.untagged) != 1 || oldURL.untagged[0] != want {
		t.Fatalf("0155 url %q", oldURL.untagged)
	}

	cert, key := writeCert(t)
	_, tlsOnly := start(t, Config{
		Listen:        "127.0.0.1:0",
		JmapTLSListen: "127.0.0.1:0",
		CertFile:      cert,
		KeyFile:       key,
		Personality:   "personalities/stalwart.json",
	})
	tlsCl := dial(t, tlsOnly.Listen)
	tlsCl.cmd("LOGIN demo secret").mustOK(t)
	want = fmt.Sprintf(`* JMAPACCESS "https://127.0.0.1:%s/.well-known/jmap"`, mustPort(t, tlsOnly.JmapTLS))
	tlsGot := tlsCl.cmd("GETJMAPACCESS").mustOK(t)
	if len(tlsGot.untagged) != 1 || tlsGot.untagged[0] != want {
		t.Fatalf("tls url %q", tlsGot.untagged)
	}

	_, both := start(t, Config{
		Listen:        "127.0.0.1:0",
		JmapListen:    "127.0.0.1:0",
		JmapTLSListen: "127.0.0.1:0",
		CertFile:      cert,
		KeyFile:       key,
		Personality:   "personalities/stalwart.json",
	})
	if both.Jmap == "" || both.JmapTLS == "" || both.Jmap == both.JmapTLS {
		t.Fatalf("bound %+v", both)
	}
	bothCl := dial(t, both.Listen)
	bothCl.cmd("LOGIN demo secret").mustOK(t)
	want = fmt.Sprintf(`* JMAPACCESS "https://127.0.0.1:%s/.well-known/jmap"`, mustPort(t, both.JmapTLS))
	bothGot := bothCl.cmd("GETJMAPACCESS").mustOK(t)
	if len(bothGot.untagged) != 1 || bothGot.untagged[0] != want {
		t.Fatalf("both url %q clear %s", bothGot.untagged, both.Jmap)
	}

	assertJMAPHost(t, "0.0.0.0:0", "http", "127.0.0.1")
	assertJMAPHost(t, "[::]:0", "http", "127.0.0.1")
	assertJMAPHost(t, "[::1]:0", "http", "[::1]")
}

func assertJMAPHost(t *testing.T, listen, scheme, host string) {
	t.Helper()
	_, bound := start(t, Config{
		Listen:      "127.0.0.1:0",
		JmapListen:  listen,
		Personality: "personalities/stalwart.json",
	})
	port := mustPort(t, bound.Jmap)
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	want := fmt.Sprintf(`* JMAPACCESS "%s://%s:%s/.well-known/jmap"`, scheme, host, port)
	got := cl.cmd("GETJMAPACCESS").mustOK(t)
	if len(got.untagged) != 1 || got.untagged[0] != want {
		t.Fatalf("listen %s url %q want %s bound %s", listen, got.untagged, want, bound.Jmap)
	}
}

func mustPort(t *testing.T, addr string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" || port == "0" {
		t.Fatalf("port of %q: %v", addr, err)
	}
	return port
}
