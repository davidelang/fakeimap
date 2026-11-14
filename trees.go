package main

import (
	"fmt"
	"strings"
	"time"
)

// boxTree is one account's mailboxes, or the single shared tree.
// The first dataset account keeps using Store.boxes and Store.order.
type boxTree struct {
	boxes map[string]*Mailbox
	order []string
}

// treeSlot is a view of one tree. commit writes a replaced map or order
// back. In-place map writes are visible without commit.
type treeSlot struct {
	id     string
	boxes  map[string]*Mailbox
	order  []string
	commit func()
}

func (sl *treeSlot) save() {
	if sl != nil && sl.commit != nil {
		sl.commit()
	}
}

func (st *Store) primaryUser() string {
	if len(st.acctOrder) > 0 {
		return st.acctOrder[0]
	}
	for u := range st.accounts {
		return u
	}
	return ""
}

func (st *Store) slotPrimary() *treeSlot {
	sl := &treeSlot{id: "primary", boxes: st.boxes, order: append([]string(nil), st.order...)}
	sl.commit = func() {
		st.boxes = sl.boxes
		st.order = sl.order
	}
	return sl
}

func (st *Store) ensureTree(user string) *boxTree {
	if st.trees == nil {
		st.trees = map[string]*boxTree{}
	}
	t := st.trees[user]
	if t == nil {
		t = &boxTree{boxes: map[string]*Mailbox{}}
		st.trees[user] = t
	}
	if t.boxes == nil {
		t.boxes = map[string]*Mailbox{}
	}
	return t
}

func (st *Store) slotUser(user string) *treeSlot {
	if user == "" || user == st.primaryUser() {
		return st.slotPrimary()
	}
	t := st.ensureTree(user)
	sl := &treeSlot{id: "user:" + user, boxes: t.boxes, order: append([]string(nil), t.order...)}
	sl.commit = func() {
		t.boxes = sl.boxes
		t.order = sl.order
	}
	return sl
}

func (st *Store) slotShared() *treeSlot {
	if st.shared == nil {
		st.shared = &boxTree{boxes: map[string]*Mailbox{}}
	}
	if st.shared.boxes == nil {
		st.shared.boxes = map[string]*Mailbox{}
	}
	t := st.shared
	sl := &treeSlot{id: "shared", boxes: t.boxes, order: append([]string(nil), t.order...)}
	sl.commit = func() {
		t.boxes = sl.boxes
		t.order = sl.order
	}
	return sl
}

// walkPersonal calls fn for each mailbox in the account's own tree.
// Caller holds st.mu. user empty is the first dataset account.
func (st *Store) walkPersonal(user string, fn func(name string, mb *Mailbox)) {
	sl := st.slotUser(user)
	for _, name := range sl.order {
		mb := sl.boxes[name]
		if mb != nil {
			fn(name, mb)
		}
	}
}

// walkAll calls fn for every personal tree and the shared tree.
// Caller holds st.mu.
func (st *Store) walkAll(fn func(name string, mb *Mailbox)) {
	seen := map[*boxTree]bool{}
	one := func(sl *treeSlot) {
		for _, name := range sl.order {
			if mb := sl.boxes[name]; mb != nil {
				fn(name, mb)
			}
		}
	}
	one(st.slotPrimary())
	for _, u := range st.acctOrder {
		if u == st.primaryUser() {
			continue
		}
		t := st.trees[u]
		if t == nil || seen[t] {
			continue
		}
		seen[t] = true
		one(&treeSlot{boxes: t.boxes, order: t.order})
	}
	if st.shared != nil {
		one(&treeSlot{boxes: st.shared.boxes, order: st.shared.order})
	}
}

func (st *Store) ownerOf(mb *Mailbox) string {
	if mb == nil {
		return st.primaryUser()
	}
	if mb.owner != "" {
		return mb.owner
	}
	if mb.shared {
		return ""
	}
	return st.primaryUser()
}

func (st *Store) walkOwner(mb *Mailbox, fn func(name string, box *Mailbox)) {
	if mb != nil && mb.shared {
		if st.shared != nil {
			for _, name := range st.shared.order {
				if b := st.shared.boxes[name]; b != nil {
					fn(name, b)
				}
			}
		}
		return
	}
	st.walkPersonal(st.ownerOf(mb), fn)
}

type resolved struct {
	sl      *treeSlot
	key     string
	wire    string
	virtual bool
	other   bool
	shared  bool
}

func otherPrefixes(p *Personality) []string {
	if p == nil {
		return nil
	}
	var out []string
	add := func(s string) {
		switch s {
		case "Other Users/", "user.", "user/", "~":
			for _, e := range out {
				if e == s {
					return
				}
			}
			out = append(out, s)
		}
	}
	add(p.OtherUsers)
	for _, ns := range p.OtherNS {
		add(ns.Prefix)
	}
	return out
}

func prefixDelim(p *Personality, pref string) string {
	if p == nil {
		return "/"
	}
	for _, ns := range p.OtherNS {
		if ns.Prefix == pref && ns.Delimiter != nil && *ns.Delimiter != "" {
			return *ns.Delimiter
		}
	}
	for _, ns := range p.SharedNS {
		if ns.Prefix == pref && ns.Delimiter != nil && *ns.Delimiter != "" {
			return *ns.Delimiter
		}
	}
	if p.Delimiter != "" {
		return p.Delimiter
	}
	return "/"
}

func sharedPrefixes(p *Personality) []string {
	if p == nil {
		return nil
	}
	var out []string
	add := func(s string) {
		if s == "" {
			return
		}
		for _, e := range out {
			if e == s {
				return
			}
		}
		out = append(out, s)
	}
	if p.SharedFolders != nil {
		add(*p.SharedFolders)
	}
	for _, ns := range p.SharedNS {
		add(ns.Prefix)
	}
	return out
}

func splitOther(p *Personality, name string) (pref, acct, rest, delim string, ok bool) {
	for _, pref = range otherPrefixes(p) {
		if !strings.HasPrefix(name, pref) {
			continue
		}
		delim = prefixDelim(p, pref)
		tail := name[len(pref):]
		if tail == "" {
			return pref, "", "", delim, true
		}
		acct = tail
		if i := strings.Index(tail, delim); i >= 0 {
			acct = tail[:i]
			rest = tail[i+len(delim):]
		}
		return pref, acct, rest, delim, true
	}
	return "", "", "", "", false
}

func matchShared(p *Personality, name string) (string, bool) {
	best := ""
	for _, pref := range sharedPrefixes(p) {
		if name == pref || strings.HasPrefix(name, pref) {
			if len(pref) > len(best) {
				best = pref
			}
		}
	}
	return best, best != ""
}

// resolveName maps a client mailbox name into a tree. A noselect other-user
// node is virtual. Caller holds st.mu.
func (st *Store) resolveName(user string, pers *Personality, name string) (resolved, error) {
	if pref, acct, rest, delim, ok := splitOther(pers, name); ok {
		if acct == "" || st.accounts[acct] == "" {
			return resolved{}, fmt.Errorf("no mailbox")
		}
		node := pref + acct
		if rest == "" {
			return resolved{wire: node, virtual: true, other: true}, nil
		}
		local, err := st.canon(rest)
		if err != nil {
			return resolved{}, err
		}
		wire := node + delim + local
		return resolved{sl: st.slotUser(acct), key: local, wire: wire, other: true}, nil
	}
	if _, ok := matchShared(pers, name); ok {
		if name == "" || strings.Contains(name, "\r") || strings.Contains(name, "\n") {
			return resolved{}, fmt.Errorf("bad mailbox name")
		}
		return resolved{sl: st.slotShared(), key: name, wire: name, shared: true}, nil
	}
	cn, err := st.canon(name)
	if err != nil {
		return resolved{}, err
	}
	u := user
	if u == "" {
		u = st.primaryUser()
	}
	return resolved{sl: st.slotUser(u), key: cn, wire: cn}, nil
}

func implyParents(p *Personality) (bool, string) {
	if p == nil {
		return false, ""
	}
	if strings.Contains(p.Greeting, "Courier-IMAP") {
		return true, `\Noselect`
	}
	if strings.Contains(p.Greeting, "2007e.404") {
		return true, `\NoSelect`
	}
	if p.listsCap("NOTIFY") {
		return true, `\Noselect`
	}
	return false, ""
}

func ancestorNames(name, delim string) []string {
	var out []string
	for {
		i := strings.LastIndex(name, delim)
		if i <= 0 {
			break
		}
		name = name[:i]
		out = append(out, name)
	}
	return out
}

func (st *Store) ensureInbox(user string) {
	sl := st.slotUser(user)
	if sl.boxes["INBOX"] == nil {
		mb := &Mailbox{
			name: "INBOX", uidValidity: st.allocUIDV(), uidNext: 1,
			st: st, owner: user, urlGen: 1,
		}
		st.stampBox(mb)
		sl.boxes["INBOX"] = mb
		sl.order = append(sl.order, "INBOX")
	}
	sl.save()
	st.ensureSub(user)
	if st.subs[user]["INBOX"] == false {
		st.subs[user]["INBOX"] = true
	}
}

func (st *Store) applySubs(user string, names []string) {
	st.ensureSub(user)
	if names == nil {
		if len(st.subs[user]) == 0 {
			st.subs[user]["INBOX"] = true
		}
		return
	}
	set := map[string]bool{}
	for _, n := range names {
		cn, err := st.canon(n)
		if err != nil {
			cn = n
		}
		set[cn] = true
	}
	set["INBOX"] = true
	st.subs[user] = set
}

// putBox stores a mailbox in the account's tree. Caller holds no lock
// during load. Duplicate names in one tree are an error.
func (st *Store) putBox(user string, mb *Mailbox) error {
	if user == "" || user == st.primaryUser() {
		if st.boxes[mb.name] != nil {
			return fmt.Errorf("duplicate mailbox %s", mb.name)
		}
		mb.owner = st.primaryUser()
		st.boxes[mb.name] = mb
		st.order = append(st.order, mb.name)
		return nil
	}
	sl := st.slotUser(user)
	if sl.boxes[mb.name] != nil {
		return fmt.Errorf("duplicate mailbox %s", mb.name)
	}
	mb.owner = user
	sl.boxes[mb.name] = mb
	sl.order = append(sl.order, mb.name)
	sl.save()
	return nil
}

func (st *Store) specialIn(boxes map[string]*Mailbox, flag string) bool {
	for _, mb := range boxes {
		for _, f := range mb.special {
			if f == flag {
				return true
			}
		}
	}
	return false
}

type injectReq struct {
	user    string
	pers    *Personality
	mailbox string
	raw     []byte
	flags   []string
	date    *time.Time
	seq     *int
	uidv    *uint32
	frozen  *time.Time
}

// injectMsg appends, or renumbers when seq and uidv are both set.
// renumber is true when UIDVALIDITY changed. Caller holds no lock.
func (st *Store) injectMsg(r injectReq) (uint32, *Mailbox, bool, error) {
	if r.seq != nil && r.uidv == nil {
		return 0, nil, false, fmt.Errorf("missing field")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if r.seq == nil {
		uv, m, err := st.appendLocked(r.user, r.pers, r.mailbox, r.flags, r.date, r.raw, r.frozen)
		if err != nil {
			return 0, nil, false, err
		}
		_ = uv
		res, err := st.resolveName(r.user, r.pers, r.mailbox)
		if err != nil {
			return m.uid, nil, false, nil
		}
		return m.uid, res.sl.boxes[res.key], false, nil
	}
	res, err := st.resolveName(r.user, r.pers, r.mailbox)
	if err != nil || res.virtual || res.sl == nil || res.sl.boxes[res.key] == nil {
		return 0, nil, false, fmt.Errorf("no mailbox")
	}
	mb := res.sl.boxes[res.key]
	seq := *r.seq
	if seq < 1 || seq > len(mb.msgs)+1 {
		return 0, nil, false, fmt.Errorf("missing field")
	}
	for _, f := range r.flags {
		if _, ok := canonFlag(f); !ok {
			return 0, nil, false, fmt.Errorf("bad flag")
		}
	}
	raw := toCRLF(r.raw)
	now := saveClock(r.frozen)
	internal := now
	if r.date != nil {
		internal = *r.date
	}
	nm := &Message{
		flags:    map[string]struct{}{},
		recent:   true,
		internal: internal,
		saved:    now,
		raw:      raw,
		size:     len(raw),
	}
	for _, f := range r.flags {
		nm.addFlag(f)
	}
	indexOverlay(nm)
	st.stampMsg(nm)
	st.stampThread(nm, mb)
	msgs := make([]*Message, 0, len(mb.msgs)+1)
	msgs = append(msgs, mb.msgs[:seq-1]...)
	msgs = append(msgs, nm)
	msgs = append(msgs, mb.msgs[seq-1:]...)
	mb.uidValidity = st.noteUIDV(*r.uidv)
	for i, m := range msgs {
		m.uid = uint32(i + 1)
	}
	mb.msgs = msgs
	mb.uidNext = uint32(len(msgs) + 1)
	mb.bump(nm)
	mb.push(mbEvent{kind: "exists", exists: len(mb.msgs), recent: mb.recentCount(), newUIDs: []uint32{nm.uid}})
	st.noteMail("created", nm.jmapID)
	return nm.uid, mb, true, nil
}

func childFlag(order []string, name, delim string) string {
	pref := name + delim
	for _, other := range order {
		if strings.HasPrefix(other, pref) {
			return `\HasChildren`
		}
	}
	return `\HasNoChildren`
}

func (st *Store) listExtras(user string, pers *Personality, full string, q listQuery, have map[string]bool) []listEntry {
	if pers == nil || q.subscribedOnly && len(st.subs[user]) == 0 {
		// still allow implied parents on a normal list
	}
	var out []listEntry
	add := func(flags, name, id string, status [][2]string) {
		if have[name] || !wildMatch(name, full, st.delim) {
			return
		}
		if q.subscribedOnly && (st.subs[user] == nil || !st.subs[user][name]) {
			return
		}
		have[name] = true
		flagText := flags
		if q.markSubscribed {
			flagText = strings.TrimSuffix(flagText, ")") + ` \Subscribed)`
		}
		out = append(out, listEntry{flags: flagText, name: name, status: status, mailboxID: id})
	}
	if ok, nose := implyParents(pers); ok && !q.subscribedOnly {
		sl := st.slotUser(user)
		seen := map[string]bool{}
		for _, name := range sl.order {
			for _, anc := range ancestorNames(name, st.delim) {
				if sl.boxes[anc] != nil || seen[anc] {
					continue
				}
				seen[anc] = true
				flags := formatListFlags(`\HasChildren`, nil)
				flags = strings.TrimSuffix(flags, ")") + " " + nose + ")"
				add(flags, anc, "", nil)
			}
		}
	}
	if !q.specialOnly {
		for _, pref := range otherPrefixes(pers) {
			delim := prefixDelim(pers, pref)
			for _, acct := range st.acctOrder {
				sl := st.slotUser(acct)
				if len(sl.order) == 0 {
					continue
				}
				node := pref + acct
				add(`(\HasChildren `+noselectWord(pers)+`)`, node, "", nil)
				for _, name := range sl.order {
					mb := sl.boxes[name]
					if mb == nil {
						continue
					}
					if q.specialOnly && len(mb.special) == 0 {
						continue
					}
					wire := node + delim + name
					flags := formatListFlags(childFlag(sl.order, name, st.delim), nil)
					var spec []string
					if q.showSpecial {
						spec = mb.special
					}
					if len(spec) > 0 {
						flags = formatListFlags(childFlag(sl.order, name, st.delim), spec)
					}
					var pairs [][2]string
					if q.wantStatus {
						pairs, _ = statusPairs(mb, q.status, q.appendLimit, q.statusSize, q.objectRFC, q.objectPlus, user, q.allowCreated, q.noRecent)
					}
					owned := acct == user || (user == "" && acct == st.primaryUser())
					entFlags := flags
					if q.markSubscribed && st.subs[user] != nil && st.subs[user][wire] {
						entFlags = strings.TrimSuffix(entFlags, ")") + ` \Subscribed)`
					}
					if q.subscribedOnly && (st.subs[user] == nil || !st.subs[user][wire]) {
						continue
					}
					if !wildMatch(wire, full, st.delim) || have[wire] {
						continue
					}
					have[wire] = true
					out = append(out, listEntry{
						flags: entFlags, name: wire, status: pairs, mailboxID: mb.jmapID,
						myrights: q.myrights && owned,
					})
				}
			}
		}
		if st.shared != nil {
			for _, name := range st.shared.order {
				mb := st.shared.boxes[name]
				if mb == nil {
					continue
				}
				if _, ok := matchShared(pers, name); !ok {
					continue
				}
				flags := formatListFlags(childFlag(st.shared.order, name, st.delim), nil)
				var pairs [][2]string
				if q.wantStatus {
					pairs, _ = statusPairs(mb, q.status, q.appendLimit, q.statusSize, q.objectRFC, q.objectPlus, user, q.allowCreated, q.noRecent)
				}
				add(flags, name, mb.jmapID, pairs)
				if q.myrights && len(out) > 0 && out[len(out)-1].name == name {
					out[len(out)-1].myrights = true
				}
			}
		}
	}
	return out
}

func noselectWord(p *Personality) string {
	_, w := implyParents(p)
	if w == "" {
		return `\Noselect`
	}
	return w
}
