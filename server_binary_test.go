package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBinaryPersonality(t *testing.T) {
	p, err := loadPersonality("personalities/binary.json")
	if err != nil {
		t.Fatal(err)
	}
	if !listed(p.PreLogin, "BINARY") || !listed(p.PostLogin, "BINARY") {
		t.Fatal("binary.json missing BINARY")
	}
	if listed(p.GreetingCaps, "BINARY") {
		t.Fatal("greeting lists BINARY")
	}
	for _, name := range []string{"URLAUTH=BINARY", "BINARY.SIZE"} {
		if listed(p.PreLogin, name) || listed(p.PostLogin, name) || listed(p.GreetingCaps, name) {
			t.Fatalf("binary.json lists %s", name)
		}
	}
	cyrus, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	if !listed(cyrus.PreLogin, "BINARY") || !listed(cyrus.PostLogin, "BINARY") || listed(cyrus.GreetingCaps, "BINARY") {
		t.Fatal("cyrus BINARY lists")
	}
	sasl, err := loadPersonality("personalities/sasl.json")
	if err != nil {
		t.Fatal(err)
	}
	if !listed(sasl.PreLogin, "BINARY") || !listed(sasl.PostLogin, "BINARY") || listed(sasl.GreetingCaps, "BINARY") {
		t.Fatal("sasl BINARY lists")
	}
	gaps, err := os.ReadFile("personalities/cyrus-3137.gaps.json")
	if err != nil {
		t.Fatal(err)
	}
	text := string(gaps)
	if strings.Contains(text, "\"BINARY\"") {
		t.Fatal("gaps still lists BINARY")
	}
	if !strings.Contains(text, "\"URLAUTH=BINARY\"") {
		t.Fatal("gaps dropped URLAUTH=BINARY")
	}
}

func TestBinaryFetch(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/binary.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"BINARY"})
	cl.cmd("CREATE BinBox").ok(t)
	cl.cmd("SELECT BinBox").ok(t)

	b64 := base64.StdEncoding.EncodeToString([]byte("HelloBinary"))
	msgs := []string{
		textPart("base64", b64+"\r\n"),
		textPart("quoted-printable", "Hello=20QP\r\n"),
		textPart("7bit", "plain7\r\n"),
		textPart("8bit", "eight\xff\r\n"),
		textPart("binary", "raw\x00bin\r\n"),
		textPart("", "noccte\r\n"),
		nestedMultipart(),
		textPart("base64", "!!!!\r\n"),
		textPart("quoted-printable", "bad\x01\r\n"),
		textPart("x-uuencode", "abc\r\n"),
	}
	for _, raw := range msgs {
		cl.cmd("APPEND BinBox \"01-Jan-2024 00:00:00 +0000\" {" + strconv.Itoa(len(raw)) + "+}\r\n" + raw).ok(t)
	}

	key, got := binaryItem(t, fetchLine(t, cl.cmd("FETCH 1 (BINARY.PEEK[1])").mustOK(t)))
	if key != "BINARY[1]" || string(got) != "HelloBinary" {
		t.Fatalf("b64 %s %q", key, got)
	}
	key, got = binaryItem(t, fetchLine(t, cl.cmd("UID FETCH 1 (BINARY.PEEK[1])").mustOK(t)))
	if key != "BINARY[1]" || string(got) != "HelloBinary" {
		t.Fatalf("uid peek %s %q", key, got)
	}
	uidLine := fetchLine(t, cl.cmd("UID FETCH 1 (BINARY.PEEK[1])").mustOK(t))
	if !strings.Contains(uidLine, "UID 1") {
		t.Fatalf("uid line %s", uidLine)
	}
	key, got = binaryItem(t, fetchLine(t, cl.cmd("FETCH 1 (BINARY[1]<0.5>)").mustOK(t)))
	if key != "BINARY[1]<0>" || string(got) != "Hello" {
		t.Fatalf("partial %s %q", key, got)
	}
	key, got = binaryItem(t, fetchLine(t, cl.cmd("FETCH 1 (BINARY.PEEK[1]<6.4>)").mustOK(t)))
	if key != "BINARY[1]<6>" || string(got) != "inar" {
		t.Fatalf("offset %s %q", key, got)
	}
	if strings.Contains(key, "<6.4>") || strings.Contains(key, "BINARY.PEEK") {
		t.Fatalf("token %s", key)
	}

	key, got = binaryItem(t, fetchLine(t, cl.cmd("FETCH 2 (BINARY.PEEK[1])").mustOK(t)))
	if key != "BINARY[1]" || string(got) != "Hello QP\r\n" {
		t.Fatalf("qp %s %q", key, got)
	}
	key, got = binaryItem(t, fetchLine(t, cl.cmd("FETCH 3 (BINARY.PEEK[1])").mustOK(t)))
	if key != "BINARY[1]" || string(got) != "plain7\r\n" {
		t.Fatalf("7bit %s %q", key, got)
	}
	key, got = binaryItem(t, fetchLine(t, cl.cmd("FETCH 4 (BINARY.PEEK[1])").mustOK(t)))
	if key != "BINARY[1]" || string(got) != "eight\xff\r\n" {
		t.Fatalf("8bit %s %q", key, got)
	}
	key, got = binaryItem(t, fetchLine(t, cl.cmd("FETCH 5 (BINARY.PEEK[1])").mustOK(t)))
	if key != "BINARY[1]" || string(got) != "raw\x00bin\r\n" {
		t.Fatalf("binary %s %q", key, got)
	}
	key, got = binaryItem(t, fetchLine(t, cl.cmd("FETCH 6 (BINARY.PEEK[1])").mustOK(t)))
	if key != "BINARY[1]" || string(got) != "noccte\r\n" {
		t.Fatalf("missing cte %s %q", key, got)
	}

	leaf, got := binaryItem(t, fetchLine(t, cl.cmd("FETCH 7 (BINARY.PEEK[1.1])").mustOK(t)))
	if leaf != "BINARY[1.1]" || string(got) != "leaf" {
		t.Fatalf("nested %s %q", leaf, got)
	}
	if cl.cmd("FETCH 7 (BINARY.PEEK[1])").status != "BAD" {
		t.Fatal("multipart part was accepted")
	}
	nilLine := fetchLine(t, cl.cmd("FETCH 7 (BINARY.PEEK[3])").mustOK(t))
	if !strings.Contains(nilLine, "BINARY[3] NIL") {
		t.Fatalf("missing part %s", nilLine)
	}

	for _, item := range []string{"BINARY.PEEK[]", "BINARY.PEEK[HEADER]", "BINARY.PEEK[TEXT]", "BINARY.PEEK[MIME]", "BINARY.PEEK[1.MIME]"} {
		rp := cl.cmd("FETCH 1 (" + item + ")")
		if rp.status != "BAD" || len(rp.untagged) != 0 {
			t.Fatalf("%s -> %s %v", item, rp.status, rp.untagged)
		}
	}
	for _, seq := range []string{"8", "9", "10"} {
		rp := cl.cmd("FETCH " + seq + " (BINARY.PEEK[1])")
		if rp.status != "NO" || !strings.Contains(rp.tagLine, "[UNKNOWN-CTE]") || len(rp.untagged) != 0 {
			t.Fatalf("msg %s -> %s %q %v", seq, rp.status, rp.tagLine, rp.untagged)
		}
	}
	flags := cl.cmd("FETCH 8 (FLAGS)")
	flags.ok(t)
	if strings.Contains(fetchLine(t, flags), `\Seen`) {
		t.Fatal("failed decode set \\Seen")
	}

	body := fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[1])").mustOK(t)))
	if !strings.Contains(string(body), b64) || strings.Contains(string(body), "HelloBinary") {
		t.Fatalf("body peek %q", body)
	}
}

func TestBinarySeen(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/binary.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE BinSeen").ok(t)
	cl.cmd("SELECT BinSeen").ok(t)
	raw := textPart("base64", base64.StdEncoding.EncodeToString([]byte("HelloBinary"))+"\r\n")
	cl.cmd("APPEND BinSeen \"01-Jan-2024 00:00:00 +0000\" {" + strconv.Itoa(len(raw)) + "+}\r\n" + raw).ok(t)
	cl.cmd("APPEND BinSeen \"01-Jan-2024 00:00:00 +0000\" {" + strconv.Itoa(len(raw)) + "+}\r\n" + raw).ok(t)

	peek := cl.cmd("FETCH 1 (BINARY.PEEK[1])")
	peek.ok(t)
	if strings.Contains(fetchLine(t, peek), `\Seen`) || strings.Contains(fetchLine(t, peek), "BINARY.PEEK") {
		t.Fatalf("peek %s", fetchLine(t, peek))
	}
	if strings.Contains(fetchLine(t, cl.cmd("FETCH 1 (FLAGS)").mustOK(t)), `\Seen`) {
		t.Fatal("peek stored \\Seen")
	}

	seen := cl.cmd("FETCH 1 (BINARY[1])")
	seen.ok(t)
	line := fetchLine(t, seen)
	keys := fetchKeys(t, line)
	if len(keys) < 2 || keys[0] != "FLAGS" || keys[1] != "BINARY[1]" || !strings.Contains(line, `\Seen`) || strings.Contains(line, "BINARY.PEEK") {
		t.Fatalf("seen %v %s", keys, line)
	}

	uidSeen := cl.cmd("UID FETCH 2 (BINARY[1])")
	uidSeen.ok(t)
	uline := fetchLine(t, uidSeen)
	ukeys := fetchKeys(t, uline)
	if len(ukeys) < 3 || ukeys[0] != "FLAGS" || !containsStr(ukeys, "UID") || !containsStr(ukeys, "BINARY[1]") || !strings.Contains(uline, `\Seen`) {
		t.Fatalf("uid seen %v %s", ukeys, uline)
	}

	ex := dial(t, bound.Listen)
	ex.cmd("LOGIN demo secret").ok(t)
	ex.cmd("EXAMINE BinSeen").ok(t)
	exam := ex.cmd("FETCH 1 (BINARY[1])")
	exam.ok(t)
	if strings.Contains(strings.Join(exam.untagged, "\n"), "FETCH (FLAGS ") {
		t.Fatalf("examine flags %v", exam.untagged)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "noseen.json")
	p, err := loadPersonality("personalities/binary.json")
	if err != nil {
		t.Fatal(err)
	}
	p.FetchSeenFlags = false
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
	cl.cmd("CREATE BinSep").ok(t)
	cl.cmd("SELECT BinSep").ok(t)
	cl.cmd("APPEND BinSep \"01-Jan-2024 00:00:00 +0000\" {" + strconv.Itoa(len(raw)) + "+}\r\n" + raw).ok(t)
	sep := cl.cmd("FETCH 1 (BINARY[1])")
	sep.ok(t)
	n, flagsLine := flagsUpdates(sep)
	data := fetchLine(t, sep)
	if n != 1 || !strings.Contains(flagsLine, `\Seen`) || strings.Contains(data, "FLAGS ") || !strings.Contains(data, "BINARY[1]") {
		t.Fatalf("separate flags n=%d flags=%q data=%q all=%v", n, flagsLine, data, sep.untagged)
	}
}

func TestBinaryRequiresCap(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/condstore.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE BinOff").ok(t)
	cl.cmd("SELECT BinOff").ok(t)
	raw := textPart("base64", base64.StdEncoding.EncodeToString([]byte("HelloBinary"))+"\r\n")
	cl.cmd("APPEND BinOff \"01-Jan-2024 00:00:00 +0000\" {" + strconv.Itoa(len(raw)) + "+}\r\n" + raw).ok(t)
	for _, item := range []string{"BINARY.PEEK[1]", "BINARY[1]", "BINARY.SIZE[1]"} {
		rp := cl.cmd("FETCH 1 (" + item + ")")
		if rp.status != "BAD" {
			t.Fatalf("%s -> %s", item, rp.status)
		}
	}
	body := fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[1])").mustOK(t)))
	if !strings.Contains(string(body), "SGVsbG9CaW5hcnk=") {
		t.Fatalf("encoded %q", body)
	}
}

func TestBinarySize(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/binary.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE BinSize").ok(t)
	cl.cmd("SELECT BinSize").ok(t)

	b64 := base64.StdEncoding.EncodeToString([]byte("HelloBinary"))
	msgs := []string{
		textPart("base64", b64+"\r\n"),
		textPart("quoted-printable", "Hello=20QP\r\n"),
		nestedMultipart(),
		textPart("base64", "!!!!\r\n"),
		textPart("quoted-printable", "bad\x01\r\n"),
		textPart("x-uuencode", "abc\r\n"),
	}
	for _, raw := range msgs {
		cl.cmd("APPEND BinSize \"01-Jan-2024 00:00:00 +0000\" {" + strconv.Itoa(len(raw)) + "+}\r\n" + raw).ok(t)
	}

	line := fetchLine(t, cl.cmd("FETCH 1 (BINARY.SIZE[1])").mustOK(t))
	key, num := sizeItem(t, line)
	_, peeked := binaryItem(t, fetchLine(t, cl.cmd("FETCH 1 (BINARY.PEEK[1])").mustOK(t)))
	if key != "BINARY.SIZE[1]" || num != "11" || num != strconv.Itoa(len(peeked)) || strings.Contains(line, "BINARY[1]") || strings.Contains(line, `\Seen`) {
		t.Fatalf("size %s %s %s", key, num, line)
	}
	uidLine := fetchLine(t, cl.cmd("UID FETCH 1 (BINARY.SIZE[1])").mustOK(t))
	ukey, unum := sizeItem(t, uidLine)
	if ukey != "BINARY.SIZE[1]" || unum != "11" || !strings.Contains(uidLine, "UID 1") || strings.Contains(uidLine, "BINARY[1]") {
		t.Fatalf("uid size %s %s %s", ukey, unum, uidLine)
	}
	if strings.Contains(fetchLine(t, cl.cmd("FETCH 1 (FLAGS)").mustOK(t)), `\Seen`) {
		t.Fatal("BINARY.SIZE stored \\Seen")
	}

	qpLine := fetchLine(t, cl.cmd("FETCH 2 (BINARY.SIZE[1])").mustOK(t))
	qpKey, qpNum := sizeItem(t, qpLine)
	_, qpBody := binaryItem(t, fetchLine(t, cl.cmd("FETCH 2 (BINARY.PEEK[1])").mustOK(t)))
	if qpKey != "BINARY.SIZE[1]" || qpNum != strconv.Itoa(len("Hello QP\r\n")) || qpNum != strconv.Itoa(len(qpBody)) {
		t.Fatalf("qp size %s %s len %d body %q", qpKey, qpNum, len(qpBody), qpBody)
	}

	leafLine := fetchLine(t, cl.cmd("FETCH 3 (BINARY.SIZE[1.1])").mustOK(t))
	leafKey, leafNum := sizeItem(t, leafLine)
	_, leafBody := binaryItem(t, fetchLine(t, cl.cmd("FETCH 3 (BINARY.PEEK[1.1])").mustOK(t)))
	if leafKey != "BINARY.SIZE[1.1]" || leafNum != "4" || leafNum != strconv.Itoa(len(leafBody)) {
		t.Fatalf("leaf size %s %s len %d", leafKey, leafNum, len(leafBody))
	}

	for _, item := range []string{"BINARY.SIZE[1]<0.5>", "BINARY.SIZE[]", "BINARY.SIZE[HEADER]", "BINARY.SIZE[TEXT]", "BINARY.SIZE[MIME]", "BINARY.SIZE[1.MIME]"} {
		rp := cl.cmd("FETCH 1 (" + item + ")")
		if rp.status != "BAD" || len(rp.untagged) != 0 {
			t.Fatalf("%s -> %s %v", item, rp.status, rp.untagged)
		}
	}
	mp := cl.cmd("FETCH 3 (BINARY.SIZE[1])")
	if mp.status != "BAD" || len(mp.untagged) != 0 {
		t.Fatalf("multipart %s %v", mp.status, mp.untagged)
	}
	nilLine := fetchLine(t, cl.cmd("FETCH 3 (BINARY.SIZE[3])").mustOK(t))
	if !strings.Contains(nilLine, "BINARY.SIZE[3] NIL") {
		t.Fatalf("missing part %s", nilLine)
	}
	for _, seq := range []string{"4", "5", "6"} {
		rp := cl.cmd("FETCH " + seq + " (BINARY.SIZE[1])")
		if rp.status != "NO" || !strings.Contains(rp.tagLine, "[UNKNOWN-CTE]") || len(rp.untagged) != 0 {
			t.Fatalf("msg %s -> %s %q %v", seq, rp.status, rp.tagLine, rp.untagged)
		}
	}
	flags := cl.cmd("FETCH 4 (FLAGS)")
	flags.ok(t)
	if strings.Contains(fetchLine(t, flags), `\Seen`) {
		t.Fatal("failed size set \\Seen")
	}
}

func textPart(cte, body string) string {
	var b strings.Builder
	b.WriteString("From: a@e.test\r\nTo: b@e.test\r\nSubject: s\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=us-ascii\r\n")
	if cte != "" {
		b.WriteString("Content-Transfer-Encoding: " + cte + "\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}

func nestedMultipart() string {
	leaf := base64.StdEncoding.EncodeToString([]byte("leaf"))
	return "From: a@e.test\r\nTo: b@e.test\r\nSubject: m\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=bnd\r\n\r\n" +
		"--bnd\r\nContent-Type: multipart/alternative; boundary=alt\r\n\r\n" +
		"--alt\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		leaf + "\r\n--alt--\r\n" +
		"--bnd\r\nContent-Type: text/plain\r\n\r\nsecond\r\n" +
		"--bnd--\r\n"
}

func binaryItem(t *testing.T, line string) (string, []byte) {
	t.Helper()
	for _, it := range parseFetchItems(line) {
		if !strings.HasPrefix(it[0], "BINARY") {
			continue
		}
		v := it[1]
		if v == "NIL" {
			return it[0], nil
		}
		if !strings.HasPrefix(v, "{") {
			t.Fatalf("value %q in %s", v, line)
		}
		k := strings.IndexByte(v, '}')
		n := 0
		for _, c := range v[1:k] {
			n = n*10 + int(c-'0')
		}
		rest := v[k+1:]
		if strings.HasPrefix(rest, "\r\n") {
			rest = rest[2:]
		}
		if len(rest) < n {
			t.Fatalf("short literal in %s", line)
		}
		return it[0], []byte(rest[:n])
	}
	t.Fatalf("no BINARY in %s", line)
	return "", nil
}

func sizeItem(t *testing.T, line string) (string, string) {
	t.Helper()
	for _, it := range parseFetchItems(line) {
		if !strings.HasPrefix(it[0], "BINARY.SIZE") {
			continue
		}
		if strings.HasPrefix(it[1], "{") || strings.HasPrefix(it[1], "\"") {
			t.Fatalf("not a number %q in %s", it[1], line)
		}
		return it[0], it[1]
	}
	t.Fatalf("no BINARY.SIZE in %s", line)
	return "", ""
}
