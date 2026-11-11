package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"
)

func toCRLF(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	b = bytes.ReplaceAll(b, []byte("\r"), []byte("\n"))
	return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
}

func splitHB(raw []byte) (header, body []byte) {
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return raw[:i+4], raw[i+4:]
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return raw[:i+2], raw[i+2:]
	}
	return raw, nil
}

func decodeHdr(s string) string {
	if s == "" {
		return ""
	}
	d, err := new(mime.WordDecoder).DecodeHeader(s)
	if err != nil || d == "" {
		return s
	}
	return d
}

// imapAtom returns an atom when s is one, otherwise a quoted string or literal.
func imapAtom(s string) string {
	if s == "" {
		return `""`
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x20 || c >= 0x7f || strings.ContainsRune("(){%*\"\\]", rune(c)) {
			return imapString(s)
		}
	}
	return s
}

func imapString(s string) string {
	if s == "" {
		return `""`
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x7f || c == '"' || c == '\\' {
			return fmt.Sprintf("{%d}\r\n%s", len(s), s)
		}
	}
	return `"` + s + `"`
}

func nstring(s string) string {
	if s == "" {
		return "NIL"
	}
	return imapString(s)
}

func literal(b []byte) string {
	return fmt.Sprintf("{%d}\r\n%s", len(b), string(b))
}

func parseIMAPDate(s string) (time.Time, error) {
	for _, layout := range []string{"2-Jan-2006", "02-Jan-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad date")
}

func parseIMAPDateTime(s string) (time.Time, error) {
	for _, layout := range []string{
		"2-Jan-2006 15:04:05 -0700",
		"02-Jan-2006 15:04:05 -0700",
		"2-Jan-2006 15:04:05 +0000",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad date-time")
}

func ymd(t time.Time) int {
	y, m, d := t.Date()
	return y*10000 + int(m)*100 + d
}

func formatInternalDate(t time.Time) string {
	return t.Format("02-Jan-2006 15:04:05 -0700")
}

type hdr interface {
	Get(string) string
}

type node struct {
	multipart bool
	typ, sub  string
	params    map[string]string
	id, desc  string
	enc       string
	octets    int
	lines     int
	children  []*node
	body      []byte
	mimeHead  []byte
}

func parseRoot(raw []byte) *node {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return &node{typ: "TEXT", sub: "PLAIN", enc: "7BIT", params: map[string]string{}, body: raw}
	}
	body, _ := io.ReadAll(msg.Body)
	n := parseEntity(msg.Header, body)
	return n
}

func parseEntity(h hdr, body []byte) *node {
	ct := h.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	typ, params, err := mime.ParseMediaType(ct)
	if err != nil || typ == "" {
		typ = "text/plain"
		params = map[string]string{"charset": "us-ascii"}
	}
	if params == nil {
		params = map[string]string{}
	}
	main, sub, _ := strings.Cut(strings.ToLower(typ), "/")
	if sub == "" {
		sub = "plain"
	}
	enc := h.Get("Content-Transfer-Encoding")
	if enc == "" {
		enc = "7bit"
	}
	n := &node{
		typ:    strings.ToUpper(main),
		sub:    strings.ToUpper(sub),
		params: params,
		id:     h.Get("Content-Id"),
		desc:   h.Get("Content-Description"),
		enc:    strings.ToUpper(enc),
		octets: len(body),
		body:   body,
	}
	if mh, ok := h.(textproto.MIMEHeader); ok {
		n.mimeHead = mimeHeaderBytes(mh)
	} else if mh, ok := h.(mail.Header); ok {
		n.mimeHead = contentHeaderBytes(mh)
	}
	if n.typ == "MULTIPART" {
		n.multipart = true
		mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			pb, _ := io.ReadAll(p)
			n.children = append(n.children, parseEntity(p.Header, pb))
		}
		return n
	}
	if n.typ == "TEXT" {
		n.lines = lineCount(body)
	}
	return n
}

func lineCount(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte("\n"))
	if !bytes.HasSuffix(b, []byte("\n")) {
		n++
	}
	return n
}

func mimeHeaderBytes(h textproto.MIMEHeader) []byte {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	for _, k := range keys {
		for _, v := range h[k] {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")
	return b.Bytes()
}

func contentHeaderBytes(h mail.Header) []byte {
	var b bytes.Buffer
	for k, vals := range h {
		if !strings.HasPrefix(strings.ToLower(k), "content-") {
			continue
		}
		for _, v := range vals {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")
	return b.Bytes()
}

func formatParams(p map[string]string) string {
	if len(p) == 0 {
		return "NIL"
	}
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('(')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(imapString(strings.ToUpper(k)))
		b.WriteByte(' ')
		b.WriteString(imapString(p[k]))
	}
	b.WriteByte(')')
	return b.String()
}

func (n *node) format(ext bool) string {
	if n == nil {
		return "NIL"
	}
	if n.multipart {
		var b strings.Builder
		b.WriteByte('(')
		if len(n.children) == 0 {
			b.WriteString("NIL")
		}
		for _, c := range n.children {
			b.WriteString(c.format(ext))
		}
		b.WriteByte(' ')
		b.WriteString(imapString(n.sub))
		if ext {
			b.WriteByte(' ')
			b.WriteString(formatParams(n.params))
			b.WriteString(" NIL NIL NIL")
		}
		b.WriteByte(')')
		return b.String()
	}
	var b strings.Builder
	b.WriteByte('(')
	b.WriteString(imapString(n.typ))
	b.WriteByte(' ')
	b.WriteString(imapString(n.sub))
	b.WriteByte(' ')
	b.WriteString(formatParams(n.params))
	b.WriteByte(' ')
	b.WriteString(nstring(n.id))
	b.WriteByte(' ')
	b.WriteString(nstring(n.desc))
	b.WriteByte(' ')
	b.WriteString(imapString(n.enc))
	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(n.octets))
	if n.typ == "TEXT" {
		b.WriteByte(' ')
		b.WriteString(strconv.Itoa(n.lines))
	}
	if ext {
		b.WriteString(" NIL NIL NIL NIL")
	}
	b.WriteByte(')')
	return b.String()
}

func envelope(raw []byte) string {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "NIL"
	}
	h := msg.Header
	from := addrList(h.Get("From"))
	sender := addrList(h.Get("Sender"))
	if sender == "NIL" {
		sender = from
	}
	reply := addrList(h.Get("Reply-To"))
	if reply == "NIL" {
		reply = from
	}
	return fmt.Sprintf("(%s %s %s %s %s %s %s %s %s %s)",
		nstring(h.Get("Date")),
		nstring(h.Get("Subject")),
		from, sender, reply,
		addrList(h.Get("To")),
		addrList(h.Get("Cc")),
		addrList(h.Get("Bcc")),
		nstring(h.Get("In-Reply-To")),
		nstring(h.Get("Message-Id")),
	)
}

func addrList(h string) string {
	if strings.TrimSpace(h) == "" {
		return "NIL"
	}
	addrs, err := mail.ParseAddressList(h)
	if err != nil || len(addrs) == 0 {
		return "NIL"
	}
	var b strings.Builder
	b.WriteByte('(')
	for _, a := range addrs {
		local, domain, _ := strings.Cut(a.Address, "@")
		b.WriteByte('(')
		b.WriteString(nstring(a.Name))
		b.WriteString(" NIL ")
		b.WriteString(nstring(local))
		b.WriteByte(' ')
		b.WriteString(nstring(domain))
		b.WriteByte(')')
	}
	b.WriteByte(')')
	return b.String()
}

func headerFields(hdr []byte, names []string, not bool) []byte {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(n)] = true
	}
	bare := hdr
	bare = bytes.TrimSuffix(bare, []byte("\r\n\r\n"))
	bare = bytes.TrimSuffix(bare, []byte("\n\n"))
	lines := bytes.Split(bare, []byte("\n"))
	var b bytes.Buffer
	keep := false
	for _, line := range lines {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if keep {
				b.Write(line)
				b.WriteString("\r\n")
			}
			continue
		}
		name, _, _ := strings.Cut(string(line), ":")
		name = strings.ToLower(strings.TrimSpace(name))
		keep = want[name]
		if not {
			keep = !want[name]
		}
		if keep {
			b.Write(line)
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\r\n")
	return b.Bytes()
}

func lookupPart(n *node, nums []int) *node {
	if n == nil {
		return nil
	}
	cur := n
	for i, num := range nums {
		if !cur.multipart {
			if i == 0 && num == 1 && len(nums) == 1 {
				return cur
			}
			if i == 0 && num == 1 {
				cur = cur
				continue
			}
			return nil
		}
		if num < 1 || num > len(cur.children) {
			return nil
		}
		cur = cur.children[num-1]
	}
	return cur
}

func splitSpec(spec string) (nums []int, tail string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, "", nil
	}
	parts := strings.Split(spec, ".")
	i := 0
	for i < len(parts) {
		n, conv := strconv.Atoi(parts[i])
		if conv != nil {
			break
		}
		if n <= 0 {
			return nil, "", fmt.Errorf("bad section")
		}
		nums = append(nums, n)
		i++
	}
	if i < len(parts) {
		tail = strings.Join(parts[i:], ".")
	}
	return nums, tail, nil
}

func parseFieldList(tail string) (not bool, names []string, ok bool) {
	const fields = "HEADER.FIELDS"
	const fieldsNot = "HEADER.FIELDS.NOT"
	if strings.HasPrefix(tail, fieldsNot) {
		not = true
		tail = strings.TrimSpace(tail[len(fieldsNot):])
	} else if strings.HasPrefix(tail, fields) {
		tail = strings.TrimSpace(tail[len(fields):])
	} else {
		return false, nil, false
	}
	if len(tail) < 2 || tail[0] != '(' || tail[len(tail)-1] != ')' {
		return false, nil, false
	}
	inner := strings.TrimSpace(tail[1 : len(tail)-1])
	if inner == "" {
		return not, nil, true
	}
	return not, strings.Fields(inner), true
}

// sectionBytes returns the requested BODY section. ok is false when the
// section does not exist (the response value is NIL).
func sectionBytes(raw []byte, spec string) ([]byte, bool) {
	nums, tail, err := splitSpec(spec)
	if err != nil {
		return nil, false
	}
	hdr, body := splitHB(raw)
	if len(nums) == 0 {
		switch {
		case tail == "":
			return raw, true
		case tail == "TEXT":
			return body, true
		case tail == "HEADER":
			return hdr, true
		case strings.HasPrefix(tail, "HEADER.FIELDS"):
			not, names, ok := parseFieldList(tail)
			if !ok {
				return nil, false
			}
			return headerFields(hdr, names, not), true
		default:
			return nil, false
		}
	}
	root := parseRoot(raw)
	part := lookupPart(root, nums)
	if part == nil {
		return nil, false
	}
	switch {
	case tail == "" || tail == "TEXT":
		return part.body, true
	case tail == "MIME" || tail == "HEADER":
		if part.mimeHead == nil {
			return []byte("\r\n"), true
		}
		return part.mimeHead, true
	case strings.HasPrefix(tail, "HEADER.FIELDS"):
		not, names, ok := parseFieldList(tail)
		if !ok {
			return nil, false
		}
		return headerFields(part.mimeHead, names, not), true
	default:
		return nil, false
	}
}

func applyPartial(b []byte, partial string) ([]byte, bool) {
	if partial == "" {
		return b, true
	}
	if len(partial) < 3 || partial[0] != '<' || partial[len(partial)-1] != '>' {
		return nil, false
	}
	inner := partial[1 : len(partial)-1]
	startS, countS, hasCount := strings.Cut(inner, ".")
	start, err := strconv.Atoi(startS)
	if err != nil || start < 0 {
		return nil, false
	}
	if start > len(b) {
		start = len(b)
	}
	if !hasCount {
		return b[start:], true
	}
	count, err := strconv.Atoi(countS)
	if err != nil || count < 0 {
		return nil, false
	}
	end := start + count
	if end > len(b) {
		end = len(b)
	}
	return b[start:end], true
}

const (
	binaryReady = iota
	binaryMissing
	binaryBadPart
	binaryBadCTE
)

// binarySpec parses a BINARY or BINARY.PEEK fetch item. The section is one
// numeric part. partial is empty or <offset.length>.
func binarySpec(item string) (section, partial string, ok bool) {
	rest := item
	switch {
	case strings.HasPrefix(item, "BINARY.PEEK["):
		rest = item[len("BINARY.PEEK"):]
	case strings.HasPrefix(item, "BINARY["):
		rest = item[len("BINARY"):]
	default:
		return "", "", false
	}
	rb := strings.IndexByte(rest, ']')
	if rb < 2 || rest[0] != '[' {
		return "", "", false
	}
	section = rest[1:rb]
	partial = rest[rb+1:]
	if !numericBinarySection(section) {
		return "", "", false
	}
	if partial != "" && !binaryPartialOK(partial) {
		return "", "", false
	}
	return section, partial, true
}

func numericBinarySection(section string) bool {
	if section == "" {
		return false
	}
	for _, p := range strings.Split(section, ".") {
		if len(p) == 0 || p[0] < '1' || p[0] > '9' {
			return false
		}
		for i := 1; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
	}
	return true
}

func binaryPartialOK(partial string) bool {
	_, _, ok := splitBinaryPartial(partial)
	return ok
}

func splitBinaryPartial(partial string) (start, count int, ok bool) {
	if len(partial) < 5 || partial[0] != '<' || partial[len(partial)-1] != '>' {
		return 0, 0, false
	}
	a, b, found := strings.Cut(partial[1:len(partial)-1], ".")
	if !found || !digitsOnly(a) || !digitsOnly(b) {
		return 0, 0, false
	}
	var err1, err2 error
	start, err1 = strconv.Atoi(a)
	count, err2 = strconv.Atoi(b)
	if err1 != nil || err2 != nil || count <= 0 {
		return 0, 0, false
	}
	return start, count, true
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// decodeBinaryPart returns the part body with its transfer encoding removed.
// 7bit, 8bit, binary, and a missing encoding stay as stored.
func decodeBinaryPart(raw []byte, section string) ([]byte, int) {
	nums, tail, err := splitSpec(section)
	if err != nil || len(nums) == 0 || tail != "" {
		return nil, binaryBadPart
	}
	part := lookupPart(parseRoot(raw), nums)
	if part == nil {
		return nil, binaryMissing
	}
	if part.multipart {
		return nil, binaryBadPart
	}
	b, err := decodeTransfer(part)
	if err != nil {
		return nil, binaryBadCTE
	}
	return b, binaryReady
}

const previewLimit = 256

// previewSnippet is the IMAP PREVIEW text. An empty result is NIL.
func previewSnippet(raw []byte) string {
	return snippetText(parseRoot(raw))
}

// snippetText is the shared IMAP and JMAP preview: the first text/plain
// part, or the body when the message itself is text/plain. Charset is
// decoded the way JMAP body text is. A charset that decode reports as a
// problem is empty. Whitespace runs become one space. The result is at
// most 256 characters. HTML is not parsed.
func snippetText(n *node) string {
	part := firstTextPlain(n)
	if part == nil {
		return ""
	}
	text, problem := decodePartText(part)
	if problem {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) > previewLimit {
		text = string(r[:previewLimit])
	}
	return text
}

func firstTextPlain(n *node) *node {
	if n == nil {
		return nil
	}
	if n.multipart {
		for _, c := range n.children {
			if p := firstTextPlain(c); p != nil {
				return p
			}
		}
		return nil
	}
	if n.typ == "TEXT" && n.sub == "PLAIN" {
		return n
	}
	return nil
}

func decodeTransfer(n *node) ([]byte, error) {
	body := n.body
	if body == nil {
		body = []byte{}
	}
	switch strings.ToLower(strings.TrimSpace(n.enc)) {
	case "", "7bit", "8bit", "binary":
		return body, nil
	case "base64":
		raw := bytes.Map(func(r rune) rune {
			switch r {
			case ' ', '\t', '\r', '\n':
				return -1
			}
			return r
		}, body)
		b, err := base64.StdEncoding.DecodeString(string(raw))
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(string(raw))
		}
		return b, err
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
	default:
		return nil, fmt.Errorf("unknown cte")
	}
}

// renderGenerated builds RFC822 bytes from the index fields. The bytes are
// not stored on the message.
func renderGenerated(m *Message) []byte {
	date := m.internal.Format("Mon, 02 Jan 2006 15:04:05 -0700")
	body := fmt.Sprintf("seed=%s\r\nuid=%d\r\n", m.genSeed, m.genUID)
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", m.from)
	fmt.Fprintf(&b, "To: %s\r\n", m.to)
	fmt.Fprintf(&b, "Subject: %s\r\n", m.subject)
	fmt.Fprintf(&b, "Date: %s\r\n", date)
	fmt.Fprintf(&b, "Message-ID: <%d.%s@fakeimap.example>\r\n", m.genUID, m.genSeed)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.Bytes()
}
