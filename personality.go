package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Personality is the Cyrus-shaped reply data. A listed capability is
// advertised only when this server implements it.
type Personality struct {
	Greeting             string   `json:"greeting"`
	Delimiter            string   `json:"delimiter"`
	GreetingCaps         []string `json:"greeting_capabilities,omitempty"`
	PreLogin             []string `json:"pre_login_capabilities"`
	PostLogin            []string `json:"post_login_capabilities"`
	OtherUsers           string   `json:"other_users,omitempty"`
	SharedFolders        string   `json:"shared_folders,omitempty"`
	LogoutBye            bool     `json:"logout_bye"`
	FetchItemOrder       []string `json:"fetch_item_order"`
	SelectFlags          string   `json:"select_flags,omitempty"`
	PermanentFlags       string   `json:"permanent_flags,omitempty"`
	SelectOrder          []string `json:"select_order,omitempty"`
	FlagOrder            []string `json:"flag_order,omitempty"`
	FetchPeekBody        bool     `json:"fetch_peek_body,omitempty"`
	FetchSeenFlags       bool     `json:"fetch_seen_flags,omitempty"`
	MoveCopyUIDUntagged  bool     `json:"move_copyuid_untagged,omitempty"`
	SortPartialCountOnly bool     `json:"sort_partial_count_only,omitempty"`
	rank                 map[string]int
}

func loadPersonality(path string) (*Personality, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var p Personality
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("personality %s: %w", path, err)
	}
	if p.Greeting == "" || p.Delimiter == "" {
		return nil, fmt.Errorf("personality %s: greeting and delimiter are required", path)
	}
	if len(p.PreLogin) == 0 || len(p.PostLogin) == 0 || len(p.FetchItemOrder) == 0 {
		return nil, fmt.Errorf("personality %s: capability lists and fetch_item_order are required", path)
	}
	p.rank = make(map[string]int, len(p.FetchItemOrder))
	for i, item := range p.FetchItemOrder {
		p.rank[strings.ToUpper(item)] = i
	}
	return &p, nil
}

// implementedCaps are the capabilities this binary actually speaks.
var implementedCaps = map[string]struct{}{
	"IMAP4REV1":               {},
	"LITERAL+":                {},
	"IDLE":                    {},
	"NAMESPACE":               {},
	"UIDPLUS":                 {},
	"MOVE":                    {},
	"UNSELECT":                {},
	"ENABLE":                  {},
	"UTF8=ACCEPT":             {},
	"CONDSTORE":               {},
	"QRESYNC":                 {},
	"LIST-EXTENDED":           {},
	"CHILDREN":                {},
	"SPECIAL-USE":             {},
	"LIST-STATUS":             {},
	"CREATE-SPECIAL-USE":      {},
	"ESEARCH":                 {},
	"ESORT":                   {},
	"PARTIAL":                 {},
	"SORT":                    {},
	"SORT=DISPLAY":            {},
	"THREAD=REFERENCES":       {},
	"THREAD=ORDEREDSUBJECT":   {},
	"THREAD=REFS":             {},
	"COMPRESS=DEFLATE":        {},
	"SASL-IR":                 {},
	"AUTH=PLAIN":              {},
	"AUTH=LOGIN":              {},
	"AUTH=CRAM-MD5":           {},
	"AUTH=SCRAM-SHA-1":        {},
	"AUTH=SCRAM-SHA-256":      {},
	"AUTH=SCRAM-SHA-1-PLUS":   {},
	"AUTH=SCRAM-SHA-256-PLUS": {},
	"AUTH=EXTERNAL":           {},
	"STARTTLS":                {},
}

// lists reports whether the greeting or pre-login capability list names it.
func (p *Personality) lists(name string) bool {
	if p == nil {
		return false
	}
	for _, c := range p.GreetingCaps {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	for _, c := range p.PreLogin {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}

func capImplemented(name string) bool {
	_, ok := implementedCaps[strings.ToUpper(name)]
	return ok
}

// advertise filters the personality list. STARTTLS is added only on a
// cleartext connection that still has a certificate and has not started TLS.
func (p *Personality) advertise(postLogin, secure, hasCert bool) []string {
	src := p.PreLogin
	if postLogin {
		src = p.PostLogin
	}
	return p.advertiseList(src, postLogin, secure, hasCert)
}

func (p *Personality) advertiseList(src []string, postLogin, secure, hasCert bool) []string {
	out := make([]string, 0, len(src)+1)
	seen := map[string]bool{}
	for _, c := range src {
		if !capImplemented(c) {
			continue
		}
		if strings.EqualFold(c, "STARTTLS") && (secure || !hasCert) {
			continue
		}
		u := strings.ToUpper(c)
		if seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, c)
	}
	if !postLogin && !secure && hasCert && !seen["STARTTLS"] {
		out = append(out, "STARTTLS")
	}
	return out
}

// fetchRank orders items inside one FETCH response. Equal ranks keep
// request order via a stable sort. Body sections share the BODY[] or
// BODY.PEEK[] rank from the personality.
func (p *Personality) fetchRank(item string) int {
	u := strings.ToUpper(item)
	key := u
	switch {
	case strings.HasPrefix(u, "BODY.PEEK"):
		key = "BODY.PEEK[]"
	case strings.HasPrefix(u, "BODY["):
		key = "BODY[]"
	}
	if n, ok := p.rank[key]; ok {
		return n
	}
	return len(p.FetchItemOrder) + 1
}
