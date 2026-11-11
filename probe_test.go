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
		`* OK [CAPABILITY IMAP4rev1 CONDSTORE] deadbeefdead Cyrus ready`,
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
	if len(gaps.Capabilities) != 1 || gaps.Capabilities[0] != "CONDSTORE" {
		t.Fatalf("gaps %+v", gaps)
	}
	if gaps.Commands == nil {
		t.Fatal("commands must be an empty list, not null")
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
