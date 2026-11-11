#!/usr/bin/env python3
"""cmd-words.py MODE COMMAND: what a shell command line runs, from its command words only.

Hooks decide on structured values, never on substrings of arbitrary text: a message that
mentions ./build_app inside a channel-send -m '...' argument is an argument, not a command.
A command word is the first word of each simple command (split at ; & | && || ( ) and newline),
after VAR=value words and the wrappers env/nohup/time/exec/command/nice/sudo/timeout/stdbuf/xargs
(with their -options and timeout's duration), and also inside sh/bash/dash -c '...'.

  build COMMAND          exit 1 when a command word is build_app or commit-build (any path), else 0
  git-commit COMMAND     exit 1 when a command word is git and its subcommand is add or commit, else 0
  channel-watch COMMAND  print "PLAN<TAB>NODE<TAB>stop|arm" for each run of cursor-channel-watch
                         (any path) with its first two arguments; nothing when there is none
An unparsable command (unbalanced quotes) exits 2 with the reason on stderr.
"""
import os
import shlex
import sys

SEP_CHARS = set(";&|()\n")
WRAP = {"env", "nohup", "time", "exec", "command", "nice", "sudo", "timeout", "stdbuf", "xargs"}
SHELLS = {"sh", "bash", "dash"}


def words(text):
    lx = shlex.shlex(text, posix=True, punctuation_chars=";&|()<>\n")
    lx.whitespace = " \t\r"
    lx.whitespace_split = True
    lx.commenters = "#"
    return list(lx)


def simple_commands(text, depth=0):
    """Yield each simple command as its list of words, command word first."""
    if depth > 4:
        return
    cur = []
    for t in words(text) + [";"]:
        if set(t) <= SEP_CHARS:
            yield from strip_prefix(cur, depth)
            cur = []
        else:
            cur.append(t)


def strip_prefix(ws, depth):
    i = 0
    while i < len(ws):
        t = ws[i]
        if "=" in t and t.split("=", 1)[0].isidentifier():
            i += 1
        elif t in WRAP:
            i += 1
            while i < len(ws) and (ws[i].startswith("-") or (t == "timeout" and ws[i][:1].isdigit())):
                i += 1
        else:
            break
    ws = ws[i:]
    if not ws:
        return
    if os.path.basename(ws[0]) in SHELLS and len(ws) > 2 and ws[1] == "-c":
        yield from simple_commands(ws[2], depth + 1)
        return
    yield ws


# Global git options that take a separate argument. Their values are not the subcommand.
GIT_TAKES_ARG = {"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--super-prefix", "--config-env", "--exec-path"}


def git_subcommand(ws):
    """Subcommand of a git argv, after global options. Empty when there is none."""
    i = 1
    while i < len(ws):
        w = ws[i]
        if w == "--":
            i += 1
            break
        if w in GIT_TAKES_ARG:
            i += 2
            continue
        if w.startswith("-"):
            i += 1
            continue
        return w
    return ws[i] if i < len(ws) else ""


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in ("build", "git-commit", "channel-watch"):
        print("usage: cmd-words.py build|git-commit|channel-watch COMMAND", file=sys.stderr)
        sys.exit(2)
    mode, text = sys.argv[1], sys.argv[2]
    try:
        cmds = list(simple_commands(text))
    except ValueError as exc:
        print("cannot parse the command: %s" % exc, file=sys.stderr)
        sys.exit(2)
    if mode == "build":
        sys.exit(1 if any(os.path.basename(c[0]) in ("build_app", "commit-build") for c in cmds) else 0)
    if mode == "git-commit":
        sys.exit(1 if any(os.path.basename(c[0]) == "git" and git_subcommand(c) in ("add", "commit") for c in cmds) else 0)
    for c in cmds:
        if os.path.basename(c[0]) == "cursor-channel-watch" and len(c) >= 3:
            print("%s\t%s\t%s" % (c[1], c[2], "stop" if len(c) > 3 and c[3] == "--stop" else "arm"))


main()
