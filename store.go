package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Message is one mailbox entry. A generated message keeps the header index
// only: raw stays nil, and bytes() rebuilds RFC822 from the seed and genUID.
// genUID is the id baked into the body, so a copy keeps the same bytes
// without storing them.
type Message struct {
	uid      uint32
	flags    map[string]struct{}
	keywords []string
	recent   bool
	internal time.Time
	raw      []byte
	from     string
	to       string
	cc       string
	subject  string
	size     int
	sent     time.Time

	generated bool
	genSeed   string
	genUID    uint32
	modseq    uint64
	jmapID    string // stable for the process; not the IMAP UID
}

func (m *Message) bytes() []byte {
	if m.generated {
		return renderGenerated(m)
	}
	return m.raw
}

func (m *Message) has(flag string) bool {
	if flag == `\Recent` {
		return m.recent
	}
	_, ok := m.flags[flag]
	return ok
}

var systemFlagOrder = []string{`\Answered`, `\Flagged`, `\Deleted`, `\Seen`, `\Draft`, `\Recent`}

func canonFlag(f string) (string, bool) {
	switch strings.ToLower(f) {
	case `\answered`:
		return `\Answered`, true
	case `\flagged`:
		return `\Flagged`, true
	case `\deleted`:
		return `\Deleted`, true
	case `\seen`:
		return `\Seen`, true
	case `\draft`:
		return `\Draft`, true
	case `\recent`:
		return `\Recent`, true
	}
	if f == "" || strings.HasPrefix(f, `\`) || strings.ContainsAny(f, " (){%*\"\\") {
		return "", false
	}
	for i := 0; i < len(f); i++ {
		if f[i] < 0x21 || f[i] > 0x7e {
			return "", false
		}
	}
	return f, true
}

func (m *Message) addFlag(f string) bool {
	c, ok := canonFlag(f)
	if !ok || c == `\Recent` {
		return c == `\Recent`
	}
	if _, exists := m.flags[c]; exists {
		return true
	}
	m.flags[c] = struct{}{}
	if !strings.HasPrefix(c, `\`) {
		m.keywords = append(m.keywords, c)
	}
	return true
}

func (m *Message) removeFlag(f string) bool {
	c, ok := canonFlag(f)
	if !ok || c == `\Recent` {
		return c == `\Recent`
	}
	delete(m.flags, c)
	if !strings.HasPrefix(c, `\`) {
		out := m.keywords[:0]
		for _, k := range m.keywords {
			if !strings.EqualFold(k, c) {
				out = append(out, k)
			}
		}
		m.keywords = out
	}
	return true
}

func copyFlags(src *Message) (map[string]struct{}, []string) {
	dst := make(map[string]struct{}, len(src.flags))
	for k := range src.flags {
		dst[k] = struct{}{}
	}
	return dst, append([]string(nil), src.keywords...)
}

func flagNames(m *Message, order []string) []string {
	if len(order) == 0 {
		order = systemFlagOrder
	}
	var parts []string
	for _, f := range order {
		if m.has(f) {
			parts = append(parts, f)
		}
	}
	keys := append([]string(nil), m.keywords...)
	sort.Strings(keys)
	return append(parts, keys...)
}

func formatFlags(m *Message, order []string) string {
	parts := flagNames(m, order)
	if len(parts) == 0 {
		return "()"
	}
	return "(" + strings.Join(parts, " ") + ")"
}

type expungedRec struct {
	uid    uint32
	modseq uint64
}

type mbEvent struct {
	id     uint64
	kind   string // expunge, exists, flags
	seq    uint32
	uid    uint32
	modseq uint64
	exists int
	recent int
	flags  string
}

// Mailbox fields are guarded by Store.mu. uidValidity stays for the
// life of the process.
type Mailbox struct {
	name          string
	uidValidity   uint32
	uidNext       uint32
	msgs          []*Message
	events        []mbEvent
	nextID        uint64
	waiters       []chan struct{}
	highestModseq uint64
	expunged      []expungedRec
	special       []string
	jmapID        string // stable for the process; not the IMAP UID
}

func (mb *Mailbox) bump(m *Message) uint64 {
	mb.highestModseq++
	if m != nil {
		m.modseq = mb.highestModseq
	}
	return mb.highestModseq
}

func (mb *Mailbox) noteExpunge(m *Message) {
	mb.bump(m)
	mb.expunged = append(mb.expunged, expungedRec{uid: m.uid, modseq: m.modseq})
}

func (mb *Mailbox) maxUID() uint32 {
	if len(mb.msgs) == 0 {
		return 0
	}
	return mb.msgs[len(mb.msgs)-1].uid
}

func (mb *Mailbox) recentCount() int {
	n := 0
	for _, m := range mb.msgs {
		if m.recent {
			n++
		}
	}
	return n
}

func (mb *Mailbox) unseenCount() int {
	n := 0
	for _, m := range mb.msgs {
		if !m.has(`\Seen`) {
			n++
		}
	}
	return n
}

func (mb *Mailbox) firstUnseen() uint32 {
	for i, m := range mb.msgs {
		if !m.has(`\Seen`) {
			return uint32(i + 1)
		}
	}
	return 0
}

func (mb *Mailbox) push(e mbEvent) uint64 {
	mb.nextID++
	e.id = mb.nextID
	mb.events = append(mb.events, e)
	for _, ch := range mb.waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return e.id
}

func (mb *Mailbox) pushExists() uint64 {
	return mb.push(mbEvent{kind: "exists", exists: len(mb.msgs), recent: mb.recentCount()})
}

// Store is the shared in-memory mailstore.
type Store struct {
	mu        sync.Mutex
	accounts  map[string]string
	boxes     map[string]*Mailbox
	order     []string
	delim     string
	usedUIDV  map[uint32]bool
	nextUIDV  uint32
	nextJmap  uint64
	jmapState uint64 // changes when the mail store changes
	changes   []jmapChange
	blobs     map[string]*jmapBlob
	nextBlob  uint64
}

// jmapChange is one email id change at the session state after it happened.
type jmapChange struct {
	state uint64
	kind  string // created, updated, destroyed
	id    string
}

type jmapBlob struct {
	account string
	typ     string
	data    []byte
}

func (st *Store) touch() {
	st.jmapState++
}

// noteMail records one email change and advances the session state.
// Caller holds st.mu.
func (st *Store) noteMail(kind, id string) {
	if id == "" {
		return
	}
	st.jmapState++
	st.changes = append(st.changes, jmapChange{state: st.jmapState, kind: kind, id: id})
}

func (st *Store) jmapStateText() string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return strconv.FormatUint(st.jmapState, 10)
}

func (st *Store) stampBox(mb *Mailbox) {
	if mb.jmapID != "" {
		return
	}
	st.nextJmap++
	mb.jmapID = "mb" + strconv.FormatUint(st.nextJmap, 10)
}

func (st *Store) stampMsg(m *Message) {
	if m.jmapID != "" {
		return
	}
	st.nextJmap++
	m.jmapID = "em" + strconv.FormatUint(st.nextJmap, 10)
}

func (st *Store) check(user, pass string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	p, ok := st.accounts[user]
	return ok && p == pass
}

func (st *Store) secret(user string) (string, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	p, ok := st.accounts[user]
	return p, ok
}

func (st *Store) canon(name string) (string, error) {
	if name == "" || strings.Contains(name, "\r") || strings.Contains(name, "\n") {
		return "", fmt.Errorf("bad mailbox name")
	}
	if strings.EqualFold(name, "INBOX") {
		return "INBOX", nil
	}
	pref := "INBOX" + st.delim
	if len(name) >= len(pref) && strings.EqualFold(name[:len(pref)], pref) {
		name = "INBOX" + st.delim + name[len(pref):]
	}
	if strings.HasPrefix(name, st.delim) || strings.HasSuffix(name, st.delim) || strings.Contains(name, st.delim+st.delim) {
		return "", fmt.Errorf("bad mailbox name")
	}
	return name, nil
}

func (st *Store) allocUIDV() uint32 {
	for {
		st.nextUIDV++
		if st.nextUIDV == 0 {
			st.nextUIDV = 1
		}
		if !st.usedUIDV[st.nextUIDV] {
			st.usedUIDV[st.nextUIDV] = true
			return st.nextUIDV
		}
	}
}

func (st *Store) noteUIDV(v uint32) uint32 {
	if v == 0 {
		return st.allocUIDV()
	}
	if st.usedUIDV[v] {
		return st.allocUIDV()
	}
	st.usedUIDV[v] = true
	if v > st.nextUIDV {
		st.nextUIDV = v
	}
	return v
}

type resyncMsg struct {
	seq    uint32
	uid    uint32
	flags  string
	modseq uint64
}

type qresyncSel struct {
	uidv     uint32
	modseq   uint64
	known    *seqSet
	knownRaw string
}

type selectInfo struct {
	exists      int
	recent      int
	uidValidity uint32
	uidNext     uint32
	unseen      uint32
	highest     uint64
	readOnly    bool
	closed      bool
	vanished    string
	changed     []resyncMsg
}

func (st *Store) selectBox(s *session, name string, readOnly bool, qr *qresyncSel) (selectInfo, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	cn, err := st.canon(name)
	if err != nil {
		return selectInfo{}, err
	}
	mb := st.boxes[cn]
	if mb == nil {
		return selectInfo{}, fmt.Errorf("no mailbox")
	}
	closed := s.box != nil
	st.detachLocked(s)
	info := selectInfo{
		exists:      len(mb.msgs),
		recent:      mb.recentCount(),
		uidValidity: mb.uidValidity,
		uidNext:     mb.uidNext,
		unseen:      mb.firstUnseen(),
		highest:     mb.highestModseq,
		readOnly:    readOnly,
		closed:      closed,
	}
	if !readOnly {
		for _, m := range mb.msgs {
			m.recent = false
		}
	}
	if qr != nil {
		info.vanished, info.changed = mb.resync(qr, s.pers.FlagOrder)
	}
	s.box = mb
	s.readonly = readOnly
	s.seenID = mb.nextID
	s.wake = make(chan struct{}, 1)
	mb.waiters = append(mb.waiters, s.wake)
	return info, nil
}

func (mb *Mailbox) resync(qr *qresyncSel, flagOrder []string) (string, []resyncMsg) {
	if qr.uidv != mb.uidValidity {
		if qr.knownRaw == "" {
			return "", nil
		}
		return qr.knownRaw, nil
	}
	max := uint32(0)
	if mb.uidNext > 1 {
		max = mb.uidNext - 1
	}
	var uids []uint32
	for _, e := range mb.expunged {
		if e.modseq <= qr.modseq {
			continue
		}
		if qr.known != nil && !qr.known.contains(e.uid, max) {
			continue
		}
		uids = append(uids, e.uid)
	}
	var changed []resyncMsg
	for i, m := range mb.msgs {
		if m.modseq <= qr.modseq {
			continue
		}
		changed = append(changed, resyncMsg{
			seq: uint32(i + 1), uid: m.uid, modseq: m.modseq,
			flags: formatFlags(m, flagOrder),
		})
	}
	if len(uids) == 0 {
		return "", changed
	}
	return formatUIDSet(uids), changed
}

func (st *Store) detachLocked(s *session) {
	if s.box == nil {
		return
	}
	mb := s.box
	if s.wake != nil {
		out := mb.waiters[:0]
		for _, ch := range mb.waiters {
			if ch != s.wake {
				out = append(out, ch)
			}
		}
		mb.waiters = out
	}
	s.box = nil
	s.wake = nil
	s.readonly = false
}

func (st *Store) deselect(s *session) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.detachLocked(s)
}

func (st *Store) advanceSeen(s *session, id uint64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if id > s.seenID {
		s.seenID = id
	}
}

func (st *Store) drain(s *session) []mbEvent {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.box == nil {
		return nil
	}
	var out []mbEvent
	for _, e := range s.box.events {
		if e.id > s.seenID {
			out = append(out, e)
			s.seenID = e.id
		}
	}
	return out
}

type listEntry struct {
	flags  string
	name   string
	status [][2]string
}

// listQuery is the extended LIST this server honors. A two-argument LIST
// leaves it zero, which keeps the old reply.
type listQuery struct {
	extended    bool
	specialOnly bool
	showSpecial bool
	wantStatus  bool
	status      []string
}

func canonPattern(pat, delim string) string {
	i := strings.IndexAny(pat, "*%")
	lit, rest := pat, ""
	if i >= 0 {
		lit, rest = pat[:i], pat[i:]
	}
	if lit == "" {
		return pat
	}
	if strings.EqualFold(lit, "INBOX") {
		return "INBOX" + rest
	}
	pref := "INBOX" + delim
	if strings.EqualFold(lit, pref) {
		return pref + rest
	}
	if len(lit) >= len(pref) && strings.EqualFold(lit[:len(pref)], pref) {
		return pref + lit[len(pref):] + rest
	}
	return pat
}

func wildMatch(name, pat, delim string) bool {
	for {
		if pat == "" {
			return name == ""
		}
		switch pat[0] {
		case '*':
			rest := pat[1:]
			if wildMatch(name, rest, delim) {
				return true
			}
			for i := 0; i < len(name); i++ {
				if wildMatch(name[i+1:], rest, delim) {
					return true
				}
			}
			return false
		case '%':
			rest := pat[1:]
			if wildMatch(name, rest, delim) {
				return true
			}
			for i := 0; i < len(name); i++ {
				if strings.Contains(name[:i+1], delim) {
					break
				}
				if wildMatch(name[i+1:], rest, delim) {
					return true
				}
			}
			return false
		default:
			if name == "" || name[0] != pat[0] {
				return false
			}
			name = name[1:]
			pat = pat[1:]
		}
	}
}

func formatListFlags(child string, special []string) string {
	parts := make([]string, 0, 1+len(special))
	parts = append(parts, child)
	parts = append(parts, special...)
	return "(" + strings.Join(parts, " ") + ")"
}

func (st *Store) list(ref, pat string, q listQuery) ([]listEntry, string, bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if q.wantStatus {
		if _, err := statusPairs(nil, q.status); err != nil {
			return nil, "", false, err
		}
	}
	if pat == "" {
		if q.extended {
			return nil, st.delim, false, nil
		}
		name := strings.TrimSuffix(ref, st.delim)
		return []listEntry{{flags: `(\Noselect)`, name: name}}, st.delim, true, nil
	}
	full := pat
	if ref != "" && !strings.HasPrefix(pat, st.delim) {
		if strings.HasSuffix(ref, st.delim) {
			full = ref + pat
		} else {
			full = ref + st.delim + pat
		}
	}
	full = canonPattern(full, st.delim)
	var out []listEntry
	for _, name := range st.order {
		mb := st.boxes[name]
		if q.specialOnly && len(mb.special) == 0 {
			continue
		}
		if !wildMatch(name, full, st.delim) {
			continue
		}
		flags := `\HasNoChildren`
		pref := name + st.delim
		for _, other := range st.order {
			if strings.HasPrefix(other, pref) {
				flags = `\HasChildren`
				break
			}
		}
		var spec []string
		if q.showSpecial {
			spec = mb.special
		}
		var pairs [][2]string
		if q.wantStatus {
			var err error
			pairs, err = statusPairs(mb, q.status)
			if err != nil {
				return nil, "", false, err
			}
			if pairs == nil {
				pairs = [][2]string{}
			}
		}
		out = append(out, listEntry{flags: formatListFlags(flags, spec), name: name, status: pairs})
	}
	return out, st.delim, false, nil
}

func (st *Store) status(name string, items []string) (string, [][2]string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	cn, err := st.canon(name)
	if err != nil {
		return "", nil, err
	}
	mb := st.boxes[cn]
	if mb == nil {
		return "", nil, fmt.Errorf("no mailbox")
	}
	out, err := statusPairs(mb, items)
	if err != nil {
		return "", nil, err
	}
	return cn, out, nil
}

// statusPairs reads one mailbox. A nil mailbox only checks the item names.
func statusPairs(mb *Mailbox, items []string) ([][2]string, error) {
	var out [][2]string
	for _, it := range items {
		var v uint32
		switch strings.ToUpper(it) {
		case "MESSAGES":
			if mb != nil {
				v = uint32(len(mb.msgs))
			}
		case "RECENT":
			if mb != nil {
				v = uint32(mb.recentCount())
			}
		case "UIDNEXT":
			if mb != nil {
				v = mb.uidNext
			}
		case "UIDVALIDITY":
			if mb != nil {
				v = mb.uidValidity
			}
		case "UNSEEN":
			if mb != nil {
				v = uint32(mb.unseenCount())
			}
		default:
			return nil, fmt.Errorf("bad status item")
		}
		out = append(out, [2]string{strings.ToUpper(it), strconv.FormatUint(uint64(v), 10)})
	}
	return out, nil
}

var specialUseOrder = []string{`\All`, `\Archive`, `\Drafts`, `\Flagged`, `\Junk`, `\Sent`, `\Trash`}

func canonSpecialUse(f string) (string, bool) {
	for _, s := range specialUseOrder {
		if strings.EqualFold(s, f) {
			return s, true
		}
	}
	return "", false
}

func canonSpecialUses(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, f := range in {
		c, ok := canonSpecialUse(f)
		if !ok {
			return nil, fmt.Errorf("bad use")
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

func (st *Store) specialOwned(flag string) bool {
	for _, mb := range st.boxes {
		for _, f := range mb.special {
			if f == flag {
				return true
			}
		}
	}
	return false
}

func (st *Store) create(name string, uses []string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	cn, err := st.canon(name)
	if err != nil {
		return err
	}
	if _, ok := st.boxes[cn]; ok {
		return fmt.Errorf("exists")
	}
	special, err := canonSpecialUses(uses)
	if err != nil {
		return err
	}
	for _, u := range special {
		if st.specialOwned(u) {
			return fmt.Errorf("use conflict")
		}
	}
	mb := &Mailbox{name: cn, uidValidity: st.allocUIDV(), uidNext: 1, special: special}
	st.stampBox(mb)
	st.boxes[cn] = mb
	st.order = append(st.order, cn)
	st.touch()
	return nil
}

func (st *Store) delete(s *session, name string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	cn, err := st.canon(name)
	if err != nil {
		return err
	}
	if cn == "INBOX" {
		return fmt.Errorf("cannot delete INBOX")
	}
	mb := st.boxes[cn]
	if mb == nil {
		return fmt.Errorf("no mailbox")
	}
	pref := cn + st.delim
	for _, other := range st.order {
		if strings.HasPrefix(other, pref) {
			return fmt.Errorf("has children")
		}
	}
	if s.box == mb {
		st.detachLocked(s)
	}
	if len(mb.waiters) > 0 {
		return fmt.Errorf("selected")
	}
	for _, m := range mb.msgs {
		st.noteMail("destroyed", m.jmapID)
	}
	if len(mb.msgs) == 0 {
		st.touch()
	}
	delete(st.boxes, cn)
	order := st.order[:0]
	for _, n := range st.order {
		if n != cn {
			order = append(order, n)
		}
	}
	st.order = order
	return nil
}

func (st *Store) rename(oldName, newName string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	oldC, err := st.canon(oldName)
	if err != nil {
		return err
	}
	newC, err := st.canon(newName)
	if err != nil {
		return err
	}
	if oldC == "INBOX" {
		return fmt.Errorf("cannot rename INBOX")
	}
	if st.boxes[oldC] == nil {
		return fmt.Errorf("no mailbox")
	}
	if oldC == newC {
		return nil
	}
	type pair struct{ old, neu string }
	var moves []pair
	pref := oldC + st.delim
	for _, name := range st.order {
		if name == oldC {
			moves = append(moves, pair{name, newC})
		} else if strings.HasPrefix(name, pref) {
			moves = append(moves, pair{name, newC + name[len(oldC):]})
		}
	}
	moving := map[string]bool{}
	for _, mv := range moves {
		moving[mv.old] = true
	}
	for _, mv := range moves {
		if _, ok := st.boxes[mv.neu]; ok && !moving[mv.neu] {
			return fmt.Errorf("exists")
		}
	}
	neuBoxes := make(map[string]*Mailbox, len(st.boxes))
	for k, v := range st.boxes {
		if !moving[k] {
			neuBoxes[k] = v
		}
	}
	rewritten := map[string]string{}
	for _, mv := range moves {
		mb := st.boxes[mv.old]
		mb.name = mv.neu
		neuBoxes[mv.neu] = mb
		rewritten[mv.old] = mv.neu
	}
	st.boxes = neuBoxes
	for i, name := range st.order {
		if n, ok := rewritten[name]; ok {
			st.order[i] = n
		}
	}
	st.touch()
	return nil
}

type seqBound struct {
	n    uint32
	star bool
}

type seqSpan struct{ a, b seqBound }

type seqSet struct{ spans []seqSpan }

func parseSeqSet(s string) (*seqSet, error) {
	if s == "" {
		return nil, fmt.Errorf("bad sequence")
	}
	var spans []seqSpan
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			return nil, fmt.Errorf("bad sequence")
		}
		left, right, ok := strings.Cut(part, ":")
		a, err := parseBound(left)
		if err != nil {
			return nil, err
		}
		b := a
		if ok {
			b, err = parseBound(right)
			if err != nil {
				return nil, err
			}
		}
		spans = append(spans, seqSpan{a, b})
	}
	return &seqSet{spans: spans}, nil
}

func parseBound(s string) (seqBound, error) {
	if s == "*" {
		return seqBound{star: true}, nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n == 0 {
		return seqBound{}, fmt.Errorf("bad sequence")
	}
	return seqBound{n: uint32(n)}, nil
}

func (b seqBound) val(max uint32) uint32 {
	if b.star {
		return max
	}
	return b.n
}

func (set *seqSet) contains(n, max uint32) bool {
	if n == 0 {
		return false
	}
	for _, sp := range set.spans {
		a, b := sp.a.val(max), sp.b.val(max)
		if a == 0 || b == 0 {
			continue
		}
		if a > b {
			a, b = b, a
		}
		if n >= a && n <= b {
			return true
		}
	}
	return false
}

func (mb *Mailbox) starMax(uidMode bool) uint32 {
	if uidMode {
		if len(mb.msgs) == 0 {
			return mb.uidNext
		}
		return mb.maxUID()
	}
	return uint32(len(mb.msgs))
}

func (mb *Mailbox) each(uidMode bool, set *seqSet, fn func(seq uint32, m *Message)) {
	max := mb.starMax(uidMode)
	for i, m := range mb.msgs {
		seq := uint32(i + 1)
		n := seq
		if uidMode {
			n = m.uid
		}
		if set.contains(n, max) {
			fn(seq, m)
		}
	}
}

type fetchSnap struct {
	seq       uint32
	uid       uint32
	modseq    uint64
	flags     string
	internal  time.Time
	size      int
	raw       []byte
	seenEvent uint64
}

func (st *Store) fetch(s *session, uidMode bool, set *seqSet, items []string, changedSince *uint64) ([]fetchSnap, []string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.box == nil {
		return nil, nil, fmt.Errorf("not selected")
	}
	expanded, err := expandFetch(items, uidMode)
	if err != nil {
		return nil, nil, err
	}
	if changedSince != nil && !itemListed(expanded, "MODSEQ") {
		expanded = append(expanded, "MODSEQ")
	}
	var snaps []fetchSnap
	mb := s.box
	// FLAGS in this response already include \Seen, so this session
	// does not also report the queued flags event.
	swallow := itemListed(expanded, "FLAGS") || s.pers.FetchSeenFlags
	mb.each(uidMode, set, func(seq uint32, m *Message) {
		if changedSince != nil && m.modseq <= *changedSince {
			return
		}
		if s.fault != nil && s.fault.do == "mutate" {
			m.addFlag(`\Flagged`)
		}
		var seenEvent uint64
		if fetchSetsSeen(expanded) && !s.readonly && !m.has(`\Seen`) {
			m.addFlag(`\Seen`)
			mb.bump(m)
			st.noteMail("updated", m.jmapID)
			seenEvent = mb.push(mbEvent{
				kind: "flags", seq: seq, uid: m.uid, modseq: m.modseq,
				flags: formatFlags(m, s.pers.FlagOrder),
			})
			if swallow && seenEvent > s.seenID {
				s.seenID = seenEvent
			}
		}
		raw := append([]byte(nil), m.bytes()...)
		snaps = append(snaps, fetchSnap{
			seq: seq, uid: m.uid, modseq: m.modseq, flags: formatFlags(m, s.pers.FlagOrder),
			internal: m.internal, size: m.size, raw: raw,
			seenEvent: seenEvent,
		})
	})
	return snaps, expanded, nil
}

func itemListed(items []string, name string) bool {
	for _, it := range items {
		if strings.EqualFold(it, name) {
			return true
		}
	}
	return false
}

func expandFetch(items []string, uidMode bool) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	var add func(string) error
	add = func(it string) error {
		u := strings.ToUpper(it)
		switch u {
		case "ALL":
			return addAll([]string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE"}, add)
		case "FAST":
			return addAll([]string{"FLAGS", "INTERNALDATE", "RFC822.SIZE"}, add)
		case "FULL":
			return addAll([]string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE", "BODY"}, add)
		}
		if !knownFetch(u) {
			return fmt.Errorf("bad fetch item")
		}
		key := u
		if seen[key] {
			return nil
		}
		seen[key] = true
		out = append(out, u)
		return nil
	}
	for _, it := range items {
		if err := add(it); err != nil {
			return nil, err
		}
	}
	if uidMode {
		if err := add("UID"); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bad fetch item")
	}
	return out, nil
}

func addAll(items []string, add func(string) error) error {
	for _, it := range items {
		if err := add(it); err != nil {
			return err
		}
	}
	return nil
}

func knownFetch(u string) bool {
	switch u {
	case "FLAGS", "UID", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE", "BODY", "BODYSTRUCTURE",
		"RFC822", "RFC822.HEADER", "RFC822.TEXT", "MODSEQ":
		return true
	}
	if strings.HasPrefix(u, "BODY[") || strings.HasPrefix(u, "BODY.PEEK[") {
		return strings.Contains(u, "]")
	}
	return false
}

func fetchSetsSeen(items []string) bool {
	for _, it := range items {
		u := strings.ToUpper(it)
		switch {
		case strings.HasPrefix(u, "BODY.PEEK"):
			continue
		case strings.HasPrefix(u, "BODY["):
			return true
		case u == "RFC822" || u == "RFC822.TEXT":
			return true
		}
	}
	return false
}

func parseFlagList(toks []Token, i int) (flags []string, next int, err error) {
	if i >= len(toks) || toks[i].Kind != "lparen" {
		return nil, i, fmt.Errorf("bad flags")
	}
	i++
	for i < len(toks) && toks[i].Kind != "rparen" {
		if toks[i].Kind != "atom" && toks[i].Kind != "quoted" {
			return nil, i, fmt.Errorf("bad flags")
		}
		if _, ok := canonFlag(toks[i].Val); !ok {
			return nil, i, fmt.Errorf("bad flag")
		}
		flags = append(flags, toks[i].Val)
		i++
	}
	if i >= len(toks) || toks[i].Kind != "rparen" {
		return nil, i, fmt.Errorf("bad flags")
	}
	return flags, i + 1, nil
}

type storeSnap struct {
	seq    uint32
	uid    uint32
	modseq uint64
	flags  string
}

func (st *Store) storeFlags(s *session, uidMode bool, set *seqSet, item string, flags []string, unchanged *uint64) ([]storeSnap, []uint32, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.box == nil {
		return nil, nil, fmt.Errorf("not selected")
	}
	if s.readonly {
		return nil, nil, fmt.Errorf("read-only")
	}
	u := strings.ToUpper(item)
	silent := false
	op := u
	if strings.HasSuffix(u, ".SILENT") {
		silent = true
		op = strings.TrimSuffix(u, ".SILENT")
	}
	switch op {
	case "FLAGS", "+FLAGS", "-FLAGS":
	default:
		return nil, nil, fmt.Errorf("bad store item")
	}
	var snaps []storeSnap
	var modified []uint32
	mb := s.box
	var last uint64
	mb.each(uidMode, set, func(seq uint32, m *Message) {
		if unchanged != nil && m.modseq > *unchanged {
			id := seq
			if uidMode {
				id = m.uid
			}
			modified = append(modified, id)
			return
		}
		before := formatFlags(m, s.pers.FlagOrder)
		switch op {
		case "FLAGS":
			m.flags = map[string]struct{}{}
			m.keywords = nil
			for _, f := range flags {
				m.addFlag(f)
			}
		case "+FLAGS":
			for _, f := range flags {
				m.addFlag(f)
			}
		case "-FLAGS":
			for _, f := range flags {
				m.removeFlag(f)
			}
		}
		formatted := formatFlags(m, s.pers.FlagOrder)
		if formatted != before {
			mb.bump(m)
			st.noteMail("updated", m.jmapID)
		}
		last = mb.push(mbEvent{kind: "flags", seq: seq, uid: m.uid, modseq: m.modseq, flags: formatted})
		if !silent {
			snaps = append(snaps, storeSnap{seq: seq, uid: m.uid, modseq: m.modseq, flags: formatted})
		}
	})
	if last > s.seenID {
		s.seenID = last
	}
	return snaps, modified, nil
}

func (st *Store) highestModseq(s *session) (uint64, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.box == nil {
		return 0, fmt.Errorf("not selected")
	}
	return s.box.highestModseq, nil
}

type searchKey struct {
	op   string
	arg  string
	num  int
	when time.Time
	set  *seqSet
}

func parseSearch(toks []Token) ([]searchKey, error) {
	var keys []searchKey
	for i := 0; i < len(toks); {
		if toks[i].Kind == "lparen" || toks[i].Kind == "rparen" {
			return nil, fmt.Errorf("bad search")
		}
		op := strings.ToUpper(toks[i].Val)
		switch op {
		case "ALL", "SEEN", "UNSEEN", "FLAGGED", "UNFLAGGED", "DELETED", "UNDELETED":
			keys = append(keys, searchKey{op: op})
			i++
		case "FROM", "TO", "SUBJECT":
			if i+1 >= len(toks) {
				return nil, fmt.Errorf("bad search")
			}
			arg, ok := astring(toks[i+1])
			if !ok {
				return nil, fmt.Errorf("bad search")
			}
			keys = append(keys, searchKey{op: op, arg: arg})
			i += 2
		case "SINCE", "BEFORE", "ON":
			if i+1 >= len(toks) {
				return nil, fmt.Errorf("bad search")
			}
			ds, ok := astring(toks[i+1])
			if !ok {
				return nil, fmt.Errorf("bad search")
			}
			dt, err := parseIMAPDate(ds)
			if err != nil {
				return nil, fmt.Errorf("bad search")
			}
			keys = append(keys, searchKey{op: op, when: dt})
			i += 2
		case "LARGER", "SMALLER":
			if i+1 >= len(toks) || toks[i+1].Kind != "atom" {
				return nil, fmt.Errorf("bad search")
			}
			n, err := strconv.Atoi(toks[i+1].Val)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("bad search")
			}
			keys = append(keys, searchKey{op: op, num: n})
			i += 2
		case "UID":
			if i+1 >= len(toks) || toks[i+1].Kind != "atom" {
				return nil, fmt.Errorf("bad search")
			}
			set, err := parseSeqSet(toks[i+1].Val)
			if err != nil {
				return nil, fmt.Errorf("bad search")
			}
			keys = append(keys, searchKey{op: "UID", set: set})
			i += 2
		default:
			return nil, fmt.Errorf("bad search")
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("bad search")
	}
	return keys, nil
}

func (m *Message) matches(k searchKey, maxUID uint32) bool {
	switch k.op {
	case "ALL":
		return true
	case "SEEN":
		return m.has(`\Seen`)
	case "UNSEEN":
		return !m.has(`\Seen`)
	case "FLAGGED":
		return m.has(`\Flagged`)
	case "UNFLAGGED":
		return !m.has(`\Flagged`)
	case "DELETED":
		return m.has(`\Deleted`)
	case "UNDELETED":
		return !m.has(`\Deleted`)
	case "FROM":
		return strings.Contains(strings.ToLower(m.from), strings.ToLower(k.arg))
	case "TO":
		return strings.Contains(strings.ToLower(m.to), strings.ToLower(k.arg))
	case "SUBJECT":
		return strings.Contains(strings.ToLower(m.subject), strings.ToLower(k.arg))
	case "SINCE":
		return ymd(m.internal) >= ymd(k.when)
	case "BEFORE":
		return ymd(m.internal) < ymd(k.when)
	case "ON":
		return ymd(m.internal) == ymd(k.when)
	case "LARGER":
		return m.size > k.num
	case "SMALLER":
		return m.size < k.num
	case "UID":
		return k.set.contains(m.uid, maxUID)
	default:
		return false
	}
}

// sortCrit is one SORT key. REVERSE flips that key only.
type sortCrit struct {
	key string
	rev bool
}

func addrMailbox(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	if addrs, err := mail.ParseAddressList(h); err == nil && len(addrs) > 0 {
		return addrs[0].Address
	}
	if i := strings.IndexByte(h, '<'); i >= 0 {
		if j := strings.IndexByte(h[i+1:], '>'); j >= 0 {
			return strings.TrimSpace(h[i+1 : i+1+j])
		}
	}
	return h
}

func (m *Message) sortDate() time.Time {
	if !m.sent.IsZero() {
		return m.sent
	}
	return m.internal
}

// lessSort orders one key. Equal keys return false so a stable sort
// keeps mailbox order, which is ascending UID, including under REVERSE.
func lessSort(a, b *Message, c *sortCrit) bool {
	cmp := 0
	switch c.key {
	case "ARRIVAL":
		cmp = a.internal.Compare(b.internal)
	case "DATE":
		cmp = a.sortDate().Compare(b.sortDate())
	case "SIZE":
		switch {
		case a.size < b.size:
			cmp = -1
		case a.size > b.size:
			cmp = 1
		}
	case "FROM":
		cmp = strings.Compare(strings.ToLower(addrMailbox(a.from)), strings.ToLower(addrMailbox(b.from)))
	case "TO":
		cmp = strings.Compare(strings.ToLower(addrMailbox(a.to)), strings.ToLower(addrMailbox(b.to)))
	case "CC":
		cmp = strings.Compare(strings.ToLower(addrMailbox(a.cc)), strings.ToLower(addrMailbox(b.cc)))
	case "SUBJECT":
		cmp = strings.Compare(strings.ToLower(a.subject), strings.ToLower(b.subject))
	}
	if c.rev {
		cmp = -cmp
	}
	return cmp < 0
}

func (st *Store) search(s *session, uidMode bool, keys []searchKey, crit *sortCrit) ([]uint32, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.box == nil {
		return nil, fmt.Errorf("not selected")
	}
	mb := s.box
	maxUID := mb.starMax(true)
	type hit struct {
		id uint32
		m  *Message
	}
	var hits []hit
	for i, m := range mb.msgs {
		ok := true
		for _, k := range keys {
			if !m.matches(k, maxUID) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		id := uint32(i + 1)
		if uidMode {
			id = m.uid
		}
		hits = append(hits, hit{id: id, m: m})
	}
	if crit != nil {
		sort.SliceStable(hits, func(i, j int) bool {
			return lessSort(hits[i].m, hits[j].m, crit)
		})
	}
	ids := make([]uint32, len(hits))
	for i, h := range hits {
		ids[i] = h.id
	}
	return ids, nil
}

func indexOverlay(m *Message) {
	msg, err := mail.ReadMessage(bytes.NewReader(m.raw))
	if err != nil {
		return
	}
	m.from = decodeHdr(msg.Header.Get("From"))
	m.to = decodeHdr(msg.Header.Get("To"))
	m.cc = decodeHdr(msg.Header.Get("Cc"))
	m.subject = decodeHdr(msg.Header.Get("Subject"))
	if t, err := mail.ParseDate(msg.Header.Get("Date")); err == nil {
		m.sent = t
	}
}

func (st *Store) appendMsg(s *session, name string, flags []string, date *time.Time, raw []byte, frozen *time.Time) (uint32, uint32, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	uv, m, err := st.appendLocked(name, flags, date, raw, frozen)
	if err != nil {
		return 0, 0, err
	}
	return uv, m.uid, nil
}

// appendLocked appends one message. Caller holds st.mu.
func (st *Store) appendLocked(name string, flags []string, date *time.Time, raw []byte, frozen *time.Time) (uint32, *Message, error) {
	cn, err := st.canon(name)
	if err != nil {
		return 0, nil, err
	}
	mb := st.boxes[cn]
	if mb == nil {
		return 0, nil, fmt.Errorf("no mailbox")
	}
	for _, f := range flags {
		if _, ok := canonFlag(f); !ok {
			return 0, nil, fmt.Errorf("bad flag")
		}
	}
	raw = toCRLF(raw)
	internal := time.Now()
	if date != nil {
		internal = *date
	} else if frozen != nil {
		internal = *frozen
	}
	m := &Message{
		uid:      mb.uidNext,
		flags:    map[string]struct{}{},
		recent:   true,
		internal: internal,
		raw:      raw,
		size:     len(raw),
	}
	for _, f := range flags {
		m.addFlag(f)
	}
	indexOverlay(m)
	st.stampMsg(m)
	mb.bump(m)
	mb.uidNext++
	mb.msgs = append(mb.msgs, m)
	mb.pushExists()
	st.noteMail("created", m.jmapID)
	return mb.uidValidity, m, nil
}

func cloneMsg(src *Message, uid uint32) *Message {
	flags, kw := copyFlags(src)
	nm := &Message{
		uid:       uid,
		flags:     flags,
		keywords:  kw,
		recent:    true,
		internal:  src.internal,
		from:      src.from,
		to:        src.to,
		cc:        src.cc,
		subject:   src.subject,
		size:      src.size,
		sent:      src.sent,
		generated: src.generated,
		genSeed:   src.genSeed,
		genUID:    src.genUID,
	}
	if src.generated {
		nm.raw = nil
	} else {
		nm.raw = src.raw
	}
	return nm
}

type copyResult struct {
	uidv  uint32
	src   []uint32
	dst   []uint32
	lines []string // expunge lines for MOVE, already accounted on this session
}

func (st *Store) copyMsgs(s *session, uidMode bool, set *seqSet, dest string, move bool) (copyResult, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.box == nil {
		return copyResult{}, fmt.Errorf("not selected")
	}
	if move && s.readonly {
		return copyResult{}, fmt.Errorf("read-only")
	}
	cn, err := st.canon(dest)
	if err != nil {
		return copyResult{}, err
	}
	dst := st.boxes[cn]
	if dst == nil {
		return copyResult{}, fmt.Errorf("no mailbox")
	}
	src := s.box
	var picked []*Message
	var srcUIDs []uint32
	src.each(uidMode, set, func(seq uint32, m *Message) {
		picked = append(picked, m)
		srcUIDs = append(srcUIDs, m.uid)
	})
	res := copyResult{uidv: dst.uidValidity, src: srcUIDs}
	if move && len(picked) > 0 {
		drop := map[uint32]bool{}
		for _, u := range srcUIDs {
			drop[u] = true
		}
		var keep []*Message
		seq := uint32(1)
		var last uint64
		var gone []uint32
		qresync := s.on("QRESYNC")
		for _, m := range src.msgs {
			if drop[m.uid] {
				src.noteExpunge(m)
				st.noteMail("destroyed", m.jmapID)
				gone = append(gone, m.uid)
				if !qresync {
					res.lines = append(res.lines, fmt.Sprintf("* %d EXPUNGE", seq))
				}
				last = src.push(mbEvent{kind: "expunge", seq: seq, uid: m.uid, modseq: m.modseq})
				continue
			}
			keep = append(keep, m)
			seq++
		}
		if qresync && len(gone) > 0 {
			res.lines = append(res.lines, "* VANISHED "+formatUIDSet(gone))
		}
		src.msgs = keep
		if last > s.seenID {
			s.seenID = last
		}
		if s.pers == nil || !s.pers.MoveCopyUIDUntagged {
			src.pushExists()
		}
	}
	var dstUIDs []uint32
	var createdIDs []string
	for _, m := range picked {
		nm := cloneMsg(m, dst.uidNext)
		st.stampMsg(nm)
		dst.bump(nm)
		dst.uidNext++
		dst.msgs = append(dst.msgs, nm)
		dstUIDs = append(dstUIDs, nm.uid)
		createdIDs = append(createdIDs, nm.jmapID)
	}
	res.dst = dstUIDs
	if len(picked) > 0 {
		dst.pushExists()
		for _, id := range createdIDs {
			st.noteMail("created", id)
		}
	}
	return res, nil
}

func (st *Store) expunge(s *session, uidSet *seqSet) ([]string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.box == nil {
		return nil, fmt.Errorf("not selected")
	}
	if s.readonly {
		return nil, fmt.Errorf("read-only")
	}
	mb := s.box
	maxUID := mb.starMax(true)
	var keep []*Message
	var lines []string
	var gone []uint32
	seq := uint32(1)
	var last uint64
	qresync := s.on("QRESYNC")
	for _, m := range mb.msgs {
		hit := m.has(`\Deleted`)
		if uidSet != nil && !uidSet.contains(m.uid, maxUID) {
			hit = false
		}
		if hit {
			mb.noteExpunge(m)
			st.noteMail("destroyed", m.jmapID)
			gone = append(gone, m.uid)
			if !qresync {
				lines = append(lines, fmt.Sprintf("* %d EXPUNGE", seq))
			}
			last = mb.push(mbEvent{kind: "expunge", seq: seq, uid: m.uid, modseq: m.modseq})
			continue
		}
		keep = append(keep, m)
		seq++
	}
	if qresync && len(gone) > 0 {
		lines = append(lines, "* VANISHED "+formatUIDSet(gone))
	}
	mb.msgs = keep
	if last > s.seenID {
		s.seenID = last
	}
	return lines, nil
}

// closeBox expunges on a read-write mailbox without sending EXPUNGE to this
// session, then deselects. Other sessions still see the events.
func (st *Store) closeBox(s *session) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if s.box == nil {
		return fmt.Errorf("not selected")
	}
	mb := s.box
	if !s.readonly {
		var keep []*Message
		seq := uint32(1)
		for _, m := range mb.msgs {
			if m.has(`\Deleted`) {
				mb.noteExpunge(m)
				st.noteMail("destroyed", m.jmapID)
				mb.push(mbEvent{kind: "expunge", seq: seq, uid: m.uid, modseq: m.modseq})
				continue
			}
			keep = append(keep, m)
			seq++
		}
		if len(keep) != len(mb.msgs) {
			mb.msgs = keep
			mb.pushExists()
		}
	}
	// Skip the events this session must not echo.
	s.seenID = mb.nextID
	st.detachLocked(s)
	return nil
}

func (st *Store) selected(s *session) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return s.box != nil
}

func (st *Store) generatedBodiesStored() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	n := 0
	for _, mb := range st.boxes {
		for _, m := range mb.msgs {
			if m.generated && m.raw != nil {
				n++
			}
		}
	}
	return n
}

func (st *Store) mailboxLen(name string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	mb := st.boxes[name]
	if mb == nil {
		return -1
	}
	return len(mb.msgs)
}

type datasetFile struct {
	Accounts []struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"accounts"`
	Mailboxes []struct {
		Name        string `json:"name"`
		UIDValidity uint32 `json:"uid_validity"`
		Messages    []struct {
			File         string   `json:"file"`
			Flags        []string `json:"flags"`
			InternalDate string   `json:"internal_date"`
		} `json:"messages"`
		Generated *struct {
			Count       int    `json:"count"`
			Seed        string `json:"seed"`
			From        string `json:"from"`
			To          string `json:"to"`
			Subject     string `json:"subject"`
			Start       string `json:"start"`
			StepSeconds int    `json:"step_seconds"`
		} `json:"generated"`
		SpecialUse []string `json:"special_use,omitempty"`
	} `json:"mailboxes"`
}

func loadStore(dir, delim string) (*Store, error) {
	f, err := os.Open(filepath.Join(dir, "dataset.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var df datasetFile
	if err := dec.Decode(&df); err != nil {
		return nil, fmt.Errorf("dataset %s: %w", dir, err)
	}
	if len(df.Accounts) == 0 {
		return nil, fmt.Errorf("dataset %s: no accounts", dir)
	}
	st := &Store{
		accounts: map[string]string{},
		boxes:    map[string]*Mailbox{},
		delim:    delim,
		usedUIDV: map[uint32]bool{},
		blobs:    map[string]*jmapBlob{},
	}
	for _, a := range df.Accounts {
		if a.Username == "" {
			return nil, fmt.Errorf("dataset %s: empty username", dir)
		}
		st.accounts[a.Username] = a.Password
	}
	for _, mbj := range df.Mailboxes {
		name, err := st.canon(mbj.Name)
		if err != nil {
			return nil, err
		}
		if _, ok := st.boxes[name]; ok {
			return nil, fmt.Errorf("dataset %s: duplicate mailbox %s", dir, name)
		}
		mb := &Mailbox{
			name:        name,
			uidValidity: st.noteUIDV(mbj.UIDValidity),
			uidNext:     1,
		}
		st.stampBox(mb)
		for _, mj := range mbj.Messages {
			path := mj.File
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			b = toCRLF(b)
			when, err := parseIMAPDateTime(mj.InternalDate)
			if err != nil {
				return nil, fmt.Errorf("dataset %s: %s: %w", dir, mj.File, err)
			}
			m := &Message{
				uid:      mb.uidNext,
				flags:    map[string]struct{}{},
				recent:   true,
				internal: when,
				raw:      b,
				size:     len(b),
			}
			for _, fl := range mj.Flags {
				if !m.addFlag(fl) && !strings.EqualFold(fl, `\Recent`) {
					return nil, fmt.Errorf("dataset %s: bad flag %s", dir, fl)
				}
			}
			indexOverlay(m)
			st.stampMsg(m)
			mb.bump(m)
			mb.uidNext++
			mb.msgs = append(mb.msgs, m)
		}
		if g := mbj.Generated; g != nil {
			if g.Count < 0 {
				return nil, fmt.Errorf("dataset %s: negative generated count", dir)
			}
			start, err := parseIMAPDateTime(g.Start)
			if err != nil {
				return nil, fmt.Errorf("dataset %s: generated start: %w", dir, err)
			}
			for i := 0; i < g.Count; i++ {
				uid := mb.uidNext
				m := &Message{
					uid:       uid,
					flags:     map[string]struct{}{},
					recent:    true,
					internal:  start.Add(time.Duration(int64(i)*int64(g.StepSeconds)) * time.Second),
					from:      g.From,
					to:        g.To,
					subject:   fmt.Sprintf("%s %d", g.Subject, uid),
					generated: true,
					genSeed:   g.Seed,
					genUID:    uid,
				}
				m.size = len(m.bytes())
				st.stampMsg(m)
				mb.bump(m)
				mb.uidNext++
				mb.msgs = append(mb.msgs, m)
			}
		}
		special, err := canonSpecialUses(mbj.SpecialUse)
		if err != nil {
			return nil, fmt.Errorf("dataset %s: bad special_use on %s", dir, name)
		}
		for _, u := range special {
			if st.specialOwned(u) {
				return nil, fmt.Errorf("dataset %s: special_use %s already used", dir, u)
			}
		}
		mb.special = special
		st.boxes[name] = mb
		st.order = append(st.order, name)
	}
	st.jmapState = 1
	return st, nil
}

// become replaces the loaded dataset. The Store pointer stays put so
// sessions keep using it. Callers drop IMAP connections first.
func (st *Store) become(neu *Store) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.accounts = neu.accounts
	st.boxes = neu.boxes
	st.order = neu.order
	st.delim = neu.delim
	st.usedUIDV = neu.usedUIDV
	st.nextUIDV = neu.nextUIDV
	st.nextJmap = neu.nextJmap
	st.jmapState = neu.jmapState
	st.changes = neu.changes
	st.blobs = neu.blobs
	st.nextBlob = neu.nextBlob
}

func (st *Store) usernames() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]string, 0, len(st.accounts))
	for u := range st.accounts {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

func (st *Store) hasAccount(user string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	_, ok := st.accounts[user]
	return ok
}

func (st *Store) addAccount(user, pass string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.accounts[user]; ok {
		return fmt.Errorf("duplicate username")
	}
	st.accounts[user] = pass
	return nil
}

type mailListItem struct {
	UID   uint32   `json:"uid"`
	Flags []string `json:"flags"`
	Size  int      `json:"size"`
}

func (st *Store) listMail(name string) ([]mailListItem, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	cn, err := st.canon(name)
	if err != nil {
		return nil, err
	}
	mb := st.boxes[cn]
	if mb == nil {
		return nil, fmt.Errorf("no mailbox")
	}
	out := make([]mailListItem, 0, len(mb.msgs))
	for _, m := range mb.msgs {
		flags := flagNames(m, nil)
		if flags == nil {
			flags = []string{}
		}
		out = append(out, mailListItem{UID: m.uid, Flags: flags, Size: m.size})
	}
	return out, nil
}

func (st *Store) fetchMail(name string, uid uint32) ([]byte, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	cn, err := st.canon(name)
	if err != nil {
		return nil, err
	}
	mb := st.boxes[cn]
	if mb == nil {
		return nil, fmt.Errorf("no mailbox")
	}
	for _, m := range mb.msgs {
		if m.uid == uid {
			return append([]byte(nil), m.bytes()...), nil
		}
	}
	return nil, fmt.Errorf("no such message")
}

func (st *Store) putBlob(account, typ string, data []byte) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.blobs == nil {
		st.blobs = map[string]*jmapBlob{}
	}
	st.nextBlob++
	id := "b" + strconv.FormatUint(st.nextBlob, 10)
	st.blobs[id] = &jmapBlob{account: account, typ: typ, data: append([]byte(nil), data...)}
	return id
}

func (st *Store) uploadBlob(account, id string) ([]byte, string, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	b := st.blobs[id]
	if b == nil || b.account != account {
		return nil, "", false
	}
	return append([]byte(nil), b.data...), b.typ, true
}

func (st *Store) mailboxByJMAP(id string) *Mailbox {
	for _, name := range st.order {
		mb := st.boxes[name]
		if mb != nil && mb.jmapID == id {
			return mb
		}
	}
	return nil
}

func (st *Store) messageByJMAP(id string) (*Mailbox, *Message) {
	for _, name := range st.order {
		mb := st.boxes[name]
		if mb == nil {
			continue
		}
		for _, m := range mb.msgs {
			if m.jmapID == id {
				return mb, m
			}
		}
	}
	return nil, nil
}

func (st *Store) messageRFC822(id string) ([]byte, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	_, m := st.messageByJMAP(id)
	if m == nil {
		return nil, false
	}
	return append([]byte(nil), m.bytes()...), true
}

type jmapImport struct {
	cid     string
	blobID  string
	mailbox string
	flags   []string
	when    *time.Time
}

func (st *Store) importJMAP(account string, items []jmapImport, frozen *time.Time) (string, string, map[string]any, string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	old := strconv.FormatUint(st.jmapState, 10)
	type ready struct {
		item jmapImport
		raw  []byte
		name string
	}
	var batch []ready
	for _, it := range items {
		b := st.blobs[it.blobID]
		if b == nil || b.account != account {
			return "", "", nil, "invalidArguments"
		}
		mb := st.mailboxByJMAP(it.mailbox)
		if mb == nil {
			return "", "", nil, "invalidArguments"
		}
		batch = append(batch, ready{item: it, raw: b.data, name: mb.name})
	}
	created := map[string]any{}
	for _, it := range batch {
		_, m, err := st.appendLocked(it.name, it.item.flags, it.item.when, it.raw, frozen)
		if err != nil {
			return "", "", nil, "invalidArguments"
		}
		created[it.item.cid] = map[string]any{
			"id":       m.jmapID,
			"blobId":   m.jmapID + "-rfc822",
			"threadId": m.jmapID,
			"size":     m.size,
		}
	}
	return old, strconv.FormatUint(st.jmapState, 10), created, ""
}

func (st *Store) destroyJMAP(ids []string) (string, string, []string, []string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	old := strconv.FormatUint(st.jmapState, 10)
	seen := map[string]bool{}
	var destroyed, missing []string
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		mb, m := st.messageByJMAP(id)
		if m == nil {
			missing = append(missing, id)
			continue
		}
		var keep []*Message
		seq := uint32(1)
		for _, cur := range mb.msgs {
			if cur == m {
				mb.noteExpunge(cur)
				mb.push(mbEvent{kind: "expunge", seq: seq, uid: cur.uid, modseq: cur.modseq})
				continue
			}
			keep = append(keep, cur)
			seq++
		}
		mb.msgs = keep
		st.noteMail("destroyed", id)
		destroyed = append(destroyed, id)
	}
	if destroyed == nil {
		destroyed = []string{}
	}
	if missing == nil {
		missing = []string{}
	}
	return old, strconv.FormatUint(st.jmapState, 10), destroyed, missing
}

func (st *Store) emailChanges(since uint64, max int, limited bool) ([]string, []string, []string, string, bool, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if since == 0 || since > st.jmapState {
		return nil, nil, nil, "", false, false
	}
	var picked []jmapChange
	for _, ch := range st.changes {
		if ch.state > since {
			picked = append(picked, ch)
		}
	}
	end := len(picked)
	more := false
	if limited {
		end = 0
		for n := 1; n <= len(picked); n++ {
			c, u, d := collapseJMAP(picked[:n])
			if len(c)+len(u)+len(d) > max {
				more = true
				break
			}
			end = n
		}
	}
	created, updated, destroyed := collapseJMAP(picked[:end])
	state := st.jmapState
	if more && end > 0 {
		state = picked[end-1].state
	}
	if created == nil {
		created = []string{}
	}
	if updated == nil {
		updated = []string{}
	}
	if destroyed == nil {
		destroyed = []string{}
	}
	return created, updated, destroyed, strconv.FormatUint(state, 10), more, true
}

func collapseJMAP(changes []jmapChange) (created, updated, destroyed []string) {
	type bits struct{ c, u, d bool }
	var order []string
	have := map[string]*bits{}
	for _, ch := range changes {
		b := have[ch.id]
		if b == nil {
			b = &bits{}
			have[ch.id] = b
			order = append(order, ch.id)
		}
		switch ch.kind {
		case "created":
			b.c, b.u, b.d = true, false, false
		case "updated":
			if !b.c && !b.d {
				b.u = true
			}
		case "destroyed":
			if b.c {
				b.c, b.u, b.d = false, false, false
			} else {
				b.c, b.u, b.d = false, false, true
			}
		}
	}
	for _, id := range order {
		b := have[id]
		switch {
		case b.c:
			created = append(created, id)
		case b.d:
			destroyed = append(destroyed, id)
		case b.u:
			updated = append(updated, id)
		}
	}
	return created, updated, destroyed
}
