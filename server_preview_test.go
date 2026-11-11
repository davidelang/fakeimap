package main

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
)

func TestPreviewPersonality(t *testing.T) {
	p, err := loadPersonality("personalities/preview.json")
	if err != nil {
		t.Fatal(err)
	}
	if !listed(p.PreLogin, "PREVIEW") || !listed(p.PostLogin, "PREVIEW") {
		t.Fatal("preview.json missing PREVIEW")
	}
	if listed(p.GreetingCaps, "PREVIEW") {
		t.Fatal("greeting lists PREVIEW")
	}
}

func TestPreviewFetch(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/preview.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"PREVIEW"})
	cl.cmd("CREATE PrevBox").ok(t)
	cl.cmd("SELECT PrevBox").ok(t)

	long := strings.Repeat("x", 300)
	runes := strings.Repeat("é", 300)
	b64 := base64.StdEncoding.EncodeToString([]byte("Hello  Preview"))
	msgs := []string{
		textPart("base64", b64+"\r\n"),
		textPart("quoted-printable", "Hello=20=20QP\r\n"),
		textPart("7bit", "plain   seven\r\n\tmore\r\n"),
		textPart("", "  spaced \r\n"),
		textPart("8bit", long+"\r\n"),
		textPart("8bit", runes),
		textPart("7bit", "   \r\n\t"),
		htmlOnly(),
		htmlThenPlain(),
		twoPlains(),
		textPart("x-uuencode", "abc\r\n"),
	}
	for _, raw := range msgs {
		cl.cmd("APPEND PrevBox \"01-Jan-2024 00:00:00 +0000\" {" + strconv.Itoa(len(raw)) + "+}\r\n" + raw).ok(t)
	}
	want := []string{
		"Hello Preview",
		"Hello QP",
		"plain seven more",
		"spaced",
		strings.Repeat("x", 256),
		strings.Repeat("é", 256),
		"",
		"",
		"from plain",
		"first plain",
		"",
	}
	for i, exp := range want {
		seq := strconv.Itoa(i + 1)
		line := fetchLine(t, cl.cmd("FETCH "+seq+" (PREVIEW)").mustOK(t))
		if strings.Contains(line, `\Seen`) || strings.Contains(line, "FLAGS") {
			t.Fatalf("seq %s set seen: %s", seq, line)
		}
		if got := previewValue(t, line); got != exp {
			t.Fatalf("seq %s preview %q want %q", seq, got, exp)
		}
		if exp == "" && !strings.Contains(line, "PREVIEW NIL") {
			t.Fatalf("seq %s not NIL: %s", seq, line)
		}
	}
	uidLine := fetchLine(t, cl.cmd("UID FETCH 1 (PREVIEW)").mustOK(t))
	keys := fetchKeys(t, uidLine)
	if len(keys) < 2 || !containsStr(keys, "UID") || !containsStr(keys, "PREVIEW") {
		t.Fatalf("uid keys %v line %s", keys, uidLine)
	}
	if previewValue(t, uidLine) != "Hello Preview" {
		t.Fatalf("uid preview %s", uidLine)
	}
	bare := fetchLine(t, cl.cmd("FETCH 2 PREVIEW").mustOK(t))
	if previewValue(t, bare) != "Hello QP" {
		t.Fatalf("bare %s", bare)
	}
	flags := fetchLine(t, cl.cmd("FETCH 1 (FLAGS)").mustOK(t))
	if strings.Contains(flags, `\Seen`) {
		t.Fatalf("preview set seen: %s", flags)
	}
	lazy := cl.cmd("FETCH 1 (PREVIEW (LAZY))")
	if lazy.status != "BAD" {
		t.Fatalf("LAZY -> %s %s", lazy.status, lazy.tagLine)
	}
	cl.cmd("NOOP").ok(t)
}

func TestPreviewRequiresCap(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE PrevOff").ok(t)
	cl.cmd("SELECT PrevOff").ok(t)
	raw := textPart("7bit", "hello\r\n")
	cl.cmd("APPEND PrevOff \"01-Jan-2024 00:00:00 +0000\" {" + strconv.Itoa(len(raw)) + "+}\r\n" + raw).ok(t)
	rp := cl.cmd("FETCH 1 (PREVIEW)")
	if rp.status != "BAD" {
		t.Fatalf("PREVIEW -> %s", rp.status)
	}
	uid := cl.cmd("UID FETCH 1 (PREVIEW)")
	if uid.status != "BAD" {
		t.Fatalf("UID PREVIEW -> %s", uid.status)
	}
}

func htmlOnly() string {
	return "From: a@e.test\r\nTo: b@e.test\r\nSubject: h\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>hello html</p>\r\n"
}

func htmlThenPlain() string {
	return "From: a@e.test\r\nTo: b@e.test\r\nSubject: h\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=alt\r\n\r\n" +
		"--alt\r\nContent-Type: text/html\r\n\r\n<p>html body</p>\r\n" +
		"--alt\r\nContent-Type: text/plain\r\n\r\n  from   plain \r\n" +
		"--alt--\r\n"
}

func twoPlains() string {
	return "From: a@e.test\r\nTo: b@e.test\r\nSubject: h\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=bnd\r\n\r\n" +
		"--bnd\r\nContent-Type: text/plain\r\n\r\nfirst   plain\r\n" +
		"--bnd\r\nContent-Type: text/plain\r\n\r\nsecond plain\r\n" +
		"--bnd--\r\n"
}

func previewValue(t *testing.T, line string) string {
	t.Helper()
	for _, it := range parseFetchItems(line) {
		if it[0] != "PREVIEW" {
			continue
		}
		v := it[1]
		if v == "NIL" {
			return ""
		}
		if strings.HasPrefix(v, "\"") {
			s, err := strconv.Unquote(v)
			if err != nil {
				t.Fatalf("quote %q: %v", v, err)
			}
			return s
		}
		if strings.HasPrefix(v, "{") {
			k := strings.IndexByte(v, '}')
			n, err := strconv.Atoi(v[1:k])
			if err != nil {
				t.Fatal(err)
			}
			rest := v[k+1:]
			if strings.HasPrefix(rest, "\r\n") {
				rest = rest[2:]
			}
			if len(rest) < n {
				t.Fatalf("short literal in %s", line)
			}
			return rest[:n]
		}
		t.Fatalf("preview value %q", v)
	}
	t.Fatalf("no PREVIEW in %s", line)
	return ""
}
