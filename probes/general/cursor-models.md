Feature: every cursor_model= / cursor_retry_model= id in roles.config exists in cursor-agent (effort is encoded in the id, e.g. grok-4.7-medium; no 500k variant).
Rule it supports: per-role cursor models.
How to check: `ids=$(~/git/cursor/bin/cursor-agent --list-models | sed -n 's/^\([^ ]*\) - .*/\1/p'); grep -o 'cursor_\(retry_\)\?model=[^ ]*' roles.config | cut -d= -f2 | sort -u | while read m; do echo "$ids" | grep -qxF "$m" || echo "MISSING $m"; done` (run logged in, e.g. as the ai-* user); cursor.com/docs/models.
Expected: no MISSING lines; update roles.config if an id was renamed.
