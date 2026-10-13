Feature: .cursor/hooks.json events and fields used by .cursor/lib/cursor-hooks.sh: sessionStart additional_context; beforeSubmitPrompt prompt; preToolUse tool_name/tool_input -> permission; postToolUse additional_context; afterAgentResponse text; stop status/loop_count -> followup_message; conversation_id, workspace_roots.
Rule it supports: role card, approval gate, facts injection, STATUS line, channel backstop.
How to check: cursor.com/docs/hooks reference; pipe sample JSON through .cursor/lib/cursor-hooks.sh.
Expected: same names and output schema.
