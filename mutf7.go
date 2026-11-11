package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf16"
)

// encodeMUTF7 encodes a Unicode mailbox name as IMAP modified UTF-7.
// Printable ASCII other than '&' is copied. '&' is "&-". Everything else
// is one UTF-16BE modified-base64 run, with ',' instead of '/'.
func encodeMUTF7(s string) string {
	var b strings.Builder
	var raw []byte
	flush := func() {
		if len(raw) == 0 {
			return
		}
		enc := base64.StdEncoding.EncodeToString(raw)
		enc = strings.TrimRight(enc, "=")
		enc = strings.ReplaceAll(enc, "/", ",")
		b.WriteByte('&')
		b.WriteString(enc)
		b.WriteByte('-')
		raw = raw[:0]
	}
	for _, r := range s {
		if r >= 0x20 && r <= 0x7e && r != '&' {
			flush()
			b.WriteRune(r)
			continue
		}
		if r == '&' {
			flush()
			b.WriteString("&-")
			continue
		}
		for _, u := range utf16.Encode([]rune{r}) {
			raw = append(raw, byte(u>>8), byte(u))
		}
	}
	flush()
	return b.String()
}

// decodeMUTF7 decodes an IMAP modified UTF-7 mailbox name. A direct octet
// outside printable ASCII is an error, as is a broken shift.
func decodeMUTF7(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			c := s[i]
			if c < 0x20 || c >= 0x7f {
				return "", fmt.Errorf("bad modified utf-7")
			}
			b.WriteByte(c)
			i++
			continue
		}
		rest := s[i+1:]
		j := strings.IndexByte(rest, '-')
		if j < 0 {
			return "", fmt.Errorf("bad modified utf-7")
		}
		if j == 0 {
			b.WriteByte('&')
			i += 2
			continue
		}
		chunk := strings.ReplaceAll(rest[:j], ",", "/")
		switch len(chunk) % 4 {
		case 0:
		case 2:
			chunk += "=="
		case 3:
			chunk += "="
		default:
			return "", fmt.Errorf("bad modified utf-7")
		}
		raw, err := base64.StdEncoding.DecodeString(chunk)
		if err != nil || len(raw) == 0 || len(raw)%2 != 0 {
			return "", fmt.Errorf("bad modified utf-7")
		}
		u := make([]uint16, len(raw)/2)
		for k := range u {
			u[k] = uint16(raw[k*2])<<8 | uint16(raw[k*2+1])
		}
		b.WriteString(string(utf16.Decode(u)))
		i += j + 2
	}
	return b.String(), nil
}
