# shellcheck shell=bash
# Helpers for dispatch/dispatch. Sourced, not run.

die()  { printf '%s: %s\n' "$PROG" "$*" >&2; exit 2; }
say()  { printf '%s %s\n' "$(date '+%F %T')" "$*"; }
vlog() { if [[ "${VERBOSE:-0}" == 1 ]]; then say "$@"; fi; }

# ---------------------------------------------------------------- config
# A run's settings: the command line (this process's own role only), else the role's
# built-in default below. The role is fixed by the run-* wrapper that starts the
# dispatcher (-r), as run-grok-coder fixes ROLE_KEY; everything per role is here, as
# grok-launch-common.sh keeps its per-role cases. Project values are the wrapper's
# options; agent rules are the worktree's roles/<role>.md; verbs are the diag's.
declare -A CFG_CLI=()

# role_default ROLE KEY: the built-in value ('' when the role has none)
role_default() {
  case "$1:$2" in
    executor:access) echo read-write ;;
    reviewer:access|tester:access) echo read-only ;;
    *:rules) echo "$HERE/roles/$1.md" ;;
    # Live wifi-mapper runs (GB-1, Nov 20): landing one plan took 80 (cap hit) + 100 + 7
    # executor turns, fix sessions 34 and 13; reviews 20 and 60 turns.
    executor:max_turns) echo 200 ;;
    reviewer:max_turns) echo 80 ;;
    tester:max_turns) echo 80 ;;
    *:timeout) echo 0 ;;
    # A STATUS: SUCCESS with HEAD unchanged is not done.
    executor:require_commit) echo yes ;;
    *) echo "" ;;
  esac
}

# cfg ROLE KEY DEFAULT [REVIEW]
cfg() {
  local role="$1" key="$2" def="$3" review="${4:-}" v
  if [[ -z "$review" && "$role" == "$ROLE" && -n "${CFG_CLI[$key]+x}" ]]; then
    printf '%s\n' "${CFG_CLI[$key]}"; return
  fi
  v=$(role_default "$role" "$key")
  printf '%s\n' "${v:-$def}"
}

# The executor's blocking reviews: one, 'conformance', run by the reviewer role (over the
# channel when the diag has 'verb <this node> <reviewer node> REVIEW', else in-process).
review_names() { echo conformance; }

# ------------------------------------------------------------------ diag
# node NAME PATH lines (also inside the /* */ key, where they live).
diag_node_path() {
  awk -v n="$2" '{ l=$0; sub(/^[ \t]+/, "", l)
    if (l ~ /^node[ \t]/) { split(l, a, /[ \t]+/); if (a[2] == n) { print a[3]; exit } } }' "$1"
}

# diag_verb_dests DIAG SRC VERB: every DEST of a 'verb SRC DEST WORD...' line that lists VERB
diag_verb_dests() {
  awk -v s="$2" -v v="$3" '{ l=$0; sub(/^[ \t]+/, "", l)
    if (l ~ /^verb[ \t]/) { n = split(l, a, /[ \t]+/)
      if (a[2] == s) for (i = 4; i <= n; i++) if (a[i] == v) { print a[3]; break } } }' "$1"
}

# body lines of 'message VERB { ... body: { ... } }'
diag_body_lines() {
  awk -v v="$2" '
    { l=$0; sub(/^[ \t]+/, "", l); sub(/[ \t]+$/, "", l) }
    st == 0 && l ~ ("^message[ \t]+" v "[ \t]*[{]") { st = 1; next }
    st == 1 && l ~ /^body:[ \t]*[{]/ { st = 2; next }
    st == 2 { if (l == "}") exit; if (l ~ /^(#|\/\/)/) next; print l }
  ' "$1"
}

# fill_body DIAG VERB OUT   (fields from the assoc array F)
# Literal lines are copied; each 'KEY: <...>' line takes F[KEY] (or 'none').
fill_body() {
  local diag="$1" verb="$2" out="$3" line k v n=0
  local re='^([A-Za-z0-9_-]+):[[:space:]]*<.*>$'
  : >"$out"
  while IFS= read -r line; do
    n=$((n + 1))
    if [[ "$line" =~ $re ]]; then
      k="${BASH_REMATCH[1]}"; v="${F[$k]:-none}"; v="${v//$'\n'/ }"
      printf '%s: %s\n' "$k" "$v" >>"$out"
    else
      printf '%s\n' "$line" >>"$out"
    fi
  done < <(diag_body_lines "$diag" "$verb")
  [[ $n -gt 0 ]]
}

# --------------------------------------------------------------- messages
# parse_message FILE: header FROM/TO/SEQ/VERB into M_*, every other
# 'KEY: value' line (first one wins) into MF.
declare -A MF=() F=()
parse_message() {
  local line k v
  M_FROM=""; M_VERB=""; M_SEQ=""; MF=()
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^([A-Za-z0-9_-]+):[[:space:]]?(.*)$ ]] || continue
    k="${BASH_REMATCH[1]}"; v="${BASH_REMATCH[2]}"
    case "$k" in
      FROM) [[ -z "$M_FROM" ]] && M_FROM="$v" ;;
      VERB) [[ -z "$M_VERB" ]] && M_VERB="$v" ;;
      SEQ)  [[ -z "$M_SEQ" ]] && M_SEQ="$v" ;;
      TO|REFS) ;;
      *) [[ -z "${MF[$k]+x}" ]] && MF[$k]="$v" ;;
    esac
  done <"$1"
  return 0
}

# ------------------------------------------------------------ agent output
# last 'STATUS: WORD rest' line of a text file -> "WORD<TAB>rest"
last_status() {
  [[ -f "$1" ]] || return 1
  sed -e 's/\*\*//g' -e 's/`//g' "$1" \
    | awk '{ l=$0; sub(/^[ \t>*-]+/, "", l)
             if (l ~ /^STATUS:[ \t]*[A-Za-z]/) { sub(/^STATUS:[ \t]*/, "", l); s=l } }
           END { if (s == "") exit 1
                 w=s; sub(/[ \t].*$/, "", w); r=s; sub(/^[^ \t]*[ \t]*/, "", r)
                 printf "%s\t%s\n", toupper(w), r }'
}

# ------------------------------------------------------------- templates
# say_prompt OUT TEXT: the session's prompt file, "Read the rules." then the job in one line
say_prompt() { printf 'Read the rules. %s\n' "$2" >"$1"; }

# ------------------------------------------------------------------- git
git_head()  { git -C "$WORKTREE" rev-parse -q --verify HEAD 2>/dev/null || printf 'none\n'; }
git_dirty() { git -C "$WORKTREE" status --porcelain 2>/dev/null | sha256sum | cut -c1-16; }
# -q: a missing tag prints nothing and exits 1; that is "no commit", not an error.
tag_commit() { git -C "$WORKTREE" rev-parse -q --verify "refs/tags/$1^{commit}" || return 0; }
