package main

import "strings"

// urlRec is one GENURLAUTH token. gen must match the mailbox at fetch time.
type urlRec struct {
	token   string
	rump    string
	user    string
	mailbox string
	owner   string
	shared  bool
	uid     uint32
	gen     uint64
	mech    string
}

func (st *Store) mintURL(user string, pers *Personality, rump, mech string) (string, bool) {
	mbox, uid, ok := parseURLMailbox(rump)
	if !ok {
		return "", false
	}
	mech = strings.ToLower(mech)
	token := randHex(16)
	rec := &urlRec{token: token, rump: rump, user: user, uid: uid, mech: mech}
	st.mu.Lock()
	defer st.mu.Unlock()
	if res, err := st.resolveName(user, pers, mbox); err == nil && !res.virtual && res.sl != nil {
		if mb := res.sl.boxes[res.key]; mb != nil {
			if mb.urlGen == 0 {
				mb.urlGen = 1
			}
			rec.mailbox = res.key
			rec.owner = mb.owner
			rec.shared = mb.shared
			rec.gen = mb.urlGen
		}
	}
	if st.urls == nil {
		st.urls = map[string]*urlRec{}
	}
	st.urls[token] = rec
	return rump + ";urlauth=" + mech + ":" + token, true
}

func (st *Store) resetURLKey(user string, pers *Personality, name string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if name == "" {
		st.walkAll(func(_ string, mb *Mailbox) {
			mb.urlGen++
		})
		return
	}
	res, err := st.resolveName(user, pers, name)
	if err != nil || res.virtual || res.sl == nil {
		return
	}
	if mb := res.sl.boxes[res.key]; mb != nil {
		mb.urlGen++
	}
}

func (st *Store) urlFetchLine(rawURL string, binary bool) string {
	quoted := imapString(rawURL)
	data, dec, decOK, found := st.urlBytes(rawURL)
	if !found {
		return "* URLFETCH " + quoted + " NIL"
	}
	if binary {
		if !decOK {
			return "* URLFETCH " + quoted + " (BINARY NIL)"
		}
		return "* URLFETCH " + quoted + " (BINARY " + literalForm(dec, true) + ")"
	}
	return "* URLFETCH " + quoted + " " + literal(data)
}

func (st *Store) urlBytes(rawURL string) (data, decoded []byte, decOK, found bool) {
	rump, mech, token, ok := splitURLAuth(rawURL)
	if !ok {
		return nil, nil, false, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	rec := st.urls[token]
	if rec == nil || rec.rump != rump || !strings.EqualFold(rec.mech, mech) || rec.mailbox == "" {
		return nil, nil, false, false
	}
	var sl *treeSlot
	if rec.shared {
		sl = st.slotShared()
	} else {
		owner := rec.owner
		if owner == "" {
			owner = rec.user
		}
		sl = st.slotUser(owner)
	}
	mb := sl.boxes[rec.mailbox]
	if mb == nil || mb.urlGen != rec.gen {
		return nil, nil, false, false
	}
	var msg *Message
	for _, m := range mb.msgs {
		if m.uid == rec.uid {
			msg = m
			break
		}
	}
	if msg == nil {
		return nil, nil, false, false
	}
	raw := append([]byte(nil), msg.bytes()...)
	sec, has := urlSection(rump)
	if !has {
		b, stt := decodeBinaryPart(raw, "1")
		return raw, b, stt == binaryReady, true
	}
	part, pok := sectionBytes(raw, sec)
	if !pok {
		return nil, nil, false, false
	}
	b, stt := decodeBinaryPart(raw, sec)
	return part, b, stt == binaryReady, true
}

func parseURLMailbox(rump string) (string, uint32, bool) {
	if !urlRumpOK(rump) {
		return "", 0, false
	}
	const mark = "/;UID="
	i := strings.Index(rump, mark)
	head := rump[:i]
	j := strings.Index(head, "://")
	if j < 0 {
		return "", 0, false
	}
	rest := head[j+3:]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 || slash+1 >= len(rest) {
		return "", 0, false
	}
	mbox := rest[slash+1:]
	num := rump[i+len(mark):]
	var uid uint32
	for _, c := range num {
		if c < '0' || c > '9' {
			break
		}
		uid = uid*10 + uint32(c-'0')
	}
	if uid == 0 || mbox == "" {
		return "", 0, false
	}
	return mbox, uid, true
}

func splitURLAuth(s string) (rump, mech, token string, ok bool) {
	low := strings.ToLower(s)
	i := strings.Index(low, ";urlauth=")
	if i < 0 {
		return "", "", "", false
	}
	rump = s[:i]
	rest := s[i+len(";urlauth="):]
	mech, token, ok = strings.Cut(rest, ":")
	if !ok || mech == "" || token == "" {
		return "", "", "", false
	}
	return rump, mech, token, true
}

func urlSection(rump string) (string, bool) {
	low := strings.ToLower(rump)
	i := strings.Index(low, ";section=")
	if i < 0 {
		return "", false
	}
	sec := rump[i+len(";section="):]
	if cut := strings.IndexByte(sec, ';'); cut >= 0 {
		sec = sec[:cut]
	}
	if sec == "" {
		return "", false
	}
	return sec, true
}
