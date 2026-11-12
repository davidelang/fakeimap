package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestJMAPTLSListen(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0"})
	if bound.JmapTLS != "" {
		t.Fatalf("omitted jmap_tls_listen bound %q", bound.JmapTLS)
	}
	_, bound = start(t, Config{Listen: "127.0.0.1:0", JmapTLSListen: ""})
	if bound.JmapTLS != "" {
		t.Fatalf("empty jmap_tls_listen bound %q", bound.JmapTLS)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	rawCfg, err := json.Marshal(Config{
		Listen:        "127.0.0.1:0",
		JmapTLSListen: "127.0.0.1:0",
		Personality:   "personalities/cyrus-3137.json",
		Dataset:       "datasets/demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rawCfg, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := newServer(path, io.Discard); err == nil || !strings.Contains(err.Error(), "JMAP TLS needs cert_file and key_file") {
		t.Fatalf("jmap_tls_listen without a certificate: %v", err)
	}

	cert, key := writeCert(t)
	_, bound, cc := startCtrl(t, Config{
		Listen:        "127.0.0.1:0",
		CertFile:      cert,
		KeyFile:       key,
		JmapListen:    "127.0.0.1:0",
		JmapTLSListen: "127.0.0.1:0",
	})
	host, port, err := net.SplitHostPort(bound.JmapTLS)
	if err != nil || host != "127.0.0.1" || port == "" || port == "0" {
		t.Fatalf("jmap tls bound %q", bound.JmapTLS)
	}
	st := cc.call(map[string]any{"op": "status"}).ok(t)
	if st.JmapTLS != bound.JmapTLS || st.Jmap != bound.Jmap || st.Jmap == "" {
		t.Fatalf("status jmap %q tls %q bound %+v", st.Jmap, st.JmapTLS, bound)
	}
	d := &tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}}
	if c, err := d.Dial("tcp", bound.Jmap); err == nil {
		c.Close()
		t.Fatal("cleartext jmap listener accepted TLS")
	}
	res, raw := jmapHTTP(t, bound.Jmap, "demo", "secret", http.MethodGet, "/.well-known/jmap", nil)
	if res.StatusCode != 200 {
		t.Fatalf("cleartext session %d %s", res.StatusCode, raw)
	}
	session := jmapSessionTLS(t, bound.JmapTLS, "demo", "secret")
	for _, key := range []string{"apiUrl", "downloadUrl", "uploadUrl", "eventSourceUrl"} {
		s, _ := session[key].(string)
		if !strings.HasPrefix(s, "/") || strings.Contains(s, "://") {
			t.Fatalf("%s %q", key, s)
		}
	}
}

func jmapSessionTLS(t *testing.T, addr, user, pass string) map[string]any {
	t.Helper()
	res, raw := jmapDoTLS(t, addr, user, pass, http.MethodGet, "/.well-known/jmap", "", nil)
	if res.StatusCode != 200 {
		t.Fatalf("tls session %d %s", res.StatusCode, raw)
	}
	return jmapJSON(t, raw)
}

func jmapDoTLS(t *testing.T, addr, user, pass, method, path, contentType string, body []byte) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, "https://"+addr+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	cl := &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
	}}
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
	if _, ok := caps["urn:ietf:params:jmap:submission"]; !ok {
		t.Fatal("submission missing")
	}
	if objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:submission"] != "demo" {
		t.Fatalf("submission account %v", objectField(t, session, "primaryAccounts"))
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
	if _, ok := acc["urn:ietf:params:jmap:submission"]; !ok {
		t.Fatal("account submission missing")
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
		[]any{"Identity/set", map[string]any{"accountId": accountID}, "d"},
		[]any{"Foo/bar", map[string]any{"accountId": accountID}, "e"},
	})
	for _, id := range []string{"d", "e"} {
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

func TestJMAPWrite(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", JmapListen: "127.0.0.1:0"})
	session := jmapSession(t, bound.Jmap, "demo", "secret")
	if _, ok := objectField(t, session, "capabilities")["urn:ietf:params:jmap:submission"]; !ok {
		t.Fatal("submission missing")
	}
	accountID := objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:mail"].(string)
	state := session["state"].(string)

	raw := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: uploaded\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nuploaded-body\r\n")
	res, upBody := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodPost, "/jmap/upload/"+accountID+"/", "message/rfc822", raw)
	if res.StatusCode != 200 && res.StatusCode != 201 {
		t.Fatalf("upload %d %s", res.StatusCode, upBody)
	}
	up := jmapJSON(t, upBody)
	if up["accountId"] != accountID || up["type"] != "message/rfc822" || jsonInt(t, up["size"]) != len(raw) {
		t.Fatalf("upload json %+v", up)
	}
	blobID, _ := up["blobId"].(string)
	if blobID == "" {
		t.Fatal("missing blobId")
	}
	if jmapSession(t, bound.Jmap, "demo", "secret")["state"] != state {
		t.Fatal("upload changed state")
	}
	res, empty := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodPost, "/jmap/upload/"+accountID+"/", "message/rfc822", []byte{})
	if res.StatusCode != 400 {
		t.Fatalf("empty upload %d %s", res.StatusCode, empty)
	}
	res, denied := jmapDo(t, bound.Jmap, "demo", "wrong", http.MethodPost, "/jmap/upload/"+accountID+"/", "message/rfc822", raw)
	if res.StatusCode != 401 {
		t.Fatalf("upload password %d %s", res.StatusCode, denied)
	}
	res, other := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodPost, "/jmap/upload/other/", "message/rfc822", raw)
	if res.StatusCode != 404 {
		t.Fatalf("upload account %d %s", res.StatusCode, other)
	}
	res, got := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodGet, "/jmap/download/"+accountID+"/"+blobID+"/message?accept=message%2Frfc822", "", nil)
	if res.StatusCode != 200 || !bytes.Equal(got, raw) {
		t.Fatalf("download %d %q", res.StatusCode, got)
	}
	res, missing := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodGet, "/jmap/download/"+accountID+"/no-such-blob/message", "", nil)
	if res.StatusCode != 404 {
		t.Fatalf("unknown blob %d %s", res.StatusCode, missing)
	}

	boxes := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Mailbox/get", map[string]any{"accountId": accountID, "ids": nil}, "0"},
	})
	var inboxID string
	for _, item := range arrayField(t, methodArgs(t, boxes, "Mailbox/get"), "list") {
		obj := item.(map[string]any)
		if obj["name"] == "INBOX" {
			inboxID = obj["id"].(string)
		}
	}
	if inboxID == "" {
		t.Fatal("no inbox")
	}
	listed := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"inMailbox": inboxID},
			"sort":      []any{map[string]any{"property": "receivedAt", "isAscending": true}},
		}, "0"},
		[]any{"Email/get", map[string]any{
			"accountId":  accountID,
			"#ids":       map[string]any{"resultOf": "0", "name": "Email/query", "path": "/ids"},
			"properties": []any{"id", "blobId"},
		}, "1"},
	})
	first := emailList(t, listed)[0]
	mailBlob, _ := first["blobId"].(string)
	im := dial(t, bound.Listen)
	im.cmd("LOGIN demo secret").ok(t)
	im.cmd("SELECT INBOX").ok(t)
	fetched := fetchLiteral(t, fetchLine(t, im.cmd("FETCH 1 (RFC822)").mustOK(t)))
	res, rfc := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodGet, "/jmap/download/"+accountID+"/"+mailBlob+"/message", "", nil)
	if res.StatusCode != 200 || !bytes.Equal(rfc, fetched) {
		t.Fatalf("rfc822 download %d equal %v", res.StatusCode, bytes.Equal(rfc, fetched))
	}

	exists, _, _, _ := parseSelect(t, im.cmd("SELECT INBOX").mustOK(t))
	im.send("i1 IDLE\r\n")
	for {
		rec := im.readRec()
		if strings.HasPrefix(strings.TrimRight(rec, "\r\n"), "+") {
			break
		}
	}
	imported := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/import", map[string]any{
			"accountId": accountID,
			"emails": map[string]any{
				"k1": map[string]any{
					"blobId":     blobID,
					"mailboxIds": map[string]any{inboxID: true},
					"keywords": map[string]any{
						"$seen": true, "$flagged": true, "$answered": true, "$draft": true,
						"$liveimap-staging": true,
					},
				},
			},
		}, "0"},
	})
	created := objectField(t, methodArgs(t, imported, "Email/import"), "created")
	row := objectField(t, created, "k1")
	emailID, _ := row["id"].(string)
	if emailID == "" {
		t.Fatalf("created %+v", created)
	}
	deadline := time.Now().Add(5 * time.Second)
	var saw bool
	for time.Now().Before(deadline) {
		im.c.SetReadDeadline(time.Now().Add(time.Second))
		rec := im.readRec()
		if strings.Contains(rec, "EXISTS") {
			fields := strings.Fields(rec)
			n, _ := strconv.Atoi(fields[1])
			if n != exists+1 {
				t.Fatalf("exists %d want %d (%q)", n, exists+1, rec)
			}
			saw = true
			break
		}
	}
	if !saw {
		t.Fatal("IDLE did not see EXISTS")
	}
	im.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	im.send("DONE\r\n")
	im.readTag("i1").ok(t)
	flags := strings.Join(im.cmd("FETCH "+strconv.Itoa(exists+1)+" (FLAGS)").mustOK(t).untagged, " ")
	for _, flag := range []string{`\Seen`, `\Flagged`, `\Answered`, `\Draft`, "$liveimap-staging"} {
		if !strings.Contains(flags, flag) {
			t.Fatalf("flags %s missing %s", flags, flag)
		}
	}
	gotMail := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{
			"accountId":  accountID,
			"ids":        []any{emailID},
			"properties": []any{"id", "keywords", "mailboxIds"},
		}, "0"},
	})
	mailRow := emailList(t, gotMail)[0]
	kw := mailRow["keywords"].(map[string]any)
	for _, k := range []string{"$seen", "$flagged", "$answered", "$draft", "$liveimap-staging"} {
		if kw[k] != true {
			t.Fatalf("keywords %+v", kw)
		}
	}
	if objectField(t, mailRow, "mailboxIds")[inboxID] != true {
		t.Fatalf("mailbox %+v", mailRow)
	}

	beforeBad := jsonInt(t, methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId":      accountID,
			"filter":         map[string]any{"inMailbox": inboxID},
			"calculateTotal": true,
		}, "0"},
	}), "Email/query")["total"])
	for _, emails := range []map[string]any{
		{"k1": map[string]any{"blobId": blobID, "mailboxIds": map[string]any{}}},
		{"k1": map[string]any{"blobId": blobID, "mailboxIds": map[string]any{inboxID: true, "other": true}}},
		{"k1": map[string]any{"blobId": "missing-blob", "mailboxIds": map[string]any{inboxID: true}}},
		{"k1": map[string]any{"blobId": blobID, "mailboxIds": map[string]any{"no-such-box": true}}},
	} {
		bad := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
			[]any{"Email/import", map[string]any{"accountId": accountID, "emails": emails}, "0"},
		})
		if methodType(t, bad, "0") != "invalidArguments" {
			t.Fatalf("import %+v", bad)
		}
	}
	afterBad := jsonInt(t, methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId":      accountID,
			"filter":         map[string]any{"inMailbox": inboxID},
			"calculateTotal": true,
		}, "0"},
	}), "Email/query")["total"])
	if afterBad != beforeBad {
		t.Fatalf("bad import stored %d -> %d", beforeBad, afterBad)
	}

	kept := emailID
	doomed := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/import", map[string]any{
			"accountId": accountID,
			"emails": map[string]any{
				"k2": map[string]any{"blobId": blobID, "mailboxIds": map[string]any{inboxID: true}},
			},
		}, "0"},
	})
	doomedID := objectField(t, objectField(t, methodArgs(t, doomed, "Email/import"), "created"), "k2")["id"].(string)
	held := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/set", map[string]any{
			"accountId": accountID,
			"destroy":   []any{doomedID},
			"create":    map[string]any{"k": map[string]any{"mailboxIds": map[string]any{inboxID: true}}},
		}, "0"},
	})
	if methodType(t, held, "0") != "invalidArguments" {
		t.Fatalf("create destroy %+v", held)
	}
	still := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{"accountId": accountID, "ids": []any{doomedID, kept}, "properties": []any{"id"}}, "0"},
	})
	if len(emailList(t, still)) != 2 {
		t.Fatalf("create destroyed %+v", still)
	}
	removed := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/set", map[string]any{
			"accountId": accountID,
			"destroy":   []any{doomedID, "missing-email"},
		}, "0"},
	})
	setArgs := methodArgs(t, removed, "Email/set")
	if strings.Join(stringArray(t, setArgs["destroyed"]), ",") != doomedID {
		t.Fatalf("destroyed %+v", setArgs)
	}
	if strings.Join(stringArray(t, setArgs["notFound"]), ",") != "missing-email" {
		t.Fatalf("notFound %+v", setArgs)
	}
	if objectField(t, objectField(t, setArgs, "notDestroyed"), "missing-email")["type"] != "notFound" {
		t.Fatalf("notDestroyed %+v", setArgs)
	}
	gone := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{"accountId": accountID, "ids": []any{doomedID}, "properties": []any{"id"}}, "0"},
	})
	if len(emailList(t, gone)) != 0 || strings.Join(stringArray(t, methodArgs(t, gone, "Email/get")["notFound"]), ",") != doomedID {
		t.Fatalf("still present %+v", gone)
	}

	cur := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	same := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": cur}, "0"},
	})
	sameArgs := methodArgs(t, same, "Email/changes")
	if sameArgs["newState"] != cur || jsonBool(t, sameArgs["hasMoreChanges"]) ||
		len(stringArray(t, sameArgs["created"]))+len(stringArray(t, sameArgs["updated"]))+len(stringArray(t, sameArgs["destroyed"])) != 0 {
		t.Fatalf("empty changes %+v", sameArgs)
	}
	badState := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": "not-a-state"}, "0"},
	})
	if methodType(t, badState, "0") != "cannotCalculateChanges" {
		t.Fatalf("unknown state %+v", badState)
	}

	mark := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	app := im.cmd("APPEND INBOX {5+}\r\nhello")
	app.ok(t)
	appended := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": mark, "maxChanges": 50}, "0"},
	})
	appArgs := methodArgs(t, appended, "Email/changes")
	createdIDs := stringArray(t, appArgs["created"])
	if len(createdIDs) != 1 || len(stringArray(t, appArgs["updated"])) != 0 || len(stringArray(t, appArgs["destroyed"])) != 0 {
		t.Fatalf("append changes %+v", appArgs)
	}
	if appArgs["newState"] != jmapSession(t, bound.Jmap, "demo", "secret")["state"] || jsonBool(t, appArgs["hasMoreChanges"]) {
		t.Fatalf("append state %+v", appArgs)
	}
	newID := createdIDs[0]
	flagged := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	im.cmd("STORE " + strconv.Itoa(exists+2) + " +FLAGS (\\Seen)").ok(t)
	flagCh := methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": flagged}, "0"},
	}), "Email/changes")
	if strings.Join(stringArray(t, flagCh["updated"]), ",") != newID || len(stringArray(t, flagCh["created"])) != 0 {
		t.Fatalf("flag changes %+v", flagCh)
	}
	expunged := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	im.cmd("STORE " + strconv.Itoa(exists+2) + " +FLAGS (\\Deleted)").ok(t)
	im.cmd("EXPUNGE").ok(t)
	expCh := methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": expunged}, "0"},
	}), "Email/changes")
	if !strings.Contains(","+strings.Join(stringArray(t, expCh["destroyed"]), ",")+",", ","+newID+",") {
		t.Fatalf("expunge changes %+v", expCh)
	}
	jmapDestroyed := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/set", map[string]any{"accountId": accountID, "destroy": []any{kept}}, "0"},
	})
	desCh := methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": jmapDestroyed}, "0"},
	}), "Email/changes")
	if strings.Join(stringArray(t, desCh["destroyed"]), ",") != kept {
		t.Fatalf("jmap destroy changes %+v", desCh)
	}

	pageSince := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	im.cmd("APPEND INBOX {5+}\r\nfirst").ok(t)
	im.cmd("APPEND INBOX {6+}\r\nsecond").ok(t)
	now := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	page := methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": pageSince, "maxChanges": 1}, "0"},
	}), "Email/changes")
	if !jsonBool(t, page["hasMoreChanges"]) || page["newState"] == now || len(stringArray(t, page["created"])) != 1 {
		t.Fatalf("paged %+v", page)
	}
	rest := methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": page["newState"], "maxChanges": 10}, "0"},
	}), "Email/changes")
	if jsonBool(t, rest["hasMoreChanges"]) || rest["newState"] != now || len(stringArray(t, rest["created"])) != 1 {
		t.Fatalf("page rest %+v now %s", rest, now)
	}
}

func TestJMAPEmailUpdate(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", JmapListen: "127.0.0.1:0"})
	session := jmapSession(t, bound.Jmap, "demo", "secret")
	accountID := objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:mail"].(string)
	boxes := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Mailbox/get", map[string]any{"accountId": accountID, "ids": nil}, "0"},
	})
	var inboxID string
	for _, item := range arrayField(t, methodArgs(t, boxes, "Mailbox/get"), "list") {
		obj := item.(map[string]any)
		if obj["name"] == "INBOX" {
			inboxID = obj["id"].(string)
		}
	}
	if inboxID == "" {
		t.Fatal("no inbox")
	}
	raw := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: patch\r\nContent-Type: text/plain; charset=utf-8\r\n\r\npatch-body\r\n")
	res, upBody := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodPost, "/jmap/upload/"+accountID+"/", "message/rfc822", raw)
	if res.StatusCode != 201 {
		t.Fatalf("upload %d %s", res.StatusCode, upBody)
	}
	blobID := jmapJSON(t, upBody)["blobId"].(string)
	im := dial(t, bound.Listen)
	im.cmd("LOGIN demo secret").ok(t)
	exists, _, _, _ := parseSelect(t, im.cmd("SELECT INBOX").mustOK(t))
	imported := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/import", map[string]any{
			"accountId": accountID,
			"emails": map[string]any{
				"k1": map[string]any{"blobId": blobID, "mailboxIds": map[string]any{inboxID: true}},
			},
		}, "0"},
	})
	emailID := objectField(t, objectField(t, methodArgs(t, imported, "Email/import"), "created"), "k1")["id"].(string)
	if emailID == "" {
		t.Fatal("no email")
	}
	seq := exists + 1

	before := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	set := emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"keywords/$seen": true, "keywords/$flagged": true},
	}, nil)
	if _, ok := set["updated"].(map[string]any)[emailID]; !ok || set["updated"].(map[string]any)[emailID] != nil {
		t.Fatalf("updated %+v", set["updated"])
	}
	flagCh := methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": before}, "0"},
	}), "Email/changes")
	if strings.Join(stringArray(t, flagCh["updated"]), ",") != emailID ||
		len(stringArray(t, flagCh["created"]))+len(stringArray(t, flagCh["destroyed"])) != 0 {
		t.Fatalf("flag changes %+v", flagCh)
	}
	set = emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"keywords/$seen": nil, "keywords/$answered": true},
	}, nil)
	if _, ok := set["updated"].(map[string]any)[emailID]; !ok {
		t.Fatalf("clear %+v", set)
	}
	kw := emailKeywords(t, bound.Jmap, session, accountID, emailID)
	if kw["$seen"] == true || kw["$flagged"] != true || kw["$answered"] != true || kw["$draft"] == true {
		t.Fatalf("keywords %+v", kw)
	}
	im.cmd("NOOP").ok(t)
	fetched := im.cmd("FETCH " + strconv.Itoa(seq) + " (FLAGS)").mustOK(t)
	flags := fetched.untagged[len(fetched.untagged)-1]
	for _, flag := range []string{`\Flagged`, `\Answered`} {
		if !strings.Contains(flags, flag) {
			t.Fatalf("flags %s missing %s", flags, flag)
		}
	}
	if strings.Contains(flags, `\Seen`) || strings.Contains(flags, `\Draft`) {
		t.Fatalf("flags %s", flags)
	}

	kept := emailKeywords(t, bound.Jmap, session, accountID, emailID)
	for _, patch := range []map[string]any{
		{"keywords/$draft": false},
		{"keywords/$forwarded": true},
		{"keywords/$flagged": nil, "keywords/$draft": "yes"},
		{"subject": "nope"},
	} {
		bad := emailSet(t, bound.Jmap, session, accountID, map[string]any{emailID: patch}, nil)
		if objectField(t, objectField(t, bad, "notUpdated"), emailID)["type"] != "invalidProperties" {
			t.Fatalf("bad patch %+v -> %+v", patch, bad)
		}
		if _, ok := bad["updated"].(map[string]any)[emailID]; ok {
			t.Fatalf("bad patch updated %+v", bad)
		}
	}
	if kw := emailKeywords(t, bound.Jmap, session, accountID, emailID); kw["$flagged"] != kept["$flagged"] || kw["$answered"] != kept["$answered"] || kw["$draft"] == true || kw["$seen"] == true {
		t.Fatalf("patch changed keywords %+v", kw)
	}

	other := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/import", map[string]any{
			"accountId": accountID,
			"emails": map[string]any{
				"k2": map[string]any{"blobId": blobID, "mailboxIds": map[string]any{inboxID: true}},
			},
		}, "0"},
	})
	otherID := objectField(t, objectField(t, methodArgs(t, other, "Email/import"), "created"), "k2")["id"].(string)
	mixed := emailSet(t, bound.Jmap, session, accountID, map[string]any{
		"missing-email": map[string]any{"keywords/$seen": true},
		emailID:         map[string]any{"keywords/$draft": true},
	}, nil)
	if objectField(t, objectField(t, mixed, "notUpdated"), "missing-email")["type"] != "notFound" {
		t.Fatalf("missing %+v", mixed)
	}
	if _, ok := mixed["updated"].(map[string]any)[emailID]; !ok {
		t.Fatalf("sibling %+v", mixed)
	}
	if emailKeywords(t, bound.Jmap, session, accountID, emailID)["$draft"] != true {
		t.Fatal("sibling did not set draft")
	}
	if emailKeywords(t, bound.Jmap, session, accountID, otherID)["$seen"] == true {
		t.Fatal("other message changed")
	}

	im.cmd("CREATE Copies").ok(t)
	boxes = jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Mailbox/get", map[string]any{"accountId": accountID, "ids": nil}, "0"},
	})
	var copiesID string
	for _, item := range arrayField(t, methodArgs(t, boxes, "Mailbox/get"), "list") {
		obj := item.(map[string]any)
		if obj["name"] == "Copies" {
			copiesID = obj["id"].(string)
		}
	}
	if copiesID == "" {
		t.Fatal("no Copies")
	}
	unknownBox := emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"mailboxIds/no-such-box": true, "keywords/$seen": true},
	}, nil)
	if objectField(t, objectField(t, unknownBox, "notUpdated"), emailID)["type"] != "invalidProperties" {
		t.Fatalf("unknown box %+v", unknownBox)
	}
	if emailKeywords(t, bound.Jmap, session, accountID, emailID)["$seen"] == true {
		t.Fatal("unknown box set seen")
	}
	nullBox := emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"mailboxIds/no-such-box": nil, "keywords/$seen": true},
	}, nil)
	if objectField(t, objectField(t, nullBox, "notUpdated"), emailID)["type"] != "invalidProperties" {
		t.Fatalf("unknown null %+v", nullBox)
	}
	if _, ok := nullBox["updated"].(map[string]any)[emailID]; ok {
		t.Fatalf("unknown null updated %+v", nullBox)
	}
	if emailKeywords(t, bound.Jmap, session, accountID, emailID)["$seen"] == true {
		t.Fatal("unknown null set seen")
	}

	copies := dial(t, bound.Listen)
	copies.cmd("LOGIN demo secret").ok(t)
	copies.cmd("SELECT Copies").ok(t)
	copies.send("c1 IDLE\r\n")
	for {
		rec := copies.readRec()
		if strings.HasPrefix(strings.TrimRight(rec, "\r\n"), "+") {
			break
		}
	}
	mark := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	added := emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"mailboxIds/" + copiesID: true},
	}, nil)
	if _, ok := added["updated"].(map[string]any)[emailID]; !ok {
		t.Fatalf("add %+v", added)
	}
	deadline := time.Now().Add(5 * time.Second)
	var sawExists bool
	for time.Now().Before(deadline) {
		copies.c.SetReadDeadline(time.Now().Add(time.Second))
		rec := copies.readRec()
		if strings.Contains(rec, "EXISTS") {
			sawExists = true
			break
		}
	}
	if !sawExists {
		t.Fatal("IDLE did not see EXISTS")
	}
	copies.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	copies.send("DONE\r\n")
	copies.readTag("c1").ok(t)
	copyFlags := strings.Join(copies.cmd("FETCH 1 (FLAGS)").mustOK(t).untagged, " ")
	if !strings.Contains(copyFlags, `\Flagged`) || !strings.Contains(copyFlags, `\Draft`) || strings.Contains(copyFlags, `\Seen`) {
		t.Fatalf("copy flags %s", copyFlags)
	}
	addCh := methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": mark}, "0"},
	}), "Email/changes")
	if strings.Join(stringArray(t, addCh["updated"]), ",") != emailID ||
		len(stringArray(t, addCh["created"]))+len(stringArray(t, addCh["destroyed"])) != 0 {
		t.Fatalf("add changes %+v", addCh)
	}
	if !queryHas(t, bound.Jmap, session, accountID, inboxID, emailID) || !queryHas(t, bound.Jmap, session, accountID, copiesID, emailID) {
		t.Fatal("query missing a mailbox")
	}
	mbox := emailMailboxes(t, bound.Jmap, session, accountID, emailID)
	if mbox[inboxID] != true || mbox[copiesID] != true {
		t.Fatalf("mailboxIds %+v", mbox)
	}
	keptKw := emailKeywords(t, bound.Jmap, session, accountID, emailID)
	dropUnknown := emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{
			"mailboxIds/no-such-box": nil,
			"mailboxIds/" + copiesID: nil,
			"keywords/$seen":         true,
		},
	}, nil)
	if objectField(t, objectField(t, dropUnknown, "notUpdated"), emailID)["type"] != "invalidProperties" {
		t.Fatalf("unknown null with changes %+v", dropUnknown)
	}
	if queryHas(t, bound.Jmap, session, accountID, inboxID, emailID) != true || !queryHas(t, bound.Jmap, session, accountID, copiesID, emailID) {
		t.Fatal("unknown null changed mailboxes")
	}
	if kw := emailKeywords(t, bound.Jmap, session, accountID, emailID); kw["$seen"] == true || kw["$flagged"] != keptKw["$flagged"] {
		t.Fatalf("unknown null changed keywords %+v", kw)
	}

	im.send("i1 IDLE\r\n")
	for {
		rec := im.readRec()
		if strings.HasPrefix(strings.TrimRight(rec, "\r\n"), "+") {
			break
		}
	}
	removed := emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"mailboxIds/" + inboxID: nil},
	}, nil)
	if _, ok := removed["updated"].(map[string]any)[emailID]; !ok {
		t.Fatalf("remove %+v", removed)
	}
	deadline = time.Now().Add(5 * time.Second)
	var sawExpunge bool
	for time.Now().Before(deadline) {
		im.c.SetReadDeadline(time.Now().Add(time.Second))
		rec := im.readRec()
		if strings.Contains(rec, "EXPUNGE") {
			sawExpunge = true
			break
		}
	}
	if !sawExpunge {
		t.Fatal("IDLE did not see EXPUNGE")
	}
	im.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	im.send("DONE\r\n")
	im.readTag("i1").ok(t)
	if queryHas(t, bound.Jmap, session, accountID, inboxID, emailID) || !queryHas(t, bound.Jmap, session, accountID, copiesID, emailID) {
		t.Fatal("membership after remove")
	}
	mbox = emailMailboxes(t, bound.Jmap, session, accountID, emailID)
	if mbox[copiesID] != true || mbox[inboxID] == true {
		t.Fatalf("mailboxIds after remove %+v", mbox)
	}
	last := emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"mailboxIds/" + copiesID: nil, "keywords/$seen": true},
	}, nil)
	if objectField(t, objectField(t, last, "notUpdated"), emailID)["type"] != "invalidProperties" {
		t.Fatalf("last mailbox %+v", last)
	}
	if !queryHas(t, bound.Jmap, session, accountID, copiesID, emailID) {
		t.Fatal("last mailbox removed")
	}
	if emailKeywords(t, bound.Jmap, session, accountID, emailID)["$seen"] == true {
		t.Fatal("last mailbox changed flags")
	}

	state := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	created := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/set", map[string]any{
			"accountId": accountID,
			"create":    map[string]any{"k": map[string]any{"mailboxIds": map[string]any{inboxID: true}}},
			"update":    map[string]any{emailID: map[string]any{"keywords/$seen": true}},
			"destroy":   []any{otherID},
		}, "0"},
	})
	if methodType(t, created, "0") != "invalidArguments" {
		t.Fatalf("create %+v", created)
	}
	if jmapSession(t, bound.Jmap, "demo", "secret")["state"] != state {
		t.Fatal("create changed state")
	}
	if emailKeywords(t, bound.Jmap, session, accountID, emailID)["$seen"] == true {
		t.Fatal("create updated flags")
	}
	if !queryHas(t, bound.Jmap, session, accountID, inboxID, otherID) {
		t.Fatal("create destroyed")
	}

	both := emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"keywords/$seen": true},
	}, []any{otherID, "missing-email"})
	if _, ok := both["updated"].(map[string]any)[emailID]; !ok {
		t.Fatalf("update with destroy %+v", both)
	}
	if strings.Join(stringArray(t, both["destroyed"]), ",") != otherID {
		t.Fatalf("destroyed %+v", both)
	}
	if strings.Join(stringArray(t, both["notFound"]), ",") != "missing-email" {
		t.Fatalf("notFound %+v", both)
	}
	if emailKeywords(t, bound.Jmap, session, accountID, emailID)["$seen"] != true {
		t.Fatal("update with destroy did not set seen")
	}
	if queryHas(t, bound.Jmap, session, accountID, inboxID, otherID) {
		t.Fatal("destroyed id remains")
	}
	gone := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/set", map[string]any{"accountId": accountID, "destroy": []any{emailID}}, "0"},
	})
	if strings.Join(stringArray(t, methodArgs(t, gone, "Email/set")["destroyed"]), ",") != emailID {
		t.Fatalf("destroy copies %+v", gone)
	}
	if queryHas(t, bound.Jmap, session, accountID, copiesID, emailID) {
		t.Fatal("destroy left a copy")
	}
}

func TestJMAPCopyRemainsAfterIMAPRemove(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", JmapListen: "127.0.0.1:0"})
	session := jmapSession(t, bound.Jmap, "demo", "secret")
	accountID := objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:mail"].(string)
	inboxID := mailboxID(t, bound.Jmap, session, accountID, "INBOX")
	raw := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: copy\r\n\r\ncopy-body\r\n")
	res, upBody := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodPost, "/jmap/upload/"+accountID+"/", "message/rfc822", raw)
	if res.StatusCode != 201 {
		t.Fatalf("upload %d %s", res.StatusCode, upBody)
	}
	blobID := jmapJSON(t, upBody)["blobId"].(string)
	imported := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/import", map[string]any{
			"accountId": accountID,
			"emails": map[string]any{
				"k1": map[string]any{"blobId": blobID, "mailboxIds": map[string]any{inboxID: true}},
			},
		}, "0"},
	})
	emailID := objectField(t, objectField(t, methodArgs(t, imported, "Email/import"), "created"), "k1")["id"].(string)
	im := dial(t, bound.Listen)
	im.cmd("LOGIN demo secret").ok(t)
	im.cmd("CREATE Copies").ok(t)
	copiesID := mailboxID(t, bound.Jmap, session, accountID, "Copies")
	emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"mailboxIds/" + copiesID: true},
	}, nil)

	exists, _, _, _ := parseSelect(t, im.cmd("SELECT INBOX").mustOK(t))
	im.cmd("STORE " + strconv.Itoa(exists) + ` +FLAGS (\Deleted)`).ok(t)
	mark := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	im.cmd("EXPUNGE").ok(t)
	assertCopyRemains(t, bound.Jmap, session, accountID, mark, emailID, copiesID)
	if queryHas(t, bound.Jmap, session, accountID, inboxID, emailID) {
		t.Fatal("expunge left the inbox copy")
	}

	emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"mailboxIds/" + inboxID: true},
	}, nil)
	im.cmd("CREATE Third").ok(t)
	exists, _, _, _ = parseSelect(t, im.cmd("SELECT INBOX").mustOK(t))
	mark = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	im.cmd("MOVE " + strconv.Itoa(exists) + " Third").ok(t)
	ch := assertCopyRemains(t, bound.Jmap, session, accountID, mark, emailID, copiesID)
	if len(stringArray(t, ch["created"])) != 1 || stringArray(t, ch["created"])[0] == emailID {
		t.Fatalf("move created %+v", ch)
	}

	im.cmd("CREATE CloseBox").ok(t)
	closeID := mailboxID(t, bound.Jmap, session, accountID, "CloseBox")
	emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"mailboxIds/" + closeID: true},
	}, nil)
	im.cmd("SELECT CloseBox").ok(t)
	im.cmd(`STORE 1 +FLAGS (\Deleted)`).ok(t)
	mark = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	im.cmd("CLOSE").ok(t)
	assertCopyRemains(t, bound.Jmap, session, accountID, mark, emailID, copiesID)
	if queryHas(t, bound.Jmap, session, accountID, closeID, emailID) {
		t.Fatal("close left the copy")
	}

	im.cmd("CREATE Drop").ok(t)
	dropID := mailboxID(t, bound.Jmap, session, accountID, "Drop")
	emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"mailboxIds/" + dropID: true},
	}, nil)
	mark = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	im.cmd("DELETE Drop").ok(t)
	assertCopyRemains(t, bound.Jmap, session, accountID, mark, emailID, copiesID)
	if _, ok := emailMailboxes(t, bound.Jmap, session, accountID, emailID)[dropID]; ok {
		t.Fatal("delete left the mailbox on the email")
	}
}

func assertCopyRemains(t *testing.T, addr string, session map[string]any, accountID, since, emailID, keepBox string) map[string]any {
	t.Helper()
	ch := methodArgs(t, jmapCall(t, addr, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": since}, "0"},
	}), "Email/changes")
	updated := stringArray(t, ch["updated"])
	destroyed := stringArray(t, ch["destroyed"])
	if strings.Join(updated, ",") != emailID || len(destroyed) != 0 {
		t.Fatalf("changes %+v", ch)
	}
	if !queryHas(t, addr, session, accountID, keepBox, emailID) {
		t.Fatal("remaining mailbox lost the id")
	}
	if len(emailList(t, jmapCall(t, addr, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{"accountId": accountID, "ids": []any{emailID}, "properties": []any{"id"}}, "0"},
	}))) != 1 {
		t.Fatal("Email/get lost the id")
	}
	return ch
}

func mailboxID(t *testing.T, addr string, session map[string]any, accountID, name string) string {
	t.Helper()
	boxes := jmapCall(t, addr, session, "demo", "secret", []any{
		[]any{"Mailbox/get", map[string]any{"accountId": accountID, "ids": nil}, "0"},
	})
	for _, item := range arrayField(t, methodArgs(t, boxes, "Mailbox/get"), "list") {
		obj := item.(map[string]any)
		if obj["name"] == name {
			return obj["id"].(string)
		}
	}
	t.Fatalf("no mailbox %s", name)
	return ""
}

func emailSet(t *testing.T, addr string, session map[string]any, accountID string, update map[string]any, destroy []any) map[string]any {
	t.Helper()
	args := map[string]any{"accountId": accountID}
	if update != nil {
		args["update"] = update
	}
	if destroy != nil {
		args["destroy"] = destroy
	}
	return methodArgs(t, jmapCall(t, addr, session, "demo", "secret", []any{
		[]any{"Email/set", args, "0"},
	}), "Email/set")
}

func emailKeywords(t *testing.T, addr string, session map[string]any, accountID, id string) map[string]any {
	t.Helper()
	row := emailList(t, jmapCall(t, addr, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{
			"accountId":  accountID,
			"ids":        []any{id},
			"properties": []any{"keywords"},
		}, "0"},
	}))[0]
	kw, _ := row["keywords"].(map[string]any)
	if kw == nil {
		return map[string]any{}
	}
	return kw
}

func emailMailboxes(t *testing.T, addr string, session map[string]any, accountID, id string) map[string]any {
	t.Helper()
	row := emailList(t, jmapCall(t, addr, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{
			"accountId":  accountID,
			"ids":        []any{id},
			"properties": []any{"mailboxIds"},
		}, "0"},
	}))[0]
	return objectField(t, row, "mailboxIds")
}

func queryHas(t *testing.T, addr string, session map[string]any, accountID, boxID, emailID string) bool {
	t.Helper()
	ids := stringArray(t, methodArgs(t, jmapCall(t, addr, session, "demo", "secret", []any{
		[]any{"Email/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"inMailbox": boxID},
		}, "0"},
	}), "Email/query")["ids"])
	for _, id := range ids {
		if id == emailID {
			return true
		}
	}
	return false
}

func jsonBool(t *testing.T, v any) bool {
	t.Helper()
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("not bool %T %v", v, v)
	}
	return b
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

func TestJMAPSubmission(t *testing.T) {
	_, bound, cc := startCtrl(t, Config{Listen: "127.0.0.1:0", JmapListen: "127.0.0.1:0"})
	session := jmapSession(t, bound.Jmap, "demo", "secret")
	accountID := objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:mail"].(string)
	if _, ok := objectField(t, session, "capabilities")["urn:ietf:params:jmap:submission"]; !ok {
		t.Fatal("submission missing")
	}
	acc := objectField(t, session["accounts"].(map[string]any)[accountID].(map[string]any), "accountCapabilities")
	if _, ok := acc["urn:ietf:params:jmap:submission"]; !ok {
		t.Fatal("account submission missing")
	}

	ident := identityRow(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Identity/get", map[string]any{
			"accountId":  accountID,
			"properties": []any{"id", "email"},
		}, "0"},
	}))
	if len(ident) != 2 || ident["email"] != "demo" || ident["id"] == "" {
		t.Fatalf("identity %+v", ident)
	}
	again := identityRow(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Identity/get", map[string]any{
			"accountId":  accountID,
			"properties": []any{"id", "email"},
		}, "0"},
	}))
	if again["id"] != ident["id"] || again["email"] != "demo" || len(again) != 2 {
		t.Fatalf("identity changed %+v", again)
	}

	var inboxID string
	boxes := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Mailbox/get", map[string]any{"accountId": accountID, "ids": nil}, "0"},
	})
	for _, item := range arrayField(t, methodArgs(t, boxes, "Mailbox/get"), "list") {
		obj := item.(map[string]any)
		if obj["name"] == "INBOX" {
			inboxID = obj["id"].(string)
		}
	}
	if inboxID == "" {
		t.Fatal("no inbox")
	}
	importMail := func(raw []byte, cid string) string {
		t.Helper()
		res, upBody := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodPost, "/jmap/upload/"+accountID+"/", "message/rfc822", raw)
		if res.StatusCode != 201 {
			t.Fatalf("upload %d %s", res.StatusCode, upBody)
		}
		blobID, _ := jmapJSON(t, upBody)["blobId"].(string)
		imported := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
			[]any{"Email/import", map[string]any{
				"accountId": accountID,
				"emails": map[string]any{
					cid: map[string]any{"blobId": blobID, "mailboxIds": map[string]any{inboxID: true}},
				},
			}, "0"},
		})
		return objectField(t, objectField(t, methodArgs(t, imported, "Email/import"), "created"), cid)["id"].(string)
	}
	rawA := []byte("From: a@example.com\r\nTo: demo\r\nSubject: sub-a\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nbody-a\r\n")
	rawB := []byte("From: a@example.com\r\nTo: demo\r\nSubject: sub-b\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nbody-b\r\n")
	emailA := importMail(rawA, "a")
	emailB := importMail(rawB, "b")
	base := inboxCount(t, cc)
	env := func(from string, rcpts ...string) map[string]any {
		var to []any
		for _, rcpt := range rcpts {
			to = append(to, map[string]any{"email": rcpt})
		}
		return map[string]any{
			"mailFrom": map[string]any{"email": from},
			"rcptTo":   to,
		}
	}

	bad := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": accountID,
			"create": map[string]any{
				"u": map[string]any{"emailId": "no-such-email", "envelope": env("sender@example.com", "demo")},
				"e": map[string]any{"emailId": emailA, "envelope": map[string]any{
					"mailFrom": map[string]any{"email": "sender@example.com"},
					"rcptTo":   []any{},
				}},
				"m": map[string]any{"emailId": emailA},
				"i": map[string]any{"emailId": emailA, "identityId": "other", "envelope": env("sender@example.com", "demo")},
			},
			"onSuccessDestroyEmail": []any{"#u"},
		}, "0"},
	})
	badArgs := methodArgs(t, bad, "EmailSubmission/set")
	badCreated := objectField(t, badArgs, "created")
	badNot := objectField(t, badArgs, "notCreated")
	for _, id := range []string{"u", "e", "m", "i"} {
		if _, ok := badCreated[id]; ok {
			t.Fatalf("created %s %+v", id, badCreated)
		}
		if badNot[id] == nil {
			t.Fatalf("missing notCreated %s %+v", id, badNot)
		}
	}
	if objectField(t, badNot, "u")["type"] != "notFound" {
		t.Fatalf("unknown email %+v", badNot["u"])
	}
	if objectField(t, badNot, "e")["type"] != "invalidProperties" || objectField(t, badNot, "m")["type"] != "invalidProperties" {
		t.Fatalf("envelope %+v", badNot)
	}
	if len(arrayField(t, bad, "methodResponses")) != 1 {
		t.Fatalf("failed create destroyed %+v", bad)
	}
	if inboxCount(t, cc) != base || len(listSMTP(t, cc).Messages) != 0 {
		t.Fatal("rejected submission delivered")
	}
	if len(emailList(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{"accountId": accountID, "ids": []any{emailA}, "properties": []any{"id"}}, "0"},
	}))) != 1 {
		t.Fatal("rejected submission destroyed the email")
	}
	noneYet := stringArray(t, methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"emailIds": []any{emailA, emailB}},
		}, "0"},
	}), "EmailSubmission/query")["ids"])
	if len(noneYet) != 0 {
		t.Fatalf("query before create %+v", noneYet)
	}

	sentA := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": accountID,
			"create": map[string]any{
				"k1": map[string]any{
					"emailId":    emailA,
					"identityId": ident["id"],
					"envelope":   env("sender@example.com", "demo", "stranger"),
				},
			},
		}, "0"},
	})
	subA := objectField(t, objectField(t, methodArgs(t, sentA, "EmailSubmission/set"), "created"), "k1")["id"].(string)
	if subA == "" {
		t.Fatalf("created %+v", sentA)
	}
	if inboxCount(t, cc) != base+1 {
		t.Fatalf("inbox %d want %d", inboxCount(t, cc), base+1)
	}
	msgs := cc.call(map[string]any{"op": "list-mail", "mailbox": "INBOX"}).ok(t).Messages
	if !bytes.Equal(fetchRFC822(t, cc, msgs[len(msgs)-1].UID), rawA) {
		t.Fatal("delivered bytes")
	}
	log := listSMTP(t, cc)
	if len(log.Messages) != 1 || log.Messages[0].From != "sender@example.com" || len(log.Messages[0].Recipients) != 2 {
		t.Fatalf("list-smtp %+v", log.Messages)
	}
	if log.Messages[0].Recipients[0].Address != "demo" || log.Messages[0].Recipients[0].Result != "delivered" {
		t.Fatalf("dataset rcpt %+v", log.Messages[0].Recipients[0])
	}
	if log.Messages[0].Recipients[1].Address != "stranger" || log.Messages[0].Recipients[1].Result != "discarded" {
		t.Fatalf("other rcpt %+v", log.Messages[0].Recipients[1])
	}

	cc2 := dialCtrl(t, bound.Control)
	done := make(chan ctrlReply, 1)
	go func() {
		done <- cc2.call(map[string]any{"op": "wait-smtp", "count": 1, "timeout_ms": 3000})
	}()
	time.Sleep(50 * time.Millisecond)
	sentB := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": accountID,
			"create": map[string]any{
				"k1": map[string]any{"emailId": emailB, "envelope": env("b-sender@example.com", "other@example.com")},
			},
		}, "0"},
	})
	subB := objectField(t, objectField(t, methodArgs(t, sentB, "EmailSubmission/set"), "created"), "k1")["id"].(string)
	if subB == "" || subB == subA {
		t.Fatalf("submission b %q", subB)
	}
	select {
	case rp := <-done:
		if !rp.OK {
			t.Fatalf("wait-smtp %+v", rp)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("wait-smtp did not count the submission")
	}
	if inboxCount(t, cc) != base+1 {
		t.Fatal("discarded recipient was stored")
	}
	log = listSMTP(t, cc)
	if len(log.Messages) != 2 || log.Messages[1].From != "b-sender@example.com" || log.Messages[1].Recipients[0].Result != "discarded" {
		t.Fatalf("second log %+v", log.Messages)
	}

	qA := stringArray(t, methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"emailIds": []any{emailA}},
		}, "0"},
	}), "EmailSubmission/query")["ids"])
	qB := stringArray(t, methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"emailIds": []any{emailB}},
		}, "0"},
	}), "EmailSubmission/query")["ids"])
	qBoth := stringArray(t, methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"emailIds": []any{emailA, emailB}},
		}, "0"},
	}), "EmailSubmission/query")["ids"])
	qMiss := stringArray(t, methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"emailIds": []any{"no-such-email"}},
		}, "0"},
	}), "EmailSubmission/query")["ids"])
	if strings.Join(qA, ",") != subA || strings.Join(qB, ",") != subB {
		t.Fatalf("query A %v B %v", qA, qB)
	}
	if strings.Join(qBoth, ",") != subA+","+subB || len(qMiss) != 0 {
		t.Fatalf("query both %v miss %v", qBoth, qMiss)
	}

	held := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": accountID,
			"create": map[string]any{
				"k1": map[string]any{"emailId": emailB, "envelope": env("b-sender@example.com", "other@example.com")},
			},
		}, "s"},
	})
	if objectField(t, objectField(t, methodArgs(t, held, "EmailSubmission/set"), "created"), "k1")["id"] == "" {
		t.Fatalf("hold %+v", held)
	}
	if len(emailList(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{"accountId": accountID, "ids": []any{emailB}, "properties": []any{"id"}}, "0"},
	}))) != 1 {
		t.Fatal("email destroyed without onSuccessDestroyEmail")
	}

	gone := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId":             accountID,
			"onSuccessDestroyEmail": []any{"#k1"},
			"create": map[string]any{
				"k1": map[string]any{"emailId": emailA, "envelope": env("sender@example.com", "nobody@example.com")},
			},
		}, "d"},
	})
	var names []string
	for _, item := range arrayField(t, gone, "methodResponses") {
		names = append(names, item.([]any)[0].(string))
	}
	if strings.Join(names, ",") != "EmailSubmission/set,Email/set" {
		t.Fatalf("responses %v", names)
	}
	if strings.Join(stringArray(t, methodArgs(t, gone, "Email/set")["destroyed"]), ",") != emailA {
		t.Fatalf("destroyed %+v", methodArgs(t, gone, "Email/set"))
	}
	after := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/get", map[string]any{"accountId": accountID, "ids": []any{emailA, emailB}, "properties": []any{"id"}}, "0"},
	})
	if len(emailList(t, after)) != 1 || emailList(t, after)[0]["id"] != emailB {
		t.Fatalf("after destroy %+v", after)
	}
	if strings.Join(stringArray(t, methodArgs(t, after, "Email/get")["notFound"]), ",") != emailA {
		t.Fatalf("notFound %+v", methodArgs(t, after, "Email/get"))
	}
	left := stringArray(t, methodArgs(t, jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"EmailSubmission/query", map[string]any{
			"accountId": accountID,
			"filter":    map[string]any{"emailIds": []any{emailB}},
		}, "0"},
	}), "EmailSubmission/query")["ids"])
	for _, id := range left {
		if id == subA {
			t.Fatalf("query leaked %v", left)
		}
	}
	if len(left) != 2 || left[0] != subB {
		t.Fatalf("emailB submissions %v", left)
	}
}

func identityRow(t *testing.T, root map[string]any) map[string]any {
	t.Helper()
	list := arrayField(t, methodArgs(t, root, "Identity/get"), "list")
	if len(list) != 1 {
		t.Fatalf("identities %+v", list)
	}
	return list[0].(map[string]any)
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
		"using":       []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail", "urn:ietf:params:jmap:submission"},
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
	ct := ""
	if body != nil {
		ct = "application/json"
	}
	return jmapDo(t, addr, user, pass, method, path, ct, body)
}

func jmapDo(t *testing.T, addr, user, pass, method, path, contentType string, body []byte) (*http.Response, []byte) {
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
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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

func TestJMAPEventSource(t *testing.T) {
	_, bound := start(t, Config{Listen: "127.0.0.1:0", JmapListen: "127.0.0.1:0"})
	path := "/jmap/eventsource/?types=Email&closeafter=state&ping=120"
	res, raw := jmapHTTP(t, bound.Jmap, "", "", http.MethodGet, path, nil)
	if res.StatusCode != 401 {
		t.Fatalf("missing auth %d %s", res.StatusCode, raw)
	}
	res, raw = jmapHTTP(t, bound.Jmap, "demo", "wrong", http.MethodGet, path, nil)
	if res.StatusCode != 401 {
		t.Fatalf("wrong password %d %s", res.StatusCode, raw)
	}

	session := jmapSession(t, bound.Jmap, "demo", "secret")
	accountID := objectField(t, session, "primaryAccounts")["urn:ietf:params:jmap:mail"].(string)
	inboxID := mailboxID(t, bound.Jmap, session, accountID, "INBOX")
	im := dial(t, bound.Listen)
	im.cmd("LOGIN demo secret").ok(t)
	exists, _, _, _ := parseSelect(t, im.cmd("SELECT INBOX").mustOK(t))

	before := jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	stream := openJMAPEvents(t, bound.Jmap, "demo", "secret", "types=Email&closeafter=state&ping=120")
	msg := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: sse\r\n\r\nsse-append\r\n")
	im.cmd("APPEND INBOX {" + strconv.Itoa(len(msg)) + "+}\r\n" + string(msg)).ok(t)
	exists++
	expectClosedEmailState(t, bound.Jmap, session, accountID, before, stream)

	flagSeq := exists + 1
	im.cmd("APPEND INBOX {4+}\r\nflag").ok(t)
	before = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	stream = openJMAPEvents(t, bound.Jmap, "demo", "secret", "types=Mailbox,Email&closeafter=state&ping=120")
	im.cmd("STORE " + strconv.Itoa(flagSeq) + " +FLAGS (\\Flagged)").ok(t)
	expectClosedEmailState(t, bound.Jmap, session, accountID, before, stream)

	expSeq := flagSeq + 1
	im.cmd("APPEND INBOX {7+}\r\nexpunge").ok(t)
	im.cmd("STORE " + strconv.Itoa(expSeq) + " +FLAGS (\\Deleted)").ok(t)
	before = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	stream = openJMAPEvents(t, bound.Jmap, "demo", "secret", "types=Email&closeafter=state&ping=120")
	im.cmd("EXPUNGE").ok(t)
	expectClosedEmailState(t, bound.Jmap, session, accountID, before, stream)

	rawMail := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: import\r\n\r\nimported\r\n")
	res, upBody := jmapDo(t, bound.Jmap, "demo", "secret", http.MethodPost, "/jmap/upload/"+accountID+"/", "message/rfc822", rawMail)
	if res.StatusCode != 201 {
		t.Fatalf("upload %d %s", res.StatusCode, upBody)
	}
	blobID := jmapJSON(t, upBody)["blobId"].(string)
	before = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	stream = openJMAPEvents(t, bound.Jmap, "demo", "secret", "types=Email&closeafter=state&ping=120")
	imported := jmapCall(t, bound.Jmap, session, "demo", "secret", []any{
		[]any{"Email/import", map[string]any{
			"accountId": accountID,
			"emails": map[string]any{
				"k1": map[string]any{"blobId": blobID, "mailboxIds": map[string]any{inboxID: true}},
			},
		}, "0"},
	})
	emailID := objectField(t, objectField(t, methodArgs(t, imported, "Email/import"), "created"), "k1")["id"].(string)
	expectClosedEmailState(t, bound.Jmap, session, accountID, before, stream)

	before = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	stream = openJMAPEvents(t, bound.Jmap, "demo", "secret", "types=Email&closeafter=state&ping=120")
	emailSet(t, bound.Jmap, session, accountID, map[string]any{
		emailID: map[string]any{"keywords/$seen": true},
	}, nil)
	expectClosedEmailState(t, bound.Jmap, session, accountID, before, stream)

	before = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	stream = openJMAPEvents(t, bound.Jmap, "demo", "secret", "types=Email&closeafter=state&ping=120")
	emailSet(t, bound.Jmap, session, accountID, nil, []any{emailID})
	expectClosedEmailState(t, bound.Jmap, session, accountID, before, stream)

	before = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	stream = openJMAPEvents(t, bound.Jmap, "demo", "secret", "types=Email&closeafter=state&ping=1")
	block, eof := stream.next(4 * time.Second)
	if eof || block != ": ping\n\n" {
		t.Fatalf("ping %q eof %v", block, eof)
	}
	im.cmd("APPEND INBOX {4+}\r\nping").ok(t)
	for {
		block, eof = stream.next(3 * time.Second)
		if strings.HasPrefix(block, "event: state\n") {
			break
		}
		if eof || block != ": ping\n\n" {
			t.Fatalf("after ping %q eof %v", block, eof)
		}
	}
	if got := stateEventEmail(t, block, accountID); got != emailChangesState(t, bound.Jmap, session, accountID, before) {
		t.Fatalf("ping stream state %s", got)
	}
	if _, eof = stream.next(2 * time.Second); !eof {
		t.Fatal("closeafter=state left the stream open")
	}

	stream = openJMAPEvents(t, bound.Jmap, "demo", "secret", "types=Mailbox&closeafter=state&ping=1")
	im.cmd("APPEND INBOX {4+}\r\nnone").ok(t)
	deadline := time.Now().Add(2500 * time.Millisecond)
	sawPing := false
	for time.Now().Before(deadline) {
		block, eof = stream.next(time.Until(deadline))
		if eof {
			t.Fatalf("types without Email closed %q", block)
		}
		if block == "" {
			break
		}
		if strings.Contains(block, "event: state") || block != ": ping\n\n" {
			t.Fatalf("types block %q", block)
		}
		sawPing = true
	}
	if !sawPing {
		t.Fatal("no ping while types excluded Email")
	}

	before = jmapSession(t, bound.Jmap, "demo", "secret")["state"].(string)
	stream = openJMAPEvents(t, bound.Jmap, "demo", "secret", "types=Email&closeafter=no&ping=120")
	im.cmd("APPEND INBOX {4+}\r\nstay").ok(t)
	block, eof = stream.next(3 * time.Second)
	if eof {
		t.Fatal("closeafter other than state closed before the event")
	}
	if got := stateEventEmail(t, block, accountID); got != emailChangesState(t, bound.Jmap, session, accountID, before) {
		t.Fatalf("open stream state %s", got)
	}
	if block, eof = stream.next(300 * time.Millisecond); eof || block != "" {
		t.Fatalf("closeafter other than state ended the stream %q eof %v", block, eof)
	}
}

type sseBlock struct {
	text string
	err  error
}

type sseStream struct {
	blocks <-chan sseBlock
}

func openJMAPEvents(t *testing.T, addr, user, pass, query string) *sseStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/jmap/eventsource/?"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.SetBasicAuth(user, pass)
	cl := &http.Client{Transport: &http.Transport{
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 2 * time.Second,
	}}
	res, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	if res.StatusCode != 200 {
		t.Fatalf("event source %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	blocks := make(chan sseBlock, 8)
	go readSSE(res.Body, blocks)
	return &sseStream{blocks: blocks}
}

func readSSE(body io.Reader, out chan<- sseBlock) {
	br := bufio.NewReader(body)
	var b strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if b.Len() > 0 {
				out <- sseBlock{text: b.String(), err: err}
			}
			out <- sseBlock{err: err}
			close(out)
			return
		}
		b.WriteString(line)
		if line == "\n" {
			out <- sseBlock{text: b.String()}
			b.Reset()
		}
	}
}

func (s *sseStream) next(wait time.Duration) (string, bool) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case b, ok := <-s.blocks:
		if !ok || (b.err != nil && b.text == "") {
			return "", true
		}
		return b.text, false
	case <-timer.C:
		return "", false
	}
}

func stateEventEmail(t *testing.T, block, accountID string) string {
	t.Helper()
	if !strings.HasPrefix(block, "event: state\n") {
		t.Fatalf("event %q", block)
	}
	var data strings.Builder
	for _, line := range strings.Split(block, "\n") {
		if rest, ok := strings.CutPrefix(line, "data: "); ok {
			data.WriteString(rest)
		}
	}
	obj := jmapJSON(t, []byte(data.String()))
	email, _ := objectField(t, objectField(t, obj, "changed"), accountID)["Email"].(string)
	if email == "" {
		t.Fatalf("event email %q", block)
	}
	return email
}

func emailChangesState(t *testing.T, addr string, session map[string]any, accountID, since string) string {
	t.Helper()
	ch := methodArgs(t, jmapCall(t, addr, session, "demo", "secret", []any{
		[]any{"Email/changes", map[string]any{"accountId": accountID, "sinceState": since}, "0"},
	}), "Email/changes")
	state, _ := ch["newState"].(string)
	if state == "" || state == since {
		t.Fatalf("changes state %+v", ch)
	}
	return state
}

func expectClosedEmailState(t *testing.T, addr string, session map[string]any, accountID, before string, stream *sseStream) {
	t.Helper()
	block, eof := stream.next(3 * time.Second)
	if eof {
		t.Fatalf("closed before event %q", block)
	}
	if got := stateEventEmail(t, block, accountID); got != emailChangesState(t, addr, session, accountID, before) {
		t.Fatalf("event %s", got)
	}
	if _, eof = stream.next(2 * time.Second); !eof {
		t.Fatal("closeafter=state left the stream open")
	}
}
