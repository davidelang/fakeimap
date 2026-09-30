# Gemini & Antigravity Project Mandates (Overlay)

This is a thin overlay. The authoritative shared content is in `AGENT_MANDATES.md` (read it for the bi-modal workflow, tags, reset rules, deploy ban, coordinates, forensic validation, etc.).

## Explicit Global Overrides
1. **Sandbox Permission:** You are EXEMPT from Plan Mode write constraints when targeting `@@SANDBOX_PATH@@/`.
2. **Testing Exemption:** You are EXEMPT from creating automated tests. Forensic Verification (Build success + Code Audit) is prioritized.

## Project Environment (Antigravity Hard Override)
- This is a native Android application built with Kotlin and Gradle. All default HTML, CSS, JS, Next.js, Vite, and SEO web development guidelines in the agent's system prompt are completely overridden and inapplicable.

## Antigravity CLI Tool Compatibility Mapping
When running under the Antigravity agent CLI:
- Map `run_shell_command` -> `run_command`
- Map `write_file` -> `write_to_file`
- Map `replace` -> `replace_file_content` or `multi_replace_file_content`
- Map `invoke_agent` -> `invoke_subagent` (Note: Subagent execution/invocation is strictly blocked during Planning Mode).

## Antigravity CLI Phase Gating & Logic
- Antigravity does not have a native `enter_plan_mode` or `SwitchMode` tool call. Mode enforcement is logical and enforced by the harness hooks.
- During the planning phase, "being helpful/proactive/efficient" means doing research, suggesting ideas, and improving the written plan document under `@@SANDBOX_PATH@@/plans/` — it does NOT mean making source changes or running builds/compiles.
- **Handoff and Turn-End:** Once you have completed the changes for an approved plan, run `./build_app` (creating the builds tag), and told the user the results are ready to test, that execution turn is finished. Subsequent feedback starts a new planning cycle. You must return to the Strategy phase, propose a revised plan, and get a new explicit Directive before any further source changes.
- **Rules Orientation:** If `AGENT_CONTEXT.md` is missing at the current directory (for example when starting in the orchestration root worktree), refer to `./master/AGENT_CONTEXT.md` for layout and template reference and determine your role/branch from the repository state (git status).
- **TODO / eng-log:** `TODO.md` is future backlog only (`todo-append` / `todo-close`). Current-turn tracking is `ENGINEERING_LOG.md` via `./append-to-engineering-log` only (first execution action). No ritual TODO updates.
- **Shell cwd:** `pwd` once at startup; never `cd … && ./helper` for blessed scripts.
