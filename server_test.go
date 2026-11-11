package main

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDecodeSubject(t *testing.T) {
	got := decodeHdr("=?utf-8?Q?Na=C3=AFve_caf=C3=A9?=")
	if got != "Naïve café" {
		t.Fatalf("decoded %q", got)
	}
}

func TestPersonalityData(t *testing.T) {
	p, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	if p.Delimiter != "/" || !p.LogoutBye || p.Greeting == "" || len(p.FetchItemOrder) == 0 {
		t.Fatalf("personality fields: %+v", p)
	}
	want := []string{"IMAP4rev1", "AUTH=PLAIN", "AUTH=LOGIN", "ENABLE", "IDLE", "LITERAL+", "MOVE", "NAMESPACE", "SASL-IR", "UIDPLUS", "UNSELECT", "CONDSTORE", "QRESYNC", "LIST-EXTENDED", "CHILDREN", "SPECIAL-USE", "LIST-STATUS", "CREATE-SPECIAL-USE", "ESEARCH", "PARTIAL", "COMPRESS=DEFLATE", "SORT", "ESORT", "SORT=DISPLAY", "THREAD=REFERENCES", "THREAD=ORDEREDSUBJECT", "THREAD=REFS", "BINARY"}
	if strings.Join(p.PreLogin, " ") != strings.Join(want, " ") {
		t.Fatalf("pre-login %#v", p.PreLogin)
	}
	for _, c := range append(append([]string{}, p.PreLogin...), p.PostLogin...) {
		u := strings.ToUpper(c)
		if strings.Contains(u, "SCRAM") || u == "COMPRESS" || u == "AUTH=DIGEST-MD5" || u == "STARTTLS" {
			t.Fatalf("personality lists %s", c)
		}
	}
}

func TestEphemeralListenAndEmptyLog(t *testing.T) {
	srv, bound := start(t, Config{Listen: "127.0.0.1:0"})
	if srv.logf != nil {
		t.Fatal("empty log path created a log")
	}
	host, port, err := net.SplitHostPort(bound.Listen)
	if err != nil || host != "127.0.0.1" || port == "0" || port == "" {
		t.Fatalf("bound %q", bound.Listen)
	}
	cl := dial(t, bound.Listen)
	if !strings.Contains(cl.greeting, "CAPABILITY") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	for _, c := range capsOf(cl.greeting) {
		if strings.EqualFold(c, "STARTTLS") {
			t.Fatal("STARTTLS advertised without a certificate")
		}
	}
	cl.cmd("NOOP").ok(t)
	cl.cmd("LOGOUT").ok(t)
}

func TestLogLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "imap.log")
	_, bound := start(t, Config{Listen: "127.0.0.1:0", LogPath: logPath})
	cl := dial(t, bound.Listen)
	cl.cmd("NOOP").ok(t)
	cl.cmd("LOGOUT")
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var sawCmd, sawReply bool
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		var rec struct {
			Kind string `json:"kind"`
			Conn int    `json:"conn"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("log line %s: %v", line, err)
		}
		if rec.Conn < 1 || rec.Text == "" {
			t.Fatalf("log rec %+v", rec)
		}
		if rec.Kind == "command" && strings.Contains(rec.Text, "NOOP") {
			sawCmd = true
		}
		if rec.Kind == "reply" && strings.Contains(rec.Text, "NOOP completed") {
			sawReply = true
		}
	}
	if !sawCmd || !sawReply {
		t.Fatalf("log missing command/reply:\n%s", b)
	}
}

func TestCleartextAuthAndCaps(t *testing.T) {
	cert, key := writeCert(t)
	_, bound := start(t, Config{
		Listen:            "127.0.0.1:0",
		ImplicitTLSListen: "127.0.0.1:0",
		CertFile:          cert,
		KeyFile:           key,
	})
	cl := dial(t, bound.Listen)
	pre := capsOf(cl.greeting)
	requireCaps(t, pre, []string{"IMAP4rev1", "AUTH=PLAIN", "ENABLE", "LITERAL+", "SASL-IR", "STARTTLS"})
	for _, c := range pre {
		if strings.EqualFold(c, "IDLE") || strings.EqualFold(c, "NAMESPACE") {
			t.Fatalf("greeting cap %s", c)
		}
	}
	forbidCaps(t, pre)
	capRp := cl.cmd("CAPABILITY")
	capRp.ok(t)
	requireCaps(t, capsOf(capRp.untagged[0]), []string{"STARTTLS", "AUTH=PLAIN"})

	bad := cl.cmd("LOGIN demo wrong")
	if bad.status != "NO" || !strings.Contains(bad.tagLine, "AUTHENTICATIONFAILED") {
		t.Fatalf("bad login: %+v", bad)
	}
	good := cl.cmd("LOGIN demo secret")
	good.ok(t)
	post := capsOf(good.tagLine)
	requireCaps(t, post, []string{"IMAP4rev1", "LITERAL+", "IDLE", "NAMESPACE", "UIDPLUS", "MOVE", "UNSELECT", "ENABLE"})
	for _, c := range post {
		if strings.EqualFold(c, "AUTH=PLAIN") || strings.EqualFold(c, "STARTTLS") || strings.EqualFold(c, "SASL-IR") || strings.Contains(strings.ToUpper(c), "SCRAM") {
			t.Fatalf("post-login cap %s in %v", c, post)
		}
	}
	forbidCaps(t, post)

	cl2 := dial(t, bound.Listen)
	cl2.cmd("AUTHENTICATE PLAIN " + plainToken("demo", "secret")).ok(t)
	cl3 := dial(t, bound.Listen)
	cl3.send("b1 AUTHENTICATE PLAIN\r\n")
	cont := cl3.readRec()
	if !strings.HasPrefix(strings.TrimRight(cont, "\r\n"), "+") {
		t.Fatalf("challenge %q", cont)
	}
	cl3.send(plainToken("demo", "nope") + "\r\n")
	bad3 := cl3.readTag("b1")
	if bad3.status != "NO" {
		t.Fatalf("plain reject %+v", bad3)
	}
	cl3.send("b2 AUTHENTICATE PLAIN\r\n")
	if c := cl3.readRec(); !strings.HasPrefix(strings.TrimRight(c, "\r\n"), "+") {
		t.Fatalf("challenge %q", c)
	}
	cl3.send(plainToken("demo", "secret") + "\r\n")
	cl3.readTag("b2").ok(t)

	cl4 := dial(t, bound.Listen)
	cl4.cmd("STARTTLS").ok(t)
	tc := tls.Client(cl4.c, &tls.Config{InsecureSkipVerify: true})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	cl4.c = tc
	cl4.r = bufio.NewReader(tc)
	cl4.w = bufio.NewWriter(tc)
	after := cl4.cmd("CAPABILITY")
	after.ok(t)
	for _, c := range capsOf(after.untagged[0]) {
		if strings.EqualFold(c, "STARTTLS") {
			t.Fatal("STARTTLS still advertised")
		}
	}
	cl4.cmd("LOGIN demo secret").ok(t)

	cl5 := dialTLS(t, bound.ImplicitTLSListen)
	for _, c := range capsOf(cl5.greeting) {
		if strings.EqualFold(c, "STARTTLS") {
			t.Fatal("implicit listener advertised STARTTLS")
		}
	}
	cl5.cmd("LOGIN demo secret").ok(t)
}

func TestLogoutBye(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	rp := cl.cmd("LOGOUT")
	rp.ok(t)
	if !hasPrefix(rp.untagged, "* BYE") {
		t.Fatalf("missing BYE: %+v", rp.untagged)
	}

	dir := t.TempDir()
	p, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	p.LogoutBye = false
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "p.json")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl = dial(t, bound.Listen)
	rp = cl.cmd("LOGOUT")
	rp.ok(t)
	for _, u := range rp.untagged {
		if strings.HasPrefix(u, "* BYE") {
			t.Fatalf("unexpected BYE %q", u)
		}
	}
}

func TestSearch(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	ids := searchIDs(t, cl.cmd("SEARCH ALL"))
	if strings.Join(ids, " ") != "1 2" {
		t.Fatalf("ALL %v", ids)
	}
	if got := searchIDs(t, cl.cmd("SEARCH SEEN")); strings.Join(got, " ") != "1" {
		t.Fatalf("SEEN %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH UNSEEN")); strings.Join(got, " ") != "2" {
		t.Fatalf("UNSEEN %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH SUBJECT "Naïve café"`)); strings.Join(got, " ") != "1" {
		t.Fatalf("encoded subject %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH FROM sender@example.com")); strings.Join(got, " ") != "1 2" {
		t.Fatalf("FROM %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH TO demo@example.com")); strings.Join(got, " ") != "1 2" {
		t.Fatalf("TO %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH ON 10-Nov-2025")); strings.Join(got, " ") != "1 2" {
		t.Fatalf("ON %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BEFORE 10-Nov-2025")); len(got) != 0 {
		t.Fatalf("BEFORE %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH SINCE 10-Nov-2025")); strings.Join(got, " ") != "1 2" {
		t.Fatalf("SINCE %v", got)
	}
	sz := fetchSize(t, cl.cmd("FETCH 1 (RFC822.SIZE)"))
	if got := searchIDs(t, cl.cmd(fmt.Sprintf("SEARCH LARGER %d", sz-1))); !containsStr(got, "1") {
		t.Fatalf("LARGER %v size %d", got, sz)
	}
	if got := searchIDs(t, cl.cmd(fmt.Sprintf("SEARCH SMALLER %d", sz))); containsStr(got, "1") {
		t.Fatalf("SMALLER included 1: %v", got)
	}
	if got := searchIDs(t, cl.cmd("UID SEARCH UID 2")); strings.Join(got, " ") != "2" {
		t.Fatalf("UID %v", got)
	}
	if got := searchIDs(t, cl.cmd("UID SEARCH UID 1:2")); strings.Join(got, " ") != "1 2" {
		t.Fatalf("UID set %v", got)
	}
	cl.cmd("STORE 2 +FLAGS (\\Flagged \\Deleted)").ok(t)
	if got := searchIDs(t, cl.cmd("SEARCH FLAGGED")); strings.Join(got, " ") != "2" {
		t.Fatalf("FLAGGED %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH UNFLAGGED")); strings.Join(got, " ") != "1" {
		t.Fatalf("UNFLAGGED %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH DELETED")); strings.Join(got, " ") != "2" {
		t.Fatalf("DELETED %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH UNDELETED")); strings.Join(got, " ") != "1" {
		t.Fatalf("UNDELETED %v", got)
	}

	cl.cmd("SELECT INBOX/Generated").ok(t)
	if got := searchIDs(t, cl.cmd("SEARCH ON 01-Jan-2020")); strings.Join(got, " ") != "1" {
		t.Fatalf("gen ON %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH SINCE 02-Jan-2020")); strings.Join(got, " ") != "2 3" {
		t.Fatalf("gen SINCE %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH BEFORE 02-Jan-2020")); strings.Join(got, " ") != "1" {
		t.Fatalf("gen BEFORE %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH FROM alice")); strings.Join(got, " ") != "1 2 3" {
		t.Fatalf("gen FROM %v", got)
	}
	if got := searchIDs(t, cl.cmd(`SEARCH SUBJECT "Generated 2"`)); strings.Join(got, " ") != "2" {
		t.Fatalf("gen SUBJECT %v", got)
	}
	if got := searchIDs(t, cl.cmd("SEARCH SEEN")); len(got) != 0 {
		t.Fatalf("gen SEEN %v", got)
	}
	cl.cmd("STORE 1 +FLAGS (\\Seen \\Flagged \\Deleted)").ok(t)
	if got := searchIDs(t, cl.cmd("SEARCH UNSEEN")); strings.Join(got, " ") != "2 3" {
		t.Fatalf("gen UNSEEN %v", got)
	}

	for _, bad := range []string{"SEARCH TEXT hello", "SEARCH NEW", "SEARCH NOT SEEN", "SEARCH OR SEEN UNSEEN", "SEARCH 1", "SEARCH HEADER FROM x"} {
		rp := cl.cmd(bad)
		if rp.status != "BAD" {
			t.Fatalf("%s -> %s", bad, rp.status)
		}
	}
	if got := searchIDs(t, cl.cmd("SEARCH CHARSET UTF-8 ALL")); strings.Join(got, " ") != "1 2 3" {
		t.Fatalf("CHARSET %v", got)
	}
	cl.cmd("NOOP").ok(t)
}

func TestMailboxFetchAndStore(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	ns := cl.cmd("NAMESPACE")
	ns.ok(t)
	if len(ns.untagged) != 1 || ns.untagged[0] != `* NAMESPACE (("" "/")) (("Other Users/" "/")) (("Shared Folders/" "/"))` {
		t.Fatalf("namespace %q", ns.untagged)
	}
	root := cl.cmd(`LIST "" ""`)
	root.ok(t)
	if len(root.untagged) != 1 || !strings.Contains(root.untagged[0], `\Noselect`) || !strings.Contains(root.untagged[0], `"/"`) {
		t.Fatalf("list root %q", root.untagged)
	}
	all := cl.cmd(`LIST "" "*"`)
	all.ok(t)
	joined := strings.Join(all.untagged, "\n")
	sawInbox, sawGen := false, false
	for _, line := range strings.Split(joined, "\n") {
		if strings.HasSuffix(line, `"/" INBOX`) {
			sawInbox = true
		}
		if strings.HasSuffix(line, `"/" INBOX/Generated`) {
			sawGen = true
		}
	}
	if !sawInbox || !sawGen {
		t.Fatalf("list %s", joined)
	}
	pct := cl.cmd(`LIST "" "%"`)
	pct.ok(t)
	pj := strings.Join(pct.untagged, "\n")
	if !strings.Contains(pj, " INBOX") || strings.Contains(pj, "Generated") {
		t.Fatalf("list %% %s", pj)
	}
	cl.cmd("CREATE INBOX").no(t)
	cl.cmd("CREATE INBOX.New").ok(t)
	cl.cmd("CREATE INBOX.New").no(t)
	if !strings.Contains(strings.Join(cl.cmd(`LIST "" "*"`).untagged, "\n"), "INBOX.New") {
		t.Fatal("missing created mailbox")
	}
	cl.cmd("DELETE INBOX").no(t)
	cl.cmd("DELETE INBOX.New").ok(t)
	cl.cmd("CREATE INBOX.New").ok(t)
	cl.cmd("RENAME INBOX.New INBOX.Renamed").ok(t)
	listed := strings.Join(cl.cmd(`LIST "" "*"`).untagged, "\n")
	if strings.Contains(listed, "INBOX.New") || !strings.Contains(listed, "INBOX.Renamed") {
		t.Fatalf("rename list %s", listed)
	}
	cl.cmd("DELETE INBOX.Renamed").ok(t)

	st := cl.cmd("STATUS INBOX (MESSAGES RECENT UIDNEXT UIDVALIDITY UNSEEN)")
	st.ok(t)
	if len(st.untagged) != 1 || !strings.Contains(st.untagged[0], "MESSAGES 2") || !strings.Contains(st.untagged[0], "UIDNEXT 3") || !strings.Contains(st.untagged[0], "UIDVALIDITY 1") || !strings.Contains(st.untagged[0], "UNSEEN 1") {
		t.Fatalf("status %q", st.untagged)
	}

	sel := cl.cmd("SELECT INBOX")
	sel.ok(t)
	if !strings.Contains(sel.tagLine, "[READ-WRITE]") {
		t.Fatalf("select tag %s", sel.tagLine)
	}
	exists, recent, uidv, uidnext := parseSelect(t, sel)
	if exists != 2 || recent != 2 || uidv != 1 || uidnext != 3 {
		t.Fatalf("select exists=%d recent=%d uidv=%d next=%d", exists, recent, uidv, uidnext)
	}
	ex := cl.cmd("EXAMINE INBOX/Generated")
	ex.ok(t)
	if !strings.Contains(ex.tagLine, "[READ-ONLY]") {
		t.Fatalf("examine %s", ex.tagLine)
	}
	if exst := cl.cmd(`STORE 1 +FLAGS (\Seen)`); exst.status != "NO" {
		t.Fatalf("examine store %s", exst.status)
	}
	cl.cmd("FETCH 1 (UID)").ok(t)
	cl.cmd("CHECK").ok(t)
	cl.cmd("NOOP").ok(t)

	cl.cmd("SELECT INBOX").ok(t)
	one := cl.cmd("FETCH 1 (BODYSTRUCTURE)")
	one.ok(t)
	bs1 := strings.ToUpper(fetchLine(t, one))
	if !strings.Contains(bs1, `"TEXT"`) || !strings.Contains(bs1, `"PLAIN"`) || strings.Contains(bs1, `"MIXED"`) {
		t.Fatalf("single bodystructure %s", bs1)
	}
	two := cl.cmd("FETCH 2 (BODYSTRUCTURE)")
	two.ok(t)
	bs2 := strings.ToUpper(fetchLine(t, two))
	if !strings.Contains(bs2, `"MIXED"`) || !strings.Contains(bs2, `"PLAIN"`) || !strings.Contains(bs2, `"HTML"`) {
		t.Fatalf("multipart bodystructure %s", bs2)
	}
	cl.cmd("SELECT INBOX/Generated").ok(t)
	gen := cl.cmd("FETCH 1 (BODYSTRUCTURE)")
	gen.ok(t)
	bsg := strings.ToUpper(fetchLine(t, gen))
	if !strings.Contains(bsg, `"TEXT"`) || !strings.Contains(bsg, `"PLAIN"`) || strings.Contains(bsg, `"MIXED"`) {
		t.Fatalf("generated bodystructure %s", bsg)
	}
	ord := cl.cmd("FETCH 1 (RFC822.SIZE UID FLAGS INTERNALDATE)")
	ord.ok(t)
	keys := fetchKeys(t, fetchLine(t, ord))
	if strings.Join(keys, " ") != "FLAGS UID INTERNALDATE RFC822.SIZE" {
		t.Fatalf("fetch order %v", keys)
	}
	body1 := fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY[])").mustOK(t)))
	body2 := fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY[])").mustOK(t)))
	if !bytes.Equal(body1, body2) || !bytes.Contains(body1, []byte("uid=1")) {
		t.Fatalf("generated bytes differ or missing uid: %q", body1)
	}

	cl.cmd("SELECT INBOX").ok(t)
	peek := cl.cmd("FETCH 2 (FLAGS BODY.PEEK[TEXT])")
	peek.ok(t)
	nPeek, peekFlags := flagsUpdates(peek)
	if nPeek != 1 || strings.Contains(peekFlags, `\Seen`) {
		t.Fatalf("peek set seen: n=%d line=%q all=%v", nPeek, peekFlags, peek.untagged)
	}
	seen := cl.cmd("FETCH 2 (FLAGS BODY[TEXT])")
	seen.ok(t)
	nSeen, seenLine := flagsUpdates(seen)
	if nSeen != 1 || !strings.Contains(seenLine, `\Seen`) || !strings.Contains(seenLine, "BODY[TEXT]") {
		t.Fatalf("body flags: n=%d line=%q all=%v", nSeen, seenLine, seen.untagged)
	}
	for _, item := range []string{"ALL BODY[TEXT]", "FAST RFC822.TEXT", "FULL BODY[TEXT]"} {
		cl.cmd(`STORE 2 -FLAGS (\Seen)`).ok(t)
		r := cl.cmd("FETCH 2 (" + item + ")")
		r.ok(t)
		n, line := flagsUpdates(r)
		if n != 1 || !strings.Contains(line, `\Seen`) {
			t.Fatalf("%s flags: n=%d line=%q all=%v", item, n, line, r.untagged)
		}
	}
	for _, item := range []string{"BODY[TEXT]", "RFC822.TEXT"} {
		cl.cmd(`STORE 2 -FLAGS (\Seen)`).ok(t)
		r := cl.cmd("FETCH 2 (" + item + ")")
		r.ok(t)
		n, line := flagsUpdates(r)
		if n != 1 || !strings.Contains(line, `\Seen`) || strings.Contains(line, "UID ") {
			t.Fatalf("%s flags: n=%d line=%q all=%v", item, n, line, r.untagged)
		}
	}
	peekKeys := fetchKeys(t, fetchLine(t, peek))
	if len(peekKeys) < 2 || peekKeys[0] != "FLAGS" {
		t.Fatalf("peek order %v", peekKeys)
	}

	en := cl.cmd("ENABLE CONDSTORE QRESYNC")
	en.ok(t)
	joinedEn := strings.ToUpper(strings.Join(en.untagged, " "))
	if !strings.Contains(joinedEn, "ENABLED") || !strings.Contains(joinedEn, "CONDSTORE") || !strings.Contains(joinedEn, "QRESYNC") {
		t.Fatalf("enable %q", en.untagged)
	}
	cl.cmd("ENABLE CONDSTORE").ok(t)
	plain := cl.cmd("ENABLE IMAP4rev1")
	plain.ok(t)
	for _, u := range plain.untagged {
		if strings.Contains(strings.ToUpper(u), "ENABLED") {
			t.Fatalf("enabled IMAP4rev1: %q", u)
		}
	}
}

func TestExamineFetchDoesNotSetSeen(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	ex := dial(t, bound.Listen)
	ex.cmd("LOGIN demo secret").ok(t)
	exam := ex.cmd("EXAMINE INBOX")
	exam.ok(t)
	if !strings.Contains(exam.tagLine, "[READ-ONLY]") {
		t.Fatalf("examine %s", exam.tagLine)
	}
	for _, item := range []string{"BODY[TEXT]", "RFC822", "RFC822.TEXT"} {
		r := ex.cmd("FETCH 2 (" + item + ")")
		r.ok(t)
		if strings.Contains(strings.Join(r.untagged, "\n"), "FETCH (FLAGS ") {
			t.Fatalf("examine %s sent flags: %v", item, r.untagged)
		}
	}

	rw := dial(t, bound.Listen)
	rw.cmd("LOGIN demo secret").ok(t)
	rw.cmd("SELECT INBOX").ok(t)
	before := rw.cmd("FETCH 2 (FLAGS)")
	before.ok(t)
	if strings.Contains(fetchLine(t, before), `\Seen`) {
		t.Fatalf("examine stored seen: %s", fetchLine(t, before))
	}
	got := rw.cmd("FETCH 2 (RFC822.TEXT)")
	got.ok(t)
	nGot, gotLine := flagsUpdates(got)
	if nGot != 1 || !strings.Contains(gotLine, `\Seen`) {
		t.Fatalf("read-write fetch flags: n=%d line=%q all=%v", nGot, gotLine, got.untagged)
	}
}

func TestUIDFetchSeenIncludesUID(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("SELECT INBOX").ok(t)

	plain := cl.cmd("UID FETCH 2 (RFC822.TEXT)")
	plain.ok(t)
	nFlags, flagsLine := flagsUpdates(plain)
	if nFlags != 1 || !strings.Contains(flagsLine, `\Seen`) || !strings.Contains(flagsLine, "UID 2") {
		t.Fatalf("uid fetch seen line: n=%d line=%q all=%v", nFlags, flagsLine, plain.untagged)
	}

	cl.cmd(`UID STORE 2 -FLAGS (\Seen)`).ok(t)
	both := cl.cmd("UID FETCH 2 (FLAGS RFC822.TEXT)")
	both.ok(t)
	nFlags, flagsLine = flagsUpdates(both)
	if nFlags != 1 || !strings.Contains(flagsLine, `\Seen`) || !strings.Contains(flagsLine, "UID 2") {
		t.Fatalf("uid fetch duplicated flags: n=%d line=%q all=%v", nFlags, flagsLine, both.untagged)
	}
}

func flagsUpdates(rp *reply) (int, string) {
	n := 0
	line := ""
	for _, u := range rp.untagged {
		if strings.Contains(u, "FETCH (FLAGS ") {
			n++
			line = u
		}
	}
	return n, line
}

func TestExpungeCopyMove(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	sel := cl.cmd("SELECT INBOX").mustOK(t)
	_, _, uidv, _ := parseSelect(t, sel)
	cl.cmd(`STORE 1 +FLAGS (\Deleted)`).ok(t)
	cl.cmd("UNSELECT").ok(t)
	sel = cl.cmd("SELECT INBOX").mustOK(t)
	exists, _, uidv2, _ := parseSelect(t, sel)
	if exists != 2 || uidv2 != uidv {
		t.Fatalf("unselect expunged or uidv changed: exists %d uidv %d/%d", exists, uidv, uidv2)
	}
	if uid := fetchUID(t, cl.cmd("FETCH 1 (UID)")); uid != 1 {
		t.Fatalf("uid %d", uid)
	}
	cl.cmd("CLOSE").ok(t)
	sel = cl.cmd("SELECT INBOX").mustOK(t)
	exists, _, uidv3, _ := parseSelect(t, sel)
	if exists != 1 || uidv3 != uidv {
		t.Fatalf("close exists %d uidv %d", exists, uidv3)
	}
	if uid := fetchUID(t, cl.cmd("FETCH 1 (UID)")); uid != 2 {
		t.Fatalf("surviving uid %d", uid)
	}
	msg := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: appended\r\n\r\nhello\r\n")
	app := cl.cmd(fmt.Sprintf("APPEND INBOX (\\Seen) \"11-Nov-2025 00:00:00 +0000\" {%d+}\r\n%s", len(msg), msg))
	app.ok(t)
	if !strings.Contains(app.tagLine, "[APPENDUID 1 ") {
		t.Fatalf("appenduid %s", app.tagLine)
	}
	cl.cmd("APPEND INBOX {5+}\r\nhello").ok(t)
	sel = cl.cmd("SELECT INBOX").mustOK(t)
	exists, _, _, _ = parseSelect(t, sel)
	if exists != 3 {
		t.Fatalf("exists after append %d", exists)
	}
	// seq 1 uid 2, seq 2 new, seq 3 new. Delete the middle one.
	mid := fetchUID(t, cl.cmd("FETCH 2 (UID)"))
	last := fetchUID(t, cl.cmd("FETCH 3 (UID)"))
	cl.cmd(`STORE 2 +FLAGS (\Deleted)`).ok(t)
	ex := cl.cmd("EXPUNGE")
	ex.ok(t)
	if !hasPrefix(ex.untagged, "* 2 EXPUNGE") {
		t.Fatalf("expunge %q", ex.untagged)
	}
	if fetchUID(t, cl.cmd("FETCH 1 (UID)")) != 2 || fetchUID(t, cl.cmd("FETCH 2 (UID)")) != last {
		t.Fatal("sequence/uid mismatch after expunge")
	}
	if fetchUID(t, cl.cmd("FETCH 2 (UID)")) == mid {
		t.Fatal("expunged uid still present")
	}
	sel = cl.cmd("SELECT INBOX").mustOK(t)
	if _, _, uv, _ := parseSelect(t, sel); uv != uidv {
		t.Fatal("uidvalidity changed")
	}

	cl.cmd("CREATE INBOX.Dest").ok(t)
	srcUID := fetchUID(t, cl.cmd("FETCH 1 (UID)"))
	cp := cl.cmd("COPY 1 INBOX.Dest")
	cp.ok(t)
	if !strings.Contains(cp.tagLine, "[COPYUID ") {
		t.Fatalf("copyuid %s", cp.tagLine)
	}
	if fetchUID(t, cl.cmd("FETCH 1 (UID)")) != srcUID {
		t.Fatal("copy removed source")
	}
	mv := cl.cmd("UID MOVE " + strconv.Itoa(int(srcUID)) + " INBOX.Dest")
	mv.ok(t)
	if !strings.Contains(strings.Join(mv.untagged, "\n"), "[COPYUID ") {
		t.Fatalf("move copyuid %s", mv.tagLine)
	}
	if strings.Contains(strings.Join(searchIDs(t, cl.cmd("UID SEARCH ALL")), " "), strconv.Itoa(int(srcUID))) {
		t.Fatal("moved uid still in source")
	}
	cl.cmd("SELECT INBOX.Dest").ok(t)
	dst := cl.cmd("UID SEARCH ALL")
	if len(searchIDs(t, dst)) < 2 {
		t.Fatalf("dest %v", dst.untagged)
	}
	cl.cmd("SELECT INBOX").ok(t)
	cl.cmd(`UID STORE 1:* +FLAGS (\Deleted)`)
	cl.cmd("UID EXPUNGE 1:*").ok(t)
	if exists, _, _, _ = parseSelect(t, cl.cmd("SELECT INBOX").mustOK(t)); exists != 0 {
		t.Fatalf("uid expunge left %d", exists)
	}
}

func TestPipelineLiteralsAndIdle(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.send("p1 NOOP\r\np2 CAPABILITY\r\n")
	r1 := cl.readTag("p1")
	r2 := cl.readTag("p2")
	r1.ok(t)
	r2.ok(t)
	if len(r1.untagged) != 0 {
		t.Fatalf("noop untagged before its tag: %q", r1.untagged)
	}

	cl.send("p3 LOGIN {4+}\r\ndemo {6+}\r\nsecret\r\n")
	r3 := cl.readTag("p3")
	r3.ok(t)

	cl2 := dial(t, bound.Listen)
	cl2.send("s1 LOGIN {4}\r\n")
	c1 := cl2.readRec()
	if !strings.HasPrefix(strings.TrimRight(c1, "\r\n"), "+") {
		t.Fatalf("sync cont %q", c1)
	}
	cl2.send("demo {6}\r\n")
	c2 := cl2.readRec()
	if !strings.HasPrefix(strings.TrimRight(c2, "\r\n"), "+") {
		t.Fatalf("sync cont %q", c2)
	}
	cl2.send("secret\r\n")
	cl2.readTag("s1").ok(t)
	cl2.cmd("SELECT INBOX").ok(t)
	before, _, _, _ := parseSelect(t, cl2.cmd("SELECT INBOX").mustOK(t))
	cl2.send("i1 IDLE\r\n")
	for {
		rec := cl2.readRec()
		if strings.HasPrefix(strings.TrimRight(rec, "\r\n"), "+") {
			break
		}
	}
	cl.cmd("SELECT INBOX").ok(t)
	cl.cmd("APPEND INBOX {5+}\r\nhello").ok(t)
	deadline := time.Now().Add(5 * time.Second)
	var saw bool
	for time.Now().Before(deadline) {
		cl2.c.SetReadDeadline(time.Now().Add(time.Second))
		rec := cl2.readRec()
		if strings.Contains(rec, "EXISTS") {
			fields := strings.Fields(rec)
			if len(fields) >= 2 {
				n, _ := strconv.Atoi(fields[1])
				if n != before+1 {
					t.Fatalf("exists %d want %d (%q)", n, before+1, rec)
				}
			}
			saw = true
			break
		}
	}
	if !saw {
		t.Fatal("IDLE did not see EXISTS")
	}
	cl2.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	cl2.send("DONE\r\n")
	cl2.readTag("i1").ok(t)
	exists, _, _, next := parseSelect(t, cl2.cmd("SELECT INBOX").mustOK(t))
	if exists != before+1 || next < 4 {
		t.Fatalf("shared append not visible: exists %d want %d uidnext %d", exists, before+1, next)
	}
}

func TestGenerated100k(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{
	  "accounts": [{"username": "demo", "password": "secret"}],
	  "mailboxes": [{
	    "name": "INBOX.Big",
	    "uid_validity": 9,
	    "generated": {
	      "count": 100000,
	      "seed": "bulk",
	      "from": "Alice Example <alice@example.com>",
	      "to": "Demo User <demo@example.com>",
	      "subject": "Generated",
	      "start": "01-Jan-2020 00:00:00 +0000",
	      "step_seconds": 60
	    }
	  }]
	}`)
	if err := os.WriteFile(filepath.Join(dir, "dataset.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	srv, bound := start(t, Config{Listen: "127.0.0.1:0", Dataset: dir})
	if srv.store.mailboxLen("INBOX.Big") != 100000 {
		t.Fatalf("len %d", srv.store.mailboxLen("INBOX.Big"))
	}
	if n := srv.store.generatedBodiesStored(); n != 0 {
		t.Fatalf("bodies stored before fetch: %d", n)
	}
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	sel := cl.cmd("SELECT INBOX.Big").mustOK(t)
	exists, _, uidv, next := parseSelect(t, sel)
	if exists != 100000 || uidv != 9 || next != 100001 {
		t.Fatalf("select exists=%d uidv=%d next=%d", exists, uidv, next)
	}
	a := fetchLiteral(t, fetchLine(t, cl.cmd("UID FETCH 100000 (BODY.PEEK[TEXT])").mustOK(t)))
	b := fetchLiteral(t, fetchLine(t, cl.cmd("UID FETCH 100000 (BODY.PEEK[TEXT])").mustOK(t)))
	if !bytes.Equal(a, b) || !bytes.Contains(a, []byte("uid=100000")) || !bytes.Contains(a, []byte("seed=bulk")) {
		t.Fatalf("body %q", a)
	}
	if n := srv.store.generatedBodiesStored(); n != 0 {
		t.Fatalf("bodies stored after fetch: %d", n)
	}
}

func start(t *testing.T, cfg Config) (*Server, Bound) {
	t.Helper()
	if cfg.Personality == "" {
		cfg.Personality = "personalities/cyrus-3137.json"
	}
	if cfg.Dataset == "" {
		cfg.Dataset = "datasets/demo"
	}
	if cfg.Listen == "" && cfg.ImplicitTLSListen == "" {
		cfg.Listen = "127.0.0.1:0"
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	srv, err := newServer(path, &out)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = srv.Serve()
		close(done)
	}()
	select {
	case <-srv.ready:
	case <-time.After(30 * time.Second):
		t.Fatal("server not ready")
	}
	if srv.startErr != nil {
		t.Fatal(srv.startErr)
	}
	var bound Bound
	if err := json.Unmarshal(out.Bytes(), &bound); err != nil {
		t.Fatalf("stdout %q: %v", out.String(), err)
	}
	t.Cleanup(func() {
		srv.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("server did not stop")
			return
		}
		if bound.Listen != "" {
			c, err := net.DialTimeout("tcp", bound.Listen, 200*time.Millisecond)
			if err == nil {
				c.Close()
				t.Errorf("listen %s still open", bound.Listen)
			}
		}
		if bound.ImplicitTLSListen != "" {
			c, err := net.DialTimeout("tcp", bound.ImplicitTLSListen, 200*time.Millisecond)
			if err == nil {
				c.Close()
				t.Errorf("implicit tls %s still open", bound.ImplicitTLSListen)
			}
		}
		if bound.Control != "" {
			if _, err := os.Stat(bound.Control); !os.IsNotExist(err) {
				t.Errorf("control socket %s still present", bound.Control)
			}
		}
		if bound.Smtp != "" {
			c, err := net.DialTimeout("tcp", bound.Smtp, 200*time.Millisecond)
			if err == nil {
				c.Close()
				t.Errorf("smtp %s still open", bound.Smtp)
			}
		}
		if bound.Jmap != "" {
			c, err := net.DialTimeout("tcp", bound.Jmap, 200*time.Millisecond)
			if err == nil {
				c.Close()
				t.Errorf("jmap %s still open", bound.Jmap)
			}
		}
		if bound.Sieve != "" {
			c, err := net.DialTimeout("tcp", bound.Sieve, 200*time.Millisecond)
			if err == nil {
				c.Close()
				t.Errorf("sieve %s still open", bound.Sieve)
			}
		}
	})
	return srv, bound
}

type reply struct {
	untagged []string
	tagLine  string
	status   string
	text     string
}

func (rp *reply) ok(t *testing.T) {
	t.Helper()
	if rp.status != "OK" {
		t.Fatalf("status %s: %s untagged %q", rp.status, rp.tagLine, rp.untagged)
	}
}

func (rp *reply) mustOK(t *testing.T) *reply {
	t.Helper()
	rp.ok(t)
	return rp
}

func (rp *reply) no(t *testing.T) {
	t.Helper()
	if rp.status != "NO" {
		t.Fatalf("want NO, got %s %s", rp.status, rp.tagLine)
	}
}

type imapClient struct {
	t        *testing.T
	c        net.Conn
	r        *bufio.Reader
	w        *bufio.Writer
	greeting string
	n        int
}

func dial(t *testing.T, addr string) *imapClient {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cl := &imapClient{t: t, c: c, r: bufio.NewReader(c), w: bufio.NewWriter(c)}
	cl.greeting = cl.readRec()
	if !strings.HasPrefix(cl.greeting, "* OK ") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	return cl
}

func dialTLS(t *testing.T, addr string) *imapClient {
	t.Helper()
	c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cl := &imapClient{t: t, c: c, r: bufio.NewReader(c), w: bufio.NewWriter(c)}
	cl.greeting = cl.readRec()
	if !strings.HasPrefix(cl.greeting, "* OK ") {
		t.Fatalf("greeting %q", cl.greeting)
	}
	return cl
}

func (cl *imapClient) send(s string) {
	cl.t.Helper()
	cl.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(cl.w, s); err != nil {
		cl.t.Fatal(err)
	}
	if err := cl.w.Flush(); err != nil {
		cl.t.Fatal(err)
	}
}

func (cl *imapClient) cmd(line string) *reply {
	cl.t.Helper()
	cl.n++
	tag := fmt.Sprintf("a%d", cl.n)
	cl.send(tag + " " + line + "\r\n")
	return cl.readTag(tag)
}

func (cl *imapClient) readTag(tag string) *reply {
	cl.t.Helper()
	rp := &reply{}
	for {
		rec := strings.TrimRight(cl.readRec(), "\r\n")
		if strings.HasPrefix(rec, tag+" ") {
			rp.tagLine = rec
			fs := strings.SplitN(rec, " ", 3)
			if len(fs) > 1 {
				rp.status = fs[1]
			}
			if len(fs) > 2 {
				rp.text = fs[2]
			}
			return rp
		}
		rp.untagged = append(rp.untagged, rec)
	}
}

func (cl *imapClient) readRec() string {
	cl.t.Helper()
	if err := cl.c.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		cl.t.Fatal(err)
	}
	var buf bytes.Buffer
	for {
		line, err := cl.r.ReadString('\n')
		if err != nil {
			cl.t.Fatalf("read: %v after %q", err, buf.Bytes())
		}
		buf.WriteString(line)
		if n, ok := literalLen(line); ok {
			if _, err := io.CopyN(&buf, cl.r, int64(n)); err != nil {
				cl.t.Fatal(err)
			}
			continue
		}
		return buf.String()
	}
}

func literalLen(line string) (int, bool) {
	trim := strings.TrimRight(line, "\r\n")
	i := strings.LastIndex(trim, "{")
	if i < 0 || !strings.HasSuffix(trim, "}") {
		return 0, false
	}
	inner := trim[i+1 : len(trim)-1]
	if strings.HasSuffix(inner, "+") {
		inner = strings.TrimSuffix(inner, "+")
	}
	if inner == "" {
		return 0, false
	}
	for _, c := range inner {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(inner)
	if err != nil {
		return 0, false
	}
	return n, true
}

func capsOf(line string) []string {
	const mark = "[CAPABILITY "
	i := strings.Index(line, mark)
	if i < 0 {
		if strings.HasPrefix(line, "* CAPABILITY ") {
			return strings.Fields(strings.TrimPrefix(strings.TrimRight(line, "\r\n"), "* CAPABILITY "))
		}
		return nil
	}
	rest := line[i+len(mark):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return nil
	}
	return strings.Fields(rest[:j])
}

func requireCaps(t *testing.T, got, want []string) {
	t.Helper()
	have := map[string]bool{}
	for _, c := range got {
		have[strings.ToUpper(c)] = true
	}
	for _, c := range want {
		if !have[strings.ToUpper(c)] {
			t.Fatalf("missing %s in %v", c, got)
		}
	}
}

func forbidCaps(t *testing.T, got []string) {
	t.Helper()
	for _, c := range got {
		u := strings.ToUpper(c)
		switch {
		case u == "COMPRESS" || u == "AUTH=DIGEST-MD5":
			t.Fatalf("forbidden %s", c)
		case strings.HasPrefix(u, "COMPRESS=") && u != "COMPRESS=DEFLATE":
			t.Fatalf("forbidden %s", c)
		case strings.Contains(u, "SCRAM") || strings.Contains(u, "DIGEST-MD5"):
			t.Fatalf("forbidden %s", c)
		}
	}
}

func plainToken(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte("\x00" + user + "\x00" + pass))
}

func searchIDs(t *testing.T, rp *reply) []string {
	t.Helper()
	if rp.status != "OK" && rp.status != "" {
		if rp.status != "OK" {
			t.Fatalf("search %s %s", rp.status, rp.tagLine)
		}
	}
	if rp.status != "OK" {
		t.Fatalf("search %s", rp.tagLine)
	}
	for _, u := range rp.untagged {
		if strings.HasPrefix(u, "* SEARCH") {
			f := strings.Fields(u)
			if len(f) <= 2 {
				return nil
			}
			return f[2:]
		}
	}
	t.Fatalf("no SEARCH in %q", rp.untagged)
	return nil
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func hasPrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func parseSelect(t *testing.T, rp *reply) (exists, recent int, uidv, uidnext uint32) {
	t.Helper()
	rp.ok(t)
	for _, u := range rp.untagged {
		fields := strings.Fields(u)
		if len(fields) >= 3 && fields[0] == "*" && fields[2] == "EXISTS" {
			n, _ := strconv.Atoi(fields[1])
			exists = n
		}
		if len(fields) >= 3 && fields[0] == "*" && fields[2] == "RECENT" {
			n, _ := strconv.Atoi(fields[1])
			recent = n
		}
		if strings.Contains(u, "[UIDVALIDITY ") {
			uidv = bracketNum(u, "UIDVALIDITY")
		}
		if strings.Contains(u, "[UIDNEXT ") {
			uidnext = bracketNum(u, "UIDNEXT")
		}
	}
	return
}

func bracketNum(line, key string) uint32 {
	i := strings.Index(line, "["+key+" ")
	if i < 0 {
		return 0
	}
	rest := line[i+len(key)+2:]
	n, _ := strconv.ParseUint(strings.Fields(rest)[0], 10, 32)
	// Fields[0] may include trailing ]
	s := strings.TrimRight(strings.Fields(rest)[0], "]")
	n, _ = strconv.ParseUint(s, 10, 32)
	return uint32(n)
}

func fetchLine(t *testing.T, rp *reply) string {
	t.Helper()
	for _, u := range rp.untagged {
		if strings.Contains(u, " FETCH (") {
			return u
		}
	}
	t.Fatalf("no FETCH in %q", rp.untagged)
	return ""
}

func fetchKeys(t *testing.T, line string) []string {
	t.Helper()
	items := parseFetchItems(line)
	var keys []string
	for _, it := range items {
		keys = append(keys, it[0])
	}
	return keys
}

func parseFetchItems(line string) [][2]string {
	i := strings.Index(line, "FETCH (")
	if i < 0 {
		return nil
	}
	s := line[i+len("FETCH ("):]
	if strings.HasSuffix(s, ")") {
		s = s[:len(s)-1]
	}
	var items [][2]string
	for len(s) > 0 {
		s = strings.TrimLeft(s, " ")
		if s == "" {
			break
		}
		j := strings.IndexByte(s, ' ')
		if j < 0 {
			break
		}
		key := s[:j]
		s = strings.TrimLeft(s[j+1:], " ")
		val, next := readIMAPValue(s)
		items = append(items, [2]string{key, val})
		s = next
	}
	return items
}

func readIMAPValue(s string) (string, string) {
	if s == "" {
		return "", ""
	}
	switch s[0] {
	case '(':
		depth := 0
		for i := 0; i < len(s); i++ {
			if s[i] == '(' {
				depth++
			} else if s[i] == ')' {
				depth--
				if depth == 0 {
					return s[:i+1], s[i+1:]
				}
			}
		}
		return s, ""
	case '"':
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == '"' {
				return s[:i+1], s[i+1:]
			}
		}
		return s, ""
	case '{':
		k := strings.IndexByte(s, '}')
		if k < 0 {
			return s, ""
		}
		n, _ := strconv.Atoi(s[1:k])
		rest := s[k+1:]
		if strings.HasPrefix(rest, "\r\n") {
			rest = rest[2:]
		}
		if n > len(rest) {
			n = len(rest)
		}
		return s[:k+1+2+n], rest[n:]
	default:
		j := strings.IndexByte(s, ' ')
		if j < 0 {
			return s, ""
		}
		return s[:j], s[j:]
	}
}

func fetchLiteral(t *testing.T, line string) []byte {
	t.Helper()
	items := parseFetchItems(line)
	for _, it := range items {
		if strings.Contains(it[0], "BODY") || strings.HasPrefix(it[0], "RFC822") {
			v := it[1]
			if strings.HasPrefix(v, "{") {
				k := strings.IndexByte(v, '}')
				n, _ := strconv.Atoi(v[1:k])
				rest := v[k+1:]
				if strings.HasPrefix(rest, "\r\n") {
					rest = rest[2:]
				}
				if len(rest) < n {
					t.Fatalf("short literal %d < %d in %q", len(rest), n, line)
				}
				return []byte(rest[:n])
			}
		}
	}
	t.Fatalf("no literal in %s", line)
	return nil
}

func fetchSize(t *testing.T, rp *reply) int {
	t.Helper()
	rp.ok(t)
	items := parseFetchItems(fetchLine(t, rp))
	for _, it := range items {
		if it[0] == "RFC822.SIZE" {
			n, err := strconv.Atoi(strings.TrimSpace(it[1]))
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatal("no size")
	return 0
}

func fetchUID(t *testing.T, rp *reply) uint32 {
	t.Helper()
	rp.ok(t)
	items := parseFetchItems(fetchLine(t, rp))
	for _, it := range items {
		if it[0] == "UID" {
			n, err := strconv.ParseUint(strings.TrimSpace(it[1]), 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			return uint32(n)
		}
	}
	t.Fatalf("no uid in %q", rp.untagged)
	return 0
}

func writeCert(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fakeimap"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
