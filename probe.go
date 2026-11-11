package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// opConfig is the probe operator file. The filled file is gitignored.
// The example ships with an empty password.
type opConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	TLS         string `json:"tls"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Personality string `json:"personality"`
}

func loadOpConfig(path string) (opConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return opConfig{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var cfg opConfig
	if err := dec.Decode(&cfg); err != nil {
		return opConfig{}, fmt.Errorf("operator config %s: %w", path, err)
	}
	switch cfg.TLS {
	case "none", "starttls", "implicit":
	default:
		return opConfig{}, fmt.Errorf("operator config %s: tls must be none, starttls, or implicit", path)
	}
	if cfg.Host == "" || cfg.Port == 0 || cfg.Username == "" || cfg.Personality == "" {
		return opConfig{}, fmt.Errorf("operator config %s: host, port, username, and personality are required", path)
	}
	return cfg, nil
}

type gapsFile struct {
	Capabilities []string `json:"capabilities"`
	Commands     []string `json:"commands"`
}

// probeMessage is the one fixed message the script appends.
var probeMessage = []byte(strings.ReplaceAll(strings.TrimLeft(`
From: Probe <probe@example.test>
To: Probe <probe@example.test>
Subject: probe
Date: Thu, 01 Jan 2024 00:00:00 +0000
Message-ID: <probe-fixed@example.test>
MIME-Version: 1.0
Content-Type: text/plain; charset=us-ascii
Content-Transfer-Encoding: 7bit

probe-body
`, "\n"), "\n", "\r\n"))

type probeScript struct {
	lines []string
	mb1   string
	mb2   string
}

type imapTalk struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

func dialProbe(host string, port int, mode string) (*imapTalk, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	var conn net.Conn
	var err error
	if mode == "implicit" {
		conn, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = net.Dial("tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	return &imapTalk{conn: conn, r: bufio.NewReader(conn), w: bufio.NewWriter(conn)}, nil
}

func (c *imapTalk) close() { _ = c.conn.Close() }

func (c *imapTalk) readRaw() (string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

var litTail = regexp.MustCompile(`\{(\d+)\+?\}$`)

func (c *imapTalk) readLogical() (string, error) {
	var b strings.Builder
	for {
		line, err := c.readRaw()
		if err != nil {
			return "", err
		}
		m := litTail.FindStringSubmatch(line)
		if m == nil {
			b.WriteString(line)
			return b.String(), nil
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return "", err
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(c.r, buf); err != nil {
			return "", err
		}
		b.WriteString(line)
		b.WriteString("\r\n")
		b.Write(buf)
	}
}

func (c *imapTalk) command(tag, text string) ([]string, error) {
	if _, err := io.WriteString(c.w, tag+" "+text+"\r\n"); err != nil {
		return nil, err
	}
	if err := c.w.Flush(); err != nil {
		return nil, err
	}
	var lines []string
	for {
		line, err := c.readLogical()
		if err != nil {
			return lines, err
		}
		lines = append(lines, line)
		if strings.HasPrefix(line, tag+" ") {
			return lines, nil
		}
	}
}

func freshMailboxPair() (string, string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	id := "p" + hex.EncodeToString(b[:])
	return id, id + "b", nil
}

func quoteIMAP(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func taggedNo(line, tag string) bool {
	rest, ok := strings.CutPrefix(line, tag+" ")
	if !ok {
		return false
	}
	return strings.HasPrefix(rest, "NO ") || strings.HasPrefix(rest, "BAD ")
}

// runProbeScript runs the fixed script and then deletes the mailboxes it created.
func runProbeScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb1, mb2, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb1, mb2: mb2}
	talk, err := dialProbe(host, port, tlsMode)
	if err != nil {
		return out, err
	}
	defer talk.close()
	greet, err := talk.readLogical()
	if err != nil {
		return out, err
	}
	out.lines = append(out.lines, greet)
	if tlsMode == "starttls" {
		lines, err := talk.command("t0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "t0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		raw := talk.conn
		tlsConn := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	steps := []struct {
		tag string
		cmd string
	}{
		{"t1", "CAPABILITY"},
		{"t2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"t3", "CAPABILITY"},
		{"t4", "NAMESPACE"},
		{"t5", "CREATE " + quoteIMAP(mb1)},
		{"t6", "SELECT " + quoteIMAP(mb1)},
		{"t7", fmt.Sprintf("APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}", quoteIMAP(mb1), len(probeMessage))},
		{"t8", "FETCH 1 (FLAGS UID INTERNALDATE RFC822.SIZE ENVELOPE BODYSTRUCTURE BODY.PEEK[HEADER])"},
		{"t9", "FETCH 1 (BODY[TEXT])"},
		{"t10", `STORE 1 +FLAGS (\Flagged)`},
		{"t11", "CREATE " + quoteIMAP(mb2)},
		{"t12", "COPY 1 " + quoteIMAP(mb2)},
		{"t13", "SELECT " + quoteIMAP(mb2)},
		{"t14", "MOVE 1 " + quoteIMAP(mb1)},
		{"t15", "SEARCH ALL"},
		{"t16", "LIST " + quoteIMAP("") + " " + quoteIMAP(mb1+"*")},
		{"t17", "LOGOUT"},
	}
	for _, st := range steps {
		var lines []string
		if st.tag == "t7" {
			if _, err := io.WriteString(talk.w, st.tag+" "+st.cmd+"\r\n"); err != nil {
				return out, err
			}
			if _, err := talk.w.Write(probeMessage); err != nil {
				return out, err
			}
			if _, err := io.WriteString(talk.w, "\r\n"); err != nil {
				return out, err
			}
			if err := talk.w.Flush(); err != nil {
				return out, err
			}
			for {
				line, err := talk.readLogical()
				if err != nil {
					return out, err
				}
				lines = append(lines, line)
				if strings.HasPrefix(line, st.tag+" ") {
					break
				}
			}
		} else {
			lines, err = talk.command(st.tag, st.cmd)
			if err != nil {
				out.lines = append(out.lines, lines...)
				return out, err
			}
		}
		out.lines = append(out.lines, lines...)
		if taggedNo(lines[len(lines)-1], st.tag) && st.tag != "t17" {
			_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb1, mb2)
			return out, fmt.Errorf("%s failed", st.tag)
		}
		if st.tag == "t17" {
			break
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb1, mb2); err != nil {
		return out, err
	}
	return out, nil
}

func deleteProbeMailboxes(host string, port int, tlsMode, user, pass, mb1, mb2 string) error {
	talk, err := dialProbe(host, port, tlsMode)
	if err != nil {
		return err
	}
	defer talk.close()
	if _, err := talk.readLogical(); err != nil {
		return err
	}
	if tlsMode == "starttls" {
		if _, err := talk.command("d0", "STARTTLS"); err != nil {
			return err
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	lines, err := talk.command("d1", "LOGIN "+quoteIMAP(user)+" "+quoteIMAP(pass))
	if err != nil {
		return err
	}
	if len(lines) == 0 || taggedNo(lines[len(lines)-1], "d1") {
		return fmt.Errorf("cleanup login failed")
	}
	for i, name := range []string{mb2, mb1} {
		tag := fmt.Sprintf("d%d", i+2)
		lines, err = talk.command(tag, "DELETE "+quoteIMAP(name))
		if err != nil {
			return err
		}
		if taggedNo(lines[len(lines)-1], tag) {
			return fmt.Errorf("cleanup delete failed")
		}
	}
	_, err = talk.command("d4", "LOGOUT")
	return err
}

func writeProbeCert(dir string) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
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
		return "", "", err
	}
	certPath := dir + "/cert.pem"
	keyPath := dir + "/key.pem"
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

func startProbeServer(user, pass, personality string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "cyrusprobe-")
	if err != nil {
		return "", nil, err
	}
	stop := func() { os.RemoveAll(dir) }
	ds := dir + "/dataset"
	if err := os.Mkdir(ds, 0755); err != nil {
		stop()
		return "", nil, err
	}
	raw := fmt.Sprintf("{\"accounts\":[{\"username\":%s,\"password\":%s}],\"mailboxes\":[{\"name\":\"INBOX\"}]}\n",
		strconv.Quote(user), strconv.Quote(pass))
	if err := os.WriteFile(ds+"/dataset.json", []byte(raw), 0644); err != nil {
		stop()
		return "", nil, err
	}
	certPath, keyPath, err := writeProbeCert(dir)
	if err != nil {
		stop()
		return "", nil, err
	}
	cfg := Config{Listen: "127.0.0.1:0", Personality: personality, Dataset: ds, CertFile: certPath, KeyFile: keyPath}
	b, err := json.Marshal(cfg)
	if err != nil {
		stop()
		return "", nil, err
	}
	if err := os.WriteFile(dir+"/c.json", b, 0644); err != nil {
		stop()
		return "", nil, err
	}
	srv, err := newServer(dir+"/c.json", io.Discard)
	if err != nil {
		stop()
		return "", nil, err
	}
	go func() { _ = srv.Serve() }()
	select {
	case <-srv.ready:
	case <-time.After(30 * time.Second):
		stop()
		return "", nil, fmt.Errorf("local server not ready")
	}
	if srv.startErr != nil {
		stop()
		return "", nil, srv.startErr
	}
	return srv.bound.Listen, func() { srv.Close(); stop() }, nil
}

var (
	reHexHost  = regexp.MustCompile(`\b[0-9a-f]{12}\b`)
	reSession  = regexp.MustCompile(`SESSIONID=<[^>]*>`)
	reTiming   = regexp.MustCompile(`\(\d+(?:\.\d+)? secs?[^)]*\)`)
	reUIDVal   = regexp.MustCompile(`\[UIDVALIDITY \d+\]`)
	reUIDNext  = regexp.MustCompile(`\[UIDNEXT \d+\]`)
	reUnseen   = regexp.MustCompile(`\[UNSEEN \d+\]`)
	reAppend   = regexp.MustCompile(`\[APPENDUID [^]]+\]`)
	reCopyUID  = regexp.MustCompile(`\[COPYUID [^]]+\]`)
	reExists   = regexp.MustCompile(`^\* \d+ EXISTS$`)
	reRecent   = regexp.MustCompile(`^\* \d+ RECENT$`)
	reExpunge  = regexp.MustCompile(`^\* \d+ EXPUNGE$`)
	reSeqFetch = regexp.MustCompile(`^\* \d+ FETCH `)
	reUIDItem  = regexp.MustCompile(`(^|[ ({])UID \d+`)
	reSizeItem = regexp.MustCompile(`RFC822\.SIZE \d+`)
	reDateItem = regexp.MustCompile(`INTERNALDATE "(?:\\.|[^"])*"`)
	reSearch   = regexp.MustCompile(`^\* SEARCH\b`)
	reLit      = regexp.MustCompile(`\{(\d+)\+?\}\r\n`)
)

// keepSelectCodes are the SELECT response-code names this comparison keeps.
// Codes from capabilities this binary does not implement are dropped.
var keepSelectCodes = map[string]bool{
	"PERMANENTFLAGS":       true,
	"UNSEEN":               true,
	"UIDVALIDITY":          true,
	"UIDNEXT":              true,
	"READ-WRITE":           true,
	"READ-ONLY":            true,
	"CAPABILITY":           true,
	"APPENDUID":            true,
	"COPYUID":              true,
	"TRYCREATE":            true,
	"AUTHENTICATIONFAILED": true,
}

func filterCapTokens(tokens []string) (kept []string, missing []string) {
	seen := map[string]bool{}
	for _, c := range tokens {
		u := strings.ToUpper(c)
		if seen[u] {
			continue
		}
		seen[u] = true
		if capImplemented(c) {
			kept = append(kept, c)
			continue
		}
		missing = append(missing, c)
	}
	return kept, missing
}

func capabilityTokens(line string) []string {
	if rest, ok := strings.CutPrefix(line, "* CAPABILITY "); ok {
		return strings.Fields(rest)
	}
	const mark = "[CAPABILITY "
	i := strings.Index(line, mark)
	if i < 0 {
		return nil
	}
	rest := line[i+len(mark):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return nil
	}
	return strings.Fields(rest[:j])
}

func rewriteCapability(line string) string {
	if rest, ok := strings.CutPrefix(line, "* CAPABILITY "); ok {
		kept, _ := filterCapTokens(strings.Fields(rest))
		sort.Slice(kept, func(i, j int) bool { return strings.ToUpper(kept[i]) < strings.ToUpper(kept[j]) })
		return "* CAPABILITY " + strings.Join(kept, " ")
	}
	const mark = "[CAPABILITY "
	i := strings.Index(line, mark)
	if i < 0 {
		return line
	}
	rest := line[i+len(mark):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return line
	}
	kept, _ := filterCapTokens(strings.Fields(rest[:j]))
	sort.Slice(kept, func(i, j int) bool { return strings.ToUpper(kept[i]) < strings.ToUpper(kept[j]) })
	return line[:i] + "[CAPABILITY " + strings.Join(kept, " ") + "]" + rest[j+1:]
}

func bracketCode(line string) string {
	i := strings.IndexByte(line, '[')
	if i < 0 {
		return ""
	}
	rest := line[i+1:]
	j := strings.IndexByte(rest, ' ')
	k := strings.IndexByte(rest, ']')
	if k < 0 {
		return ""
	}
	if j < 0 || j > k {
		return rest[:k]
	}
	return rest[:j]
}

func stripTraceHeaders(raw string) string {
	if !strings.Contains(raw, ":") || !strings.Contains(raw, "\n") {
		return raw
	}
	lines := strings.Split(raw, "\r\n")
	var b strings.Builder
	skip := false
	for _, line := range lines {
		if line == "" {
			b.WriteString("\r\n")
			continue
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if !skip {
				b.WriteString(line)
				b.WriteString("\r\n")
			}
			continue
		}
		name, _, _ := strings.Cut(line, ":")
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "received", "return-path", "delivered-to":
			skip = true
			continue
		default:
			skip = false
		}
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	return b.String()
}

func rewriteLiterals(line string) string {
	var b strings.Builder
	for {
		m := reLit.FindStringSubmatchIndex(line)
		if m == nil {
			b.WriteString(line)
			return b.String()
		}
		n, _ := strconv.Atoi(line[m[2]:m[3]])
		start := m[1]
		if start+n > len(line) {
			b.WriteString(line)
			return b.String()
		}
		body := stripTraceHeaders(line[start : start+n])
		b.WriteString(line[:m[0]])
		b.WriteString(fmt.Sprintf("{%d}\r\n", len(body)))
		b.WriteString(body)
		line = line[start+n:]
	}
}

func reduceTagged(line string) string {
	fields := strings.SplitN(line, " ", 3)
	if len(fields) < 2 {
		return line
	}
	status := fields[1]
	if status != "OK" && status != "NO" && status != "BAD" {
		return line
	}
	rest := ""
	if len(fields) == 3 {
		rest = fields[2]
	}
	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end > 0 {
			code := bracketCode(" " + rest)
			if keepSelectCodes[code] {
				inner := rest[1:end]
				switch code {
				case "UIDVALIDITY", "UIDNEXT", "UNSEEN", "APPENDUID", "COPYUID":
					return "T " + status + " [" + code + "]"
				case "CAPABILITY":
					return "T " + status + " " + rest[:end+1]
				default:
					return "T " + status + " [" + inner + "]"
				}
			}
		}
	}
	return "T " + status
}

func reduceUntaggedOK(line string) (string, bool) {
	code := bracketCode(line)
	if code == "" {
		return "", false
	}
	if !keepSelectCodes[code] {
		return "", false
	}
	switch code {
	case "UIDVALIDITY", "UIDNEXT", "UNSEEN":
		return "* OK [" + code + "]", true
	case "PERMANENTFLAGS":
		i := strings.IndexByte(line, '[')
		j := strings.IndexByte(line, ']')
		if i < 0 || j < i {
			return "* OK [PERMANENTFLAGS]", true
		}
		return "* OK " + line[i:j+1], true
	default:
		return "* OK [" + code + "]", true
	}
}

// normalizeScript compares reply shape. Tags, ids the script caused,
// timestamps, and capabilities this binary does not implement are removed.
func normalizeScript(lines []string, mb1, mb2 string) []string {
	var out []string
	for i, line := range lines {
		line = strings.ReplaceAll(line, mb2, "MB2")
		line = strings.ReplaceAll(line, mb1, "MB1")
		if strings.HasPrefix(line, "t") {
			sp := strings.IndexByte(line, ' ')
			if sp > 1 && tagNum(line[:sp]) {
				line = "T" + line[sp:]
			}
		}
		line = rewriteCapability(line)
		line = reSession.ReplaceAllString(line, "")
		line = reTiming.ReplaceAllString(line, "")
		if i == 0 {
			line = reHexHost.ReplaceAllString(line, "")
			line = strings.Join(strings.Fields(line), " ")
		}
		line = reAppend.ReplaceAllString(line, "[APPENDUID]")
		line = reCopyUID.ReplaceAllString(line, "[COPYUID]")
		line = reUIDVal.ReplaceAllString(line, "[UIDVALIDITY]")
		line = reUIDNext.ReplaceAllString(line, "[UIDNEXT]")
		line = reUnseen.ReplaceAllString(line, "[UNSEEN]")
		line = rewriteLiterals(line)
		line = reDateItem.ReplaceAllString(line, `INTERNALDATE "DATE"`)
		line = reSizeItem.ReplaceAllString(line, "RFC822.SIZE SIZE")
		line = reUIDItem.ReplaceAllString(line, "${1}UID N")
		switch {
		case reExists.MatchString(line):
			line = "* EXISTS"
		case reRecent.MatchString(line):
			line = "* RECENT"
		case reExpunge.MatchString(line):
			line = "* EXPUNGE"
		case reSearch.MatchString(line):
			n := len(strings.Fields(line)) - 2
			if n < 0 {
				n = 0
			}
			line = "* SEARCH " + strconv.Itoa(n)
		case strings.HasPrefix(line, "* BYE"):
			line = "* BYE"
		case reSeqFetch.MatchString(line):
			line = reSeqFetch.ReplaceAllString(line, "* SEQ FETCH ")
		case i != 0 && strings.HasPrefix(line, "* OK ["):
			next, ok := reduceUntaggedOK(line)
			if !ok {
				continue
			}
			line = next
		case strings.HasPrefix(line, "T "):
			line = reduceTagged(line)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func tagNum(s string) bool {
	if len(s) < 2 || s[0] != 't' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func collectGaps(lines []string) gapsFile {
	seen := map[string]string{}
	for _, line := range lines {
		for _, c := range capabilityTokens(line) {
			if capImplemented(c) {
				continue
			}
			u := strings.ToUpper(c)
			if _, ok := seen[u]; !ok {
				seen[u] = c
			}
		}
	}
	caps := make([]string, 0, len(seen))
	for _, c := range seen {
		caps = append(caps, c)
	}
	sort.Slice(caps, func(i, j int) bool { return strings.ToUpper(caps[i]) < strings.ToUpper(caps[j]) })
	return gapsFile{Capabilities: caps, Commands: []string{}}
}

func scriptDiff(remote, local []string) string {
	if strings.Join(remote, "\n") == strings.Join(local, "\n") {
		return ""
	}
	n := len(remote)
	if len(local) > n {
		n = len(local)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		var r, l string
		if i < len(remote) {
			r = remote[i]
		}
		if i < len(local) {
			l = local[i]
		}
		if r != l {
			fmt.Fprintf(&b, "remote: %s\nlocal:  %s\n", r, l)
		}
	}
	return b.String()
}

// compareProbe runs the script against the operator target and a local server.
func compareProbe(cfg opConfig) (string, gapsFile, error) {
	remote, err := runProbeScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", gapsFile{}, err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", collectGaps(remote.lines), err
		}
		return "", collectGaps(remote.lines), serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", collectGaps(remote.lines), perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", collectGaps(remote.lines), perr
	}
	local, lerr := runProbeScript(host, port, "none", cfg.Username, cfg.Password)
	rn := normalizeScript(remote.lines, remote.mb1, remote.mb2)
	ln := normalizeScript(local.lines, local.mb1, local.mb2)
	diff := scriptDiff(rn, ln)
	if err != nil && diff == "" {
		return "", collectGaps(remote.lines), err
	}
	if lerr != nil && diff == "" {
		return "", collectGaps(remote.lines), lerr
	}
	return diff, collectGaps(remote.lines), nil
}
