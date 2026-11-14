package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	jmapCore       = "urn:ietf:params:jmap:core"
	jmapMail       = "urn:ietf:params:jmap:mail"
	jmapSubmission = "urn:ietf:params:jmap:submission"
	jmapAPIPath    = "/jmap/"
	jmapWellKnown  = "/.well-known/jmap"
)

// jmapSessionURL is the absolute session resource. The TLS listener wins
// when both are bound. A host of 0.0.0.0 or :: is 127.0.0.1. An IPv6 host
// is written in brackets. The port is the one that was bound.
func (s *Server) jmapSessionURL() (string, bool) {
	addr := s.bound.JmapTLS
	scheme := "https"
	if addr == "" {
		addr = s.bound.Jmap
		scheme = "http"
	}
	if addr == "" {
		return "", false
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", false
	}
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	} else if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		host = "[" + host + "]"
	}
	return scheme + "://" + host + ":" + port + jmapWellKnown, true
}

func (s *Server) acceptJMAP(ln net.Listener) {
	hs := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	hs.SetKeepAlivesEnabled(false)
	_ = hs.Serve(ln)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if s.stalwartSessionGate() && (path == jmapWellKnown || path == "/jmap/session") {
		s.serveStalwartSession(w, r, path)
		return
	}
	user, ok := s.jmapUser(w, r)
	if !ok {
		return
	}
	switch {
	case path == jmapWellKnown:
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return
		}
		s.writeJMAPSession(w, user)
	case path == "/jmap" || path == jmapAPIPath:
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.jmapAPI(w, r, user)
	case strings.HasPrefix(path, "/jmap/upload/"):
		s.jmapUpload(w, r, user)
	case strings.HasPrefix(path, "/jmap/download/"):
		s.jmapDownload(w, r, user)
	case path == "/jmap/eventsource" || path == "/jmap/eventsource/":
		s.jmapEventSource(w, r, user)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) watchJMAPDrop(id int) chan struct{} {
	ch := make(chan struct{}, 1)
	s.jmapDropMu.Lock()
	if s.jmapDrops == nil {
		s.jmapDrops = map[int]chan struct{}{}
	}
	s.jmapDrops[id] = ch
	s.jmapDropMu.Unlock()
	return ch
}

func (s *Server) unwatchJMAPDrop(id int) {
	s.jmapDropMu.Lock()
	delete(s.jmapDrops, id)
	s.jmapDropMu.Unlock()
}

func (s *Server) wakeJMAPDrops() {
	s.jmapDropMu.Lock()
	defer s.jmapDropMu.Unlock()
	for _, ch := range s.jmapDrops {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func jmapUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="jmap"`)
	w.WriteHeader(http.StatusUnauthorized)
}

func (s *Server) jmapEventSource(w http.ResponseWriter, r *http.Request, user string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := int(s.jmapSeq.Add(1))
	if s.takeJMAPFault(id, "eventsource", "unauthorized") != nil {
		jmapUnauthorized(w)
		return
	}
	if s.takeJMAPFault(id, "eventsource", "server_fail") != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	dropCh := s.watchJMAPDrop(id)
	defer s.unwatchJMAPDrop(id)
	closeAfterFirst := s.takeJMAPFault(id, "eventsource", "drop_eventsource") != nil
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	wantEmail := true
	if _, set := q["types"]; set {
		wantEmail = false
		for _, part := range strings.Split(q.Get("types"), ",") {
			if strings.TrimSpace(part) == "Email" {
				wantEmail = true
				break
			}
		}
	}
	closeAfterState := q.Get("closeafter") == "state"
	var ping time.Duration
	if raw := q.Get("ping"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			ping = time.Duration(n) * time.Second
		}
	}
	since, watch := s.store.watchJMAPState()
	defer s.store.unwatchJMAPState(watch)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	var ticks <-chan time.Time
	if ping > 0 {
		ticker := time.NewTicker(ping)
		defer ticker.Stop()
		ticks = ticker.C
	}
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-dropCh:
			if s.takeJMAPFault(id, "eventsource", "drop_eventsource") != nil {
				return
			}
		case <-ticks:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-watch:
			now := s.store.jmapStateText()
			if now == since {
				continue
			}
			since = now
			if !wantEmail {
				continue
			}
			payload, err := json.Marshal(map[string]any{
				"changed": map[string]any{
					user: map[string]any{"Email": now},
				},
			})
			if err != nil {
				return
			}
			if _, err := io.WriteString(w, "event: state\ndata: "+string(payload)+"\n\n"); err != nil {
				return
			}
			flusher.Flush()
			if closeAfterFirst || closeAfterState {
				return
			}
		}
	}
}

func personalityBase(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	return strings.TrimSuffix(path, ".json")
}

func (s *Server) stalwartSessionGate() bool {
	switch personalityBase(s.cfg.Personality) {
	case "stalwart", "stalwart-0155", "stalwart-01625", "stalwart-dev", "stalwart-oauth":
		return true
	}
	return false
}

func (s *Server) serveStalwartSession(w http.ResponseWriter, r *http.Request, path string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if path == jmapWellKnown {
		w.Header().Set("Location", "/jmap/session")
		w.WriteHeader(http.StatusTemporaryRedirect)
		return
	}
	if h := strings.TrimSpace(r.Header.Get("Authorization")); h != "" {
		user, ok := s.jmapUser(w, r)
		if !ok {
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return
		}
		s.writeJMAPSession(w, user)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	s.writeJMAPSessionAnon(w)
}

func (s *Server) jmapUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(h, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		res := s.checkBearer(token, "jmap", 0)
		if res.fail != "" || res.user == "" {
			w.Header().Add("WWW-Authenticate", `Bearer realm="jmap"`)
			w.Header().Add("WWW-Authenticate", `Basic realm="jmap"`)
			w.WriteHeader(http.StatusUnauthorized)
			return "", false
		}
		return res.user, true
	}
	user, pass, ok := r.BasicAuth()
	if !ok || user == "" || !s.store.check(user, pass) {
		w.Header().Set("WWW-Authenticate", `Basic realm="jmap"`)
		w.WriteHeader(http.StatusUnauthorized)
		return "", false
	}
	return user, true
}

func (s *Server) jmapSessionDoc(user string, anon bool) map[string]any {
	capObj := func() map[string]any { return map[string]any{} }
	caps := map[string]any{
		jmapCore:       capObj(),
		jmapMail:       capObj(),
		jmapSubmission: capObj(),
	}
	accounts := map[string]any{}
	primary := map[string]any{}
	username := ""
	if !anon {
		username = user
		accounts[user] = map[string]any{
			"name":       user,
			"isPersonal": true,
			"isReadOnly": false,
			"accountCapabilities": map[string]any{
				jmapCore:       capObj(),
				jmapMail:       capObj(),
				jmapSubmission: capObj(),
			},
		}
		primary = map[string]any{
			jmapCore:       user,
			jmapMail:       user,
			jmapSubmission: user,
		}
	}
	return map[string]any{
		"capabilities":    caps,
		"accounts":        accounts,
		"primaryAccounts": primary,
		"username":        username,
		"apiUrl":          jmapAPIPath,
		"downloadUrl":     "/jmap/download/{accountId}/{blobId}/{name}?accept={type}",
		"uploadUrl":       "/jmap/upload/{accountId}/",
		"eventSourceUrl":  "/jmap/eventsource/?types={types}&closeafter={closeafter}&ping={ping}",
		"state":           s.store.jmapStateText(),
	}
}

func (s *Server) writeJMAPSession(w http.ResponseWriter, user string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.jmapSessionDoc(user, false))
}

func (s *Server) writeJMAPSessionAnon(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.jmapSessionDoc("", true))
}

func jmapProblem(w http.ResponseWriter, status int, typ string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": typ, "status": status})
}

type jmapInvoke struct {
	name string
	args map[string]json.RawMessage
	id   string
}

type jmapDone struct {
	name string
	args map[string]any
	ok   bool
}

func (s *Server) jmapAPI(w http.ResponseWriter, r *http.Request, user string) {
	connID := int(s.jmapSeq.Add(1))
	if s.takeJMAPFault(connID, "api", "unauthorized") != nil {
		jmapUnauthorized(w)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		jmapProblem(w, http.StatusBadRequest, "urn:ietf:params:jmap:error:notJSON")
		return
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		jmapProblem(w, http.StatusBadRequest, "urn:ietf:params:jmap:error:notJSON")
		return
	}
	var using []string
	if json.Unmarshal(req["using"], &using) != nil || len(using) == 0 {
		jmapProblem(w, http.StatusBadRequest, "urn:ietf:params:jmap:error:notRequest")
		return
	}
	subOK := false
	for _, u := range using {
		switch u {
		case jmapCore, jmapMail:
		case jmapSubmission:
			subOK = true
		default:
			jmapProblem(w, http.StatusBadRequest, "urn:ietf:params:jmap:error:unknownCapability")
			return
		}
	}
	var rawCalls []json.RawMessage
	if json.Unmarshal(req["methodCalls"], &rawCalls) != nil || len(rawCalls) > 64 {
		jmapProblem(w, http.StatusBadRequest, "urn:ietf:params:jmap:error:notRequest")
		return
	}
	var calls []jmapInvoke
	for _, raw := range rawCalls {
		var parts []json.RawMessage
		if json.Unmarshal(raw, &parts) != nil || len(parts) != 3 {
			jmapProblem(w, http.StatusBadRequest, "urn:ietf:params:jmap:error:notRequest")
			return
		}
		var name, id string
		if json.Unmarshal(parts[0], &name) != nil || name == "" || json.Unmarshal(parts[2], &id) != nil {
			jmapProblem(w, http.StatusBadRequest, "urn:ietf:params:jmap:error:notRequest")
			return
		}
		var args map[string]json.RawMessage
		if json.Unmarshal(parts[1], &args) != nil {
			jmapProblem(w, http.StatusBadRequest, "urn:ietf:params:jmap:error:notRequest")
			return
		}
		if args == nil {
			args = map[string]json.RawMessage{}
		}
		calls = append(calls, jmapInvoke{name: name, args: args, id: id})
	}
	for _, call := range calls {
		if s.takeJMAPFault(connID, call.name, "unauthorized") != nil {
			jmapUnauthorized(w)
			return
		}
	}
	prev := map[string]jmapDone{}
	var responses []any
	allFail := s.takeJMAPFault(connID, "api", "server_fail") != nil
	for _, call := range calls {
		if allFail || s.takeJMAPFault(connID, call.name, "server_fail") != nil {
			responses = append(responses, []any{"error", map[string]any{"type": "serverFail"}, call.id})
			prev[call.id] = jmapDone{name: call.name}
			continue
		}
		args, refErr := resolveJMAPArgs(call.args, prev)
		if refErr != "" {
			responses = append(responses, []any{"error", map[string]any{"type": refErr}, call.id})
			prev[call.id] = jmapDone{name: call.name}
			continue
		}
		var extra []any
		result, typ := s.jmapCall(user, call.name, args, call.id, subOK, &extra, connID)
		if typ != "" {
			responses = append(responses, []any{"error", map[string]any{"type": typ}, call.id})
			prev[call.id] = jmapDone{name: call.name}
			continue
		}
		responses = append(responses, []any{call.name, result, call.id})
		prev[call.id] = jmapDone{name: call.name, args: result, ok: true}
		responses = append(responses, extra...)
	}
	if responses == nil {
		responses = []any{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"methodResponses": responses,
		"sessionState":    s.store.jmapStateText(),
	})
}

func resolveJMAPArgs(raw map[string]json.RawMessage, prev map[string]jmapDone) (map[string]any, string) {
	out := map[string]any{}
	var refs []string
	for k, v := range raw {
		if strings.HasPrefix(k, "#") {
			refs = append(refs, k)
			continue
		}
		val, err := decodeJMAP(v)
		if err != "" {
			return nil, err
		}
		out[k] = val
	}
	for _, k := range refs {
		var ref struct {
			ResultOf string `json:"resultOf"`
			Name     string `json:"name"`
			Path     string `json:"path"`
		}
		if json.Unmarshal(raw[k], &ref) != nil || ref.ResultOf == "" || ref.Name == "" || !strings.HasPrefix(ref.Path, "/") && ref.Path != "" {
			return nil, "invalidResultReference"
		}
		done, ok := prev[ref.ResultOf]
		if !ok || !done.ok || done.name != ref.Name {
			return nil, "invalidResultReference"
		}
		val, ok := jsonPointer(done.args, ref.Path)
		if !ok {
			return nil, "invalidResultReference"
		}
		out[strings.TrimPrefix(k, "#")] = val
	}
	return out, ""
}

func decodeJMAP(raw json.RawMessage) (any, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ""
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, "invalidArguments"
	}
	return v, ""
}

func jsonPointer(doc any, path string) (any, bool) {
	if path == "" || path == "/" {
		return doc, true
	}
	if !strings.HasPrefix(path, "/") {
		return nil, false
	}
	cur := doc
	for _, raw := range strings.Split(path[1:], "/") {
		tok := strings.ReplaceAll(raw, "~1", "/")
		tok = strings.ReplaceAll(tok, "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[tok]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		case []string:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func (s *Server) jmapCall(user, name string, args map[string]any, callID string, subOK bool, extra *[]any, connID int) (map[string]any, string) {
	if (name == "Email/import" || name == "Email/set") && s.matchProtoFault("jmap", connID, name) != nil {
		return nil, "overQuota"
	}
	switch name {
	case "Mailbox/get":
		return s.jmapMailboxGet(user, args)
	case "Mailbox/query":
		return s.jmapMailboxQuery(user, args)
	case "Email/query":
		return s.jmapEmailQuery(user, args)
	case "Email/get":
		return s.jmapEmailGet(user, args)
	case "Email/set":
		return s.jmapEmailSet(user, args)
	case "Email/import":
		return s.jmapEmailImport(user, args)
	case "Email/changes":
		return s.jmapEmailChanges(user, args)
	case "Identity/get":
		if !subOK {
			return nil, "unknownMethod"
		}
		return s.jmapIdentityGet(user, args)
	case "EmailSubmission/set":
		if !subOK {
			return nil, "unknownMethod"
		}
		return s.jmapSubmissionSet(user, args, callID, extra)
	case "EmailSubmission/query":
		if !subOK {
			return nil, "unknownMethod"
		}
		return s.jmapSubmissionQuery(user, args)
	default:
		return nil, "unknownMethod"
	}
}

const jmapUploadMax = 32 << 20

func (s *Server) jmapUpload(w http.ResponseWriter, r *http.Request, user string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	reqID := int(s.jmapSeq.Add(1))
	if s.takeJMAPFault(reqID, "upload", "unauthorized") != nil {
		jmapUnauthorized(w)
		return
	}
	if s.takeJMAPFault(reqID, "upload", "server_fail") != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	account, ok := jmapOneSegment(r.URL.Path, "/jmap/upload/")
	if !ok || account != user {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, jmapUploadMax+1))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(body) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(body) > jmapUploadMax {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	typ := jmapMediaType(r.Header.Get("Content-Type"))
	id := s.store.putBlob(user, typ, body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"accountId": user,
		"blobId":    id,
		"type":      typ,
		"size":      len(body),
	})
}

func (s *Server) jmapDownload(w http.ResponseWriter, r *http.Request, user string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := int(s.jmapSeq.Add(1))
	if s.takeJMAPFault(id, "download", "unauthorized") != nil {
		jmapUnauthorized(w)
		return
	}
	if s.takeJMAPFault(id, "download", "server_fail") != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	account, blobID, ok := jmapDownloadPath(r.URL.Path)
	if !ok || account != user {
		http.NotFound(w, r)
		return
	}
	data, typ, ok := s.store.uploadBlob(user, blobID)
	if !ok {
		emailID, part, split := splitMailBlob(blobID)
		if !split {
			http.NotFound(w, r)
			return
		}
		raw, found := s.store.messageRFC822(emailID)
		if !found {
			http.NotFound(w, r)
			return
		}
		if part == "rfc822" {
			data, typ, ok = raw, "message/rfc822", true
		} else {
			data, typ, ok = partBytes(raw, part)
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func jmapOneSegment(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	return rest, true
}

func jmapDownloadPath(path string) (string, string, bool) {
	const prefix = "/jmap/download/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(path, prefix), "/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func jmapMediaType(v string) string {
	if strings.TrimSpace(v) == "" {
		return "application/octet-stream"
	}
	typ, _, err := mime.ParseMediaType(v)
	if err != nil || typ == "" || !strings.Contains(typ, "/") {
		return "application/octet-stream"
	}
	return typ
}

func splitMailBlob(id string) (string, string, bool) {
	emailID, part, ok := strings.Cut(id, "-")
	if !ok || part == "" || !strings.HasPrefix(emailID, "em") || emailID == "em" {
		return "", "", false
	}
	for _, c := range emailID[2:] {
		if c < '0' || c > '9' {
			return "", "", false
		}
	}
	return emailID, part, true
}

func partBytes(raw []byte, want string) ([]byte, string, bool) {
	root := parseRoot(raw)
	seq := 0
	var found []byte
	var typ string
	var ok bool
	var walk func(*node)
	walk = func(n *node) {
		if n == nil || ok {
			return
		}
		if n.multipart {
			for _, c := range n.children {
				walk(c)
			}
			return
		}
		seq++
		if strconv.Itoa(seq) != want {
			return
		}
		b, err := decodePartBytes(n)
		if err != nil {
			b = n.body
		}
		if b == nil {
			b = []byte{}
		}
		found = b
		typ = strings.ToLower(n.typ) + "/" + strings.ToLower(n.sub)
		ok = true
	}
	walk(root)
	if !ok {
		return nil, "", false
	}
	return found, typ, true
}

func (s *Server) jmapEmailSet(user string, args map[string]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	if args["create"] != nil {
		return nil, "invalidArguments"
	}
	hasDestroy := args["destroy"] != nil
	hasUpdate := args["update"] != nil
	var ids []string
	if hasDestroy {
		var typ string
		ids, typ = stringList(args["destroy"])
		if typ != "" {
			return nil, typ
		}
		for _, id := range ids {
			if id == "" {
				return nil, "invalidArguments"
			}
		}
	}
	var patches []emailPatch
	if hasUpdate {
		var typ string
		patches, typ = parseEmailUpdate(args["update"])
		if typ != "" {
			return nil, typ
		}
	}
	old, neu, updated, notUpdated, destroyed, missing := s.store.applyEmailSet(patches, ids, s.frozenTime())
	res := map[string]any{
		"accountId": user,
		"oldState":  old,
		"newState":  neu,
	}
	if hasUpdate {
		up := map[string]any{}
		for _, id := range updated {
			up[id] = nil
		}
		nu := map[string]any{}
		for id, typ := range notUpdated {
			nu[id] = map[string]any{"type": typ}
		}
		res["updated"] = up
		res["notUpdated"] = nu
	}
	if hasDestroy || !hasUpdate {
		res["destroyed"] = destroyed
		res["notFound"] = missing
		if len(missing) > 0 {
			nd := map[string]any{}
			for _, id := range missing {
				nd[id] = map[string]any{"type": "notFound"}
			}
			res["notDestroyed"] = nd
		}
	}
	return res, ""
}

// emailPatch is one Email/set update. ok is false when a key or value is not
// a keyword or mailbox patch this server accepts.
type emailPatch struct {
	id       string
	ok       bool
	keywords map[string]bool
	add      []string
	del      []string
}

func parseEmailUpdate(v any) ([]emailPatch, string) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, "invalidArguments"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []emailPatch
	for _, id := range keys {
		p := emailPatch{id: id, ok: true, keywords: map[string]bool{}}
		row, ok := m[id].(map[string]any)
		if !ok || id == "" {
			p.ok = false
			out = append(out, p)
			continue
		}
		for k, raw := range row {
			on, good := patchBool(raw)
			if !good {
				p.ok = false
				break
			}
			switch {
			case strings.HasPrefix(k, "keywords/"):
				flag, ok := jmapPatchKeyword(strings.TrimPrefix(k, "keywords/"))
				if !ok {
					p.ok = false
				} else {
					p.keywords[flag] = on
				}
			case strings.HasPrefix(k, "mailboxIds/"):
				box := strings.TrimPrefix(k, "mailboxIds/")
				if box == "" {
					p.ok = false
				} else if on {
					p.add = append(p.add, box)
				} else {
					p.del = append(p.del, box)
				}
			default:
				p.ok = false
			}
			if !p.ok {
				break
			}
		}
		out = append(out, p)
	}
	return out, ""
}

func patchBool(v any) (bool, bool) {
	if v == nil {
		return false, true
	}
	b, ok := v.(bool)
	if !ok || !b {
		return false, false
	}
	return true, true
}

func jmapPatchKeyword(name string) (string, bool) {
	switch name {
	case "$seen":
		return `\Seen`, true
	case "$flagged":
		return `\Flagged`, true
	case "$answered":
		return `\Answered`, true
	case "$draft":
		return `\Draft`, true
	default:
		return "", false
	}
}

func (s *Server) jmapEmailImport(user string, args map[string]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	items, typ := parseJMAPImport(args["emails"])
	if typ != "" {
		return nil, typ
	}
	old, neu, created, typ := s.store.importJMAP(user, items, s.frozenTime())
	if typ != "" {
		return nil, typ
	}
	return map[string]any{
		"accountId": user,
		"oldState":  old,
		"newState":  neu,
		"created":   created,
	}, ""
}

func parseJMAPImport(v any) ([]jmapImport, string) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, "invalidArguments"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []jmapImport
	for _, cid := range keys {
		if cid == "" {
			return nil, "invalidArguments"
		}
		row, ok := m[cid].(map[string]any)
		if !ok {
			return nil, "invalidArguments"
		}
		blobID, _ := row["blobId"].(string)
		if blobID == "" {
			return nil, "invalidArguments"
		}
		box, typ := oneMailbox(row["mailboxIds"])
		if typ != "" {
			return nil, typ
		}
		flags, typ := jmapKeywordFlags(row["keywords"])
		if typ != "" {
			return nil, typ
		}
		when, typ := jmapReceivedAt(row["receivedAt"])
		if typ != "" {
			return nil, typ
		}
		out = append(out, jmapImport{cid: cid, blobID: blobID, mailbox: box, flags: flags, when: when})
	}
	return out, ""
}

func oneMailbox(v any) (string, string) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", "invalidArguments"
	}
	var id string
	n := 0
	for k, raw := range m {
		on, ok := raw.(bool)
		if !ok || k == "" {
			return "", "invalidArguments"
		}
		if !on {
			continue
		}
		n++
		id = k
	}
	if n != 1 {
		return "", "invalidArguments"
	}
	return id, ""
}

func jmapKeywordFlags(v any) ([]string, string) {
	if v == nil {
		return nil, ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, "invalidArguments"
	}
	var flags []string
	for k, raw := range m {
		on, ok := raw.(bool)
		if !ok {
			return nil, "invalidArguments"
		}
		if !on {
			continue
		}
		switch k {
		case "$seen":
			flags = append(flags, `\Seen`)
		case "$flagged":
			flags = append(flags, `\Flagged`)
		case "$answered":
			flags = append(flags, `\Answered`)
		case "$draft":
			flags = append(flags, `\Draft`)
		default:
			if _, ok := canonFlag(k); !ok {
				return nil, "invalidArguments"
			}
			flags = append(flags, k)
		}
	}
	return flags, ""
}

func jmapReceivedAt(v any) (*time.Time, string) {
	if v == nil {
		return nil, ""
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil, "invalidArguments"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, "invalidArguments"
	}
	return &t, ""
}

func (s *Server) jmapEmailChanges(user string, args map[string]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	raw, ok := args["sinceState"].(string)
	if !ok || raw == "" {
		return nil, "invalidArguments"
	}
	since, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil, "cannotCalculateChanges"
	}
	max, hasMax, typ := optNonNeg(args, "maxChanges")
	if typ != "" {
		return nil, typ
	}
	if hasMax && max < 1 {
		return nil, "invalidArguments"
	}
	created, updated, destroyed, neu, more, known := s.store.emailChanges(since, max, hasMax)
	if !known {
		return nil, "cannotCalculateChanges"
	}
	return map[string]any{
		"accountId":      user,
		"oldState":       raw,
		"newState":       neu,
		"hasMoreChanges": more,
		"created":        created,
		"updated":        updated,
		"destroyed":      destroyed,
	}, ""
}

func jmapAccount(user string, args map[string]any) string {
	v, ok := args["accountId"]
	if !ok {
		return "invalidArguments"
	}
	id, ok := v.(string)
	if !ok || id == "" {
		return "invalidArguments"
	}
	if id != user {
		return "accountNotFound"
	}
	return ""
}

type mbSnap struct {
	id         string
	name       string
	parent     string
	parentNull bool
	role       string
	total      int
	unread     int
	sortOrder  int
}

func (st *Store) jmapMailboxSnaps(user string) (string, []mbSnap) {
	st.mu.Lock()
	defer st.mu.Unlock()
	sl := st.slotUser(user)
	out := make([]mbSnap, 0, len(sl.order))
	for i, name := range sl.order {
		mb := sl.boxes[name]
		if mb == nil {
			continue
		}
		snap := mbSnap{
			id:         mb.jmapID,
			name:       leafMailbox(name, st.delim),
			parentNull: true,
			total:      len(mb.msgs),
			unread:     mb.unseenCount(),
			sortOrder:  i,
		}
		if pn, ok := parentMailboxName(name, st.delim); ok {
			if p := sl.boxes[pn]; p != nil {
				snap.parent = p.jmapID
				snap.parentNull = false
			}
		}
		if role, ok := mailboxRole(mb); ok {
			snap.role = role
		}
		out = append(out, snap)
	}
	return strconv.FormatUint(st.jmapState, 10), out
}

func parentMailboxName(name, delim string) (string, bool) {
	if strings.EqualFold(name, "INBOX") {
		return "", false
	}
	i := strings.LastIndex(name, delim)
	if i <= 0 {
		return "", false
	}
	return name[:i], true
}

func leafMailbox(name, delim string) string {
	if strings.EqualFold(name, "INBOX") {
		return "INBOX"
	}
	i := strings.LastIndex(name, delim)
	if i < 0 {
		return name
	}
	return name[i+len(delim):]
}

func mailboxRole(mb *Mailbox) (string, bool) {
	if strings.EqualFold(mb.name, "INBOX") {
		return "inbox", true
	}
	for _, flag := range specialUseOrder {
		for _, have := range mb.special {
			if have == flag {
				return jmapRoleName(flag), true
			}
		}
	}
	return "", false
}

func jmapRoleName(flag string) string {
	switch flag {
	case `\All`:
		return "all"
	case `\Archive`:
		return "archive"
	case `\Drafts`:
		return "drafts"
	case `\Flagged`:
		return "flagged"
	case `\Junk`:
		return "junk"
	case `\Sent`:
		return "sent"
	case `\Trash`:
		return "trash"
	default:
		return ""
	}
}

func (s *Server) jmapMailboxGet(user string, args map[string]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	props, typ := propSet(args["properties"])
	if typ != "" {
		return nil, typ
	}
	state, snaps := s.store.jmapMailboxSnaps(user)
	var want []string
	all := true
	if raw, ok := args["ids"]; ok && raw != nil {
		all = false
		want, typ = stringList(raw)
		if typ != "" {
			return nil, typ
		}
	}
	byID := map[string]mbSnap{}
	for _, snap := range snaps {
		byID[snap.id] = snap
	}
	var list []any
	var notFound []string
	emit := func(snap mbSnap) {
		list = append(list, mailboxJSON(snap, props))
	}
	if all {
		for _, snap := range snaps {
			emit(snap)
		}
	} else {
		seen := map[string]bool{}
		for _, id := range want {
			if seen[id] {
				continue
			}
			seen[id] = true
			snap, ok := byID[id]
			if !ok {
				notFound = append(notFound, id)
				continue
			}
			emit(snap)
		}
	}
	if list == nil {
		list = []any{}
	}
	if notFound == nil {
		notFound = []string{}
	}
	res := map[string]any{
		"accountId": user,
		"state":     state,
		"list":      list,
	}
	if !all {
		res["notFound"] = notFound
	}
	return res, ""
}

func mailboxJSON(snap mbSnap, props map[string]bool) map[string]any {
	obj := map[string]any{"id": snap.id}
	put := func(k string, v any) {
		if props == nil || props[k] {
			obj[k] = v
		}
	}
	put("name", snap.name)
	if snap.parentNull {
		put("parentId", nil)
	} else {
		put("parentId", snap.parent)
	}
	if snap.role == "" {
		put("role", nil)
	} else {
		put("role", snap.role)
	}
	put("sortOrder", snap.sortOrder)
	put("totalEmails", snap.total)
	put("unreadEmails", snap.unread)
	return obj
}

type mbSort struct {
	prop string
	asc  bool
}

func (s *Server) jmapMailboxQuery(user string, args map[string]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	match, typ := compileMBFilter(args["filter"])
	if typ != "" {
		return nil, typ
	}
	sorts, typ := parseMBSort(args["sort"])
	if typ != "" {
		return nil, typ
	}
	pos, hasPos, typ := optNonNeg(args, "position")
	if typ != "" {
		return nil, typ
	}
	if !hasPos {
		pos = 0
	}
	limit, hasLimit, typ := optNonNeg(args, "limit")
	if typ != "" {
		return nil, typ
	}
	calc, typ := optBool(args, "calculateTotal")
	if typ != "" {
		return nil, typ
	}
	state, snaps := s.store.jmapMailboxSnaps(user)
	var rows []mbSnap
	for _, snap := range snaps {
		if match(snap) {
			rows = append(rows, snap)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return lessMailbox(rows[i], rows[j], sorts)
	})
	total := len(rows)
	ids := pageIDs(mbIDs(rows), pos, limit, hasLimit)
	res := map[string]any{
		"accountId":           user,
		"queryState":          state,
		"canCalculateChanges": false,
		"position":            pos,
		"ids":                 ids,
	}
	if calc {
		res["total"] = total
	}
	return res, ""
}

func mbIDs(rows []mbSnap) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.id
	}
	return out
}

func lessMailbox(a, b mbSnap, sorts []mbSort) bool {
	for _, s := range sorts {
		var c int
		switch s.prop {
		case "sortOrder":
			c = a.sortOrder - b.sortOrder
		case "name":
			c = strings.Compare(a.name, b.name)
		}
		if c != 0 {
			if s.asc {
				return c < 0
			}
			return c > 0
		}
	}
	return a.id < b.id
}

func compileMBFilter(v any) (func(mbSnap) bool, string) {
	if v == nil {
		return func(mbSnap) bool { return true }, ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, "invalidArguments"
	}
	if _, ok := m["operator"]; ok {
		op, _ := m["operator"].(string)
		conds, ok := m["conditions"].([]any)
		if !ok || (op != "AND" && op != "OR" && op != "NOT") {
			return nil, "invalidArguments"
		}
		for k := range m {
			if k != "operator" && k != "conditions" {
				return nil, "invalidArguments"
			}
		}
		if op == "NOT" {
			if len(conds) != 1 {
				return nil, "invalidArguments"
			}
			fn, typ := compileMBFilter(conds[0])
			if typ != "" {
				return nil, typ
			}
			return func(s mbSnap) bool { return !fn(s) }, ""
		}
		var subs []func(mbSnap) bool
		for _, c := range conds {
			fn, typ := compileMBFilter(c)
			if typ != "" {
				return nil, typ
			}
			subs = append(subs, fn)
		}
		if op == "AND" {
			return func(s mbSnap) bool {
				for _, fn := range subs {
					if !fn(s) {
						return false
					}
				}
				return true
			}, ""
		}
		return func(s mbSnap) bool {
			for _, fn := range subs {
				if fn(s) {
					return true
				}
			}
			return false
		}, ""
	}
	if len(m) != 1 {
		return nil, "invalidArguments"
	}
	raw, ok := m["parentId"]
	if !ok {
		return nil, "invalidArguments"
	}
	switch p := raw.(type) {
	case nil:
		return func(s mbSnap) bool { return s.parentNull }, ""
	case string:
		if p == "" {
			return nil, "invalidArguments"
		}
		return func(s mbSnap) bool { return !s.parentNull && s.parent == p }, ""
	default:
		return nil, "invalidArguments"
	}
}

func parseMBSort(v any) ([]mbSort, string) {
	if v == nil {
		return []mbSort{{prop: "sortOrder", asc: true}, {prop: "name", asc: true}}, ""
	}
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil, "invalidArguments"
	}
	var out []mbSort
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, "invalidArguments"
		}
		prop, _ := m["property"].(string)
		if prop != "sortOrder" && prop != "name" {
			return nil, "invalidArguments"
		}
		asc := true
		if raw, ok := m["isAscending"]; ok {
			b, ok := raw.(bool)
			if !ok {
				return nil, "invalidArguments"
			}
			asc = b
		}
		for k := range m {
			if k != "property" && k != "isAscending" {
				return nil, "invalidArguments"
			}
		}
		out = append(out, mbSort{prop: prop, asc: asc})
	}
	return out, ""
}

type emHead struct {
	id        string
	mailboxID string
	received  time.Time
	uid       uint32
}

type emSnap struct {
	emHead
	mailboxes map[string]bool
	keywords  map[string]bool
	size      int
	sent      time.Time
	subject   string
	from      string
	to        string
	raw       []byte
}

func (st *Store) jmapEmailHeads(user string) (string, []emHead, map[string]bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []emHead
	boxes := map[string]bool{}
	sl := st.slotUser(user)
	for _, name := range sl.order {
		mb := sl.boxes[name]
		if mb == nil {
			continue
		}
		boxes[mb.jmapID] = true
		for _, m := range mb.msgs {
			out = append(out, emHead{
				id:        m.jmapID,
				mailboxID: mb.jmapID,
				received:  m.internal,
				uid:       m.uid,
			})
		}
	}
	return strconv.FormatUint(st.jmapState, 10), out, boxes
}

func (st *Store) jmapEmailSnaps(user string, ids []string, all bool) (string, []emSnap, []string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	var order []emSnap
	index := map[string]int{}
	sl := st.slotUser(user)
	for _, name := range sl.order {
		mb := sl.boxes[name]
		if mb == nil {
			continue
		}
		for _, m := range mb.msgs {
			if i, ok := index[m.jmapID]; ok {
				order[i].mailboxes[mb.jmapID] = true
				for k, on := range jmapKeywords(m) {
					if on {
						order[i].keywords[k] = true
					}
				}
				continue
			}
			snap := emSnap{
				emHead: emHead{
					id:        m.jmapID,
					mailboxID: mb.jmapID,
					received:  m.internal,
					uid:       m.uid,
				},
				mailboxes: map[string]bool{mb.jmapID: true},
				keywords:  jmapKeywords(m),
				size:      m.size,
				sent:      m.sent,
				subject:   m.subject,
				from:      m.from,
				to:        m.to,
				raw:       append([]byte(nil), m.bytes()...),
			}
			if snap.sent.IsZero() {
				snap.sent = m.internal
			}
			index[m.jmapID] = len(order)
			order = append(order, snap)
		}
	}
	if all {
		return strconv.FormatUint(st.jmapState, 10), order, nil
	}
	byID := map[string]emSnap{}
	for _, snap := range order {
		byID[snap.id] = snap
	}
	var found []emSnap
	var missing []string
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		snap, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		found = append(found, snap)
	}
	return strconv.FormatUint(st.jmapState, 10), found, missing
}

func jmapKeywords(m *Message) map[string]bool {
	kw := map[string]bool{}
	if m.has(`\Seen`) {
		kw["$seen"] = true
	}
	if m.has(`\Flagged`) {
		kw["$flagged"] = true
	}
	if m.has(`\Answered`) {
		kw["$answered"] = true
	}
	if m.has(`\Draft`) {
		kw["$draft"] = true
	}
	for _, k := range m.keywords {
		kw[k] = true
	}
	return kw
}

func (s *Server) jmapEmailQuery(user string, args map[string]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	boxID, typ := parseInMailbox(args["filter"])
	if typ != "" {
		return nil, typ
	}
	asc := true
	if raw, ok := args["sort"]; ok && raw != nil {
		asc, typ = parseReceivedSort(raw)
		if typ != "" {
			return nil, typ
		}
	}
	pos, _, typ := optNonNeg(args, "position")
	if typ != "" {
		return nil, typ
	}
	limit, hasLimit, typ := optNonNeg(args, "limit")
	if typ != "" {
		return nil, typ
	}
	calc, typ := optBool(args, "calculateTotal")
	if typ != "" {
		return nil, typ
	}
	if _, ok := args["anchor"]; ok {
		return nil, "invalidArguments"
	}
	state, heads, boxes := s.store.jmapEmailHeads(user)
	if !boxes[boxID] {
		return nil, "invalidArguments"
	}
	var rows []emHead
	for _, h := range heads {
		if h.mailboxID == boxID {
			rows = append(rows, h)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].received.Equal(rows[j].received) {
			if asc {
				return rows[i].received.Before(rows[j].received)
			}
			return rows[j].received.Before(rows[i].received)
		}
		if asc {
			return rows[i].uid < rows[j].uid
		}
		return rows[i].uid > rows[j].uid
	})
	total := len(rows)
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.id
	}
	res := map[string]any{
		"accountId":           user,
		"queryState":          state,
		"canCalculateChanges": false,
		"position":            pos,
		"ids":                 pageIDs(ids, pos, limit, hasLimit),
	}
	if calc {
		res["total"] = total
	}
	return res, ""
}

func parseInMailbox(v any) (string, string) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return "", "invalidArguments"
	}
	raw, ok := m["inMailbox"]
	if !ok {
		return "", "invalidArguments"
	}
	id, ok := raw.(string)
	if !ok || id == "" {
		return "", "invalidArguments"
	}
	return id, ""
}

func parseReceivedSort(v any) (bool, string) {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return false, "invalidArguments"
	}
	asc := true
	seen := false
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return false, "invalidArguments"
		}
		prop, _ := m["property"].(string)
		if prop != "receivedAt" {
			return false, "invalidArguments"
		}
		dir := true
		if raw, ok := m["isAscending"]; ok {
			b, ok := raw.(bool)
			if !ok {
				return false, "invalidArguments"
			}
			dir = b
		}
		for k := range m {
			if k != "property" && k != "isAscending" {
				return false, "invalidArguments"
			}
		}
		if seen && dir != asc {
			return false, "invalidArguments"
		}
		asc = dir
		seen = true
	}
	return asc, ""
}

func pageIDs(ids []string, pos, limit int, hasLimit bool) []string {
	if pos > len(ids) {
		return []string{}
	}
	end := len(ids)
	if hasLimit {
		end = pos + limit
		if end > len(ids) {
			end = len(ids)
		}
	}
	out := append([]string{}, ids[pos:end]...)
	if out == nil {
		return []string{}
	}
	return out
}

func (s *Server) jmapEmailGet(user string, args map[string]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	props, typ := propSet(args["properties"])
	if typ != "" {
		return nil, typ
	}
	bodyProps, typ := propSet(args["bodyProperties"])
	if typ != "" {
		return nil, typ
	}
	fetchAll, typ := optBool(args, "fetchAllBodyValues")
	if typ != "" {
		return nil, typ
	}
	fetchText, typ := optBool(args, "fetchTextBodyValues")
	if typ != "" {
		return nil, typ
	}
	fetchHTML, typ := optBool(args, "fetchHTMLBodyValues")
	if typ != "" {
		return nil, typ
	}
	maxBytes, hasMax, typ := optNonNeg(args, "maxBodyValueBytes")
	if typ != "" {
		return nil, typ
	}
	rawIDs, present := args["ids"]
	if !present {
		return nil, "invalidArguments"
	}
	all := rawIDs == nil
	var ids []string
	if !all {
		ids, typ = stringList(rawIDs)
		if typ != "" {
			return nil, typ
		}
	}
	state, snaps, missing := s.store.jmapEmailSnaps(user, ids, all)
	var list []any
	for _, snap := range snaps {
		list = append(list, emailJSON(snap, props, bodyProps, fetchAll, fetchText, fetchHTML, maxBytes, hasMax))
	}
	if list == nil {
		list = []any{}
	}
	res := map[string]any{
		"accountId": user,
		"state":     state,
		"list":      list,
	}
	if !all {
		if missing == nil {
			missing = []string{}
		}
		res["notFound"] = missing
	}
	return res, ""
}

func emailJSON(snap emSnap, props, bodyProps map[string]bool, fetchAll, fetchText, fetchHTML bool, maxBytes int, hasMax bool) map[string]any {
	obj := map[string]any{"id": snap.id}
	put := func(k string, v any) {
		if props == nil || props[k] {
			obj[k] = v
		}
	}
	put("blobId", snap.id+"-rfc822")
	put("threadId", snap.id)
	boxes := snap.mailboxes
	if len(boxes) == 0 && snap.mailboxID != "" {
		boxes = map[string]bool{snap.mailboxID: true}
	}
	put("mailboxIds", boxes)
	if len(snap.keywords) == 0 {
		put("keywords", map[string]bool{})
	} else {
		put("keywords", snap.keywords)
	}
	put("size", snap.size)
	put("receivedAt", snap.received.UTC().Format(time.RFC3339))
	put("sentAt", snap.sent.UTC().Format(time.RFC3339))
	put("subject", snap.subject)
	put("from", jmapAddresses(snap.from))
	put("to", jmapAddresses(snap.to))
	root := parseRoot(snap.raw)
	put("preview", jmapPreview(root))
	put("hasAttachment", partAttached(root))
	var texts []partText
	seq := 0
	put("bodyStructure", buildBody(root, snap.id, bodyProps, &seq, &texts))
	headers := jmapHeaders(snap.raw)
	if headers == nil {
		headers = []map[string]string{}
	}
	put("headers", headers)
	wantValues := props == nil || props["bodyValues"]
	if wantValues && (fetchAll || fetchText || fetchHTML) {
		values := map[string]any{}
		for _, part := range texts {
			if !wantBodyValue(part.typ, fetchAll, fetchText, fetchHTML) {
				continue
			}
			val, cut := part.value, false
			if hasMax {
				val, cut = trimUTF8Bytes(part.value, maxBytes)
			}
			values[part.id] = map[string]any{
				"value":             val,
				"isEncodingProblem": part.problem,
				"isTruncated":       cut,
			}
		}
		obj["bodyValues"] = values
	} else if props != nil && props["bodyValues"] {
		obj["bodyValues"] = map[string]any{}
	}
	return obj
}

func wantBodyValue(typ string, fetchAll, fetchText, fetchHTML bool) bool {
	if fetchAll && strings.HasPrefix(typ, "text/") {
		return true
	}
	if fetchText && typ == "text/plain" {
		return true
	}
	if fetchHTML && typ == "text/html" {
		return true
	}
	return false
}

func jmapAddresses(s string) []any {
	s = strings.TrimSpace(s)
	if s == "" {
		return []any{}
	}
	list, err := mail.ParseAddressList(s)
	if err != nil {
		return []any{map[string]any{"name": s, "email": ""}}
	}
	out := make([]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{"name": a.Name, "email": a.Address})
	}
	return out
}

func jmapPreview(n *node) string {
	return snippetText(n)
}

func partAttached(n *node) bool {
	if n == nil {
		return false
	}
	if n.multipart {
		for _, c := range n.children {
			if partAttached(c) {
				return true
			}
		}
		return false
	}
	kind, _ := partDisp(n)
	if kind == "attachment" {
		return true
	}
	return !strings.EqualFold(n.typ, "TEXT")
}

type partText struct {
	id      string
	value   string
	problem bool
	typ     string
}

func buildBody(n *node, emailID string, props map[string]bool, seq *int, texts *[]partText) map[string]any {
	if n == nil {
		n = &node{typ: "TEXT", sub: "PLAIN", enc: "7BIT", params: map[string]string{}}
	}
	obj := map[string]any{}
	put := func(k string, v any) {
		if props == nil || props[k] {
			obj[k] = v
		}
	}
	multi := n.multipart
	var partID string
	if !multi {
		*seq++
		partID = strconv.Itoa(*seq)
		put("partId", partID)
		put("blobId", emailID+"-"+partID)
	} else {
		put("partId", nil)
		put("blobId", nil)
	}
	typ := strings.ToLower(n.typ) + "/" + strings.ToLower(n.sub)
	put("type", typ)
	if cs := strings.ToLower(n.params["charset"]); cs != "" {
		put("charset", cs)
	} else {
		put("charset", nil)
	}
	kind, filename := partDisp(n)
	if kind == "" {
		put("disposition", nil)
	} else {
		put("disposition", kind)
	}
	if filename == "" {
		put("name", nil)
	} else {
		put("name", filename)
	}
	raw, err := decodePartBytes(n)
	if err != nil {
		raw = n.body
	}
	put("size", len(raw))
	if multi {
		subs := make([]any, 0, len(n.children))
		for _, c := range n.children {
			subs = append(subs, buildBody(c, emailID, props, seq, texts))
		}
		put("subParts", subs)
		return obj
	}
	put("subParts", nil)
	if strings.EqualFold(n.typ, "TEXT") {
		val, problem := decodePartText(n)
		*texts = append(*texts, partText{id: partID, value: val, problem: problem, typ: typ})
	}
	return obj
}

func partDisp(n *node) (string, string) {
	filename := n.params["name"]
	if n.mimeHead == nil {
		return "", filename
	}
	h := headerValue(n.mimeHead, "Content-Disposition")
	if h == "" {
		return "", filename
	}
	kind, params, err := mime.ParseMediaType(h)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(strings.Trim(h, "\""))), filename
	}
	if params["filename"] != "" {
		filename = params["filename"]
	}
	return strings.ToLower(kind), filename
}

func headerValue(raw []byte, name string) string {
	for _, h := range jmapHeaders(raw) {
		if strings.EqualFold(h["name"], name) {
			return h["value"]
		}
	}
	return ""
}

func jmapHeaders(raw []byte) []map[string]string {
	head, _ := splitHB(raw)
	s := string(head)
	s = strings.TrimSuffix(s, "\r\n\r\n")
	s = strings.TrimSuffix(s, "\n\n")
	s = strings.TrimSuffix(s, "\r\n")
	if strings.TrimSpace(s) == "" {
		return []map[string]string{}
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []map[string]string
	var name, val string
	flush := func() {
		if name == "" {
			return
		}
		out = append(out, map[string]string{"name": name, "value": val})
		name, val = "", ""
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if name != "" {
				val += " " + strings.TrimSpace(line)
			}
			continue
		}
		flush()
		n, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(n)
		val = strings.TrimSpace(v)
	}
	flush()
	if out == nil {
		return []map[string]string{}
	}
	return out
}

func decodePartBytes(n *node) ([]byte, error) {
	if n.multipart {
		if n.body == nil {
			return []byte{}, nil
		}
		return n.body, nil
	}
	enc := strings.ToLower(strings.TrimSpace(n.enc))
	switch enc {
	case "", "7bit", "8bit", "binary":
		if n.body == nil {
			return []byte{}, nil
		}
		return n.body, nil
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(n.body)))
	case "base64":
		raw := bytes.Map(func(r rune) rune {
			switch r {
			case ' ', '\t', '\r', '\n':
				return -1
			}
			return r
		}, n.body)
		b, err := base64.StdEncoding.DecodeString(string(raw))
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(string(raw))
		}
		return b, err
	default:
		return nil, io.ErrUnexpectedEOF
	}
}

func decodePartText(n *node) (string, bool) {
	b, err := decodePartBytes(n)
	if err != nil {
		return string(n.body), true
	}
	cs := strings.ToLower(n.params["charset"])
	switch cs {
	case "", "us-ascii", "utf-8", "utf8":
		if (cs == "utf-8" || cs == "utf8") && !utf8.Valid(b) {
			return string(b), true
		}
		return string(b), false
	case "iso-8859-1", "latin1", "iso8859-1", "windows-1252", "cp1252":
		return latin1String(b), false
	default:
		if utf8.Valid(b) {
			return string(b), false
		}
		return string(b), true
	}
}

func latin1String(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func trimUTF8Bytes(s string, n int) (string, bool) {
	b := []byte(s)
	if n >= len(b) {
		return s, false
	}
	if n <= 0 {
		return "", len(b) > 0
	}
	b = b[:n]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b), true
}

func propSet(v any) (map[string]bool, string) {
	if v == nil {
		return nil, ""
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, "invalidArguments"
	}
	out := map[string]bool{}
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, "invalidArguments"
		}
		out[s] = true
	}
	return out, ""
}

func stringList(v any) ([]string, string) {
	switch a := v.(type) {
	case []string:
		return append([]string{}, a...), ""
	case []any:
		out := make([]string, len(a))
		for i, item := range a {
			s, ok := item.(string)
			if !ok {
				return nil, "invalidArguments"
			}
			out[i] = s
		}
		return out, ""
	default:
		return nil, "invalidArguments"
	}
}

func optBool(args map[string]any, key string) (bool, string) {
	v, ok := args[key]
	if !ok || v == nil {
		return false, ""
	}
	b, ok := v.(bool)
	if !ok {
		return false, "invalidArguments"
	}
	return b, ""
}

func optNonNeg(args map[string]any, key string) (int, bool, string) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false, ""
	}
	n, ok := asInt(v)
	if !ok || n < 0 {
		return 0, false, "invalidArguments"
	}
	return n, true, ""
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	case float64:
		if n < 0 || n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

// jmapSub is one EmailSubmission remembered for EmailSubmission/query.
type jmapSub struct {
	id      string
	emailID string
}

func jmapIdentityID(user string) string {
	return "idn-" + user
}

func (s *Server) submissionState(user string) string {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	return strconv.Itoa(len(s.subs[user]))
}

func (s *Server) addSubmission(user, emailID string) string {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if s.subs == nil {
		s.subs = map[string][]jmapSub{}
	}
	s.subN++
	id := "sm" + strconv.FormatUint(s.subN, 10)
	s.subs[user] = append(s.subs[user], jmapSub{id: id, emailID: emailID})
	return id
}

func (s *Server) submissionEmail(user, id string) (string, bool) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, sub := range s.subs[user] {
		if sub.id == id {
			return sub.emailID, true
		}
	}
	return "", false
}

func (s *Server) submissionIDs(user string, emailIDs map[string]bool) (string, []string) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	var ids []string
	for _, sub := range s.subs[user] {
		if emailIDs != nil && !emailIDs[sub.emailID] {
			continue
		}
		ids = append(ids, sub.id)
	}
	return strconv.Itoa(len(s.subs[user])), ids
}

func (s *Server) jmapIdentityGet(user string, args map[string]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	props, typ := propSet(args["properties"])
	if typ != "" {
		return nil, typ
	}
	id := jmapIdentityID(user)
	one := identityJSON(id, user, props)
	var list []any
	var notFound []string
	raw, has := args["ids"]
	if !has || raw == nil {
		list = []any{one}
		notFound = []string{}
	} else {
		ids, typ := stringList(raw)
		if typ != "" {
			return nil, typ
		}
		seen := map[string]bool{}
		for _, want := range ids {
			if seen[want] {
				continue
			}
			seen[want] = true
			if want == id {
				list = append(list, one)
			} else {
				notFound = append(notFound, want)
			}
		}
		if list == nil {
			list = []any{}
		}
		if notFound == nil {
			notFound = []string{}
		}
	}
	return map[string]any{
		"accountId": user,
		"state":     "1",
		"list":      list,
		"notFound":  notFound,
	}, ""
}

func identityJSON(id, email string, props map[string]bool) map[string]any {
	obj := map[string]any{"id": id}
	if props == nil || props["email"] {
		obj["email"] = email
	}
	return obj
}

func (s *Server) jmapSubmissionSet(user string, args map[string]any, callID string, extra *[]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	if args["update"] != nil || args["destroy"] != nil {
		return nil, "invalidArguments"
	}
	var creates map[string]any
	if raw, ok := args["create"]; ok && raw != nil {
		var good bool
		creates, good = raw.(map[string]any)
		if !good {
			return nil, "invalidArguments"
		}
	}
	refs, typ := submissionDestroyRefs(args["onSuccessDestroyEmail"])
	if typ != "" {
		return nil, typ
	}
	keys := make([]string, 0, len(creates))
	for k := range creates {
		if k == "" {
			return nil, "invalidArguments"
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	old := s.submissionState(user)
	created := map[string]any{}
	notCreated := map[string]any{}
	okEmail := map[string]string{}
	ident := jmapIdentityID(user)
	for _, cid := range keys {
		emailID, identityID, from, rcpts, problem := parseSubmissionCreate(creates[cid])
		if problem != "" {
			notCreated[cid] = map[string]any{"type": problem}
			continue
		}
		if identityID != "" && identityID != ident {
			notCreated[cid] = map[string]any{"type": "invalidProperties"}
			continue
		}
		if identityID == "" {
			identityID = ident
		}
		_, snaps, missing := s.store.jmapEmailSnaps(user, []string{emailID}, false)
		if len(snaps) == 0 || len(missing) > 0 {
			notCreated[cid] = map[string]any{"type": "notFound"}
			continue
		}
		env := make([]smtpRcpt, len(rcpts))
		for i, addr := range rcpts {
			env[i] = smtpRcpt{addr: addr, deliver: s.smtpDelivers(addr)}
		}
		if err := s.acceptMail(user, from, env, snaps[0].raw, smtpMeta{}); err != nil {
			notCreated[cid] = map[string]any{"type": "serverFail"}
			continue
		}
		subID := s.addSubmission(user, emailID)
		okEmail[cid] = emailID
		created[cid] = map[string]any{
			"id":         subID,
			"emailId":    emailID,
			"identityId": identityID,
			"threadId":   emailID,
			"undoStatus": "final",
		}
	}
	s.destroySubmitted(user, callID, refs, okEmail, extra)
	return map[string]any{
		"accountId":  user,
		"oldState":   old,
		"newState":   s.submissionState(user),
		"created":    created,
		"notCreated": notCreated,
	}, ""
}

// destroySubmitted destroys emails named by onSuccessDestroyEmail after the
// creates have been accepted. A "#id" is a creation id from this call.
func (s *Server) destroySubmitted(user, callID string, refs []string, okEmail map[string]string, extra *[]any) {
	if len(refs) == 0 {
		return
	}
	seen := map[string]bool{}
	var ids []string
	for _, ref := range refs {
		var emailID string
		var ok bool
		if strings.HasPrefix(ref, "#") {
			emailID, ok = okEmail[strings.TrimPrefix(ref, "#")]
		} else {
			emailID, ok = s.submissionEmail(user, ref)
		}
		if !ok || emailID == "" || seen[emailID] {
			continue
		}
		seen[emailID] = true
		ids = append(ids, emailID)
	}
	if len(ids) == 0 {
		return
	}
	old, neu, _, _, destroyed, missing := s.store.applyEmailSet(nil, ids, s.frozenTime())
	res := map[string]any{
		"accountId": user,
		"oldState":  old,
		"newState":  neu,
		"destroyed": destroyed,
		"notFound":  missing,
	}
	if len(missing) > 0 {
		nd := map[string]any{}
		for _, id := range missing {
			nd[id] = map[string]any{"type": "notFound"}
		}
		res["notDestroyed"] = nd
	}
	*extra = append(*extra, []any{"Email/set", res, callID})
}

func parseSubmissionCreate(v any) (emailID, identityID, from string, rcpts []string, problem string) {
	row, ok := v.(map[string]any)
	if !ok {
		return "", "", "", nil, "invalidProperties"
	}
	emailID, _ = row["emailId"].(string)
	if emailID == "" {
		return "", "", "", nil, "invalidProperties"
	}
	if raw, ok := row["identityId"]; ok && raw != nil {
		identityID, ok = raw.(string)
		if !ok || identityID == "" {
			return "", "", "", nil, "invalidProperties"
		}
	}
	env, ok := row["envelope"].(map[string]any)
	if !ok {
		return "", "", "", nil, "invalidProperties"
	}
	mf, ok := env["mailFrom"].(map[string]any)
	if !ok {
		return "", "", "", nil, "invalidProperties"
	}
	from, _ = mf["email"].(string)
	if from == "" {
		return "", "", "", nil, "invalidProperties"
	}
	rawRcpt, ok := env["rcptTo"].([]any)
	if !ok || len(rawRcpt) == 0 {
		return "", "", "", nil, "invalidProperties"
	}
	for _, item := range rawRcpt {
		obj, ok := item.(map[string]any)
		if !ok {
			return "", "", "", nil, "invalidProperties"
		}
		addr, _ := obj["email"].(string)
		if addr == "" {
			return "", "", "", nil, "invalidProperties"
		}
		rcpts = append(rcpts, addr)
	}
	return emailID, identityID, from, rcpts, ""
}

func submissionDestroyRefs(v any) ([]string, string) {
	if v == nil {
		return nil, ""
	}
	switch a := v.(type) {
	case string:
		if a == "" {
			return nil, "invalidArguments"
		}
		return []string{a}, ""
	case []any:
		out := make([]string, 0, len(a))
		for _, item := range a {
			s, ok := item.(string)
			if !ok || s == "" {
				return nil, "invalidArguments"
			}
			out = append(out, s)
		}
		return out, ""
	default:
		return nil, "invalidArguments"
	}
}

func (s *Server) jmapSubmissionQuery(user string, args map[string]any) (map[string]any, string) {
	if typ := jmapAccount(user, args); typ != "" {
		return nil, typ
	}
	var want map[string]bool
	if raw, ok := args["filter"]; ok && raw != nil {
		filt, ok := raw.(map[string]any)
		if !ok {
			return nil, "invalidArguments"
		}
		if rawIDs, ok := filt["emailIds"]; ok && rawIDs != nil {
			ids, typ := stringList(rawIDs)
			if typ != "" {
				return nil, typ
			}
			want = map[string]bool{}
			for _, id := range ids {
				want[id] = true
			}
		}
	}
	pos, _, typ := optNonNeg(args, "position")
	if typ != "" {
		return nil, typ
	}
	limit, hasLimit, typ := optNonNeg(args, "limit")
	if typ != "" {
		return nil, typ
	}
	state, ids := s.submissionIDs(user, want)
	return map[string]any{
		"accountId":           user,
		"queryState":          state,
		"canCalculateChanges": false,
		"position":            pos,
		"ids":                 pageIDs(ids, pos, limit, hasLimit),
	}, ""
}
