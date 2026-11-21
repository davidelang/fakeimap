# shellcheck shell=bash
# Helpers for dispatch/dispatch. Sourced, not run.

die()  { printf '%s: %s\n' "$PROG" "$*" >&2; exit 2; }
say()  { printf '%s %s\n' "$(date '+%F %T')" "$*"; }
vlog() { if [[ "${VERBOSE:-0}" == 1 ]]; then say "$@"; fi; }

# ---------------------------------------------------------------- config
# Sources, highest first: CLI > sidecar > '# dispatch.' lines in the diag >
# dispatch/roles/<role>.conf. Inside each source a review key
# (review.NAME.KEY) beats a role key (ROLE.KEY), which beats a bare KEY.
declare -A CFG_CLI=() CFG_SIDE=() CFG_DIAG=() CFG_ROLE=()

# Keys whose values are paths, resolved against the file that set them.
is_path_key() {
  case "${1##*.}" in
    rules|prompt|fix_prompt|evidence_prompt|addendum) return 0 ;;
  esac
  return 1
}
is_cmd_key() {
  case "${1##*.}" in
    launch|channel|agent_bin) return 0 ;;
  esac
  return 1
}

# resolve_value KEY VALUE BASEDIR
resolve_value() {
  local key="$1" val="$2" base="$3" first rest
  if is_path_key "$key" && [[ -n "$val" && "$val" != /* ]]; then
    printf '%s/%s\n' "$base" "$val"; return
  fi
  if is_cmd_key "$key" && [[ -n "$val" ]]; then
    first="${val%% *}"; rest=""
    [[ "$val" == *" "* ]] && rest=" ${val#* }"
    if [[ "$first" == */* && "$first" != /* ]]; then
      printf '%s/%s%s\n' "$base" "$first" "$rest"; return
    fi
  fi
  printf '%s\n' "$val"
}

# load_kv FILE ARRAYNAME [prefix-to-strip]
# KEY=VALUE lines; '#' starts a comment line; blank lines skipped.
load_kv() {
  local file="$1" arr="$2" base line key val
  base=$(CDPATH='' cd -- "$(dirname -- "$file")" && pwd)
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    [[ "$line" == *=* ]] || { say "WARNING $file: ignored line: $line" >&2; continue; }
    key="${line%%=*}"; val="${line#*=}"
    key="${key%"${key##*[![:space:]]}"}"; val="${val#"${val%%[![:space:]]*}"}"
    key="${key#dispatch.}"
    val=$(resolve_value "$key" "$val" "$base")
    printf -v "${arr}[$key]" '%s' "$val"
  done <"$file"
}

# '# dispatch.KEY=VALUE' (or '// dispatch.KEY=VALUE') lines anywhere in the diag.
# channel skips every '#' / '//' line, so these never reach its parser.
load_diag_comments() {
  local diag="$1" base line key val
  base=$(CDPATH='' cd -- "$(dirname -- "$diag")" && pwd)
  while IFS= read -r line; do
    line="${line#"${line%%[![:space:]]*}"}"
    case "$line" in
      '# dispatch.'*) line="${line#'# dispatch.'}" ;;
      '// dispatch.'*) line="${line#'// dispatch.'}" ;;
      *) continue ;;
    esac
    line="${line%"${line##*[![:space:]]}"}"
    [[ "$line" == *=* ]] || continue
    key="${line%%=*}"; val="${line#*=}"
    CFG_DIAG[$key]=$(resolve_value "$key" "$val" "$base")
  done <"$diag"
}

# cfg ROLE KEY DEFAULT [REVIEW]
# The CLI only applies to the role this process was started as, not to reviews it runs.
cfg() {
  local role="$1" key="$2" def="$3" review="${4:-}" k src
  local -a keys=()
  if [[ -z "$review" && "$role" == "$ROLE" && -n "${CFG_CLI[$key]+x}" ]]; then
    printf '%s\n' "${CFG_CLI[$key]}"; return
  fi
  [[ -n "$review" ]] && keys+=("review.$review.$key")
  keys+=("$role.$key" "$key")
  for k in "${keys[@]}"; do
    for src in CFG_SIDE CFG_DIAG; do
      local -n m="$src"
      if [[ -n "${m[$k]+x}" ]]; then printf '%s\n' "${m[$k]}"; unset -n m; return; fi
      unset -n m
    done
  done
  if [[ -n "$review" && -n "${CFG_ROLE[$ROLE:review.$review.$key]+x}" ]]; then
    printf '%s\n' "${CFG_ROLE[$ROLE:review.$review.$key]}"; return
  fi
  if [[ -n "${CFG_ROLE[$role:$key]+x}" ]]; then printf '%s\n' "${CFG_ROLE[$role:$key]}"; return; fi
  printf '%s\n' "$def"
}

load_role_conf() {
  local role="$1" f="$HERE/roles/$1.conf" tmp k
  [[ -f "$f" ]] || die "no role file $f"
  declare -A _RC=()
  load_kv "$f" _RC
  for k in "${!_RC[@]}"; do CFG_ROLE["$role:$k"]="${_RC[$k]}"; done
  unset _RC tmp
}

# review names, in sorted order, from every source
review_names() {
  local k
  {
    for k in "${!CFG_SIDE[@]}" "${!CFG_DIAG[@]}"; do
      [[ "$k" == review.*.* ]] && { k="${k#review.}"; printf '%s\n' "${k%%.*}"; }
    done
    for k in "${!CFG_ROLE[@]}"; do
      [[ "$k" == "$ROLE:review."*.* ]] && { k="${k#"$ROLE:review."}"; printf '%s\n' "${k%%.*}"; }
    done
  } | sort -u
}

# ------------------------------------------------------------------ diag
# node NAME PATH lines (also inside the /* */ key, where they live).
diag_node_path() {
  awk -v n="$2" '{ l=$0; sub(/^[ \t]+/, "", l)
    if (l ~ /^node[ \t]/) { split(l, a, /[ \t]+/); if (a[2] == n) { print a[3]; exit } } }' "$1"
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
# render TEMPLATE OUT  ({{KEY}} -> TV[KEY]; unknown {{...}} left as is)
declare -A TV=()
render() {
  local t k
  t=$(<"$1")
  for k in "${!TV[@]}"; do t="${t//"{{$k}}"/"${TV[$k]}"}"; done
  printf '%s\n' "$t" >"$2"
}

# ------------------------------------------------------------------- git
git_head()  { git -C "$WORKTREE" rev-parse -q --verify HEAD 2>/dev/null || printf 'none\n'; }
git_dirty() { git -C "$WORKTREE" status --porcelain 2>/dev/null | sha256sum | cut -c1-16; }
# -q: a missing tag prints nothing and exits 1; that is "no commit", not an error.
tag_commit() { git -C "$WORKTREE" rev-parse -q --verify "refs/tags/$1^{commit}" || return 0; }
