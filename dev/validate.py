#!/usr/bin/env python3
"""validate.py — L4 byte-exact acceptance orchestrator (parallel DAG).

All work is modelled as a DAG of nodes — one node per program invocation, with
explicit input/output FILES — and run by a small micro-executor that joins nodes
on those files and runs independent ones concurrently (bounded by VALIDATE_JOBS).
There is no caching: no content hashes, no timestamps — every node always runs.

Per case the chain is: gen_<graph>.sh -> normalize -> sort for our graph, and
normalize(--ref-graph) -> sort for the upstream reference (which starts as soon as
the binary is built, in parallel with the generator), then a compare node byte-
compares (gating cases) or counts exact normalized-node parity + writes the diff
(xfail cases). Cross-case and our/ref work overlap, so the wall time collapses to
the critical path instead of the sum.

xfail values: False = gating (byte-compare); True = xfail (parity metrics only);
"auto" = gate when byte-exact, xfail otherwise (self-promoting once parity is reached).

Usage: validate.py [out-dir]   (default: <repo>/.out/validate)
Env:   VALIDATE_JOBS — max concurrent nodes (default: cpu count).
"""
import os
import subprocess
import sys
import threading
import time
from dataclasses import dataclass

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(SCRIPT_DIR)
GO = "go"
# AY (the built binary) and WORK_CWD (where gen_*.sh runs, resolving its `./ay`)
# are repointed at the writable out-dir in main(), so REPO_ROOT may be read-only
# (the ./acceptance merge gate runs both repos from $TMPDIR). Defaults keep a
# direct `validate.py` invocation working when nothing overrides them.
AY = os.path.join(REPO_ROOT, "ay")
WORK_CWD = REPO_ROOT

# Per-case generation wall-time budgets (seconds): a gen slower than
# GEN_TIME_SLACK * budget FAILs the case as a perf regression — optimize the
# code, do NOT raise the budget. Only sg5 is meaningfully gated (largest graph,
# the one the 10x boost regression hit, and stable enough to time). The small
# sub-2s cases jitter too much to gate reliably, so they get an
# effectively-infinite budget (gate disabled, time still printed).
GEN_TIME_BUDGET = {
    "sg2": 10000.0,
    "sg2_x86_64": 10000.0,
    "sg3": 10000.0,
    "sg4": 10000.0,
    "sg5": 8.80,
}
GEN_TIME_SLACK = 1.2


# name, normalize target, source root, raw upstream reference, xfail (see docstring for values)
# A case is SKIPPED (never affects exit code) when its source root or reference
# json is absent from this host — references are large and not every box has
# every checkout, so a missing one means "no data here", not a failure.
CASES = [
    ("sg2", "devtools/ymake/bin", "/home/pg/monorepo/yatool", "/home/pg/monorepo/yatool/sg2.json", False),
    ("sg2_x86_64", "devtools/ymake/bin", "/home/pg/monorepo/yatool", "/home/pg/monorepo/yatool/sg2_x86_64.json", False),
    ("sg3", "devtools/ya/bin", "/home/pg/monorepo/yatool", "/home/pg/monorepo/yatool/sg3.json", False),
    ("sg4", "util/ut", "/home/pg/monorepo/ydb", "/home/pg/monorepo/ydb/sg4.json", False),
    ("sg5", "ydb/apps/ydbd", "/home/pg/monorepo/ydb", "/home/pg/monorepo/ydb/sg5.json", False),
    ("sg6", "devtools/ya/bin", "/home/pg/monorepo/3", "/home/pg/monorepo/3/sg6.json", False),
    ("sg7", "yabs/server/daemons/bs_static", "/home/pg/monorepo/4", "/home/pg/monorepo/4/sg7.json", False),
]


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

    return ParityCounts(
        exact=exact,
        left_only=left_only,
        right_only=right_only,
        left_total=left_total,
        right_total=right_total,
    )


# ---------------------------------------------------------------------------
# DAG micro-executor
# ---------------------------------------------------------------------------


@dataclass
class Node:
    name: str        # unique id, also the log label prefix
    action: object   # callable() -> result dict; must contain "ok": bool
    inputs: list     # file paths it reads
    outputs: list    # file paths it produces


def run_graph(nodes, jobs):
    """Run the node DAG, joining each node on the producers of its input files,
    up to `jobs` concurrently. No caching — every node runs exactly once. A node
    whose any dependency failed is not run (marked failed, propagating). Returns
    {name: result}."""
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


def sh(cmd, gogc=False):
    """Run a subprocess in WORK_CWD; return its exit code. gogc=True passes
    GOGC=800 to that one invocation — the heavy ay commands (gen / normalize /
    sort / diff) spend ~half their CPU on GC at the default GOGC=100; 800 roughly
    halves it. Set per-command, never process-wide (build / cmp must not get it)."""
    env = dict(os.environ, GOGC="800") if gogc else None

    return subprocess.run(cmd, cwd=WORK_CWD, env=env).returncode


# ---------------------------------------------------------------------------
# Node actions (one per program invocation)
# ---------------------------------------------------------------------------


def build_action():
    t = time.monotonic()
    # The project has no cgo imports; force CGO_ENABLED=0 so the build never drags
    # in runtime/cgo (which needs a C toolchain with stddef.h that may be absent).
    env = dict(os.environ, CGO_ENABLED="0")
    rc = subprocess.run([GO, "build", "-o", AY, "."], cwd=REPO_ROOT, env=env).returncode
    print(f"[build] {time.monotonic() - t:.2f}s", flush=True)

    return {"ok": rc == 0}


def gen_action(name, gen, raw, budget):
    def action():
        t = time.monotonic()
        rc = sh([gen, raw], gogc=True)
        secs = time.monotonic() - t
        over = budget is not None and secs > GEN_TIME_SLACK * budget

        if budget is None:
            print(f"[{name}] gen {secs:.2f}s (no budget)", flush=True)
        else:
            print(f"[{name}] gen {secs:.2f}s (budget {budget:.2f}s, limit {GEN_TIME_SLACK * budget:.2f}s)", flush=True)

        return {"ok": rc == 0, "secs": secs, "budget_over": over}

    return action


def normalize_action(label, raw, target, out_unsorted, ref_graph):
    def action():
        cmd = [AY, "dev", "dump", "normalize", "--in", raw, "--target", target, "--out", out_unsorted]

        if ref_graph:
            cmd.append("--ref-graph")

        _remove_if_exists(out_unsorted)
        t = time.monotonic()
        rc = sh(cmd, gogc=True)
        print(f"[{label}] subproc: normalize {'ref' if ref_graph else 'our'} {time.monotonic() - t:.2f}s", flush=True)

        return {"ok": rc == 0}

    return action


def sort_action(label, in_unsorted, out_sorted, side):
    def action():
        _remove_if_exists(out_sorted)
        t = time.monotonic()
        rc = sh([AY, "dev", "dump", "sort", "--in", in_unsorted, "--out", out_sorted], gogc=True)
        print(f"[{label}] subproc: sort {side} {time.monotonic() - t:.2f}s", flush=True)

        if rc == 0:
            _remove_if_exists(in_unsorted)  # drop the large intermediate; no caching

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


def build_nodes(out_dir, active):
    """One node per program invocation; deps are implied by the shared files."""
    nodes = [Node("build", build_action, [], [AY])]

    for name, target, _src, ref, xfail in active:
        raw = os.path.join(out_dir, f"{name}.our.json")
        our_u = os.path.join(out_dir, f"{name}.our.norm.unsorted")
        ref_u = os.path.join(out_dir, f"{name}.ref.norm.unsorted")
        our_s = os.path.join(out_dir, f"{name}.our.norm.jsonl")
        ref_s = os.path.join(out_dir, f"{name}.ref.norm.jsonl")
        gen = os.path.join(SCRIPT_DIR, f"gen_{name}.sh")
        budget = GEN_TIME_BUDGET.get(name)

        nodes += [
            Node(f"gen:{name}", gen_action(name, gen, raw, budget), [AY], [raw]),
            Node(f"norm-our:{name}", normalize_action(name, raw, target, our_u, False), [AY, raw], [our_u]),
            Node(f"sort-our:{name}", sort_action(name, our_u, our_s, "our"), [AY, our_u], [our_s]),
            # ref normalize depends only on the binary + the external reference json,
            # so it starts right after build — in parallel with the generator.
            Node(f"norm-ref:{name}", normalize_action(name, ref, target, ref_u, True), [AY], [ref_u]),
            Node(f"sort-ref:{name}", sort_action(name, ref_u, ref_s, "ref"), [AY, ref_u], [ref_s]),
            Node(f"cmp:{name}", compare_action(name, xfail, our_s, ref_s, out_dir), [our_s, ref_s], []),
        ]

    return nodes


def evaluate(active, results):
    """Read node results in case order; print the gating contract; return status."""
    status = 0

    for name, _target, _src, _ref, xfail in active:
        gen = results.get(f"gen:{name}", {})

        if not gen.get("ok"):
            print(f"[{name}] FAIL (generate)")

            if not xfail:
                status = 1

            continue

        if gen.get("budget_over"):
            budget = GEN_TIME_BUDGET.get(name)
            print(
                f"[{name}] FAIL (perf regression): gen {gen['secs']:.2f}s > "
                f"{GEN_TIME_SLACK:g}x budget {budget:.2f}s — optimize the code, do NOT raise the budget"
            )
            status = 1

        norm_ok = all(
            results.get(f"{step}:{name}", {}).get("ok")
            for step in ("norm-our", "sort-our", "norm-ref", "sort-ref")
        )

        if not norm_ok:
            print(f"[{name}] FAIL (normalize)")

            if not xfail:
                status = 1

            continue

        verdict = results.get(f"cmp:{name}", {}).get("verdict")

        if verdict == "OK":
            print(f"[{name}] OK")
        elif verdict == "XFAIL":
            cmp = results[f"cmp:{name}"]
            p = cmp["parity"]
            print(
                f"[{name}] exact normalized-node parity: "
                f"matched={p.exact} our_only={p.left_only} ref_only={p.right_only} "
                f"our_total={p.left_total} ref_total={p.right_total}"
            )
            print(f"[{name}] XFAIL (not gating) — full diff: {cmp['diff_file']}")
        else:
            print(f"[{name}] FAIL")
            status = 1

    return status


def main() -> int:
    # Absolutize so AY / WORK_CWD / the per-case paths are cwd-independent (a
    # relative out-dir would re-resolve against WORK_CWD and double-nest).
    out_dir = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else os.path.join(REPO_ROOT, ".out", "validate"))
    os.makedirs(out_dir, exist_ok=True)

    # Build into the writable out-dir (REPO_ROOT may be read-only) and run gen_*.sh
    # from there (WORK_CWD=out_dir resolves their `./ay`); AY points at the same binary.
    global AY, WORK_CWD
    AY = os.path.join(out_dir, "ay")
    WORK_CWD = out_dir

    active = []

    for case in CASES:
        name, _target, source_root, ref, _xfail = case
        missing = [p for p in (source_root, ref) if not os.path.exists(p)]

        if missing:
            print(f"[{name}] SKIP (data not present on host: {', '.join(missing)})")
            continue

        active.append(case)

    jobs = int(os.environ.get("VALIDATE_JOBS", os.cpu_count() or 4))
    print(f"[graph] {len(active)} cases, jobs={jobs}", flush=True)

    t0 = time.monotonic()
    results = run_graph(build_nodes(out_dir, active), jobs)
    print(f"[total] graph wall {time.monotonic() - t0:.2f}s", flush=True)

    if not results.get("build", {}).get("ok"):
        print("validate.py: build failed")
        return 1

    status = evaluate(active, results)
    print("validate.py: all gating cases byte-exact" if status == 0 else "validate.py: failures above")

    return status


if __name__ == "__main__":
    raise SystemExit(main())
