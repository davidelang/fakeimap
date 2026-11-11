Feature: .cursor/hooks.json events and fields used by .cursor/lib/cursor-hooks.sh: sessionStart additional_context; preToolUse tool_name/tool_input -> permission; postToolUse additional_context; afterAgentResponse text; stop status/loop_count -> followup_message; conversation_id, workspace_roots.
Rule it supports: role card, role write limits, facts injection, STATUS line, channel backstop.
How to check: cursor.com/docs/hooks reference; pipe sample JSON through .cursor/lib/cursor-hooks.sh. If the installed build is newer than 2026.10.01-e373342, confirm postToolUse additional_context for a subagent Task still uses that field (changelog v2026.10.17).
Expected: same names and output schema.
