package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompressDeflate(t *testing.T) {
	p, err := loadPersonality("personalities/compress.json")
	if err != nil {
		t.Fatal(err)
	}
	if !listed(p.PreLogin, "COMPRESS=DEFLATE") || !listed(p.PostLogin, "COMPRESS=DEFLATE") {
		t.Fatal("compress.json missing COMPRESS=DEFLATE")
	}

	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: "personalities/compress.json"})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	caps := cl.cmd("CAPABILITY")
	caps.ok(t)
	requireCaps(t, capsOf(caps.untagged[0]), []string{"COMPRESS=DEFLATE"})

	if cl.cmd("COMPRESS").status != "BAD" {
		t.Fatal("COMPRESS with no algorithm was not BAD")
	}
	if cl.cmd("COMPRESS NOTREAL").status != "BAD" {
		t.Fatal("unknown algorithm was not BAD")
	}
	cl.cmd("NOOP").ok(t)

	okLine := cl.cmd("COMPRESS DEFLATE")
	okLine.ok(t)
	if !strings.Contains(okLine.tagLine, " OK ") {
		t.Fatalf("cleartext OK %q", okLine.tagLine)
	}
	cl.enableDeflate()

	again := cl.cmd("CAPABILITY")
	again.ok(t)
	requireCaps(t, capsOf(again.untagged[0]), []string{"COMPRESS=DEFLATE"})
	if cl.cmd("COMPRESS DEFLATE").status != "NO" {
		t.Fatal("second COMPRESS was not NO")
	}
	cl.cmd("NOOP").ok(t)
	cl.cmd("SELECT INBOX").ok(t)
	body := fetchLiteral(t, fetchLine(t, cl.cmd("FETCH 1 (BODY.PEEK[TEXT])").mustOK(t)))
	if !bytes.Contains(body, []byte("in the body")) {
		t.Fatalf("compressed fetch body %q", body)
	}
}

func TestCompressNotAdvertised(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-compress.json")
	p, err := loadPersonality("personalities/cyrus-3137.json")
	if err != nil {
		t.Fatal(err)
	}
	p.GreetingCaps = dropCap(p.GreetingCaps, "COMPRESS=DEFLATE")
	p.PreLogin = dropCap(p.PreLogin, "COMPRESS=DEFLATE")
	p.PostLogin = dropCap(p.PostLogin, "COMPRESS=DEFLATE")
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	_, bound := start(t, Config{Listen: "127.0.0.1:0", Personality: path})
	cl := dial(t, bound.Listen)
	cl.cmd("LOGIN demo secret").ok(t)
	if cl.cmd("COMPRESS DEFLATE").status != "NO" {
		t.Fatal("unadvertised COMPRESS DEFLATE was not NO")
	}
	cl.cmd("NOOP").ok(t)
	if cl.cmd("COMPRESS").status != "BAD" {
		t.Fatal("COMPRESS with no algorithm was not BAD")
	}
	if cl.cmd("COMPRESS OTHER").status != "BAD" {
		t.Fatal("unknown algorithm was not BAD")
	}
	cl.cmd("NOOP").ok(t)
}

func (cl *imapClient) enableDeflate() {
	cl.t.Helper()
	var src io.Reader = cl.c
	if n := cl.r.Buffered(); n > 0 {
		pre := make([]byte, n)
		if _, err := io.ReadFull(cl.r, pre); err != nil {
			cl.t.Fatal(err)
		}
		src = io.MultiReader(bytes.NewReader(pre), cl.c)
	}
	zw, err := newSyncDeflateWriter(cl.c)
	if err != nil {
		cl.t.Fatal(err)
	}
	cl.w = bufio.NewWriter(zw)
	cl.r = bufio.NewReader(&lazyDeflateReader{r: src})
}
