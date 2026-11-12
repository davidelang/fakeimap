package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFaultReplyByeIdleAndMatch(t *testing.T) {
	srv, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	facts := string(mustRead(t, "docs/facts/imap-server.md"))
	guide := string(mustRead(t, "docs/guide.md"))
	for _, name := range []string{"reply", "bye", "idle_burst", "drop_literal", "rst_literal", "overquota", "expunge_mid"} {
		if !strings.Contains(facts, name) || !strings.Contains(guide, name) {
			t.Fatalf("docs miss %s", name)
		}
	}
	if !strings.Contains(facts, "`bye`") || !strings.Contains(guide, "`bye`") || !strings.Contains(guide, "docs/facts/imap-server.md") {
		t.Fatal("bye op is not named")
	}
	if strings.Contains(guide, "552 5.2.2") || strings.Contains(guide, "noteExpungedIDs") {
		t.Fatal("guide pastes the fault rules")
	}

	for _, req := range []map[string]any{
		{"op": "arm-fault", "do": "reply", "command": "NOOP"},
		{"op": "arm-fault", "do": "reply", "command": "NOOP", "status": "MAYBE"},
		{"op": "arm-fault", "do": "idle_burst", "command": "NOOP", "text": "* X"},
		{"op": "arm-fault", "do": "idle_burst", "command": "IDLE"},
		{"op": "arm-fault", "do": "drop_literal", "command": "APPEND"},
		{"op": "arm-fault", "do": "rst_literal", "command": "APPEND", "bytes": -1},
		{"op": "arm-fault", "do": "overquota"},
		{"op": "arm-fault", "do": "expunge_mid", "command": "NOOP"},
		{"op": "arm-fault", "do": "expunge_mid", "command": "NOOP", "uid": 1, "after": -1},
		{"op": "arm-fault", "do": "bad_literal", "command": "FETCH", "size": -1},
		{"op": "arm-fault", "do": "bye"},
	} {
		rp := cc.call(req)
		if rp.OK || rp.Error != "missing field" {
			t.Fatalf("missing field %+v -> %+v", req, rp)
		}
	}
	for _, req := range []map[string]any{
		{"op": "arm-fault", "do": "nope", "command": "NOOP"},
		{"op": "arm-fault", "do": "overquota", "command": "APPEND", "proto": "pop3"},
		{"op": "arm-fault", "do": "close", "command": "NOOP", "proto": "nope"},
	} {
		rp := cc.call(req)
		if rp.OK || rp.Error != "unknown fault" {
			t.Fatalf("unknown fault %+v -> %+v", req, rp)
		}
	}
	missingUID := cc.call(map[string]any{"op": "arm-fault", "do": "expunge_mid", "command": "NOOP", "uid": 99})
	if missingUID.OK || missingUID.Error == "" {
		t.Fatalf("missing uid %+v", missingUID)
	}
	if n := len(cc.call(map[string]any{"op": "status"}).ok(t).Faults); n != 0 {
		t.Fatalf("armed a missing uid, faults %d", n)
	}

	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cc.arm(map[string]any{"do": "reply", "command": "SELECT", "status": "NO", "code": "NOPE", "text": "no", "times": 1})
	sel := cl.cmd("SELECT INBOX")
	if sel.status != "NO" || sel.text != "[NOPE] no" || len(sel.untagged) != 0 {
		t.Fatalf("select reply %s %q", sel.tagLine, sel.untagged)
	}
	if rp := cl.cmd("FETCH 1 (UID)"); rp.status != "NO" {
		t.Fatalf("select stuck %s", rp.tagLine)
	}
	cl.cmd("SELECT INBOX").ok(t)

	before := inboxCount(t, cc)
	cc.arm(map[string]any{"do": "reply", "command": "APPEND", "status": "NO", "times": 1})
	app := cl.cmd("APPEND INBOX " + imapLit("Subject: z\r\n\r\nz\r\n"))
	if app.status != "NO" || len(app.untagged) != 0 {
		t.Fatalf("append reply %s %q", app.tagLine, app.untagged)
	}
	if inboxCount(t, cc) != before {
		t.Fatal("reply append stored")
	}
	cl.cmd("APPEND INBOX " + imapLit("Subject: z\r\n\r\nz\r\n")).ok(t)
	if inboxCount(t, cc) != before+1 {
		t.Fatal("spent fault still blocked append")
	}

	cc.call(map[string]any{"op": "clear-faults"}).ok(t)
	cc.arm(map[string]any{"do": "reply", "command": "NOOP", "status": "OK", "times": 1})
	only := cl.cmd("NOOP")
	if only.status != "OK" || only.text != "" || !strings.HasSuffix(only.tagLine, " OK") {
		t.Fatalf("bare ok %q", only.tagLine)
	}

	cc.call(map[string]any{"op": "clear-faults"}).ok(t)
	cc.arm(map[string]any{"do": "reply", "command": "NOOP", "status": "BAD", "times": 2})
	cl.cmd("NOOP").ok(t)
	if cl.cmd("NOOP").status != "BAD" {
		t.Fatal("times 2 did not fire")
	}
	cl.cmd("NOOP").ok(t)
	cc.call(map[string]any{"op": "clear-faults"}).ok(t)
	cc.arm(map[string]any{"do": "reply", "command": "NOOP", "status": "BAD"})
	if cl.cmd("NOOP").status != "BAD" || cl.cmd("NOOP").status != "BAD" {
		t.Fatal("times 0 did not repeat")
	}
	cc.call(map[string]any{"op": "clear-faults"}).ok(t)

	other := dial(t, bound.Listen)
	cc.arm(map[string]any{"do": "reply", "command": "NOOP", "status": "NO", "conn": 2, "times": 1})
	cl.cmd("NOOP").ok(t)
	if other.cmd("NOOP").status != "NO" {
		t.Fatal("conn 2 did not match")
	}

	cc.arm(map[string]any{"do": "bye", "command": "NOOP", "times": 1})
	cl.send("z1 NOOP\r\n")
	bye := strings.TrimRight(cl.readRec(), "\r\n")
	if bye != "* BYE Server logging out" {
		t.Fatalf("do bye %q", bye)
	}
	if _, err := readSome(cl.c, 300*time.Millisecond); err == nil {
		t.Fatal("do bye left the connection open")
	}

	idle := dial(t, bound.Listen)
	idle.cmd("LOGIN demo secret").ok(t)
	idle.cmd("SELECT INBOX").ok(t)
	idle.send("i1 IDLE\r\n")
	plus := strings.TrimRight(idle.readRec(), "\r\n")
	if plus != "+ idling" {
		t.Fatalf("idle plus %q", plus)
	}
	cc.call(map[string]any{"op": "bye", "conn": 3, "text": "go away"}).ok(t)
	got := strings.TrimRight(idle.readRec(), "\r\n")
	if got != "* BYE go away" {
		t.Fatalf("idle bye %q", got)
	}
	if buf, err := readSome(idle.c, 300*time.Millisecond); err == nil || bytes.Contains(buf, []byte(" OK")) {
		t.Fatalf("idle tagged after bye %q %v", buf, err)
	}
	cl2 := dial(t, bound.Listen)
	cc.call(map[string]any{"op": "bye"}).ok(t)
	def := strings.TrimRight(cl2.readRec(), "\r\n")
	if def != "* BYE Server logging out" {
		t.Fatalf("default bye %q", def)
	}
	if cc.call(map[string]any{"op": "bye", "conn": 99}).OK {
		t.Fatal("bye missing conn")
	}

	client, br, writes := countedIMAP(t, srv)
	rawCmd(t, client, br, "a1", "LOGIN demo secret")
	rawCmd(t, client, br, "a2", "SELECT INBOX")
	cc.arm(map[string]any{"do": "idle_burst", "command": "IDLE", "times": 1, "text": "* 9 EXISTS"})
	if _, err := io.WriteString(client, "a3 IDLE\r\n"); err != nil {
		t.Fatal(err)
	}
	burst := awaitWriteContaining(t, writes, []byte("+ idling\r\n* 9 EXISTS\r\n"))
	if !bytes.Equal(burst, []byte("+ idling\r\n* 9 EXISTS\r\n")) {
		t.Fatalf("idle burst %q", burst)
	}
	if _, err := io.WriteString(client, "DONE\r\n"); err != nil {
		t.Fatal(err)
	}
	okLine := readRawUntil(t, br, "a3 ")
	if !strings.HasPrefix(okLine, "a3 OK") {
		t.Fatalf("idle done %q", okLine)
	}
}

func TestFaultLiteralsQuotaAndProtocols(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{
		Listen:     "127.0.0.1:0",
		SmtpListen: "127.0.0.1:0",
		JmapListen: "127.0.0.1:0",
	})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE Box").ok(t)
	cl.cmd("SELECT Box").ok(t)
	if got := statusField(t, cl, "Box", "MESSAGES"); got != "0" {
		t.Fatalf("box %s", got)
	}

	cc.arm(map[string]any{"do": "drop_literal", "command": "APPEND", "bytes": 0, "times": 1})
	raw := dialRaw(t, bound.Listen)
	_, _ = raw.Write([]byte("b1 LOGIN demo secret\r\nb2 SELECT Box\r\nb3 APPEND Box {4}\r\n"))
	cont := readUntil(t, raw, 2*time.Second, []byte("+ go ahead"))
	if !bytes.Contains(cont, []byte("+ go ahead")) {
		t.Fatalf("sync literal %q", cont)
	}
	if buf, err := readSome(raw, time.Second); err == nil || bytes.Contains(buf, []byte("b3 OK")) {
		t.Fatalf("drop 0 %q %v", buf, err)
	}
	if got := statusField(t, cl, "Box", "MESSAGES"); got != "0" {
		t.Fatalf("drop 0 stored %s", got)
	}

	cc.arm(map[string]any{"do": "drop_literal", "command": "APPEND", "bytes": 3, "times": 1})
	plus := dialRaw(t, bound.Listen)
	_, _ = plus.Write([]byte("p1 LOGIN demo secret\r\np2 APPEND Box {5+}\r\nhello\r\n"))
	buf, err := readClosed(plus, 2*time.Second)
	if err == nil || bytes.Contains(buf, []byte("+ go ahead")) || bytes.Contains(buf, []byte("p2 OK")) {
		t.Fatalf("literal+ drop %q %v", buf, err)
	}
	if got := statusField(t, cl, "Box", "MESSAGES"); got != "0" {
		t.Fatalf("literal+ stored %s", got)
	}

	cc.arm(map[string]any{"do": "rst_literal", "command": "APPEND", "bytes": 2, "times": 1})
	rst := dialRaw(t, bound.Listen)
	_, _ = rst.Write([]byte("r1 LOGIN demo secret\r\nr2 APPEND Box {4}\r\n"))
	_ = readUntil(t, rst, 2*time.Second, []byte("+ go ahead"))
	_, _ = rst.Write([]byte("abcd"))
	rbuf, rerr := readSome(rst, time.Second)
	if rerr == nil || !strings.Contains(strings.ToLower(rerr.Error()), "reset") || bytes.Contains(rbuf, []byte("OK")) {
		t.Fatalf("rst literal %q %v", rbuf, rerr)
	}
	if got := statusField(t, cl, "Box", "MESSAGES"); got != "0" {
		t.Fatalf("rst stored %s", got)
	}

	cc.arm(map[string]any{"do": "bad_literal", "command": "FETCH", "size": 40, "times": 1})
	cl.cmd("APPEND Box " + imapLit("Subject: q\r\n\r\nbody\r\n")).ok(t)
	peek := dialRaw(t, bound.Listen)
	_, _ = peek.Write([]byte("f1 LOGIN demo secret\r\nf2 SELECT Box\r\nf3 FETCH 1 (BODY.PEEK[TEXT])\r\n"))
	blob := readUntil(t, peek, 2*time.Second, []byte("f3 OK"))
	if !bytes.Contains(blob, []byte("{40}")) && !bytes.Contains(blob, []byte("{40+}")) {
		t.Fatalf("size advertise %q", blob)
	}
	if bytes.Contains(blob, []byte("{4}")) || bytes.Contains(blob, []byte("{5}")) {
		t.Fatalf("old size kept %q", blob)
	}
	if !bytes.Contains(blob, []byte("body")) {
		t.Fatalf("payload changed %q", blob)
	}
	kept := badLiteral([]byte("* 1 FETCH (BODY[] {3+}\r\nABC)\r\n"), true, 9)
	if string(kept) != "* 1 FETCH (BODY[] {9+}\r\nABC)\r\n" {
		t.Fatalf("plus size %q", kept)
	}
	plain := badLiteral([]byte("* 1 FETCH (RFC822 {3}\r\nABC)\r\n"), false, 0)
	if string(plain) != "* 1 FETCH (RFC822 {4}\r\nABC)\r\n" {
		t.Fatalf("plus one %q", plain)
	}

	quota := joined(cl.cmd("GETQUOTA ROOT").mustOK(t).untagged)
	root := joined(cl.cmd("GETQUOTAROOT Box").mustOK(t).untagged)
	cc.arm(map[string]any{"do": "overquota", "command": "APPEND"})
	if got := joined(cl.cmd("GETQUOTA ROOT").mustOK(t).untagged); got != quota {
		t.Fatalf("quota changed %s", got)
	}
	if got := joined(cl.cmd("GETQUOTAROOT Box").mustOK(t).untagged); got != root {
		t.Fatalf("quotaroot changed %s", got)
	}
	one := cl.cmd("APPEND Box " + imapLit("one"))
	if one.status != "NO" || !strings.Contains(one.tagLine, "[OVERQUOTA]") || len(one.untagged) != 0 {
		t.Fatalf("append quota %s %q", one.tagLine, one.untagged)
	}
	many := cl.cmd("APPEND Box " + imapLit("aa") + " " + imapLit("bb"))
	if many.status != "NO" || !strings.Contains(many.tagLine, "[OVERQUOTA]") {
		t.Fatalf("multi %s", many.tagLine)
	}
	if got := statusField(t, cl, "Box", "MESSAGES"); got != "1" {
		t.Fatalf("overquota stored %s", got)
	}
	cc.call(map[string]any{"op": "clear-faults"}).ok(t)
	cl.cmd("CREATE Dest").ok(t)
	cc.arm(map[string]any{"do": "overquota", "command": "COPY", "times": 1})
	cp := cl.cmd("UID COPY 1 Dest")
	if cp.status != "NO" || !strings.Contains(cp.tagLine, "[OVERQUOTA]") {
		t.Fatalf("uid copy %s", cp.tagLine)
	}
	cc.arm(map[string]any{"do": "overquota", "command": "MOVE", "times": 1})
	mv := cl.cmd("MOVE 1 Dest")
	if mv.status != "NO" || !strings.Contains(mv.tagLine, "[OVERQUOTA]") || strings.Contains(joined(mv.untagged), "EXPUNGE") {
		t.Fatalf("move %s %q", mv.tagLine, mv.untagged)
	}
	cc.arm(map[string]any{"do": "overquota", "command": "XMOVE", "times": 1})
	xm := cl.cmd("UID XMOVE 1 Dest")
	if xm.status != "NO" || !strings.Contains(xm.tagLine, "[OVERQUOTA]") {
		t.Fatalf("xmove %s", xm.tagLine)
	}
	cc.arm(map[string]any{"do": "overquota", "command": "REPLACE", "times": 1})
	rp := cl.cmd("UID REPLACE 1 Dest " + imapLit("new"))
	if rp.status != "NO" || !strings.Contains(rp.tagLine, "[OVERQUOTA]") || strings.Contains(joined(rp.untagged), "EXPUNGE") {
		t.Fatalf("replace %s %q", rp.tagLine, rp.untagged)
	}
	if got := statusField(t, cl, "Box", "MESSAGES"); got != "1" {
		t.Fatalf("source %s", got)
	}
	if got := statusField(t, cl, "Dest", "MESSAGES"); got != "0" {
		t.Fatalf("dest %s", got)
	}
	cc.arm(map[string]any{"do": "overquota", "command": "SETQUOTA", "times": 1})
	cl.cmd("SETQUOTA ROOT (STORAGE 9)").ok(t)
	if got := joined(cl.cmd("GETQUOTA ROOT").mustOK(t).untagged); got == quota || !strings.Contains(got, "STORAGE") {
		t.Fatalf("setquota unchanged %s", got)
	}

	smtp1 := dialSMTP(t, bound.Smtp)
	smtp2 := dialSMTP(t, bound.Smtp)
	smtp1.authPlain("demo", "secret")
	smtp2.authPlain("demo", "secret")
	cc.arm(map[string]any{"do": "overquota", "command": "MAIL", "proto": "smtp", "conn": 2, "times": 1})
	if code, _ := smtp1.cmd("MAIL FROM:<a@b>"); code != 250 {
		t.Fatalf("other smtp conn %d", code)
	}
	code, lines := smtp2.cmd("MAIL FROM:<a@b>")
	if code != 552 || !strings.Contains(strings.Join(lines, " "), "5.2.2") {
		t.Fatalf("smtp mail %d %v", code, lines)
	}
	cc.arm(map[string]any{"do": "overquota", "command": "RCPT", "proto": "smtp", "times": 1})
	if code, _ := smtp1.cmd("RCPT TO:<demo>"); code != 552 {
		t.Fatalf("rcpt %d", code)
	}
	cc.call(map[string]any{"op": "clear-faults"}).ok(t)
	beforeSMTP := len(listSMTP(t, cc).Messages)
	beforeN := inboxCount(t, cc)
	cc.arm(map[string]any{"do": "overquota", "command": "DATA", "proto": "smtp", "times": 1})
	smtp3 := dialSMTP(t, bound.Smtp)
	smtp3.authPlain("demo", "secret")
	if code, _ := smtp3.cmd("MAIL FROM:<a@b>"); code != 250 {
		t.Fatal("mail")
	}
	if code, _ := smtp3.cmd("RCPT TO:<demo>"); code != 250 {
		t.Fatal("rcpt")
	}
	code, lines = smtp3.cmd("DATA")
	if code != 552 || !strings.Contains(strings.Join(lines, " "), "5.2.2") {
		t.Fatalf("data %d %v", code, lines)
	}
	if len(listSMTP(t, cc).Messages) != beforeSMTP || inboxCount(t, cc) != beforeN {
		t.Fatal("smtp data stored")
	}
	fresh := dialSMTP(t, bound.Smtp)
	fresh.authPlain("demo", "secret")
	cc.arm(map[string]any{"do": "overquota", "command": "MAIL", "proto": "imap", "times": 1})
	if code, _ := fresh.cmd("MAIL FROM:<a@b>"); code != 250 {
		t.Fatal("imap proto matched smtp")
	}

	session := jmapSession(t, bound.Jmap, "demo", "secret")
	accountID := objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:mail"].(string)
	rawMsg := []byte("From: a@example.com\r\nSubject: q\r\n\r\nquota\r\n")
	res, upBody := jmapDo(t, bound.Jmap, "demo", "secret", "POST", "/jmap/upload/"+accountID+"/", "message/rfc822", rawMsg)
	if res.StatusCode != 201 && res.StatusCode != 200 {
		t.Fatalf("upload %d %s", res.StatusCode, upBody)
	}
	blobID, _ := jmapJSON(t, upBody)["blobId"].(string)
	boxes := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Mailbox/get", map[string]any{"accountId": accountID}, "0"},
	})
	var inboxID string
	for _, item := range arrayField(t, methodArgs(t, boxes, "Mailbox/get"), "list") {
		obj := item.(map[string]any)
		if obj["name"] == "INBOX" {
			inboxID = obj["id"].(string)
		}
	}
	cc.arm(map[string]any{"do": "overquota", "command": "Email/import", "proto": "jmap", "times": 1})
	imported := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/import", map[string]any{
			"accountId": accountID,
			"emails": map[string]any{
				"k1": map[string]any{"blobId": blobID, "mailboxIds": map[string]any{inboxID: true}},
			},
		}, "0"},
	})
	if methodType(t, imported, "0") != "overQuota" {
		t.Fatalf("import %+v", imported)
	}
	cc.arm(map[string]any{"do": "overquota", "command": "Email/set", "proto": "jmap", "times": 1})
	listed := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"inMailbox": inboxID},
		}, "0"},
	})
	ids := stringArray(t, methodArgs(t, listed, "Email/query")["ids"])
	if len(ids) == 0 {
		t.Fatal("no email id")
	}
	set := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/set", map[string]any{"accountId": accountID, "destroy": []any{ids[0]}}, "1"},
	})
	if methodType(t, set, "1") != "overQuota" {
		t.Fatalf("set %+v", set)
	}
	again := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{"accountId": accountID, "ids": []any{ids[0]}, "properties": []any{"id"}}, "2"},
	})
	if emailList(t, again)[0]["id"] != ids[0] {
		t.Fatalf("destroyed anyway %+v", again)
	}
}

func TestFaultExpungeMid(t *testing.T) {
	srv, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	cl.cmd("CREATE Box").ok(t)
	cl.cmd("SELECT Box").ok(t)
	cl.cmd("APPEND Box " + imapLit("one")).ok(t)
	cl.cmd("APPEND Box " + imapLit("two")).ok(t)
	cl.cmd("APPEND Box " + imapLit("three")).ok(t)

	cc.arm(map[string]any{"do": "expunge_mid", "command": "FETCH", "uid": 2, "after": 1, "times": 1})
	got := cl.cmd("FETCH 1,3 (UID)").mustOK(t)
	if strings.Join(got.untagged, "\n") != "* 1 FETCH (UID 1 FLAGS (\\Recent))\n* 2 EXPUNGE\n* 2 FETCH (UID 3 FLAGS (\\Recent))" &&
		!expungeOrder(got.untagged, "* 1 FETCH", "* 2 EXPUNGE", "* 2 FETCH") {
		t.Fatalf("mid fetch %q", got.untagged)
	}
	if !strings.Contains(joined(got.untagged), "UID 1") || !strings.Contains(joined(got.untagged), "UID 3") || strings.Contains(joined(got.untagged), "UID 2") {
		t.Fatalf("uids %q", got.untagged)
	}
	if got := statusField(t, cl, "Box", "MESSAGES"); got != "2" {
		t.Fatalf("left %s", got)
	}

	cl.cmd("APPEND Box " + imapLit("two-again")).ok(t)
	// UIDs now 1, 3, 4. Expunge uid 1 before the first line.
	cc.arm(map[string]any{"do": "expunge_mid", "command": "FETCH", "uid": 1, "after": 0, "times": 1})
	early := cl.cmd("FETCH 2:3 (UID)").mustOK(t)
	text := joined(early.untagged)
	if !strings.HasPrefix(text, "* 1 EXPUNGE") || !strings.Contains(text, "UID 3") || !strings.Contains(text, "UID 4") {
		t.Fatalf("after 0 %q", early.untagged)
	}
	if strings.Contains(text, "* 3 FETCH") || strings.Contains(text, "* 2 FETCH (UID 4") && strings.Contains(text, "* 3 FETCH") {
		t.Fatalf("seq not corrected %q", early.untagged)
	}

	cl.cmd("ENABLE QRESYNC").ok(t)
	cc.arm(map[string]any{"do": "expunge_mid", "command": "FETCH", "uid": 3, "times": 1})
	van := cl.cmd("FETCH 1:2 (UID)").mustOK(t)
	vtext := joined(van.untagged)
	if !strings.Contains(vtext, "VANISHED") || strings.Contains(vtext, "EXPUNGE") {
		t.Fatalf("qresync %q", van.untagged)
	}
	if !strings.Contains(vtext, "UID 4") || strings.Contains(vtext, "* 2 FETCH (UID 4") {
		t.Fatalf("qresync seq %q", van.untagged)
	}

	cl.cmd("CLOSE").ok(t)
	cl.cmd("SELECT Box").ok(t)
	cl.cmd("ENABLE UIDONLY").ok(t)
	// Remaining UIDs are 4 and whatever survived. Re-read with UID SEARCH.
	left := cl.cmd("UID SEARCH ALL").mustOK(t)
	if len(left.untagged) != 1 {
		t.Fatalf("left search %q", left.untagged)
	}
	cc.arm(map[string]any{"do": "expunge_mid", "command": "FETCH", "uid": 4, "after": 0, "times": 1})
	uidOnly := cl.cmd("UID FETCH 4 (UID)").mustOK(t)
	ut := joined(uidOnly.untagged)
	if !strings.Contains(ut, "VANISHED") || strings.Contains(ut, "EXPUNGE") || !strings.Contains(ut, "UIDFETCH") {
		t.Fatalf("uidonly %q", uidOnly.untagged)
	}

	ctxSrv, ctxBound, ctxCC := startCtrl(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/dovecot-245.json"})
	_ = ctxSrv
	watch := dial(t, ctxBound.Listen)
	watch.cmd("LOGIN demo secret").ok(t)
	watch.cmd("CREATE Ctx").ok(t)
	watch.cmd("SELECT Ctx").ok(t)
	watch.cmd("APPEND Ctx " + imapLit("a")).ok(t)
	watch.cmd("APPEND Ctx " + imapLit("b")).ok(t)
	watch.cmd("SEARCH RETURN (UPDATE) ALL").ok(t)
	ctxCC.arm(map[string]any{"do": "expunge_mid", "command": "NOOP", "uid": 1, "times": 1})
	noop := watch.cmd("NOOP").mustOK(t)
	ntext := joined(noop.untagged)
	rem := strings.Index(ntext, "REMOVEFROM")
	exp := strings.Index(ntext, "EXPUNGE")
	if rem < 0 || exp < 0 || rem > exp || strings.Contains(ntext, "VANISHED") {
		t.Fatalf("context %q", noop.untagged)
	}
	if got := statusField(t, watch, "Ctx", "MESSAGES"); got != "1" {
		t.Fatalf("context left %s", got)
	}

	// A second connection's EXPUNGE lands between FETCH lines, and the later
	// line uses the corrected sequence number. The hook runs on the FETCH
	// session after a line is written and before that line's flush.
	a := dial(t, bound.Listen)
	b := dial(t, bound.Listen)
	a.cmd("LOGIN demo secret").ok(t)
	b.cmd("LOGIN demo secret").ok(t)
	a.cmd("CREATE Gap").ok(t)
	a.cmd("SELECT Gap").ok(t)
	b.cmd("SELECT Gap").ok(t)
	a.cmd("APPEND Gap " + imapLit("Subject: s1\r\n\r\nsmall-1")).ok(t)
	a.cmd("APPEND Gap " + imapLit("Subject: big\r\n\r\nbig-body")).ok(t)
	a.cmd("APPEND Gap " + imapLit("Subject: s3\r\n\r\nsmall-3")).ok(t)
	b.cmd("NOOP").ok(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv.fetchLineHook = func() {
		once.Do(func() {
			entered <- struct{}{}
			<-release
		})
	}
	gotCh := make(chan *reply, 1)
	go func() {
		gotCh <- a.cmd("FETCH 2:3 (UID BODY.PEEK[TEXT])")
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not pause between lines")
	}
	b.cmd("UID STORE 1 +FLAGS (\\Deleted)").ok(t)
	b.cmd("UID EXPUNGE 1").ok(t)
	close(release)
	var gap *reply
	select {
	case gap = <-gotCh:
	case <-time.After(10 * time.Second):
		t.Fatal("fetch did not finish")
	}
	gap.ok(t)
	gtext := joined(gap.untagged)
	bigAt := strings.Index(gtext, "big-")
	expAt := strings.Index(gtext, "EXPUNGE")
	smallAt := strings.Index(gtext, "small-3")
	if bigAt < 0 || expAt < 0 || smallAt < 0 || !(bigAt < expAt && expAt < smallAt) {
		t.Fatalf("gap order %q", gap.untagged)
	}
	if !strings.Contains(gtext, "* 2 FETCH") || strings.Contains(gtext, "* 3 FETCH") {
		t.Fatalf("gap seq %q", gap.untagged)
	}
}

func readClosed(c net.Conn, d time.Duration) ([]byte, error) {
	deadline := time.Now().Add(d)
	var buf bytes.Buffer
	tmp := make([]byte, 2048)
	var err error
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, e := c.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if e != nil {
			err = e
			return buf.Bytes(), err
		}
	}
	return buf.Bytes(), err
}

func expungeOrder(lines []string, parts ...string) bool {
	text := strings.Join(lines, "\n")
	at := 0
	for _, p := range parts {
		i := strings.Index(text[at:], p)
		if i < 0 {
			return false
		}
		at += i + len(p)
	}
	return true
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func countedIMAP(t *testing.T, srv *Server) (net.Conn, *bufio.Reader, *countConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	srvConn := <-accepted
	wrapped := &countConn{Conn: srvConn}
	go srv.handle(wrapped, false)
	br := bufio.NewReader(client)
	line, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "* OK ") {
		t.Fatalf("counted greet %q %v", line, err)
	}
	return client, br, wrapped
}

func rawCmd(t *testing.T, c net.Conn, br *bufio.Reader, tag, cmd string) string {
	t.Helper()
	if _, err := io.WriteString(c, tag+" "+cmd+"\r\n"); err != nil {
		t.Fatal(err)
	}
	return readRawUntil(t, br, tag+" ")
}

func readRawUntil(t *testing.T, br *bufio.Reader, prefix string) string {
	t.Helper()
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		trim := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trim, prefix) {
			return trim
		}
	}
}

func awaitWriteContaining(t *testing.T, c *countConn, needle []byte) []byte {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, w := range c.snapshot() {
			if bytes.Contains(w, needle) {
				return w
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no write containing %q", needle)
	return nil
}
