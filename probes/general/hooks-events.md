Feature: hooks PreToolUse (additionalContext, deny), Stop/SubagentStop (decision:block).
Rule it supports: .grok/hooks/rules-hooks.sh role write limits, facts injection, STATUS line.
How to check: grok docs/changelog for hook events and field names (sessionId, toolName, toolInput, lastAssistantMessage, stopHookActive).
Expected: all present with those names.
