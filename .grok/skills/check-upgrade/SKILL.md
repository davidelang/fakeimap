---
name: check-upgrade
description: Maintainer-only. After a grok upgrade, detect drift between grok capabilities and the rules.
disable-model-invocation: true
---
# check-upgrade

Intent: the rules rely on specific grok behaviors (see `probes/`). After an upgrade,
find which ones changed and propose rule edits. Read-only; output a short report.

1. `tools/upgrade-scan`: version vs last reviewed, changelog hits mapped to probes.
2. `tools/upgrade-probe`: list probes; it fails if a protected probe was removed (stop and tell David).
3. Run each hit probe's "How to check"; mark PASS / CHANGED / UNKNOWN.
4. Also scan the changelog for new features that could cut tokens or enforce a rule
   (hooks, roles, caching, context); list them as proposals.
5. Report: CHANGED first, then proposals. On approval, edit canon
   (`~/git/orchestration-example`), bump `reviewed` in `tools/upgrade-map.tsv`, publish
   with `./deploy-orchestration`. Add a `probes/protected/` probe for any incident.
End with `STATUS: <state> - <one line>`.
