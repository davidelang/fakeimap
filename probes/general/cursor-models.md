Feature: model ids in roles.config cursor_model= (grok-4.7-500k, grok-4.7, composer-2.5) exist.
Rule it supports: per-role cursor models.
How to check: `sudo -u ai-coder -H ~/git/cursor/bin/cursor-agent --list-models` (logged in); cursor.com/docs/models.
Expected: every cursor_model value listed; update roles.config if renamed.
