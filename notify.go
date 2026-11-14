package main

import (
	"fmt"
	"strings"
)

// notifyReg is one session's NOTIFY SET. status is the STATUS items pushed
// for a mailbox that is not selected. An omitted list is already filled in.
type notifyReg struct {
	status []string
	groups []notifyGroup
}

type notifyGroup struct {
	kind   string
	names  []string
	none   bool
	events map[string]notifyEvent
}

type notifyEvent struct {
	fetch []string
}

// notifyNote is one change waiting to be pushed. subAt is whether this
// session's account subscribed to name when the change was recorded.
type notifyNote struct {
	kind  string
	name  string
	subAt bool
}

func poke(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (s *session) cmdNotify(cmd command) error {
	if !s.needAuth(cmd.tag) {
		return nil
	}
	if !s.advertiseCap("NOTIFY") {
		return s.bad(cmd.tag, "NOTIFY not supported")
	}
	if len(cmd.args) == 0 || cmd.args[0].Kind != "atom" {
		return s.bad(cmd.tag, "NOTIFY syntax")
	}
	switch strings.ToUpper(cmd.args[0].Val) {
	case "NONE":
		if len(cmd.args) != 1 {
			return s.bad(cmd.tag, "NOTIFY syntax")
		}
		s.srv.store.setNotify(s, nil)
		return s.ok(cmd.tag, "NOTIFY completed")
	case "SET":
		reg, err := parseNotifySet(s, cmd.args[1:])
		if err != nil {
			return s.bad(cmd.tag, "NOTIFY syntax")
		}
		if err := s.prepareNotify(reg); err != nil {
			if err.Error() == "bad status item" || err.Error() == "bad fetch item" || err == errSyntax {
				return s.bad(cmd.tag, "NOTIFY syntax")
			}
			return err
		}
		s.srv.store.setNotify(s, reg)
		return s.ok(cmd.tag, "NOTIFY completed")
	default:
		return s.bad(cmd.tag, "NOTIFY syntax")
	}
}

func (s *session) canonBox(name string) (string, error) {
	n, err := s.boxIn(name)
	if err != nil {
		return "", err
	}
	return s.srv.store.canonName(n)
}

func isNotifyFilter(name string) bool {
	switch strings.ToUpper(name) {
	case "SELECTED", "SELECTED-DELAYED", "INBOXES", "PERSONAL", "SUBSCRIBED", "SUBTREE", "MAILBOXES":
		return true
	default:
		return false
	}
}

func canonNotifyEvent(name string) string {
	switch strings.ToLower(name) {
	case "messagenew":
		return "MessageNew"
	case "messageexpunge":
		return "MessageExpunge"
	case "flagchange":
		return "FlagChange"
	case "mailboxname":
		return "MailboxName"
	case "subscriptionchange":
		return "SubscriptionChange"
	default:
		return ""
	}
}

func parenStartsFilter(args []Token, i int) bool {
	return i+1 < len(args) && args[i].Kind == "lparen" && args[i+1].Kind == "atom" && isNotifyFilter(args[i+1].Val)
}

func parseAtomList(args []Token, i int) ([]string, int, error) {
	if i >= len(args) || args[i].Kind != "lparen" {
		return nil, i, errSyntax
	}
	i++
	var out []string
	for i < len(args) && args[i].Kind != "rparen" {
		if args[i].Kind != "atom" {
			return nil, i, errSyntax
		}
		out = append(out, args[i].Val)
		i++
	}
	if i >= len(args) || args[i].Kind != "rparen" {
		return nil, i, errSyntax
	}
	return out, i + 1, nil
}

// parseFetchList reads a MessageNew fetch list. NOTIFY is split by readTokens,
// which ends an atom at '(' and ')'. FETCH keeps those inside one item via
// readOneFetchItem. Spaces that splitter dropped are put back in the RFC shape:
// a space before '(', none after it, none before ')' or a token that continues
// the section (']', '<'). A ')' at parenthesis depth zero ends the item and
// stays for the list, including when the section is still open. expandFetch
// then accepts or rejects that string the same way FETCH does.
func parseFetchList(args []Token, i int) ([]string, int, error) {
	if i >= len(args) || args[i].Kind != "lparen" {
		return nil, i, errSyntax
	}
	i++
	var items []string
	for i < len(args) && args[i].Kind != "rparen" {
		item, next, err := oneFetchItem(args, i)
		if err != nil || item == "" {
			return nil, i, errSyntax
		}
		items = append(items, item)
		i = next
	}
	if len(items) == 0 || i >= len(args) || args[i].Kind != "rparen" {
		return nil, i, errSyntax
	}
	return items, i + 1, nil
}

func oneFetchItem(args []Token, i int) (string, int, error) {
	var b strings.Builder
	bracket, paren := 0, 0
	for i < len(args) {
		t := args[i]
		if t.Kind == "rparen" && paren == 0 {
			if b.Len() == 0 {
				return "", i, errSyntax
			}
			return b.String(), i, nil
		}
		if b.Len() > 0 && bracket == 0 && paren == 0 {
			return b.String(), i, nil
		}
		var err error
		bracket, paren, err = appendFetchTok(&b, t, bracket, paren)
		if err != nil {
			return "", i, err
		}
		i++
	}
	if b.Len() == 0 || bracket != 0 || paren != 0 {
		return "", i, errSyntax
	}
	return b.String(), i, nil
}

func appendFetchTok(b *strings.Builder, t Token, bracket, paren int) (int, int, error) {
	if b.Len() > 0 {
		s := b.String()
		prev := s[len(s)-1]
		glue := false
		switch {
		case t.Kind == "rparen" || prev == '(':
			glue = true
		case t.Kind == "lparen":
			glue = false
		default:
			val := t.Val
			if t.Kind == "quoted" {
				val = imapString(t.Val)
			}
			if val != "" && (val[0] == ']' || val[0] == '<' || val[0] == '>') {
				glue = true
			}
		}
		if !glue {
			b.WriteByte(' ')
		}
	}
	switch t.Kind {
	case "lparen":
		b.WriteByte('(')
		return bracket, paren + 1, nil
	case "rparen":
		if paren == 0 {
			return bracket, paren, errSyntax
		}
		b.WriteByte(')')
		return bracket, paren - 1, nil
	case "atom", "quoted", "literal":
		text := t.Val
		if t.Kind == "quoted" {
			text = imapString(t.Val)
		} else if t.Kind == "literal" {
			text = fmt.Sprintf("{%d}\r\n%s", len(t.Val), t.Val)
		}
		b.WriteString(text)
		nb, np, err := countFetchDelims(t.Val, bracket, paren)
		return nb, np, err
	default:
		return bracket, paren, errSyntax
	}
}

func countFetchDelims(val string, bracket, paren int) (int, int, error) {
	for i := 0; i < len(val); i++ {
		switch val[i] {
		case '[':
			bracket++
		case ']':
			if bracket == 0 {
				return bracket, paren, errSyntax
			}
			bracket--
		case '(':
			paren++
		case ')':
			if paren == 0 {
				return bracket, paren, errSyntax
			}
			paren--
		}
	}
	return bracket, paren, nil
}

func parseNotifySet(s *session, args []Token) (*notifyReg, error) {
	reg := &notifyReg{}
	i := 0
	if i < len(args) && args[i].Kind == "lparen" && !parenStartsFilter(args, i) {
		items, next, err := parseAtomList(args, i)
		if err != nil {
			return nil, err
		}
		reg.status = items
		i = next
	} else {
		reg.status = []string{"MESSAGES", "UIDNEXT", "UNSEEN"}
	}
	if i >= len(args) {
		return nil, errSyntax
	}
	for i < len(args) {
		g, next, err := parseNotifyGroup(s, args, i)
		if err != nil {
			return nil, err
		}
		if !notifyEventsOK(g) {
			return nil, errSyntax
		}
		reg.groups = append(reg.groups, g)
		i = next
	}
	if len(reg.groups) == 0 {
		return nil, errSyntax
	}
	return reg, nil
}

func parseNotifyGroup(s *session, args []Token, i int) (notifyGroup, int, error) {
	var g notifyGroup
	if i >= len(args) || args[i].Kind != "lparen" {
		return g, i, errSyntax
	}
	i++
	if i >= len(args) || args[i].Kind != "atom" || !isNotifyFilter(args[i].Val) {
		return g, i, errSyntax
	}
	g.kind = strings.ToLower(args[i].Val)
	i++
	switch g.kind {
	case "subtree":
		if i >= len(args) {
			return g, i, errSyntax
		}
		name, ok := astring(args[i])
		if !ok || name == "" {
			return g, i, errSyntax
		}
		cn, err := s.canonBox(name)
		if err != nil {
			return g, i, errSyntax
		}
		g.names = []string{cn}
		i++
	case "mailboxes":
		if i >= len(args) || args[i].Kind != "lparen" {
			return g, i, errSyntax
		}
		i++
		for i < len(args) && args[i].Kind != "rparen" {
			name, ok := astring(args[i])
			if !ok || name == "" {
				return g, i, errSyntax
			}
			cn, err := s.canonBox(name)
			if err != nil {
				return g, i, errSyntax
			}
			g.names = append(g.names, cn)
			i++
		}
		if len(g.names) == 0 || i >= len(args) || args[i].Kind != "rparen" {
			return g, i, errSyntax
		}
		i++
	}
	if i >= len(args) {
		return g, i, errSyntax
	}
	if args[i].Kind == "atom" && strings.EqualFold(args[i].Val, "NONE") {
		g.none = true
		i++
	} else if args[i].Kind == "lparen" {
		ev, next, err := parseNotifyEvents(args, i)
		if err != nil {
			return g, i, err
		}
		g.events = ev
		i = next
	} else {
		return g, i, errSyntax
	}
	if i >= len(args) || args[i].Kind != "rparen" {
		return g, i, errSyntax
	}
	return g, i + 1, nil
}

func parseNotifyEvents(args []Token, i int) (map[string]notifyEvent, int, error) {
	if i >= len(args) || args[i].Kind != "lparen" {
		return nil, i, errSyntax
	}
	i++
	ev := map[string]notifyEvent{}
	for i < len(args) && args[i].Kind != "rparen" {
		if args[i].Kind != "atom" {
			return nil, i, errSyntax
		}
		name := canonNotifyEvent(args[i].Val)
		if name == "" {
			return nil, i, errSyntax
		}
		if _, ok := ev[name]; ok {
			return nil, i, errSyntax
		}
		i++
		var fetch []string
		if name == "MessageNew" && i < len(args) && args[i].Kind == "lparen" {
			items, next, err := parseFetchList(args, i)
			if err != nil || len(items) == 0 {
				return nil, i, errSyntax
			}
			fetch = items
			i = next
		}
		ev[name] = notifyEvent{fetch: fetch}
	}
	if i >= len(args) || args[i].Kind != "rparen" {
		return nil, i, errSyntax
	}
	return ev, i + 1, nil
}

func notifyEventsOK(g notifyGroup) bool {
	if g.none {
		return len(g.events) == 0
	}
	_, hasNew := g.events["MessageNew"]
	_, hasExp := g.events["MessageExpunge"]
	if hasNew != hasExp {
		return false
	}
	if _, hasFlag := g.events["FlagChange"]; hasFlag && !hasNew {
		return false
	}
	if ev, ok := g.events["MessageNew"]; ok && len(ev.fetch) > 0 {
		if g.kind != "selected" && g.kind != "selected-delayed" {
			return false
		}
	}
	return true
}

// prepareNotify checks status items and fetch items the way STATUS and FETCH do.
func (s *session) prepareNotify(reg *notifyReg) error {
	if _, err := statusPairs(nil, reg.status, s.appendLimitValue(), s.advertiseCap("STATUS=SIZE"), s.rfcObject(), s.plusObject(), s.user, s.advertiseCap("X-CREATEDMODSEQ"), s.on("IMAP4REV2")); err != nil {
		return err
	}
	modseq := false
	for i := range reg.groups {
		g := &reg.groups[i]
		ev, ok := g.events["MessageNew"]
		if !ok || len(ev.fetch) == 0 {
			continue
		}
		expanded, err := expandFetch(ev.fetch, false, s.advertiseCap("BINARY") || s.on("IMAP4REV2"), s.advertiseCap("PREVIEW") || s.advertiseCap("SNIPPET=FUZZY"), s.advertiseCap("SAVEDATE"), s.rfcObject(), s.plusObject(), s.advertiseCap("X-CREATEDMODSEQ"))
		if err != nil {
			return err
		}
		if err := binarySpecsOK(expanded); err != nil {
			return err
		}
		if itemListed(expanded, "MODSEQ") {
			if !s.advertiseCap("CONDSTORE") {
				return fmt.Errorf("bad fetch item")
			}
			modseq = true
		}
		ev.fetch = expanded
		g.events["MessageNew"] = ev
	}
	if !modseq {
		return nil
	}
	if s.srv.store.selected(s) {
		return s.announceCondstore("NOTIFY")
	}
	if !s.on("CONDSTORE") {
		if s.enabled == nil {
			s.enabled = map[string]struct{}{}
		}
		s.enabled["CONDSTORE"] = struct{}{}
	}
	return nil
}

func (st *Store) groupMatch(s *session, g notifyGroup, name string, subAt bool) bool {
	switch g.kind {
	case "selected", "selected-delayed":
		return s.box != nil && s.box.name == name
	case "inboxes":
		return name == "INBOX"
	case "personal":
		return true
	case "subscribed":
		return subAt
	case "subtree":
		if len(g.names) != 1 {
			return false
		}
		root := g.names[0]
		if name == root {
			return true
		}
		return strings.HasPrefix(name, root+st.delim)
	case "mailboxes":
		for _, n := range g.names {
			if n == name {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (st *Store) firstGroup(s *session, name string, subAt bool) (notifyGroup, bool) {
	if s.notify == nil {
		return notifyGroup{}, false
	}
	for _, g := range s.notify.groups {
		if st.groupMatch(s, g, name, subAt) {
			return g, true
		}
	}
	return notifyGroup{}, false
}

// selectedNewItems is the MessageNew fetch list when the first group that
// matches the selected mailbox asks for one.
func (st *Store) selectedNewItems(s *session) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.notify == nil || s.box == nil {
		return nil
	}
	g, ok := st.firstGroup(s, s.box.name, st.subAt(s, s.box.name))
	if !ok || g.none {
		return nil
	}
	ev, ok := g.events["MessageNew"]
	if !ok || len(ev.fetch) == 0 {
		return nil
	}
	return append([]string(nil), ev.fetch...)
}

func (st *Store) listFlagsLocked(user string, pers *Personality, name string, showSpecial bool) (string, bool) {
	res, err := st.resolveName(user, pers, name)
	if err != nil || res.virtual || res.sl == nil {
		return "", false
	}
	mb := res.sl.boxes[res.key]
	if mb == nil {
		return "", false
	}
	child := `\HasNoChildren`
	pref := name + st.delim
	for _, other := range st.order {
		if strings.HasPrefix(other, pref) {
			child = `\HasChildren`
			break
		}
	}
	var spec []string
	if showSpecial {
		spec = mb.special
	}
	return formatListFlags(child, spec), true
}

func (s *session) sendNewFetch(uids []uint32, items []string) error {
	snaps, show, _, _, err := s.srv.store.fetch(s, true, false, seqFromIDs(uids), items, nil, 0)
	if err != nil {
		if err.Error() == "not selected" {
			return nil
		}
		return err
	}
	if itemListed(show, "OBJECTID") {
		if err := s.activateObjectPlus(); err != nil {
			return err
		}
	}
	// A non-peek body sets \Seen inside fetch. FetchSeenFlags puts that on
	// this same response. The selected mailbox does not flush the flags note.
	for _, sn := range snaps {
		use := show
		if s.pers.FetchSeenFlags && sn.seenEvent != 0 && !itemListed(show, "FLAGS") {
			use = append([]string{"FLAGS"}, show...)
		}
		if err := s.sendRaw([]byte(s.formatFetch(sn, use))); err != nil {
			return err
		}
	}
	seenUID := s.on("QRESYNC")
	if s.on("UIDONLY") {
		seenUID = s.on("QRESYNC") || itemListed(items, "UID")
	}
	if !s.pers.FetchSeenFlags && !itemListed(show, "FLAGS") {
		var last uint64
		for _, sn := range snaps {
			if sn.seenEvent == 0 {
				continue
			}
			if err := s.sendLine(s.flagsFetch(sn.seq, sn.flags, sn.uid, seenUID, sn.modseq, s.on("CONDSTORE"))); err != nil {
				return err
			}
			if sn.seenEvent > last {
				last = sn.seenEvent
			}
		}
		if last > 0 {
			s.srv.store.advanceSeen(s, last)
		}
	}
	return nil
}

func (s *session) flushNotify() error {
	for _, line := range s.srv.store.notifyLines(s) {
		if err := s.sendLine(line); err != nil {
			return err
		}
	}
	return nil
}

// notifyLines drains this session's NOTIFY queue. Message changes in the
// selected mailbox are left to flush. Two groups do not produce two lines:
// the first match is the only one consulted.
func (st *Store) notifyLines(s *session) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	notes := s.notifyQ
	s.notifyQ = nil
	if s.notify == nil || len(notes) == 0 {
		return nil
	}
	showSpecial := s.advertiseCap("SPECIAL-USE")
	limit := s.appendLimitValue()
	withSize := s.advertiseCap("STATUS=SIZE")
	var lines []string
	statusSent := map[string]bool{}
	for _, n := range notes {
		g, ok := st.firstGroup(s, n.name, n.subAt)
		if !ok || g.none {
			if n.kind == "new" || n.kind == "expunge" || n.kind == "flags" {
				statusSent[n.name] = true
			}
			continue
		}
		switch n.kind {
		case "new", "expunge", "flags":
			if s.box != nil && s.box.name == n.name {
				continue
			}
			if statusSent[n.name] {
				continue
			}
			want := false
			switch n.kind {
			case "new":
				_, want = g.events["MessageNew"]
			case "expunge":
				_, want = g.events["MessageExpunge"]
			case "flags":
				_, want = g.events["FlagChange"]
			}
			if !want {
				continue
			}
			statusSent[n.name] = true
			res, rerr := st.resolveName(s.user, s.pers, n.name)
			if rerr != nil || res.virtual || res.sl == nil || res.sl.boxes[res.key] == nil {
				continue
			}
			mb := res.sl.boxes[res.key]
			pairs, err := statusPairs(mb, s.notify.status, limit, withSize, s.rfcObject(), s.plusObject(), s.user, s.advertiseCap("X-CREATEDMODSEQ"), s.on("IMAP4REV2"))
			if err != nil || len(pairs) == 0 {
				continue
			}
			lines = append(lines, formatStatus(s.boxOut(n.name), pairs))
		case "create", "rename-new":
			if _, has := g.events["MailboxName"]; !has {
				continue
			}
			flags, ok := st.listFlagsLocked(s.user, s.pers, n.name, showSpecial)
			if !ok {
				continue
			}
			lines = append(lines, listNote(flags, st.delim, s.boxOut(n.name)))
		case "delete", "rename-old":
			if _, has := g.events["MailboxName"]; !has {
				continue
			}
			lines = append(lines, listNote(`(\NonExistent)`, st.delim, s.boxOut(n.name)))
		case "sub", "unsub":
			if _, has := g.events["SubscriptionChange"]; !has {
				continue
			}
			flags, ok := st.listFlagsLocked(s.user, s.pers, n.name, showSpecial)
			if !ok {
				continue
			}
			lines = append(lines, listNote(flags, st.delim, s.boxOut(n.name)))
		}
	}
	return lines
}

func listNote(flags, delim, name string) string {
	return fmt.Sprintf("* LIST %s %s %s", flags, imapString(delim), mailboxToken(name, false))
}
