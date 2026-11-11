# IMAP server

Binary: `fakeimap`, at the checkout root. `make` runs `go test ./...` and then `go build -o fakeimap .`. `make clean` deletes that binary.

Run: `fakeimap <config.json>`. The one argument is the config path. Paths inside the config are relative to the process working directory, not to the config file.

Before accept, the process writes one JSON object to stdout and flushes it. The object has `listen` when a cleartext listener is bound, `implicit_tls_listen` when an implicit-TLS listener is bound, `control` when the control socket is bound, `smtp` when the submission listener is bound, `jmap` when the JMAP listener is bound, and `sieve` when the ManageSieve listener is bound. A configured port of `0` is the bound ephemeral port.

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
- `smtp_listen` — `host:port` cleartext SMTP submission listener. Empty or omitted: no SMTP listener. Port `0`: ephemeral. The port speaks cleartext. `STARTTLS` is not offered. `AUTH PLAIN` and `AUTH LOGIN` use the same dataset passwords as IMAP. `MAIL FROM` and `RCPT TO` before a successful `AUTH` are rejected. `RCPT TO` a dataset account, as a bare name or `name@domain`, delivers `DATA` to that account's `INBOX`. Any other recipient is accepted and the message is not stored. There is no queue, relay, or DSN.
- `jmap_listen` — `host:port` cleartext HTTP JMAP listener. Empty or omitted: no JMAP listener. Port `0`: ephemeral. The port speaks cleartext. It does not satisfy the requirement for an IMAP listener.
- `sieve_listen` — `host:port` ManageSieve listener. Empty or omitted: no ManageSieve listener. Port `0`: ephemeral. It does not satisfy the requirement for an IMAP listener. Scripts are stored in memory, per dataset account, and are not executed. `CHECKSCRIPT` does not parse Sieve.

At least one listener is required. Implicit TLS requires `cert_file` and `key_file`. `STARTTLS` is offered on a cleartext connection only when a certificate is loaded and TLS has not started. `smtp_listen`, `jmap_listen`, and `sieve_listen` do not satisfy that requirement.

## JMAP

`GET /.well-known/jmap` with HTTP Basic for a dataset account returns the session. A missing or wrong password is 401. `capabilities` and that account's `accountCapabilities` are `urn:ietf:params:jmap:core`, `urn:ietf:params:jmap:mail`, and `urn:ietf:params:jmap:submission`. `primaryAccounts` points each of those at that account. `username` is the account name. `apiUrl` is `/jmap/`. `downloadUrl` is `/jmap/download/{accountId}/{blobId}/{name}?accept={type}`. `uploadUrl` is `/jmap/upload/{accountId}/`. `eventSourceUrl` is `/jmap/eventsource/?types={types}&closeafter={closeafter}&ping={ping}`. Those are path-only. `state` is a string and changes when the store changes, including an IMAP append, flag change, or expunge, and a JMAP import, update, or destroy.

One JMAP account per dataset user. The account id is the username. Mailbox ids and email ids are assigned for the life of the process and are not IMAP UIDs. `POST /jmap/upload/{accountId}/` with a non-empty body stores those bytes and returns JSON `accountId`, `blobId`, `type`, and `size` with status 201. An empty body is 400. A missing or wrong password is 401. An account id other than the authenticated user is 404. `GET` of the download URL returns an uploaded blob, or the RFC822 bytes of an `Email/get` `blobId`. An unknown blob id is 404. The event source URL is not served.

`Mailbox/get` returns `id`, `name`, `parentId`, `role`, `sortOrder`, `totalEmails`, and `unreadEmails`. `INBOX` has role `inbox`. A special-use flag maps to the matching role (`all`, `archive`, `drafts`, `flagged`, `junk`, `sent`, `trash`). `totalEmails` and `unreadEmails` are IMAP `STATUS` `MESSAGES` and `UNSEEN`. `Mailbox/query` accepts a `parentId` filter, including null, and `AND`, `OR`, or `NOT` of those filters. Sort properties are `sortOrder` and `name`.

`Email/query` accepts `filter.inMailbox`, a `receivedAt` sort, `limit`, `position`, and `calculateTotal`. A filter other than `inMailbox`, or a sort other than `receivedAt`, is `invalidArguments`. `Email/get` returns `id`, `blobId`, `threadId`, `mailboxIds`, `keywords`, `size`, `receivedAt`, `sentAt`, `subject`, `from`, `to`, `preview`, `hasAttachment`, `bodyStructure`, `headers`, and `bodyValues`. `preview` is the IMAP `PREVIEW` string, including the 256-character cap and the first `text/plain` part. An empty snippet is `""`. Keywords are `$seen`, `$flagged`, `$answered`, and `$draft` for those IMAP flags. `headers` are `{name, value}` objects. `bodyValues` follows `fetchAllBodyValues`, `fetchTextBodyValues`, `fetchHTMLBodyValues`, and `maxBodyValueBytes`, and sets `isTruncated` when the value is cut. A method argument whose name starts with `#` is a result reference to an earlier call in the same request.

`Email/import` of an uploaded blob id into exactly one mailbox appends it there, returns the new email id under `created`, and an IMAP `IDLE` on that mailbox receives `EXISTS`. The keywords `$seen`, `$flagged`, `$answered`, and `$draft` become those IMAP flags. Zero mailboxes, more than one mailbox, an unknown blob id, or an unknown mailbox id is `invalidArguments` and stores nothing. `Email/set` `destroy` removes those ids from every mailbox they are in and lists them in `destroyed`. An id that was not there is listed in `notFound`, and the others are still removed. `create` is `invalidArguments` and changes nothing, including when `update` or `destroy` is also present. `update` patches `keywords/$seen`, `keywords/$flagged`, `keywords/$answered`, and `keywords/$draft` to `true` or `null`, which sets or clears that IMAP flag, and `mailboxIds/<id>` to `true` or `null`, which adds or removes that mailbox. One email id may be in more than one mailbox. Adding a mailbox appends the message there so IMAP `IDLE` sees `EXISTS`. Removing one expunges it there so `IDLE` sees `EXPUNGE`. A successful id is listed in `updated`. Removing the last mailbox, an unknown mailbox id, any other keyword, or a value other than `true` or `null` lists that id in `notUpdated` with type `invalidProperties` and does not change it. An unknown email id is `notUpdated` with type `notFound`. Other ids in the same `update` still change. A call with `update` and `destroy` applies both. `Email/changes` with `sinceState` returns `created`, `updated`, and `destroyed` since that state, `newState` equal to the current session `state`, and `hasMoreChanges` false when the list fits in `maxChanges`. An IMAP append or a JMAP import is `created`. An IMAP flag change or a JMAP update, including a mailbox add or remove, is `updated`. An IMAP expunge, move, close, or mailbox delete that leaves another copy of the same email id is `updated`. One that removes the last copy, or a JMAP destroy, is `destroyed`. `sinceState` equal to the current state returns empty lists. An unknown `sinceState` is `cannotCalculateChanges`.

`Identity/get` returns one identity. Its `id` is `idn-` plus the account username and does not change. Its `email` is the account username. A `properties` list of `id` and `email` returns those two and no others. `id` is included even when `properties` omits it.

`EmailSubmission/set` `create` copies the stored RFC822 of `emailId` through the SMTP `DATA` accept path. `envelope.mailFrom.email` is the sender. Each `envelope.rcptTo` address is a recipient. A dataset account is appended to `INBOX` once. Any other recipient is accepted and not stored. `list-smtp` and `wait-smtp` count that submission the same way they count an SMTP `DATA`. The new id is returned under `created`. An unknown `emailId` is `notCreated` with type `notFound`. An empty `rcptTo` or a missing envelope is `notCreated` with type `invalidProperties`. Neither delivers. `onSuccessDestroyEmail` is a list of submission ids, or one id, including a creation id prefixed with `#`. Each id whose create succeeded destroys that submission's `emailId`. The same response then includes an `Email/set` whose `destroyed` lists those emails. Without `onSuccessDestroyEmail`, the email stays. `identityId` may be omitted; when set, it is that account's identity id.

`EmailSubmission/query` with `filter.emailIds` returns the ids of submissions created for those emails, in creation order, and no others. A filter without `emailIds` returns every submission for the account.

Any other method, including `Identity/set` and `EmailSubmission/get`, returns `unknownMethod`. `using` must list `urn:ietf:params:jmap:submission` or those three methods return `unknownMethod`.

## ManageSieve

`sieve_listen` speaks RFC 5804. The greeting is capability lines and then `OK`. It includes `IMPLEMENTATION`, `VERSION` `1.0`, `SASL` `PLAIN` and `CRAM-MD5`, and `SIEVE` including `fileinto` and `include`. `STARTTLS` is listed only when a certificate is loaded. `STARTTLS` returns `OK`, and the next greeting has no `STARTTLS`. `AUTHENTICATE "PLAIN"` with an initial response then works on that connection.

`AUTHENTICATE "PLAIN"` with an initial response, and `AUTHENTICATE "CRAM-MD5"`, accept a dataset account and return `NO` for a wrong password. Before that, `LISTSCRIPTS`, `GETSCRIPT`, `PUTSCRIPT`, `CHECKSCRIPT`, and `SETACTIVE` are `NO`.

`CHECKSCRIPT` of any non-empty literal returns an empty `OK` and stores nothing. That empty `OK` means no warnings. `PUTSCRIPT` stores the literal under that account and name. `GETSCRIPT` returns those bytes. An unknown name is `NO`. `LISTSCRIPTS` returns each stored name as a quoted string. The active name has `ACTIVE`. `SETACTIVE` of a stored name sets it. `SETACTIVE ""` clears it. `SETACTIVE` of an unknown name is `NO` and leaves the active name unchanged. `reset` and `set-dataset` drop every stored script.

## Log lines

One JSON object per line, append-only:

- `kind` — `command` (one client command) or `reply` (one server reply)
- `conn` — connection number, starting at 1
- `text` — the command or reply

## Personality

File: `personalities/cyrus-3137.json`

The probed Cyrus personality advertises `AUTH=LOGIN` on the greeting and before login.

Keys: `greeting`, `delimiter`, `greeting_capabilities`, `pre_login_capabilities`, `post_login_capabilities`, `other_users`, `shared_folders`, `logout_bye`, `fetch_item_order`, `select_flags`, `permanent_flags`, `select_order`, `flag_order`, `fetch_peek_body`, `fetch_seen_flags`, `move_copyuid_untagged`.

A capability listed there is advertised only if this binary implements it. `personalities/sasl.json` is the same mailbox behavior as `cyrus-3137.json`, plus `AUTH=LOGIN`, `AUTH=CRAM-MD5`, `AUTH=SCRAM-SHA-1`, `AUTH=SCRAM-SHA-256`, `AUTH=SCRAM-SHA-1-PLUS`, `AUTH=SCRAM-SHA-256-PLUS`, and `AUTH=EXTERNAL` on the greeting and pre-login lists. It is not the probed Cyrus personality. Its post-login list matches `cyrus-3137.json`, including `SORT`, `ESORT`, `SORT=DISPLAY`, and `sort_partial_count_only`. `greeting_capabilities`, when set, is the greeting list instead of `pre_login_capabilities`. `fetch_item_order` is the order of items inside one `FETCH` response. `logout_bye` controls whether `LOGOUT` sends `BYE`. `other_users` and `shared_folders` are NAMESPACE prefixes; empty means NIL. `select_order` is the untagged SELECT order. `select_flags` and `permanent_flags` are the flag lists in that response. `flag_order` is the flag order in FETCH and STORE. `fetch_peek_body` answers `BODY.PEEK` with the `BODY` name. `fetch_seen_flags` puts `FLAGS` in the FETCH that sets `\Seen`. `move_copyuid_untagged` sends MOVE's `COPYUID` as an untagged OK before `EXPUNGE`, and does not send `EXISTS`, `RECENT`, or a tagged `COPYUID`. `CONDSTORE`, when listed, adds `HIGHESTMODSEQ` to `SELECT` and `EXAMINE` and accepts `FETCH MODSEQ`, `CHANGEDSINCE`, and `STORE UNCHANGEDSINCE`. `ENABLE CONDSTORE` makes a later flag change from another connection arrive as one unsolicited `FETCH` with `FLAGS` and `MODSEQ`. `ENABLE QRESYNC` also enables `CONDSTORE`. Expunges reported to that connection are `VANISHED`, not `EXPUNGE`. `SELECT` or `EXAMINE` with `QRESYNC` is `BAD` until `QRESYNC` is enabled; after that, a known UID that was expunged is reported as `VANISHED (EARLIER)`. `personalities/condstore.json` lists `CONDSTORE` and `QRESYNC` for the second probe's loopback test.

`LIST-EXTENDED`, `CHILDREN`, `SPECIAL-USE`, `LIST-STATUS`, and `CREATE-SPECIAL-USE`, when listed, answer an extended `LIST` and `CREATE` with `USE`. A two-argument `LIST` is unchanged and still carries `\HasChildren` or `\HasNoChildren`. `RETURN (CHILDREN)` includes those flags. A mailbox special-use flag is included when `SPECIAL-USE` is listed. `RETURN (STATUS (...))` is followed by `STATUS` for each listed mailbox, using the same items and counts as the `STATUS` command. `LIST (SPECIAL-USE)` returns only mailboxes that have a special-use flag. `LIST (SUBSCRIBED)` returns only that account's subscribed mailboxes, and adds `\Subscribed` to the flags a plain `LIST` of that mailbox already uses. The reference and pattern still apply. `LSUB` returns those same names and does not need `LIST-EXTENDED`. A plain `LIST`, and `LIST` with `RETURN`, do not add `\Subscribed` and do not change which mailboxes they return. Without `LIST-EXTENDED`, `RETURN` and a parenthesized selection are tagged `BAD`. `CREATE name (USE (\Sent))` stores that flag when `CREATE-SPECIAL-USE` is listed. The accepted flags are `\All`, `\Archive`, `\Drafts`, `\Flagged`, `\Junk`, `\Sent`, and `\Trash`. A flag already used by another mailbox is tagged `NO` and creates nothing. Without `CREATE-SPECIAL-USE`, `USE` is tagged `BAD`. `personalities/list.json` is the Cyrus personality plus these five capabilities. `personalities/cyrus-3137.json` lists them too.

Each account has a subscription set. `INBOX` starts subscribed. A mailbox created later is not. `SUBSCRIBE` of an existing mailbox adds it. `SUBSCRIBE` of a missing mailbox is tagged `NO`. `UNSUBSCRIBE` removes it and is tagged `OK` when it was not subscribed. `DELETE` drops it. `RENAME` keeps the subscription on the new name. `reset` and `set-dataset` leave only `INBOX` subscribed.

`COMPRESS=DEFLATE`, when listed, accepts `COMPRESS DEFLATE`. The tagged `OK` is cleartext. After that line, both directions are raw deflate (RFC 4978: zlib window bits -15, not a zlib header), and each write is sync-flushed so the peer can read it. A later `CAPABILITY` still lists `COMPRESS=DEFLATE`. A second `COMPRESS` on that connection is tagged `NO`. `COMPRESS` with no algorithm, or an unknown algorithm, is tagged `BAD`. Without the capability, `COMPRESS DEFLATE` is tagged `NO` and the connection stays cleartext. `personalities/compress.json` is the Cyrus personality plus `COMPRESS=DEFLATE`. `personalities/cyrus-3137.json` lists it on the pre-login and post-login lists, where Cyrus advertises it, and not on the greeting.

`ESEARCH`, when listed, answers `SEARCH RETURN` and `UID SEARCH RETURN` with one `* ESEARCH (TAG "...")` line. `UID` is present only on a UID command. The result items are `COUNT`, then `MIN`, then `MAX`, then `ALL`, then `PARTIAL`, and only the ones the command named. `COUNT` is the number of matches, including `0`. `MIN`, `MAX`, and `ALL` are omitted when nothing matches. `ALL` is a sequence set of the whole result. An empty `RETURN ()` means `ALL`. Without `ESEARCH`, `RETURN` is tagged `BAD`. Plain `SEARCH` and `UID SEARCH` stay `* SEARCH`.

`SEARCH` and `UID SEARCH` match `TEXT` as a case-insensitive substring of the rendered RFC822, including headers, `BODY` as that substring of the body after the header break, and `CC` as that substring of the Cc header. `OR` matches a message that matches either of the next two keys. `NOT` matches a message that does not match the next key. A parenthesized list matches only when every key inside it matches. The three nest. A top-level sequence of keys is still AND. `ANSWERED` matches `\Answered`. `UNANSWERED` matches a message without it. `NEW` matches `\Recent` and not `\Seen`. `KEYWORD` matches one flag, a system flag or a keyword such as `$Forwarded`, compared case-insensitively. `UNKEYWORD` is the complement. `KEYWORD \Recent` uses the session `\Recent` bit. `OLD`, `RECENT`, `DRAFT`, and any other unknown key stay tagged `BAD`. `KEYWORD` or `UNKEYWORD` without a flag is tagged `BAD`. A missing key after `OR` or `NOT`, or an unclosed parenthesis, is tagged `BAD`.

`SEARCH=FUZZY`, when listed, accepts `FUZZY` before one search key on `SEARCH` and `UID SEARCH`, including after `CHARSET` and with `RETURN`. `FUZZY` consumes that key. An `OR`, a `NOT`, or a parenthesized list passes the mark to every text leaf under it. A text leaf is `TEXT`, `BODY`, `FROM`, `TO`, `CC`, or `SUBJECT`. An exact case-insensitive substring still matches. A text leaf also matches when the search string is at least 3 characters and one insertion, deletion, or substitution makes it equal to a word in that text. A word is a run of letters and digits. Comparison is case-insensitive. `FUZZY SUBJECT aple` matches a subject word `apple`. `FUZZY SUBJECT ap` does not match `apple` by the edit rule. A flag, date, size, or `UID` leaf under `FUZZY` stays exact. The reply is the same `SEARCH` or `ESEARCH` line as a search without `FUZZY`. It has no `RELEVANCY` item. Without `SEARCH=FUZZY`, `FUZZY` is tagged `BAD`. `SORT` and `THREAD` still reject `FUZZY`. `personalities/fuzzy.json` is the Cyrus personality plus `SEARCH=FUZZY`.

`MULTISEARCH`, when listed, answers `ESEARCH IN` for `subtree`, `personal`, and `subscribed`. The command does not need a selected mailbox. `subtree` is the named mailbox plus every mailbox whose name starts with that name and the personality delimiter. A name that only shares a prefix, such as `INBOX` and `INBOX2` when the delimiter is `/`, is not a descendant. `personal` is every mailbox in the account. `subscribed` is that account's subscription set. `INBOX` starts in the set. A missing subtree mailbox, `IN (mailboxes ...)`, and any other source are tagged `BAD`. Without `MULTISEARCH`, the `ESEARCH` command is tagged `BAD`. `CHARSET` and `FUZZY` follow the `SEARCH` and `SEARCH=FUZZY` rules. `UID SEARCH` is unchanged.

Each mailbox with hits is one line: `* ESEARCH (TAG "<tag>" MAILBOX "<name>" UIDVALIDITY <n>) UID ALL <uids>` or the same correlator with `UID COUNT <n>`. A mailbox with no hits produces no line. The tag is the command tag. The numbers are UIDs in ascending order, never sequence numbers and never `*`. `UIDVALIDITY` is that mailbox's value. An empty scope is tagged `OK` and returns no lines. `personalities/multisearch.json` is the Cyrus personality plus `MULTISEARCH`. `personalities/cyrus-3137.json` lists it on the pre-login and post-login lists, where that server advertises it, and not on the greeting. `personalities/sasl.json` lists it on those same lists. It is not in the gaps file.

`PARTIAL`, when listed as well, answers `RETURN (PARTIAL m:n ...)`. The response repeats that range. `1` is the first match and `-1` is the last. The set is the overlap, in result order, or `NIL` when the range misses the result. `COUNT` stays the full length. `PARTIAL` together with `ALL`, or `PARTIAL` without the `PARTIAL` capability, is tagged `BAD`. `personalities/esearch.json` is the Cyrus personality plus `ESEARCH` and `PARTIAL`. `personalities/cyrus-3137.json` lists both too.

`SORT`, when listed, answers `SORT (key) UTF-8 criteria` and `UID SORT` with one `* SORT` line of sequence numbers or UIDs. The key is `ARRIVAL`, `CC`, `DATE`, `FROM`, `SIZE`, `SUBJECT`, `TO`, or `REVERSE` of one of those. `SORT=DISPLAY`, when listed as well, also accepts `DISPLAYFROM` and `DISPLAYTO`. Those sort by the display name of the first address, case-insensitively, and use the address when the name is empty. `FROM` and `TO` still use the address, so a name that sorts differently from its address produces a different order. Without `SORT=DISPLAY`, `DISPLAYFROM` and `DISPLAYTO` are tagged `BAD`. `DATE` is the Date header, or the internal date when that header is missing. Address keys use the first mailbox. Ties break by ascending UID. The charset is `UTF-8` or `US-ASCII`. An unknown key or charset is tagged `BAD`. Criteria `SEARCH` already rejects are tagged `BAD`. `FUZZY` is tagged `BAD` even when `SEARCH` accepts it. Without `SORT`, the command is tagged `BAD`.

`ESORT`, when listed as well, answers `SORT RETURN` with one `* ESEARCH` line, the same items as `SEARCH RETURN`. `ALL` and `PARTIAL` are in sort order. `MIN` and `MAX` stay the lowest and highest numbers. `COUNT` is the full length. Without `ESORT`, `RETURN` is tagged `BAD`. `sort_partial_count_only`, when true, accepts `RETURN (PARTIAL m:n ...)` but sends `COUNT` and does not send `PARTIAL`. `personalities/sort.json` is the Cyrus personality plus `SORT`, `ESORT`, and `SORT=DISPLAY`, and that field is false, so the window is returned. `personalities/cyrus-3137.json` lists `SORT`, `ESORT`, and `SORT=DISPLAY` and sets `sort_partial_count_only` because that server omits the window. `SORT=MODSEQ` and `SORT=UID` are not implemented.

`THREAD=REFERENCES`, `THREAD=ORDEREDSUBJECT`, and `THREAD=REFS`, when listed, answer `THREAD` and `UID THREAD` for that algorithm. `THREAD=REFS` is the same algorithm as `THREAD=REFERENCES`. The charset is `UTF-8` or `US-ASCII`, the same position as `SORT`. An unknown algorithm, an algorithm the personality does not list, or an unknown charset is tagged `BAD`. Criteria `SEARCH` already rejects are tagged `BAD`. `FUZZY` is tagged `BAD` even when `SEARCH` accepts it. `REFERENCES` nests a message under the result whose `Message-ID` is its last `References` id, or its first `In-Reply-To` id when `References` has none. A named parent that is not in the result is a placeholder: two or more children stay in one thread whose first element is a parenthesized list. One such child is promoted. A message with no parent is its own thread. Siblings are ordered by sent date, then by ascending UID. `ORDEREDSUBJECT` groups messages whose base subject is equal after stripping leading `Re:` and `Fwd:` prefixes, case-insensitively. Groups are ordered by that base subject. Inside a group, messages are ordered by sent date, then by ascending UID. The earliest is the root. One child is written after the root. Further children are siblings, so there are no grandchildren. `personalities/thread.json` is the Cyrus personality plus the three capabilities. `personalities/cyrus-3137.json` lists them too, on the pre-login and post-login lists. They are not in the gaps file. `UTF8=ACCEPT` stays off that personality.

`BINARY`, when listed, answers `FETCH` and `UID FETCH` of `BINARY.PEEK[section]` and `BINARY[section]`. The section is one numeric part, including a dotted part such as `1.1`. The reply name is `BINARY` plus that section. It is not `BINARY.PEEK`. `base64` and `quoted-printable` are removed. `7bit`, `8bit`, `binary`, and a missing encoding stay as stored. A `<offset.length>` counts those decoded octets. The reply origin, when a partial was requested, is `<` and the start offset and `>`, not `<offset.length>`. `BINARY.PEEK` does not set `\Seen`. `BINARY` without `PEEK` does, and `fetch_seen_flags` puts `FLAGS` in that `FETCH`. An empty section, `HEADER`, `TEXT`, `MIME`, or a multipart part is tagged `BAD`. A numeric part that is not in the message is `NIL`. Any other encoding, or a `base64` or `quoted-printable` body that does not decode, is tagged `NO` with `[UNKNOWN-CTE]` and no `FETCH` data. Without `BINARY`, both items are tagged `BAD`. `BINARY.SIZE`, an `APPEND` of a binary literal, and `URLAUTH=BINARY` are not implemented. `personalities/binary.json` is the Cyrus personality plus `BINARY`. `personalities/cyrus-3137.json` lists `BINARY` on the pre-login and post-login lists, where that server advertises it, and not on the greeting. `personalities/sasl.json` lists it on those same lists. `URLAUTH=BINARY` stays in the gaps file.

`PREVIEW`, when listed, answers `FETCH` and `UID FETCH` of `PREVIEW` with `PREVIEW` and an nstring. The text is the first `text/plain` part, or the body when the message itself is `text/plain`. Its transfer encoding is removed the same way `BINARY` removes one. The charset is decoded the same way JMAP body text is. A charset that decode reports as a problem is an empty snippet. Each whitespace run becomes one space, the ends are trimmed, and the result is at most 256 characters. A longer body is that prefix. A message with no `text/plain` part, or an empty snippet, is `PREVIEW NIL`. `PREVIEW` does not set `\Seen`. `PREVIEW (LAZY)` is tagged `BAD`. Without `PREVIEW`, the item is tagged `BAD`. HTML is not parsed. `personalities/preview.json` is the Cyrus personality plus `PREVIEW` on the pre-login and post-login lists, and not on the greeting.

`UTF8=ACCEPT`, when listed, accepts `ENABLE UTF8=ACCEPT` on that connection. The store keeps mailbox names as Unicode. Before enable, `CREATE`, `LIST`, `SELECT`, `STATUS`, `APPEND`, `DELETE`, `RENAME`, and `COPY` or `MOVE` use modified UTF-7 on the wire. After enable, that connection passes UTF-8 through, and a connection that has not enabled it still sees modified UTF-7. After enable, `SEARCH` with a `CHARSET` argument is tagged `BAD`, and `SORT` or `THREAD` with a charset argument (`UTF-8` or `US-ASCII`) is tagged `BAD`. The same search without `CHARSET` treats the text as UTF-8, so an encoded-word subject matches the Unicode text. Before enable, `SEARCH CHARSET UTF-8` and `SEARCH CHARSET US-ASCII` still work, and `SORT` and `THREAD` still require `UTF-8` or `US-ASCII`. `personalities/utf8.json` is the Cyrus personality plus `UTF8=ACCEPT`. `cyrusprobe -utf8` runs a further script that creates a modified UTF-7 mailbox and searches with `CHARSET UTF-8`. The diff compares response kinds. It does not replace the earlier scripts. `make` does not run it against Docker.

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

Ops: `status`, `reset`, `set-personality`, `set-dataset`, `disconnect`, `inject`, `list-mail`, `fetch-mail`, `accounts`, `freeze-clock`, `thaw-clock`, `arm-fault`, `clear-faults`, `list-smtp`, `wait-smtp`. `status` includes `listen`, `implicit_tls_listen`, `control`, `smtp`, `jmap`, and `sieve`; `smtp`, `jmap`, and `sieve` are the bound `host:port`, or `""` when that listener is not up.

Request fields: `path`, `conn`, `mailbox`, `user`, `username`, `password`, `raw`, `uid`, `time`, `id`, `command`, `times`, `do`, `delay_ms`, `bytes`, `text`, `count`, `offset`, `tag`, `order`, `timeout_ms`.

`list-smtp` returns `messages`. Each item has `from` and `recipients`. A recipient has `address` and `result` (`delivered` or `discarded`). A delivered recipient was appended to `INBOX`. A discarded recipient was accepted and not stored. `wait-smtp` takes `count`. Zero returns immediately. A positive count also takes `timeout_ms` and waits until that many SMTP messages have been accepted since the call. If `timeout_ms` passes first, the reply is `ok` false.

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

`cyrusprobe -compress` runs a further script that sends `COMPRESS DEFLATE` and decompresses both directions before it compares response kinds. It does not replace the earlier scripts. `make` does not run it against Docker.

`cyrusprobe -sort` runs a further script for `SORT` and one positive `ESORT` `PARTIAL` window. The messages it appends differ in `FROM`, `DATE`, `SUBJECT`, and `SIZE`, and one command uses `REVERSE`. The diff compares response kinds. It does not replace the earlier scripts. `make` does not run it against Docker.

`cyrusprobe -display` runs a further script for one `UID SORT (DISPLAYFROM)` whose display name sorts differently from the address. The diff compares response kinds. It does not replace the earlier scripts. `make` does not run it against Docker.

`cyrusprobe -thread` runs a further script for one `UID THREAD REFERENCES` and one `UID THREAD ORDEREDSUBJECT`. The messages have distinct subjects and one reply whose parent is in the set. The diff compares thread shape, with message numbers folded to `N`. It does not replace the earlier scripts. `make` does not run it against Docker.

`cyrusprobe -binary` runs a further script for one `BINARY.PEEK[1]` of a `base64` part and one `<0.5>` partial of that part. The diff compares response kinds, including the `BINARY` item token. It does not compare the literal bytes. It does not replace the earlier scripts. `make` does not run it against Docker.

`cyrusprobe -fuzzy` runs a further script for one `UID SEARCH RETURN (COUNT) FUZZY TEXT` of a word in an appended message. The diff compares response kinds. `COUNT` is recorded and the number is not. It does not replace the earlier scripts. `make` does not run it against Docker.

`cyrusprobe -multisearch` runs a further script for one `ESEARCH IN (subtree <fresh>) RETURN (ALL) TEXT` whose word is in one child of that mailbox. The diff compares response kinds. The correlator is skipped and `UID ALL` is recorded. It does not replace the earlier scripts. `make` does not run it against Docker.

`cyrusprobe -preview` runs a further script for one `FETCH` of `PREVIEW`. The diff compares response kinds. It does not replace the earlier scripts. `make` does not run it against Docker.
