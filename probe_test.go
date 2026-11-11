package main

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestProbeScriptEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/cyrus-3137.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runProbeScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runProbeScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(a.lines, "\n")
	if !strings.Contains(joined, "probe-body") {
		t.Fatalf("script did not return the fixed body:\n%s", joined)
	}
	if !strings.Contains(joined, "* BYE") {
		t.Fatalf("script did not see BYE:\n%s", joined)
	}
	diff := scriptDiff(normalizeScript(a.lines, a.mb1, a.mb2), normalizeScript(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("normalized diff:\n%s", diff)
	}
}

func TestNormalizeDropsCausedNumbers(t *testing.T) {
	lines := []string{
		`* OK [CAPABILITY IMAP4rev1 ID] deadbeefdead Cyrus ready`,
		`* 1 EXISTS`,
		`* 1 FETCH (UID 9 INTERNALDATE "01-Jan-2024 00:00:00 +0000" RFC822.SIZE 4)`,
		`t7 OK [APPENDUID 42 9] APPEND completed`,
		`* OK [HIGHESTMODSEQ 3] Ok`,
		`* BYE LOGOUT received`,
	}
	got := normalizeScript(lines, "mbx", "mbx2")
	want := []string{
		"* OK [CAPABILITY IMAP4rev1] Cyrus ready",
		"* EXISTS",
		`* SEQ FETCH (UID N INTERNALDATE "DATE" RFC822.SIZE SIZE)`,
		"T OK [APPENDUID]",
		"* BYE",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q", got)
	}
	gaps := collectGaps(lines)
	if len(gaps.Capabilities) != 1 || gaps.Capabilities[0] != "ID" {
		t.Fatalf("gaps %+v", gaps)
	}
	if gaps.Commands == nil {
		t.Fatal("commands must be an empty list, not null")
	}
}

func TestCondstoreProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/condstore.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runCondstoreScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runCondstoreScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	diff := scriptDiff(normalizeKinds(a.lines, a.mb1, a.mb2), normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("kind diff:\n%s\n--- a ---\n%s", diff, strings.Join(normalizeKinds(a.lines, a.mb1, a.mb2), "\n"))
	}
	joined := strings.Join(normalizeKinds(a.lines, a.mb1, a.mb2), "\n")
	for _, want := range []string{"* ENABLED CONDSTORE", "* VANISHED", "* VANISHED EARLIER", "T OK [MODIFIED]", "T OK [HIGHESTMODSEQ]", "T BAD", "* OK [HIGHESTMODSEQ]"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in\n%s", want, joined)
		}
	}
}

func TestListProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/list.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runListScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runListScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("kind diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	joined := strings.Join(kinds, "\n")
	for _, want := range []string{"* LIST \\HasChildren", "* LIST \\HasNoChildren \\Sent", "* STATUS MESSAGES UNSEEN", "* LIST \\Noselect", "T NO"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in\n%s", want, joined)
		}
	}
	if strings.Count(joined, "* LIST \\Noselect") != 1 {
		t.Fatalf("noselect count\n%s", joined)
	}
}

func TestEsearchProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/esearch.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runEsearchScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runEsearchScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("kind diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	have := map[string]bool{}
	for _, k := range kinds {
		have[k] = true
	}
	for _, want := range []string{
		"* SEARCH",
		"* ESEARCH COUNT MIN MAX ALL",
		"* ESEARCH UID COUNT MIN MAX ALL",
		"* ESEARCH COUNT PARTIAL",
		"* ESEARCH UID COUNT PARTIAL",
		"* ESEARCH COUNT",
	} {
		if !have[want] {
			t.Fatalf("missing %s in\n%s", want, strings.Join(kinds, "\n"))
		}
	}
}

func TestSortProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/sort.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runSortScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runSortScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("kind diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	have := map[string]bool{}
	for _, k := range kinds {
		have[k] = true
	}
	for _, want := range []string{
		"* SORT",
		"* ESEARCH UID COUNT PARTIAL",
	} {
		if !have[want] {
			t.Fatalf("missing %s in\n%s", want, strings.Join(kinds, "\n"))
		}
	}
}

func TestUtf8ProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/utf8.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runUtf8Script(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runUtf8Script(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("kind diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	joined := strings.Join(a.lines, "\n")
	if !strings.Contains(joined, "caf&AOk-") {
		t.Fatalf("modified utf-7 name missing\n%s", joined)
	}
	have := map[string]bool{}
	for _, k := range kinds {
		have[k] = true
	}
	for _, want := range []string{"* LIST \\HasNoChildren", "* SEARCH", "* SORT"} {
		if !have[want] {
			t.Fatalf("missing %s in\n%s", want, strings.Join(kinds, "\n"))
		}
	}
}

func TestCompressProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/compress.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runCompressScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runCompressScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("kind diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	have := map[string]bool{}
	for _, k := range kinds {
		have[k] = true
	}
	for _, want := range []string{"* CAPABILITY", "* EXISTS", "* FETCH FLAGS UID BODY[TEXT]", "* SEARCH", "T OK"} {
		if !have[want] {
			t.Fatalf("missing %s in\n%s", want, strings.Join(kinds, "\n"))
		}
	}
}

func TestDisplayProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/sort.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runDisplayScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runDisplayScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("kind diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	have := map[string]bool{}
	for _, k := range kinds {
		have[k] = true
	}
	if !have["* SORT"] {
		t.Fatalf("missing * SORT in\n%s", strings.Join(kinds, "\n"))
	}
	var sortLine string
	for _, line := range a.lines {
		if strings.HasPrefix(line, "* SORT") {
			sortLine = line
		}
	}
	if sortLine != "* SORT 2 1" {
		t.Fatalf("DISPLAYFROM order %q", sortLine)
	}
}

func TestThreadProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/thread.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runThreadScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runThreadScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("shape diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	n := 0
	for _, line := range a.lines {
		if strings.HasPrefix(line, "* THREAD ") {
			n++
			if strings.TrimSpace(strings.TrimPrefix(line, "* THREAD")) != "(1 2)(3)" {
				t.Fatalf("shape %q", line)
			}
		}
	}
	if n != 2 {
		t.Fatalf("thread lines %d\n%s", n, strings.Join(a.lines, "\n"))
	}
	if !strings.Contains(strings.Join(kinds, "\n"), "* THREAD (N N)(N)") {
		t.Fatalf("missing shape in\n%s", strings.Join(kinds, "\n"))
	}
}

func TestBinaryProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/binary.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runBinaryScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runBinaryScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("shape diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	var fetches []string
	for _, line := range a.lines {
		if strings.Contains(line, " FETCH (") {
			fetches = append(fetches, fetchKind(line))
			if strings.Contains(line, "BINARY.PEEK") || strings.Contains(line, "<0.5>") {
				t.Fatalf("token %s", fetchKind(line))
			}
		}
	}
	if len(fetches) != 2 || fetches[0] != "* FETCH BINARY[1]" || fetches[1] != "* FETCH BINARY[1]<0>" {
		t.Fatalf("fetch kinds %v", fetches)
	}
}

func TestPreviewProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/preview.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runPreviewScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runPreviewScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("shape diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	var fetches []string
	for _, line := range a.lines {
		if strings.Contains(line, " FETCH (") {
			fetches = append(fetches, fetchKind(line))
		}
	}
	if len(fetches) != 1 || fetches[0] != "* FETCH PREVIEW" {
		t.Fatalf("fetch kinds %v", fetches)
	}
}

func TestFuzzyProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/fuzzy.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runFuzzyScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runFuzzyScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("shape diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	var raw, kind string
	for _, line := range a.lines {
		if strings.HasPrefix(line, "* ESEARCH") {
			raw = line
			kind = esearchKind(line)
		}
	}
	if kind != "* ESEARCH UID COUNT" {
		t.Fatalf("esearch kind %q raw %q", kind, raw)
	}
	if !strings.Contains(raw, "COUNT ") || strings.Contains(kind, "0") || strings.Contains(kind, "1") {
		t.Fatalf("count number leaked into kind %q raw %q", kind, raw)
	}
	if strings.Contains(raw, "RELEVANCY") {
		t.Fatalf("relevancy %q", raw)
	}
}

func TestMultisearchProbeEmptyDiff(t *testing.T) {
	const user, pass = "probe", "probe-secret"
	pers := "personalities/multisearch.json"
	aAddr, aStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer aStop()
	bAddr, bStop, err := startProbeServer(user, pass, pers)
	if err != nil {
		t.Fatal(err)
	}
	defer bStop()
	aHost, aPort, err := splitAddr(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	bHost, bPort, err := splitAddr(bAddr)
	if err != nil {
		t.Fatal(err)
	}
	a, err := runMultisearchScript(aHost, aPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runMultisearchScript(bHost, bPort, "none", user, pass)
	if err != nil {
		t.Fatal(err)
	}
	kinds := normalizeKinds(a.lines, a.mb1, a.mb2)
	diff := scriptDiff(kinds, normalizeKinds(b.lines, b.mb1, b.mb2))
	if diff != "" {
		t.Fatalf("shape diff:\n%s\n--- a ---\n%s", diff, strings.Join(kinds, "\n"))
	}
	var raw, kind string
	n := 0
	for _, line := range a.lines {
		if strings.HasPrefix(line, "* ESEARCH") {
			n++
			raw = line
			kind = esearchKind(line)
		}
	}
	if n != 1 || kind != "* ESEARCH UID ALL" {
		t.Fatalf("esearch kind %q raw %q n %d", kind, raw, n)
	}
	if !strings.Contains(raw, "MAILBOX ") || !strings.Contains(raw, "UIDVALIDITY ") || !strings.Contains(raw, " UID ALL ") {
		t.Fatalf("correlator %q", raw)
	}
	all := raw[strings.Index(raw, " UID ALL ")+len(" UID ALL "):]
	if strings.Contains(all, "*") || strings.Contains(kind, "1") {
		t.Fatalf("set leaked %q kind %q", raw, kind)
	}
}

func splitAddr(addr string) (string, int, error) {
	host, portS, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portS)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}
