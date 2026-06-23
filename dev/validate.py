#!/usr/bin/env python3
"""validate.py — config-driven byte-exact acceptance gate (parallel DAG).

Reads dev/config.json (one entry per case, produced by dev/provision.py), and
for each case:

  * fetch the source slice and the raw upstream reference graph from Sandbox
    (authenticated `ay fetch sandbox`, cached under .out/acceptance/cache so a
    ~200MB slice downloads once),
  * generate our graph with `ay make -G ... --source-root <slice>`,
  * normalize+sort both ours and the reference, then byte-compare (gating cases)
    or count normalized-node parity + write the diff (xfail cases).

Each case entry:
    {id, command:[ya,make,...,target], target, slice_url, graph_url, xfail}
xfail: false = gating (byte-compare); true = xfail (parity only); "auto" = gate
when byte-exact else xfail (self-promoting).

All work is a DAG of nodes (one per program invocation) joined on shared files
and run up to VALIDATE_JOBS concurrently. Fetch nodes are cached; everything
else always runs.

Usage: validate.py [out-dir]   (default: <repo>/.out/validate)
Env:   VALIDATE_JOBS — max concurrent nodes (default: cpu count).
"""
import json
import os
import shutil
import subprocess
import sys
import threading
import time
from dataclasses import dataclass

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(SCRIPT_DIR)
GO = "go"
CONFIG_PATH = os.path.join(SCRIPT_DIR, "config.json")
CACHE_DIR = os.path.join(REPO_ROOT, ".out", "acceptance", "cache")

# AY (the built binary) and WORK_CWD are repointed at the writable out-dir in
# main(), so REPO_ROOT may be read-only.
AY = os.path.join(REPO_ROOT, "ay")
WORK_CWD = REPO_ROOT

GEN_TIME_SLACK = 1.2


@dataclass(frozen=True)
class ParityCounts:
    exact: int
    left_only: int
    right_only: int
    left_total: int
    right_total: int


def _remove_if_exists(path):
    try:
        os.remove(path)
    except FileNotFoundError:
        pass


def _advance_line(handle):
    line = handle.readline()
    if line == "":
        return None
    return line


def normalized_node_parity_counts(left_path, right_path):
    """Count exact normalized-node matches between two sorted JSONL files."""
    exact = left_only = right_only = left_total = right_total = 0
    with open(left_path, encoding="utf-8") as left, open(right_path, encoding="utf-8") as right:
        left_line = _advance_line(left)
        right_line = _advance_line(right)
        if left_line is not None:
            left_total += 1
        if right_line is not None:
            right_total += 1

        while left_line is not None and right_line is not None:
            if left_line == right_line:
                exact += 1
                left_line = _advance_line(left)
                right_line = _advance_line(right)
                if left_line is not None:
                    left_total += 1
                if right_line is not None:
                    right_total += 1
                continue
            if left_line < right_line:
                left_only += 1
                left_line = _advance_line(left)
                if left_line is not None:
                    left_total += 1
                continue
            right_only += 1
            right_line = _advance_line(right)
            if right_line is not None:
                right_total += 1

        while left_line is not None:
            left_only += 1
            left_line = _advance_line(left)
            if left_line is not None:
                left_total += 1

        while right_line is not None:
            right_only += 1
            right_line = _advance_line(right)
            if right_line is not None:
                right_total += 1

    return ParityCounts(exact, left_only, right_only, left_total, right_total)


# ---------------------------------------------------------------------------
# DAG micro-executor
# ---------------------------------------------------------------------------


@dataclass
class Node:
    name: str
    action: object
    inputs: list
    outputs: list


def run_graph(nodes, jobs):
    """Run the node DAG, joining each node on the producers of its input files,
    up to `jobs` concurrently. A node whose any dependency failed is skipped
    (marked failed, propagating). Returns {name: result}."""
    by_out = {o: n.name for n in nodes for o in n.outputs}
    deps = {n.name: {by_out[i] for i in n.inputs if i in by_out} for n in nodes}
    dependents = {n.name: [] for n in nodes}

    for n in nodes:
        for d in deps[n.name]:
            dependents[d].append(n.name)

    node_by_name = {n.name: n for n in nodes}
    results = {}
    failed = set()
    remaining = {n.name: len(deps[n.name]) for n in nodes}
    left = [len(nodes)]
    lock = threading.Lock()
    sem = threading.Semaphore(jobs)
    done = threading.Event()

    def complete(name, result):
        ready = []

        with lock:
            results[name] = result

            if not result.get("ok"):
                failed.add(name)

            for dep in dependents[name]:
                remaining[dep] -= 1

                if remaining[dep] == 0:
                    ready.append(dep)

            left[0] -= 1

            if left[0] == 0:
                done.set()

        for dep in ready:
            dispatch(dep)

    def dispatch(name):
        with lock:
            blocked = bool(deps[name] & failed)

        if blocked:
            complete(name, {"ok": False, "skipped": True})

            return

        def task():
            sem.acquire()

            try:
                res = node_by_name[name].action()
            except Exception as e:  # noqa: BLE001 — surface, don't crash the run
                res = {"ok": False, "error": str(e)}
            finally:
                sem.release()

            complete(name, res)

        threading.Thread(target=task, daemon=True).start()

    for n in nodes:
        if remaining[n.name] == 0:
            dispatch(n.name)

    done.wait()

    return results


def sh(cmd, gogc=False, stdout=None):
    """Run a subprocess in WORK_CWD; return its exit code. gogc=True passes
    GOGC=800 (the heavy ay commands spend ~half their CPU on GC at default)."""
    env = dict(os.environ)
    if gogc:
        env["GOGC"] = "800"

    if stdout is not None:
        with open(stdout, "wb") as f:
            return subprocess.run(cmd, cwd=WORK_CWD, env=env, stdout=f).returncode

    return subprocess.run(cmd, cwd=WORK_CWD, env=env).returncode


# ---------------------------------------------------------------------------
# Config + cache
# ---------------------------------------------------------------------------


def load_config():
    if not os.path.exists(CONFIG_PATH):
        return []
    with open(CONFIG_PATH) as f:
        return json.load(f)


def resource_id(url):
    return url.rstrip("/").rsplit("/", 1)[-1]


def resolve_slice_root(slice_dir):
    """The arcadia root inside the unpacked slice — `ya upload --tar` nests the
    tree under the uploaded dir's basename, so the root is not slice_dir itself."""
    if os.path.exists(os.path.join(slice_dir, ".arcadia.root")):
        return slice_dir
    for root, _dirs, files in os.walk(slice_dir):
        if ".arcadia.root" in files:
            return root
    return slice_dir


def resolve_ref_raw(graph_dir):
    cand = os.path.join(graph_dir, "graph.fuse.json")
    if os.path.exists(cand):
        return cand
    for root, _dirs, files in os.walk(graph_dir):
        for f in files:
            if f.endswith(".json"):
                return os.path.join(root, f)
    return None


# ---------------------------------------------------------------------------
# Node actions
# ---------------------------------------------------------------------------


def build_action():
    t = time.monotonic()
    env = dict(os.environ, CGO_ENABLED="0")
    rc = subprocess.run([GO, "build", "-o", AY, "."], cwd=REPO_ROOT, env=env).returncode
    print(f"[build] {time.monotonic() - t:.2f}s", flush=True)

    return {"ok": rc == 0}


def fetch_action(kind, rid, dst_dir, ready):
    def action():
        if os.path.exists(ready):
            print(f"[{kind}:{rid}] cached", flush=True)
            return {"ok": True}

        t = time.monotonic()
        if os.path.exists(dst_dir):
            shutil.rmtree(dst_dir)
        os.makedirs(dst_dir, exist_ok=True)
        rc = sh([AY, "fetch", "sandbox", "--resource-id", rid, "--untar-to", dst_dir])
        print(f"[{kind}:{rid}] fetch {time.monotonic() - t:.2f}s", flush=True)

        if rc == 0:
            open(ready, "wb").close()

        return {"ok": rc == 0}

    return action


def gen_action(name, command, slice_dir, slice_ready, raw, budget):
    def action():
        cmd = [AY] + command[1:] + ["--source-root", resolve_slice_root(slice_dir)]
        _remove_if_exists(raw)
        t = time.monotonic()
        rc = sh(cmd, gogc=True, stdout=raw)
        secs = time.monotonic() - t
        over = budget is not None and secs > GEN_TIME_SLACK * budget

        if budget is None:
            print(f"[{name}] gen {secs:.2f}s (no budget)", flush=True)
        else:
            print(f"[{name}] gen {secs:.2f}s (budget {budget:.2f}s, limit {GEN_TIME_SLACK * budget:.2f}s)", flush=True)

        return {"ok": rc == 0, "secs": secs, "budget_over": over}

    return action


def normalize_our_action(label, raw, target, out_unsorted):
    def action():
        _remove_if_exists(out_unsorted)
        t = time.monotonic()
        rc = sh([AY, "dev", "dump", "normalize", "--in", raw, "--target", target, "--out", out_unsorted], gogc=True)
        print(f"[{label}] subproc: normalize our {time.monotonic() - t:.2f}s", flush=True)

        return {"ok": rc == 0}

    return action


def normalize_ref_action(label, graph_dir, target, out_unsorted):
    def action():
        raw = resolve_ref_raw(graph_dir)
        if raw is None:
            print(f"[{label}] no ref graph json under {graph_dir}", flush=True)
            return {"ok": False}

        _remove_if_exists(out_unsorted)
        t = time.monotonic()
        rc = sh([AY, "dev", "dump", "normalize", "--in", raw, "--target", target, "--out", out_unsorted, "--ref-graph"], gogc=True)
        print(f"[{label}] subproc: normalize ref {time.monotonic() - t:.2f}s", flush=True)

        return {"ok": rc == 0}

    return action


def sort_action(label, in_unsorted, out_sorted, side):
    def action():
        _remove_if_exists(out_sorted)
        t = time.monotonic()
        rc = sh([AY, "dev", "dump", "sort", "--in", in_unsorted, "--out", out_sorted], gogc=True)
        print(f"[{label}] subproc: sort {side} {time.monotonic() - t:.2f}s", flush=True)

        if rc == 0:
            _remove_if_exists(in_unsorted)

        return {"ok": rc == 0}

    return action


def compare_action(name, xfail, our_sorted, ref_sorted, out_dir):
    def action():
        if xfail is False:
            rc = sh(["cmp", "-s", our_sorted, ref_sorted])

            return {"ok": True, "verdict": "OK" if rc == 0 else "FAIL"}

        if xfail == "auto" and sh(["cmp", "-s", our_sorted, ref_sorted]) == 0:
            return {"ok": True, "verdict": "OK"}

        parity = normalized_node_parity_counts(our_sorted, ref_sorted)
        diff_file = os.path.join(out_dir, f"{name}.diff.txt")
        t = time.monotonic()
        sh([AY, "dev", "dump", "diff", "--left", our_sorted, "--right", ref_sorted, "--out", diff_file], gogc=True)
        print(f"[{name}] subproc: diff {time.monotonic() - t:.2f}s", flush=True)

        return {"ok": True, "verdict": "XFAIL", "parity": parity, "diff_file": diff_file}

    return action


# ---------------------------------------------------------------------------
# Graph construction + gating evaluation
# ---------------------------------------------------------------------------


def build_nodes(out_dir, cases):
    nodes = [Node("build", build_action, [], [AY])]
    fetched = {}  # ready-path -> Node, so a resource shared by several cases is fetched once

    def fetch(kind, url):
        rid = resource_id(url)
        dst = os.path.join(CACHE_DIR, f"{kind}-{rid}")
        ready = dst + ".ready"
        if ready not in fetched:
            fetched[ready] = Node(f"fetch-{kind}:{rid}", fetch_action(kind, rid, dst, ready), [AY], [ready])
        return dst, ready

    for e in cases:
        cid = e["id"]
        xfail = e.get("xfail", False)
        budget = e.get("budget")

        slice_dir, slice_ready = fetch("slice", e["slice_url"])
        graph_dir, graph_ready = fetch("graph", e["graph_url"])

        raw = os.path.join(out_dir, f"{cid}.our.json")
        our_u = os.path.join(out_dir, f"{cid}.our.norm.unsorted")
        ref_u = os.path.join(out_dir, f"{cid}.ref.norm.unsorted")
        our_s = os.path.join(out_dir, f"{cid}.our.norm.jsonl")
        ref_s = os.path.join(out_dir, f"{cid}.ref.norm.jsonl")

        nodes += [
            Node(f"gen:{cid}", gen_action(cid, e["command"], slice_dir, slice_ready, raw, budget), [AY, slice_ready], [raw]),
            Node(f"norm-our:{cid}", normalize_our_action(cid, raw, e["target"], our_u), [AY, raw], [our_u]),
            Node(f"sort-our:{cid}", sort_action(cid, our_u, our_s, "our"), [AY, our_u], [our_s]),
            Node(f"norm-ref:{cid}", normalize_ref_action(cid, graph_dir, e["target"], ref_u), [AY, graph_ready], [ref_u]),
            Node(f"sort-ref:{cid}", sort_action(cid, ref_u, ref_s, "ref"), [AY, ref_u], [ref_s]),
            Node(f"cmp:{cid}", compare_action(cid, xfail, our_s, ref_s, out_dir), [our_s, ref_s], []),
        ]

    return nodes + list(fetched.values())


def print_debug(cid, e, out_dir):
    slice_dir = os.path.join(CACHE_DIR, "slice-" + resource_id(e["slice_url"]))
    graph_dir = os.path.join(CACHE_DIR, "graph-" + resource_id(e["graph_url"]))
    our_raw = os.path.join(out_dir, f"{cid}.our.json")
    ref_raw = resolve_ref_raw(graph_dir)
    our_s = os.path.join(out_dir, f"{cid}.our.norm.jsonl")
    ref_s = os.path.join(out_dir, f"{cid}.ref.norm.jsonl")

    print(f"[{cid}] debug:")
    print(f"[{cid}]   source-root : {resolve_slice_root(slice_dir)}")
    print(f"[{cid}]   target      : {e['target']}")
    print(f"[{cid}]   our graph   : {our_raw}")
    print(f"[{cid}]   ref graph   : {ref_raw}")
    print(f"[{cid}]   our sorted  : {our_s}")
    print(f"[{cid}]   ref sorted  : {ref_s}")
    print(f"[{cid}]   diff        : {os.path.join(out_dir, f'{cid}.diff.txt')}")
    print(f"[{cid}]   inspect     : {AY} dev dump diff --left {our_s} --right {ref_s} --by-token")


def evaluate(cases, results, out_dir):
    status = 0

    for e in cases:
        cid = e["id"]
        xfail = e.get("xfail", False)

        fetch_ok = (results.get(f"fetch-slice:{resource_id(e['slice_url'])}", {}).get("ok")
                    and results.get(f"fetch-graph:{resource_id(e['graph_url'])}", {}).get("ok"))
        if not fetch_ok:
            print(f"[{cid}] FAIL (fetch)")
            if xfail is not True:
                status = 1
            continue

        gen = results.get(f"gen:{cid}", {})
        if not gen.get("ok"):
            print(f"[{cid}] FAIL (generate)")
            print_debug(cid, e, out_dir)
            if xfail is not True:
                status = 1
            continue

        if gen.get("budget_over"):
            budget = e.get("budget")
            print(f"[{cid}] FAIL (perf regression): gen {gen['secs']:.2f}s > "
                  f"{GEN_TIME_SLACK:g}x budget {budget:.2f}s — optimize the code, do NOT raise the budget")
            status = 1

        norm_ok = all(results.get(f"{step}:{cid}", {}).get("ok")
                      for step in ("norm-our", "sort-our", "norm-ref", "sort-ref"))
        if not norm_ok:
            print(f"[{cid}] FAIL (normalize)")
            print_debug(cid, e, out_dir)
            if xfail is not True:
                status = 1
            continue

        verdict = results.get(f"cmp:{cid}", {}).get("verdict")

        if verdict == "OK":
            print(f"[{cid}] OK")
        elif verdict == "XFAIL":
            cmp = results[f"cmp:{cid}"]
            p = cmp["parity"]
            print(f"[{cid}] exact normalized-node parity: "
                  f"matched={p.exact} our_only={p.left_only} ref_only={p.right_only} "
                  f"our_total={p.left_total} ref_total={p.right_total}")
            print(f"[{cid}] XFAIL (not gating) — full diff: {cmp['diff_file']}")
            print_debug(cid, e, out_dir)
        else:
            print(f"[{cid}] FAIL")
            print_debug(cid, e, out_dir)
            status = 1

    return status


def main() -> int:
    out_dir = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else os.path.join(REPO_ROOT, ".out", "validate"))
    os.makedirs(out_dir, exist_ok=True)
    os.makedirs(CACHE_DIR, exist_ok=True)

    global AY, WORK_CWD
    AY = os.path.join(out_dir, "ay")
    WORK_CWD = out_dir

    cases = load_config()

    if not cases:
        print(f"validate.py: no cases in {CONFIG_PATH}")
        return 0

    jobs = int(os.environ.get("VALIDATE_JOBS", os.cpu_count() or 4))
    print(f"[graph] {len(cases)} cases, jobs={jobs}", flush=True)

    t0 = time.monotonic()
    results = run_graph(build_nodes(out_dir, cases), jobs)
    print(f"[total] graph wall {time.monotonic() - t0:.2f}s", flush=True)

    if not results.get("build", {}).get("ok"):
        print("validate.py: build failed")
        return 1

    status = evaluate(cases, results, out_dir)
    print("validate.py: all gating cases byte-exact" if status == 0 else "validate.py: failures above")

    return status


if __name__ == "__main__":
    raise SystemExit(main())
