package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIMAP4rev2NeverAdvertised(t *testing.T) {
	if _, ok := implementedCaps["IMAP4REV2"]; ok || capImplemented("IMAP4rev2") {
		t.Fatal("IMAP4rev2 is implemented")
	}
	const stalSentence = "Stalwart IMAP4rev2 at your service."
	stalwarts := map[string]bool{
		"stalwart.json":       true,
		"stalwart-0155.json":  true,
		"stalwart-01625.json": true,
		"stalwart-dev.json":   true,
	}
	entries, err := os.ReadDir("personalities")
	if err != nil {
		t.Fatal(err)
	}
	var nPers int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join("personalities", e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() == "cyrus-3137.gaps.json" {
			if !strings.Contains(string(raw), "IMAP4rev2") {
				t.Fatal("gaps file dropped IMAP4rev2")
			}
			continue
		}
		nPers++
		p, err := loadPersonality(path)
		if err != nil {
			t.Fatal(err)
		}
		if listed(p.GreetingCaps, "IMAP4rev2") || listed(p.PreLogin, "IMAP4rev2") || listed(p.PostLogin, "IMAP4rev2") {
			t.Fatalf("%s lists IMAP4rev2", path)
		}
		if strings.Contains(string(raw), "IMAP4rev2") {
			if !stalwarts[e.Name()] || p.Greeting != stalSentence {
				t.Fatalf("%s has the capability token", path)
			}
			stripped := strings.ReplaceAll(string(raw), stalSentence, "")
			if strings.Contains(stripped, "IMAP4rev2") {
				t.Fatalf("%s has the capability token outside the greeting sentence", path)
			}
		}
	}
	if nPers < 2 || len(stalwarts) != 4 {
		t.Fatal("expected personality files")
	}
	probe, err := os.ReadFile("probe_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(probe), `* OK [CAPABILITY IMAP4rev1 IMAP4rev2] deadbeefdead Cyrus ready`) || !strings.Contains(string(probe), `gaps.Capabilities[0] != "IMAP4rev2"`) {
		t.Fatal("probe fixture no longer records the IMAP4rev2 gap")
	}

	facts, err := os.ReadFile("docs/facts/imap-server.md")
	if err != nil {
		t.Fatal(err)
	}
	factText := string(facts)
	for _, phrase := range []string{
		"never advertised",
		"advertiseList",
		"a personality that lists it is filtered",
		"cyrus-3137.gaps.json",
		stalSentence,
		"ENABLE IMAP4rev2",
	} {
		if !strings.Contains(factText, phrase) {
			t.Fatalf("facts missing %q", phrase)
		}
	}
	guideRaw, err := os.ReadFile("docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := string(guideRaw)
	off := strings.Index(guide, "## Capabilities that stay off")
	if off < 0 {
		t.Fatal("guide missing stay-off section")
	}
	stay := guide[off:]
	if !strings.Contains(stay, "IMAP4rev2") || !strings.Contains(stay, "never advertised") || !strings.Contains(stay, "filtered") || !strings.Contains(stay, stalSentence) || !strings.Contains(stay, "docs/facts/imap-server.md") {
		t.Fatal("guide missing the IMAP4rev2 filter")
	}
	if strings.Contains(stay, "drops that token by name") || strings.Contains(stay, "even if the token were added") {
		t.Fatal("guide pastes the facts file")
	}

	implementedCaps["IMAP4REV2"] = struct{}{}
	t.Cleanup(func() { delete(implementedCaps, "IMAP4REV2") })
	if !capImplemented("IMAP4rev2") {
		t.Fatal("test did not mark IMAP4rev2 implemented")
	}
	base, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	base.GreetingCaps = []string{"IMAP4rev1", "IMAP4rev2", "AUTH=PLAIN"}
	base.PreLogin = []string{"IMAP4rev1", "imap4rev2", "AUTH=PLAIN", "ENABLE"}
	base.PostLogin = []string{"IMAP4rev1", "IMAP4REV2", "ENABLE", "CONDSTORE"}
	for _, spec := range []struct {
		src       []string
		postLogin bool
		want      string
	}{
		{base.GreetingCaps, false, "IMAP4rev1 AUTH=PLAIN"},
		{base.PreLogin, false, "IMAP4rev1 AUTH=PLAIN ENABLE"},
		{base.PostLogin, true, "IMAP4rev1 ENABLE CONDSTORE"},
	} {
		got := base.advertiseList(spec.src, spec.postLogin, false, false, false)
		if listed(got, "IMAP4rev2") || strings.Join(got, " ") != spec.want {
			t.Fatalf("advertiseList %v -> %v", spec.src, got)
		}
	}
	b, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	if listed(capsOf(cl.greeting), "IMAP4rev2") || strings.Join(capsOf(cl.greeting), " ") != "IMAP4rev1 AUTH=PLAIN" {
		t.Fatalf("greeting %q", cl.greeting)
	}
	pre := cl.cmd("CAPABILITY").mustOK(t)
	if len(pre.untagged) != 1 || listed(capsOf(pre.untagged[0]), "IMAP4rev2") || strings.Join(capsOf(pre.untagged[0]), " ") != "IMAP4rev1 AUTH=PLAIN ENABLE" {
		t.Fatalf("pre-login %q", pre.untagged)
	}
	login := cl.cmd("LOGIN demo secret").mustOK(t)
	if listed(capsOf(login.tagLine), "IMAP4rev2") {
		t.Fatalf("login capability %q", login.tagLine)
	}
	post := cl.cmd("CAPABILITY").mustOK(t)
	if len(post.untagged) != 1 || listed(capsOf(post.untagged[0]), "IMAP4rev2") || strings.Join(capsOf(post.untagged[0]), " ") != "IMAP4rev1 ENABLE CONDSTORE" {
		t.Fatalf("post-login %q", post.untagged)
	}
	only := cl.cmd("ENABLE IMAP4rev2")
	only.mustOK(t)
	if len(only.untagged) != 0 || strings.Contains(strings.ToUpper(only.tagLine), "IMAP4REV2") {
		t.Fatalf("ENABLE IMAP4rev2 %q %q", only.tagLine, only.untagged)
	}
	mixed := cl.cmd(`ENABLE IMAP4rev2 CONDSTORE`)
	mixed.mustOK(t)
	joined := strings.ToUpper(strings.Join(mixed.untagged, " "))
	if len(mixed.untagged) != 1 || joined != "* ENABLED CONDSTORE" || strings.Contains(joined, "IMAP4REV2") {
		t.Fatalf("mixed ENABLE %q", mixed.untagged)
	}
}
