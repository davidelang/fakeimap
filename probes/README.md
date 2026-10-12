# Upgrade probes (used by the check-upgrade skill)

Each probe is a short markdown file: `Feature`, `Rule it supports`, `How to check`
(a command, doc grep, or session-log check), `Expected`.

- `general/`: capabilities the rules depend on. Edit freely when the rules change.
- `protected/`: one probe per past incident (something that broke the rules). Add one
  when an incident happens: `protected/YYYYMMDD-<slug>.md`, with the incident in one line.
  **Never delete or weaken a protected probe.** `tools/upgrade-probe` fails if the
  count of protected probes drops below `protected/COUNT`.
