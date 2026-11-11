# project-facts.md — fakeimap

- Host: multi-agent host cloned from orchestration-example
- Sandbox: `@@SANDBOX_PATH@@/` (absolute: @@ORCH_ROOT@@/@@SANDBOX_PATH@@/)
- Layout: `@@ORCH_ROOT@@/` branch `orchestration`; `@@ORCH_ROOT@@/master/` branch `master`
- git_home: @@GIT_HOME@@
- Launch: `./run-grok-*` via `.grok/lib/grok-launch-common.sh` + `agent-landlock`
- Groups: source `./orch/env` (refresh-shell setuid)
- Before adding or changing user-visible wording, translations, or the user manual: read `docs/facts/translations.md`.
- Before changing the IMAP server, its config, personality, dataset, or binary behavior: read `docs/facts/imap-server.md`.

## Build and commit
- **Build type:** makefile
- Build: `make`
- Commit: `git add` the files of that change, then `git commit` as David Lang <david@lang.hm>. Do not push.
