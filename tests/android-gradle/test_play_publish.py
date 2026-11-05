#!/usr/bin/env python3
"""Regression tests for play-publish notes-base resolution and release-name provenance.

Scratch git repos and caches stay under ~/playstore (never /tmp). No live Play calls.
Run from anywhere:
  PYTHONDONTWRITEBYTECODE=1 PYTHONPYCACHEPREFIX=$HOME/playstore/pycache \\
    python3 tests/android-gradle/test_play_publish.py
"""
from __future__ import annotations

import importlib.util
import os
import shutil
import subprocess
import sys
import tempfile
import traceback
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PP_PATH = ROOT / "build-examples/android-gradle/play-publish"
SCRATCH = Path(os.path.expanduser("~/playstore/play-publish-test"))
PASS = FAIL = 0


def load_pp():
    import importlib.machinery
    loader = importlib.machinery.SourceFileLoader("play_publish", str(PP_PATH))
    spec = importlib.util.spec_from_loader(loader.name, loader)
    mod = importlib.util.module_from_spec(spec)
    loader.exec_module(mod)
    return mod


def check(name, cond, detail=""):
    global PASS, FAIL
    if cond:
        PASS += 1
        print("PASS  " + name)
    else:
        FAIL += 1
        print("FAIL  " + name + (("  " + detail) if detail else ""))


def run(cmd, cwd, check_rc=True, env=None):
    e = os.environ.copy()
    e["PYTHONDONTWRITEBYTECODE"] = "1"
    e["PYTHONPYCACHEPREFIX"] = str(SCRATCH / "pycache")
    if env:
        e.update(env)
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, env=e)
    if check_rc and p.returncode:
        raise RuntimeError("%s failed in %s: %s%s" % (cmd, cwd, p.stdout, p.stderr))
    return p


def git_repo(name):
    d = SCRATCH / name
    if d.exists():
        shutil.rmtree(d)
    d.mkdir(parents=True)
    run(["git", "init", "-b", "master"], d)
    run(["git", "config", "user.name", "Test"], d)
    run(["git", "config", "user.email", "test@example.com"], d)
    (d / "f").write_text("a\n")
    run(["git", "add", "f"], d)
    run(["git", "commit", "-m", "root"], d)
    return d


def sha(cwd, rev="HEAD"):
    return run(["git", "rev-parse", rev], cwd).stdout.strip()


def die_caught(fn, *a, **k):
    try:
        fn(*a, **k)
        return None
    except SystemExit as e:
        return e.code


def test_format_and_parse(pp):
    c = "a36abf76ad089e82173e76bb4e598543026ecd52"
    check("format exact-tag versionName",
          pp.format_release_name(25, "v0.9.3", c) == "25 (v0.9.3 @a36abf76ad08)")
    check("format describe versionName",
          pp.format_release_name(25, "v0.9.3-5-ga36abf76", c) ==
          "25 (v0.9.3-5-ga36abf76 @a36abf76ad08)")
    check("reject unsafe versionName paren",
          die_caught(pp.format_release_name, 1, "x(y)", c) == 1)
    check("parse provenance prefers @ over -g",
          pp.notes_base_from_release_name("25 (v0.9.3-5-ga36abf76 @a36abf76ad08)") ==
          ("provenance", "a36abf76ad08"))
    check("parse legacy describe -g",
          pp.notes_base_from_release_name("15 (v0.9.2-38-gb2c2ca61)") ==
          ("describe-hash", "b2c2ca61"))
    check("parse legacy exact-tag name",
          pp.notes_base_from_release_name("24 (v0.9.3)") == ("tag", "v0.9.3"))
    check("parse empty name fails",
          pp.notes_base_from_release_name("")[0] is None)
    check("NOTES_LIMIT is 500", pp.NOTES_LIMIT == 500)


def test_git_resolution(pp):
    repo = git_repo("tag-ancestor")
    root = sha(repo)
    (repo / "f").write_text("b\n")
    run(["git", "add", "f"], repo)
    run(["git", "commit", "-m", "second"], repo)
    head = sha(repo)
    run(["git", "tag", "-a", "v0.9.3", "-m", "v0.9.3", root], repo)

    # cwd must be the repo for git helpers
    old = os.getcwd()
    os.chdir(repo)
    try:
        check("resolve_exact_tag finds annotated tag",
              pp.resolve_exact_tag("v0.9.3") == root)
        check("resolve_exact_tag ignores missing",
              pp.resolve_exact_tag("v9.9.9") is None)
        # branch with same name must not count as tag
        run(["git", "branch", "not-a-release"], repo)
        check("resolve_exact_tag refuses branch name",
              pp.resolve_exact_tag("not-a-release") is None)

        # provenance path via require_ancestor
        check("require_ancestor accepts ancestor",
              die_caught(pp.require_ancestor, root, "tag v0.9.3") is None)
        # non-ancestor: create divergent commit
        run(["git", "checkout", "-q", "--orphan", "other"], repo)
        (repo / "f").write_text("z\n")
        run(["git", "add", "f"], repo)
        run(["git", "commit", "-m", "orphan"], repo)
        orphan = sha(repo)
        run(["git", "checkout", "-q", "master"], repo)
        check("require_ancestor rejects non-ancestor",
              die_caught(pp.require_ancestor, orphan, "orphan") == 1)

        # Simulate notes_base_from_release_name + tag resolution for "24 (v0.9.3)"
        kind, token = pp.notes_base_from_release_name("24 (v0.9.3)")
        base = pp.resolve_exact_tag(token)
        check("legacy hashless tag resolves to root", kind == "tag" and base == root)
        check("legacy hashless tag is ancestor of HEAD",
              die_caught(pp.require_ancestor, base, "tag v0.9.3") is None)

        # absent tag name in release
        kind2, tok2 = pp.notes_base_from_release_name("24 (v0.9.9)")
        check("absent tag candidate stays tag kind", kind2 == "tag" and tok2 == "v0.9.9")
        check("absent tag does not resolve", pp.resolve_exact_tag(tok2) is None)

        # new provenance name
        name = pp.format_release_name(25, "v0.9.3", head)
        kind3, tok3 = pp.notes_base_from_release_name(name)
        check("new provenance kind", kind3 == "provenance")
        check("new provenance token is short hash prefix of HEAD",
              head.startswith(tok3) and len(tok3) == 12)
    finally:
        os.chdir(old)


def test_mocked_dry_run_notes_base(pp):
    """Drive notes-base selection with a fake highest release; no HTTP upload."""
    repo = git_repo("mock-play")
    root = sha(repo)
    run(["git", "tag", "-a", "v0.9.3", "-m", "v0.9.3", root], repo)
    (repo / "f").write_text("c\n")
    run(["git", "add", "f"], repo)
    run(["git", "commit", "-m", "feature"], repo)
    head = sha(repo)
    # write notes exclude next to play-publish (already exists in tree); also local empty
    work = SCRATCH / "mock-work"
    if work.exists():
        shutil.rmtree(work)
    work.mkdir()
    # exclude file is loaded from dirname of play-publish

    old = os.getcwd()
    os.chdir(repo)
    try:
        # Build a tiny harness that calls the resolution block logic
        cases = [
            ("24 (v0.9.3)", "tag", root),
            ("15 (v0.9.2-38-g%s)" % root[:8], "describe-hash", root),
            ("25 (v0.9.3 @%s)" % head[:12], "provenance", head),
        ]
        for name, expect_kind, expect_full in cases:
            kind, token = pp.notes_base_from_release_name(name)
            if kind == "tag":
                base = pp.resolve_exact_tag(token)
            else:
                base = pp.git_commit(token)
            check("mock resolve %s -> %s" % (name, expect_kind),
                  kind == expect_kind and base == expect_full,
                  detail="got %s %s" % (kind, base))

        # notes-file path only (editor path needs a TTY and WORK_DIR; covered by limits below)
        nf = work / "notes.txt"
        nf.write_text("custom notes for play\n")
        check("get_notes from file ignores base",
              pp.get_notes(None, str(nf)) == "custom notes for play")
        empty = work / "empty.txt"
        empty.write_text("  \n")
        check("empty notes-file dies", die_caught(pp.get_notes, None, str(empty)) == 1)
        big = work / "big.txt"
        big.write_text("x" * 501)
        check("oversize notes-file dies", die_caught(pp.get_notes, None, str(big)) == 1)
        # git log subjects used for generated notes (no editor): mirror get_notes filter
        subjects = subprocess.run(
            ["git", "log", "--no-merges", "--format=%s", root + "..HEAD"],
            capture_output=True, text=True, check=True).stdout.splitlines()
        check("generated notes would include feature commit", "feature" in subjects)

    finally:
        os.chdir(old)


def test_select_play_print_commit():
    """android-build-info select play prints the stamped commit (unit-level via source)."""
    text = (ROOT / "build-examples/android-gradle/android-build-info").read_text()
    check("select_play prints commit field",
          'print("bundle %s %d %s" % (s["aab"], s["version_code"], s["commit"]))' in text)
    dep = (ROOT / "build-examples/android-gradle/deploy").read_text()
    check("deploy passes --build-commit", "--build-commit" in dep and "PLAY_COMMIT" in dep)


def main():
    SCRATCH.mkdir(parents=True, exist_ok=True)
    (SCRATCH / "pycache").mkdir(exist_ok=True)
    pp = load_pp()
    test_format_and_parse(pp)
    test_git_resolution(pp)
    test_mocked_dry_run_notes_base(pp)
    test_select_play_print_commit()
    print("----")
    print("%d passed, %d failed" % (PASS, FAIL))
    return 1 if FAIL else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:
        traceback.print_exc()
        sys.exit(2)
