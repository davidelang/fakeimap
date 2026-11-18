Incident: Cursor's install script runs `rm -f ~/.local/bin/agent ~/.local/bin/cursor-agent` and relinks agent to cursor-agent, which would replace the grok link.
Rule it supports: cursor lives in ~/git/cursor; never run cursor.com/install; auto-update must not touch ~/.local/bin/agent.
How to check: `readlink ~/.local/bin/agent` points to ~/git/grok/bin/agent; `curl -fsS https://cursor.com/install | grep -n 'local/bin/agent'`.
Expected: link unchanged after every cursor-agent update.
