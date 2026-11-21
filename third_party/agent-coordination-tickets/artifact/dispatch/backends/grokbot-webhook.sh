# shellcheck shell=bash
# Backend: hand the job to a Grok Bot through a webhook. STUB: the endpoint,
# auth and result path are not settled, so this backend refuses to run.
# The channel's own per-node webhook= (see docs/plan-file.md) is the
# supported way to wake a Grok Bot node today.
backend_argv() {
  echo "grokbot-webhook backend is a stub; use a channel node with webhook= instead" >&2
  return 1
}
backend_text() { cat "$1"; }
backend_cost() { return 1; }
