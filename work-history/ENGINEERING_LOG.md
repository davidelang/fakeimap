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
