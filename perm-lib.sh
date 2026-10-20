#!/bin/bash
# perm-lib.sh: apply and verify the permission model in perm-model.tsv (same directory;
# the work is done by perm-engine.py, also in that directory).
#
# Source it, then call perm_init with the project.config to resolve owner/group keys:
#   . "$DIR/perm-lib.sh"; perm_init "$DIR/project.config"
#   perm_umask AREA                       print the area's umask
#   perm_apply AREA PATH [opts]           set owner/group/modes where they differ from the model
#   perm_verify AREA PATH [opts]          list every path that differs; return 1 if any
#   perm_preflight AREA PATH [opts]       list every difference this user cannot fix (another
#                                         owner needed, or a mode change on a path someone else
#                                         owns); return 1 if any. Changes nothing.
#   perm_readable_nogroups USER PATH...   PATH readable by USER with only USER's primary group
#                                         (no supplementary groups, as in a login session that
#                                         predates a group change); return 1 naming the blocker
# opts: --prune DIR (repeatable; never entered), --prune-name NAME (e.g. .git), --top-only
#       (PATH itself, not below it), --maxdepth N, --skip-glob GLOB, --owner USER (overrides
#       the model's owner column, e.g. fix-perms' SUDO_USER).
# perm_apply only calls chown/chgrp/chmod (from PATH) on paths that differ, and every failure
# is reported and returned. Nothing here hides an error. Symlinks are never followed;
# their own owner is set with chown -h, their mode is never touched.

PERM_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PERM_MODEL="$PERM_LIB_DIR/perm-model.tsv"
PERM_ENGINE="$PERM_LIB_DIR/perm-engine.py"
PERM_CONFIG=""

perm_init() {
  local cfg="${1:-}"
  if [ -z "$cfg" ] || [ ! -f "$cfg" ]; then
    echo "ERROR: perm_init: project.config not found: '$cfg'" >&2
    return 1
  fi
  if [ ! -f "$PERM_MODEL" ] || [ ! -f "$PERM_ENGINE" ]; then
    echo "ERROR: perm_init: $PERM_MODEL or $PERM_ENGINE missing (they ship with perm-lib.sh)" >&2
    return 1
  fi
  PERM_CONFIG="$cfg"
}

_perm_engine() {
  if [ -z "$PERM_CONFIG" ]; then
    echo "ERROR: perm-lib: call perm_init PROJECT_CONFIG first" >&2
    return 1
  fi
  python3 "$PERM_LIB_DIR/perm-engine.py" "$PERM_MODEL" "$PERM_CONFIG" "$@"
}

perm_umask()              { _perm_engine umask "$@"; }
perm_apply()              { _perm_engine apply "$@"; }
perm_verify()             { _perm_engine verify "$@"; }
perm_preflight()          { _perm_engine preflight "$@"; }
perm_readable_nogroups()  { _perm_engine readable-nogroups "$@"; }
