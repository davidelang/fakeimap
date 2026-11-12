package main

import (
	"fmt"
	"sort"
	"strings"
)

// errContextExists is a second UPDATE with a tag that already names a context.
var errContextExists = fmt.Errorf("context exists")

// searchCtx is one SEARCH or UID SEARCH UPDATE. The tag names it.
// have is the matching UIDs in mailbox order. keys are frozen at registration.
type searchCtx struct {
	tag   string
	uid   bool
	keys  []searchKey
	limit uint64
	have  []uint32
}

// ctxHit is one context's adds and removes for a single mailbox event.
// Numbers are sequence numbers, or UIDs when uid is set.
type ctxHit struct {
	tag string
	uid bool
	add []uint32
	rem []uint32
}

// ctxPend is the context delta for one mailbox event.
type ctxPend struct {
	id   uint64
	kind string
	hits []ctxHit
}

func freezeSearchKeys(keys []searchKey) {
	for i := range keys {
		if keys[i].op == "$" {
			keys[i].op = "UID"
		}
		if len(keys[i].kids) > 0 {
			freezeSearchKeys(keys[i].kids)
		}
	}
}

func (st *Store) addCtxWatchLocked(s *session) {
	for _, w := range st.ctxWatch {
		if w == s {
			return
		}
	}
	st.ctxWatch = append(st.ctxWatch, s)
}

func (st *Store) removeCtxWatchLocked(s *session) {
	out := st.ctxWatch[:0]
	for _, w := range st.ctxWatch {
		if w != s {
			out = append(out, w)
		}
	}
	st.ctxWatch = out
}

func (st *Store) hasContextTag(s *session, tag string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, c := range s.contexts {
		if c.tag == tag {
			return true
		}
	}
	return false
}

// cancelContexts drops the named contexts. An unknown tag is ignored.
// Caller does not hold st.mu.
func (st *Store) cancelContexts(s *session, tags []string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	drop := map[string]bool{}
	for _, tag := range tags {
		drop[tag] = true
	}
	var keep []searchCtx
	for _, c := range s.contexts {
		if !drop[c.tag] {
			keep = append(keep, c)
		}
	}
	s.contexts = keep
	if len(keep) == 0 {
		s.ctxPend = nil
		st.removeCtxWatchLocked(s)
		return
	}
	var pend []ctxPend
	for _, p := range s.ctxPend {
		var hits []ctxHit
		for _, h := range p.hits {
			if !drop[h.tag] {
				hits = append(hits, h)
			}
		}
		if len(hits) == 0 {
			continue
		}
		p.hits = hits
		pend = append(pend, p)
	}
	s.ctxPend = pend
}

func (st *Store) clearContextsLocked(s *session) {
	s.contexts = nil
	s.ctxPend = nil
	st.removeCtxWatchLocked(s)
}

// noteContexts records ADDTO and REMOVEFROM for sessions watching mb.
// Caller holds st.mu. The mailbox still contains an expunged message:
// bulk expunge replaces msgs after the push.
func (st *Store) noteContexts(mb *Mailbox, e mbEvent) {
	if e.kind != "expunge" && e.kind != "flags" && e.kind != "exists" {
		return
	}
	if e.kind == "exists" && len(e.newUIDs) == 0 {
		return
	}
	for _, s := range st.ctxWatch {
		if s.box != mb || len(s.contexts) == 0 {
			continue
		}
		var hits []ctxHit
		for i := range s.contexts {
			c := &s.contexts[i]
			var add, rem []uint32
			switch e.kind {
			case "expunge":
				if !uidListed(c.have, e.uid) {
					break
				}
				c.have = dropUID(c.have, e.uid)
				n := e.uid
				if !c.uid {
					n = mailboxSeq(mb, e.uid)
					if n == 0 {
						n = e.seq
					}
				}
				rem = []uint32{n}
			case "exists", "flags":
				add, rem = c.sync(mb)
			}
			if len(add) == 0 && len(rem) == 0 {
				continue
			}
			hits = append(hits, ctxHit{tag: c.tag, uid: c.uid, add: add, rem: rem})
		}
		if len(hits) == 0 {
			continue
		}
		s.ctxPend = append(s.ctxPend, ctxPend{id: e.id, kind: e.kind, hits: hits})
	}
}

// sync replaces have with the current matches. Adds and removes are the
// numbers to report: UIDs, or sequence numbers for a non-UID search.
func (c *searchCtx) sync(mb *Mailbox) (add, rem []uint32) {
	hits := contextHits(mb, c.keys, c.limit)
	now := make([]uint32, 0, len(hits))
	have := make(map[uint32]bool, len(c.have))
	for _, u := range c.have {
		have[u] = true
	}
	for _, h := range hits {
		now = append(now, h.m.uid)
		if have[h.m.uid] {
			continue
		}
		n := h.m.uid
		if !c.uid {
			n = h.seq
		}
		add = append(add, n)
	}
	nowSet := make(map[uint32]bool, len(now))
	for _, u := range now {
		nowSet[u] = true
	}
	for _, u := range c.have {
		if nowSet[u] {
			continue
		}
		n := u
		if !c.uid {
			n = mailboxSeq(mb, u)
			if n == 0 {
				continue
			}
		}
		rem = append(rem, n)
	}
	c.have = now
	return add, rem
}

func contextHits(mb *Mailbox, keys []searchKey, limit uint64) []msgHit {
	maxUID := mb.starMax(true)
	var cands []msgHit
	for i, m := range mb.msgs {
		if limit > 0 && !inRequested(m, keys, maxUID) {
			continue
		}
		cands = append(cands, msgHit{seq: uint32(i + 1), m: m})
	}
	if limit > 0 && uint64(len(cands)) > limit {
		sort.SliceStable(cands, func(i, j int) bool { return cands[i].m.uid > cands[j].m.uid })
		cands = cands[:limit]
		sort.SliceStable(cands, func(i, j int) bool { return cands[i].m.uid < cands[j].m.uid })
	}
	var hits []msgHit
	for _, c := range cands {
		ok := true
		for _, k := range keys {
			if !c.m.matches(k, maxUID) {
				ok = false
				break
			}
		}
		if ok {
			hits = append(hits, c)
		}
	}
	return hits
}

func mailboxSeq(mb *Mailbox, uid uint32) uint32 {
	for i, m := range mb.msgs {
		if m.uid == uid {
			return uint32(i + 1)
		}
	}
	return 0
}

func uidListed(ids []uint32, uid uint32) bool {
	for _, id := range ids {
		if id == uid {
			return true
		}
	}
	return false
}

func dropUID(ids []uint32, uid uint32) []uint32 {
	var keep []uint32
	for _, id := range ids {
		if id != uid {
			keep = append(keep, id)
		}
	}
	return keep
}

// takePends removes context deltas at or before seenID.
// Hits for a tag that is no longer registered are dropped.
func (st *Store) takePends(s *session) []ctxPend {
	st.mu.Lock()
	defer st.mu.Unlock()
	live := map[string]bool{}
	for _, c := range s.contexts {
		live[c.tag] = true
	}
	var take, keep []ctxPend
	for _, p := range s.ctxPend {
		if p.id > s.seenID {
			keep = append(keep, p)
			continue
		}
		var hits []ctxHit
		for _, h := range p.hits {
			if live[h.tag] {
				hits = append(hits, h)
			}
		}
		if len(hits) == 0 {
			continue
		}
		p.hits = hits
		take = append(take, p)
	}
	if len(keep) == 0 {
		s.ctxPend = nil
	} else {
		s.ctxPend = keep
	}
	return take
}

func (s *session) sendCaughtContext() error {
	for _, line := range formatCtxPends(s.srv.store.takePends(s)) {
		if err := s.sendLine(line); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) contextWake() <-chan struct{} {
	s.srv.store.mu.Lock()
	defer s.srv.store.mu.Unlock()
	if len(s.contexts) == 0 || s.wake == nil {
		return nil
	}
	return s.wake
}

func formatCtxPends(ps []ctxPend) []string {
	var lines []string
	for i := 0; i < len(ps); {
		j := i + 1
		for j < len(ps) && ps[j].kind == ps[i].kind {
			j++
		}
		lines = append(lines, formatCtxGroup(ps[i:j])...)
		i = j
	}
	return lines
}

func formatCtxGroup(ps []ctxPend) []string {
	type acc struct {
		tag string
		uid bool
		add []uint32
		rem []uint32
	}
	var order []string
	by := map[string]*acc{}
	for _, p := range ps {
		for _, h := range p.hits {
			a := by[h.tag]
			if a == nil {
				a = &acc{tag: h.tag, uid: h.uid}
				by[h.tag] = a
				order = append(order, h.tag)
			}
			a.rem = append(a.rem, h.rem...)
			a.add = append(a.add, h.add...)
		}
	}
	var lines []string
	for _, tag := range order {
		a := by[tag]
		if nums := sortU32(a.rem); len(nums) > 0 {
			lines = append(lines, formatCtxLine(a.tag, a.uid, "REMOVEFROM", nums))
		}
	}
	for _, tag := range order {
		a := by[tag]
		if nums := sortU32(a.add); len(nums) > 0 {
			lines = append(lines, formatCtxLine(a.tag, a.uid, "ADDTO", nums))
		}
	}
	return lines
}

func formatCtxLine(tag string, uid bool, op string, nums []uint32) string {
	var b strings.Builder
	b.WriteString("* ESEARCH (TAG ")
	b.WriteString(imapString(tag))
	b.WriteByte(')')
	if uid {
		b.WriteString(" UID")
	}
	b.WriteByte(' ')
	b.WriteString(op)
	b.WriteString(" (0 ")
	b.WriteString(formatUIDSet(nums))
	b.WriteByte(')')
	return b.String()
}

func sortU32(ids []uint32) []uint32 {
	if len(ids) == 0 {
		return nil
	}
	out := append([]uint32(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	n := 1
	for _, v := range out[1:] {
		if v != out[n-1] {
			out[n] = v
			n++
		}
	}
	return out[:n]
}
