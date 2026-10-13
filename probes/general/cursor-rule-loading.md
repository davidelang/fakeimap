Feature: cursor-agent loads AGENTS.md and CLAUDE.md at the project root (and nested AGENTS.md) plus .cursor/rules/*.mdc.
Rule it supports: core law in AGENTS.md for run-cursor*.
How to check: cursor.com/docs/cli/using "Rules"; live: ask a fresh run-cursor session to quote the "Law:" line once.
Expected: AGENTS.md applied once; no .cursor/rules needed.
