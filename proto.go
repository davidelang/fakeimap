package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Token is one IMAP command token after the tag and verb.
type Token struct {
	Kind string // atom, quoted, literal, lparen, rparen
	Val  string
}

type proto struct {
	br   *bufio.Reader
	send func(line string) error
}

func (p *proto) skipSP() error {
	for {
		c, err := p.br.ReadByte()
		if err != nil {
			return err
		}
		if c == ' ' || c == '\t' {
			continue
		}
		return p.br.UnreadByte()
	}
}

func (p *proto) expectCRLF() error {
	c, err := p.br.ReadByte()
	if err != nil {
		return err
	}
	if c == '\n' {
		return nil
	}
	if c != '\r' {
		return fmt.Errorf("expected CRLF")
	}
	lf, err := p.br.ReadByte()
	if err != nil {
		return err
	}
	if lf != '\n' {
		return fmt.Errorf("expected CRLF")
	}
	return nil
}

func atomEnd(c byte) bool {
	return c == ' ' || c == '\t' || c == '(' || c == ')' || c == '{' || c == '"' || c == '\r' || c == '\n'
}

func (p *proto) readAtom() (string, error) {
	var b []byte
	for {
		c, err := p.br.ReadByte()
		if err != nil {
			return "", err
		}
		if atomEnd(c) {
			if err := p.br.UnreadByte(); err != nil {
				return "", err
			}
			break
		}
		b = append(b, c)
	}
	if len(b) == 0 {
		return "", fmt.Errorf("expected atom")
	}
	return string(b), nil
}

func (p *proto) readQuoted() (string, error) {
	var b []byte
	for {
		c, err := p.br.ReadByte()
		if err != nil {
			return "", err
		}
		switch c {
		case '"':
			return string(b), nil
		case '\\':
			n, err := p.br.ReadByte()
			if err != nil {
				return "", err
			}
			b = append(b, n)
		case '\r', '\n':
			return "", fmt.Errorf("newline in quoted string")
		default:
			b = append(b, c)
		}
	}
}

func (p *proto) readLiteral() (string, error) {
	var num []byte
	plus := false
	for {
		c, err := p.br.ReadByte()
		if err != nil {
			return "", err
		}
		switch {
		case c >= '0' && c <= '9':
			num = append(num, c)
		case c == '+':
			if plus || len(num) == 0 {
				return "", fmt.Errorf("bad literal")
			}
			plus = true
		case c == '}':
			if len(num) == 0 {
				return "", fmt.Errorf("bad literal")
			}
			n, err := strconv.Atoi(string(num))
			if err != nil || n < 0 || n > 25*1024*1024 {
				return "", fmt.Errorf("bad literal size")
			}
			if err := p.expectCRLF(); err != nil {
				return "", err
			}
			if !plus {
				if err := p.send("+ go ahead"); err != nil {
					return "", err
				}
			}
			buf := make([]byte, n)
			if _, err := io.ReadFull(p.br, buf); err != nil {
				return "", err
			}
			return string(buf), nil
		default:
			return "", fmt.Errorf("bad literal")
		}
	}
}

func (p *proto) readToken() (Token, error) {
	c, err := p.br.ReadByte()
	if err != nil {
		return Token{}, err
	}
	switch c {
	case '(':
		return Token{Kind: "lparen", Val: "("}, nil
	case ')':
		return Token{Kind: "rparen", Val: ")"}, nil
	case '"':
		s, err := p.readQuoted()
		if err != nil {
			return Token{}, err
		}
		return Token{Kind: "quoted", Val: s}, nil
	case '{':
		s, err := p.readLiteral()
		if err != nil {
			return Token{}, err
		}
		return Token{Kind: "literal", Val: s}, nil
	default:
		if err := p.br.UnreadByte(); err != nil {
			return Token{}, err
		}
		s, err := p.readAtom()
		if err != nil {
			return Token{}, err
		}
		return Token{Kind: "atom", Val: s}, nil
	}
}

func (p *proto) readTokens() ([]Token, error) {
	var toks []Token
	for {
		if err := p.skipSP(); err != nil {
			return nil, err
		}
		c, err := p.br.ReadByte()
		if err != nil {
			return nil, err
		}
		if c == '\r' {
			lf, err := p.br.ReadByte()
			if err != nil {
				return nil, err
			}
			if lf != '\n' {
				return nil, fmt.Errorf("expected CRLF")
			}
			return toks, nil
		}
		if c == '\n' {
			return toks, nil
		}
		if err := p.br.UnreadByte(); err != nil {
			return nil, err
		}
		tok, err := p.readToken()
		if err != nil {
			return nil, err
		}
		toks = append(toks, tok)
	}
}

// readFetchItems reads one fetch attribute or a parenthesized list,
// including section brackets and an optional <partial>. It consumes CRLF.
func (p *proto) readFetchItems() ([]string, error) {
	if err := p.skipSP(); err != nil {
		return nil, err
	}
	c, err := p.br.ReadByte()
	if err != nil {
		return nil, err
	}
	if c != '(' {
		if err := p.br.UnreadByte(); err != nil {
			return nil, err
		}
		item, err := p.readOneFetchItem()
		if err != nil {
			return nil, err
		}
		return []string{item}, nil
	}
	var items []string
	for {
		if err := p.skipSP(); err != nil {
			return nil, err
		}
		c, err := p.br.ReadByte()
		if err != nil {
			return nil, err
		}
		if c == ')' {
			break
		}
		if err := p.br.UnreadByte(); err != nil {
			return nil, err
		}
		item, err := p.readOneFetchItem()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("empty fetch list")
	}
	return items, nil
}

// finishFetch reads an optional CHANGEDSINCE modifier list and the CRLF.
func (p *proto) finishFetch(items []string) ([]string, *uint64, error) {
	if err := p.skipSP(); err != nil {
		return nil, nil, err
	}
	c, err := p.br.ReadByte()
	if err != nil {
		return nil, nil, err
	}
	var since *uint64
	if c == '(' {
		since, err = p.readChangedSince()
		if err != nil {
			return nil, nil, err
		}
		if err := p.expectCRLF(); err != nil {
			return nil, nil, err
		}
		return items, since, nil
	}
	if err := p.br.UnreadByte(); err != nil {
		return nil, nil, err
	}
	if err := p.expectCRLF(); err != nil {
		return nil, nil, err
	}
	return items, nil, nil
}

func (p *proto) readChangedSince() (*uint64, error) {
	var since *uint64
	for {
		if err := p.skipSP(); err != nil {
			return nil, err
		}
		c, err := p.br.ReadByte()
		if err != nil {
			return nil, err
		}
		if c == ')' {
			return since, nil
		}
		if err := p.br.UnreadByte(); err != nil {
			return nil, err
		}
		name, err := p.readAtom()
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(name, "CHANGEDSINCE") || since != nil {
			return nil, fmt.Errorf("bad fetch modifier")
		}
		if err := p.skipSP(); err != nil {
			return nil, err
		}
		num, err := p.readAtom()
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseUint(num, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad fetch modifier")
		}
		since = &n
	}
}

func (p *proto) finishLine() error {
	if err := p.skipSP(); err != nil {
		return err
	}
	return p.expectCRLF()
}

func (p *proto) readOneFetchItem() (string, error) {
	var buf []byte
	bracket := 0
	paren := 0
	for {
		c, err := p.br.ReadByte()
		if err != nil {
			return "", err
		}
		switch c {
		case '[':
			bracket++
			buf = append(buf, c)
		case ']':
			if bracket == 0 {
				return "", fmt.Errorf("bad fetch item")
			}
			bracket--
			buf = append(buf, c)
			if bracket == 0 {
				pk, err := p.br.ReadByte()
				if err != nil {
					return "", err
				}
				if pk == '<' {
					buf = append(buf, pk)
					for {
						d, err := p.br.ReadByte()
						if err != nil {
							return "", err
						}
						buf = append(buf, d)
						if d == '>' {
							break
						}
					}
				} else if err := p.br.UnreadByte(); err != nil {
					return "", err
				}
			}
		case '(':
			paren++
			buf = append(buf, c)
		case ')':
			if paren > 0 {
				paren--
				buf = append(buf, c)
				continue
			}
			if err := p.br.UnreadByte(); err != nil {
				return "", err
			}
			if len(buf) == 0 {
				return "", fmt.Errorf("bad fetch item")
			}
			return string(buf), nil
		case ' ', '\t':
			if bracket == 0 && paren == 0 {
				if err := p.br.UnreadByte(); err != nil {
					return "", err
				}
				if len(buf) == 0 {
					return "", fmt.Errorf("bad fetch item")
				}
				return string(buf), nil
			}
			buf = append(buf, c)
		case '\r', '\n':
			if err := p.br.UnreadByte(); err != nil {
				return "", err
			}
			if len(buf) == 0 || bracket != 0 || paren != 0 {
				return "", fmt.Errorf("bad fetch item")
			}
			return string(buf), nil
		default:
			buf = append(buf, c)
		}
	}
}

func astring(t Token) (string, bool) {
	switch t.Kind {
	case "atom", "quoted", "literal":
		return t.Val, true
	default:
		return "", false
	}
}

func joinCmd(tag, name string, uid bool, args []Token, fetch []string) string {
	var b strings.Builder
	b.WriteString(tag)
	b.WriteByte(' ')
	if uid {
		b.WriteString("UID ")
	}
	b.WriteString(name)
	for _, t := range args {
		b.WriteByte(' ')
		switch t.Kind {
		case "quoted":
			b.WriteString(imapString(t.Val))
		case "literal":
			fmt.Fprintf(&b, "{%d}\r\n%s", len(t.Val), t.Val)
		default:
			b.WriteString(t.Val)
		}
	}
	if len(fetch) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(fetch, " "))
		b.WriteByte(')')
	}
	return b.String()
}
