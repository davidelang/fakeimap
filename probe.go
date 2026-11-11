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

// binaryProbeMessage is one base64 part. BINARY.PEEK[1] decodes to HelloBinary.
var binaryProbeMessage = []byte(strings.ReplaceAll(strings.TrimLeft(`
From: a@example.test
To: b@example.test
Subject: bin
Date: Mon, 01 Jan 2024 00:00:00 +0000
MIME-Version: 1.0
Content-Type: text/plain; charset=us-ascii
Content-Transfer-Encoding: base64

SGVsbG9CaW5hcnk=
`, "\n"), "\n", "\r\n"))

// previewProbeMessage is one text/plain body. FETCH PREVIEW collapses its spaces.
var previewProbeMessage = []byte(strings.ReplaceAll(strings.TrimLeft(`
From: Probe <probe@example.test>
To: Probe <probe@example.test>
Subject: preview
Date: Thu, 01 Jan 2024 00:00:00 +0000
Message-ID: <preview-fixed@example.test>
MIME-Version: 1.0
Content-Type: text/plain; charset=us-ascii
Content-Transfer-Encoding: 7bit

preview   text
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
	names := []string{mb2, mb1}
	if mb1 == mb2 {
		names = []string{mb1}
	}
	for i, name := range names {
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

var (
	reUIDValNum = regexp.MustCompile(`\[UIDVALIDITY (\d+)\]`)
	reModseqNum = regexp.MustCompile(`MODSEQ \((\d+)\)`)
)

// runCondstoreScript is the second probe. It exercises CONDSTORE and QRESYNC.
// The first script is unchanged.
func runCondstoreScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb, mb2: mb}
	a, err := dialProbe(host, port, tlsMode)
	if err != nil {
		return out, err
	}
	defer a.close()
	greet, err := a.readLogical()
	if err != nil {
		return out, err
	}
	out.lines = append(out.lines, greet)
	var uidv, modseq string
	note := func(lines []string) {
		for _, ln := range lines {
			if uidv == "" {
				if m := reUIDValNum.FindStringSubmatch(ln); m != nil {
					uidv = m[1]
				}
			}
			if modseq == "" {
				if m := reModseqNum.FindStringSubmatch(ln); m != nil {
					modseq = m[1]
				}
			}
		}
	}
	step := func(talk *imapTalk, tag, cmd string, allowBad bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		note(lines)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowBad) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	appendMsg := func(tag string) error {
		head := fmt.Sprintf("%s APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n", tag, quoteIMAP(mb), len(probeMessage))
		if _, err := io.WriteString(a.w, head); err != nil {
			return err
		}
		if _, err := a.w.Write(probeMessage); err != nil {
			return err
		}
		if _, err := io.WriteString(a.w, "\r\n"); err != nil {
			return err
		}
		if err := a.w.Flush(); err != nil {
			return err
		}
		var lines []string
		for {
			line, err := a.readLogical()
			if err != nil {
				out.lines = append(out.lines, lines...)
				return err
			}
			lines = append(lines, line)
			if strings.HasPrefix(line, tag+" ") {
				break
			}
		}
		out.lines = append(out.lines, lines...)
		note(lines)
		if taggedNo(lines[len(lines)-1], tag) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	for _, st := range []struct{ tag, cmd string }{
		{"a1", "CAPABILITY"},
		{"a2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"a3", "CAPABILITY"},
		{"a4", "CREATE " + quoteIMAP(mb)},
		{"a5", "SELECT " + quoteIMAP(mb)},
	} {
		if err := step(a, st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	if err := appendMsg("a6"); err != nil {
		return fail(err)
	}
	if err := appendMsg("a7"); err != nil {
		return fail(err)
	}
	if err := step(a, "a8", "ENABLE CONDSTORE", false); err != nil {
		return fail(err)
	}
	if err := step(a, "a9", "FETCH 1 (MODSEQ)", false); err != nil {
		return fail(err)
	}
	if modseq == "" || uidv == "" {
		return fail(fmt.Errorf("missing modseq or uidvalidity"))
	}
	if err := step(a, "a10", "FETCH 1:* (FLAGS) (CHANGEDSINCE 0)", false); err != nil {
		return fail(err)
	}
	if err := step(a, "a11", "FETCH 1 (FLAGS) (CHANGEDSINCE 9999999999)", false); err != nil {
		return fail(err)
	}
	if err := step(a, "a12", "STORE 1 (UNCHANGEDSINCE "+modseq+") +FLAGS (\\Flagged)", false); err != nil {
		return fail(err)
	}
	if err := step(a, "a13", "STORE 1 (UNCHANGEDSINCE 0) +FLAGS (\\Seen)", false); err != nil {
		return fail(err)
	}
	b, err := dialProbe(host, port, tlsMode)
	if err != nil {
		return fail(err)
	}
	defer b.close()
	greet, err = b.readLogical()
	if err != nil {
		return fail(err)
	}
	out.lines = append(out.lines, greet)
	if err := step(b, "b1", "LOGIN "+quoteIMAP(user)+" "+quoteIMAP(pass), false); err != nil {
		return fail(err)
	}
	if err := step(b, "b2", "ENABLE CONDSTORE", false); err != nil {
		return fail(err)
	}
	if err := step(b, "b3", "SELECT "+quoteIMAP(mb), false); err != nil {
		return fail(err)
	}
	if err := step(b, "b4", "SELECT "+quoteIMAP(mb)+" (QRESYNC ("+uidv+" 0 1:2))", true); err != nil {
		return fail(err)
	}
	if err := step(a, "a14", "STORE 2 +FLAGS (\\Answered)", false); err != nil {
		return fail(err)
	}
	if err := step(b, "b5", "NOOP", false); err != nil {
		return fail(err)
	}
	if err := step(a, "a15", "ENABLE QRESYNC", false); err != nil {
		return fail(err)
	}
	if err := step(a, "a16", "STORE 1 +FLAGS (\\Deleted)", false); err != nil {
		return fail(err)
	}
	if err := step(a, "a17", "EXPUNGE", false); err != nil {
		return fail(err)
	}
	if err := step(b, "b6", "NOOP", false); err != nil {
		return fail(err)
	}
	if err := step(a, "a18", "SELECT "+quoteIMAP(mb)+" (QRESYNC ("+uidv+" 0 1:2))", false); err != nil {
		return fail(err)
	}
	if err := step(a, "a19", "LOGOUT", true); err != nil {
		return fail(err)
	}
	if err := step(b, "b7", "LOGOUT", true); err != nil {
		return fail(err)
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

func normalizeKinds(lines []string, mb1, mb2 string) []string {
	var out []string
	for _, line := range lines {
		line = strings.ReplaceAll(line, mb2, "MB2")
		line = strings.ReplaceAll(line, mb1, "MB1")
		if k, ok := kindOf(line); ok {
			out = append(out, k)
		}
	}
	return out
}

func kindOf(line string) (string, bool) {
	switch {
	case strings.HasPrefix(line, "* CAPABILITY "):
		return "* CAPABILITY", true
	case strings.HasPrefix(line, "* FLAGS"):
		return "* FLAGS", true
	case strings.HasPrefix(line, "* BYE"):
		return "* BYE", true
	case strings.HasPrefix(line, "* ENABLED"):
		rest := strings.TrimSpace(strings.TrimPrefix(line, "* ENABLED"))
		if rest == "" {
			return "* ENABLED", true
		}
		return "* ENABLED " + rest, true
	case strings.HasPrefix(line, "* VANISHED (EARLIER)"):
		return "* VANISHED EARLIER", true
	case strings.HasPrefix(line, "* VANISHED"):
		return "* VANISHED", true
	case strings.HasPrefix(line, "* LIST "):
		return listKind(line), true
	case strings.HasPrefix(line, "* STATUS "):
		return statusKind(line), true
	case strings.HasPrefix(line, "* SEARCH"):
		return "* SEARCH", true
	case strings.HasPrefix(line, "* SORT"):
		return "* SORT", true
	case strings.HasPrefix(line, "* THREAD"):
		return threadShape(line), true
	case strings.HasPrefix(line, "* ESEARCH"):
		return esearchKind(line), true
	case reExists.MatchString(line):
		return "* EXISTS", true
	case reRecent.MatchString(line):
		return "* RECENT", true
	case reExpunge.MatchString(line):
		return "* EXPUNGE", true
	case reSeqFetch.MatchString(line):
		return fetchKind(line), true
	case strings.HasPrefix(line, "* OK"):
		code := bracketCode(line)
		switch code {
		case "HIGHESTMODSEQ", "CLOSED", "PERMANENTFLAGS", "UIDVALIDITY", "UIDNEXT", "UNSEEN", "CAPABILITY":
			return "* OK [" + code + "]", true
		default:
			return "", false
		}
	}
	fields := strings.SplitN(line, " ", 3)
	if len(fields) < 2 || strings.HasPrefix(line, "*") {
		return "", false
	}
	status := fields[1]
	if status != "OK" && status != "NO" && status != "BAD" {
		return "", false
	}
	if status != "OK" {
		return "T " + status, true
	}
	code := ""
	if len(fields) == 3 {
		code = bracketCode(fields[2])
	}
	switch code {
	case "MODIFIED", "HIGHESTMODSEQ", "APPENDUID", "READ-WRITE", "READ-ONLY", "COPYUID":
		return "T OK [" + code + "]", true
	default:
		return "T OK", true
	}
}

func fetchKind(line string) string {
	i := strings.Index(line, "FETCH (")
	if i < 0 {
		return "* FETCH"
	}
	body := line[i+len("FETCH ("):]
	if strings.HasSuffix(body, ")") {
		body = body[:len(body)-1]
	}
	var names []string
	for len(body) > 0 {
		body = strings.TrimLeft(body, " ")
		if body == "" {
			break
		}
		j := strings.IndexByte(body, ' ')
		name := body
		if j >= 0 {
			name = body[:j]
			body = body[j+1:]
		} else {
			body = ""
		}
		names = append(names, name)
		body = strings.TrimLeft(body, " ")
		_, body = skipKindValue(body)
	}
	return "* FETCH " + strings.Join(names, " ")
}

func skipKindValue(s string) (string, string) {
	if s == "" {
		return "", ""
	}
	switch s[0] {
	case '(':
		depth := 0
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
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
	default:
		j := strings.IndexByte(s, ' ')
		if j < 0 {
			return s, ""
		}
		return s[:j], s[j:]
	}
}

func esearchKind(line string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "* ESEARCH"))
	var b strings.Builder
	b.WriteString("* ESEARCH")
	if strings.HasPrefix(rest, "(") {
		_, rest = skipKindValue(rest)
		rest = strings.TrimLeft(rest, " ")
	}
	if rest == "UID" || strings.HasPrefix(rest, "UID ") {
		b.WriteString(" UID")
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "UID"))
	}
	for rest != "" {
		rest = strings.TrimLeft(rest, " ")
		name := rest
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			name = rest[:i]
			rest = strings.TrimLeft(rest[i+1:], " ")
		} else {
			rest = ""
		}
		b.WriteByte(' ')
		b.WriteString(name)
		_, rest = skipKindValue(rest)
	}
	return b.String()
}

func listKind(line string) string {
	i := strings.IndexByte(line, '(')
	j := strings.IndexByte(line, ')')
	if i < 0 || j < i {
		return "* LIST"
	}
	flags := strings.TrimSpace(line[i+1 : j])
	if flags == "" {
		return "* LIST"
	}
	return "* LIST " + flags
}

func statusKind(line string) string {
	i := strings.LastIndexByte(line, '(')
	j := strings.LastIndexByte(line, ')')
	if i < 0 || j < i {
		return "* STATUS"
	}
	fields := strings.Fields(line[i+1 : j])
	var names []string
	for n := 0; n+1 < len(fields); n += 2 {
		names = append(names, fields[n])
	}
	if len(names) == 0 {
		return "* STATUS"
	}
	return "* STATUS " + strings.Join(names, " ")
}

// runListScript is the third probe. It exercises extended LIST and CREATE USE.
// The first two scripts are unchanged. The kind diff ignores counts.
func runListScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb1, mb2, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	child := mb1 + "/k"
	clash := mb2 + "x"
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
		lines, err := talk.command("l0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "l0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteListMailboxes(host, port, tlsMode, user, pass, child, mb1, mb2)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	q := func(s string) string { return quoteIMAP(s) }
	steps := []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"l1", "CAPABILITY", false},
		{"l2", "LOGIN " + q(user) + " " + q(pass), false},
		{"l3", "CAPABILITY", false},
		{"l4", "CREATE " + q(mb1), false},
		{"l5", "CREATE " + q(child), false},
		{"l6", "LIST " + q("") + " " + q(mb1+"*"), false},
		{"l7", "LIST " + q("") + " " + q(mb1+"*") + " RETURN (CHILDREN)", false},
		{"l8", "LIST " + q("") + " " + q(mb1+"*") + " RETURN (CHILDREN SPECIAL-USE)", false},
		{"l9", "LIST " + q("") + " " + q(mb1+"*") + " RETURN (STATUS (MESSAGES UNSEEN))", false},
		{"l10", "CREATE " + q(mb2) + ` (USE (\Sent))`, false},
		{"l11", "LIST " + q("") + " " + q(mb2), false},
		{"l12", "LIST " + q("") + " " + q(mb2) + " RETURN (CHILDREN SPECIAL-USE)", false},
		{"l13", "LIST (SPECIAL-USE) " + q("") + " " + q(mb2), false},
		{"l14", "CREATE " + q(clash) + ` (USE (\Sent))`, true},
		{"l15", "LIST " + q("") + " " + q(clash), false},
		{"l16", "LIST " + q("") + " " + q(""), false},
		{"l17", "LIST " + q("") + " " + q("") + " RETURN (CHILDREN)", false},
		{"l18", "LOGOUT", true},
	}
	for _, st := range steps {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteListMailboxes(host, port, tlsMode, user, pass, child, mb1, mb2); err != nil {
		return out, err
	}
	return out, nil
}

func deleteListMailboxes(host string, port int, tlsMode, user, pass, child, mb1, mb2 string) error {
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
	// Cyrus refuses DELETE while a special-use flag is set. Clearing it is
	// not part of the compared script. A server without SETMETADATA still
	// deletes the mailbox below.
	_, _ = talk.command("d2", "SETMETADATA "+quoteIMAP(mb2)+" (/private/specialuse NIL)")
	names := []string{child, mb1, mb2}
	for i, name := range names {
		tag := fmt.Sprintf("d%d", i+3)
		lines, err = talk.command(tag, "DELETE "+quoteIMAP(name))
		if err != nil {
			return err
		}
		if taggedNo(lines[len(lines)-1], tag) {
			return fmt.Errorf("cleanup delete failed")
		}
	}
	_, err = talk.command("d6", "LOGOUT")
	return err
}

func compareList(cfg opConfig) (string, error) {
	remote, err := runListScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runListScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

// runEsearchScript is a further probe. It exercises ESEARCH and a positive
// PARTIAL window. The earlier scripts are unchanged.
func runEsearchScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb, mb2: mb}
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
		lines, err := talk.command("e0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "e0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	appendMsg := func(tag string) error {
		head := fmt.Sprintf("%s APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n", tag, quoteIMAP(mb), len(probeMessage))
		if _, err := io.WriteString(talk.w, head); err != nil {
			return err
		}
		if _, err := talk.w.Write(probeMessage); err != nil {
			return err
		}
		if _, err := io.WriteString(talk.w, "\r\n"); err != nil {
			return err
		}
		if err := talk.w.Flush(); err != nil {
			return err
		}
		var lines []string
		for {
			line, err := talk.readLogical()
			if err != nil {
				out.lines = append(out.lines, lines...)
				return err
			}
			lines = append(lines, line)
			if strings.HasPrefix(line, tag+" ") {
				break
			}
		}
		out.lines = append(out.lines, lines...)
		if taggedNo(lines[len(lines)-1], tag) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	for _, st := range []struct{ tag, cmd string }{
		{"e1", "CAPABILITY"},
		{"e2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"e3", "CAPABILITY"},
		{"e4", "CREATE " + quoteIMAP(mb)},
		{"e5", "SELECT " + quoteIMAP(mb)},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	for _, tag := range []string{"e6", "e7", "e8"} {
		if err := appendMsg(tag); err != nil {
			return fail(err)
		}
	}
	q := quoteIMAP
	steps := []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"e9", "SEARCH ALL", false},
		{"e10", "UID SEARCH ALL", false},
		{"e11", "SEARCH RETURN (MIN MAX COUNT ALL) ALL", false},
		{"e12", "UID SEARCH RETURN (MIN MAX COUNT ALL) ALL", false},
		{"e13", "SEARCH RETURN (PARTIAL 1:2 COUNT) ALL", false},
		{"e14", "UID SEARCH RETURN (PARTIAL 1:2 COUNT) ALL", false},
		{"e15", "SEARCH RETURN (MIN MAX COUNT ALL) FROM " + q("nosuch@example.test"), false},
		{"e16", "UID SEARCH RETURN (PARTIAL 50:60 COUNT) ALL", false},
		{"e17", "LOGOUT", true},
	}
	for _, st := range steps {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

// runCompressScript is a further probe. It turns on COMPRESS=DEFLATE and
// reads the decompressed replies. The earlier scripts are unchanged.
func runCompressScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb, mb2: mb}
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
		lines, err := talk.command("z0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "z0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	for _, st := range []struct{ tag, cmd string }{
		{"z1", "CAPABILITY"},
		{"z2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"z3", "CAPABILITY"},
		{"z4", "COMPRESS DEFLATE"},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	if err := talk.startDeflate(); err != nil {
		return fail(err)
	}
	if err := step("z5", "CAPABILITY", false); err != nil {
		return fail(err)
	}
	if err := step("z6", "CREATE "+quoteIMAP(mb), false); err != nil {
		return fail(err)
	}
	if err := step("z7", "SELECT "+quoteIMAP(mb), false); err != nil {
		return fail(err)
	}
	tag := "z8"
	head := fmt.Sprintf("%s APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n", tag, quoteIMAP(mb), len(probeMessage))
	if _, err := io.WriteString(talk.w, head); err != nil {
		return fail(err)
	}
	if _, err := talk.w.Write(probeMessage); err != nil {
		return fail(err)
	}
	if _, err := io.WriteString(talk.w, "\r\n"); err != nil {
		return fail(err)
	}
	if err := talk.w.Flush(); err != nil {
		return fail(err)
	}
	var lines []string
	for {
		line, err := talk.readLogical()
		if err != nil {
			out.lines = append(out.lines, lines...)
			return fail(err)
		}
		lines = append(lines, line)
		if strings.HasPrefix(line, tag+" ") {
			break
		}
	}
	out.lines = append(out.lines, lines...)
	if taggedNo(lines[len(lines)-1], tag) {
		return fail(fmt.Errorf("%s failed", tag))
	}
	for _, st := range []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"z9", "FETCH 1 (FLAGS UID BODY.PEEK[TEXT])", false},
		{"z10", "SEARCH ALL", false},
		{"z11", "NOOP", false},
		{"z12", "LOGOUT", true},
	} {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

func compareCompress(cfg opConfig) (string, error) {
	remote, err := runCompressScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runCompressScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

// runSortScript is a further probe. It sorts on FROM, DATE, SUBJECT, and
// SIZE, one REVERSE key, and one positive PARTIAL window. The earlier
// scripts are unchanged.
func runSortScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb, mb2: mb}
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
		lines, err := talk.command("s0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "s0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	appendRaw := func(tag string, raw []byte) error {
		head := fmt.Sprintf("%s APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n", tag, quoteIMAP(mb), len(raw))
		if _, err := io.WriteString(talk.w, head); err != nil {
			return err
		}
		if _, err := talk.w.Write(raw); err != nil {
			return err
		}
		if _, err := io.WriteString(talk.w, "\r\n"); err != nil {
			return err
		}
		if err := talk.w.Flush(); err != nil {
			return err
		}
		var lines []string
		for {
			line, err := talk.readLogical()
			if err != nil {
				out.lines = append(out.lines, lines...)
				return err
			}
			lines = append(lines, line)
			if strings.HasPrefix(line, tag+" ") {
				break
			}
		}
		out.lines = append(out.lines, lines...)
		if taggedNo(lines[len(lines)-1], tag) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	msgs := []struct {
		from, date, subj, body string
	}{
		{"amy@example.test", "Tue, 02 Jan 2024 00:00:00 +0000", "apple", "x"},
		{"zed@example.test", "Wed, 03 Jan 2024 00:00:00 +0000", "mango", "xx"},
		{"mid@example.test", "Mon, 01 Jan 2024 00:00:00 +0000", "cherry", "xxxx"},
	}
	for _, st := range []struct{ tag, cmd string }{
		{"s1", "CAPABILITY"},
		{"s2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"s3", "CAPABILITY"},
		{"s4", "CREATE " + quoteIMAP(mb)},
		{"s5", "SELECT " + quoteIMAP(mb)},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	for i, m := range msgs {
		raw := []byte(fmt.Sprintf("From: %s\r\nTo: dest@example.test\r\nSubject: %s\r\nDate: %s\r\n\r\n%s", m.from, m.subj, m.date, m.body))
		if err := appendRaw(fmt.Sprintf("s%d", i+6), raw); err != nil {
			return fail(err)
		}
	}
	steps := []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"s9", "SORT (FROM) UTF-8 ALL", false},
		{"s10", "UID SORT (DATE) UTF-8 ALL", false},
		{"s11", "SORT (SUBJECT) UTF-8 ALL", false},
		{"s12", "UID SORT (SIZE) UTF-8 ALL", false},
		{"s13", "UID SORT (REVERSE FROM) UTF-8 ALL", false},
		{"s14", "UID SORT RETURN (PARTIAL 1:2 COUNT) (FROM) UTF-8 ALL", false},
		{"s15", "LOGOUT", true},
	}
	for _, st := range steps {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

// runDisplayScript is a further probe. One DISPLAYFROM key uses a display
// name that sorts differently from the address. The earlier scripts are unchanged.
func runDisplayScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb, mb2: mb}
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
		lines, err := talk.command("d0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "d0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	appendRaw := func(tag string, raw []byte) error {
		head := fmt.Sprintf("%s APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n", tag, quoteIMAP(mb), len(raw))
		if _, err := io.WriteString(talk.w, head); err != nil {
			return err
		}
		if _, err := talk.w.Write(raw); err != nil {
			return err
		}
		if _, err := io.WriteString(talk.w, "\r\n"); err != nil {
			return err
		}
		if err := talk.w.Flush(); err != nil {
			return err
		}
		var lines []string
		for {
			line, err := talk.readLogical()
			if err != nil {
				out.lines = append(out.lines, lines...)
				return err
			}
			lines = append(lines, line)
			if strings.HasPrefix(line, tag+" ") {
				break
			}
		}
		out.lines = append(out.lines, lines...)
		if taggedNo(lines[len(lines)-1], tag) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	msgs := []string{
		"From: Zed Person <amy@example.test>\r\nTo: dest@example.test\r\nSubject: one\r\nDate: Tue, 02 Jan 2024 00:00:00 +0000\r\n\r\na",
		"From: Amy Person <zed@example.test>\r\nTo: dest@example.test\r\nSubject: two\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\nb",
	}
	for _, st := range []struct{ tag, cmd string }{
		{"d1", "CAPABILITY"},
		{"d2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"d3", "CAPABILITY"},
		{"d4", "CREATE " + quoteIMAP(mb)},
		{"d5", "SELECT " + quoteIMAP(mb)},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	for i, raw := range msgs {
		if err := appendRaw(fmt.Sprintf("d%d", i+6), []byte(raw)); err != nil {
			return fail(err)
		}
	}
	for _, st := range []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"d8", "UID SORT (DISPLAYFROM) UTF-8 ALL", false},
		{"d9", "LOGOUT", true},
	} {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

func threadShape(line string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "* THREAD"))
	var b strings.Builder
	b.WriteString("* THREAD")
	if rest == "" {
		return b.String()
	}
	b.WriteByte(' ')
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c >= '0' && c <= '9' {
			for i+1 < len(rest) && rest[i+1] >= '0' && rest[i+1] <= '9' {
				i++
			}
			b.WriteByte('N')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// runThreadScript is a further probe. It threads one REFERENCES case and
// one ORDEREDSUBJECT case. The messages have distinct subjects and one
// reply whose parent is in the set. The earlier scripts are unchanged.
func runThreadScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb, mb2: mb}
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
		lines, err := talk.command("h0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "h0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	appendRaw := func(tag string, raw []byte) error {
		head := fmt.Sprintf("%s APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n", tag, quoteIMAP(mb), len(raw))
		if _, err := io.WriteString(talk.w, head); err != nil {
			return err
		}
		if _, err := talk.w.Write(raw); err != nil {
			return err
		}
		if _, err := io.WriteString(talk.w, "\r\n"); err != nil {
			return err
		}
		if err := talk.w.Flush(); err != nil {
			return err
		}
		var lines []string
		for {
			line, err := talk.readLogical()
			if err != nil {
				out.lines = append(out.lines, lines...)
				return err
			}
			lines = append(lines, line)
			if strings.HasPrefix(line, tag+" ") {
				break
			}
		}
		out.lines = append(out.lines, lines...)
		if taggedNo(lines[len(lines)-1], tag) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	msgs := []string{
		"From: a@example.test\r\nTo: dest@example.test\r\nSubject: apple\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\nMessage-ID: <thread-apple@example.test>\r\n\r\nx\r\n",
		"From: a@example.test\r\nTo: dest@example.test\r\nSubject: Re: apple\r\nDate: Tue, 02 Jan 2024 00:00:00 +0000\r\nMessage-ID: <thread-reply@example.test>\r\nIn-Reply-To: <thread-apple@example.test>\r\nReferences: <thread-apple@example.test>\r\n\r\nxx\r\n",
		"From: a@example.test\r\nTo: dest@example.test\r\nSubject: zebra\r\nDate: Wed, 03 Jan 2024 00:00:00 +0000\r\nMessage-ID: <thread-zebra@example.test>\r\n\r\nxxx\r\n",
	}
	for _, st := range []struct{ tag, cmd string }{
		{"h1", "CAPABILITY"},
		{"h2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"h3", "CAPABILITY"},
		{"h4", "CREATE " + quoteIMAP(mb)},
		{"h5", "SELECT " + quoteIMAP(mb)},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	for i, raw := range msgs {
		if err := appendRaw(fmt.Sprintf("h%d", i+6), []byte(raw)); err != nil {
			return fail(err)
		}
	}
	steps := []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"h9", "UID THREAD REFERENCES UTF-8 ALL", false},
		{"h10", "UID THREAD ORDEREDSUBJECT UTF-8 ALL", false},
		{"h11", "LOGOUT", true},
	}
	for _, st := range steps {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

func compareThread(cfg opConfig) (string, error) {
	remote, err := runThreadScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runThreadScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

func compareDisplay(cfg opConfig) (string, error) {
	remote, err := runDisplayScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runDisplayScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

func compareSort(cfg opConfig) (string, error) {
	remote, err := runSortScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runSortScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

func compareEsearch(cfg opConfig) (string, error) {
	remote, err := runEsearchScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runEsearchScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

func compareCondstore(cfg opConfig) (string, error) {
	remote, err := runCondstoreScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runCondstoreScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

// runUtf8Script is a further probe. It creates a modified UTF-7 mailbox
// and searches with CHARSET UTF-8. The earlier scripts are unchanged.
func runUtf8Script(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	id, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	mb := id + "caf&AOk-"
	out := probeScript{mb1: mb, mb2: mb}
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
		lines, err := talk.command("u0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "u0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	raw := []byte("From: a@example.test\r\nTo: b@example.test\r\nSubject: apple\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\nx\r\n")
	for _, st := range []struct{ tag, cmd string }{
		{"u1", "CAPABILITY"},
		{"u2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"u3", "CAPABILITY"},
		{"u4", "CREATE " + quoteIMAP(mb)},
		{"u5", "LIST \"\" " + quoteIMAP(mb)},
		{"u6", "SELECT " + quoteIMAP(mb)},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	appendCmd := fmt.Sprintf("APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n%s", quoteIMAP(mb), len(raw), raw)
	if err := step("u7", appendCmd, false); err != nil {
		return fail(err)
	}
	for _, st := range []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"u8", "SEARCH CHARSET UTF-8 SUBJECT apple", false},
		{"u9", "SORT (SUBJECT) UTF-8 ALL", false},
		{"u10", "LOGOUT", true},
	} {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

func compareUtf8(cfg opConfig) (string, error) {
	remote, err := runUtf8Script(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runUtf8Script(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

// runBinaryScript is a further probe. It fetches one base64 part with
// BINARY.PEEK and one partial of that part. The earlier scripts are unchanged.
func runBinaryScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb, mb2: mb}
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
		lines, err := talk.command("y0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "y0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	for _, st := range []struct{ tag, cmd string }{
		{"y1", "CAPABILITY"},
		{"y2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"y3", "CAPABILITY"},
		{"y4", "CREATE " + quoteIMAP(mb)},
		{"y5", "SELECT " + quoteIMAP(mb)},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	tag := "y6"
	head := fmt.Sprintf("%s APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n", tag, quoteIMAP(mb), len(binaryProbeMessage))
	if _, err := io.WriteString(talk.w, head); err != nil {
		return fail(err)
	}
	if _, err := talk.w.Write(binaryProbeMessage); err != nil {
		return fail(err)
	}
	if _, err := io.WriteString(talk.w, "\r\n"); err != nil {
		return fail(err)
	}
	if err := talk.w.Flush(); err != nil {
		return fail(err)
	}
	for {
		line, err := talk.readLogical()
		if err != nil {
			return fail(err)
		}
		out.lines = append(out.lines, line)
		if strings.HasPrefix(line, tag+" ") {
			if taggedNo(line, tag) {
				return fail(fmt.Errorf("%s failed", tag))
			}
			break
		}
	}
	for _, st := range []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"y7", "FETCH 1 (BINARY.PEEK[1])", false},
		{"y8", "FETCH 1 (BINARY.PEEK[1]<0.5>)", false},
		{"y9", "LOGOUT", true},
	} {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

func compareBinary(cfg opConfig) (string, error) {
	remote, err := runBinaryScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runBinaryScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

// runFuzzyScript is a further probe. It searches one appended word with
// UID SEARCH RETURN (COUNT) FUZZY TEXT. The earlier scripts are unchanged.
func runFuzzyScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb, mb2: mb}
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
		lines, err := talk.command("f0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "f0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	for _, st := range []struct{ tag, cmd string }{
		{"f1", "CAPABILITY"},
		{"f2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"f3", "CAPABILITY"},
		{"f4", "CREATE " + quoteIMAP(mb)},
		{"f5", "SELECT " + quoteIMAP(mb)},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	raw := []byte("From: a@example.test\r\nTo: b@example.test\r\nSubject: probe\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\napple\r\n")
	appendCmd := fmt.Sprintf("APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n%s", quoteIMAP(mb), len(raw), raw)
	if err := step("f6", appendCmd, false); err != nil {
		return fail(err)
	}
	for _, st := range []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"f7", "UID SEARCH RETURN (COUNT) FUZZY TEXT apple", false},
		{"f8", "LOGOUT", true},
	} {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

func compareFuzzy(cfg opConfig) (string, error) {
	remote, err := runFuzzyScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runFuzzyScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

// runMultisearchScript is a further probe. It sends one ESEARCH IN subtree
// whose word is in one child of a fresh mailbox. The earlier scripts are unchanged.
func runMultisearchScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	parent, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	child := parent + "/k"
	out := probeScript{mb1: parent, mb2: child}
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
		lines, err := talk.command("m0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "m0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, parent, child)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	for _, st := range []struct{ tag, cmd string }{
		{"m1", "CAPABILITY"},
		{"m2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"m3", "CAPABILITY"},
		{"m4", "CREATE " + quoteIMAP(parent)},
		{"m5", "CREATE " + quoteIMAP(child)},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	raw := []byte("From: a@example.test\r\nTo: b@example.test\r\nSubject: probe\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\nmultisearchprobe\r\n")
	appendCmd := fmt.Sprintf("APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n%s", quoteIMAP(child), len(raw), raw)
	if err := step("m6", appendCmd, false); err != nil {
		return fail(err)
	}
	search := "ESEARCH IN (subtree " + quoteIMAP(parent) + ") RETURN (ALL) TEXT multisearchprobe"
	for _, st := range []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"m7", search, false},
		{"m8", "LOGOUT", true},
	} {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, parent, child); err != nil {
		return out, err
	}
	return out, nil
}

func compareMultisearch(cfg opConfig) (string, error) {
	remote, err := runMultisearchScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runMultisearchScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}

// runPreviewScript is a further probe. It fetches PREVIEW of one text/plain
// message. The earlier scripts are unchanged.
func runPreviewScript(host string, port int, tlsMode, user, pass string) (probeScript, error) {
	mb, _, err := freshMailboxPair()
	if err != nil {
		return probeScript{}, err
	}
	out := probeScript{mb1: mb, mb2: mb}
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
		lines, err := talk.command("w0", "STARTTLS")
		out.lines = append(out.lines, lines...)
		if err != nil {
			return out, err
		}
		if len(lines) == 0 || taggedNo(lines[len(lines)-1], "w0") {
			return out, fmt.Errorf("STARTTLS failed")
		}
		tlsConn := tls.Client(talk.conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			return out, err
		}
		_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
		talk.conn = tlsConn
		talk.r = bufio.NewReader(tlsConn)
		talk.w = bufio.NewWriter(tlsConn)
	}
	fail := func(err error) (probeScript, error) {
		_ = deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb)
		return out, err
	}
	step := func(tag, cmd string, allowNo bool) error {
		lines, err := talk.command(tag, cmd)
		out.lines = append(out.lines, lines...)
		if err != nil {
			return err
		}
		if len(lines) == 0 || (taggedNo(lines[len(lines)-1], tag) && !allowNo) {
			return fmt.Errorf("%s failed", tag)
		}
		return nil
	}
	for _, st := range []struct{ tag, cmd string }{
		{"w1", "CAPABILITY"},
		{"w2", "LOGIN " + quoteIMAP(user) + " " + quoteIMAP(pass)},
		{"w3", "CAPABILITY"},
		{"w4", "CREATE " + quoteIMAP(mb)},
		{"w5", "SELECT " + quoteIMAP(mb)},
	} {
		if err := step(st.tag, st.cmd, false); err != nil {
			return fail(err)
		}
	}
	tag := "w6"
	head := fmt.Sprintf("%s APPEND %s \"01-Jan-2024 00:00:00 +0000\" {%d+}\r\n", tag, quoteIMAP(mb), len(previewProbeMessage))
	if _, err := io.WriteString(talk.w, head); err != nil {
		return fail(err)
	}
	if _, err := talk.w.Write(previewProbeMessage); err != nil {
		return fail(err)
	}
	if _, err := io.WriteString(talk.w, "\r\n"); err != nil {
		return fail(err)
	}
	if err := talk.w.Flush(); err != nil {
		return fail(err)
	}
	for {
		line, err := talk.readLogical()
		if err != nil {
			return fail(err)
		}
		out.lines = append(out.lines, line)
		if strings.HasPrefix(line, tag+" ") {
			if taggedNo(line, tag) {
				return fail(fmt.Errorf("%s failed", tag))
			}
			break
		}
	}
	for _, st := range []struct {
		tag, cmd string
		allowNo  bool
	}{
		{"w7", "FETCH 1 (PREVIEW)", false},
		{"w8", "LOGOUT", true},
	} {
		if err := step(st.tag, st.cmd, st.allowNo); err != nil {
			return fail(err)
		}
	}
	if err := deleteProbeMailboxes(host, port, tlsMode, user, pass, mb, mb); err != nil {
		return out, err
	}
	return out, nil
}

func comparePreview(cfg opConfig) (string, error) {
	remote, err := runPreviewScript(cfg.Host, cfg.Port, cfg.TLS, cfg.Username, cfg.Password)
	if err != nil && len(remote.lines) == 0 {
		return "", err
	}
	addr, stop, serr := startProbeServer(cfg.Username, cfg.Password, cfg.Personality)
	if serr != nil {
		if err != nil {
			return "", err
		}
		return "", serr
	}
	defer stop()
	host, portS, perr := net.SplitHostPort(addr)
	if perr != nil {
		return "", perr
	}
	port, perr := strconv.Atoi(portS)
	if perr != nil {
		return "", perr
	}
	local, lerr := runPreviewScript(host, port, "none", cfg.Username, cfg.Password)
	diff := scriptDiff(normalizeKinds(remote.lines, remote.mb1, remote.mb2), normalizeKinds(local.lines, local.mb1, local.mb2))
	if err != nil && diff == "" {
		return "", err
	}
	if lerr != nil && diff == "" {
		return "", lerr
	}
	return diff, nil
}
