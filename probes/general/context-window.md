Feature: context window set via `/context-window <n>` or `/model <m> <n> <effort>`; no CLI flag.
Rule it supports: roles.config context column (TODO-VERIFY 512k).
How to check: `grok --help | grep -i context`; models_cache.json context sizes.
Expected: 512k accepted for the orchestrator/planner model, or a CLI flag appears (then use it in the launcher).
