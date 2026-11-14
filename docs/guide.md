# fakeimap

Developer notes for running and extending the server. This is not a user manual. It stays English and is not a translation-catalog entry.

Long protocol rules — search keys, FETCH items, JMAP methods, ManageSieve script commands, and each probe script — are in `docs/facts/imap-server.md`. This page does not paste that file.

## How to use

`make` builds `fakeimap` and `cyrusprobe`. It runs the Go tests and does not start Docker. No certificate is in this repo. Generate a certificate outside the tree when a listener needs TLS.

```
fakeimap <config.json>
```

The one argument is the config path. Paths inside the config are relative to the process working directory, not to the config file.

Before accept, the process writes one JSON object to stdout and flushes it. The object names the listeners that bound: `listen`, `implicit_tls_listen`, `control`, `smtp`, `smtp_listeners`, `smtp_tls`, `smtp_tls_listeners`, `jmap`, `jmap_tls`, and `sieve`. A configured port of `0` is the bound ephemeral port. `smtp` and `smtp_tls` are the first bind of each kind. The fields are defined in `docs/facts/imap-server.md`.

A short harness reads that object, writes one `arm-fault` line to the mode-0600 control socket, and writes `reset`:

```python
import json, socket, sys
ready = json.loads(sys.stdin.readline())
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.connect(ready["control"])
f = s.makefile("rw")
f.write(json.dumps({
    "op": "arm-fault", "do": "silent", "command": "NOOP", "times": 1,
}) + "\n")
f.flush()
print(f.readline(), end="")
f.write(json.dumps({"op": "reset"}) + "\n")
f.flush()
print(f.readline(), end="")
```

## Config

Each key is defined in `docs/facts/imap-server.md`. Summary:

- `listen` — `host:port` cleartext IMAP. Empty or omitted: no cleartext listener. Port `0`: ephemeral.
- `implicit_tls_listen` — `host:port` implicit TLS. Empty or omitted: no implicit-TLS listener. Port `0`: ephemeral.
- `cert_file` — PEM certificate for `STARTTLS` and implicit TLS. Not part of the repo.
- `key_file` — PEM private key matching `cert_file`. Set together with `cert_file`.
- `client_ca_file` — PEM trust anchors for an optional client certificate, used when the personality lists `AUTH=EXTERNAL` and a server certificate is loaded.
- `log_path` — file to append. Empty or omitted: no log file.
- `personality` — personality JSON file. Required.
- `dataset` — dataset directory containing `dataset.json`. Required.
- `control` — filesystem path of the mode-0600 Unix control socket. Empty or omitted: no socket.
- `smtp_listen` — one `host:port`, or an array of `host:port` strings and `{"addr","dsn"}` objects. Empty or omitted: not bound. Port `0`: ephemeral. Optional `smtp_dsn` applies to the string form. See `docs/facts/imap-server.md`.
- `smtp_tls_listen` — SMTP with TLS from the first byte. A string or an array. Empty or omitted: not bound. Requires `cert_file` and `key_file`.
- `smtp_auth_required` — default true. False accepts `MAIL`, `RCPT`, and `DATA` with no AUTH and does not deliver them.
- `smtp_size` — EHLO `SIZE` limit. Omitted means `10000000`.
- `jmap_listen` — `host:port` cleartext HTTP JMAP. Empty or omitted: not bound. Port `0`: ephemeral. It does not speak TLS.
- `jmap_tls_listen` — `host:port` JMAP with TLS from the first byte. Empty or omitted: not bound. Port `0`: ephemeral.
- `sieve_listen` — `host:port` ManageSieve. Empty or omitted: not bound. Port `0`: ephemeral.

At least one IMAP listener is required (`listen` or `implicit_tls_listen`). `smtp_listen`, `smtp_tls_listen`, `jmap_listen`, `jmap_tls_listen`, and `sieve_listen` do not satisfy that. Implicit TLS, `jmap_tls_listen`, and `smtp_tls_listen` require `cert_file` and `key_file` together. Setting one of those two files without the other is a config error.

Example, cleartext only, no certificate:

```json
{
  "listen": "127.0.0.1:0",
  "personality": "personalities/cyrus-3137.json",
  "dataset": "datasets/demo",
  "control": "/tmp/fakeimap.sock",
  "log_path": "/tmp/fakeimap.jsonl"
}
```

## Control socket

One JSON object per line in and one per line out: `{"ok":true,...}` or `{"ok":false,"error":"..."}`. The socket mode is 0600.

An empty `op` is `missing field`. Any other `op` is `unknown op`.

| op | fields | result |
| --- | --- | --- |
| `status` | none | bound addresses plus `personality`, `dataset`, `connections`, `clock_frozen`, `faults` |
| `reset` | none | reloads the current dataset |
| `set-personality` | `path` | loads that personality file |
| `set-dataset` | `path` | loads that dataset directory |
| `disconnect` | `conn` | closes that IMAP connection |
| `inject` | `mailbox`, `raw`; optional `user` | appends the message; returns `uid`. A non-empty `user` must name an account |
| `list-mail` | `mailbox` | messages in that mailbox |
| `fetch-mail` | `mailbox`, `uid` | `rfc822` is base64 |
| `accounts` | none lists `usernames`; `username` and `password` together add one | |
| `freeze-clock` | `time` as RFC3339 | later dates use that instant |
| `thaw-clock` | none | the clock runs again |
| `arm-fault` | see below | returns `id` |
| `clear-faults` | optional `id` | a missing `id` clears every fault |
| `bye` | optional `conn` and `text` | `* BYE` and close; see `docs/facts/imap-server.md` |
| `set-login-disabled` | `value` true or false | forces `LOGINDISABLED` on or off; see `docs/facts/imap-server.md` |
| `create-mailbox` | `name` or `name_b64`; optional `user` | see `docs/facts/imap-server.md` |
| `set-auth-policy` | optional `fail_delay_ms`, `lockout_after`, `lockout_ms` | see `docs/facts/imap-server.md` |
| `clear-auth-policy` | none | see `docs/facts/imap-server.md` |
| `reload-cert` | optional `cert_file` and `key_file`, both or neither | see `docs/facts/imap-server.md` |
| `inject-dsn` | `code` `delivered` or `failed`; optional `to`, `recipient`, `envid`, `raw` | see `docs/facts/imap-server.md` |
| `list-smtp` | none | accepted SMTP messages |
| `wait-smtp` | `count` | zero returns immediately. A positive count also needs `timeout_ms`. A timeout is `ok` false |

`status` includes `listen`, `implicit_tls_listen`, `control`, `smtp`, `smtp_listeners`, `smtp_tls`, `smtp_tls_listeners`, `jmap`, `jmap_tls`, and `sieve`. `smtp` is the first cleartext SMTP bind. `smtp_tls` is the first implicit SMTP TLS bind. `jmap`, `jmap_tls`, and `sieve` are the bound `host:port`, or `""` when that listener is not up. The lists and the recorded SMTP fields are in `docs/facts/imap-server.md`.

Example:

```json
{"op":"arm-fault","do":"silent","command":"NOOP","times":1}
```

## Faults

`conn` omitted or `0` matches every connection. Any other `conn` matches that connection id.

`command` is required except for `wrong_name`, `expired`, and `self_signed`.

`times` omitted or `0` fires on every match. `times` N fires on the Nth match only, then the fault is spent.

Reply faults run in `runCommand` before the command handler. The certificate faults apply at the handshake.

`do` values and the effect in `emitFault`, `faultParts`, and `store.go`:

- `close` drops the connection.
- `rst` sets linger 0 and drops it.
- `half_close` closes the write side and drops it.
- `silent` writes nothing.
- `delay` sleeps `delay_ms` and writes the real reply. `delay_ms` is required.
- `drip` writes the reply in two halves with `delay_ms` between them. `delay_ms` is required.
- `truncate` writes the first `bytes` octets. `bytes` is required.
- `raw` writes `text` in place of the reply. `text` is required.
- `bad_literal` rewrites a literal length. With no `size` the number in `{...}` becomes that number plus one. A `size` and a `+` follow `docs/facts/imap-server.md`.
- `flood` writes `text` `count` times and then the real reply. Both fields are required, and `count` is at least 1.
- `split` writes the reply as two socket writes at `offset`. `offset` is required.
- `coalesce` writes this reply and the next command's reply in one socket write.
- `wrong_tag` replaces the tag with `tag`. `tag` is required.
- `unsolicited` writes `text` and then the real reply. `text` is required.
- `reorder` finishes the next pipelined command before this reply.
- `mid` inserts `text` at `offset`. Both are required.
- `fetch_order` orders FETCH items by `order`, then by the personality order. `order` is required.
- `tag_first` sends the tagged OK before the untagged data.
- `mutate` adds `\Flagged` on the fetched message.
- `wrong_name` presents a certificate for `wrong.example`.
- `expired` presents a certificate that has already expired.
- `self_signed` presents a generated localhost certificate.
- `reply`, `bye`, `idle_burst`, `drop_literal`, `rst_literal`, `overquota`, `expunge_mid`, and `inprogress` are named here. What each one does, and what the `bye` op does, is in `docs/facts/imap-server.md`.
- `proto` defaults to `imap`. `proto` `smtp` applies `close`, `rst`, `half_close`, `silent`, `delay`, `raw`, `reply`, `drop_after_354`, `bounce`, and `overquota` to the named SMTP verb. An IMAP fault does not match an SMTP command. The SMTP rules are in `docs/facts/imap-server.md`.
- `unauthorized`, `server_fail`, and `drop_eventsource` are `proto` `jmap` faults. `proto` `sieve` `reply` forces `NO` on `CHECKSCRIPT` and `PUTSCRIPT`. The effects are in `docs/facts/imap-server.md`.

An unknown `do` is `unknown fault`. A missing required field is `missing field`.

## Personality

`personality.go` loads one JSON object. Unknown keys are rejected. `greeting`, `delimiter`, both capability lists, and `fetch_item_order` are required.

Keys:

- `greeting` — text after the tag-less greeting code.
- `delimiter` — mailbox hierarchy delimiter. `LIST` uses it.
- `greeting_capabilities` — greeting list. When set, it replaces `pre_login_capabilities` on the greeting. Omitted keeps that capability code. An empty list sends the greeting text with no `[CAPABILITY]` code.
- `pre_login_capabilities` — untagged `CAPABILITY` before login.
- `post_login_capabilities` — untagged `CAPABILITY` after login.
- `personal_prefix` — personal `NAMESPACE` prefix. Omitted, it stays `""`.
- `other_users` — other-user prefix. Empty means `NIL`.
- `shared_folders` — shared prefix. Omitted means `NIL`. `""` is an empty prefix.
- `personal_namespaces`, `other_namespaces`, `shared_namespaces` — when present, replace that group. A namespace entry has `prefix` and `delimiter`. A null delimiter is `NIL`. A group can list more than one entry.
- `logout_bye` — whether `LOGOUT` sends `BYE`.
- `fetch_item_order` — order of items inside one `FETCH` response.
- `select_flags` — `FLAGS` list in `SELECT`.
- `permanent_flags` — `PERMANENTFLAGS` list in `SELECT`.
- `select_order` — untagged `SELECT` order.
- `flag_order` — flag order in `FETCH` and `STORE`.
- `fetch_peek_body` — answer `BODY.PEEK` with the `BODY` name.
- `fetch_seen_flags` — put `FLAGS` in the `FETCH` that sets `\Seen`.
- `move_copyuid_untagged` — send `MOVE`'s `COPYUID` as an untagged OK before `EXPUNGE`, and do not send `EXISTS`, `RECENT`, or a tagged `COPYUID`.
- `sort_partial_count_only` — when true, `RETURN (PARTIAL m:n ...)` is accepted but the reply sends `COUNT` and does not send `PARTIAL`.

A capability is advertised only when `capImplemented` accepts it. Named tokens are the `implementedCaps` map. On a cleartext connection that has a certificate and has not started TLS, `advertiseList` also appends `STARTTLS` when the list it is filtering is not the post-login list and that list omitted `STARTTLS`. It drops `STARTTLS` once TLS has started, and it drops a listed `STARTTLS` when no certificate is loaded. `DIGEST-MD5` is absent from `implementedCaps` and is not advertised. `AUTHENTICATE` implements `PLAIN`, `LOGIN`, `CRAM-MD5`, `SCRAM-SHA-1`, `SCRAM-SHA-256`, `SCRAM-SHA-1-PLUS`, `SCRAM-SHA-256-PLUS`, and `EXTERNAL`, and only when the personality lists `AUTH=<name>`.

`UNAUTHENTICATE`, when listed, returns an authenticated connection to the not-authenticated state without expunging the selected mailbox. TLS and `COMPRESS=DEFLATE` stay up. Enabled extensions do not. Which personalities list it, and the reply, are in `docs/facts/imap-server.md`.

`NOTIFY`, when listed, registers which mailbox events this session wants. A later change in a matched mailbox is pushed with no further command from this session, including while it is in `IDLE` on a different mailbox. `dovecot-245.json` and `dovecot-dev.json` advertise the token after login. The command shape and the lines that are pushed are in `docs/facts/imap-server.md`.

`INPROGRESS`, when listed, stays quiet until an `inprogress` fault is armed. `SEARCH`, `ESEARCH`, and `SORT`, including the `UID` forms, then send the progress lines and the same result. Which files list the token, and the fault fields, are in `docs/facts/imap-server.md`.

`LOGINDISABLED`, while it is on the wire, refuses cleartext `LOGIN`. A listed `AUTHENTICATE` mechanism still succeeds. `advertiseList` keeps the token only on cleartext with a certificate, unless `set-login-disabled` forces it. The filter, the refusal, and which file lists the token are in `docs/facts/imap-server.md`.

`JMAPACCESS` is advertised only when the personality lists it and `jmap_listen` or `jmap_tls_listen` is bound. Otherwise the token is omitted and `GETJMAPACCESS` is tagged `BAD`. When it is offered, the command takes no arguments, is tagged `NO` before login, and does not need a selected mailbox. It returns one line, `* JMAPACCESS` and a quoted absolute `/.well-known/jmap` URL: `https` when the TLS listener is bound, and `http` from the cleartext listener otherwise. A host of `0.0.0.0` or `::` is written as `127.0.0.1`, and an IPv6 host is in brackets. `stalwart.json`, `stalwart-01625.json`, `stalwart-dev.json`, and `stalwart-oauth.json` list the token only after login. `stalwart-0155.json` lists it on the greeting and on both capability lists. No other lab personality lists it. The rules are in `docs/facts/imap-server.md`.

`OBJECTID`, when the personality lists it, is RFC 8474 and does not need `ENABLE`. `MAILBOXID` is the mailbox JMAP id and `EMAILID` is the message JMAP id. `THREADID` is assigned when the message is stored. `COPY` and `MOVE` keep both. `OBJECTID+` without `OBJECTID` is the compound form. `ACCOUNTID` is the logged-in user name, and those replies start only after activation. A `STORE` of a flag other than `\Deleted` or `\Recent` then applies to every copy of that `EMAILID`. `CATENATE` stays unimplemented. The replies, the id rules, activation, and which files list each token are in `docs/facts/imap-server.md`.

`ID`, when listed, is accepted before and after login. The untagged reply is `* ID ("name" "fakeimap")`. Otherwise the command is tagged `BAD`. The argument rules are in `docs/facts/imap-server.md`.

`APPENDLIMIT=<positive integer>` is the maximum `APPEND` literal on that connection, for every mailbox. The number is the one on the capability list in effect, which after login is the post-login list. A longer literal is tagged `NO` and includes `[TOOBIG]`, and it is not stored. A literal of that length is stored. `STATUS` and `LIST RETURN (STATUS ...)` report the number. Without the token, `STATUS` of `APPENDLIMIT` is tagged `BAD`. A bare `APPENDLIMIT`, `APPENDLIMIT=0`, and a non-numeric suffix are not advertised. Which files list it, and the Cyrus numbers, are in `docs/facts/imap-server.md`.

`UIDONLY`, when the personality lists it, is advertised and stays off until `ENABLE UIDONLY`. That command arms only the session that sent it. Afterwards the session rejects message numbers with `[UIDREQUIRED]`, answers `UID FETCH` and `UID STORE` with `UIDFETCH`, and reports expunges as `VANISHED`. Which files list the token, and the command rules, are in `docs/facts/imap-server.md`.

`MESSAGELIMIT=<positive integer>` caps how many messages one command examines. The number is the one on the capability list in effect. `COPY` and a `MULTIAPPEND` that is too large store nothing. `FETCH`, `STORE`, `MOVE`, `SEARCH`, and `UID EXPUNGE` process the highest UIDs and name the lowest of those in the response code. `SORT` and `THREAD` of a mailbox that is too large are refused and return no result. `UIDAFTER` and `UIDBEFORE` are search keys when the token is listed. `SAVELIMIT=<positive integer>` limits `COPY`, `UID COPY`, `APPEND`, and `MULTIAPPEND`. `MOVE` stays under `MESSAGELIMIT`. Which commands stop, which files list the token, and what the response code contains are in `docs/facts/imap-server.md`.

`MULTIAPPEND`, when listed, lets one `APPEND` carry more than one message. The command stores every message or none of them. One message stays a normal `APPEND` with or without the token. A second message without the token is rejected and stores nothing. Which files list the token, and the failure rules, are in `docs/facts/imap-server.md`.

`REPLACE`, when listed, appends one new message and expunges the addressed one, or does neither. The new message gets a new `EMAILID` and does not keep the old one. A failed destination, flag, date, or literal leaves the source in place. After `ENABLE UIDONLY`, `REPLACE` without `UID` is rejected. `cyrus-3124.json`, `cyrus-3137.json`, `sasl.json`, `dovecot-245.json`, and `dovecot-dev.json` list the token. The command, the failure rule, and the id rules are in `docs/facts/imap-server.md`.

`STATUS=SIZE`, when listed, reports the sum of the RFC822 sizes of the messages in that mailbox from `STATUS` and from `LIST RETURN (STATUS ...)`. An empty mailbox is 0. A message marked `\Deleted` still counts until `EXPUNGE`. Without the capability, `STATUS` of `SIZE` is tagged `BAD`. Which files list the token is in `docs/facts/imap-server.md`.

`SAVEDATE`, when listed, reports when the message was saved into this mailbox. That time is separate from `INTERNALDATE`: an `APPEND` date stays the internal date, and a copy takes a new save time. `SAVEDBEFORE`, `SAVEDON`, and `SAVEDSINCE` compare the save time the way `BEFORE`, `ON`, and `SINCE` compare the internal date. The rules and which files list the token are in `docs/facts/imap-server.md`.

`WITHIN`, when listed, answers `OLDER` and `YOUNGER`. The interval is one non-zero number of seconds. The target is the current time minus that interval, to one second, and the frozen clock is that current time when it is set. `OLDER` includes an internal date at or before the target. `YOUNGER` includes one at or after it, so the equal instant matches both. The rules are in `docs/facts/imap-server.md`.

`CONTEXT=SEARCH`, when listed, lets `SEARCH` and `UID SEARCH` keep a result with `RETURN (UPDATE)` and push `ADDTO` and `REMOVEFROM` when that set changes. `CONTEXT` inside `RETURN` changes nothing. `CANCELUPDATE` drops the named contexts. `SELECT`, `EXAMINE`, `CLOSE`, `UNSELECT`, and `UNAUTHENTICATE` drop every context on that session. `CONTEXT=SORT` stays unimplemented. The return options, the push lines, what clears a context, and which files list the token are in `docs/facts/imap-server.md`.

`SCAN`, when listed, filters mailbox names. It returns the mailboxes `LIST` would return for the same reference and pattern whose names contain a string, and it does not search message text. `cyrus-24.json`, `cyrus-25.json`, `cyrus-36.json`, and `uwimap.json` list it on the pre-login and post-login lists and not on the greeting. The rules are in `docs/facts/imap-server.md`.

`XLIST`, when listed, is `LIST` with the response word `XLIST`. `XMOVE`, when listed, is `MOVE`, including `UID XMOVE`, and it does not require `MOVE` to be listed. Which files list each token, and the reply rules, are in `docs/facts/imap-server.md`.

`IMAP4rev2`, when listed, is enabled with `ENABLE IMAP4rev2`, which answers `* ENABLED IMAP4REV2`. Greeting spelling is `IMAP4rev2`. Post-login spelling is `IMAP4REV2`. The Stalwart greeting sentence stays `Stalwart IMAP4rev2 at your service.` After that `ENABLE`, search uses `ESEARCH`, `RECENT` lines are omitted, and `CHECK` is rejected. `LOGIN-REFERRALS` and `MAILBOX-REFERRALS` make `LOGIN`, `AUTHENTICATE`, and the mailbox commands return a referral response code when `set-referral` is set. `LIST-MYRIGHTS` adds a `MYRIGHTS` line to `LIST RETURN`. `SAVELIMIT=<positive integer>` caps copies and appends. `OAUTHBEARER` and `XOAUTH2` authenticate with a bearer token on `stalwart-oauth.json`. The token rules, the OAuth routes, the rotation flag, and which files list each token are in `docs/facts/imap-server.md`.

`SORT=MODSEQ`, when listed, accepts `MODSEQ` as the one sort key. The order is the message mod-sequence, ascending, and `REVERSE MODSEQ` is descending. `SORT=UID`, when listed, is the final tie-break, ascending UID, and `REVERSE` does not flip it. It is not a key the client writes. Without that token, equal keys stay in ascending sequence order. Which files list each token is in `docs/facts/imap-server.md`.

`BINARY.SIZE` is a `FETCH` item when `BINARY` is listed. It is not its own capability. The reply is the decoded size of one section as a number, using the same section as `BINARY.PEEK`, and it does not set `\Seen`. The item, including the `BAD` and `NO` cases, is in `docs/facts/imap-server.md`.

Lab files are `personalities/<server>.json` for each lab server: Cyrus `cyrus-3137`, `cyrus-22`, `cyrus-24`, `cyrus-25`, `cyrus-36`, `cyrus-3124`; Dovecot `dovecot-245`, `dovecot-dev`, `dovecot-10`, `dovecot-12`, `dovecot-22`; Stalwart `stalwart`, `stalwart-01625`, `stalwart-dev`, `stalwart-0155`; Zimbra `zimbra-883`; Courier `courier-50`, `courier-606`; UW-IMAP `uwimap`. `sasl.json` is the Cyrus mailbox behavior plus the extra `AUTH=` tokens. It is not the probed Cyrus personality.

The fixture files `sasl`, `condstore`, `list`, `esearch`, `compress`, `sort`, `utf8`, `thread`, `fuzzy`, `binary`, `preview`, and `multisearch` add one capability family on the Cyrus mailbox behavior. What each family answers is in `docs/facts/imap-server.md`.

`cyrus-3137.json` lists `SEARCH=FUZZY` on the pre-login and post-login lists and not on the greeting. `sasl.json` does not list it. `SEARCH=FUZZY` is implemented and is not in the gaps file.

`SEARCHRES`, when listed, saves the full match of `RETURN (SAVE)` as UIDs and lets `$` stand for that set. `SELECT` and `EXAMINE` make `$` empty, and an empty `$` is a valid `FETCH` that sends no `FETCH` line. The `SAVE` rules and the files that list the token are in `docs/facts/imap-server.md`.

`GETQUOTA` and `GETQUOTAROOT` report one shared root, `ROOT`. `STORAGE` is the store's RFC822 size in 1024-octet units, rounded up, and `MESSAGE` is the message count when `QUOTA=RES-MESSAGE` is listed. The limits start at 1048576 and 1000000. `SETQUOTA` changes them when `QUOTASET` is listed. Which files list each token, and the reply rules, are in `docs/facts/imap-server.md`.

`SETACL`, `DELETEACL`, `GETACL`, `LISTRIGHTS`, and `MYRIGHTS` are recognized on every personality. Before login each is tagged `NO` `must authenticate first`. After login a command with valid arguments is tagged `NO` `valid syntax not implemented`, and nothing is stored. Invalid arguments are tagged `BAD`. `ACL` is not advertised. The argument rules are in `docs/facts/imap-server.md`.

`FILTER` and `UID FILTER` check a Sieve filter spec and search criteria. With no mailbox selected, including before login, each is tagged `NO` `FILTER not selected`. With a mailbox selected, valid arguments are tagged `NO` `not implemented`, and no script runs. Invalid arguments are tagged `BAD`. `FILTER=SIEVE` is not advertised. The argument rules are in `docs/facts/imap-server.md`.

`COMPARATOR`, `GENURLAUTH`, `URLFETCH`, and `RESETKEY` are recognized on every personality. `COMPARATOR` requires login and a valid argument list is tagged `NO` `not implemented`. `GENURLAUTH` and `RESETKEY` require login and mint or reset a URL. `URLFETCH` does not require login and returns the stored bytes or `NIL`. Invalid arguments are tagged `BAD`. `URLAUTH` and `URLAUTH=BINARY` are advertised on the Cyrus files that list them. `I18NLEVEL=1` and `URL-PARTIAL` stay filtered. The argument rules are in `docs/facts/imap-server.md`.

## Dataset

`dataset.json` keys, as `docs/facts/imap-server.md` defines them:

- `accounts` — objects with `username` and `password`.
- `mailboxes` — objects with `name`, optional `uid_validity`, optional `messages`, optional `generated`, optional `special_use`.
- `messages` — objects with `file` (path relative to the dataset directory), `flags`, and `internal_date` (`02-Jan-2006 15:04:05 -0700`).
- `generated` — `count`, `seed`, `from`, `to`, `subject`, `start` (same date form), `step_seconds`.
- `special_use` — optional array of special-use flags on that mailbox. A flag belongs to at most one mailbox.

An overlaid message is the `.eml` file. A generated mailbox keeps a header index (`from`, `to`, `subject`, `date`, `size`, `flags`) and builds RFC822 bytes from the seed and the generation id when a command needs them. Those bytes are not kept on the message.

The sample tree is `datasets/demo/`.

## Reply path

A matching reply fault runs first. Then the command handler uses the store and the personality fields. An unknown command is tagged `BAD`. There is no hook map.

## Listeners

SMTP cleartext listeners and the implicit TLS listener, the EHLO list, the recorded `list-smtp` fields, and the SMTP faults are in `docs/facts/imap-server.md`. `MAIL`, `RCPT`, and `DATA` before `AUTH` are `530` while `smtp_auth_required` is true. A dataset recipient is delivered to `INBOX` only in that mode. Nothing is relayed. A failure report is stored only when `inject-dsn` or a `bounce` fault asks for it.

`jmap_listen` is cleartext HTTP. It does not speak TLS. `jmap_tls_listen` speaks TLS from the first byte on the same routes, reports the bound address as `jmap_tls`, allows port `0`, and leaves session URLs as paths. An empty or omitted address is not bound. Setting it without a certificate is a config error. Method behavior is in `docs/facts/imap-server.md`.

ManageSieve stores scripts and does not execute them. `CHECKSCRIPT` and `PUTSCRIPT` reject a script outside the small command list. The checker is in `docs/facts/imap-server.md`. Its `STARTTLS` is listed only when a certificate is loaded. `STARTTLS` returns `OK`, and the next greeting has no `STARTTLS`.

## Log and clock

`log_path` appends one JSON object per line: `kind` `command` or `reply`, `conn`, and `text`. `freeze-clock` and `thaw-clock` are the clock ops above, and `status` reports `clock_frozen`.

## Probe

`cyrusprobe` is a separate binary. `make` builds it and does not start Docker.

```
cyrusprobe [-gaps gaps.json] operator.json
```

The operator file is JSON. `cyrusprobe.example.json` has an empty password. A filled file is `cyrusprobe.local.json` (gitignored). Do not commit a password.

```json
{
  "host": "127.0.0.1",
  "port": 143,
  "tls": "none",
  "username": "user@example.test",
  "password": "",
  "personality": "personalities/cyrus-3137.json"
}
```

`tls` is `none`, `starttls`, or `implicit`. The command runs one script against that server and against a local `fakeimap` using the personality file, then prints a normalized diff. The normalized diff ignores tags, UIDs, `UIDVALIDITY`, `UIDNEXT`, `INTERNALDATE`, message counts the script caused, and timestamps. Capability tokens absent from `implementedCaps` are dropped before the diff. `-gaps` writes capabilities the remote advertised that this binary does not implement.

Further flags (`-condstore`, `-list`, `-esearch`, `-compress`, `-sort`, `-display`, `-utf8`, `-thread`, `-binary`, `-fuzzy`, `-multisearch`, `-preview`) are named in `docs/facts/imap-server.md`. `make` does not run them against Docker.

## Shapes a personality file cannot express

These are current limits. The guide records them. It does not change the server to match them.

- A bare tagged `LOGIN` OK.
- Lowercase `BODYSTRUCTURE`, a Stalwart MD5 on that item, and a Dovecot 1.0 `BODYSTRUCTURE` with no final NIL.
- A Dovecot extra `RECENT` after `MOVE`, and `$HasAttachment` / `$HasNoAttachment` on a dovecot-dev `APPEND`.
- Stalwart `EXISTS` and `RECENT` before `APPENDUID`, a zero-padded ENVELOPE day, no `EXISTS` after `MOVE`, and unquoted `LIST` names with `\HasNoChildren`.
- Dovecot 1.0 `APPEND` and `COPY` with no UIDPLUS codes.

The base probe always creates a top-level mailbox and always sends `MOVE`. Old Cyrus and both Couriers answer `NO` to that `CREATE`. dovecot-10, dovecot-12, uwimap, and zimbra answer `BAD` to `MOVE`. The script stops.

The UW greeting appends ` at ` and a timestamp after the fixed text. One post-login list cannot both omit and include `SASL-IR` and `STARTTLS` the way the live UW `LOGIN` reply and its untagged `CAPABILITY` do.

## Capabilities that stay off

Capabilities the lab advertises that stay off are the tokens in `personalities/cyrus-3137.gaps.json`, including `ACL` and `CATENATE`. After `SEARCH=FUZZY`, `APPENDLIMIT=2147483647`, `SEARCHRES`, `STATUS=SIZE`, `SAVEDATE`, `SORT=MODSEQ`, `SORT=UID`, `XLIST`, `MULTIAPPEND`, `QUOTA`, `QUOTA=RES-STORAGE`, `QUOTA=RES-MESSAGE`, `QUOTASET`, `UIDONLY`, `OBJECTID`, `REPLACE`, `INPROGRESS`, and `LOGINDISABLED` left that file, the tokens are `ACL`, `ANNOTATE-EXPERIMENT-1`, `CATENATE`, `DIGEST=SHA1`, `LIST-METADATA`, `METADATA`, `NO_ATOMIC_RENAME`, `RIGHTS=kxten`, `UIDBATCHES`, `URL-PARTIAL`, `X-REPLICATION`, `X-REPLICATION-ARCHIVE`, and `X-SIEVE-MAILBOX`.

`I18NLEVEL=1` and `URL-PARTIAL` stay unimplemented. `ACL`, `RIGHTS=kxten`, and `CATENATE` stay unimplemented. Those rules are in `docs/facts/imap-server.md`.

## How to extend

A behavior the personality data can say is a new field, a loopback test, and a sentence in `docs/facts/imap-server.md`. A Go branch is for a behavior a second personality would need when a field cannot say it. The tree has no hook map. A capability is added to `implementedCaps` only when the binary speaks it. `APPENDLIMIT=<positive integer>` and `MESSAGELIMIT=<positive integer>` are recognized by `capImplemented` instead of that map, because the number is part of the token.
