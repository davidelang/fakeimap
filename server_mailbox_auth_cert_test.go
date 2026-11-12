package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateMailboxRaw(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	if cc.call(map[string]any{"op": "create-mailbox"}).Error != "missing field" {
		t.Fatal("neither name")
	}
	if cc.call(map[string]any{"op": "create-mailbox", "name": "A", "name_b64": base64.StdEncoding.EncodeToString([]byte("B"))}).Error != "bad name" {
		t.Fatal("both names")
	}
	if cc.call(map[string]any{"op": "create-mailbox", "name_b64": "!!!!"}).Error != "bad name" {
		t.Fatal("bad base64")
	}
	if cc.call(map[string]any{"op": "create-mailbox", "name": "Nope", "user": "nobody"}).Error != "no such account" {
		t.Fatal("missing account")
	}
	cc.call(map[string]any{"op": "accounts", "username": "other", "password": "secret"}).ok(t)
	cc.call(map[string]any{"op": "create-mailbox", "name": "PlainBox", "user": "demo"}).ok(t)
	cc.call(map[string]any{"op": "create-mailbox", "name": "a b"}).ok(t)
	raw := []byte{0xff}
	cc.call(map[string]any{"op": "create-mailbox", "name_b64": base64.StdEncoding.EncodeToString(raw)}).ok(t)
	utf := "Entwürfe"
	cc.call(map[string]any{"op": "create-mailbox", "name": utf}).ok(t)

	check := func(cl *imapClient) {
		t.Helper()
		list := strings.Join(cl.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n")
		if !strings.Contains(list, " PlainBox") || strings.Contains(list, `"PlainBox"`) {
			t.Fatalf("plain box %q", list)
		}
		if !strings.Contains(list, "{3}\r\na b") || strings.Contains(list, `"a b"`) {
			t.Fatalf("spaced name %q", list)
		}
		if !strings.Contains(list, "{1}\r\n"+string(raw)) || !strings.Contains(list, utf) || strings.Contains(list, encodeMUTF7(utf)) {
			t.Fatalf("raw list %q", list)
		}
		cl.cmd("SUBSCRIBE PlainBox").mustOK(t)
		cmdLiteral(t, cl, "SUBSCRIBE ", raw, "").mustOK(t)
		cl.cmd(`SUBSCRIBE "a b"`).mustOK(t)
		cmdLiteral(t, cl, "SUBSCRIBE ", []byte(utf), "").mustOK(t)
		lsub := strings.Join(cl.cmd(`LSUB "" "*"`).mustOK(t).untagged, "\n")
		if !strings.Contains(lsub, " PlainBox") || !strings.Contains(lsub, "{1}\r\n"+string(raw)) || !strings.Contains(lsub, "{3}\r\na b") || !strings.Contains(lsub, utf) {
			t.Fatalf("lsub %q", lsub)
		}
		st := cl.cmd("STATUS PlainBox (MESSAGES)").mustOK(t).untagged
		if len(st) != 1 || !strings.Contains(st[0], `STATUS "PlainBox" (MESSAGES 0)`) {
			t.Fatalf("status atom %q", st)
		}
		st = cl.cmd(`STATUS "a b" (MESSAGES)`).mustOK(t).untagged
		if len(st) != 1 || !strings.Contains(st[0], "{3}\r\na b") || strings.Contains(st[0], `"a b"`) {
			t.Fatalf("status space %q", st)
		}
		st = cmdLiteral(t, cl, "STATUS ", raw, " (MESSAGES)").mustOK(t).untagged
		if len(st) != 1 || !strings.Contains(st[0], "{1}\r\n"+string(raw)) || !strings.Contains(st[0], "MESSAGES 0") {
			t.Fatalf("status raw %q", st)
		}
		cmdLiteral(t, cl, "SELECT ", raw, "").mustOK(t)
		cl.cmd("SELECT PlainBox").mustOK(t)
	}

	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	check(cl)
	other := dial(t, bound.Listen)
	other.cmd("LOGIN other secret").mustOK(t)
	if !strings.Contains(strings.Join(other.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n"), " PlainBox") {
		t.Fatal("mailbox partitioned by user")
	}

	cc.call(map[string]any{"op": "set-personality", "path": "personalities/utf8.json"}).ok(t)
	cl2 := dial(t, bound.Listen)
	cl2.cmd("LOGIN demo secret").mustOK(t)
	if !strings.Contains(cl2.cmd("ENABLE UTF8=ACCEPT").mustOK(t).tagLine, "OK") {
		t.Fatal("enable")
	}
	check(cl2)

	facts := string(mustRead(t, "docs/facts/imap-server.md"))
	guide := string(mustRead(t, "docs/guide.md"))
	for _, phrase := range []string{"create-mailbox", "name_b64", "set-auth-policy", "clear-auth-policy", "reload-cert", "fail_delay_ms", "lockout_after", "UNAVAILABLE", "PRIVACYREQUIRED"} {
		if !strings.Contains(facts, phrase) {
			t.Fatalf("facts missing %s", phrase)
		}
	}
	for _, phrase := range []string{"`create-mailbox`", "`set-auth-policy`", "`clear-auth-policy`", "`reload-cert`", "docs/facts/imap-server.md"} {
		if !strings.Contains(guide, phrase) {
			t.Fatalf("guide missing %s", phrase)
		}
	}
	if strings.Contains(guide, "Authentication temporarily locked") || strings.Contains(guide, "name_b64 is standard base64") {
		t.Fatal("guide pastes the facts")
	}
}

func TestRawNameClearedOnReload(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	unicode := "Entwürfe"
	wire := encodeMUTF7(unicode)
	cc.call(map[string]any{"op": "create-mailbox", "name": unicode}).ok(t)
	before := dial(t, bound.Listen)
	before.cmd("LOGIN demo secret").mustOK(t)
	rawList := strings.Join(before.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n")
	if !strings.Contains(rawList, unicode) || strings.Contains(rawList, wire) {
		t.Fatalf("raw name before reset %q", rawList)
	}
	cc.call(map[string]any{"op": "reset"}).ok(t)
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("CREATE " + wire).mustOK(t)
	got := strings.Join(cl.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n")
	if !strings.Contains(got, " "+wire) || strings.Contains(got, unicode) {
		t.Fatalf("reset left the raw name %q", got)
	}

	marked := "A&B"
	cc.call(map[string]any{"op": "create-mailbox", "name": marked}).ok(t)
	cc.call(map[string]any{"op": "set-dataset", "path": "datasets/demo"}).ok(t)
	cl = dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl.cmd("CREATE " + encodeMUTF7(marked)).mustOK(t)
	got = strings.Join(cl.cmd(`LIST "" "*"`).mustOK(t).untagged, "\n")
	if !strings.Contains(got, " "+encodeMUTF7(marked)) || strings.Contains(got, " "+marked) {
		t.Fatalf("set-dataset left the raw name %q", got)
	}
}

func TestAuthPolicy(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0"})
	if cc.call(map[string]any{"op": "set-auth-policy", "fail_delay_ms": -1}).Error != "missing field" {
		t.Fatal("negative delay")
	}
	cc.call(map[string]any{"op": "set-auth-policy", "fail_delay_ms": 200}).ok(t)
	cl := dial(t, bound.Listen)
	started := time.Now()
	bad := cl.cmd(`LOGIN demo wrong`)
	if time.Since(started) < 180*time.Millisecond {
		t.Fatal("fail delay did not wait")
	}
	if bad.status != "NO" || !strings.Contains(bad.tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatalf("delayed login %s", bad.tagLine)
	}
	cc.call(map[string]any{"op": "reset"}).ok(t)
	cl = dial(t, bound.Listen)
	started = time.Now()
	bad = cl.cmd(`LOGIN demo wrong`)
	if time.Since(started) < 180*time.Millisecond || !strings.Contains(bad.tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatalf("reset cleared the delay %s %s", time.Since(started), bad.tagLine)
	}
	cc.call(map[string]any{"op": "set-dataset", "path": "datasets/demo"}).ok(t)
	cl = dial(t, bound.Listen)
	started = time.Now()
	bad = cl.cmd(`LOGIN demo wrong`)
	if time.Since(started) < 180*time.Millisecond {
		t.Fatal("set-dataset cleared the delay")
	}
	cc.call(map[string]any{"op": "clear-auth-policy"}).ok(t)
	cl = dial(t, bound.Listen)
	started = time.Now()
	bad = cl.cmd(`LOGIN demo wrong`)
	if time.Since(started) > 120*time.Millisecond || !strings.Contains(bad.tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatalf("clear did not restore instant failures %s %s", time.Since(started), bad.tagLine)
	}
	cc.call(map[string]any{"op": "set-auth-policy", "fail_delay_ms": 200}).ok(t)
	cc.call(map[string]any{"op": "set-auth-policy", "fail_delay_ms": 0, "lockout_after": 0, "lockout_ms": 0}).ok(t)
	cl = dial(t, bound.Listen)
	started = time.Now()
	bad = cl.cmd(`LOGIN demo wrong`)
	if time.Since(started) > 120*time.Millisecond {
		t.Fatal("all zeros did not restore instant failures")
	}

	cc.call(map[string]any{"op": "set-auth-policy", "lockout_after": 2, "lockout_ms": 60000}).ok(t)
	cl = dial(t, bound.Listen)
	if !strings.Contains(cl.cmd(`LOGIN demo wrong`).tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatal("first failure")
	}
	cl.cmd("LOGIN demo secret").mustOK(t)
	cl2 := dial(t, bound.Listen)
	if !strings.Contains(cl2.cmd(`LOGIN demo wrong`).tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatal("count survived success")
	}
	second := cl2.cmd(`LOGIN demo wrong`)
	if !strings.Contains(second.tagLine, "[AUTHENTICATIONFAILED]") || strings.Contains(second.tagLine, "UNAVAILABLE") {
		t.Fatalf("second after success %s", second.tagLine)
	}
	locked := cl2.cmd(`LOGIN demo wrong`)
	if locked.status != "NO" || !strings.Contains(locked.tagLine, "[UNAVAILABLE] Authentication temporarily locked") {
		t.Fatalf("lock %s", locked.tagLine)
	}
	other := dial(t, bound.Listen)
	if !strings.Contains(other.cmd(`LOGIN demo wrong`).tagLine, "[UNAVAILABLE]") {
		t.Fatal("username lock did not apply on another connection")
	}

	cc.call(map[string]any{"op": "clear-auth-policy"}).ok(t)
	cc.call(map[string]any{"op": "set-auth-policy", "lockout_after": 1, "lockout_ms": 60000}).ok(t)
	empty1 := dial(t, bound.Listen)
	if !strings.Contains(empty1.cmd(`LOGIN "" "x"`).tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatal("empty user first")
	}
	if !strings.Contains(empty1.cmd(`LOGIN "" "x"`).tagLine, "[UNAVAILABLE]") {
		t.Fatal("empty user lock")
	}
	empty2 := dial(t, bound.Listen)
	if !strings.Contains(empty2.cmd(`LOGIN "" "x"`).tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatal("empty user counted across connections")
	}

	cc.call(map[string]any{"op": "clear-auth-policy"}).ok(t)
	cc.call(map[string]any{"op": "set-auth-policy", "lockout_after": 1, "lockout_ms": 60000}).ok(t)
	mech := dial(t, bound.Listen)
	plain := mech.cmd("AUTHENTICATE PLAIN " + plainToken("demo", "wrong"))
	if !strings.Contains(plain.tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatalf("plain %s", plain.tagLine)
	}
	if !strings.Contains(mech.cmd("AUTHENTICATE PLAIN "+plainToken("demo", "wrong")).tagLine, "[UNAVAILABLE]") {
		t.Fatal("plain did not lock")
	}

	cc.call(map[string]any{"op": "clear-auth-policy"}).ok(t)
	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	cc.call(map[string]any{"op": "freeze-clock", "time": when.Format(time.RFC3339)}).ok(t)
	cc.call(map[string]any{"op": "set-auth-policy", "lockout_after": 1, "lockout_ms": 5000}).ok(t)
	fr := dial(t, bound.Listen)
	if !strings.Contains(fr.cmd(`LOGIN demo wrong`).tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatal("frozen first")
	}
	if !strings.Contains(fr.cmd(`LOGIN demo wrong`).tagLine, "[UNAVAILABLE]") {
		t.Fatal("frozen lock")
	}
	cc.call(map[string]any{"op": "freeze-clock", "time": when.Add(5 * time.Second).Format(time.RFC3339)}).ok(t)
	if !strings.Contains(fr.cmd(`LOGIN demo wrong`).tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatal("freeze at the deadline did not end the lock")
	}

	cc.call(map[string]any{"op": "thaw-clock"}).ok(t)
	cc.call(map[string]any{"op": "clear-auth-policy"}).ok(t)
	cc.call(map[string]any{"op": "set-auth-policy", "lockout_after": 1, "lockout_ms": 200}).ok(t)
	wall := dial(t, bound.Listen)
	wall.cmd(`LOGIN demo wrong`)
	if !strings.Contains(wall.cmd(`LOGIN demo wrong`).tagLine, "[UNAVAILABLE]") {
		t.Fatal("wall lock")
	}
	time.Sleep(250 * time.Millisecond)
	if !strings.Contains(wall.cmd(`LOGIN demo wrong`).tagLine, "[AUTHENTICATIONFAILED]") {
		t.Fatal("wall lock did not end")
	}

	cert, key := writeCert(t)
	_, bound, cc = startCtrl(t, Config{Listen: "127.0.0.1:0", CertFile: cert, KeyFile: key})
	cc.call(map[string]any{"op": "set-auth-policy", "fail_delay_ms": 200, "lockout_after": 1, "lockout_ms": 60000}).ok(t)
	priv := dial(t, bound.Listen)
	started = time.Now()
	refused := priv.cmd(`LOGIN demo wrong`)
	if time.Since(started) > 120*time.Millisecond {
		t.Fatal("PRIVACYREQUIRED slept")
	}
	if refused.status != "NO" || !strings.Contains(refused.tagLine, "[PRIVACYREQUIRED]") || strings.Contains(refused.tagLine, "AUTHENTICATIONFAILED") {
		t.Fatalf("privacy %s", refused.tagLine)
	}
	if err := startTLS(t, priv); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	after := priv.cmd(`LOGIN demo wrong`)
	if time.Since(started) < 180*time.Millisecond || !strings.Contains(after.tagLine, "[AUTHENTICATIONFAILED]") || strings.Contains(after.tagLine, "UNAVAILABLE") {
		t.Fatalf("privacy counted as a failure %s %s", time.Since(started), after.tagLine)
	}
	if !strings.Contains(priv.cmd(`LOGIN demo secret`).tagLine, "[UNAVAILABLE]") {
		t.Fatal("locked correct password was accepted")
	}
}

func TestReloadCert(t *testing.T) {
	certA, keyA := writeNamedCert(t, "alpha", 1)
	certB, keyB := writeNamedCert(t, "beta", 2)
	leafA := parsePEMCert(t, mustRead(t, certA))
	_, bound, cc := startCtrl(t, Config{
		Listen:            "127.0.0.1:0",
		ImplicitTLSListen: "127.0.0.1:0",
		SmtpListen:        "127.0.0.1:0",
		JmapListen:        "127.0.0.1:0",
		JmapTLSListen:     "127.0.0.1:0",
		SieveListen:       "127.0.0.1:0",
		CertFile:          certA,
		KeyFile:           keyA,
	})
	oldIMAP := tls.Client(dialTCP(t, bound.ImplicitTLSListen), &tls.Config{InsecureSkipVerify: true})
	if err := oldIMAP.Handshake(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { oldIMAP.Close() })
	if cn := oldIMAP.ConnectionState().PeerCertificates[0].Subject.CommonName; cn != "alpha" {
		t.Fatalf("implicit cn %s", cn)
	}
	if cc.call(map[string]any{"op": "reload-cert", "cert_file": certB}).Error != "missing field" {
		t.Fatal("one path")
	}
	if cn := oldIMAP.ConnectionState().PeerCertificates[0].Subject.CommonName; cn != "alpha" {
		t.Fatal("partial reload changed the live certificate")
	}
	if cc.call(map[string]any{"op": "reload-cert", "cert_file": certB, "key_file": filepath.Join(t.TempDir(), "missing.pem")}).OK {
		t.Fatal("missing key loaded")
	}
	if got := peerName(t, bound.ImplicitTLSListen); got != "alpha" {
		t.Fatalf("failed reload replaced cert %s", got)
	}
	cc.call(map[string]any{"op": "reload-cert", "cert_file": certB, "key_file": keyB}).ok(t)
	if cn := oldIMAP.ConnectionState().PeerCertificates[0].Subject.CommonName; cn != "alpha" {
		t.Fatalf("finished handshake changed to %s", cn)
	}
	if got := peerName(t, bound.ImplicitTLSListen); got != "beta" {
		t.Fatalf("implicit next %s", got)
	}
	cl := dial(t, bound.Listen)
	if err := startTLS(t, cl); err != nil {
		t.Fatal(err)
	}
	if cn := cl.c.(*tls.Conn).ConnectionState().PeerCertificates[0].Subject.CommonName; cn != "beta" {
		t.Fatalf("starttls cn %s", cn)
	}
	if got := smtpPeerName(t, bound.Smtp); got != "beta" {
		t.Fatalf("smtp cn %s", got)
	}
	if got := peerName(t, bound.JmapTLS); got != "beta" {
		t.Fatalf("jmap cn %s", got)
	}
	if got := sievePeerName(t, bound.Sieve); got != "beta" {
		t.Fatalf("sieve cn %s", got)
	}
	cc.call(map[string]any{"op": "reload-cert"}).ok(t)
	if got := peerName(t, bound.ImplicitTLSListen); got != "alpha" {
		t.Fatalf("reread configured %s", got)
	}
	cc.call(map[string]any{"op": "reload-cert", "cert_file": certB, "key_file": keyB}).ok(t)
	cc.arm(map[string]any{"do": "expired", "times": 1})
	if err := dialImplicitChecked(t, bound.ImplicitTLSListen, leafA, "date"); err == nil || !strings.Contains(err.Error(), "date") || strings.Contains(err.Error(), "problem") {
		t.Fatalf("expired fault did not win: %v", err)
	}
	cc.call(map[string]any{"op": "clear-faults"}).ok(t)
	if got := peerName(t, bound.ImplicitTLSListen); got != "beta" {
		t.Fatalf("after fault %s", got)
	}

	_, bound, cc = startCtrl(t, Config{Listen: "127.0.0.1:0"})
	plain := dial(t, bound.Listen)
	if strings.Contains(plain.greeting, "LOGINDISABLED") || strings.Contains(plain.greeting, "STARTTLS") {
		t.Fatalf("greeting before cert %q", plain.greeting)
	}
	cc.call(map[string]any{"op": "reload-cert", "cert_file": certA, "key_file": keyA}).ok(t)
	again := dial(t, bound.Listen)
	if !strings.Contains(again.greeting, "LOGINDISABLED") || !strings.Contains(again.greeting, "STARTTLS") {
		t.Fatalf("greeting after cert %q", again.greeting)
	}
	refused := again.cmd(`LOGIN demo secret`)
	if refused.status != "NO" || !strings.Contains(refused.tagLine, "[PRIVACYREQUIRED]") {
		t.Fatalf("login after cert load %s", refused.tagLine)
	}
}

func cmdLiteral(t *testing.T, cl *imapClient, prefix string, raw []byte, suffix string) *reply {
	t.Helper()
	cl.n++
	tag := fmt.Sprintf("a%d", cl.n)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s{%d+}\r\n", tag, prefix, len(raw))
	b.Write(raw)
	b.WriteString(suffix)
	cl.send(b.String() + "\r\n")
	return cl.readTag(tag)
}

func dialTCP(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func peerName(t *testing.T, addr string) string {
	t.Helper()
	c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.ConnectionState().PeerCertificates) == 0 {
		t.Fatal("no peer certificate")
	}
	return c.ConnectionState().PeerCertificates[0].Subject.CommonName
}

func smtpPeerName(t *testing.T, addr string) string {
	t.Helper()
	cl := dialSMTP(t, addr)
	if code, _ := cl.cmd("EHLO client.example"); code != 250 {
		t.Fatalf("ehlo %d", code)
	}
	if code, _ := cl.cmd("STARTTLS"); code != 220 {
		t.Fatalf("smtp starttls %d", code)
	}
	tc := tls.Client(cl.c, &tls.Config{InsecureSkipVerify: true})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	return tc.ConnectionState().PeerCertificates[0].Subject.CommonName
}

func sievePeerName(t *testing.T, addr string) string {
	t.Helper()
	cl := dialSieve(t, addr)
	cl.greeting()
	if !strings.HasPrefix(cl.transact("STARTTLS"), "OK") {
		t.Fatal("sieve starttls")
	}
	tc := tls.Client(cl.c, &tls.Config{InsecureSkipVerify: true})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	return tc.ConnectionState().PeerCertificates[0].Subject.CommonName
}

func startTLS(t *testing.T, cl *imapClient) error {
	t.Helper()
	cl.cmd("STARTTLS").ok(t)
	tc := tls.Client(cl.c, &tls.Config{InsecureSkipVerify: true})
	if err := tc.Handshake(); err != nil {
		return err
	}
	cl.c = tc
	cl.r = bufio.NewReader(tc)
	cl.w = bufio.NewWriter(tc)
	return nil
}

func writeNamedCert(t *testing.T, cn string, serial int64) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: cn},
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
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
