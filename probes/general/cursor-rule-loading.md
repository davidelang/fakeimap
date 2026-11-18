Feature: cursor-agent loads AGENTS.md and CLAUDE.md at the project root (and nested AGENTS.md) plus .cursor/rules/*.mdc.
Rule it supports: core law in AGENTS.md for run-cursor*.
How to check: cursor.com/docs/cli/using "Rules"; live: ask a fresh run-cursor session to quote the "Law:" line once. If the installed build is newer than 2026.10.01-e373342, confirm a nested AGENTS.md is still applied once per conversation (changelog v2026.10.22). Do not run https://cursor.com/install.
Expected: AGENTS.md applied once; no .cursor/rules needed.
