Feature: reasoning effort selectable per launch.
Rule it supports: roles.config effort column; executor retry_effort=xhigh.
How to check: `grok --help | grep -i effort`
Expected: low/medium/high/xhigh accepted.
