# ENGINEERING_LOG


## 2026-08-21 - clone-orchestration-project execution start

- Approved plan: sandbox/plans/clone-orchestration-project-20260821-1819-plan.md
- Work: add ./clone-orchestration-project to clone this example into git_home/<name> with orchestration + master worktrees
- First action per STANDARD BLOCK; host has no ./build_app (phase gate: bash -n + forensic read)

## 2026-08-21 - clone-orchestration-project landed (staged)

- Added ./clone-orchestration-project (discovery, clone --no-hardlinks as primary, orchestration+master worktrees, fix-perms --skip-sudoers)
- README.md + project-facts.md document the helper
- Verified: bash -n; --dry-run SomeName from this tree, git_home, and --source (same source/dest)
- git commit blocked: ai-orchestrator has no user.name/email (changes remain staged)

## 2026-08-21 - clone-seed-worktree-config execution start

- Approved plan: sandbox/plans/clone-seed-worktree-config-20260821-1840-plan.md
- Work: seed dest+master project.config via ve_seed_worktree_project_config; dest-root DAC 2775; --repair; repair torque-ford-gas-tracking
- First action per STANDARD BLOCK; host has no ./build_app

## 2026-08-21 - clone-seed-worktree-config script landed; live --repair blocked

- clone-orchestration-project now seeds dest+master project.config via ve_seed_worktree_project_config, dest-root chown 2775, and --repair
- Live --repair of torque-ford-gas-tracking failed in this session: dest is 770 dlang:dlang and sudo is blocked (Landlock no-new-privileges)
- Human: run as dlang ./clone-orchestration-project --repair /home/dlang/git/torque-ford-gas-tracking

## 2026-08-21 - clone-landlock-exec-dac execution start

- Approved plan: sandbox/plans/clone-landlock-exec-dac-20260821-1901-plan.md
- Work: fix-perms lists agent-landlock+env; clone do_fix_perms uses this tree fix-perms with explicit DEST and DEST/master; force landlock 775
- First action per STANDARD BLOCK; host has no ./build_app

## 2026-08-21 - clone-landlock-exec-dac landed

- fix-perms: agent-landlock and env in executable-scripts list (2775/775 o+x)
- clone-orchestration-project: this-tree fix-perms --skip-sudoers DEST and DEST/master; force landlock 775
- Live dest not repaired from this session (sudo/Landlock nnp). Human: --repair torque-ford-gas-tracking as dlang

## 2026-11-10 - imap session core

- Approved plan: sandbox/plans/imap-session-core-20261110-2318-plan.md
- Work: Go IMAP session core with a Cyrus 3.13-shaped personality, in-memory store, loopback tests, and a makefile build

## 2026-11-11 - one FLAGS when FETCH sets \Seen

- Approved plan: sandbox/plans/fetch-seen-flags-20261111-0037-plan.md
- A FETCH that already returns FLAGS, including ALL, FAST, and FULL, does not send a second FLAGS line when it sets \Seen

## 2026-11-11 - control socket and fault injection

- Approved plan: sandbox/plans/faults-control-20261111-0048-plan.md
- Work: mode-0600 Unix control socket (reset, personality, dataset, inject, accounts, clock) and armed reply, reorder, and certificate faults

## 2026-11-11 - cyrus probe execution start

- Approved plan: sandbox/plans/cyrus-probe-20261111-0136-plan.md
- Work: separate probe command, operator config example, self-test of one script against two local servers, then match Cyrus 3.13.7 reply shapes for commands this binary already implements
- make stays Docker-free; lab password stays out of the repo and this log

## 2026-11-11 - cyrus probe blocked on a running lab container

- Approved plan: sandbox/plans/cyrus-probe-20261111-0136-plan.md
- liveimap-cyrus-3137 is already running. It was not stopped and not recreated. The Cyrus probe did not run, and the personality was not invented.
- Probe command, empty-password example, self-test, and docs are in the working tree and are not committed.

## 2026-11-11 - probe the running Cyrus container

- Approved plan: sandbox/plans/cyrus-probe-running-20261111-0153-plan.md
- Work: keep the uncommitted probe, run it against the already-running liveimap-cyrus-3137 published IMAP port, and match reply shapes. Do not recreate or stop that container.
- The lab password stays in the gitignored operator file and out of this log.

## 2026-11-11 - Cyrus session probe landed

- Approved plan: sandbox/plans/cyrus-probe-running-20261111-0153-plan.md
- Separate probe matched the already-running liveimap-cyrus-3137 on its published IMAP port. The container was not recreated and was not stopped.
- Personality fields record the Cyrus reply shapes this binary already implements. Unimplemented capabilities stay in personalities/cyrus-3137.gaps.json and are not advertised.
- The lab password stayed in the gitignored operator file.

## 2026-11-11 - SASL mechanisms execution start

- Approved plan: sandbox/plans/sasl-mechs-20261111-0215-plan.md
- Work: LOGIN, CRAM-MD5, SCRAM-SHA-1, SCRAM-SHA-256, the PLUS forms, and EXTERNAL against the dataset password. No DIGEST-MD5. personalities/cyrus-3137.json stays unchanged.

## 2026-11-11 - SASL mechanisms landed

- Approved plan: sandbox/plans/sasl-mechs-20261111-0215-plan.md
- AUTHENTICATE implements LOGIN, CRAM-MD5, SCRAM-SHA-1, SCRAM-SHA-256, both PLUS forms, and EXTERNAL. DIGEST-MD5 is not implemented. personalities/cyrus-3137.json was not edited.

## 2026-11-11 - CONDSTORE and QRESYNC

- Approved plan: sandbox/plans/condstore-qresync-20261111-0248-plan.md
- Work: speak CONDSTORE and QRESYNC when a personality advertises them, add a second probe script, and match the running Cyrus lab if it is liveimap-cyrus-3137.

## 2026-11-11 - CONDSTORE and QRESYNC landed

- Personalities that list CONDSTORE and QRESYNC speak HIGHESTMODSEQ, MODSEQ, CHANGEDSINCE, UNCHANGEDSINCE, ENABLE, VANISHED, and SELECT QRESYNC.
- The second probe kind diff against the already-running liveimap-cyrus-3137 was empty. That container was not recreated and was not stopped. CONDSTORE and QRESYNC are on personalities/cyrus-3137.json and are gone from the gaps file.

## 2026-11-11 - extended LIST execution start

- Implementing LIST-EXTENDED, CHILDREN, SPECIAL-USE, LIST-STATUS, and CREATE-SPECIAL-USE, plus a third probe that compares reply kinds.
- liveimap-cyrus-3137 is already running. It will not be recreated or stopped. ESEARCH and COMPRESS=DEFLATE stay unimplemented.

## 2026-11-11 - extended LIST landed

- Personalities that list LIST-EXTENDED, CHILDREN, SPECIAL-USE, LIST-STATUS, and CREATE-SPECIAL-USE answer extended LIST and CREATE USE. A two-argument LIST is unchanged. Without those capabilities the new syntax is tagged BAD.
- The third probe kind diff against the already-running liveimap-cyrus-3137 was empty. That container was not recreated and was not stopped. The five capabilities are on personalities/cyrus-3137.json and are gone from the gaps file. ESEARCH and COMPRESS=DEFLATE stay unimplemented.

## 2026-11-11 - AUTH=LOGIN on Cyrus personality

- Approved plan: sandbox/plans/cyrus-auth-login-20261111-0351-plan.md
- Work: put AUTH=LOGIN only on the Cyrus capability lists the already-running liveimap-cyrus-3137 probe names, and drop it from the gaps file. Do not recreate or stop that container.
- The lab password stays in the gitignored operator file and out of this log.

## 2026-11-11 - AUTH=LOGIN on Cyrus personality landed

- AUTH=LOGIN is on the greeting and pre-login lists in personalities/cyrus-3137.json, not on the post-login list. It is gone from the gaps file. personalities/sasl.json still advertises it.
- The original probe diff against the already-running liveimap-cyrus-3137 was empty. That container was not recreated and was not stopped.

## 2026-11-11 - AUTH=LOGIN on fixture personalities

- Work: put AUTH=LOGIN beside AUTH=PLAIN on the greeting and pre-login lists of personalities/list.json and personalities/condstore.json, and say in docs/facts/imap-server.md that the probed Cyrus personality advertises it there. No other capability changes. personalities/cyrus-3137.json and personalities/sasl.json stay as they are.

## 2026-11-11 - AUTH=LOGIN on fixture personalities landed

- AUTH=LOGIN is beside AUTH=PLAIN on the greeting and pre-login lists in personalities/list.json and personalities/condstore.json, and not on the post-login lists. personalities/cyrus-3137.json and personalities/sasl.json are unchanged. docs/facts/imap-server.md says the probed Cyrus personality advertises AUTH=LOGIN on the greeting and before login.

## 2026-11-11 - ESEARCH and PARTIAL

- SEARCH RETURN answers with one ESEARCH line when the personality lists ESEARCH. PARTIAL slices that result. personalities/esearch.json advertises both. cyrusprobe -esearch compares response kinds. SORT, ESORT, and COMPRESS=DEFLATE are unchanged.

## 2026-11-11 - COMPRESS=DEFLATE

- COMPRESS DEFLATE sends a cleartext tagged OK, then both directions are raw deflate with a sync flush. personalities/compress.json advertises COMPRESS=DEFLATE. The running liveimap-cyrus-3137 kind diff was empty, so the capability is on cyrus-3137.json and gone from the gaps file. cyrusprobe -compress decompresses before it compares response kinds. A second COMPRESS is NO. SORT and ESORT are unchanged.

## 2026-11-11 - SORT and ESORT execution start

- Approved plan: sandbox/plans/sort-esort-20261111-0443-plan.md
- Work: answer SORT and UID SORT, including ESORT RETURN PARTIAL, when the personality advertises them. THREAD, SORT=DISPLAY, SORT=MODSEQ, and SORT=UID stay gaps.
- A personality field sort_partial_count_only omits the PARTIAL window. The lab container is not recreated or stopped. The lab password stays out of this log.

## 2026-11-11 - SORT and ESORT landed

- Approved plan: sandbox/plans/sort-esort-20261111-0443-plan.md
- SORT and UID SORT return sequence numbers or UIDs for one key, and ESORT reuses ESEARCH with ALL and PARTIAL in that order. personalities/sort.json returns the window.
- The already-running liveimap-cyrus-3137 omits the PARTIAL window, so personalities/cyrus-3137.json sets sort_partial_count_only and lists SORT and ESORT. Those two are gone from the gaps file. SORT=DISPLAY, SORT=MODSEQ, and SORT=UID stay unimplemented. The container was not recreated and was not stopped. cyrusprobe -sort kind diff was empty.
- The lab password stayed in the gitignored operator file.

## 2026-11-11 - smtp submission execution start

- Approved plan: sandbox/plans/smtp-submit-20261111-0504-plan.md
- Work: smtp_listen, AUTH/MAIL/RCPT/DATA into INBOX or discard, list-smtp and wait-smtp
- First action per standard-plan-compliance-block.md

## 2026-11-11 - jmap read execution start

- Approved plan: sandbox/plans/jmap-read-20261111-0525-plan.md
- Work: jmap_listen, session, Mailbox/get, Email/query, Email/get; unknown methods stay unknownMethod
- First action per standard-plan-compliance-block.md

## 2026-11-11 - jmap read landed

- Approved plan: sandbox/plans/jmap-read-20261111-0525-plan.md
- jmap_listen binds a cleartext HTTP listener. GET /.well-known/jmap returns the session. Mailbox/get, Mailbox/query, Email/query, and Email/get read the IMAP store. Email/set, Email/import, Email/changes, and EmailSubmission/set return unknownMethod.

## 2026-11-11 - status lists SMTP and JMAP

- Approved plan: sandbox/plans/status-listeners-20261111-0554-plan.md
- Control status includes smtp and jmap as the bound host:port, or an empty string when that listener is not up. listen, implicit_tls_listen, and control are unchanged.

## 2026-11-11 - JMAP upload import destroy and changes

- Approved plan: sandbox/plans/jmap-write-20261111-0559-plan.md
- Upload stores a blob and download returns it or an Email/get RFC822 blob. Email/import appends that blob into one mailbox. Email/set destroy expunges. Email/set create or update is invalidArguments and destroys nothing. Email/changes lists IMAP append, flag change, and expunge, plus JMAP import and destroy. EmailSubmission stays unknownMethod.

## 2026-11-11 - JMAP Email/set update

- Implementing Email/set update for keywords and mailbox membership (jmap-update-20261111-0630-plan.md). create stays invalidArguments. destroy stays as it is.

## 2026-11-11 - JMAP copy change records

- An unknown mailboxIds null is invalidProperties and changes nothing. Expunging one copy of a multi-mailbox email is updated while another copy remains.

## 2026-11-11 - JMAP EmailSubmission

- Implementing urn:ietf:params:jmap:submission, Identity/get, EmailSubmission/set, and EmailSubmission/query. Delivery uses the SMTP DATA accept path (jmap-submission-20261111-0700-plan.md).

## 2026-11-11 - managesieve execution start

- Approved plan: sandbox/plans/managesieve-20261111-0721-plan.md
- Work: sieve_listen, in-memory ManageSieve script store, CHECKSCRIPT accepts text and does not parse or store, scripts are not executed
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - managesieve landed

- sieve_listen binds a ManageSieve listener. Scripts stay in memory per account and are not executed. CHECKSCRIPT accepts a non-empty literal, returns an empty OK, and stores nothing.

## 2026-11-11 - UTF8=ACCEPT

- A personality that lists UTF8=ACCEPT accepts ENABLE UTF8=ACCEPT on that connection. Mailbox names stay Unicode. The wire form is modified UTF-7 until enable, then UTF-8. SEARCH CHARSET and a SORT charset are rejected only after enable. personalities/utf8.json advertises it. cyrusprobe -utf8 compares reply kinds and does not replace the earlier scripts.

## 2026-11-11 - THREAD REFERENCES and ORDEREDSUBJECT

- Approved plan: sandbox/plans/thread-20261111-0801-plan.md
- Work: THREAD and UID THREAD for REFERENCES, REFS, and ORDEREDSUBJECT. personalities/thread.json advertises the three. cyrusprobe -thread compares thread shape and does not replace the earlier scripts.
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - THREAD REFERENCES and ORDEREDSUBJECT landed

- THREAD and UID THREAD answer REFERENCES, REFS, and ORDEREDSUBJECT. REFS uses the REFERENCES tree. personalities/thread.json advertises the three. After UTF8=ACCEPT a charset argument is BAD.
- The already-running liveimap-cyrus-3137 thread-shape diff was empty, so those three capabilities are on cyrus-3137.json and gone from the gaps file. UTF8=ACCEPT was not added. The container was not recreated and was not stopped. cyrusprobe -thread does not replace the earlier scripts.

## 2026-11-11 - SORT=DISPLAY

- Approved plan: sandbox/plans/sort-display-20261111-0822-plan.md
- Work: accept DISPLAYFROM and DISPLAYTO when SORT=DISPLAY is advertised, sorting by the address phrase and falling back to the address. FROM and TO stay address order. personalities/sort.json advertises it. cyrusprobe -display is a further script and does not replace the earlier scripts. SORT=UID and SORT=MODSEQ stay gaps.
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - SORT=DISPLAY landed

- DISPLAYFROM and DISPLAYTO sort by the address phrase, case-insensitively, and use the address when the phrase is empty. FROM and TO stay address order. Without SORT=DISPLAY those keys are BAD. personalities/sort.json advertises SORT=DISPLAY.
- The already-running liveimap-cyrus-3137 display kind diff was empty, so SORT=DISPLAY is on personalities/cyrus-3137.json and gone from the gaps file. SORT=UID and SORT=MODSEQ stay unimplemented. The container was not recreated and was not stopped. cyrusprobe -display does not replace the earlier scripts.

## 2026-11-11 - BINARY.PEEK

- Approved plan: sandbox/plans/binary-peek-20261111-0842-plan.md
- Work: FETCH and UID FETCH of BINARY.PEEK and BINARY decode base64 and quoted-printable. personalities/binary.json advertises BINARY. cyrusprobe -binary is a further script and does not replace the earlier scripts. BINARY.SIZE, binary APPEND, and URLAUTH=BINARY stay unimplemented.
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - BINARY.PEEK landed

- FETCH and UID FETCH of BINARY.PEEK and BINARY return the part with base64 and quoted-printable removed. The reply name is BINARY, and a partial origin is the start offset. BINARY.PEEK does not set \Seen. BINARY does.
- personalities/binary.json advertises BINARY. The already-running liveimap-cyrus-3137 kind diff was empty, so BINARY is on cyrus-3137.json and sasl.json where that server sends it, and gone from the gaps file. BINARY.SIZE, binary APPEND, and URLAUTH=BINARY stay unimplemented. The container was not recreated and was not stopped. cyrusprobe -binary does not replace the earlier scripts.

## 2026-11-11 - SEARCH TEXT BODY CC

- Approved plan: sandbox/plans/search-text-20261111-0913-plan.md
- Work: SEARCH and UID SEARCH accept TEXT, BODY, and CC. TEXT is the rendered RFC822. BODY is the body after the header break. CC is the Cc header. OR, NOT, parentheses, and FUZZY stay BAD.
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - SEARCH TEXT BODY CC landed

- SEARCH and UID SEARCH match TEXT against the rendered RFC822, BODY against the body after the header break, and CC against the Cc header. Comparison is a case-insensitive substring. FROM, TO, and SUBJECT are unchanged. OR, NOT, parentheses, and FUZZY stay BAD.
- Loopback covers CHARSET UTF-8, a body-only word, a generated message, and ESEARCH RETURN (ALL) and RETURN (COUNT). No personality file changed.

## 2026-11-11 - SEARCH OR and NOT

- Approved plan: sandbox/plans/search-bool-20261111-0923-plan.md
- Work: SEARCH and UID SEARCH accept OR, NOT, and a parenthesized key list. A top-level sequence stays AND. FUZZY stays BAD.
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - SEARCH OR and NOT landed

- SEARCH and UID SEARCH accept OR, NOT, and a parenthesized key list. OR matches either key. NOT matches when its key does not. A parenthesized list matches only when every key inside it matches. The three nest. A top-level sequence stays AND.
- FUZZY and any other unknown key stay BAD. A missing key after OR or NOT, or an unclosed parenthesis, is BAD. Loopback covers CHARSET UTF-8 and ESEARCH RETURN (ALL). No personality file changed.

## 2026-11-11 - SEARCH flag keys

- Approved plan: sandbox/plans/search-flags-20261111-0932-plan.md
- Work: SEARCH and UID SEARCH accept ANSWERED, UNANSWERED, NEW, KEYWORD, and UNKEYWORD. FUZZY, OLD, RECENT, and DRAFT stay BAD.
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - SEARCH flag keys landed

- SEARCH and UID SEARCH accept ANSWERED, UNANSWERED, NEW, KEYWORD, and UNKEYWORD. ANSWERED is \Answered. NEW is \Recent and not \Seen. KEYWORD matches one system flag or keyword, including $Forwarded, case-insensitively. UNKEYWORD is the complement. KEYWORD \Recent uses the session \Recent bit.
- FUZZY, OLD, RECENT, DRAFT, and any other unknown key stay BAD. KEYWORD or UNKEYWORD without a flag is BAD. Loopback covers NOT, OR, and ESEARCH RETURN (ALL). No personality file changed.

## 2026-11-11 - SEARCH=FUZZY

- Approved plan: sandbox/plans/search-fuzzy-20261111-0941-plan.md
- Work: SEARCH and UID SEARCH accept FUZZY before one key when SEARCH=FUZZY is advertised. A text leaf keeps an exact hit and also matches one insertion, deletion, or substitution against a word when the search string is at least 3 characters. The reply stays the existing SEARCH or ESEARCH line, with no RELEVANCY. SORT and THREAD still reject FUZZY. personalities/fuzzy.json advertises SEARCH=FUZZY. cyrusprobe -fuzzy is a further script and does not replace the earlier scripts.
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - SEARCH=FUZZY landed

- SEARCH and UID SEARCH accept FUZZY before one key when SEARCH=FUZZY is advertised, including after CHARSET and with RETURN. A text leaf keeps a case-insensitive substring and also matches one insertion, deletion, or substitution against a word when the search string is at least 3 characters. Flag, date, size, and UID leaves stay exact. OR, NOT, and a parenthesized list pass the mark. The reply is the existing SEARCH or ESEARCH line and has no RELEVANCY. Without the capability, FUZZY is BAD. SORT and THREAD still reject FUZZY.
- personalities/fuzzy.json advertises SEARCH=FUZZY. cyrusprobe -fuzzy does not replace the earlier scripts. liveimap-cyrus-3137 was already running. Its kind diff was not empty: FUZZY TEXT apple on an appended message returned no ESEARCH line and zero hits, while TEXT apple was COUNT 1. personalities/cyrus-3137.json, personalities/sasl.json, and the gaps file were left unchanged. The container was not recreated and was not stopped.

## 2026-11-11 - MULTISEARCH

- Approved plan: sandbox/plans/multisearch-20261111-1000-plan.md
- Work: ESEARCH IN for subtree, personal, and subscribed when MULTISEARCH is advertised. Each hit mailbox is one ESEARCH line of UIDs. UID SEARCH stays the selected-mailbox path. personalities/multisearch.json advertises MULTISEARCH. cyrusprobe -multisearch is a further script and does not replace the earlier scripts.
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - MULTISEARCH landed

- ESEARCH IN answers subtree, personal, and subscribed when MULTISEARCH is advertised. subtree is the named mailbox and descendants separated by the delimiter, so INBOX does not include INBOX2. personal is every mailbox. subscribed returns no lines. Each hit mailbox is one ESEARCH line of UIDs with TAG, MAILBOX, and UIDVALIDITY. A mailbox with no hits is omitted. The command does not need a selected mailbox. Without MULTISEARCH the command is BAD. FUZZY follows SEARCH=FUZZY. UID SEARCH is unchanged.
- personalities/multisearch.json advertises MULTISEARCH. cyrusprobe -multisearch does not replace the earlier scripts. liveimap-cyrus-3137 was already running. Its kind diff was empty, so MULTISEARCH is on the pre-login and post-login lists of personalities/cyrus-3137.json and personalities/sasl.json, not on the greeting, and gone from the gaps file. The container was not recreated and was not stopped.

## 2026-11-11 - Subscription set

- Approved plan: sandbox/plans/subscribe-20261111-1021-plan.md
- Work: each account keeps a subscription set. INBOX starts subscribed. SUBSCRIBE adds an existing mailbox and is NO when it is missing. UNSUBSCRIBE removes one and is OK when it was not subscribed. DELETE drops it. RENAME keeps it on the new name. reset and set-dataset leave only INBOX. LIST (SUBSCRIBED) and LSUB return that set. ESEARCH IN (subscribed) searches it.
- Compliance: standard-plan-compliance-block.md

## 2026-11-11 - PREVIEW execution start

- Approved plan: sandbox/plans/preview-20261111-1037-plan.md
- Work: answer FETCH and UID FETCH of PREVIEW with a short plain-text snippet when the personality advertises it. PREVIEW does not set \Seen. PREVIEW (LAZY) is BAD. Do not parse HTML.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - PREVIEW landed

- Approved plan: sandbox/plans/preview-20261111-1037-plan.md
- FETCH and UID FETCH of PREVIEW return a trimmed plain-text snippet of at most 256 characters when the personality advertises PREVIEW. personalities/preview.json lists it on the pre-login and post-login lists. PREVIEW does not set \Seen. PREVIEW (LAZY) is BAD. HTML is not parsed. cyrusprobe -preview does not replace the earlier scripts.
- liveimap-cyrus-3137 was already running and was the only liveimap container. Its greeting, pre-login, and post-login CAPABILITY lists do not include PREVIEW, so personalities/cyrus-3137.json, personalities/sasl.json, and the gaps file were left unchanged. The container was not recreated and was not stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - Shared IMAP and JMAP preview

- FETCH PREVIEW and JMAP Email/get preview are one text/plain snippet: charset-decoded like JMAP body text, collapsed, at most 256 characters. A charset problem is empty (IMAP NIL, JMAP "").

## 2026-11-11 - JMAP event source

- GET /jmap/eventsource/ stays open, writes Email state events, and pings while it waits.

## 2026-11-11 - Other Cyrus personalities

- Approved plan: sandbox/plans/cyrus-personalities-20261111-1318-plan.md
- Work: personalities for cyrus-22, cyrus-24, cyrus-25, cyrus-36, and cyrus-3124 from implemented capability tokens. Greeting with an empty capability list has no capability code. Personal namespace prefix omitted stays empty.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - Dovecot, Stalwart, and Zimbra personalities

- Approved plan: sandbox/plans/other-servers-20261111-1335-plan.md
- Work: personalities for the lab Dovecot, Stalwart, and Zimbra servers from implemented capability tokens. Courier and UW-IMAP stay out.
- First action per standard-plan-compliance-block.md

## 2026-11-11 - Courier and UW-IMAP personalities

- Approved plan: sandbox/plans/courier-uw-20261111-1345-plan.md
- Work: personalities for Courier 5.0, Courier 6.0.6, and UW-IMAP. NAMESPACE can list more than one prefix in a group, and an entry delimiter can be NIL. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Dovecot 2.4.5 execution start

- Approved plan: sandbox/plans/probe-dovecot-245-20261111-1505-plan.md
- Work: run the base cyrusprobe script against liveimap-dovecot-245 and update personalities/dovecot-245.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Dovecot 2.4.5 landed

- Approved plan: sandbox/plans/probe-dovecot-245-20261111-1505-plan.md
- personalities/dovecot-245.json now lists AUTH=PLAIN and AUTH=EXTERNAL after LITERAL+ on the greeting and pre-login lists, uses flag order Answered Flagged Deleted Seen Draft Recent, and sends MOVE COPYUID untagged. IMAP4rev2, ID, and LOGIN-REFERRALS stay off. Post-login tokens and order, NAMESPACE, and SELECT flags and order already matched.
- The normalized diff still has shapes the file cannot express: LOGIN sends an untagged CAPABILITY and a tagged OK with no capability code; BODYSTRUCTURE tokens are lowercase; MOVE still sends RECENT after EXPUNGE; LIST of the two probe mailboxes is MB2 then MB1. Reply code was not changed. liveimap-dovecot-245 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Dovecot dev execution start

- Approved plan: sandbox/plans/probe-dovecot-dev-20261111-1521-plan.md
- Work: run the base cyrusprobe script against liveimap-dovecot-dev and update personalities/dovecot-dev.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Dovecot dev landed

- Approved plan: sandbox/plans/probe-dovecot-dev-20261111-1521-plan.md
- personalities/dovecot-dev.json lists AUTH=PLAIN after LITERAL+ on the greeting and pre-login lists, uses flag order Answered Flagged Deleted Seen Draft Recent, and sends MOVE COPYUID untagged. ID and LOGIN-REFERRALS stay off. Post-login tokens and order, NAMESPACE, and SELECT flags and order already matched.
- The normalized diff still has shapes the file cannot express: LOGIN sends an untagged CAPABILITY and a tagged OK with no capability code; APPEND sends an unsolicited FLAGS line that adds attachment keywords; FETCH and the later SELECT include those keywords; BODYSTRUCTURE tokens are lowercase; MOVE still sends RECENT after EXPUNGE; LIST of the two probe mailboxes is MB2 then MB1. Reply code was not changed.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 and then started. liveimap-dovecot-dev was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Dovecot 1.0 execution start

- Approved plan: sandbox/plans/probe-dovecot-10-20261111-1533-plan.md
- Work: run the base cyrusprobe script against liveimap-dovecot-10 and update personalities/dovecot-10.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Dovecot 1.0 landed

- Approved plan: sandbox/plans/probe-dovecot-10-20261111-1533-plan.md
- personalities/dovecot-10.json lists AUTH=PLAIN then AUTH=LOGIN at the end of the pre-login list, and uses flag order Answered Flagged Deleted Seen Draft Recent. Greeting, post-login tokens and order, NAMESPACE, and SELECT flags and order already matched. MULTIAPPEND and LOGIN-REFERRALS stay off.
- The normalized diff still has shapes the file cannot express: LOGIN is a tagged OK with no capability code; APPEND and COPY are tagged OK with no UIDPLUS code; BODYSTRUCTURE tokens are lowercase and omit the final NIL; MOVE is tagged BAD, so the remote script stops there. Reply code was not changed.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 and then started. liveimap-dovecot-10 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Dovecot 1.2 execution start

- Approved plan: sandbox/plans/probe-dovecot-12-20261111-1544-plan.md
- Work: run the base cyrusprobe script against liveimap-dovecot-12 and update personalities/dovecot-12.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Dovecot 1.2 landed

- Approved plan: sandbox/plans/probe-dovecot-12-20261111-1544-plan.md
- personalities/dovecot-12.json lists AUTH=PLAIN then AUTH=LOGIN after ENABLE on the greeting and at the end of the pre-login list, and uses flag order Answered Flagged Deleted Seen Draft Recent. Post-login tokens and order, NAMESPACE, and SELECT flags and order already matched. LOGIN-REFERRALS, ID, MULTIAPPEND, and I18NLEVEL=1 stay off. STARTTLS stays out of the file.
- The normalized diff still has shapes the file cannot express: BODYSTRUCTURE tokens are lowercase. MOVE is tagged BAD because that capability is unlisted, so the remote script stops there. Reply code was not changed.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 and then started. liveimap-dovecot-12 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Dovecot 2.2 execution start

- Approved plan: sandbox/plans/probe-dovecot-22-20261111-1553-plan.md
- Work: run the base cyrusprobe script against liveimap-dovecot-22 and update personalities/dovecot-22.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Dovecot 2.2 landed

- Approved plan: sandbox/plans/probe-dovecot-22-20261111-1553-plan.md
- personalities/dovecot-22.json lists AUTH=PLAIN after IDLE on the greeting and pre-login lists, uses flag order Answered Flagged Deleted Seen Draft Recent, and sends MOVE COPYUID untagged. Post-login tokens and order, NAMESPACE, and SELECT flags and order already matched. ID and LOGIN-REFERRALS stay off. STARTTLS stays out of the file.
- The normalized diff still has shapes the file cannot express: LOGIN sends an untagged CAPABILITY and a tagged OK with no capability code; BODYSTRUCTURE tokens are lowercase; MOVE still sends RECENT after EXPUNGE. Reply code was not changed.
- Remaining diff:
  remote: * CAPABILITY BINARY CHILDREN CONDSTORE ENABLE ESEARCH ESORT IDLE IMAP4rev1 LIST-EXTENDED LIST-STATUS LITERAL+ MOVE NAMESPACE QRESYNC SASL-IR SORT SORT=DISPLAY SPECIAL-USE THREAD=ORDEREDSUBJECT THREAD=REFERENCES THREAD=REFS UIDPLUS UNSELECT
  local:  T OK [CAPABILITY BINARY CHILDREN CONDSTORE ENABLE ESEARCH ESORT IDLE IMAP4rev1 LIST-EXTENDED LIST-STATUS LITERAL+ MOVE NAMESPACE QRESYNC SASL-IR SORT SORT=DISPLAY SPECIAL-USE THREAD=ORDEREDSUBJECT THREAD=REFERENCES THREAD=REFS UIDPLUS UNSELECT]
  remote: T OK
  remote: BODYSTRUCTURE ("text" "plain" ("charset" "us-ascii") NIL NIL "7bit" ...)
  local:  BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "us-ascii") NIL NIL "7BIT" ...)
  remote: * RECENT
  local:  (no RECENT after MOVE EXPUNGE)
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 and then started. liveimap-dovecot-22 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Stalwart execution start

- Approved plan: sandbox/plans/probe-stalwart-20261111-1604-plan.md
- Work: run the base cyrusprobe script against liveimap-stalwart and update personalities/stalwart.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Stalwart landed

- Approved plan: sandbox/plans/probe-stalwart-20261111-1604-plan.md
- personalities/stalwart.json lists AUTH=PLAIN after UTF8=ACCEPT on the greeting and pre-login lists, lists STARTTLS at the end of the post-login list, uses flag order Deleted Seen Answered Flagged Draft, and sends MOVE COPYUID untagged. Greeting text, NAMESPACE, and SELECT flags and order already matched. IMAP4rev2, ID, and JMAPACCESS stay off.
- The normalized diff still has shapes the file cannot express: APPEND sends no untagged EXISTS or RECENT; the ENVELOPE date is Mon, 1 Jan rather than Thu, 01 Jan; BODYSTRUCTURE tokens are lowercase and include a body MD5; FETCH of BODY[TEXT] puts FLAGS after the body; MOVE still sends EXISTS after EXPUNGE; LIST uses empty flags and a quoted name. Reply code was not changed.
- Remaining diff:
  remote: T OK [APPENDUID]
  local:  * EXISTS
  local:  * RECENT
  local:  T OK [APPENDUID]
  remote: ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" ...)
  local:  ENVELOPE ("Thu, 01 Jan 2024 00:00:00 +0000" ...)
  remote: BODYSTRUCTURE ("text" "plain" ("charset" "us-ascii") NIL NIL "7bit" 12 1 "6db539cda4f12afaccecaede3c3cc946" NIL NIL NIL)
  local:  BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "us-ascii") NIL NIL "7BIT" 12 1 NIL NIL NIL NIL)
  remote: * SEQ FETCH (BODY[TEXT] {12} ... FLAGS (\Seen))
  local:  * SEQ FETCH (FLAGS (\Seen) BODY[TEXT] {12} ...)
  remote: * EXISTS
  local:  (no EXISTS after MOVE EXPUNGE)
  remote: * LIST () "/" "MB1"
  local:  * LIST (\HasNoChildren) "/" MB1
  remote: * LIST () "/" "MB2"
  local:  * LIST (\HasNoChildren) "/" MB2
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-stalwart was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Stalwart FETCH order fix

- Review on 8cb3ff9d: BODY[TEXT] uses the BODY[] rank, so personalities/stalwart.json lists BODY[] before FLAGS. A FETCH that sets Seen now puts FLAGS after the body. The FLAGS UID header fetch has no BODY[ item, so its order is unchanged.
- The other leftovers stay: APPEND still has local EXISTS and RECENT, the ENVELOPE date text differs, BODYSTRUCTURE case and body MD5 differ, MOVE still has remote EXISTS after EXPUNGE, and LIST still has empty flags and a quoted name. Reply code was not changed.
- liveimap-stalwart was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe stalwart 0.16.25

- Approved plan: sandbox/plans/probe-stalwart-01625-20261111-1627-plan.md
- Work: run base cyrusprobe against liveimap-stalwart-01625 and update personalities/stalwart-01625.json until the normalized diff is empty, or record a shape the file cannot express
- No new capability. Stop the container before commit.

## 2026-11-11 - probe Stalwart 0.16.25 landed

- Approved plan: sandbox/plans/probe-stalwart-01625-20261111-1627-plan.md
- personalities/stalwart-01625.json lists AUTH=PLAIN after UTF8=ACCEPT on the greeting and pre-login lists, lists STARTTLS at the end of the post-login list, uses flag order Deleted Seen Answered Flagged Draft, lists BODY[] before FLAGS, and sends MOVE COPYUID untagged. Greeting text, NAMESPACE, and SELECT flags and order already matched. IMAP4rev2, ID, and JMAPACCESS stay off.
- The normalized diff still has shapes the file cannot express: APPEND sends no untagged EXISTS or RECENT; the ENVELOPE date is Mon, 1 Jan rather than Thu, 01 Jan; BODYSTRUCTURE tokens are lowercase and include a body MD5; MOVE still sends EXISTS after EXPUNGE; LIST uses empty flags and a quoted name. Reply code was not changed.
- Remaining diff:
  remote: T OK [APPENDUID]
  local:  * EXISTS
  local:  * RECENT
  local:  T OK [APPENDUID]
  remote: ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" ...)
  local:  ENVELOPE ("Thu, 01 Jan 2024 00:00:00 +0000" ...)
  remote: BODYSTRUCTURE ("text" "plain" ("charset" "us-ascii") NIL NIL "7bit" 12 1 "6db539cda4f12afaccecaede3c3cc946" NIL NIL NIL)
  local:  BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "us-ascii") NIL NIL "7BIT" 12 1 NIL NIL NIL NIL)
  remote: * EXISTS
  local:  (no EXISTS after MOVE EXPUNGE)
  remote: * LIST () "/" "MB1"
  local:  * LIST (\\HasNoChildren) "/" MB1
  remote: * LIST () "/" "MB2"
  local:  * LIST (\\HasNoChildren) "/" MB2
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-stalwart-01625 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Stalwart dev execution start

- Approved plan: sandbox/plans/probe-stalwart-dev-20261111-1641-plan.md
- Work: run the base cyrusprobe script against liveimap-stalwart-dev and update personalities/stalwart-dev.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Stalwart dev landed

- Approved plan: sandbox/plans/probe-stalwart-dev-20261111-1641-plan.md
- personalities/stalwart-dev.json lists AUTH=PLAIN after UTF8=ACCEPT on the greeting and pre-login lists, lists STARTTLS at the end of the post-login list, uses flag order Deleted Seen Answered Flagged Draft, lists BODY[] before FLAGS, and sends MOVE COPYUID untagged. Greeting text, NAMESPACE, and SELECT flags and order already matched. IMAP4rev2, ID, and JMAPACCESS stay off.
- The normalized diff still has shapes the file cannot express: APPEND sends no untagged EXISTS or RECENT; the ENVELOPE date is Mon, 1 Jan rather than Thu, 01 Jan; BODYSTRUCTURE tokens are lowercase and include a body MD5; MOVE still sends EXISTS after EXPUNGE; LIST uses empty flags and a quoted name. Reply code was not changed.
- Remaining diff:
  remote: T OK [APPENDUID]
  local:  * EXISTS
  local:  * RECENT
  local:  T OK [APPENDUID]
  remote: ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" ...)
  local:  ENVELOPE ("Thu, 01 Jan 2024 00:00:00 +0000" ...)
  remote: BODYSTRUCTURE ("text" "plain" ("charset" "us-ascii") NIL NIL "7bit" 12 1 "6db539cda4f12afaccecaede3c3cc946" NIL NIL NIL)
  local:  BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "us-ascii") NIL NIL "7BIT" 12 1 NIL NIL NIL NIL)
  remote: * EXISTS
  local:  (no EXISTS after MOVE EXPUNGE)
  remote: * LIST () "/" "MB1"
  local:  * LIST (\HasNoChildren) "/" MB1
  remote: * LIST () "/" "MB2"
  local:  * LIST (\HasNoChildren) "/" MB2
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-stalwart-dev was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Stalwart 0.15.5 execution start

- Approved plan: sandbox/plans/probe-stalwart-0155-20261111-1652-plan.md
- Work: run the base cyrusprobe script against liveimap-stalwart-0155 and update personalities/stalwart-0155.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Stalwart 0.15.5 landed

- Approved plan: sandbox/plans/probe-stalwart-0155-20261111-1652-plan.md
- personalities/stalwart-0155.json lists AUTH=PLAIN after UTF8=ACCEPT on the greeting and pre-login lists, lists STARTTLS at the end of the post-login list, uses flag order Deleted Seen Answered Flagged Draft, lists BODY[] before FLAGS, and sends MOVE COPYUID untagged. Greeting text, NAMESPACE, and SELECT flags and order already matched. IMAP4rev2, ID, and JMAPACCESS stay off.
- The normalized diff still has shapes the file cannot express: APPEND sends no untagged EXISTS or RECENT; the ENVELOPE date is Mon, 1 Jan rather than Thu, 01 Jan; BODYSTRUCTURE tokens are lowercase and include a body MD5; MOVE still sends EXISTS after EXPUNGE; LIST uses empty flags and a quoted name. Reply code was not changed.
- Remaining diff:
  remote: T OK [APPENDUID]
  local:  * EXISTS
  local:  * RECENT
  local:  T OK [APPENDUID]
  remote: ENVELOPE ("Mon, 1 Jan 2024 00:00:00 +0000" ...)
  local:  ENVELOPE ("Thu, 01 Jan 2024 00:00:00 +0000" ...)
  remote: BODYSTRUCTURE ("text" "plain" ("charset" "us-ascii") NIL NIL "7bit" 12 1 "6db539cda4f12afaccecaede3c3cc946" NIL NIL NIL)
  local:  BODYSTRUCTURE ("TEXT" "PLAIN" ("CHARSET" "us-ascii") NIL NIL "7BIT" 12 1 NIL NIL NIL NIL)
  remote: * EXISTS
  local:  (no EXISTS after MOVE EXPUNGE)
  remote: * LIST () "/" "MB1"
  local:  * LIST (\HasNoChildren) "/" MB1
  remote: * LIST () "/" "MB2"
  local:  * LIST (\HasNoChildren) "/" MB2
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-stalwart-0155 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Cyrus 3.12.4 execution start

- Approved plan: sandbox/plans/probe-cyrus-3124-20261111-1705-plan.md
- Work: run the base cyrusprobe script against liveimap-cyrus-3124 and update personalities/cyrus-3124.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Cyrus 3.12.4 landed

- Approved plan: sandbox/plans/probe-cyrus-3124-20261111-1705-plan.md
- personalities/cyrus-3124.json greets localhost Cyrus IMAP 3.12.4 server ready. AUTH=PLAIN and AUTH=LOGIN are on the greeting and pre-login lists. THREAD=REFS follows UNSELECT. MOVE sends COPYUID untagged. NAMESPACE, SELECT flags, and SELECT order already matched. FLAGS stay before BODY[TEXT]. IMAP4rev2, ID, and STARTTLS stay off the file.
- liveimap@localhost.local is refused by this server. The seed user authenticated, and the base script normalized diff was empty.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-cyrus-3124 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Cyrus 3.13.7 execution start

- Approved plan: sandbox/plans/probe-cyrus-3137-20261111-1716-plan.md
- Work: run the base cyrusprobe script against liveimap-cyrus-3137 and update personalities/cyrus-3137.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Cyrus 3.13.7 landed

- Approved plan: sandbox/plans/probe-cyrus-3137-20261111-1716-plan.md
- personalities/cyrus-3137.json keeps the greeting localhost Cyrus IMAP unknown server ready. SEARCH=FUZZY is on the pre-login and post-login lists, and THREAD=REFS follows UNSELECT. Greeting, NAMESPACE, and SELECT order already matched. IMAP4rev2, ID, and STARTTLS stay off the file.
- The seed user authenticated. The base script normalized diff was empty. liveimap-cyrus-3137 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Cyrus 3.13.7 search tests

- SEARCH FUZZY TEXT is no longer tagged BAD on the default personality, because personalities/cyrus-3137.json now lists SEARCH=FUZZY. A missing key is still BAD. The unadvertised case stays on personalities/condstore.json.

## 2026-11-11 - probe Cyrus 2.2 execution start

- Approved plan: sandbox/plans/probe-cyrus-22-20261111-1727-plan.md
- Work: run the base cyrusprobe script against liveimap-cyrus-22 and update personalities/cyrus-22.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Cyrus 2.2 landed

- Approved plan: sandbox/plans/probe-cyrus-22-20261111-1727-plan.md
- personalities/cyrus-22.json greets localhost Cyrus IMAP4 v2.2.13-Debian-2.2.13-14+lenny6 server ready. Capability lists, NAMESPACE, and SELECT flags and order already matched. IMAP4, ACL, QUOTA, ID, and STARTTLS stay off the file.
- The seed user authenticated. The base script did not stop on MOVE. It stopped on CREATE of a top-level mailbox, which the server answers NO Permission denied. LOGIN's tagged OK has no CAPABILITY code. Neither shape is expressible in the personality file. Reply code was not changed.
- Remaining diff:
  remote: T OK
  local:  T OK [CAPABILITY BINARY CHILDREN IDLE IMAP4rev1 LITERAL+ NAMESPACE SORT THREAD=ORDEREDSUBJECT THREAD=REFERENCES UIDPLUS UNSELECT]
  remote: T NO
  local:  T OK
  The rest of the local script has no remote lines because the script stopped there.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-cyrus-22 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Cyrus 2.4 execution start

- Approved plan: sandbox/plans/probe-cyrus-24-20261111-1737-plan.md
- Work: run the base cyrusprobe script against liveimap-cyrus-24 and update personalities/cyrus-24.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Cyrus 2.4 landed

- Approved plan: sandbox/plans/probe-cyrus-24-20261111-1737-plan.md
- personalities/cyrus-24.json greets localhost Cyrus IMAP v2.4.16-Debian-2.4.16-4+deb7u2 server ready. AUTH=PLAIN and AUTH=LOGIN are on the greeting and the pre-login list, in the live order. The post-login list follows that server's CAPABILITY line. ID, ACL, QUOTA, and STARTTLS stay off the file. NAMESPACE and SELECT flags and order already matched. FLAGS stay before BODY[TEXT].
- The seed user authenticated. The base script did not stop on MOVE. It stopped on CREATE of a top-level mailbox, which the server answers NO Permission denied. Reply code was not changed.
- Remaining diff:
  remote: T NO
  local:  T OK
  The rest of the local script has no remote lines because the script stopped there.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-cyrus-24 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Cyrus 2.5 execution start

- Approved plan: sandbox/plans/probe-cyrus-25-20261111-1749-plan.md
- Work: run the base cyrusprobe script against liveimap-cyrus-25 and update personalities/cyrus-25.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Cyrus 2.5 landed

- Approved plan: sandbox/plans/probe-cyrus-25-20261111-1749-plan.md
- personalities/cyrus-25.json greets localhost Cyrus IMAP 2.5.10-Debian-2.5.10-3+deb9u3 server ready. AUTH=PLAIN and AUTH=LOGIN are on the greeting and the pre-login list, in the live order. The post-login list follows that server's CAPABILITY line. ID, ACL, QUOTA, and STARTTLS stay off the file. NAMESPACE and SELECT flags and order already matched. FLAGS stay before BODY[TEXT].
- The seed user authenticated. The base script did not stop on MOVE. It stopped on CREATE of a top-level mailbox, which the server answers NO Permission denied. Reply code was not changed.
- Remaining diff:
  remote: T NO
  local:  T OK
  The rest of the local script has no remote lines because the script stopped there.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-cyrus-25 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Cyrus 3.6 execution start

- Approved plan: sandbox/plans/probe-cyrus-36-20261111-1801-plan.md
- Work: run the base cyrusprobe script against liveimap-cyrus-36 and update personalities/cyrus-36.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Cyrus 3.6 landed

- Approved plan: sandbox/plans/probe-cyrus-36-20261111-1801-plan.md
- personalities/cyrus-36.json greets localhost Cyrus IMAP 3.6.1-Debian-3.6.1-4+deb12u5 server ready. AUTH=PLAIN and AUTH=LOGIN are on the greeting and the pre-login list, in the live order. The post-login list follows that server's CAPABILITY line. ID, ACL, QUOTA, and STARTTLS stay off the file. NAMESPACE already matched. SELECT was not reached. FLAGS stay before BODY[TEXT] in the file.
- The seed user authenticated. The base script did not stop on MOVE. It stopped on CREATE of a top-level mailbox, which the server answers NO Permission denied. Reply code was not changed.
- Remaining diff:
  remote: T NO
  local:  T OK
  The rest of the local script has no remote lines because the script stopped there.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-cyrus-36 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Courier 6.0.6 execution start

- Approved plan: sandbox/plans/probe-courier-606-20261111-1810-plan.md
- Work: run the base cyrusprobe script against liveimap-courier-606 and update personalities/courier-606.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Courier 6.0.6 landed

- Approved plan: sandbox/plans/probe-courier-606-20261111-1810-plan.md
- personalities/courier-606.json already matched the live greeting, the implemented capability order, and NAMESPACE. QUOTA, ACL, and ACL2=UNION stay off the file. STARTTLS stays off the file. SELECT was not reached, so flag strings and select order were left as recorded. No personality edit, so no commit and no make.
- The seed user authenticated. The base script stopped on CREATE of a mailbox outside the personal prefix INBOX. The server answers NO Invalid mailbox name. Reply code was not changed. The LOGIN tagged OK has no CAPABILITY code; the personality file cannot say that.
- Remaining diff:
  remote: T OK
  local:  T OK [CAPABILITY CHILDREN ENABLE IDLE IMAP4rev1 NAMESPACE SORT THREAD=ORDEREDSUBJECT THREAD=REFERENCES UIDPLUS UTF8=ACCEPT]
  remote: T NO
  local:  T OK
  The rest of the local script has no remote lines because the script stopped on CREATE.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-courier-606 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe UW-IMAP landed

- Approved plan: sandbox/plans/probe-uwimap-20261111-1824-plan.md
- personalities/uwimap.json now records the live post-login list, including STARTTLS at the end, flag order \Recent \Seen \Deleted \Flagged \Answered \Draft, fetch_seen_flags false, and BODY[] before FLAGS. NAMESPACE and SELECT order already matched. QUOTA, ACL, ACL2=UNION, I18NLEVEL=1, and LOGIN-REFERRALS stay off the file.
- CREATE of a top-level mailbox succeeded. The base script then stops because MOVE is unrecognized. Reply code was not changed.
- The live greeting appends " at " and a timestamp after the host token. The LOGIN tagged OK capability list omits SASL-IR and STARTTLS while the later CAPABILITY command still lists both. The personality file has one post-login list and cannot say that.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-uwimap was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - probe Zimbra 8.8.3 execution start

- Approved plan: sandbox/plans/probe-zimbra-883-20261111-1842-plan.md
- Work: run the base cyrusprobe script against liveimap-zimbra-883 and update personalities/zimbra-883.json until the normalized diff is empty, or record a shape the file cannot express. No new capability.
- First action per standard-plan-compliance-block.md
- The lab password stays out of this log.

## 2026-11-11 - probe Zimbra 8.8.3 landed

- Approved plan: sandbox/plans/probe-zimbra-883-20261111-1842-plan.md
- personalities/zimbra-883.json keeps the live greeting, empty greeting list, delimiter /, and other-user prefix /home/. The pre-login list ends with AUTH=PLAIN then STARTTLS. The post-login list already matched the implemented tokens. SELECT flags and order already matched. flag_order is \Answered \Flagged \Deleted \Seen \Draft \Recent. BODY[] ranks before FLAGS. ACL, CATENATE, I18NLEVEL=1, ID, LOGIN-REFERRALS, MULTIAPPEND, QUOTA, RIGHTS=ektx, SEARCHRES, WITHIN, and XLIST stay off the file.
- CREATE of a top-level mailbox succeeded. The base script then stops because MOVE is BAD. Reply code was not changed.
- Remaining diff:
  remote: T BAD
  local:  * EXPUNGE
  The rest of the local script has no remote lines because the script stopped on MOVE.
- start.sh could not rewrite lab-ports.tsv.new, so the missing container was created with -p 20000:143 -p 20002:993 -p 20003:4190 -p 20004:443 -p 20001:587 and then started. liveimap-zimbra-883 was stopped. The lab password stayed in the gitignored operator file.

## 2026-11-11 - SMTP STARTTLS execution start

- Approved plan: sandbox/plans/smtp-starttls-20261111-1902-plan.md
- Work: offer STARTTLS on SMTP when the IMAP certificate is loaded, upgrade that connection, and reject it when no certificate is configured
- First action per standard-plan-compliance-block.md

## 2026-11-11 - SMTP STARTTLS landed

- Approved plan: sandbox/plans/smtp-starttls-20261111-1902-plan.md
- EHLO lists STARTTLS only when the IMAP certificate is loaded. STARTTLS returns 220 and that connection then speaks TLS. The next EHLO omits STARTTLS. A second STARTTLS, or STARTTLS with no certificate, is rejected. MAIL before AUTH stays 530.
- No certificate was added. No personality file changed.

## 2026-11-11 - JMAP implicit TLS execution start

- Approved plan: sandbox/plans/jmap-tls-20261111-1912-plan.md
- Work: jmap_tls_listen binds TLS from the first byte with the certificate IMAP already loads. Empty or omitted does not bind. Cleartext jmap_listen stays cleartext. No certificate is added to the repo.
- First action per standard-plan-compliance-block.md

## 2026-11-11 - JMAP implicit TLS landed

- Approved plan: sandbox/plans/jmap-tls-20261111-1912-plan.md
- jmap_tls_listen binds TLS from the first byte with the certificate IMAP already loads. Ready JSON and status report it as jmap_tls. Empty or omitted does not bind. Cleartext jmap_listen stays cleartext HTTP. Setting jmap_tls_listen without a certificate is a config error. No certificate was added.

## 2026-11-11 - user guide execution start

- Approved plan: sandbox/plans/user-guide-20261111-1924-plan.md
- Work: write docs/guide.md, replace the stub README, correct the named facts sentences, and drop SEARCH=FUZZY from personalities/cyrus-3137.gaps.json
- First action per standard-plan-compliance-block.md
- Server behavior stays as it is. No Go change. AGENTS.md stays untouched.

## 2026-11-11 - user guide landed

- Approved plan: sandbox/plans/user-guide-20261111-1924-plan.md
- docs/guide.md is the developer guide. README names fakeimap, says make builds it, and points at the guide.
- Facts sentences for STARTTLS, the three Stalwart files, sasl.json post-login, the personality keys, and SEARCH=FUZZY now match the tree. SEARCH=FUZZY left personalities/cyrus-3137.gaps.json. URLAUTH=BINARY stayed.
- No Go behavior change. No certificate added. make does not start Docker.

## 2026-11-11 - ID command

- Approved plan: sandbox/plans/id-command-20261111-2120-plan.md
- Work: advertise ID where the lab lists it and answer with one canned server identity
- First action per STANDARD BLOCK: ./orch/exec-preflight

## 2026-11-11 - UNAUTHENTICATE execution start

- Approved plan: sandbox/plans/unauthenticate-20261111-2131-plan.md
- Work: return an authenticated connection to the not-authenticated state without expunging, and list UNAUTHENTICATE on the four Stalwart post-login lists
- First action per standard-plan-compliance-block.md

## 2026-11-11 - UNAUTHENTICATE landed

- Approved plan: sandbox/plans/unauthenticate-20261111-2131-plan.md
- UNAUTHENTICATE returns the connection to the not-authenticated state without expunging. TLS and COMPRESS=DEFLATE stay. The enabled set is cleared. The four Stalwart post-login lists place it between QRESYNC and PREVIEW. The guide names the ID reply and points at the facts file.
