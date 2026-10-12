Feature: AGENTS.md/CLAUDE.md auto-loaded once into the first user message and kept through compaction.
Rule it supports: small cached core law (AGENTS.md).
How to check: newest ~/.grok/sessions/*/chat_history.jsonl contains "Law:" text exactly once, also after /compact.
Expected: one copy.
