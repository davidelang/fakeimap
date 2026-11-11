Feature: context window set via `/context-window <n>` or `/model <m> <n> <effort>`; no CLI flag.
Rule it supports: roles.config context column (orchestrator and planner 500k, others 256k).
How to check: `grok --help | grep -i context`; models_cache.json context sizes for the role model.
Expected: grok-4.7 offers 256000 and 500000 (labels 256k and 500k). No CLI flag. Orchestrator and planner use 500k.
