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

Keys: `greeting`, `delimiter`, `pre_login_capabilities`, `post_login_capabilities`, `logout_bye`, `fetch_item_order`.

A capability listed there is advertised only if this binary implements it. `fetch_item_order` is the order of items inside one `FETCH` response. `logout_bye` controls whether `LOGOUT` sends `BYE`.

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
