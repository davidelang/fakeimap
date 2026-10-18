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

def parse_opts(a):
    o = {"prune": [], "prune_name": [], "top_only": False, "maxdepth": None, "skip_glob": [], "owner": None}
    i = 0
    while i < len(a):
        k = a[i]
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

def diffs(area, root, o):
    row = area_row(area)
    uid, gid, owner, group = principals(row, o)
    for path, st in walk(root, o):
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

def run(argv, path, st):
    r = subprocess.run(argv)
    if r.returncode != 0:
        me = pwd.getpwuid(os.geteuid()).pw_name
        print(f"ERROR: perm-lib: '{' '.join(argv)}' failed (exit {r.returncode}); "
              f"{path} is {name_u(st.st_uid)}:{name_g(st.st_gid)} {format(st.st_mode & 0o7777, '04o')}, "
              f"running as {me}", file=sys.stderr)
        sys.exit(1)

def cmd_apply(area, root, *a):
    o = parse_opts(list(a))
    # Streamed: a directory is fixed before os.walk lists it, so a pass can descend into a
    # directory whose search bit it is restoring.
    for path, st, owner, group, mode, own, md in diffs(area, root, o):
        if own:
            spec = (owner or "") + (":" + group if group else "")
            run(["chown", "-h", spec, path], path, st)
        if md:
            # Five digits: GNU chmod applies them exactly (with four it keeps a directory's
            # setuid/setgid bits that the model does not list).
            run(["chmod", format(mode, "05o"), path], path, st)

def cmd_verify(area, root, *a):
    o = parse_opts(list(a))
    bad = 0
    for path, st, owner, group, mode, own, md in diffs(area, root, o):
        bad += 1
        got = f"{name_u(st.st_uid)}:{name_g(st.st_gid)} {format(st.st_mode & 0o7777, '04o')}"
        want = f"{owner or '*'}:{group or '*'} {format(mode, '04o') if mode is not None else '*'}"
        print(f"PERM-MISMATCH area={area} path={path} want={want} got={got}")
    if bad:
        print(f"ERROR: perm-lib: {bad} path(s) differ from area '{area}' in perm-model.tsv", file=sys.stderr)
        sys.exit(1)

def cmd_umask(area):
    u = area_row(area)["umask"]
    if not re.fullmatch(r"0?[0-7]{3}", u):
        die(f"area '{area}' has no umask (nobody creates files there)")
    print(u)

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
            if "system.posix_acl_access" in os.listxattr(c, follow_symlinks=False):
                print(f"NOT-READABLE user={user} path={p} blocker={c} reason=has-acl (model has no ACLs)"); bad += 1; break
            need = 0o4 if last else 0o1   # read the file, search each directory
            if last and stat.S_ISDIR(st.st_mode):
                need = 0o5
            if pw.pw_uid == 0:
                continue
            if st.st_uid == pw.pw_uid: cls, sh = "owner", 6
            elif st.st_gid == pw.pw_gid: cls, sh = "primary-group", 3
            else: cls, sh = "other", 0
            if ((st.st_mode >> sh) & need) != need:
                print(f"NOT-READABLE user={user} path={p} blocker={c} class={cls} mode={format(st.st_mode & 0o7777, '04o')} owner={name_u(st.st_uid)}:{name_g(st.st_gid)}")
                bad += 1
                break
    if bad:
        print(f"ERROR: perm-lib: {bad} path(s) not readable by {user} without supplementary groups", file=sys.stderr)
        sys.exit(1)

def main(argv):
    global model_path, config_path, MODEL
    if len(argv) < 3:
        die("usage: perm-engine.py MODEL CONFIG COMMAND [ARGS]")
    model_path, config_path, cmd, *args = argv
    MODEL = load_model()
    cmds = {"apply": cmd_apply, "verify": cmd_verify, "umask": cmd_umask, "readable-nogroups": cmd_readable_nogroups}
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
    main(sys.argv[1:])
