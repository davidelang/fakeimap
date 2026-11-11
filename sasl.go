package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"net"
	"strings"
	"time"
)

var errAuthCancelled = errors.New("sasl cancelled")

const scramIters = 4096

func (s *session) authFail(tag string) error {
	return s.no(tag, "[AUTHENTICATIONFAILED] AUTHENTICATE failed")
}

func (s *session) authErr(tag string, err error) error {
	if errors.Is(err, errAuthCancelled) {
		return s.bad(tag, "AUTHENTICATE cancelled")
	}
	return err
}

// saslRead sends one continuation and reads the client response.
// An empty challenge is a bare "+". A "*" response is errAuthCancelled.
func (s *session) saslRead(challenge string) (string, error) {
	line := "+"
	if challenge != "" {
		line = "+ " + challenge
	}
	if err := s.sendLine(line); err != nil {
		return "", err
	}
	resp, err := s.readRawLine()
	if err != nil {
		return "", err
	}
	if resp == "*" {
		return "", errAuthCancelled
	}
	return resp, nil
}

func decodeSASL(s string) ([]byte, error) {
	if s == "" || s == "=" {
		return []byte{}, nil
	}
	return decodeB64(s)
}

func b64std(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func (s *session) authPlain(tag string, hasIR bool, ir string) error {
	b64 := ir
	if !hasIR {
		var err error
		b64, err = s.saslRead("")
		if err != nil {
			return s.authErr(tag, err)
		}
	}
	raw, err := decodeB64(b64)
	if err != nil {
		return s.authFail(tag)
	}
	user, pass, ok := plainCreds(raw)
	if !ok {
		return s.authFail(tag)
	}
	return s.loginOK(tag, "AUTHENTICATE", user, pass)
}

func (s *session) authLogin(tag string, hasIR bool, ir string) error {
	var user string
	if hasIR {
		raw, err := decodeSASL(ir)
		if err != nil || len(raw) == 0 {
			return s.authFail(tag)
		}
		user = string(raw)
	} else {
		line, err := s.saslRead(b64std([]byte("Username:")))
		if err != nil {
			return s.authErr(tag, err)
		}
		raw, err := decodeSASL(line)
		if err != nil || len(raw) == 0 {
			return s.authFail(tag)
		}
		user = string(raw)
	}
	line, err := s.saslRead(b64std([]byte("Password:")))
	if err != nil {
		return s.authErr(tag, err)
	}
	raw, err := decodeSASL(line)
	if err != nil {
		return s.authFail(tag)
	}
	return s.loginOK(tag, "AUTHENTICATE", user, string(raw))
}

func cramHex(password, challenge string) string {
	m := hmac.New(md5.New, []byte(password))
	m.Write([]byte(challenge))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *session) authCRAM(tag string, hasIR bool, ir string) error {
	if hasIR && ir != "" && ir != "=" {
		return s.authFail(tag)
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	challenge := fmt.Sprintf("<%d.%x@fakeimap>", time.Now().UnixNano(), nonce)
	line, err := s.saslRead(b64std([]byte(challenge)))
	if err != nil {
		return s.authErr(tag, err)
	}
	raw, err := decodeSASL(line)
	if err != nil {
		return s.authFail(tag)
	}
	user, digest, ok := strings.Cut(string(raw), " ")
	if !ok || user == "" || strings.Contains(digest, " ") {
		return s.authFail(tag)
	}
	pass, found := s.srv.store.secret(user)
	if !found {
		pass = "secret"
	}
	sum, decErr := hex.DecodeString(digest)
	want, _ := hex.DecodeString(cramHex(pass, challenge))
	if decErr != nil || !found || !hmac.Equal(sum, want) {
		return s.authFail(tag)
	}
	s.user = user
	return s.authDone(tag, "AUTHENTICATE")
}

func scramHash(mech string) (func() hash.Hash, string, bool) {
	switch mech {
	case "SCRAM-SHA-1", "SCRAM-SHA-1-PLUS":
		return sha1.New, "SCRAM-SHA-1", true
	case "SCRAM-SHA-256", "SCRAM-SHA-256-PLUS":
		return sha256.New, "SCRAM-SHA-256", true
	default:
		return nil, "", false
	}
}

func hi(newHash func() hash.Hash, password, salt []byte, iter int) []byte {
	mac := hmac.New(newHash, password)
	mac.Write(salt)
	var be [4]byte
	be[3] = 1
	mac.Write(be[:])
	u := mac.Sum(nil)
	out := make([]byte, len(u))
	copy(out, u)
	for n := 1; n < iter; n++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(nil)
		for i := range out {
			out[i] ^= u[i]
		}
	}
	return out
}

func hmacSum(newHash func() hash.Hash, key, msg []byte) []byte {
	m := hmac.New(newHash, key)
	m.Write(msg)
	return m.Sum(nil)
}

func hashSum(newHash func() hash.Hash, msg []byte) []byte {
	h := newHash()
	h.Write(msg)
	return h.Sum(nil)
}

// scramProof is ClientProof and ServerSignature for one SCRAM exchange.
func scramProof(newHash func() hash.Hash, password, salt []byte, iter int, authMsg string) (proof, serverSig []byte) {
	salted := hi(newHash, password, salt, iter)
	clientKey := hmacSum(newHash, salted, []byte("Client Key"))
	storedKey := hashSum(newHash, clientKey)
	clientSig := hmacSum(newHash, storedKey, []byte(authMsg))
	proof = make([]byte, len(clientKey))
	for i := range proof {
		proof[i] = clientKey[i] ^ clientSig[i]
	}
	serverKey := hmacSum(newHash, salted, []byte("Server Key"))
	serverSig = hmacSum(newHash, serverKey, []byte(authMsg))
	return proof, serverSig
}

func scramUnescape(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '=' {
			b.WriteByte(s[i])
			continue
		}
		if i+3 > len(s) {
			return "", false
		}
		switch s[i : i+3] {
		case "=2C":
			b.WriteByte(',')
		case "=3D":
			b.WriteByte('=')
		default:
			return "", false
		}
		i += 2
	}
	return b.String(), true
}

func scramAttrs(s string) (map[string]string, []string, bool) {
	if s == "" {
		return nil, nil, false
	}
	parts := strings.Split(s, ",")
	vals := make(map[string]string, len(parts))
	order := make([]string, 0, len(parts))
	for _, p := range parts {
		eq := strings.IndexByte(p, '=')
		if eq != 1 {
			return nil, nil, false
		}
		k := p[:eq]
		if _, dup := vals[k]; dup {
			return nil, nil, false
		}
		vals[k] = p[eq+1:]
		order = append(order, k)
	}
	return vals, order, true
}

func printableNonce(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e || s[i] == ',' {
			return false
		}
	}
	return true
}

// parseClientFirst splits a SCRAM client-first message.
// gs2 is the raw GS2 header, including its trailing comma.
func parseClientFirst(msg string) (gs2, bare, flag, cbindName, authzid string, ok bool) {
	var i int
	switch {
	case strings.HasPrefix(msg, "n,"):
		flag = "n"
		i = 2
	case strings.HasPrefix(msg, "y,"):
		flag = "y"
		i = 2
	case strings.HasPrefix(msg, "p="):
		comma := strings.IndexByte(msg, ',')
		if comma < 3 {
			return "", "", "", "", "", false
		}
		flag = "p"
		cbindName = msg[2:comma]
		if cbindName == "" || strings.ContainsAny(cbindName, ",=") {
			return "", "", "", "", "", false
		}
		i = comma + 1
	default:
		return "", "", "", "", "", false
	}
	if i > len(msg) {
		return "", "", "", "", "", false
	}
	rest := msg[i:]
	if strings.HasPrefix(rest, "a=") {
		comma := strings.IndexByte(rest, ',')
		if comma < 0 {
			return "", "", "", "", "", false
		}
		authzid = rest[2:comma]
		rest = rest[comma+1:]
	} else if strings.HasPrefix(rest, ",") {
		rest = rest[1:]
	} else {
		return "", "", "", "", "", false
	}
	if !strings.HasPrefix(rest, "n=") {
		return "", "", "", "", "", false
	}
	bare = rest
	gs2 = msg[:len(msg)-len(bare)]
	return gs2, bare, flag, cbindName, authzid, true
}

func (s *session) authSCRAM(tag, mech string, hasIR bool, ir string) error {
	newHash, base, ok := scramHash(mech)
	if !ok {
		return s.authFail(tag)
	}
	plus := strings.HasSuffix(mech, "-PLUS")
	if plus && !s.secure {
		return s.authFail(tag)
	}
	rawLine := ir
	if !hasIR {
		var err error
		rawLine, err = s.saslRead("")
		if err != nil {
			return s.authErr(tag, err)
		}
	}
	raw, err := decodeSASL(rawLine)
	if err != nil || len(raw) == 0 {
		return s.authFail(tag)
	}
	gs2, bare, flag, cbindName, authzidRaw, ok := parseClientFirst(string(raw))
	if !ok {
		return s.authFail(tag)
	}
	attrs, order, ok := scramAttrs(bare)
	if !ok || len(order) < 2 || order[0] != "n" || order[1] != "r" {
		return s.authFail(tag)
	}
	if _, bad := attrs["m"]; bad {
		return s.authFail(tag)
	}
	user, ok := scramUnescape(attrs["n"])
	if !ok || user == "" {
		return s.authFail(tag)
	}
	if authzidRaw != "" {
		zid, ok := scramUnescape(authzidRaw)
		if !ok || zid != user {
			return s.authFail(tag)
		}
	}
	clientNonce := attrs["r"]
	if !printableNonce(clientNonce) {
		return s.authFail(tag)
	}
	if flag == "y" && s.pers.lists("AUTH="+base+"-PLUS") {
		return s.authFail(tag)
	}
	if plus {
		if flag != "p" || (cbindName != "tls-unique" && cbindName != "tls-exporter") {
			return s.authFail(tag)
		}
	} else if flag == "p" {
		return s.authFail(tag)
	}
	var serverNonceBuf [18]byte
	if _, err := rand.Read(serverNonceBuf[:]); err != nil {
		return err
	}
	fullNonce := clientNonce + base64.RawStdEncoding.EncodeToString(serverNonceBuf[:])
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	serverFirst := fmt.Sprintf("r=%s,s=%s,i=%d", fullNonce, b64std(salt), scramIters)
	finalLine, err := s.saslRead(b64std([]byte(serverFirst)))
	if err != nil {
		return s.authErr(tag, err)
	}
	finalRaw, err := decodeSASL(finalLine)
	if err != nil || len(finalRaw) == 0 {
		return s.authFail(tag)
	}
	final := string(finalRaw)
	pAt := strings.LastIndex(final, ",p=")
	if pAt < 0 || !strings.HasPrefix(final, "c=") {
		return s.authFail(tag)
	}
	without := final[:pAt]
	gotProof, err := decodeB64(final[pAt+3:])
	if err != nil {
		return s.authFail(tag)
	}
	fattrs, forder, ok := scramAttrs(without)
	if !ok || len(forder) < 2 || forder[0] != "c" || forder[1] != "r" {
		return s.authFail(tag)
	}
	if _, bad := fattrs["m"]; bad {
		return s.authFail(tag)
	}
	if fattrs["r"] != fullNonce {
		return s.authFail(tag)
	}
	cRaw, err := decodeB64(fattrs["c"])
	if err != nil {
		return s.authFail(tag)
	}
	wantC := []byte(gs2)
	if plus {
		cb, ok := s.channelBinding(cbindName)
		if !ok {
			return s.authFail(tag)
		}
		wantC = append(wantC, cb...)
	}
	if !bytes.Equal(cRaw, wantC) {
		return s.authFail(tag)
	}
	pass, found := s.srv.store.secret(user)
	if !found {
		pass = "secret"
	}
	authMsg := bare + "," + serverFirst + "," + without
	proof, serverSig := scramProof(newHash, []byte(pass), salt, scramIters, authMsg)
	if !found || !hmac.Equal(gotProof, proof) {
		return s.authFail(tag)
	}
	serverFinal := "v=" + b64std(serverSig)
	ack, err := s.saslRead(b64std([]byte(serverFinal)))
	if err != nil {
		return s.authErr(tag, err)
	}
	if ack != "" && ack != "=" {
		return s.bad(tag, "AUTHENTICATE syntax")
	}
	s.user = user
	return s.authDone(tag, "AUTHENTICATE")
}

func sessionTLS(c net.Conn) *tls.Conn {
	for i := 0; i < 8 && c != nil; i++ {
		switch t := c.(type) {
		case *tls.Conn:
			return t
		case *prefixConn:
			c = t.Conn
		default:
			return nil
		}
	}
	return nil
}

func (s *session) channelBinding(kind string) ([]byte, bool) {
	tc := sessionTLS(s.conn)
	if tc == nil {
		return nil, false
	}
	st := tc.ConnectionState()
	switch kind {
	case "tls-unique":
		if len(st.TLSUnique) == 0 {
			return nil, false
		}
		return append([]byte(nil), st.TLSUnique...), true
	case "tls-exporter":
		b, err := st.ExportKeyingMaterial("EXPORTER-Channel-Binding", []byte{}, 32)
		if err != nil || len(b) != 32 {
			return nil, false
		}
		return b, true
	default:
		return nil, false
	}
}

func (s *session) verifiedClientCert() *x509.Certificate {
	tc := sessionTLS(s.conn)
	if tc == nil {
		return nil
	}
	st := tc.ConnectionState()
	if len(st.VerifiedChains) == 0 || len(st.VerifiedChains[0]) == 0 {
		return nil
	}
	return st.VerifiedChains[0][0]
}

func (s *session) authExternal(tag string, hasIR bool, ir string) error {
	line := ir
	if !hasIR {
		var err error
		line, err = s.saslRead("")
		if err != nil {
			return s.authErr(tag, err)
		}
	}
	raw, err := decodeSASL(line)
	if err != nil {
		return s.authFail(tag)
	}
	if bytes.Contains(raw, []byte{0}) {
		return s.authFail(tag)
	}
	authzid := string(raw)
	cert := s.verifiedClientCert()
	if cert == nil {
		return s.authFail(tag)
	}
	names := certIdentities(cert)
	if authzid != "" {
		for _, n := range names {
			if n == authzid && s.srv.store.hasAccount(n) {
				s.user = n
				return s.authDone(tag, "AUTHENTICATE")
			}
		}
		return s.authFail(tag)
	}
	for _, n := range names {
		if s.srv.store.hasAccount(n) {
			s.user = n
			return s.authDone(tag, "AUTHENTICATE")
		}
	}
	return s.authFail(tag)
}

func certIdentities(c *x509.Certificate) []string {
	out := make([]string, 0, 1+len(c.EmailAddresses)+len(c.DNSNames))
	out = append(out, c.EmailAddresses...)
	out = append(out, c.DNSNames...)
	if c.Subject.CommonName != "" {
		out = append(out, c.Subject.CommonName)
	}
	return out
}
