# IMAP server

Binary: `fakeimap`, at the checkout root. `make` runs `go test ./...` and then `go build -o fakeimap .`. `make clean` deletes that binary.

Run: `fakeimap <config.json>`. The one argument is the config path. Paths inside the config are relative to the process working directory, not to the config file.

Before accept, the process writes one JSON object to stdout and flushes it. The object has `listen` when a cleartext listener is bound, `implicit_tls_listen` when an implicit-TLS listener is bound, and `control` when the control socket is bound. A configured port of `0` is the bound ephemeral port.

## Config keys

- `listen` — `host:port` cleartext listener. Empty or omitted: no cleartext listener. Port `0`: ephemeral.
- `implicit_tls_listen` — `host:port` implicit TLS listener. Empty or omitted: no implicit-TLS listener. Port `0`: ephemeral.
- `cert_file` — PEM certificate for `STARTTLS` and implicit TLS.
- `key_file` — PEM private key matching `cert_file`. Not part of the repo; tests generate one.
- `client_ca_file` — PEM trust anchors for an optional client certificate. Used when the personality lists `AUTH=EXTERNAL` and a server certificate is loaded. Empty or omitted: a client certificate is requested only if `AUTH=EXTERNAL` is listed, and it is not verified. A connection without a client certificate still completes the handshake.
- `log_path` — file to append. Empty or omitted: no log file is created.
- `personality` — personality JSON file.
- `dataset` — dataset directory. The directory contains `dataset.json`.
- `control` — filesystem path of the mode-0600 Unix control socket. Empty or omitted: no socket.

At least one listener is required. Implicit TLS requires `cert_file` and `key_file`. `STARTTLS` is offered on a cleartext connection only when a certificate is loaded and TLS has not started.

## Log lines

One JSON object per line, append-only:

- `kind` — `command` (one client command) or `reply` (one server reply)
- `conn` — connection number, starting at 1
- `text` — the command or reply

## Personality

File: `personalities/cyrus-3137.json`

The probed Cyrus personality advertises `AUTH=LOGIN` on the greeting and before login.

Keys: `greeting`, `delimiter`, `greeting_capabilities`, `pre_login_capabilities`, `post_login_capabilities`, `other_users`, `shared_folders`, `logout_bye`, `fetch_item_order`, `select_flags`, `permanent_flags`, `select_order`, `flag_order`, `fetch_peek_body`, `fetch_seen_flags`, `move_copyuid_untagged`.

A capability listed there is advertised only if this binary implements it. `personalities/sasl.json` is the same mailbox behavior as `cyrus-3137.json`, plus `AUTH=LOGIN`, `AUTH=CRAM-MD5`, `AUTH=SCRAM-SHA-1`, `AUTH=SCRAM-SHA-256`, `AUTH=SCRAM-SHA-1-PLUS`, `AUTH=SCRAM-SHA-256-PLUS`, and `AUTH=EXTERNAL` on the greeting and pre-login lists. It is not the probed Cyrus personality. `greeting_capabilities`, when set, is the greeting list instead of `pre_login_capabilities`. `fetch_item_order` is the order of items inside one `FETCH` response. `logout_bye` controls whether `LOGOUT` sends `BYE`. `other_users` and `shared_folders` are NAMESPACE prefixes; empty means NIL. `select_order` is the untagged SELECT order. `select_flags` and `permanent_flags` are the flag lists in that response. `flag_order` is the flag order in FETCH and STORE. `fetch_peek_body` answers `BODY.PEEK` with the `BODY` name. `fetch_seen_flags` puts `FLAGS` in the FETCH that sets `\Seen`. `move_copyuid_untagged` sends MOVE's `COPYUID` as an untagged OK before `EXPUNGE`, and does not send `EXISTS`, `RECENT`, or a tagged `COPYUID`. `CONDSTORE`, when listed, adds `HIGHESTMODSEQ` to `SELECT` and `EXAMINE` and accepts `FETCH MODSEQ`, `CHANGEDSINCE`, and `STORE UNCHANGEDSINCE`. `ENABLE CONDSTORE` makes a later flag change from another connection arrive as one unsolicited `FETCH` with `FLAGS` and `MODSEQ`. `ENABLE QRESYNC` also enables `CONDSTORE`. Expunges reported to that connection are `VANISHED`, not `EXPUNGE`. `SELECT` or `EXAMINE` with `QRESYNC` is `BAD` until `QRESYNC` is enabled; after that, a known UID that was expunged is reported as `VANISHED (EARLIER)`. `personalities/condstore.json` lists `CONDSTORE` and `QRESYNC` for the second probe's loopback test.

`LIST-EXTENDED`, `CHILDREN`, `SPECIAL-USE`, `LIST-STATUS`, and `CREATE-SPECIAL-USE`, when listed, answer an extended `LIST` and `CREATE` with `USE`. A two-argument `LIST` is unchanged and still carries `\HasChildren` or `\HasNoChildren`. `RETURN (CHILDREN)` includes those flags. A mailbox special-use flag is included when `SPECIAL-USE` is listed. `RETURN (STATUS (...))` is followed by `STATUS` for each listed mailbox, using the same items and counts as the `STATUS` command. `LIST (SPECIAL-USE)` returns only mailboxes that have a special-use flag. Without `LIST-EXTENDED`, `RETURN` and a parenthesized selection are tagged `BAD`. `CREATE name (USE (\Sent))` stores that flag when `CREATE-SPECIAL-USE` is listed. The accepted flags are `\All`, `\Archive`, `\Drafts`, `\Flagged`, `\Junk`, `\Sent`, and `\Trash`. A flag already used by another mailbox is tagged `NO` and creates nothing. Without `CREATE-SPECIAL-USE`, `USE` is tagged `BAD`. `personalities/list.json` is the Cyrus personality plus these five capabilities. `personalities/cyrus-3137.json` lists them too.

`ESEARCH`, when listed, answers `SEARCH RETURN` and `UID SEARCH RETURN` with one `* ESEARCH (TAG "...")` line. `UID` is present only on a UID command. The result items are `COUNT`, then `MIN`, then `MAX`, then `ALL`, then `PARTIAL`, and only the ones the command named. `COUNT` is the number of matches, including `0`. `MIN`, `MAX`, and `ALL` are omitted when nothing matches. `ALL` is a sequence set of the whole result. An empty `RETURN ()` means `ALL`. Without `ESEARCH`, `RETURN` is tagged `BAD`. Plain `SEARCH` and `UID SEARCH` stay `* SEARCH`.

`PARTIAL`, when listed as well, answers `RETURN (PARTIAL m:n ...)`. The response repeats that range. `1` is the first match and `-1` is the last. The set is the overlap, in result order, or `NIL` when the range misses the result. `COUNT` stays the full length. `PARTIAL` together with `ALL`, or `PARTIAL` without the `PARTIAL` capability, is tagged `BAD`. `personalities/esearch.json` is the Cyrus personality plus `ESEARCH` and `PARTIAL`. `personalities/cyrus-3137.json` lists both too.

## SASL

`AUTHENTICATE` mechanisms this binary implements: `PLAIN`, `LOGIN`, `CRAM-MD5`, `SCRAM-SHA-1`, `SCRAM-SHA-256`, `SCRAM-SHA-1-PLUS`, `SCRAM-SHA-256-PLUS`, and `EXTERNAL`. A mechanism is advertised only when the personality lists `AUTH=<name>`, and `AUTHENTICATE` of a mechanism that is not listed fails. `DIGEST-MD5` is not implemented and is not advertised.

`LOGIN` asks for the username and then the password. An initial response, when the client sends one, is the username.

`CRAM-MD5` checks HMAC-MD5 of the server challenge against the dataset password.

`SCRAM-SHA-1` and `SCRAM-SHA-256` check the dataset password over the four SCRAM messages. A wrong password or a bad client proof fails. A client `*` cancels. The `-PLUS` forms require TLS and channel binding `tls-unique` or `tls-exporter`, and fail when the binding does not match the connection. Either `-PLUS` mechanism on a cleartext connection fails.

`EXTERNAL` succeeds when the client certificate was verified and an email SAN, a DNS SAN, or the subject CN equals an account username, and the authorization identity is empty or that same username. No client certificate, or a name that matches no account, fails. `PLAIN` still works on a listener that accepts a client certificate.

## Dataset

Directory: `datasets/demo/`

`dataset.json` keys:

- `accounts` — objects with `username` and `password`
- `mailboxes` — objects with `name`, optional `uid_validity`, optional `messages`, optional `generated`, optional `special_use`
- `messages` — objects with `file` (path relative to the dataset directory), `flags` (IMAP flag names), and `internal_date` (`02-Jan-2006 15:04:05 -0700`)
- `generated` — `count`, `seed`, `from`, `to`, `subject`, `start` (same date form as `internal_date`), `step_seconds`
- `special_use` — optional array of special-use flags on that mailbox: `\All`, `\Archive`, `\Drafts`, `\Flagged`, `\Junk`, `\Sent`, `\Trash`. A flag belongs to at most one mailbox.

An overlaid message is stored from its file. A generated mailbox keeps a header index only (`from`, `to`, `subject`, `date`, `size`, `flags`). Its RFC822 bytes are produced from the seed and the generation id when a command needs them, and are not kept on the message.

## Control socket

One JSON object per line in, one JSON object per line out: `{"ok":true,...}` or `{"ok":false,"error":"..."}`.

Ops: `status`, `reset`, `set-personality`, `set-dataset`, `disconnect`, `inject`, `list-mail`, `fetch-mail`, `accounts`, `freeze-clock`, `thaw-clock`, `arm-fault`, `clear-faults`.

Request fields: `path`, `conn`, `mailbox`, `user`, `username`, `password`, `raw`, `uid`, `time`, `id`, `command`, `times`, `do`, `delay_ms`, `bytes`, `text`, `count`, `offset`, `tag`, `order`.

Fault `do` names: `close`, `rst`, `half_close`, `silent`, `delay`, `drip`, `truncate`, `raw`, `bad_literal`, `flood`, `split`, `coalesce`, `wrong_tag`, `unsolicited`, `reorder`, `mid`, `fetch_order`, `tag_first`, `mutate`, `wrong_name`, `expired`, `self_signed`.

## Probe

`cyrusprobe` is a separate command from `fakeimap`. `make` builds it with `go build -tags probe -o cyrusprobe .` after `go test`. `make` does not start Docker.

Run: `cyrusprobe [-gaps gaps.json] operator.json`. The operator file is JSON. The example `cyrusprobe.example.json` has an empty password. A filled file is `cyrusprobe.local.json` (gitignored).

Operator keys:

- `host` — IMAP host
- `port` — IMAP port
- `tls` — `none`, `starttls`, or `implicit`
- `username`
- `password` — empty in the example
- `personality` — personality JSON file used for the local server

The command runs one script against that server and against a local `fakeimap` using the personality file, then prints a normalized diff. `-gaps` writes capabilities the remote advertised that this binary does not implement. `commands` in that file is the unimplemented commands the script saw. Tags, UIDs, `UIDVALIDITY`, `UIDNEXT`, `INTERNALDATE`, message counts the script caused, and timestamps are not differences. Capability sets in the diff keep only capabilities this binary implements.

The script reads the greeting, then `CAPABILITY`, `LOGIN`, `CAPABILITY`, `NAMESPACE`, `CREATE` a fresh mailbox, `SELECT` it, `APPEND` one fixed message, `FETCH` `FLAGS UID INTERNALDATE RFC822.SIZE ENVELOPE BODYSTRUCTURE BODY.PEEK[HEADER]`, `FETCH` `BODY[TEXT]`, `STORE` a flag, `CREATE` a second fresh mailbox, `COPY` into it, `SELECT` that mailbox, `MOVE` the message back, `SEARCH ALL`, `LIST` the probe mailboxes, `LOGOUT`. It then logs in again and deletes the mailboxes it created.

`cyrusprobe -condstore` runs a second script for `CONDSTORE` and `QRESYNC`: enable them, `CHANGEDSINCE`, `UNCHANGEDSINCE`, an unsolicited flag update, `VANISHED` versus `EXPUNGE`, and `SELECT QRESYNC`. The diff compares response kinds. It does not replace the first script. `make` does not run it against Docker.

`cyrusprobe -list` runs a third script for extended `LIST` and `CREATE` with `USE`. The diff compares response kinds, not counts. It does not replace the first two scripts. `make` does not run it against Docker.

`cyrusprobe -esearch` runs a further script for `ESEARCH` and a positive `PARTIAL` window. The diff compares response kinds. It does not replace the earlier scripts. `make` does not run it against Docker.
