#!/usr/bin/env python3
"""provision.py — build a self-contained acceptance case from a source tree.

Two modes:

  --mount <tree>  arc FUSE working copy: STRACE the upstream `ya make -G` run,
                  copy a minimal slice (reads + listed dirs + stat-only + build/
                  + root markers), and verify ya reproduces the same graph off
                  the slice. The slice does not depend on the FUSE mount.

  --url <zip>     github archive .zip: no slicing — download + store the whole
                  zip, and build the reference graph by running ya on it.

Both then `ya upload` the slice/zip + reference graph (ttl inf) and append a
dev/config.json entry (pass --no-upload to stop after verify).

  1. resolve the tree's commit sha,
  2. run upstream `ya make -G <CMD>` under strace, capturing the reference graph
     AND every path the build read / listed,
  3. copy a minimal slice (read files = real content; listed-dir entries that
     were not read = empty placeholders; symlinks recreated; root markers
     ya/ya.conf/.arcadia.root always copied),
  4. re-run `ya make -G <CMD>` against the slice and require the graph to be
     byte-identical to the FUSE run — a hole in the slice fails here,
  5. (later, run by the operator with their creds) zstd + `ya upload` the slice
     and graph, append a config.json entry.

The slice is validated by ya itself (step 4), so it never depends on ay.

Usage:
    dev/provision.py --mount <tree> --workspace <dir> -- ya make <flags> <target>

Phases write intermediate artifacts under <workspace> and are skippable with
--reuse so the strace parse / slice build can be iterated without re-running ya:

    <workspace>/trace            strace -f -y output of the FUSE run
    <workspace>/graph.fuse.json  reference graph from the FUSE run
    <workspace>/slice/           the sliced tree
    <workspace>/graph.slice.json reference graph rebuilt from the slice
    <workspace>/meta.json        {sha, command, target}

Env:
    YA — ya launcher to invoke (default: ya on PATH, the thin wrapper that jumps
         into the tree's repo ya). The SAME launcher is used for both runs.
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys

ROOT_MARKERS = (".arcadia.root", "ya", "ya.conf")
# ymake's config tree: small, always needed, and partly only stat'd (lint
# configs, plugins). Copy it whole rather than chase individual touches.
ALWAYS_COPY_DIRS = ("build",)

# strace -y annotates fds with their resolved path: `= 5</abs/path>`, and decodes
# dir fds inline: `getdents64(5</abs/dir>, ...)`. These regexes lift those. We
# must cover open/openat/openat2 — ymake opens sources via openat2, so tracing
# only openat misses every source read.
RE_OPEN = re.compile(r"open(?:at2?)?(?:\s+resumed>|\().*?\)\s*=\s*\d+<(?P<path>[^>]*)>")
RE_GETDENTS = re.compile(r"getdents64\(\d+<(?P<path>[^>]*)>")
RE_READLINK = re.compile(r'readlink\("(?P<link>[^"]*)"(?:<[^>]*>)?,\s*"(?P<target>[^"]*)"')
# stat-family existence checks: ymake validates that some files (e.g. LINT-CONFIGS
# .clang-format / flake8.conf) exist without opening them, so they never show up
# as reads — but their absence fails the slice. Capture successful AT_FDCWD stats.
RE_STAT = re.compile(
    r'(?:newfstatat|statx|lstat|stat|faccessat2?|access)\('
    r'(?:AT_FDCWD(?:<[^>]*>)?,\s*)?"(?P<path>[^"]*)".*=\s*0(?:\s|$)')


def run(cmd, **kw):
    return subprocess.run(cmd, **kw)


def resolve_sha(mount):
    """Commit sha of the tree: git for a checkout, arc for a live mount."""
    if os.path.isdir(os.path.join(mount, ".git")):
        return run(["git", "-C", mount, "rev-parse", "HEAD"],
                   capture_output=True, text=True, check=True).stdout.strip()

    for args in (["rev-parse", "HEAD"], ["log", "-n1", "--format=%H"]):
        r = run(["arc"] + args, cwd=mount, capture_output=True, text=True)
        if r.returncode == 0 and r.stdout.strip():
            return r.stdout.strip().split()[0]

    raise SystemExit(f"provision: cannot resolve sha of {mount} (not git, arc failed)")


def _inject_after_make(toks, flag):
    i = toks.index("make") + 1
    return toks[:i] + [flag] + toks[i:]


def split_command(argv):
    """Normalize the `ya make ...` tokens: ensure -G and -j0 (graph-only, no
    build); the target is the last token. -xx is added only for the provision
    runs (run_command), never stored in config — without it `ya make` can hand
    back a cached graph, which would make the slice verify pass falsely since
    both runs share ~/.ya."""
    if "make" not in argv:
        raise SystemExit("provision: command must be a `ya make ...` invocation")

    toks = list(argv)

    if "-G" not in toks:
        toks = _inject_after_make(toks, "-G")

    if not any(t == "-j" or t.startswith("-j") for t in toks):
        toks = _inject_after_make(toks, "-j0")

    target = toks[-1]

    if target.startswith("-"):
        raise SystemExit("provision: last command token must be the build target")

    return toks, target


def run_command(cmd_cfg):
    """The provision-run form: cmd_cfg + -xx (force a fresh graph, not cached)."""
    if "-xx" in cmd_cfg:
        return cmd_cfg

    return _inject_after_make(cmd_cfg, "-xx")


def resolve_strace(mount, override):
    """A strace new enough to know openat2. The strace on PATH (under the
    /ix env) is too old and rejects `-e trace=openat2`; ya ships a recent one,
    `ya tool strace`, whose path leaks in its no-arg usage error."""
    if override:
        return override

    r = run(["ya", "tool", "strace"], cwd=mount, capture_output=True, text=True)
    m = re.search(r"(\S+/strace):", r.stdout + r.stderr)

    if m and os.path.exists(m.group(1)):
        return m.group(1)

    return "strace"


def repo_ya(root, tokens):
    """Run the repo's own ./ya launcher (pinned to this sha's ymake), not the
    system ya — a version skew breaks config. Replaces argv[0] and ensures the
    launcher is executable (zip extraction drops the +x bit)."""
    ya = os.path.join(root, "ya")
    if not os.path.isfile(ya):
        return list(tokens)
    os.chmod(ya, 0o755)
    return [ya] + list(tokens[1:])


def ya_env():
    """Env for every ya invocation here. YA_TC=no keeps ya from spawning the
    persistent tools-cache daemon — under `strace -f` that daemon never exits,
    so strace would hang forever after ymake itself finished. YA_NO_RESPAWN
    stops the launcher re-exec'ing itself. -xx (added to run_command) bypasses
    the graph cache, so reads stay complete without a cold cache dir."""
    return dict(os.environ, YA_TC="no", YA_NO_RESPAWN="yes")


def strace_run(mount, ya_cmd, trace_path, graph_path, strace_bin):
    """Run `ya make -G ...` from the mount under strace; graph -> graph_path."""
    strace = [
        strace_bin, "-f", "-y", "-qq", "-s", "4096",
        # %file = every path-taking syscall (open/openat/openat2/stat/...), so we
        # never guess which open variant ymake uses; getdents64 is fd-based.
        "-e", "trace=%file,getdents64",
        "-o", trace_path,
    ]

    with open(graph_path, "wb") as gf:
        r = run(strace + repo_ya(mount, ya_cmd), cwd=mount, env=ya_env(), stdout=gf)

    if r.returncode != 0:
        raise SystemExit(f"provision: FUSE ya run failed (rc={r.returncode}); see {trace_path}")


def parse_trace(trace_path, mount):
    """Return (reads, listed_dirs, exists, symlinks{link:target}) mount-relative."""
    reads, listed, exists, links = set(), set(), set(), {}
    prefix = mount.rstrip("/") + "/"

    def rel(p):
        return p[len(prefix):] if p.startswith(prefix) else None

    with open(trace_path, encoding="utf-8", errors="replace") as f:
        for line in f:
            m = RE_OPEN.search(line)
            if m and (r := rel(m["path"])):
                reads.add(r)
                continue

            m = RE_GETDENTS.search(line)
            if m and (r := rel(m["path"])) is not None:
                listed.add(r)
                continue

            m = RE_READLINK.search(line)
            if m and (r := rel(m["link"])) is not None:
                links[r] = m["target"]
                continue

            m = RE_STAT.search(line)
            if m and (r := rel(m["path"])):
                exists.add(r)

    return reads, listed, exists, links


def copy_real(mount, slice_dir, rel):
    src = os.path.join(mount, rel)
    if not os.path.isfile(src) or os.path.islink(src):
        return
    dst = os.path.join(slice_dir, rel)
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    shutil.copyfile(src, dst)


def _progress(label, i, total, every=2000):
    if i == total or i % every == 0:
        print(f"[provision]   {label} {i}/{total}", flush=True)


def mirror_entry(mount, slice_dir, rel):
    """Reproduce rel's presence (not content) in the slice, type-matched."""
    sp = os.path.join(mount, rel)
    dst = os.path.join(slice_dir, rel)
    if os.path.lexists(dst) or os.path.islink(sp):
        return
    if os.path.isdir(sp):
        os.makedirs(dst, exist_ok=True)
    elif os.path.isfile(sp):
        os.makedirs(os.path.dirname(dst) or slice_dir, exist_ok=True)
        open(dst, "wb").close()


def build_slice(mount, slice_dir, reads, listed, exists, links):
    if os.path.exists(slice_dir):
        print(f"[provision]   clearing old slice {slice_dir}", flush=True)
        shutil.rmtree(slice_dir)
    os.makedirs(slice_dir, exist_ok=True)

    for rel in ROOT_MARKERS:
        if os.path.exists(os.path.join(mount, rel)):
            copy_real(mount, slice_dir, rel)
    print(f"[provision]   root markers copied", flush=True)

    for d in ALWAYS_COPY_DIRS:
        src = os.path.join(mount, d)
        if os.path.isdir(src):
            shutil.copytree(src, os.path.join(slice_dir, d), symlinks=True, dirs_exist_ok=True)
            print(f"[provision]   copied {d}/ wholesale", flush=True)

    for i, rel in enumerate(sorted(reads), 1):
        copy_real(mount, slice_dir, rel)
        _progress("copy reads", i, len(reads))

    # listed dirs: reproduce the name set so ymake's dir scans see the same
    # entries; entries not already real-copied become empty placeholders.
    placeholders = 0
    for i, d in enumerate(sorted(listed), 1):
        src_dir = os.path.join(mount, d)
        if not os.path.isdir(src_dir):
            continue
        dst_dir = os.path.join(slice_dir, d)
        os.makedirs(dst_dir, exist_ok=True)
        for name in os.listdir(src_dir):
            dst = os.path.join(dst_dir, name)
            if os.path.exists(dst) or os.path.islink(dst):
                continue
            sp = os.path.join(src_dir, name)
            if os.path.islink(sp):
                continue
            if os.path.isdir(sp):
                os.makedirs(dst, exist_ok=True)
            else:
                open(dst, "wb").close()
                placeholders += 1
        _progress("expand listed dirs", i, len(listed), every=1000)
    print(f"[provision]   placeholders={placeholders}", flush=True)

    for rel in exists:
        mirror_entry(mount, slice_dir, rel)
    print(f"[provision]   stat-only entries={len(exists)}", flush=True)

    made = 0
    for link, target in links.items():
        if not target or link == ".arc" or link.startswith(".arc/"):
            continue
        dst = os.path.join(slice_dir, link)
        os.makedirs(os.path.dirname(dst) or slice_dir, exist_ok=True)
        if not os.path.lexists(dst):
            os.symlink(target, dst)
            made += 1
    print(f"[provision]   symlinks={made}", flush=True)


def graph_core(path):
    """The semantic part of a ya -G dump: nodes + results, minus the volatile
    `conf` header (gsid is a random per-run id, host/arc-version differ on the
    arc-less slice) which must NOT count as a slice hole."""
    with open(path, encoding="utf-8") as f:
        obj = json.load(f)
    return obj.get("graph"), obj.get("result")


def verify_slice(slice_dir, ya_cmd, graph_slice, graph_fuse):
    with open(graph_slice, "wb") as gf:
        r = run(repo_ya(slice_dir, ya_cmd), cwd=slice_dir, env=ya_env(), stdout=gf)

    if r.returncode != 0:
        raise SystemExit(f"provision: slice ya run failed (rc={r.returncode}) — slice incomplete")

    fuse_g, fuse_r = graph_core(graph_fuse)
    slice_g, slice_r = graph_core(graph_slice)

    if fuse_g == slice_g and fuse_r == slice_r:
        return

    msg = [f"provision: slice graph != FUSE graph (nodes fuse={len(fuse_g or [])} slice={len(slice_g or [])})"]

    fg = {n.get("uid"): n for n in (fuse_g or [])}
    sg = {n.get("uid"): n for n in (slice_g or [])}
    only_fuse = sorted(set(fg) - set(sg))
    only_slice = sorted(set(sg) - set(fg))

    if only_fuse:
        msg.append(f"  nodes only in FUSE ({len(only_fuse)}): e.g. {fuse_node_label(fg[only_fuse[0]])}")
    if only_slice:
        msg.append(f"  nodes only in SLICE ({len(only_slice)}): e.g. {fuse_node_label(sg[only_slice[0]])}")
    if not only_fuse and not only_slice:
        msg.append("  same uid set; content/order differs — see graph.*.json")

    raise SystemExit("\n".join(msg))


def fuse_node_label(node):
    outs = node.get("outputs") or []
    return outs[0] if outs else node.get("uid", "?")


def find_url(obj):
    found = []

    def walk(o):
        if isinstance(o, dict):
            for v in o.values():
                walk(v)
        elif isinstance(o, list):
            for v in o:
                walk(v)
        elif isinstance(o, str) and o.startswith("http"):
            found.append(o)

    walk(obj)

    for pref in ("proxy", "mds", "storage"):
        for u in found:
            if pref in u:
                return u

    return found[0] if found else None


def ya_upload(path, tar, owner, descr):
    """Upload one artifact eternally (ttl inf); return its http link. tar=True
    tars+zstd a directory; tar=False uploads a single file as-is (e.g. a zip)."""
    cmd = ["ya", "upload", path, "--ttl", "inf",
           "--owner", owner, "-d", descr, "--json-output"]
    if tar:
        cmd[3:3] = ["--tar", "--zstd"]

    print(f"[provision]   ya upload {os.path.basename(path)} ...", flush=True)
    r = run(cmd, env=ya_env(), capture_output=True, text=True)

    if r.returncode != 0:
        raise SystemExit(f"provision: ya upload {path} failed:\n{(r.stderr or r.stdout)[-2000:]}")

    out = r.stdout.strip()

    try:
        url = find_url(json.loads(out)) if out else None
    except json.JSONDecodeError:
        url = None

    if not url:
        m = re.search(r"https?://\S+", out + "\n" + r.stderr)
        url = m.group(0).rstrip(".,)") if m else None

    if not url:
        raise SystemExit(f"provision: cannot parse upload URL:\n{(out + r.stderr)[-2000:]}")

    return url


def load_config(config_path):
    if not os.path.exists(config_path):
        return []
    with open(config_path) as f:
        return json.load(f)


def case_key(e):
    return (e.get("remote"), e.get("sha"), tuple(e.get("command", [])))


def append_config(config_path, entry):
    data = [e for e in load_config(config_path) if case_key(e) != case_key(entry)]

    ids = {e["id"] for e in data}
    base, i = entry["id"], 2
    while entry["id"] in ids:
        entry["id"] = f"{base}-{i}"
        i += 1

    data.append(entry)

    with open(config_path, "w") as f:
        json.dump(data, f, indent=2)
        f.write("\n")


def find_arcadia_root(d):
    if os.path.exists(os.path.join(d, ".arcadia.root")):
        return d
    for root, _dirs, files in os.walk(d):
        if ".arcadia.root" in files:
            return root
    raise SystemExit(f"provision: no .arcadia.root under {d}")


def sha_from_url(url, fallback_dir):
    m = re.search(r"/archive/(?:refs/[^/]+/)?(?P<sha>[0-9a-fA-F]{7,40})\.zip", url)
    if m:
        return m.group("sha")
    return os.path.basename(fallback_dir).rsplit("-", 1)[-1]


def download(url, dst):
    print(f"[provision]   download {url}", flush=True)
    r = run(["curl", "-fSL", "-o", dst, url])
    if r.returncode != 0:
        raise SystemExit(f"provision: download failed (rc={r.returncode}): {url}")


def extract_zip(zip_path, dst_dir):
    import zipfile
    if os.path.exists(dst_dir):
        shutil.rmtree(dst_dir)
    os.makedirs(dst_dir, exist_ok=True)
    with zipfile.ZipFile(zip_path) as z:
        z.extractall(dst_dir)
    return find_arcadia_root(dst_dir)


def detect_vcs(mount):
    return "git" if os.path.isdir(os.path.join(mount, ".git")) else "arc"


def resolve_remote(mount, vcs, override):
    if override:
        return override
    if vcs == "git":
        r = run(["git", "-C", mount, "remote", "get-url", "origin"], capture_output=True, text=True)
        if r.returncode == 0:
            return r.stdout.strip()
    return "arcadia"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--mount", default=None, help="arc/git working tree (slicing mode)")
    ap.add_argument("--url", default=None, help="github archive .zip (no-slice mode: store the whole zip)")
    ap.add_argument("--workspace", required=True)
    ap.add_argument("--id", default=None)
    ap.add_argument("--strace", default=os.environ.get("STRACE"),
                    help="strace binary (default: ya tool strace)")
    ap.add_argument("--reuse", action="store_true",
                    help="skip phases whose artifacts already exist")
    ap.add_argument("--no-upload", action="store_true",
                    help="stop after verify; do not ya upload or write config")
    ap.add_argument("--config", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "config.json"))
    ap.add_argument("--remote", default=None, help="provenance remote (default: git origin / arcadia)")
    ap.add_argument("--xfail", default="auto", help="gating mode for this case (false|true|auto)")
    ap.usage = "provision.py (--mount TREE | --url ZIP) --workspace W [opts] -- ya make <flags> <target>"

    # Split on the first `--` ourselves; argparse REMAINDER mishandles `--`
    # once other optionals are present.
    argv = sys.argv[1:]
    if "--" in argv:
        i = argv.index("--")
        pre, cmd = argv[:i], argv[i + 1:]
    else:
        pre, cmd = argv, []

    args = ap.parse_args(pre)

    if not cmd:
        raise SystemExit("provision: missing `-- ya make <flags> <target>`")

    if bool(args.mount) == bool(args.url):
        raise SystemExit("provision: pass exactly one of --mount or --url")

    ws = os.path.abspath(args.workspace)
    os.makedirs(ws, exist_ok=True)

    ya_cmd, target = split_command(cmd)
    ya_run = run_command(ya_cmd)
    case_id = args.id or target.replace("/", "_")
    graph_fuse = os.path.join(ws, "graph.fuse.json")
    owner = os.environ.get("USER") or "ay"

    if args.url:
        # git: no slicing — store the whole archive, build the ref graph off it.
        vcs, remote = "git", (args.remote or args.url)
        zip_path = os.path.join(ws, "repo.zip")
        repo_dir = os.path.join(ws, "repo")

        # same url already uploaded? reuse its repo blob link, rebuild only the graph.
        reuse_slice = next((e.get("slice_url") for e in load_config(args.config)
                            if e.get("vcs") == "git" and e.get("remote") == remote and e.get("slice_url")), None)

        root = find_arcadia_root(repo_dir) if (args.reuse and os.path.isdir(repo_dir)) else None
        if root is None:
            download(args.url, zip_path)
            root = extract_zip(zip_path, repo_dir)
        sha = sha_from_url(args.url, root)

        print(f"[provision] id={case_id} sha={sha} target={target} (git, no slice)", flush=True)
        if reuse_slice:
            print(f"[provision] reusing repo blob {reuse_slice}", flush=True)
        print(f"[provision] run cmd: {' '.join(ya_run)}  (root {root})", flush=True)
        print("[provision] build ref graph", flush=True)
        with open(graph_fuse, "wb") as gf:
            rc = run(repo_ya(root, ya_run), cwd=root, env=ya_env(), stdout=gf).returncode
        if rc != 0:
            raise SystemExit(f"provision: ya graph build failed (rc={rc})")

        slice_artifact, slice_tar, slice_url = zip_path, False, reuse_slice
    else:
        mount = os.path.abspath(args.mount)
        vcs, remote = detect_vcs(mount), resolve_remote(mount, detect_vcs(mount), args.remote)
        sha = resolve_sha(mount)
        trace = os.path.join(ws, "trace")
        slice_dir = os.path.join(ws, "slice")
        graph_slice = os.path.join(ws, "graph.slice.json")

        print(f"[provision] id={case_id} sha={sha} target={target}", flush=True)
        print(f"[provision] config cmd: {' '.join(ya_cmd)}", flush=True)
        print(f"[provision] run cmd:    {' '.join(ya_run)}", flush=True)

        if not (args.reuse and os.path.exists(trace) and os.path.exists(graph_fuse)):
            strace_bin = resolve_strace(mount, args.strace)
            print(f"[provision] phase 1: strace FUSE run ({strace_bin})", flush=True)
            strace_run(mount, ya_run, trace, graph_fuse, strace_bin)

        print("[provision] phase 2: parse trace", flush=True)
        reads, listed, exists, links = parse_trace(trace, mount)
        print(f"[provision]   reads={len(reads)} listed_dirs={len(listed)} stat_only={len(exists)} symlinks={len(links)}", flush=True)

        print("[provision] phase 3: build slice", flush=True)
        build_slice(mount, slice_dir, reads, listed, exists, links)
        nfiles = sum(len(fs) for _, _, fs in os.walk(slice_dir))
        print(f"[provision]   slice files={nfiles}", flush=True)

        print("[provision] phase 4: verify (ya on slice == FUSE)", flush=True)
        verify_slice(slice_dir, ya_run, graph_slice, graph_fuse)
        print("[provision]   slice graph byte-identical to FUSE graph ✓", flush=True)

        slice_artifact, slice_tar, slice_url = slice_dir, True, None

    entry = {
        "id": case_id,
        "vcs": vcs,
        "remote": remote,
        "sha": sha,
        "command": ya_cmd,
        "target": target,
        "xfail": args.xfail,
    }

    with open(os.path.join(ws, "meta.json"), "w") as f:
        json.dump(entry, f, indent=2)

    if args.no_upload:
        print(f"[provision] OK — artifact at {slice_artifact} (--no-upload: skipped upload + config)", flush=True)
        return 0

    print("[provision] phase 6: upload + config", flush=True)
    entry["slice_url"] = slice_url or ya_upload(slice_artifact, slice_tar, owner, f"ay acceptance slice {case_id} @ {sha}")
    entry["graph_url"] = ya_upload(graph_fuse, True, owner, f"ay acceptance ref graph {case_id} @ {sha}")
    append_config(args.config, entry)

    print(f"[provision]   slice_url: {entry['slice_url']}", flush=True)
    print(f"[provision]   graph_url: {entry['graph_url']}", flush=True)
    print(f"[provision] OK — config updated: {args.config}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
