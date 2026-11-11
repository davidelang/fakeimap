#!/bin/bash
# install-refresh-shell.sh — build + install setuid-root refresh-shell
#
# The binary is NOT checked into git (architecture-specific; setuid root cannot
# be stored safely). Source refresh-shell.c IS tracked.
#
# Usage:
#   ./orch/install-refresh-shell.sh                 # install into cwd
#   ./orch/install-refresh-shell.sh /path/to/wt     # install into that worktree
#   ./orch/install-refresh-shell.sh -a|--all        # orch root + all worktrees
#   ./orch/install-refresh-shell.sh -h|--help
#
# Requires sudo for chown root + chmod 4755. Idempotent.
# NEVER leaves setuid bit on a non-root-owned binary (kills terminals via env).

set -euo pipefail

ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
ORCH="$ROOT"
if [ -f "$ROOT/update-rules.sh" ]; then
  ORCH="$ROOT"
fi

is_good() {
  local b="$1"
  [ -f "$b" ] && [ -x "$b" ] && [ -u "$b" ] && [ "$(stat -c '%U' "$b" 2>/dev/null)" = "root" ]
}

install_one() {
  local dest="$1"
  local src_c bin
  [ -d "$dest" ] || return 1

  mkdir -p "$dest/orch" || return 1
  src_c=""
  if [ -f "$dest/orch/refresh-shell.c" ]; then
    src_c="$dest/orch/refresh-shell.c"
  elif [ -f "$ORCH/orch/refresh-shell.c" ]; then
    src_c="$ORCH/orch/refresh-shell.c"
    if ! cp -f "$src_c" "$dest/orch/refresh-shell.c"; then
      echo "install-refresh-shell: cannot copy refresh-shell.c into $dest/orch" >&2
      return 1
    fi
    src_c="$dest/orch/refresh-shell.c"
  else
    echo "install-refresh-shell: no refresh-shell.c in $dest or $ORCH" >&2
    return 1
  fi

  bin="$dest/orch/refresh-shell"
  if [ ! -f "$bin" ] || [ "$src_c" -nt "$bin" ]; then
    echo "  Building $bin ..."
    if ! gcc -O2 -Wall -o "$bin" "$src_c"; then
      echo "install-refresh-shell: gcc failed for $bin" >&2
      return 1
    fi
  fi

  # Always strip setuid until root owns the file.
  chmod a-s "$bin" 2>/dev/null || true
  chmod 755 "$bin" 2>/dev/null || true

  if is_good "$bin"; then
    echo "  OK (already): $bin ($(stat -c '%U:%G %a' "$bin" 2>/dev/null))"
    return 0
  fi

  if [ "$(id -u)" -eq 0 ]; then
    chown root:root "$bin" && chmod 4755 "$bin"
    echo "  OK: $bin ($(stat -c '%U:%G %a' "$bin" 2>/dev/null))"
    return 0
  fi
  if command -v sudo >/dev/null 2>&1; then
    if sudo chown root:root "$bin" 2>/dev/null && sudo chmod 4755 "$bin" 2>/dev/null; then
      echo "  OK: $bin ($(stat -c '%U:%G %a' "$bin" 2>/dev/null))"
      return 0
    fi
  fi

  echo "  WARN: $bin not setuid-root (owner=$(stat -c '%U' "$bin" 2>/dev/null)). Run:" >&2
  echo "    sudo chown root:root $bin && sudo chmod 4755 $bin" >&2
  return 1
}

parsed="$(getopt -o ha -l help,all -- "$@")" || exit 2
eval set -- "$parsed"
ALL=0
while true; do
  case "$1" in
    -h|--help)
      echo "Usage: ./orch/install-refresh-shell.sh [-a|--all] [-h|--help] [DIR]"
      echo "  no DIR: install into the current directory"
      echo "  -a, --all: orchestration root and every worktree"
      exit 0 ;;
    -a|--all) ALL=1; shift ;;
    --) shift; break ;;
    *) echo "usage: ./orch/install-refresh-shell.sh [--all] [DIR]" >&2; exit 2 ;;
  esac
done
if [ "$ALL" -eq 1 ] && [ $# -gt 0 ]; then
  echo "usage: ./orch/install-refresh-shell.sh [--all] [DIR]" >&2
  exit 2
fi
if [ "$ALL" -eq 0 ] && [ $# -gt 1 ]; then
  echo "usage: ./orch/install-refresh-shell.sh [--all] [DIR]" >&2
  exit 2
fi
MODE="${1:-.}"
if [ "$ALL" -eq 1 ]; then
  echo "install-refresh-shell: orchestration + worktrees under $ORCH"
  install_one "$ORCH" || true
  for d in "$ORCH"/agent-* "$ORCH"/master; do
    [ -d "$d" ] || continue
    [ -f "$d/.git" ] || [ -d "$d/.git" ] || continue
    echo "install-refresh-shell: $d"
    install_one "$d" || true
  done
  if command -v git >/dev/null 2>&1; then
    while read -r wt; do
      [ -n "$wt" ] || continue
      [ "$wt" = "$ORCH" ] && continue
      [ -d "$wt" ] || continue
      echo "install-refresh-shell: $wt"
      install_one "$wt" || true
    done < <(git -C "$ORCH" worktree list --porcelain 2>/dev/null | awk '/^worktree /{print $2}')
  fi
  exit 0
fi

DEST="$MODE"
if [ "$DEST" = "." ] || [ -z "$DEST" ]; then
  DEST=$(pwd)
fi
DEST=$(cd "$DEST" && pwd)
echo "install-refresh-shell: $DEST"
install_one "$DEST"
if is_good "$DEST/orch/refresh-shell"; then
  exit 0
fi
exit 1
