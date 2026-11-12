# fakeimap

Developer notes for running and extending the server. This is not a user manual. It stays English and is not a translation-catalog entry.

Long protocol rules — search keys, FETCH items, JMAP methods, ManageSieve script commands, and each probe script — are in `docs/facts/imap-server.md`. This page does not paste that file.

## How to use

`make` builds `fakeimap` and `cyrusprobe`. It runs the Go tests and does not start Docker. No certificate is in this repo. Generate a certificate outside the tree when a listener needs TLS.

```
fakeimap <config.json>
```

The one argument is the config path. Paths inside the config are relative to the process working directory, not to the config file.

Before accept, the process writes one JSON object to stdout and flushes it. The object names the listeners that bound: `listen`, `implicit_tls_listen`, `control`, `smtp`, `jmap`, `jmap_tls`, and `sieve`. A configured port of `0` is the bound ephemeral port.

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
- `smtp_listen` — `host:port` cleartext SMTP submission. Empty or omitted: not bound. Port `0`: ephemeral.
- `jmap_listen` — `host:port` cleartext HTTP JMAP. Empty or omitted: not bound. Port `0`: ephemeral. It does not speak TLS.
- `jmap_tls_listen` — `host:port` JMAP with TLS from the first byte. Empty or omitted: not bound. Port `0`: ephemeral.
- `sieve_listen` — `host:port` ManageSieve. Empty or omitted: not bound. Port `0`: ephemeral.

At least one IMAP listener is required (`listen` or `implicit_tls_listen`). `smtp_listen`, `jmap_listen`, `jmap_tls_listen`, and `sieve_listen` do not satisfy that. Implicit TLS and `jmap_tls_listen` require `cert_file` and `key_file` together. Setting one of those two files without the other is a config error.

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
| `list-smtp` | none | accepted SMTP messages |
| `wait-smtp` | `count` | zero returns immediately. A positive count also needs `timeout_ms`. A timeout is `ok` false |

`status` includes `listen`, `implicit_tls_listen`, `control`, `smtp`, `jmap`, `jmap_tls`, and `sieve`. `smtp`, `jmap`, `jmap_tls`, and `sieve` are the bound `host:port`, or `""` when that listener is not up.

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
- `bad_literal` rewrites a literal length (the number in `{...}` plus one).
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

`ID`, when listed, is accepted before and after login. The untagged reply is `* ID ("name" "fakeimap")`. Otherwise the command is tagged `BAD`. The argument rules are in `docs/facts/imap-server.md`.

`APPENDLIMIT=<positive integer>` is the maximum `APPEND` literal on that connection, for every mailbox. The number is the one on the capability list in effect, which after login is the post-login list. A longer literal is tagged `NO` and includes `[TOOBIG]`, and it is not stored. A literal of that length is stored. `STATUS` and `LIST RETURN (STATUS ...)` report the number. Without the token, `STATUS` of `APPENDLIMIT` is tagged `BAD`. A bare `APPENDLIMIT`, `APPENDLIMIT=0`, and a non-numeric suffix are not advertised. Which files list it, and the Cyrus numbers, are in `docs/facts/imap-server.md`.

`STATUS=SIZE`, when listed, reports the sum of the RFC822 sizes of the messages in that mailbox from `STATUS` and from `LIST RETURN (STATUS ...)`. An empty mailbox is 0. A message marked `\Deleted` still counts until `EXPUNGE`. Without the capability, `STATUS` of `SIZE` is tagged `BAD`. Which files list the token is in `docs/facts/imap-server.md`.

`SAVEDATE`, when listed, reports when the message was saved into this mailbox. That time is separate from `INTERNALDATE`: an `APPEND` date stays the internal date, and a copy takes a new save time. `SAVEDBEFORE`, `SAVEDON`, and `SAVEDSINCE` compare the save time the way `BEFORE`, `ON`, and `SINCE` compare the internal date. The rules and which files list the token are in `docs/facts/imap-server.md`.

`WITHIN`, when listed, answers `OLDER` and `YOUNGER`. The interval is one non-zero number of seconds. The target is the current time minus that interval, to one second, and the frozen clock is that current time when it is set. `OLDER` includes an internal date at or before the target. `YOUNGER` includes one at or after it, so the equal instant matches both. The rules are in `docs/facts/imap-server.md`.

`SORT=MODSEQ`, when listed, accepts `MODSEQ` as the one sort key. The order is the message mod-sequence, ascending, and `REVERSE MODSEQ` is descending. `SORT=UID`, when listed, is the final tie-break, ascending UID, and `REVERSE` does not flip it. It is not a key the client writes. Without that token, equal keys stay in ascending sequence order. Which files list each token is in `docs/facts/imap-server.md`.

`BINARY.SIZE` is a `FETCH` item when `BINARY` is listed. It is not its own capability. The reply is the decoded size of one section as a number, using the same section as `BINARY.PEEK`, and it does not set `\Seen`. The item, including the `BAD` and `NO` cases, is in `docs/facts/imap-server.md`.

Lab files are `personalities/<server>.json` for each lab server: Cyrus `cyrus-3137`, `cyrus-22`, `cyrus-24`, `cyrus-25`, `cyrus-36`, `cyrus-3124`; Dovecot `dovecot-245`, `dovecot-dev`, `dovecot-10`, `dovecot-12`, `dovecot-22`; Stalwart `stalwart`, `stalwart-01625`, `stalwart-dev`, `stalwart-0155`; Zimbra `zimbra-883`; Courier `courier-50`, `courier-606`; UW-IMAP `uwimap`. `sasl.json` is the Cyrus mailbox behavior plus the extra `AUTH=` tokens. It is not the probed Cyrus personality.

The fixture files `sasl`, `condstore`, `list`, `esearch`, `compress`, `sort`, `utf8`, `thread`, `fuzzy`, `binary`, `preview`, and `multisearch` add one capability family on the Cyrus mailbox behavior. What each family answers is in `docs/facts/imap-server.md`.

`cyrus-3137.json` lists `SEARCH=FUZZY` on the pre-login and post-login lists and not on the greeting. `sasl.json` does not list it. `SEARCH=FUZZY` is implemented and is not in the gaps file.

`SEARCHRES`, when listed, saves the full match of `RETURN (SAVE)` as UIDs and lets `$` stand for that set. `SELECT` and `EXAMINE` make `$` empty, and an empty `$` is a valid `FETCH` that sends no `FETCH` line. The `SAVE` rules and the files that list the token are in `docs/facts/imap-server.md`.

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

SMTP stays cleartext. `EHLO` lists `AUTH PLAIN LOGIN`, `8BITMIME`, and `STARTTLS` only when a certificate is loaded and the connection is not yet TLS. `STARTTLS` with an argument is `501`. `STARTTLS` with no certificate, or a second `STARTTLS`, is `454`. Success is `220` and then TLS, and the next `EHLO` omits `STARTTLS`. `MAIL`, `RCPT`, and `DATA` before `AUTH` are `530`. `AUTH PLAIN` and `AUTH LOGIN` use the same dataset passwords as IMAP. `RCPT TO` of a dataset account, as a bare name or `name@domain`, delivers `DATA` to that account's `INBOX`. Any other recipient is accepted and the message is not stored. There is no queue, relay, or DSN.

`jmap_listen` is cleartext HTTP. It does not speak TLS. `jmap_tls_listen` speaks TLS from the first byte on the same routes, reports the bound address as `jmap_tls`, allows port `0`, and leaves session URLs as paths. An empty or omitted address is not bound. Setting it without a certificate is a config error. Method behavior is in `docs/facts/imap-server.md`.

ManageSieve stores scripts and does not execute them. `CHECKSCRIPT` does not parse Sieve. Its `STARTTLS` is listed only when a certificate is loaded. `STARTTLS` returns `OK`, and the next greeting has no `STARTTLS`.

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

Capabilities the lab advertises that stay off are the tokens in `personalities/cyrus-3137.gaps.json`, including `QUOTA`, `ACL`, and `IMAP4rev2`. After `SEARCH=FUZZY`, `APPENDLIMIT=2147483647`, `SEARCHRES`, `STATUS=SIZE`, `SAVEDATE`, `SORT=MODSEQ`, and `SORT=UID` left that file, the tokens are `ACL`, `ANNOTATE-EXPERIMENT-1`, `CATENATE`, `DIGEST=SHA1`, `IMAP4rev2`, `INPROGRESS`, `LIST-METADATA`, `LIST-MYRIGHTS`, `LOGINDISABLED`, `MAILBOX-REFERRALS`, `METADATA`, `MULTIAPPEND`, `NO_ATOMIC_RENAME`, `OBJECTID`, `QUOTA` and its `QUOTA=RES-*` forms, `QUOTASET`, `REPLACE`, `RIGHTS=kxten`, `UIDBATCHES`, `UIDONLY`, `URL-PARTIAL`, `URLAUTH`, `URLAUTH=BINARY`, `X-CREATEDMODSEQ`, `X-REPLICATION`, `X-REPLICATION-ARCHIVE`, `X-SIEVE-MAILBOX`, and `XLIST`.

`URLAUTH` and `URLAUTH=BINARY` stay unimplemented. `LOGIN-REFERRALS` and `JMAPACCESS` stay off the personality files. `URLAUTH=BINARY` stays in the gaps file.

## How to extend

A behavior the personality data can say is a new field, a loopback test, and a sentence in `docs/facts/imap-server.md`. A Go branch is for a behavior a second personality would need when a field cannot say it. The tree has no hook map. A capability is added to `implementedCaps` only when the binary speaks it. `APPENDLIMIT=<positive integer>` is recognized by `capImplemented` instead of that map, because the number is part of the token.
