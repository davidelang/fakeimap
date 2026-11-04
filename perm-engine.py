#!/usr/bin/env python3
# perm-engine.py: the engine behind perm-lib.sh (apply / verify / umask / readable-nogroups
# over perm-model.tsv). deploy-orchestration imports it for apply_spec/load_model, so a
# written file's mode comes from the same model. See perm-lib.sh for the commands.
import grp, os, pwd, re, stat, subprocess, sys, fnmatch

model_path = config_path = None   # set by main()

def die(msg):
    print(f"ERROR: perm-lib: {msg}", file=sys.stderr)
    sys.exit(1)

def load_model(path=None):
    path = path or model_path
    rows = {}
    with open(path) as f:
        for ln, line in enumerate(f, 1):
            line = line.rstrip("\n")
            if not line or line.startswith("#"):
                continue
            cols = line.split("\t")
            if len(cols) != 8:
                die(f"{path}:{ln}: {len(cols)} columns, want 8")
            if cols[0] == "area":
                continue
            rows[cols[0]] = dict(zip(("area", "owner", "group", "dir", "file", "umask", "writers", "what"), cols))
    return rows

def load_config():
    cfg = {}
    with open(config_path) as f:
        for line in f:
            line = line.split("#", 1)[0].strip()
            m = re.match(r"^([A-Za-z_][A-Za-z0-9_]*)=(.*)$", line)
            if m:
                cfg[m.group(1)] = m.group(2).strip()
    return cfg

MODEL = {}

def area_row(name):
    if name not in MODEL:
        die(f"unknown area '{name}' (areas: {' '.join(MODEL)})")
    return MODEL[name]

def resolve_principal(val, kind):
    if val == "-":
        return None
    if val == "root":
        return "root"
    cfg = load_config()
    v = cfg.get(val, "")
    if not v or v.startswith("@@"):
        die(f"{config_path}: {val} unset or unsmudged (needed for {kind})")
    return v

# chmod spec -> new mode, GNU chmod semantics for the forms the model uses
WHO = {"u": 0o4700, "g": 0o2070, "o": 0o0007}
def apply_spec(spec, mode, is_dir):
    if re.fullmatch(r"[0-7]{3,4}", spec):
        return int(spec, 8)                    # absolute = exactly this mode
    cur = mode & 0o7777
    for clause in spec.split(","):
        m = re.fullmatch(r"([ugoa]*)([-+=])([rwxXst]*)", clause)
        if not m:
            die(f"bad mode clause '{clause}' in '{spec}'")
        who, op, perms = m.groups()
        if who in ("", "a"):
            who = "ugo"
        bits = 0
        for w in who:
            for p in perms:
                if p == "r": bits |= {"u": 0o400, "g": 0o040, "o": 0o004}[w]
                elif p == "w": bits |= {"u": 0o200, "g": 0o020, "o": 0o002}[w]
                elif p == "x": bits |= {"u": 0o100, "g": 0o010, "o": 0o001}[w]
                elif p == "X":
                    if is_dir or (mode & 0o111):
                        bits |= {"u": 0o100, "g": 0o010, "o": 0o001}[w]
                elif p == "s":
                    bits |= {"u": 0o4000, "g": 0o2000, "o": 0}[w]
                elif p == "t":
                    bits |= 0o1000 if w == "o" else 0
        if op == "+": cur |= bits
        elif op == "-": cur &= ~bits
        else:
            mask = 0
            for w in who: mask |= WHO[w] & 0o777
            cur = (cur & ~mask) | bits
    return cur

_OPT_LONG = {
    "-t": "--top-only",
    "-p": "--prune",
    "-P": "--prune-name",
    "-d": "--maxdepth",
    "-s": "--skip-glob",
    "-o": "--owner",
}

def parse_opts(a):
    o = {"prune": [], "prune_name": [], "top_only": False, "maxdepth": None, "skip_glob": [], "owner": None}
    i = 0
    while i < len(a):
        k = _OPT_LONG.get(a[i], a[i])
        if k == "--top-only":
            o["top_only"] = True; i += 1; continue
        if k in ("--prune", "--prune-name", "--maxdepth", "--skip-glob", "--owner"):
            if i + 1 >= len(a): die(f"{k} needs a value")
            v = a[i + 1]
            if k == "--prune": o["prune"].append(os.path.realpath(v))
            elif k == "--prune-name": o["prune_name"].append(v)
            elif k == "--maxdepth": o["maxdepth"] = int(v)
            elif k == "--skip-glob": o["skip_glob"].append(v)
            else: o["owner"] = v
            i += 2; continue
        die(f"unknown option {k}")
    return o

def walk(root, o):
    """Yield (path, lstat) for root and everything below it, honouring prunes."""
    root = os.path.abspath(root)
    st = os.lstat(root)  # FileNotFoundError is a loud failure
    yield root, st
    if o["top_only"] or not stat.S_ISDIR(st.st_mode):
        return
    errors = []
    def onerror(e): errors.append(e)
    base_depth = root.rstrip("/").count("/")
    for dirpath, dirnames, filenames in os.walk(root, onerror=onerror):
        depth = dirpath.rstrip("/").count("/") - base_depth
        keep = []
        for d in sorted(dirnames):
            p = os.path.join(dirpath, d)
            if d in o["prune_name"] or os.path.realpath(p) in o["prune"]:
                continue
            keep.append(d)
        dirnames[:] = keep
        if o["maxdepth"] is not None and depth >= o["maxdepth"]:
            dirnames[:] = []
            continue
        for n in sorted(keep) + sorted(filenames):
            if n in o["prune_name"]:
                continue
            if any(fnmatch.fnmatch(n, g) for g in o["skip_glob"]):
                continue
            p = os.path.join(dirpath, n)
            yield p, os.lstat(p)
    if errors:
        for e in errors:
            print(f"ERROR: perm-lib: cannot read {e.filename}: {e.strerror}", file=sys.stderr)
        sys.exit(1)

def principals(row, o):
    owner = o["owner"] if o["owner"] else resolve_principal(row["owner"], "owner")
    group = resolve_principal(row["group"], "group")
    uid = pwd.getpwnam(owner).pw_uid if owner else None
    gid = grp.getgrnam(group).gr_gid if group else None
    return uid, gid, owner, group

def wanted_mode(row, st):
    if stat.S_ISDIR(st.st_mode) and row["dir"] != "-":
        return apply_spec(row["dir"], st.st_mode, True)
    if stat.S_ISREG(st.st_mode) and row["file"] != "-":
        return apply_spec(row["file"], st.st_mode, False)
    return None

def shared_inode(st, uid):
    """A regular file with other hard links, owned by someone other than the area's owner. Its
    other links are outside this walk (Gradle hard-links the NDK's libc++_shared.so into
    app/build), so a chown or chmod here would change that file too: it is never changed."""
    return uid is not None and stat.S_ISREG(st.st_mode) and st.st_nlink > 1 and st.st_uid != uid

def shared_unreadable(path):
    return os.geteuid() != 0 and not os.access(path, os.R_OK)

def report_shared(area, kept, unreadable):
    if kept:
        print(f"NOTE: perm-lib: area '{area}': left {len(kept)} hard-linked file(s) owned by "
              f"{', '.join(sorted({name_u(st.st_uid) for _, st in kept}))} unchanged (other links "
              f"outside this tree, e.g. {kept[0][0]})", file=sys.stderr)
    for path, st in unreadable:
        print(f"CANNOT-READ area={area} path={path} got={name_u(st.st_uid)}:{name_g(st.st_gid)} "
              f"{format(st.st_mode & 0o7777, '04o')} links={st.st_nlink}")
    if unreadable:
        die(f"{len(unreadable)} hard-linked file(s) in area '{area}' are not readable by "
            f"{name_u(os.geteuid())}; only their owner can change that, or delete them from this "
            f"tree (the build makes them again)")

def diffs(area, root, o, kept=None):
    row = area_row(area)
    uid, gid, owner, group = principals(row, o)
    for path, st in walk(root, o):
        if shared_inode(st, uid):
            if kept is not None:
                kept.append((path, st))
            continue
        mode = wanted_mode(row, st)
        own = (uid is not None and st.st_uid != uid) or (gid is not None and st.st_gid != gid)
        md = (mode is not None and not stat.S_ISLNK(st.st_mode) and (st.st_mode & 0o7777) != mode)
        if own or md:
            yield path, st, owner, group, mode, own, md

def name_u(uid):
    try: return pwd.getpwuid(uid).pw_name
    except KeyError: return str(uid)
def name_g(gid):
    try: return grp.getgrgid(gid).gr_name
    except KeyError: return str(gid)

def fix_hint(uid):
    if uid == 0:
        return "only root can change it: run ./fix-perms from the orchestration root (it must run as root)"
    return f"only {name_u(uid)} (its owner) can change it: have {name_u(uid)} delete it if it is not needed"

def run(argv, path, st):
    r = subprocess.run(argv)
    if r.returncode != 0:
        me = pwd.getpwuid(os.geteuid()).pw_name
        hint = ""
        if os.geteuid() != 0 and st.st_uid != os.geteuid():
            hint = f"; it is not owned by {me}, so {fix_hint(st.st_uid)}"
        print(f"ERROR: perm-lib: '{' '.join(argv)}' failed (exit {r.returncode}); "
              f"{path} is {name_u(st.st_uid)}:{name_g(st.st_gid)} {format(st.st_mode & 0o7777, '04o')}, "
              f"running as {me}{hint}", file=sys.stderr)
        sys.exit(1)

def cmd_preflight(area, root, *a):
    """List every difference from the model that this user cannot fix (needs another owner, or
    a mode change on a path someone else owns); exit 1 if any. Changes nothing."""
    o = parse_opts(list(a))
    if os.geteuid() == 0:
        return
    me = os.geteuid()
    bad = 0
    root_owned = False
    kept = []
    for path, st, owner, group, mode, own, md in diffs(area, root, o, kept):
        why = []
        if own and ((owner and pwd.getpwnam(owner).pw_uid != st.st_uid) or st.st_uid != me):
            why.append(f"owner should be {owner or name_u(st.st_uid)}:{group or name_g(st.st_gid)}")
        if md and st.st_uid != me:
            why.append(f"mode should be {format(mode, '04o')}")
        if why:
            bad += 1
            root_owned = root_owned or st.st_uid == 0
            print(f"CANNOT-FIX area={area} path={path} got={name_u(st.st_uid)}:{name_g(st.st_gid)} "
                  f"{format(st.st_mode & 0o7777, '04o')} ({'; '.join(why)})")
    report_shared(area, [], [k for k in kept if shared_unreadable(k[0])])
    if bad:
        tail = ", or run ./fix-perms from the orchestration root for the root-owned ones (it must run as root)" if root_owned else ""
        print(f"ERROR: perm-lib: {bad} path(s) in area '{area}' differ from perm-model.tsv and are not "
              f"owned by {name_u(me)}; only their owners can change them: have each owner remove "
              f"the ones that are not needed{tail}",
              file=sys.stderr)
        sys.exit(1)

def cmd_apply(area, root, *a):
    o = parse_opts(list(a))
    # Streamed: a directory is fixed before os.walk lists it, so a pass can descend into a
    # directory whose search bit it is restoring.
    kept = []
    for path, st, owner, group, mode, own, md in diffs(area, root, o, kept):
        if own:
            spec = (owner or "") + (":" + group if group else "")
            run(["chown", "-h", spec, path], path, st)
        if md:
            # Five digits: GNU chmod applies them exactly (with four it keeps a directory's
            # setuid/setgid bits that the model does not list).
            run(["chmod", format(mode, "05o"), path], path, st)
    report_shared(area, kept, [k for k in kept if shared_unreadable(k[0])])

def cmd_verify(area, root, *a):
    o = parse_opts(list(a))
    bad = 0
    kept = []
    for path, st, owner, group, mode, own, md in diffs(area, root, o, kept):
        bad += 1
        got = f"{name_u(st.st_uid)}:{name_g(st.st_gid)} {format(st.st_mode & 0o7777, '04o')}"
        want = f"{owner or '*'}:{group or '*'} {format(mode, '04o') if mode is not None else '*'}"
        print(f"PERM-MISMATCH area={area} path={path} want={want} got={got}")
    if bad:
        report_shared(area, kept, [])
        print(f"ERROR: perm-lib: {bad} path(s) differ from area '{area}' in perm-model.tsv", file=sys.stderr)
        sys.exit(1)

def cmd_umask(area):
    u = area_row(area)["umask"]
    if not re.fullmatch(r"0?[0-7]{3}", u):
        die(f"area '{area}' has no umask (nobody creates files there)")
    print(u)

# POSIX ACL access check (acl(5) "ACCESS CHECK ALGORITHM") for a user with only its primary
# group: owner -> user::, named user:<uid> -> entry & mask, owning or named group matching the
# primary gid -> entry & mask, else other::. Without an ACL this is the plain mode-bit check.
ACL_USER_OBJ, ACL_USER, ACL_GROUP_OBJ, ACL_GROUP, ACL_MASK, ACL_OTHER = 0x01, 0x02, 0x04, 0x08, 0x10, 0x20

def read_acl(path):
    """[(tag, perm, id)] from system.posix_acl_access, or None when the path has no ACL."""
    if "system.posix_acl_access" not in os.listxattr(path, follow_symlinks=False):
        return None
    raw = os.getxattr(path, "system.posix_acl_access", follow_symlinks=False)
    if len(raw) < 4 or int.from_bytes(raw[:4], "little") != 2 or (len(raw) - 4) % 8:
        die(f"{path}: unreadable POSIX ACL xattr ({len(raw)} bytes)")
    out = []
    for i in range(4, len(raw), 8):
        out.append((int.from_bytes(raw[i:i + 2], "little"), int.from_bytes(raw[i + 2:i + 4], "little"),
                    int.from_bytes(raw[i + 4:i + 8], "little")))
    return out

def effective_perm(path, st, uid, gid):
    """(class, rwx bits 0..7) that uid with only primary group gid gets on path."""
    m = st.st_mode
    acl = read_acl(path)
    if acl is None:
        if st.st_uid == uid: return "owner", (m >> 6) & 7
        if st.st_gid == gid: return "primary-group", (m >> 3) & 7
        return "other", m & 7
    ent = {}
    for tag, perm, i in acl:
        ent.setdefault(tag, {})[i] = perm
    if st.st_uid == uid:
        return "owner", (m >> 6) & 7
    mask = next(iter(ent.get(ACL_MASK, {0: 7}).values()))
    if uid in ent.get(ACL_USER, {}):
        return "acl-user", ent[ACL_USER][uid] & mask
    if st.st_gid == gid:
        return "acl-group", next(iter(ent[ACL_GROUP_OBJ].values())) & mask
    if gid in ent.get(ACL_GROUP, {}):
        return "acl-named-group", ent[ACL_GROUP][gid] & mask
    return "other", m & 7

def cmd_readable_nogroups(user, *paths):
    if not paths: die("readable-nogroups needs at least one path")
    pw = pwd.getpwnam(user)
    bad = 0
    for p in paths:
        p = os.path.abspath(p)
        parts = p.split("/")
        chain = ["/"] + ["/".join(parts[:i]) for i in range(2, len(parts) + 1)]
        for i, c in enumerate(chain):
            last = (i == len(chain) - 1)
            st = os.lstat(c)
            if stat.S_ISLNK(st.st_mode):
                print(f"NOT-READABLE user={user} path={p} blocker={c} reason=symlink-in-path"); bad += 1; break
            need = 0o4 if last else 0o1   # read the file, search each directory
            if last and stat.S_ISDIR(st.st_mode):
                need = 0o5
            if pw.pw_uid == 0:
                continue
            cls, perm = effective_perm(c, st, pw.pw_uid, pw.pw_gid)
            if (perm & need) != need:
                print(f"NOT-READABLE user={user} path={p} blocker={c} class={cls} mode={format(st.st_mode & 0o7777, '04o')} owner={name_u(st.st_uid)}:{name_g(st.st_gid)}")
                bad += 1
                break
    if bad:
        print(f"ERROR: perm-lib: {bad} path(s) not readable by {user} without supplementary groups", file=sys.stderr)
        sys.exit(1)

def main(argv):
    global model_path, config_path, MODEL
    usage_line = "usage: perm-engine.py [-h] MODEL CONFIG COMMAND [ARGS] [options]"
    if argv and argv[0] in ("-h", "--help"):
        print(f"""{usage_line}

The engine behind perm-lib.sh: owners, groups and modes from MODEL (perm-model.tsv).
COMMAND is apply|verify|preflight AREA ROOT, umask AREA, or readable-nogroups USER PATH...

options:
  -h, --help             show this help message and exit
  -t, --top-only         ROOT itself, not below it
  -p, --prune PATH       skip PATH (repeatable)
  -P, --prune-name NAME  skip every entry named NAME (repeatable)
  -d, --maxdepth N       descend at most N levels
  -s, --skip-glob GLOB   skip paths matching GLOB (repeatable)
  -o, --owner USER       owner to set instead of the model's owner column

Details: the comment at the top of perm-engine.py, and perm-lib.sh.""")
        return 0
    if argv and argv[0].startswith("-"):
        print(usage_line, file=sys.stderr)
        print(f"perm-engine.py: error: unrecognized option: {argv[0]}", file=sys.stderr)
        return 2
    if len(argv) < 3:
        die("usage: perm-engine.py MODEL CONFIG COMMAND [ARGS]")
    model_path, config_path, cmd, *args = argv
    MODEL = load_model()
    cmds = {"apply": cmd_apply, "verify": cmd_verify, "preflight": cmd_preflight, "umask": cmd_umask, "readable-nogroups": cmd_readable_nogroups}
    if cmd not in cmds:
        die(f"unknown command {cmd}")
    try:
        cmds[cmd](*args)
    except TypeError as e:
        die(f"{cmd}: bad arguments ({e})")
    except (FileNotFoundError, PermissionError) as e:
        die(f"{e.filename}: {e.strerror}")
    except KeyError as e:
        die(f"unknown user or group {e}")

if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]) or 0)
