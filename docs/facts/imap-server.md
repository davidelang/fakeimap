# IMAP server

Binary: `fakeimap`, at the checkout root. `make` runs `go test ./...` and then `go build -o fakeimap .`. `make clean` deletes that binary.

Run: `fakeimap <config.json>`. The one argument is the config path. Paths inside the config are relative to the process working directory, not to the config file.

Before accept, the process writes one JSON object to stdout and flushes it. The object has `listen` when a cleartext listener is bound, `implicit_tls_listen` when an implicit-TLS listener is bound, and `control` when the control socket is bound. A configured port of `0` is the bound ephemeral port.

## Config keys

- `listen` — `host:port` cleartext listener. Empty or omitted: no cleartext listener. Port `0`: ephemeral.
- `implicit_tls_listen` — `host:port` implicit TLS listener. Empty or omitted: no implicit-TLS listener. Port `0`: ephemeral.
- `cert_file` — PEM certificate for `STARTTLS` and implicit TLS.
- `key_file` — PEM private key matching `cert_file`. Not part of the repo; tests generate one.
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

Keys: `greeting`, `delimiter`, `greeting_capabilities`, `pre_login_capabilities`, `post_login_capabilities`, `other_users`, `shared_folders`, `logout_bye`, `fetch_item_order`, `select_flags`, `permanent_flags`, `select_order`, `flag_order`, `fetch_peek_body`, `fetch_seen_flags`, `move_copyuid_untagged`.

A capability listed there is advertised only if this binary implements it. `greeting_capabilities`, when set, is the greeting list instead of `pre_login_capabilities`. `fetch_item_order` is the order of items inside one `FETCH` response. `logout_bye` controls whether `LOGOUT` sends `BYE`. `other_users` and `shared_folders` are NAMESPACE prefixes; empty means NIL. `select_order` is the untagged SELECT order. `select_flags` and `permanent_flags` are the flag lists in that response. `flag_order` is the flag order in FETCH and STORE. `fetch_peek_body` answers `BODY.PEEK` with the `BODY` name. `fetch_seen_flags` puts `FLAGS` in the FETCH that sets `\Seen`. `move_copyuid_untagged` sends MOVE's `COPYUID` as an untagged OK before `EXPUNGE`, and does not send `EXISTS`, `RECENT`, or a tagged `COPYUID`.

## Dataset

Directory: `datasets/demo/`

`dataset.json` keys:

- `accounts` — objects with `username` and `password`
- `mailboxes` — objects with `name`, optional `uid_validity`, optional `messages`, optional `generated`
- `messages` — objects with `file` (path relative to the dataset directory), `flags` (IMAP flag names), and `internal_date` (`02-Jan-2006 15:04:05 -0700`)
- `generated` — `count`, `seed`, `from`, `to`, `subject`, `start` (same date form as `internal_date`), `step_seconds`

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
