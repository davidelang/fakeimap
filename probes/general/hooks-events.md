Feature: hooks UserPromptSubmit, PreToolUse (additionalContext, deny), Stop/SubagentStop (decision:block).
Rule it supports: .grok/hooks/rules-hooks.sh approval gate, facts injection, STATUS line.
How to check: grok docs/changelog for hook events and field names (sessionId, toolName, toolInput, lastAssistantMessage, stopHookActive).
Expected: all present with those names.
