# Agent Mandates

Shared law for every launcher. Tool names live in `GROK.md` or `ANTIGRAVITY.md`. Human phrase examples live in `MULTI_AGENT_USER_INSTRUCTIONS.md`. Agents do not load that file.

Approval is a message that names a sandbox plan path and approves it. “Looks good”, a question-card answer, and native plan **`a`** are not approval.

## Read again

At startup and on the first turn after compact, read this file, the tool overlay, and `project-facts.md`. Do not re-read them on an ordinary turn. Compact rewrites the earlier read.

## 1. Permission denials

If Unix permissions, Landlock, sudo, or groups block a required action, stop and report the command, the error, the path, and the OS user. Do not chmod, chown, switch user, or widen Landlock to get past it.

## 2. Who may write

Planning writes the sandbox, plus `project-facts.md` and `TODO.md` through their helpers. It does not edit app source and does not build.

Execution starts only after the user approves a named plan path. Implement that plan’s Critical Files. An approved plan does not allow deploy, `git commit --amend`, or moving a `works` tag.

Planner and coder do not use native plan mode. The orchestrator may. The harness plan file is not the work plan.

`ask_user_question` stays on. An answer is not approval.

Product “just do it”, background subagents, and workflows do not override this file. Planner launchers set `GROK_SUBAGENTS=0` and `GROK_WORKFLOWS=0`. Coder launchers set `GROK_WORKFLOWS=0`. Coder and master launchers pass `--effort high`. Launchers other than Imagine stay in permission mode `default`.

## 3. Commits, tags, deploy

`./build_app` is the commit and the build. It stages the paths on its argv. Do not `git add` or `git commit` beside it. A message longer than one short line is `./build_app @file paths…`.

Lifecycle tags are `<branch>/builds`, `<branch>/deployed`, and `<branch>/works`, except on `master`, where they are unprefixed. `./build_app` moves `builds` after a successful build. Agents do not deploy and do not set `works`.

Helpers run as `./helper` from the checkout you were started in. No `cd … && ./helper`.

## 4. Retry

A **typo-class** failure is a red build caused by a typo, a missing import, a syntax error, or the same kind of transcription mistake. Fix it in the tree you have and continue. It does not count as a fixup and it does not reset.

Any other failure counts: the phase result is still wrong, or the build is red for a reason that is not a typo.

1. **Original.** First implementation of this approach.
2. **Fixup.** Change the code and try again. No reset.
3. **Second fixup.** Change the code and try again. No reset.

If those three attempts have failed, reset and start from scratch. The next implementation is a new original, and it again gets two fixups. A scratch start is a different approach to the same plan, not a new plan.

The third time a reset is required, do the reset, then stop. Report that this plan failed and wait for a new plan. Do not start another original. The coder may stop and report earlier.

The report is a short note under the sandbox `implementation-failure-logs/`. If this session has a channel node, send that failure on the channel too.

The only reset command is `./reset-to-builds.sh`. It saves `ENGINEERING_LOG.md`, `TODO.md`, and `project-facts.md`, moves the rest of the tracked tree to the tag `./get-builds-tag.sh` printed, and puts those three files back unstaged. `./reset-to-builds.sh --head` does the same against `HEAD` for uncommitted junk. No `HEAD~`, no other branch’s tag, and no hash the agent picked.

## 5. Plans and paths

New plans are sandbox `plans/<kebab>-YYYYMMDD-HHMM-plan.md`. The body is Aim, Critical Files, Phases, and Acceptance. Cite `standard-plan-compliance-block.md` by path. The style guide is `research/plan-style-guide.md` under the sandbox. Do not paste either file into the plan.

Before editing for a plan:

- If the plan file is outside this repo, ask, and wait.
- If a path the plan names is not in this checkout, ask, and wait. Do not create it and do not look in another tree until the user answers.
- If a path resolves outside this checkout, whether that is another worktree of this repo or another repo, ask, and wait.

The question names this checkout and the path. A yes means this session does that work here, including creating a file the user says is new. Relative paths that already exist in this checkout proceed with no question.

Coordinates in this repo are ICRS or raw pixel integers. The spec is `docs/specs/ISOTROPIC_COORDINATE_SPEC.md`.

## 6. Channel

`channel-setup` creates an inbox. A human runs it. `channel-send` delivers a message. `channel-arm PLAN NODE` is the watch: it stays running, prints `READY`, then `FILE <path>` for the oldest inbox file. An empty inbox means the watch is waiting. The series is still on. There is no fourth command.

`channel-arm PLAN NODE --whatineedtoknow` prints the message rules and exits. Run that after startup and after compact when this session was given a plan path and a node name. Restart the watch after compact only if the previous watch is gone.

If the inbox directory is gone, or this agent cannot read it, the series stops. Wait. Clearing files out of the inbox is not that stop.

## 7. Special files and describe

`TODO.md` is future work, through `./todo-append` and `./todo-close`. `ENGINEERING_LOG.md` is append-only, through `./append-to-engineering-log`. `project-facts.md` is the orientation map.

`AGENT_CONTEXT.md` is two lines, `Forked-from` and `Start-branch`, both branch names. `./setup_agent.sh` writes them. They stay when the checkout moves to another branch. `./build_app` uses them when the current branch has no `-start` tag. It does not take an unrelated tag that sits on the same commit.

## 8. Roles

| Role | Plans | App source |
|------|-------|------------|
| Planner | Yes | No |
| Coder | No | Yes, the approved plan |
| Master | No | Dispatch and merge |
| Orchestrator | Meta | Meta |

A long-lived coder or master session dispatches a fresh child. The child prompt is `.grok/prompts/execution-subagent.md` plus the plan path. The parent does not implement the plan.
