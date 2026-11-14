package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type tokenRec struct {
	id         string
	user       string
	client     string
	scopes     []string
	access     string
	refresh    string
	accessExp  time.Time
	refreshExp time.Time
	revoked    bool
}

type authCode struct {
	code      string
	user      string
	client    string
	redirect  string
	challenge string
	scopes    []string
	exp       time.Time
}

type authAttempt struct {
	When   time.Time
	Proto  string
	Mech   string
	Who    string
	Result string
}

type armedInject struct {
	command string
	conn    int
	times   int
	hits    int
	spent   bool
	user    string
	mailbox string
	raw     string
	flags   []string
	date    *time.Time
	seq     *int
	uidv    *uint32
}

type mailRef struct {
	kind    string
	url     string
	mailbox string
	conn    int
}

type oauthClient struct {
	id        string
	secret    string
	redirects []string
}

type tokenBook struct {
	mu         sync.Mutex
	next       int
	recs       []*tokenRec
	byID       map[string]*tokenRec
	byAccess   map[string]*tokenRec
	byRefresh  map[string]*tokenRec
	attempts   []authAttempt
	clients    map[string]*oauthClient
	codes      map[string]*authCode
	rotate     bool
	accessFor  time.Duration
	refreshFor time.Duration
	codeFor    time.Duration
}

func newTokenBook() *tokenBook {
	b := &tokenBook{
		byID:       map[string]*tokenRec{},
		byAccess:   map[string]*tokenRec{},
		byRefresh:  map[string]*tokenRec{},
		clients:    map[string]*oauthClient{},
		codes:      map[string]*authCode{},
		rotate:     true,
		accessFor:  time.Hour,
		refreshFor: 24 * time.Hour,
		codeFor:    time.Minute,
	}
	b.clients["test-client"] = &oauthClient{
		id:        "test-client",
		redirects: []string{"com.example.app:/oauth2redirect"},
	}
	return b
}

func (b *tokenBook) addClient(id, secret string, redirects []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clients[id] = &oauthClient{id: id, secret: secret, redirects: append([]string(nil), redirects...)}
}

func (b *tokenBook) client(id string) *oauthClient {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.clients[id]
}

func (c *oauthClient) allows(redir string) bool {
	if c == nil {
		return false
	}
	for _, r := range c.redirects {
		if r == redir {
			return true
		}
	}
	return false
}

func randHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func (s *Server) clock() time.Time {
	if t := s.frozenTime(); t != nil {
		return *t
	}
	return time.Now()
}

func (s *Server) logAttempt(proto, mech, who, result string) {
	if s.tok == nil {
		return
	}
	now := s.clock()
	s.tok.mu.Lock()
	s.tok.attempts = append(s.tok.attempts, authAttempt{
		When: now, Proto: proto, Mech: mech, Who: who, Result: result,
	})
	s.tok.mu.Unlock()
}

func scopeList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		for _, p := range strings.Fields(s) {
			p = strings.ToLower(p)
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func scopeOK(scopes []string, proto string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, s := range scopes {
		if s == proto {
			return true
		}
	}
	return false
}

func knownScope(scopes []string) bool {
	for _, s := range scopes {
		switch s {
		case "imap", "smtp", "jmap":
		default:
			return false
		}
	}
	return true
}

// takeDo returns a live fault with this do and marks it the way takeHit does,
// except these names are not reply faults. conn 0 matches every connection.
func (s *Server) takeDo(do string, conn int) *fault {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.faults {
		if f.spent || f.do != do {
			continue
		}
		if f.conn != 0 && conn != 0 && f.conn != conn {
			continue
		}
		return takeHit(f)
	}
	return nil
}

// takeNextN fails the next count exchanges. count 0 means one.
func (s *Server) takeNextN(do string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.faults {
		if f.spent || f.do != do {
			continue
		}
		n := f.count
		if n <= 0 {
			n = 1
		}
		n--
		f.count = n
		if n <= 0 {
			f.spent = true
		}
		return true
	}
	return false
}

func (s *Server) tokenSleep() {
	s.mu.Lock()
	ms := 0
	for _, f := range s.faults {
		if f.spent || f.do != "slow_token" {
			continue
		}
		ms = f.delayMs
		f.spent = true
		break
	}
	s.mu.Unlock()
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

type bearerResult struct {
	user   string
	scopes []string
	fail   string // invalid, expired, scope, or empty
}

func (s *Server) checkBearer(token, proto string, conn int) bearerResult {
	if s.tok == nil || token == "" {
		return bearerResult{fail: "invalid"}
	}
	s.tok.mu.Lock()
	rec := s.tok.byAccess[token]
	s.tok.mu.Unlock()
	if rec == nil || rec.revoked || !s.store.hasAccount(rec.user) {
		return bearerResult{fail: "invalid"}
	}
	forcedExp := s.takeDo("expired_token", conn) != nil
	if forcedExp || !s.clock().Before(rec.accessExp) {
		return bearerResult{user: rec.user, fail: "expired"}
	}
	if s.takeDo("wrong_scope", conn) != nil || !scopeOK(rec.scopes, proto) {
		return bearerResult{user: rec.user, scopes: rec.scopes, fail: "scope"}
	}
	return bearerResult{user: rec.user, scopes: rec.scopes}
}

func (s *Server) issueToken(user, client string, scopes []string, accessFor, refreshFor time.Duration) (map[string]any, error) {
	if !s.store.hasAccount(user) {
		return nil, fmt.Errorf("no such account")
	}
	scopes = scopeList(scopes)
	if !knownScope(scopes) {
		return nil, fmt.Errorf("invalid_scope")
	}
	if accessFor <= 0 {
		accessFor = s.tok.accessFor
	}
	if refreshFor <= 0 {
		refreshFor = s.tok.refreshFor
	}
	now := s.clock()
	rec := &tokenRec{
		user: user, client: client, scopes: scopes,
		access: randHex(24), refresh: randHex(24),
		accessExp: now.Add(accessFor), refreshExp: now.Add(refreshFor),
	}
	s.tok.mu.Lock()
	s.tok.next++
	rec.id = fmt.Sprintf("t%d", s.tok.next)
	s.tok.recs = append(s.tok.recs, rec)
	s.tok.byID[rec.id] = rec
	s.tok.byAccess[rec.access] = rec
	s.tok.byRefresh[rec.refresh] = rec
	s.tok.mu.Unlock()
	return map[string]any{
		"id":            rec.id,
		"access_token":  rec.access,
		"refresh_token": rec.refresh,
		"token_type":    "Bearer",
		"expires_in":    int(accessFor.Seconds()),
		"scope":         strings.Join(scopes, " "),
	}, nil
}

func (s *Server) listTokens() []map[string]any {
	now := s.clock()
	s.tok.mu.Lock()
	defer s.tok.mu.Unlock()
	out := make([]map[string]any, 0, len(s.tok.recs))
	for _, rec := range s.tok.recs {
		out = append(out, map[string]any{
			"id":        rec.id,
			"username":  rec.user,
			"client_id": rec.client,
			"scopes":    append([]string(nil), rec.scopes...),
			"expires":   rec.accessExp.Format(time.RFC3339),
			"revoked":   rec.revoked,
			"expired":   !now.Before(rec.accessExp),
		})
	}
	return out
}

func (s *Server) revokeToken(id string) error {
	s.tok.mu.Lock()
	defer s.tok.mu.Unlock()
	rec := s.tok.byID[id]
	if rec == nil {
		return fmt.Errorf("no such token")
	}
	rec.revoked = true
	return nil
}

func (s *Server) authLog() []map[string]any {
	s.tok.mu.Lock()
	defer s.tok.mu.Unlock()
	out := make([]map[string]any, 0, len(s.tok.attempts))
	for _, a := range s.tok.attempts {
		out = append(out, map[string]any{
			"time":      a.When.Format(time.RFC3339Nano),
			"proto":     a.Proto,
			"mechanism": a.Mech,
			"who":       a.Who,
			"result":    a.Result,
		})
	}
	return out
}

func parseOAuthBearer(b []byte) (user, token string, ok bool) {
	parts := strings.Split(string(b), "\x01")
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "n,") {
		return "", "", false
	}
	head := strings.TrimPrefix(parts[0], "n,")
	if !strings.HasPrefix(head, "a=") {
		return "", "", false
	}
	user = strings.TrimSuffix(strings.TrimPrefix(head, "a="), ",")
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, "auth=Bearer ") {
			token = strings.TrimPrefix(p, "auth=Bearer ")
		}
	}
	if token == "" {
		return "", "", false
	}
	return user, token, true
}

func parseXOAuth2(b []byte) (user, token string, ok bool) {
	parts := strings.Split(string(b), "\x01")
	for _, p := range parts {
		if strings.HasPrefix(p, "user=") {
			user = strings.TrimPrefix(p, "user=")
		}
		if rest, ok := strings.CutPrefix(p, "auth=Bearer "); ok {
			token = rest
		}
	}
	return user, token, token != "" && user != ""
}

func (s *Server) bearerContinue(status string, scopes []string) string {
	obj := map[string]string{"status": status}
	if len(scopes) > 0 {
		obj["scope"] = strings.Join(scopes, " ")
	}
	if iss := s.oauthIssuer(); iss != "" {
		obj["openid-configuration"] = iss + "/.well-known/openid-configuration"
	}
	b, _ := json.Marshal(obj)
	return base64.StdEncoding.EncodeToString(b)
}

func (s *session) authBearer(tag, mech string, hasIR bool, ir string) error {
	raw, err := s.readInitial(hasIR, ir)
	if err != nil {
		return s.authErr(tag, err)
	}
	var user, token string
	var ok bool
	if strings.EqualFold(mech, "OAUTHBEARER") {
		user, token, ok = parseOAuthBearer(raw)
	} else {
		user, token, ok = parseXOAuth2(raw)
	}
	fail := "invalid"
	var scopes []string
	bound := ""
	if ok {
		res := s.srv.checkBearer(token, "imap", s.id)
		bound = res.user
		scopes = res.scopes
		fail = res.fail
		if fail == "" {
			if user != "" && user != bound {
				fail = "invalid"
			} else {
				s.user = bound
				s.srv.logAttempt("imap", mech, bound, "ok")
				return s.authDone(tag, "AUTHENTICATE")
			}
		}
	}
	who := user
	if who == "" {
		who = bound
	}
	s.srv.logAttempt("imap", mech, who, fail)
	st := "invalid_token"
	if fail == "scope" {
		st = "insufficient_scope"
	}
	if _, err := s.saslRead(s.srv.bearerContinue(st, scopes)); err != nil {
		return s.authErr(tag, err)
	}
	return s.no(tag, "[AUTHENTICATIONFAILED] AUTHENTICATE failed")
}

// readInitial uses a SASL-IR initial response, including "=" for empty.
// Without SASL-IR it always sends "+" and reads one line.
func (s *session) readInitial(hasIR bool, ir string) ([]byte, error) {
	if !s.advertiseCap("SASL-IR") {
		hasIR = false
	}
	b64 := ir
	if !hasIR {
		var err error
		b64, err = s.saslRead("")
		if err != nil {
			return nil, err
		}
	}
	return decodeSASL(b64)
}

func (s *Server) oauthIssuer() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bound.OAuth != "" {
		return "http://" + s.bound.OAuth
	}
	if s.bound.OAuthTLS != "" {
		return "https://" + s.bound.OAuthTLS
	}
	return ""
}

func (s *Server) oauthMetadata() map[string]any {
	iss := s.oauthIssuer()
	return map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/authorize",
		"token_endpoint":                        iss + "/token",
		"revocation_endpoint":                   iss + "/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post"},
	}
}

func (s *Server) serveOAuth(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server", "/.well-known/openid-configuration":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(s.oauthMetadata())
	case "/authorize":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.oauthAuthorize(w, r)
	case "/token":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.oauthToken(w, r)
	case "/revoke":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.oauthRevoke(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redir := q.Get("redirect_uri")
	clientID := q.Get("client_id")
	cl := s.tok.client(clientID)
	if cl == nil || !cl.allows(redir) {
		s.logAttempt("authorize", "code", clientID, "invalid_request")
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}
	sendErr := func(code string) {
		s.logAttempt("authorize", "code", clientID, code)
		u, _ := url.Parse(redir)
		v := u.Query()
		v.Set("error", code)
		if st := q.Get("state"); st != "" {
			v.Set("state", st)
		}
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	}
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" {
		sendErr("invalid_request")
		return
	}
	hint := q.Get("login_hint")
	user := q.Get("username")
	if hint == "" && user == "" {
		sendErr("invalid_request")
		return
	}
	if hint != "" && user != "" && hint != user {
		sendErr("invalid_request")
		return
	}
	if user == "" {
		user = hint
	}
	if hint == "" {
		hint = user
	}
	if !s.store.hasAccount(user) {
		sendErr("invalid_request")
		return
	}
	scopes := scopeList([]string{q.Get("scope")})
	if q.Get("scope") != "" && !knownScope(scopes) {
		sendErr("invalid_scope")
		return
	}
	code := randHex(16)
	s.tok.mu.Lock()
	s.tok.codes[code] = &authCode{
		code: code, user: user, client: clientID, redirect: redir,
		challenge: q.Get("code_challenge"), scopes: scopes,
		exp: s.clock().Add(s.tok.codeFor),
	}
	s.tok.mu.Unlock()
	s.logAttempt("authorize", "code", user, "ok")
	u, _ := url.Parse(redir)
	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func pkceOK(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	s.tokenSleep()
	if err := r.ParseForm(); err != nil {
		s.oauthErr(w, "invalid_request", http.StatusBadRequest, "", "token")
		return
	}
	if s.takeNextN("invalid_grant") {
		s.oauthErr(w, "invalid_grant", http.StatusBadRequest, r.Form.Get("client_id"), "token")
		return
	}
	grant := r.Form.Get("grant_type")
	clientID := r.Form.Get("client_id")
	cl := s.tok.client(clientID)
	if cl == nil || !clientAuthOK(cl, r.Form.Get("client_secret")) {
		s.oauthErr(w, "invalid_client", http.StatusUnauthorized, clientID, "token")
		return
	}
	switch grant {
	case "authorization_code":
		s.oauthCodeGrant(w, r, cl)
	case "refresh_token":
		s.oauthRefreshGrant(w, r, cl)
	default:
		s.oauthErr(w, "unsupported_grant_type", http.StatusBadRequest, clientID, "token")
	}
}

func clientAuthOK(cl *oauthClient, secret string) bool {
	if cl.secret == "" {
		return secret == ""
	}
	return secret == cl.secret
}

func (s *Server) oauthErr(w http.ResponseWriter, code string, status int, who, mech string) {
	s.logAttempt("token", mech, who, code)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func (s *Server) oauthCodeGrant(w http.ResponseWriter, r *http.Request, cl *oauthClient) {
	code := r.Form.Get("code")
	s.tok.mu.Lock()
	ac := s.tok.codes[code]
	if ac != nil {
		delete(s.tok.codes, code)
	}
	s.tok.mu.Unlock()
	if ac == nil || ac.client != cl.id || ac.redirect != r.Form.Get("redirect_uri") || !s.clock().Before(ac.exp) || !pkceOK(r.Form.Get("code_verifier"), ac.challenge) {
		s.oauthErr(w, "invalid_grant", http.StatusBadRequest, cl.id, "authorization_code")
		return
	}
	body, err := s.issueToken(ac.user, cl.id, ac.scopes, s.tok.accessFor, s.tok.refreshFor)
	if err != nil {
		s.oauthErr(w, "invalid_grant", http.StatusBadRequest, ac.user, "authorization_code")
		return
	}
	s.logAttempt("token", "authorization_code", ac.user, "ok")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) oauthRefreshGrant(w http.ResponseWriter, r *http.Request, cl *oauthClient) {
	if s.takeDo("revoked_refresh", 0) != nil {
		s.oauthErr(w, "invalid_grant", http.StatusBadRequest, cl.id, "refresh_token")
		return
	}
	presented := r.Form.Get("refresh_token")
	s.tok.mu.Lock()
	rec := s.tok.byRefresh[presented]
	rotate := s.tok.rotate
	s.tok.mu.Unlock()
	if rec == nil || rec.revoked || !s.clock().Before(rec.refreshExp) || (rec.client != "" && rec.client != cl.id) {
		s.oauthErr(w, "invalid_grant", http.StatusBadRequest, cl.id, "refresh_token")
		return
	}
	accessFor := s.tok.accessFor
	now := s.clock()
	s.tok.mu.Lock()
	delete(s.tok.byAccess, rec.access)
	rec.access = randHex(24)
	rec.accessExp = now.Add(accessFor)
	s.tok.byAccess[rec.access] = rec
	var refresh string
	if rotate {
		delete(s.tok.byRefresh, rec.refresh)
		rec.refresh = randHex(24)
		rec.refreshExp = now.Add(s.tok.refreshFor)
		s.tok.byRefresh[rec.refresh] = rec
		refresh = rec.refresh
	}
	body := map[string]any{
		"access_token": rec.access,
		"token_type":   "Bearer",
		"expires_in":   int(accessFor.Seconds()),
		"scope":        strings.Join(rec.scopes, " "),
	}
	if refresh != "" {
		body["refresh_token"] = refresh
	}
	s.tok.mu.Unlock()
	s.logAttempt("token", "refresh_token", rec.user, "ok")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.logAttempt("revoke", "revoke", "", "invalid_request")
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}
	tok := r.Form.Get("token")
	clientID := r.Form.Get("client_id")
	cl := s.tok.client(clientID)
	if clientID != "" && (cl == nil || !clientAuthOK(cl, r.Form.Get("client_secret"))) {
		s.logAttempt("revoke", "revoke", clientID, "invalid_client")
		http.Error(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	s.tok.mu.Lock()
	rec := s.tok.byAccess[tok]
	if rec == nil {
		rec = s.tok.byRefresh[tok]
	}
	if rec != nil {
		rec.revoked = true
	}
	s.tok.mu.Unlock()
	who := clientID
	if rec != nil {
		who = rec.user
	}
	s.logAttempt("revoke", "revoke", who, "ok")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) acceptOAuth(ln net.Listener, tlsOn bool) {
	var serve net.Listener = ln
	if tlsOn {
		serve = tlsListener(ln, s)
	}
	hs := &http.Server{Handler: http.HandlerFunc(s.serveOAuth), ReadHeaderTimeout: 30 * time.Second}
	_ = hs.Serve(serve)
}

func tlsListener(ln net.Listener, s *Server) net.Listener {
	return &tlsWrap{ln: ln, srv: s}
}

type tlsWrap struct {
	ln  net.Listener
	srv *Server
}

func (w *tlsWrap) Accept() (net.Conn, error) {
	c, err := w.ln.Accept()
	if err != nil {
		return nil, err
	}
	return w.srv.tlsServerFor(c, 0, "TLS"), nil
}

func (w *tlsWrap) Close() error   { return w.ln.Close() }
func (w *tlsWrap) Addr() net.Addr { return w.ln.Addr() }

// keep io import used if a handler reads a body later
var _ = io.EOF
