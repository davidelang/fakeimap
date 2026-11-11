package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestJMAPListen(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	if bound.Jmap != "" {
		t.Fatalf("omitted jmap_listen bound %q", bound.Jmap)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", JmapListen: ""})
	if bound.Jmap != "" {
		t.Fatalf("empty jmap_listen bound %q", bound.Jmap)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", JmapListen: "127.0.0.1:0"})
	host, port, err := net.SplitHostPort(bound.Jmap)
	if err != nil || host != "127.0.0.1" || port == "" || port == "0" {
		t.Fatalf("jmap bound %q", bound.Jmap)
	}
	d := &tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}}
	if c, err := d.Dial("tcp", bound.Jmap); err == nil {
		c.Close()
		t.Fatal("jmap listener accepted TLS")
	}
	res, raw := jmapHTTP(t, bound.Jmap, "", "", http.MethodGet, "/.well-known/jmap", nil)
	if res.StatusCode != 401 {
		t.Fatalf("missing auth %d %s", res.StatusCode, raw)
	}
	res, raw = jmapHTTP(t, bound.Jmap, "demo", "wrong", http.MethodGet, "/.well-known/jmap", nil)
	if res.StatusCode != 401 {
		t.Fatalf("wrong password %d %s", res.StatusCode, raw)
	}
}

func TestJMAPRead(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", JmapListen: "127.0.0.1:0"})
	session := jmapSession(t, bound.Jmap, "demo", "secret")
	if session["username"] != "demo" {
		t.Fatalf("username %v", session["username"])
	}
	for _, key := range []string{"apiUrl", "downloadUrl", "uploadUrl", "eventSourceUrl"} {
		s, _ := session[key].(string)
		if !strings.HasPrefix(s, "/") || strings.Contains(s, "://") {
			t.Fatalf("%s %q", key, s)
		}
	}
	if session["apiUrl"] != "/jmap/" {
		t.Fatalf("apiUrl %v", session["apiUrl"])
	}
	caps := objectField(t, session, "capabilities")
	if _, ok := caps["urn:ietf:params:jmap:core"]; !ok {
		t.Fatal("missing core")
	}
	if _, ok := caps["urn:ietf:params:jmap:mail"]; !ok {
		t.Fatal("missing mail")
	}
	if _, ok := caps["urn:ietf:params:jmap:submission"]; ok {
		t.Fatal("submission advertised")
	}
	accounts := objectField(t, session, "accounts")
	primary := objectField(t, session, "primaryAccounts")
	accountID, _ := primary["urn:ietf:params:jmap:mail"].(string)
	if accountID == "" || accounts[accountID] == nil {
		t.Fatalf("primary %v accounts %v", primary, accounts)
	}
	acc := objectField(t, session["accounts"].(map[string]any)[accountID].(map[string]any), "accountCapabilities")
	if _, ok := acc["urn:ietf:params:jmap:core"]; !ok {
		t.Fatal("account missing core")
	}
	if _, ok := acc["urn:ietf:params:jmap:mail"]; !ok {
		t.Fatal("account missing mail")
	}
	if _, ok := acc["urn:ietf:params:jmap:submission"]; ok {
		t.Fatal("account submission")
	}
	state, _ := session["state"].(string)
	if state == "" {
		t.Fatal("empty state")
	}

	im := dial(t, bound.Listen)
	im.cmd("LOGIN demo secret").ok(t)
	im.cmd(`CREATE Sent (USE (\Sent))`).ok(t)
	boxes := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Mailbox/get", map[string]any{"accountId": accountID, "ids": nil}, "0"},
	})
	list := arrayField(t, methodArgs(t, boxes, "Mailbox/get"), "list")
	byName := map[string]map[string]any{}
	for _, item := range list {
		obj := item.(map[string]any)
		byName[obj["name"].(string)] = obj
	}
	inbox := byName["INBOX"]
	if inbox["role"] != "inbox" || inbox["parentId"] != nil {
		t.Fatalf("inbox %+v", inbox)
	}
	gen := byName["Generated"]
	if gen["role"] != nil || gen["parentId"] != inbox["id"] {
		t.Fatalf("generated %+v parent %v", gen, inbox["id"])
	}
	sent := byName["Sent"]
	if sent["role"] != "sent" || sent["parentId"] != nil {
		t.Fatalf("sent %+v", sent)
	}
	for _, name := range []string{"INBOX", "INBOX/Generated", "Sent"} {
		st := im.cmd("STATUS " + name + " (MESSAGES UNSEEN)").mustOK(t)
		line := strings.Join(st.untagged, " ")
		leaf := name
		if i := strings.LastIndex(name, "/"); i >= 0 && name != "INBOX" {
			leaf = name[i+1:]
		}
		obj := byName[leaf]
		if jsonInt(t, obj["totalEmails"]) != statusNum(t, line, "MESSAGES") || jsonInt(t, obj["unreadEmails"]) != statusNum(t, line, "UNSEEN") {
			t.Fatalf("%s jmap %+v status %s", name, obj, line)
		}
	}
	if _, err := strconv.Atoi(inbox["id"].(string)); err == nil {
		t.Fatalf("mailbox id is a bare uid %v", inbox["id"])
	}

	q := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Mailbox/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"parentId": nil},
			"sort":      []any{map[string]any{"property": "sortOrder", "isAscending": true}, map[string]any{"property": "name", "isAscending": true}},
		}, "0"},
		[]any{"Mailbox/get", map[string]any{
			"accountId": accountID,
			"#ids":      map[string]any{"resultOf": "0", "name": "Mailbox/query", "path": "/ids"},
		}, "1"},
	})
	top := arrayField(t, methodArgs(t, q, "Mailbox/get"), "list")
	var sawInbox bool
	for _, item := range top {
		if item.(map[string]any)["name"] == "INBOX" {
			sawInbox = true
		}
		if item.(map[string]any)["name"] == "Generated" {
			t.Fatal("child returned at the top level")
		}
	}
	if !sawInbox {
		t.Fatalf("top %+v", top)
	}

	inboxID := inbox["id"].(string)
	page := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId":      accountID,
			"filter":         map[string]any{"inMailbox": inboxID},
			"sort":           []any{map[string]any{"property": "receivedAt", "isAscending": true}},
			"limit":          1,
			"position":       0,
			"calculateTotal": true,
		}, "0"},
	})
	qargs := methodArgs(t, page, "Email/query")
	ids := stringArray(t, qargs["ids"])
	if len(ids) != 1 || jsonInt(t, qargs["total"]) != 2 {
		t.Fatalf("page %+v", qargs)
	}
	again := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId":      accountID,
			"filter":         map[string]any{"inMailbox": inboxID},
			"sort":           []any{map[string]any{"property": "receivedAt", "isAscending": true}},
			"position":       1,
			"limit":          10,
			"calculateTotal": true,
		}, "0"},
		[]any{"Email/get", map[string]any{
			"accountId":          accountID,
			"#ids":               map[string]any{"resultOf": "0", "name": "Email/query", "path": "/ids"},
			"properties":         []any{"id", "blobId", "keywords", "mailboxIds", "bodyStructure", "headers", "bodyValues"},
			"bodyProperties":     []any{"partId", "blobId", "type", "charset", "disposition", "name", "size", "subParts"},
			"fetchAllBodyValues": true,
			"maxBodyValueBytes":  100000,
		}, "1"},
	})
	rest := stringArray(t, methodArgs(t, again, "Email/query")["ids"])
	if len(rest) != 1 || rest[0] == ids[0] {
		t.Fatalf("rest %v first %v", rest, ids)
	}
	got := emailList(t, again)[0]
	if got["id"] != rest[0] || got["blobId"] == "" {
		t.Fatalf("email %+v", got)
	}
	if _, err := strconv.Atoi(got["id"].(string)); err == nil {
		t.Fatalf("email id is a bare uid %v", got["id"])
	}
	kw := got["keywords"].(map[string]any)
	if len(kw) != 0 {
		t.Fatalf("second message keywords %+v", kw)
	}
	mbox := got["mailboxIds"].(map[string]any)
	if mbox[inboxID] != true {
		t.Fatalf("mailboxIds %+v", mbox)
	}
	structure := got["bodyStructure"].(map[string]any)
	if structure["type"] != "multipart/mixed" {
		t.Fatalf("structure %+v", structure)
	}
	subs := structure["subParts"].([]any)
	if len(subs) != 2 || subs[0].(map[string]any)["type"] != "text/plain" || subs[1].(map[string]any)["type"] != "text/html" {
		t.Fatalf("subParts %+v", subs)
	}
	plainID := subs[0].(map[string]any)["partId"].(string)
	values := got["bodyValues"].(map[string]any)
	plain := values[plainID].(map[string]any)
	if plain["isTruncated"] != false || !strings.Contains(plain["value"].(string), "This is the plain part.") {
		t.Fatalf("plain %+v", plain)
	}
	var sawHeader bool
	for _, h := range got["headers"].([]any) {
		obj := h.(map[string]any)
		if _, ok := obj["name"].(string); !ok {
			t.Fatalf("header %+v", h)
		}
		if _, ok := obj["value"].(string); !ok {
			t.Fatalf("header %+v", h)
		}
		if strings.EqualFold(obj["name"].(string), "From") && strings.Contains(obj["value"].(string), "sender@example.com") {
			sawHeader = true
		}
	}
	if !sawHeader {
		t.Fatalf("headers %+v", got["headers"])
	}

	first := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{
			"accountId":          accountID,
			"ids":                []any{ids[0]},
			"properties":         []any{"keywords", "bodyValues", "bodyStructure"},
			"fetchAllBodyValues": true,
		}, "0"},
	})
	one := emailList(t, first)[0]
	if one["keywords"].(map[string]any)["$seen"] != true {
		t.Fatalf("seen %+v", one["keywords"])
	}
	im.cmd("SELECT INBOX").ok(t)
	fetched := fetchLiteral(t, fetchLine(t, im.cmd("FETCH 1 (BODY.PEEK[TEXT])").mustOK(t)))
	textID := one["bodyStructure"].(map[string]any)["partId"].(string)
	if one["bodyStructure"].(map[string]any)["type"] != "text/plain" {
		t.Fatalf("single structure %+v", one["bodyStructure"])
	}
	if one["bodyValues"].(map[string]any)[textID].(map[string]any)["value"] != string(fetched) {
		t.Fatalf("text %q fetch %q", one["bodyValues"], fetched)
	}
	if !strings.Contains(string(fetched), "Naïve café is in the body.") {
		t.Fatalf("fetch %q", fetched)
	}

	short := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{
			"accountId":          accountID,
			"ids":                []any{ids[0]},
			"properties":         []any{"bodyValues"},
			"fetchAllBodyValues": true,
			"maxBodyValueBytes":  5,
		}, "0"},
	})
	cut := emailList(t, short)[0]["bodyValues"].(map[string]any)[textID].(map[string]any)
	if cut["isTruncated"] != true || len(cut["value"].(string)) > 5 || !strings.HasPrefix(string(fetched), cut["value"].(string)) {
		t.Fatalf("cut %+v", cut)
	}
	none := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{
			"accountId":  accountID,
			"ids":        []any{ids[0]},
			"properties": []any{"bodyValues"},
		}, "0"},
	})
	if len(emailList(t, none)[0]["bodyValues"].(map[string]any)) != 0 {
		t.Fatalf("body values without fetch %+v", emailList(t, none)[0]["bodyValues"])
	}

	genID := gen["id"].(string)
	gens := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId":      accountID,
			"filter":         map[string]any{"inMailbox": genID},
			"sort":           []any{map[string]any{"property": "receivedAt", "isAscending": true}},
			"calculateTotal": true,
		}, "0"},
		[]any{"Email/get", map[string]any{
			"accountId":          accountID,
			"#ids":               map[string]any{"resultOf": "0", "name": "Email/query", "path": "/ids"},
			"properties":         []any{"id", "bodyStructure", "bodyValues"},
			"fetchAllBodyValues": true,
		}, "1"},
	})
	genIDs := stringArray(t, methodArgs(t, gens, "Email/query")["ids"])
	if jsonInt(t, methodArgs(t, gens, "Email/query")["total"]) != 3 || len(genIDs) != 3 {
		t.Fatalf("generated query %+v", methodArgs(t, gens, "Email/query"))
	}
	g0 := emailList(t, gens)[0]
	gs := g0["bodyStructure"].(map[string]any)
	if gs["type"] != "text/plain" || gs["subParts"] != nil {
		t.Fatalf("generated structure %+v", gs)
	}
	im.cmd("SELECT INBOX/Generated").ok(t)
	gfetch := fetchLiteral(t, fetchLine(t, im.cmd("FETCH 1 (BODY.PEEK[TEXT])").mustOK(t)))
	gval := g0["bodyValues"].(map[string]any)[gs["partId"].(string)].(map[string]any)["value"].(string)
	if gval != string(gfetch) || !strings.Contains(gval, "seed=demo") {
		t.Fatalf("generated %q fetch %q", gval, gfetch)
	}

	badFilter := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"text": "cafe"},
		}, "0"},
	})
	if methodType(t, badFilter, "0") != "invalidArguments" {
		t.Fatalf("filter %+v", badFilter)
	}
	badSort := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"inMailbox": inboxID},
			"sort":      []any{map[string]any{"property": "subject", "isAscending": true}},
		}, "0"},
	})
	if methodType(t, badSort, "0") != "invalidArguments" {
		t.Fatalf("sort %+v", badSort)
	}
	unknown := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/set", map[string]any{"accountId": accountID}, "a"},
		[]any{"Email/import", map[string]any{"accountId": accountID}, "b"},
		[]any{"Email/changes", map[string]any{"accountId": accountID}, "c"},
		[]any{"EmailSubmission/set", map[string]any{"accountId": accountID}, "d"},
	})
	for _, id := range []string{"a", "b", "c", "d"} {
		if methodType(t, unknown, id) != "unknownMethod" {
			t.Fatalf("%s %+v", id, unknown)
		}
	}

	before := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	msg := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: appended\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello-jmap\r\n")
	app := im.cmd("APPEND INBOX (\\Seen \\Answered \\Flagged \\Draft) \"11-Nov-2025 00:00:00 +0000\" {" + strconv.Itoa(len(msg)) + "+}\r\n" + string(msg))
	app.ok(t)
	uid := appendUID(t, app.tagLine)
	after := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	if after == "" || after == before {
		t.Fatalf("state %q -> %q", before, after)
	}
	added := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId":      accountID,
			"filter":         map[string]any{"inMailbox": inboxID},
			"sort":           []any{map[string]any{"property": "receivedAt", "isAscending": true}},
			"calculateTotal": true,
		}, "0"},
	})
	addedIDs := stringArray(t, methodArgs(t, added, "Email/query")["ids"])
	if jsonInt(t, methodArgs(t, added, "Email/query")["total"]) != 3 || len(addedIDs) != 3 {
		t.Fatalf("after append %+v", methodArgs(t, added, "Email/query"))
	}
	newID := addedIDs[len(addedIDs)-1]
	if newID == uid || newID == "" {
		t.Fatalf("id %q uid %q", newID, uid)
	}
	stable := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"inMailbox": inboxID},
			"sort":      []any{map[string]any{"property": "receivedAt", "isAscending": true}},
		}, "0"},
		[]any{"Email/get", map[string]any{
			"accountId":          accountID,
			"ids":                []any{newID},
			"properties":         []any{"id", "keywords", "bodyValues"},
			"fetchAllBodyValues": true,
		}, "1"},
	})
	if strings.Join(stringArray(t, methodArgs(t, stable, "Email/query")["ids"]), ",") != strings.Join(addedIDs, ",") {
		t.Fatal("ids changed")
	}
	row := emailList(t, stable)[0]
	if row["id"] != newID {
		t.Fatalf("id changed %+v", row)
	}
	flags := row["keywords"].(map[string]any)
	for _, kw := range []string{"$seen", "$answered", "$flagged", "$draft"} {
		if flags[kw] != true {
			t.Fatalf("flags %+v", flags)
		}
	}
	var body string
	for _, v := range row["bodyValues"].(map[string]any) {
		body = v.(map[string]any)["value"].(string)
	}
	if body != "hello-jmap\r\n" {
		t.Fatalf("appended body %q", body)
	}
}

func appendUID(t *testing.T, line string) string {
	t.Helper()
	i := strings.Index(line, "[APPENDUID ")
	if i < 0 {
		t.Fatalf("no appenduid %s", line)
	}
	fields := strings.Fields(line[i:])
	if len(fields) < 3 {
		t.Fatalf("appenduid %s", line)
	}
	return strings.TrimRight(fields[2], "]")
}

func statusNum(t *testing.T, line, item string) int {
	t.Helper()
	i := strings.Index(line, item+" ")
	if i < 0 {
		t.Fatalf("no %s in %s", item, line)
	}
	n, err := strconv.Atoi(strings.Trim(strings.Fields(line[i+len(item)+1:])[0], ")"))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func jmapSession(t *testing.T, addr, user, pass string) map[string]any {
	t.Helper()
	res, raw := jmapHTTP(t, addr, user, pass, http.MethodGet, "/.well-known/jmap", nil)
	if res.StatusCode != 200 {
		t.Fatalf("session %d %s", res.StatusCode, raw)
	}
	return jmapJSON(t, raw)
}

func jmapCall(t *testing.T, addr string, session map[string]any, user, pass string, calls []any) map[string]any {
	t.Helper()
	api, _ := session["apiUrl"].(string)
	body, err := json.Marshal(map[string]any{
		"using":       []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"},
		"methodCalls": calls,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, raw := jmapHTTP(t, addr, user, pass, http.MethodPost, api, body)
	if res.StatusCode != 200 {
		t.Fatalf("api %d %s", res.StatusCode, raw)
	}
	return jmapJSON(t, raw)
}

func jmapHTTP(t *testing.T, addr, user, pass, method, path string, body []byte) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://"+addr+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cl := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	res, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, raw
}

func jmapJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("json %s: %v", raw, err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not object %s", raw)
	}
	return m
}

func objectField(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%s %+v", key, m[key])
	}
	return v
}

func arrayField(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s %+v", key, m[key])
	}
	return v
}

func methodArgs(t *testing.T, root map[string]any, name string) map[string]any {
	t.Helper()
	for _, item := range arrayField(t, root, "methodResponses") {
		call := item.([]any)
		if call[0].(string) == name {
			return call[1].(map[string]any)
		}
	}
	t.Fatalf("no %s in %+v", name, root)
	return nil
}

func methodType(t *testing.T, root map[string]any, id string) string {
	t.Helper()
	for _, item := range arrayField(t, root, "methodResponses") {
		call := item.([]any)
		if call[2].(string) != id {
			continue
		}
		if call[0].(string) != "error" {
			t.Fatalf("call %s %+v", id, call)
		}
		return call[1].(map[string]any)["type"].(string)
	}
	t.Fatalf("no call %s", id)
	return ""
}

func emailList(t *testing.T, root map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, item := range arrayField(t, methodArgs(t, root, "Email/get"), "list") {
		out = append(out, item.(map[string]any))
	}
	return out
}

func stringArray(t *testing.T, v any) []string {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("not array %T", v)
	}
	var out []string
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("not string %T", item)
		}
		out = append(out, s)
	}
	return out
}

func jsonInt(t *testing.T, v any) int {
	t.Helper()
	n, ok := v.(json.Number)
	if !ok {
		t.Fatalf("not number %T %v", v, v)
	}
	i, err := n.Int64()
	if err != nil {
		t.Fatal(err)
	}
	return int(i)
}
