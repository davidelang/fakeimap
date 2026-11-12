package main

import (
	"strings"
	"testing"
)

func TestIDCommand(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/cyrus-3137.json"})
	cl := dial(t, bound.Listen)
	checkID := func(line string) {
		t.Helper()
		rp := cl.cmd(line).mustOK(t)
		if len(rp.untagged) != 1 || rp.untagged[0] != `* ID ("name" "fakeimap")` {
			t.Fatalf("%s untagged %q", line, rp.untagged)
		}
		if strings.Contains(rp.tagLine, "client") || strings.Contains(strings.Join(rp.untagged, "\n"), "client") {
			t.Fatalf("%s echoed the client list: %+v", line, rp)
		}
	}
	checkID("ID NIL")
	checkID(`ID ("name" "client")`)
	if rp := cl.cmd("ID"); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("no argument %+v", rp)
	}
	if rp := cl.cmd(`ID ("name" "client") ("a" "b")`); rp.status != "BAD" || len(rp.untagged) != 0 {
		t.Fatalf("two lists %+v", rp)
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	checkID("ID NIL")
	checkID(`ID ("name" "client")`)

	_, off := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/dovecot-10.json"})
	plain := dial(t, off.Listen)
	if rp := plain.cmd("ID NIL"); rp.status != "BAD" {
		t.Fatalf("unlisted ID %s %q", rp.status, rp.untagged)
	}
	plain.cmd("LOGIN demo secret").mustOK(t)
	if rp := plain.cmd("ID NIL"); rp.status != "BAD" {
		t.Fatalf("unlisted ID after login %s", rp.status)
	}

	_, saslBound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/sasl.json"})
	sasl := dial(t, saslBound.Listen)
	if rp := sasl.cmd("ID NIL"); rp.status != "BAD" {
		t.Fatalf("sasl pre-login ID %s", rp.status)
	}
	sasl.cmd("LOGIN demo secret").mustOK(t)
	got := sasl.cmd("ID NIL").mustOK(t)
	if len(got.untagged) != 1 || got.untagged[0] != `* ID ("name" "fakeimap")` {
		t.Fatalf("sasl post ID %q", got.untagged)
	}
}
