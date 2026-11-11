package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMultisearchPersonality(t *testing.T) {
	p, err := loadPersonality("personalities/multisearch.json")
	if err != nil {
		t.Fatal(err)
	}
	if !listed(p.PreLogin, "MULTISEARCH") || !listed(p.PostLogin, "MULTISEARCH") {
		t.Fatal("multisearch.json missing MULTISEARCH")
	}
	if listed(p.GreetingCaps, "MULTISEARCH") {
		t.Fatal("greeting lists MULTISEARCH")
	}
}

func TestMultisearch(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/multisearch.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"MULTISEARCH"})

	word := "qxmultisearch"
	msg := func(body string) string {
		return "From: a@example.com\r\nTo: b@example.com\r\nSubject: plain\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\n" + body + "\r\n"
	}
	appendTo := func(name, body string) {
		t.Helper()
		raw := msg(body)
		cl.cmd(fmt.Sprintf("APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n%s", name, len(raw), raw)).ok(t)
	}
	for _, name := range []string{"INBOX2", "INBOX/Kid", "INBOX/KidExtra", "INBOX/Kid/Grand"} {
		cl.cmd("CREATE " + name).ok(t)
	}
	appendTo("INBOX2", word)
	cl.cmd("SELECT INBOX/Kid").ok(t)
	appendTo("INBOX/Kid", "other")
	appendTo("INBOX/Kid", word)
	appendTo("INBOX/Kid", word)
	cl.cmd("STORE 1 +FLAGS (\\Deleted)").ok(t)
	cl.cmd("EXPUNGE").ok(t)
	cl.cmd("UNSELECT").ok(t)
	appendTo("INBOX/KidExtra", word)
	appendTo("INBOX/Kid/Grand", word)

	uv := map[string]uint32{}
	for _, name := range []string{"INBOX2", "INBOX/Kid", "INBOX/KidExtra", "INBOX/Kid/Grand"} {
		uv[name] = statusUIDValidity(t, cl, name)
	}
	wantSubtree := []string{"INBOX/Kid", "INBOX/KidExtra", "INBOX/Kid/Grand"}
	allOf := map[string]string{
		"INBOX2":          "1",
		"INBOX/Kid":       "2:3",
		"INBOX/KidExtra":  "1",
		"INBOX/Kid/Grand": "1",
	}
	countOf := map[string]string{
		"INBOX2":          "1",
		"INBOX/Kid":       "2",
		"INBOX/KidExtra":  "1",
		"INBOX/Kid/Grand": "1",
	}

	tag, lines := multiLines(t, cl.cmd("ESEARCH IN (subtree INBOX) RETURN (ALL) TEXT "+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, wantSubtree, uv, "ALL", allOf), "\n") {
		t.Fatalf("subtree all\n%s", strings.Join(lines, "\n"))
	}
	tag, lines = multiLines(t, cl.cmd(`ESEARCH IN (subtree "INBOX") RETURN (ALL) CHARSET UTF-8 TEXT `+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, wantSubtree, uv, "ALL", allOf), "\n") {
		t.Fatalf("charset subtree\n%s", strings.Join(lines, "\n"))
	}
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (subtree INBOX) RETURN (COUNT) TEXT "+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, wantSubtree, uv, "COUNT", countOf), "\n") {
		t.Fatalf("subtree count\n%s", strings.Join(lines, "\n"))
	}
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (subtree INBOX) RETURN (COUNT) CHARSET UTF-8 TEXT "+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, wantSubtree, uv, "COUNT", countOf), "\n") {
		t.Fatalf("charset count\n%s", strings.Join(lines, "\n"))
	}
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (subtree INBOX/Kid) RETURN (ALL) TEXT "+word))
	kidOnly := []string{"INBOX/Kid", "INBOX/Kid/Grand"}
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, kidOnly, uv, "ALL", allOf), "\n") {
		t.Fatalf("kid subtree\n%s", strings.Join(lines, "\n"))
	}
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (subtree INBOX2) RETURN (ALL) CHARSET UTF-8 TEXT "+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, []string{"INBOX2"}, uv, "ALL", allOf), "\n") {
		t.Fatalf("inbox2\n%s", strings.Join(lines, "\n"))
	}

	personal := []string{"INBOX2", "INBOX/Kid", "INBOX/KidExtra", "INBOX/Kid/Grand"}
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (personal) RETURN (ALL) TEXT "+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, personal, uv, "ALL", allOf), "\n") {
		t.Fatalf("personal\n%s", strings.Join(lines, "\n"))
	}
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (personal) RETURN (COUNT) CHARSET UTF-8 TEXT "+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, personal, uv, "COUNT", countOf), "\n") {
		t.Fatalf("personal count\n%s", strings.Join(lines, "\n"))
	}
	if _, lines = multiLines(t, cl.cmd("ESEARCH IN (subscribed) RETURN (ALL) TEXT "+word)); len(lines) != 0 {
		t.Fatalf("subscribed %v", lines)
	}
	if _, lines = multiLines(t, cl.cmd("ESEARCH IN (subscribed) RETURN (COUNT) ALL")); len(lines) != 0 {
		t.Fatalf("subscribed count %v", lines)
	}
	if _, lines = multiLines(t, cl.cmd("ESEARCH IN (personal) RETURN (ALL) TEXT nosuchwordqx")); len(lines) != 0 {
		t.Fatalf("empty personal %v", lines)
	}

	cl.cmd("SELECT INBOX2").ok(t)
	tag, lines = multiLines(t, cl.cmd("ESEARCH IN (subtree INBOX/Kid) RETURN (ALL) TEXT "+word))
	if strings.Join(lines, "\n") != strings.Join(multiWant(tag, kidOnly, uv, "ALL", allOf), "\n") {
		t.Fatalf("selected other\n%s", strings.Join(lines, "\n"))
	}
	if cl.cmd("FETCH 1 (UID)").status != "OK" {
		t.Fatal("selection changed")
	}

	cl.cmd("SELECT INBOX/Kid").ok(t)
	if got := strings.Join(searchIDs(t, cl.cmd("UID SEARCH TEXT "+word)), " "); got != "2 3" {
		t.Fatalf("uid search %q", got)
	}
	if got := strings.Join(searchIDs(t, cl.cmd("SEARCH TEXT "+word)), " "); got != "1 2" {
		t.Fatalf("search %q", got)
	}
	uidLine := cl.cmd("UID SEARCH TEXT " + word)
	if len(uidLine.untagged) != 1 || !strings.HasPrefix(uidLine.untagged[0], "* SEARCH ") {
		t.Fatalf("uid search line %q", uidLine.untagged)
	}

	for _, bad := range []string{
		"ESEARCH IN (mailboxes INBOX) RETURN (ALL) ALL",
		"ESEARCH IN (mailboxes INBOX INBOX2) RETURN (COUNT) ALL",
		"ESEARCH IN (selected) RETURN (ALL) ALL",
		"ESEARCH IN (subtree-one INBOX) RETURN (ALL) ALL",
		"ESEARCH IN (subtree MissingBoxQx) RETURN (ALL) ALL",
		"ESEARCH IN (subtree) RETURN (ALL) ALL",
		"ESEARCH IN personal RETURN (ALL) ALL",
		"ESEARCH",
		"ESEARCH IN (personal) RETURN (ALL) FUZZY TEXT " + word,
		"ESEARCH IN (subtree INBOX) RETURN (ALL) CHARSET UTF-8 FUZZY SUBJECT plain",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
}

func TestMultisearchRequiresCap(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/fuzzy.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	for _, bad := range []string{
		"ESEARCH IN (subtree INBOX) RETURN (ALL) ALL",
		"ESEARCH IN (personal) RETURN (COUNT) TEXT seed",
		"ESEARCH IN (subscribed) RETURN (ALL) ALL",
		"ESEARCH IN (personal) RETURN (ALL) FUZZY TEXT seed",
	} {
		if cl.cmd(bad).status != "BAD" {
			t.Fatalf("%s was accepted", bad)
		}
	}
	cl.cmd("SELECT INBOX").ok(t)
	if got := cl.cmd("UID SEARCH ALL"); got.status != "OK" || len(got.untagged) != 1 || !strings.HasPrefix(got.untagged[0], "* SEARCH") {
		t.Fatalf("uid search changed %+v", got)
	}
}

func TestMultisearchFuzzy(t *testing.T) {
	p, err := loadPersonality("personalities/multisearch.json")
	if err != nil {
		t.Fatal(err)
	}
	p.PreLogin = append(append([]string{}, p.PreLogin...), "SEARCH=FUZZY")
	p.PostLogin = append(append([]string{}, p.PostLogin...), "SEARCH=FUZZY")
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE Fbox").ok(t)
	cl.cmd("CREATE Fbox/Child").ok(t)
	raw := "From: a@example.com\r\nTo: b@example.com\r\nSubject: apple\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\nhello\r\n"
	cl.cmd(fmt.Sprintf("APPEND Fbox/Child \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n%s", len(raw), raw)).ok(t)
	uv := statusUIDValidity(t, cl, "Fbox/Child")
	tag, lines := multiLines(t, cl.cmd("ESEARCH IN (subtree Fbox) RETURN (ALL) FUZZY SUBJECT aple"))
	want := fmt.Sprintf(`* ESEARCH (TAG "%s" MAILBOX "Fbox/Child" UIDVALIDITY %d) UID ALL 1`, tag, uv)
	if len(lines) != 1 || lines[0] != want || strings.Contains(lines[0], "RELEVANCY") {
		t.Fatalf("fuzzy %q", lines)
	}
	tag, lines = multiLines(t, cl.cmd(`ESEARCH IN (subtree Fbox) RETURN (COUNT) CHARSET UTF-8 FUZZY SUBJECT aple`))
	want = fmt.Sprintf(`* ESEARCH (TAG "%s" MAILBOX "Fbox/Child" UIDVALIDITY %d) UID COUNT 1`, tag, uv)
	if len(lines) != 1 || lines[0] != want || strings.Contains(lines[0], "RELEVANCY") {
		t.Fatalf("fuzzy count %q", lines)
	}
	if _, lines = multiLines(t, cl.cmd("ESEARCH IN (personal) RETURN (ALL) FUZZY SUBJECT zq")); len(lines) != 0 {
		t.Fatalf("miss fuzzy %v", lines)
	}
}

func statusUIDValidity(t *testing.T, cl *imapClient, name string) uint32 {
	t.Helper()
	rp := cl.cmd("STATUS " + name + " (UIDVALIDITY)")
	rp.ok(t)
	if len(rp.untagged) != 1 {
		t.Fatalf("status %q", rp.untagged)
	}
	const key = "UIDVALIDITY "
	i := strings.Index(rp.untagged[0], key)
	if i < 0 {
		t.Fatalf("status %q", rp.untagged)
	}
	field := strings.TrimRight(rp.untagged[0][i+len(key):], ")")
	n, err := strconv.ParseUint(strings.TrimSpace(field), 10, 32)
	if err != nil {
		t.Fatalf("status %q: %v", rp.untagged[0], err)
	}
	return uint32(n)
}

func multiLines(t *testing.T, rp *reply) (string, []string) {
	t.Helper()
	if rp.status != "OK" {
		t.Fatalf("status %s %s untagged %q", rp.status, rp.tagLine, rp.untagged)
	}
	tag := strings.SplitN(rp.tagLine, " ", 2)[0]
	var lines []string
	for _, u := range rp.untagged {
		if !strings.HasPrefix(u, "* ESEARCH ") {
			t.Fatalf("unexpected %q", u)
		}
		if strings.Contains(u, " *") || strings.Contains(u, "*") && strings.Count(u, "*") != 1 {
			t.Fatalf("star in set %q", u)
		}
		lines = append(lines, u)
	}
	return tag, lines
}

func multiWant(tag string, names []string, uv map[string]uint32, item string, vals map[string]string) []string {
	var lines []string
	for _, name := range names {
		lines = append(lines, fmt.Sprintf(`* ESEARCH (TAG "%s" MAILBOX "%s" UIDVALIDITY %d) UID %s %s`, tag, name, uv[name], item, vals[name]))
	}
	return lines
}
