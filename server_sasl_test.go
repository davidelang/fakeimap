package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"hash"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSCRAMRFC5802(t *testing.T) {
	salt, err := base64.StdEncoding.DecodeString("QSXCR+Q6sek8bf92")
	if err != nil {
		t.Fatal(err)
	}
	bare := "n=user,r=fyko+d2lbbFgONRv9qkxdawL"
	server := "r=fyko+d2lbbFgONRv9qkxdawL3rfcNHYJY1ZVvWVs7j,s=QSXCR+Q6sek8bf92,i=4096"
	without := "c=biws,r=fyko+d2lbbFgONRv9qkxdawL3rfcNHYJY1ZVvWVs7j"
	proof, sig := scramProof(sha1.New, []byte("pencil"), salt, 4096, bare+","+server+","+without)
	if b64std(proof) != "v0X8v3Bz2T0CJGbJQyF0X+HI4Ts=" {
		t.Fatalf("proof %s", b64std(proof))
	}
	if b64std(sig) != "rmF9pqV8S7suAoZWja4dJRkFsKQ=" {
		t.Fatalf("server %s", b64std(sig))
	}
	gs2, bareGot, flag, cbind, zid, ok := parseClientFirst("n,," + bare)
	if !ok || gs2 != "n,," || bareGot != bare || flag != "n" || cbind != "" || zid != "" {
		t.Fatalf("parse %+v %q %q %q", ok, gs2, bareGot, flag)
	}
}

func TestCRAMRFC2195(t *testing.T) {
	got := cramHex("tanstaaftanstaaf", "<1896.697170952@postoffice.reston.mci.net>")
	if got != "b913a602c7eda7a495b4e6e7334d3890" {
		t.Fatalf("cram %s", got)
	}
}

func TestSaslPersonality(t *testing.T) {
	cyrus, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	sasl, err := loadPersonality("personalities/sasl.json")
	if err != nil {
		t.Fatal(err)
	}
	if cyrus.Delimiter != sasl.Delimiter || cyrus.Greeting != sasl.Greeting || cyrus.LogoutBye != sasl.LogoutBye ||
		cyrus.OtherUsers != sasl.OtherUsers || cyrus.PersonalPrefix != sasl.PersonalPrefix ||
		!sameString(cyrus.SharedFolders, sasl.SharedFolders) ||
		cyrus.SelectFlags != sasl.SelectFlags || cyrus.PermanentFlags != sasl.PermanentFlags ||
		cyrus.FetchPeekBody != sasl.FetchPeekBody || cyrus.FetchSeenFlags != sasl.FetchSeenFlags ||
		cyrus.MoveCopyUIDUntagged != sasl.MoveCopyUIDUntagged ||
		strings.Join(cyrus.SelectOrder, " ") != strings.Join(sasl.SelectOrder, " ") ||
		strings.Join(cyrus.FlagOrder, " ") != strings.Join(sasl.FlagOrder, " ") ||
		strings.Join(cyrus.FetchItemOrder, " ") != strings.Join(sasl.FetchItemOrder, " ") ||
		!sameCapSet(dropCap(dropCap(cyrus.PostLogin, "SEARCH=FUZZY"), "INPROGRESS"), sasl.PostLogin) {
		t.Fatalf("mailbox behavior drifted")
	}
	want := []string{"AUTH=LOGIN", "AUTH=CRAM-MD5", "AUTH=SCRAM-SHA-1", "AUTH=SCRAM-SHA-256", "AUTH=SCRAM-SHA-1-PLUS", "AUTH=SCRAM-SHA-256-PLUS", "AUTH=EXTERNAL"}
	for _, cap := range want {
		if !sasl.lists(cap) {
			t.Fatalf("sasl missing %s", cap)
		}
	}
	if !cyrus.lists("AUTH=LOGIN") {
		t.Fatal("cyrus missing AUTH=LOGIN")
	}
	for _, cap := range want[1:] {
		if cyrus.lists(cap) {
			t.Fatalf("cyrus lists %s", cap)
		}
	}
	if sasl.lists("AUTH=DIGEST-MD5") || cyrus.lists("AUTH=DIGEST-MD5") {
		t.Fatal("DIGEST-MD5 listed")
	}
	if listed(sasl.GreetingCaps, "ID") || listed(sasl.PreLogin, "ID") || !listed(sasl.PostLogin, "ID") {
		t.Fatal("sasl ID placement")
	}
}

func sameCapSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, c := range a {
		seen[c]++
	}
	for _, c := range b {
		seen[c]--
		if seen[c] < 0 {
			return false
		}
	}
	return true
}

func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestUnlistedMechanisms(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	cl := dial(t, bound.Listen)
	for _, c := range append(capsOf(cl.greeting), capsOf(cl.cmd("CAPABILITY").mustOK(t).untagged[0])...) {
		u := strings.ToUpper(c)
		if u == "AUTH=CRAM-MD5" || u == "AUTH=EXTERNAL" || strings.Contains(u, "SCRAM") || strings.Contains(u, "DIGEST-MD5") {
			t.Fatalf("advertised %s", c)
		}
	}
	for _, mech := range []string{"CRAM-MD5", "SCRAM-SHA-1", "SCRAM-SHA-256", "SCRAM-SHA-1-PLUS", "SCRAM-SHA-256-PLUS", "EXTERNAL", "DIGEST-MD5"} {
		rp := cl.cmd("AUTHENTICATE " + mech)
		if rp.status != "NO" {
			t.Fatalf("%s status %s", mech, rp.status)
		}
	}
}

func TestDigestMD5NotImplemented(t *testing.T) {
	sasl, err := loadPersonality("personalities/sasl.json")
	if err != nil {
		t.Fatal(err)
	}
	sasl.PreLogin = append(append([]string{}, sasl.PreLogin...), "AUTH=DIGEST-MD5")
	sasl.GreetingCaps = append(append([]string{}, sasl.GreetingCaps...), "AUTH=DIGEST-MD5")
	b, err := json.Marshal(sasl)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	for _, c := range capsOf(cl.greeting) {
		if strings.Contains(strings.ToUpper(c), "DIGEST-MD5") {
			t.Fatalf("advertised %s", c)
		}
	}
	requireCaps(t, capsOf(cl.greeting), []string{
		"AUTH=LOGIN", "AUTH=CRAM-MD5", "AUTH=SCRAM-SHA-1", "AUTH=SCRAM-SHA-256",
		"AUTH=SCRAM-SHA-1-PLUS", "AUTH=SCRAM-SHA-256-PLUS", "AUTH=EXTERNAL",
	})
	cl.cmd("AUTHENTICATE DIGEST-MD5").no(t)
}

func TestAuthenticateLogin(t *testing.T) {
	addr := saslListen(t)
	cl := dial(t, addr)
	cl.send("l1 AUTHENTICATE LOGIN\r\n")
	if got := prompt(t, cl); got != "Username:" {
		t.Fatalf("prompt %q", got)
	}
	cl.send(b64std([]byte("demo")) + "\r\n")
	if got := prompt(t, cl); got != "Password:" {
		t.Fatalf("prompt %q", got)
	}
	cl.send(b64std([]byte("secret")) + "\r\n")
	cl.readTag("l1").ok(t)

	cl2 := dial(t, addr)
	cl2.send("l2 AUTHENTICATE LOGIN\r\n")
	if got := prompt(t, cl2); got != "Username:" {
		t.Fatalf("prompt %q", got)
	}
	cl2.send(b64std([]byte("demo")) + "\r\n")
	if got := prompt(t, cl2); got != "Password:" {
		t.Fatalf("prompt %q", got)
	}
	cl2.send(b64std([]byte("nope")) + "\r\n")
	if cl2.readTag("l2").status != "NO" {
		t.Fatal("want NO")
	}

	cl3 := dial(t, addr)
	cl3.send("l3 AUTHENTICATE LOGIN " + b64std([]byte("demo")) + "\r\n")
	if got := prompt(t, cl3); got != "Password:" {
		t.Fatalf("prompt %q", got)
	}
	cl3.send(b64std([]byte("secret")) + "\r\n")
	cl3.readTag("l3").ok(t)
}

func saslListen(t *testing.T) string {
	t.Helper()
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/sasl.json"})
	return bound.Listen
}

func prompt(t *testing.T, cl *imapClient) string {
	t.Helper()
	line := strings.TrimRight(cl.readRec(), "\r\n")
	if !strings.HasPrefix(line, "+ ") {
		t.Fatalf("prompt %q", line)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "+ "))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAuthenticateCRAM(t *testing.T) {
	addr := saslListen(t)
	cl := dial(t, addr)
	if status := doCRAM(t, cl, "demo", "secret"); status != "OK" {
		t.Fatalf("cram %s", status)
	}
	cl2 := dial(t, addr)
	if status := doCRAM(t, cl2, "demo", "nope"); status != "NO" {
		t.Fatalf("cram reject %s", status)
	}
}

func doCRAM(t *testing.T, cl *imapClient, user, pass string) string {
	t.Helper()
	cl.n++
	tag := "c" + strconv.Itoa(cl.n)
	cl.send(tag + " AUTHENTICATE CRAM-MD5\r\n")
	line := strings.TrimRight(cl.readRec(), "\r\n")
	if !strings.HasPrefix(line, "+ ") {
		t.Fatalf("challenge %q", line)
	}
	ch, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "+ "))
	if err != nil {
		t.Fatal(err)
	}
	resp := user + " " + cramHex(pass, string(ch))
	cl.send(b64std([]byte(resp)) + "\r\n")
	return cl.readTag(tag).status
}

func TestAuthenticateSCRAM(t *testing.T) {
	addr := saslListen(t)
	for _, mech := range []string{"SCRAM-SHA-1", "SCRAM-SHA-256"} {
		cl := dial(t, addr)
		if status := doScram(t, cl, mech, "demo", "secret", "n,,", nil, false, false); status != "OK" {
			t.Fatalf("%s %s", mech, status)
		}
		cl2 := dial(t, addr)
		if status := doScram(t, cl2, mech, "demo", "nope", "n,,", nil, false, false); status != "NO" {
			t.Fatalf("%s bad pass %s", mech, status)
		}
		cl3 := dial(t, addr)
		if status := doScram(t, cl3, mech, "demo", "secret", "n,,", nil, true, false); status != "NO" {
			t.Fatalf("%s bad proof %s", mech, status)
		}
	}
	cl := dial(t, addr)
	cl.send("z1 AUTHENTICATE SCRAM-SHA-256\r\n")
	if line := strings.TrimRight(cl.readRec(), "\r\n"); !strings.HasPrefix(line, "+") {
		t.Fatalf("challenge %q", line)
	}
	cl.send("*\r\n")
	if rp := cl.readTag("z1"); rp.status != "BAD" {
		t.Fatalf("cancel %+v", rp)
	}
	cl2 := dial(t, addr)
	if status := doScram(t, cl2, "SCRAM-SHA-256", "demo", "secret", "n,,", nil, false, true); status != "OK" {
		t.Fatalf("initial response %s", status)
	}
	cl3 := dial(t, addr)
	if status := doScram(t, cl3, "SCRAM-SHA-1", "demo", "secret", "y,,", nil, false, false); status != "NO" {
		t.Fatalf("downgrade flag %s", status)
	}
}

func doScram(t *testing.T, cl *imapClient, mech, user, pass, gs2 string, cbind []byte, badProof, useIR bool) string {
	t.Helper()
	h := scramNew(mech)
	nonce := "rO3nonceValue9qkxdawL"
	bare := "n=" + user + ",r=" + nonce
	first := gs2 + bare
	cl.n++
	tag := "s" + strconv.Itoa(cl.n)
	if useIR {
		cl.send(tag + " AUTHENTICATE " + mech + " " + b64std([]byte(first)) + "\r\n")
	} else {
		cl.send(tag + " AUTHENTICATE " + mech + "\r\n")
		line := strings.TrimRight(cl.readRec(), "\r\n")
		if strings.HasPrefix(line, tag+" ") {
			return statusOf(line)
		}
		if !strings.HasPrefix(line, "+") {
			t.Fatalf("challenge %q", line)
		}
		cl.send(b64std([]byte(first)) + "\r\n")
	}
	sfLine := strings.TrimRight(cl.readRec(), "\r\n")
	if strings.HasPrefix(sfLine, tag+" ") {
		return statusOf(sfLine)
	}
	if !strings.HasPrefix(sfLine, "+ ") {
		t.Fatalf("server-first %q", sfLine)
	}
	sfRaw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sfLine, "+ "))
	if err != nil {
		t.Fatal(err)
	}
	sf := string(sfRaw)
	vals := map[string]string{}
	for _, p := range strings.Split(sf, ",") {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			t.Fatalf("server-first %q", sf)
		}
		vals[k] = v
	}
	if !strings.HasPrefix(vals["r"], nonce) || vals["r"] == nonce {
		t.Fatalf("nonce %q", vals["r"])
	}
	salt, err := base64.StdEncoding.DecodeString(vals["s"])
	if err != nil {
		t.Fatal(err)
	}
	iter, err := strconv.Atoi(vals["i"])
	if err != nil || iter < 4096 {
		t.Fatalf("iter %q", vals["i"])
	}
	c := b64std(append([]byte(gs2), cbind...))
	without := "c=" + c + ",r=" + vals["r"]
	proof, sig := scramProof(h, []byte(pass), salt, iter, bare+","+sf+","+without)
	if badProof {
		proof[0] ^= 0xff
	}
	final := without + ",p=" + b64std(proof)
	cl.send(b64std([]byte(final)) + "\r\n")
	line := strings.TrimRight(cl.readRec(), "\r\n")
	if strings.HasPrefix(line, tag+" ") {
		return statusOf(line)
	}
	if !strings.HasPrefix(line, "+ ") {
		t.Fatalf("server-final %q", line)
	}
	vf, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "+ "))
	if err != nil {
		t.Fatal(err)
	}
	if string(vf) != "v="+b64std(sig) {
		t.Fatalf("server sig %q", vf)
	}
	cl.send("\r\n")
	return cl.readTag(tag).status
}

func scramNew(mech string) func() hash.Hash {
	if strings.Contains(mech, "SHA-1") {
		return sha1.New
	}
	return sha256.New
}

func statusOf(line string) string {
	fs := strings.Fields(line)
	if len(fs) < 2 {
		return ""
	}
	return fs[1]
}

func TestSCRAMPlus(t *testing.T) {
	cert, key := writeCert(t)
	_, bound := start(t, Config{
		Listen:            "127.0.0.1:0",
		ImplicitTLSListen: "127.0.0.1:0",
		CertFile:          cert,
		KeyFile:           key,
		Personality:       "personalities/sasl.json",
	})
	cl := dial(t, bound.Listen)
	if status := cl.cmd("AUTHENTICATE SCRAM-SHA-1-PLUS").status; status != "NO" {
		t.Fatalf("cleartext SHA-1-PLUS %s", status)
	}
	if status := cl.cmd("AUTHENTICATE SCRAM-SHA-256-PLUS").status; status != "NO" {
		t.Fatalf("cleartext SHA-256-PLUS %s", status)
	}

	for _, mech := range []string{"SCRAM-SHA-1-PLUS", "SCRAM-SHA-256-PLUS"} {
		c12 := dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, nil)
		unique := c12.c.(*tls.Conn).ConnectionState().TLSUnique
		if len(unique) == 0 {
			t.Fatal("no tls-unique")
		}
		if status := doScram(t, c12, mech, "demo", "secret", "p=tls-unique,,", unique, false, false); status != "OK" {
			t.Fatalf("%s tls-unique %s", mech, status)
		}
		c12b := dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, nil)
		bad := append([]byte(nil), c12b.c.(*tls.Conn).ConnectionState().TLSUnique...)
		bad[len(bad)-1] ^= 0xff
		if status := doScram(t, c12b, mech, "demo", "secret", "p=tls-unique,,", bad, false, false); status != "NO" {
			t.Fatalf("%s tls-unique mismatch %s", mech, status)
		}

		c13 := dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS13, nil)
		st := c13.c.(*tls.Conn).ConnectionState()
		if len(st.TLSUnique) != 0 {
			t.Fatal("tls 1.3 has tls-unique")
		}
		exp, err := st.ExportKeyingMaterial("EXPORTER-Channel-Binding", []byte{}, 32)
		if err != nil || len(exp) != 32 {
			t.Fatalf("exporter %v %d", err, len(exp))
		}
		if status := doScram(t, c13, mech, "demo", "secret", "p=tls-exporter,,", exp, false, false); status != "OK" {
			t.Fatalf("%s tls-exporter %s", mech, status)
		}
		c13b := dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS13, nil)
		st = c13b.c.(*tls.Conn).ConnectionState()
		exp, err = st.ExportKeyingMaterial("EXPORTER-Channel-Binding", []byte{}, 32)
		if err != nil {
			t.Fatal(err)
		}
		exp[0] ^= 0xff
		if status := doScram(t, c13b, mech, "demo", "secret", "p=tls-exporter,,", exp, false, false); status != "NO" {
			t.Fatalf("%s tls-exporter mismatch %s", mech, status)
		}
	}

	raw := dial(t, bound.Listen)
	raw.cmd("STARTTLS").ok(t)
	tc := tls.Client(raw.c, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	raw.c = tc
	raw.r = bufio.NewReader(tc)
	raw.w = bufio.NewWriter(tc)
	if len(tc.ConnectionState().TLSUnique) == 0 {
		t.Fatal("starttls tls-unique")
	}
	if status := doScram(t, raw, "SCRAM-SHA-256-PLUS", "demo", "secret", "p=tls-unique,,", tc.ConnectionState().TLSUnique, false, false); status != "OK" {
		t.Fatalf("starttls plus %s", status)
	}
}

func dialTLSVersion(t *testing.T, addr string, version uint16, client *tls.Certificate) *imapClient {
	t.Helper()
	cfg := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         version,
		MaxVersion:         version,
	}
	if client != nil {
		cfg.Certificates = []tls.Certificate{*client}
	}
	c, err := tls.Dial("tcp", addr, cfg)
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

func TestAuthenticateExternal(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{
	  "accounts": [
	    {"username": "demo", "password": "secret"},
	    {"username": "demo@example.com", "password": "secret"},
	    {"username": "mail.example.com", "password": "secret"}
	  ],
	  "mailboxes": [{"name": "INBOX"}]
	}`)
	if err := os.WriteFile(filepath.Join(dir, "dataset.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	ca, caKey, caPath := newTestCA(t)
	cert, key := writeCert(t)
	_, bound := start(t, Config{
		Listen:            "127.0.0.1:0",
		ImplicitTLSListen: "127.0.0.1:0",
		CertFile:          cert,
		KeyFile:           key,
		ClientCAFile:      caPath,
		Personality:       "personalities/sasl.json",
		Dataset:           dir,
	})
	cn := leafCert(t, ca, caKey, "demo", nil, nil)
	if status := externalAuth(t, dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, &cn), "AUTHENTICATE EXTERNAL ="); status != "OK" {
		t.Fatalf("cn %s", status)
	}
	email := leafCert(t, ca, caKey, "not-an-account", []string{"demo@example.com"}, nil)
	if status := externalAuth(t, dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, &email), "AUTHENTICATE EXTERNAL ="); status != "OK" {
		t.Fatalf("email %s", status)
	}
	dns := leafCert(t, ca, caKey, "not-an-account", nil, []string{"mail.example.com"})
	if status := externalAuth(t, dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, &dns), "AUTHENTICATE EXTERNAL ="); status != "OK" {
		t.Fatalf("dns %s", status)
	}
	none := leafCert(t, ca, caKey, "not-an-account", []string{"other@example.com"}, nil)
	if status := externalAuth(t, dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, &none), "AUTHENTICATE EXTERNAL ="); status != "NO" {
		t.Fatalf("unknown name %s", status)
	}
	if status := externalAuth(t, dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, nil), "AUTHENTICATE EXTERNAL ="); status != "NO" {
		t.Fatalf("no cert %s", status)
	}
	matched := leafCert(t, ca, caKey, "demo", nil, nil)
	if status := externalAuth(t, dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, &matched), "AUTHENTICATE EXTERNAL "+b64std([]byte("demo"))); status != "OK" {
		t.Fatalf("authzid %s", status)
	}
	other := leafCert(t, ca, caKey, "demo", nil, nil)
	if status := externalAuth(t, dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, &other), "AUTHENTICATE EXTERNAL "+b64std([]byte("other"))); status != "NO" {
		t.Fatalf("bad authzid %s", status)
	}
	withCert := leafCert(t, ca, caKey, "demo", nil, nil)
	plainCl := dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, &withCert)
	plainCl.cmd("AUTHENTICATE PLAIN " + plainToken("demo", "secret")).ok(t)
	dialTLSVersion(t, bound.ImplicitTLSListen, tls.VersionTLS12, nil).cmd("AUTHENTICATE PLAIN " + plainToken("demo", "secret")).ok(t)

	clear := dial(t, bound.Listen)
	if clear.cmd("AUTHENTICATE EXTERNAL =").status != "NO" {
		t.Fatal("cleartext external")
	}
	clear.cmd("STARTTLS").ok(t)
	startCert := leafCert(t, ca, caKey, "demo", nil, nil)
	tc := tls.Client(clear.c, &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		Certificates:       []tls.Certificate{startCert},
	})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	clear.c = tc
	clear.r = bufio.NewReader(tc)
	clear.w = bufio.NewWriter(tc)
	if status := externalAuth(t, clear, "AUTHENTICATE EXTERNAL ="); status != "OK" {
		t.Fatalf("starttls external %s", status)
	}
}

func externalAuth(t *testing.T, cl *imapClient, cmd string) string {
	t.Helper()
	return cl.cmd(cmd).status
}

func newTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pemBytes, 0644); err != nil {
		t.Fatal(err)
	}
	return ca, key, path
}

var leafSerial int64 = 2

func leafCert(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, emails, dns []string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafSerial++
	tmpl := &x509.Certificate{
		SerialNumber:   big.NewInt(leafSerial),
		Subject:        pkix.Name{CommonName: cn},
		NotBefore:      time.Now().Add(-time.Hour),
		NotAfter:       time.Now().Add(24 * time.Hour),
		KeyUsage:       x509.KeyUsageDigitalSignature,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		EmailAddresses: emails,
		DNSNames:       dns,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
