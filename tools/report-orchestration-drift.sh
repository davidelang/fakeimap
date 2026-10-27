#!/usr/bin/env bash
# report-orchestration-drift.sh — compare orchestration tooling across git_home hosts.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

GIT_HOME=""
REF=""
JSON=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --git-home) shift; GIT_HOME="${1:-}" ;;
    --ref) shift; REF="${1:-}" ;;
    --json) JSON=1 ;;
    -h|--help)
      echo "Usage: $0 [--git-home DIR] [--ref DIR] [--json]"
      exit 0
      ;;
    *) echo "Unknown: $1" >&2; exit 2 ;;
  esac
  shift
done

# Resolve git_home
if [[ -z "$GIT_HOME" && -f "$EXAMPLE_ROOT/project.config" ]]; then
  GIT_HOME="$(grep -E '^git_home=' "$EXAMPLE_ROOT/project.config" | head -1 | cut -d= -f2- || true)"
fi
GIT_HOME="${GIT_HOME:-${HOME}/git}"
REF="${REF:-$GIT_HOME/VehicleExpenses-automated}"

if [[ ! -d "$REF" ]]; then
  echo "ERROR: reference not found: $REF" >&2
  exit 1
fi

# Canonical orchestration paths relative to host root.
# Format: canonical_name|ref_relpath|alt_relpath|alt_relpath...
CANON_FILES=(
  "setup_agent.sh|setup_agent.sh"
  "remove_worktree.sh|remove_worktree.sh"
  "update-rules.sh|update-rules.sh"
  "fix-perms|fix-perms"
  "env|ve-env|env"
  "refresh-shell.c|ve-refresh-shell.c|refresh-shell.c"
  "install-refresh-shell.sh|install-ve-refresh-shell.sh|install-refresh-shell.sh"
  "agent-landlock|agent-landlock"
  "landlock-smoke-matrix|landlock-smoke-matrix"
  "landlock-write-probe|landlock-write-probe"
  "landlock.config|landlock.config"
  "landlock.config.example|landlock.config.example"
  "grok-launch-common|.grok/lib/grok-launch-common.sh"
  "run-grok|run-grok"
  "run-grok-orchestrator|run-grok-orchestrator"
  "run-grok-planner|run-grok-planner"
  "run-grok-coder|run-grok-coder"
  "run-grok-master|run-grok-master"
  "filter-apply-config|filter-apply-config"
  "filter-clean-config|filter-clean-config"
  "append-to-engineering-log|append-to-engineering-log"
  "todo-append|todo-append"
  "todo-close|todo-close"
  "get-builds-tag.sh|get-builds-tag.sh"
  "install-merge-drivers.sh|install-merge-drivers.sh"
  "generate_pr.sh|generate_pr.sh"
  "hooks/post-checkout|hooks/post-checkout"
  "project.config.example|project.config.example"
  "AGENT_MANDATES.md|AGENT_MANDATES.md"
  "AGENTS.md|AGENTS.md"
  "standard-plan-compliance-block.md|standard-plan-compliance-block.md"
  "MASTER_AGENT_MANDATE.md|MASTER_AGENT_MANDATE.md"
  "MULTI_AGENT_USER_INSTRUCTIONS.md|MULTI_AGENT_USER_INSTRUCTIONS.md"
)

resolve_file() {
  local root="$1"; shift
  local p
  for p in "$@"; do
    [[ -z "$p" ]] && continue
    if [[ -e "$root/$p" ]]; then
      echo "$p"
      return 0
    fi
  done
  return 1
}

# Discover candidate hosts: directories under GIT_HOME that look like multi-agent orch
discover_hosts() {
  local d base
  for d in "$GIT_HOME"/*; do
    [[ -d "$d" ]] || continue
    base="$(basename "$d")"
    [[ "$base" == "orchestration-example" ]] && continue
    if [[ -f "$d/setup_agent.sh" || -f "$d/run-grok-orchestrator" || -f "$d/AGENT_MANDATES.md" ]]; then
      echo "$d"
    fi
  done
}

file_sig() {
  local f="$1"
  if [[ ! -e "$f" ]]; then
    echo "MISSING"
    return
  fi
  local sz md
  sz=$(wc -c <"$f" | tr -d ' ')
  md=$(md5sum "$f" | awk '{print $1}')
  # landlock wiring marker
  local mark=""
  if [[ "$f" == *grok-launch-common.sh ]]; then
    if grep -q 'agent-landlock' "$f" 2>/dev/null; then mark="+landlock"; else mark="-landlock"; fi
    if grep -q 'build_grok_sandbox_profile' "$f" 2>/dev/null; then mark="${mark}+sandbox"; else mark="${mark}-sandbox"; fi
  fi
  echo "${sz}:${md}${mark}"
}

echo "=== Orchestration drift report ==="
echo "git_home=$GIT_HOME"
echo "reference=$REF"
echo "generated=$(date -Is)"
echo ""

hosts=()
while IFS= read -r h; do hosts+=("$h"); done < <(discover_hosts)
# always include example if present
if [[ -d "$EXAMPLE_ROOT" ]]; then
  hosts+=("$EXAMPLE_ROOT")
fi

# unique
mapfile -t hosts < <(printf '%s\n' "${hosts[@]}" | awk '!a[$0]++')

if [[ "$JSON" -eq 1 ]]; then
  echo '{"ref":"'"$REF"'","git_home":"'"$GIT_HOME"'","hosts":['
fi

for host in "${hosts[@]}"; do
  [[ "$host" == "$REF" ]] && continue
  name="$(basename "$host")"
  echo "######################################################################"
  echo "# host: $host"
  echo "######################################################################"

  # project.config git_home
  if [[ -f "$host/project.config" ]]; then
    gh=$(grep -E '^git_home=' "$host/project.config" | head -1 || true)
    if [[ -z "$gh" || "$gh" == *@@* ]]; then
      echo "CRITICAL project.config missing/unresolved git_home"
    else
      echo "OK $gh"
    fi
  else
    echo "HIGH project.config missing (gitignored local file)"
  fi

  # refresh-shell setuid
  for cand in refresh-shell ve-refresh-shell; do
    if [[ -e "$host/$cand" ]]; then
      own=$(stat -c '%U:%G %a' "$host/$cand")
      if [[ -u "$host/$cand" && "$(stat -c '%U' "$host/$cand")" == "root" ]]; then
        echo "OK $cand setuid-root ($own)"
      else
        echo "HIGH $cand present but not setuid-root ($own)"
      fi
    fi
  done

  for entry in "${CANON_FILES[@]}"; do
    IFS='|' read -r cname rest <<<"$entry"
    # shellcheck disable=SC2206
    alts=(${rest//|/ })
    ref_rel="$(resolve_file "$REF" "${alts[@]}" || true)"
    host_rel="$(resolve_file "$host" "${alts[@]}" || true)"
    if [[ -z "$ref_rel" && -z "$host_rel" ]]; then
      continue
    fi
    if [[ -z "$host_rel" ]]; then
      # optional android-only tools
      case "$cname" in
        fix-android*|sync-debug*|build_app|deploy) echo "LOW $cname missing on host (optional/product)" ;;
        *) echo "HIGH $cname MISSING on host (ref has ${ref_rel:-?})" ;;
      esac
      continue
    fi
    if [[ -z "$ref_rel" ]]; then
      echo "LOW $cname only on host ($host_rel) not on ref"
      continue
    fi
    rs=$(file_sig "$REF/$ref_rel")
    hs=$(file_sig "$host/$host_rel")
    if [[ "$rs" == "$hs" ]]; then
      echo "OK $cname ($host_rel) match ref"
    else
      # size class
      rsz=${rs%%:*}; hsz=${hs%%:*}
      if [[ "$rs" == MISSING ]]; then
        echo "MED $cname host-only path?"
      elif [[ "$cname" == "setup_agent.sh" || "$cname" == "update-rules.sh" || "$cname" == "fix-perms" || "$cname" == "remove_worktree.sh" ]]; then
        # thin stub heuristic
        if [[ "${hsz:-0}" -lt $((rsz / 3)) ]]; then
          echo "CRITICAL $cname thin stub host=${hsz}B ref=${rsz}B ($host_rel vs $ref_rel)"
        else
          echo "HIGH $cname content drift host=$hs ref=$rs"
        fi
      elif [[ "$cname" == "grok-launch-common" ]]; then
        if [[ "$hs" == *"-landlock"* && "$rs" == *"+landlock"* ]]; then
          echo "CRITICAL grok-launch-common missing agent-landlock wiring"
        elif [[ "$hs" == *"-sandbox"* && "$rs" == *"+sandbox"* ]]; then
          echo "CRITICAL grok-launch-common missing Grok sandbox profile wiring"
        else
          echo "HIGH grok-launch-common drift host=$hs ref=$rs"
        fi
      elif [[ "$cname" == AGENT_* || "$cname" == AGENTS.md || "$cname" == standard-plan* || "$cname" == MULTI_AGENT* ]]; then
        echo "MED policy doc drift $cname (may be intentional library adaptation)"
      else
        echo "MED $cname drift host=$hs ref=$rs"
      fi
    fi
  done
  echo ""
done

echo "=== done ==="
echo "See docs/ORCHESTRATION_DRIFT.md for severity and fix guidance."
