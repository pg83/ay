#!/usr/bin/env python3
"""validate.py — L4 byte-exact acceptance orchestrator.

For each case: run its gen_<graph>.sh generator, normalize both our output
and the raw upstream reference into canonical JSONL (streaming `ay dev dump
normalize | ay dev dump sort`), then either byte-compare (gating cases) or run
diff.py metrics plus exact normalized-node parity counts (xfail cases).
xfail cases never affect the exit code; the suite fails only when a gating
case diverges.

xfail values: False = gating (byte-compare); True = xfail (parity metrics only);
"auto" = gate when byte-exact, xfail otherwise (self-promoting once parity is reached).

Usage: validate.py [out-dir]   (default: <repo>/.out/validate)
"""
import os
import subprocess
import sys
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
# sub-2s cases jitter too much under shared-box contention to gate reliably, so
# they get an effectively-infinite budget (gate disabled, time still printed).
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
    ("sg7", "yabs/server/daemons/bs_static", "/home/pg/monorepo/4", "/home/pg/monorepo/4/sg7.json", True),
]


@dataclass(frozen=True)
class ParityCounts:
    exact: int
    left_only: int
    right_only: int
    left_total: int
    right_total: int


def run(cmd):
    return subprocess.run(cmd, cwd=WORK_CWD)


def _remove_if_exists(path):
    try:
        os.remove(path)
    except FileNotFoundError:
        pass


def _normalize_sort_go(raw, target, out, ref_graph=False):
    """ay dev dump normalize <raw> | ay dev dump sort > out (streaming, bounded mem).

    ref_graph marks the input as the upstream reference (ymake) graph, enabling
    reference-side normalizations (filterARLDInputs input pruning + build-order-only
    dep strip) that discount artifacts our generator does not model. Our graph is
    normalized WITHOUT it (faithful), so any superfluous input/dep WE emit — or any
    over-filtration on the reference side — surfaces in the diff.
    """
    norm_cmd = [AY, "dev", "dump", "normalize", "--in", raw, "--target", target, "--out", "-"]
    if ref_graph:
        norm_cmd.append("--ref-graph")
    tmp = out + ".tmp"
    _remove_if_exists(tmp)
    p1 = subprocess.Popen(
        norm_cmd,
        cwd=WORK_CWD, stdout=subprocess.PIPE,
    )
    p2 = subprocess.Popen(
        [AY, "dev", "dump", "sort", "--out", tmp],
        cwd=WORK_CWD, stdin=p1.stdout,
    )
    p1.stdout.close()
    p2.communicate()
    p1_rc = p1.wait()
    if p1_rc == 0 and p2.returncode == 0:
        os.replace(tmp, out)
        return True
    _remove_if_exists(tmp)
    return False


def normalize_pair(name, our_raw, ref_raw, target, our_out, ref_out):
    # Our graph normalized faithfully; upstream ref gets reference-side pruning.
    if _normalize_sort_go(our_raw, target, our_out) and _normalize_sort_go(ref_raw, target, ref_out, ref_graph=True):
        return True
    print(f"[{name}] FAIL (normalize)")
    return False


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


def _timed_gen(gen, raw):
    """Run the generator once; return (returncode, wall seconds)."""
    t0 = time.monotonic()
    rc = run([gen, raw]).returncode
    return rc, time.monotonic() - t0


def measured_generate(name, gen, raw, budget):
    """Run `gen`, returning (ok, secs). When a budget is set and the first run
    blows GEN_TIME_SLACK*budget, re-run twice more and keep the BEST (min) of
    the three: one wall sample can spike under shared-box contention, but a real
    regression stays slow across all three. ok=False means the generator itself
    failed."""
    rc, secs = _timed_gen(gen, raw)
    if rc != 0:
        return False, secs
    if budget is not None and secs > GEN_TIME_SLACK * budget:
        samples = [secs]
        for _ in range(2):
            rc, s = _timed_gen(gen, raw)
            if rc != 0:
                return False, s
            samples.append(s)
        secs = min(samples)
        print(f"[{name}] over limit; remeasured {', '.join(f'{x:.2f}' for x in samples)}s — best {secs:.2f}s")
    return True, secs


def main() -> int:
    # Absolutize: AY / WORK_CWD / the per-case paths below are all derived from
    # out_dir and then used from processes whose cwd is WORK_CWD=out_dir. A relative
    # out_dir (e.g. `.out/reviewer-validate`) would re-resolve against that cwd and
    # double-nest (the normalize .tmp then lands under out_dir/out_dir and os.replace
    # fails). Resolve once, up front, so every derived path is cwd-independent.
    out_dir = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else os.path.join(REPO_ROOT, ".out", "validate"))
    os.makedirs(out_dir, exist_ok=True)

    # Build the binary into the writable out-dir (never into REPO_ROOT, which may
    # be read-only) and run everything from there: the gen_*.sh scripts invoke
    # `./ay`, resolved under WORK_CWD=out_dir, and AY (normalize/sort/diff) points
    # at the same binary. REPO_ROOT stays the build cwd so `go build` finds the module.
    global AY, WORK_CWD
    AY = os.path.join(out_dir, "ay")
    WORK_CWD = out_dir

    # The project has no cgo imports; force CGO_ENABLED=0 so the build never
    # drags in runtime/cgo (which needs a C toolchain with stddef.h that may be
    # absent from the host clang resource dir). Pure-Go net/user resolvers are
    # equivalent for our purposes and more hermetic.
    build_env = dict(os.environ, CGO_ENABLED="0")
    subprocess.run([GO, "build", "-o", AY, "."], cwd=REPO_ROOT, check=True, env=build_env)

    status = 0
    for name, target, source_root, ref, xfail in CASES:
        missing = [p for p in (source_root, ref) if not os.path.exists(p)]
        if missing:
            print(f"[{name}] SKIP (data not present on host: {', '.join(missing)})")
            continue

        raw = os.path.join(out_dir, f"{name}.our.json")
        our_n = os.path.join(out_dir, f"{name}.our.norm.jsonl")
        ref_n = os.path.join(out_dir, f"{name}.ref.norm.jsonl")
        gen = os.path.join(SCRIPT_DIR, f"gen_{name}.sh")

        print(f"[{name}] generate")
        budget = GEN_TIME_BUDGET.get(name)
        ok, gen_secs = measured_generate(name, gen, raw, budget)
        if not ok:
            print(f"[{name}] FAIL (generate)")
            if not xfail:
                status = 1
            continue

        if budget is None:
            print(f"[{name}] gen time {gen_secs:.2f}s (no budget)")
        else:
            limit = GEN_TIME_SLACK * budget
            print(f"[{name}] gen time {gen_secs:.2f}s (budget {budget:.2f}s, limit {limit:.2f}s)")
            if gen_secs > limit:
                print(
                    f"[{name}] FAIL (perf regression): best gen {gen_secs:.2f}s > "
                    f"{GEN_TIME_SLACK:g}x budget {budget:.2f}s = {limit:.2f}s — the generator "
                    f"got slower; optimize the code, do NOT raise the budget"
                )
                status = 1

        print(f"[{name}] normalize our + ref")
        if not normalize_pair(name, raw, ref, target, our_n, ref_n):
            if not xfail:
                status = 1
            continue

        if xfail:
            if xfail == "auto" and run(["cmp", "-s", our_n, ref_n]).returncode == 0:
                print(f"[{name}] OK")
                continue
            parity = normalized_node_parity_counts(our_n, ref_n)
            print(
                f"[{name}] exact normalized-node parity: "
                f"matched={parity.exact} "
                f"our_only={parity.left_only} ref_only={parity.right_only} "
                f"our_total={parity.left_total} ref_total={parity.right_total}"
            )
            diff_file = os.path.join(out_dir, f"{name}.diff.txt")
            run([AY, "dev", "dump", "diff", "--left", our_n, "--right", ref_n, "--out", diff_file])
            print(f"[{name}] XFAIL (not gating) — full diff: {diff_file}")
            continue

        print(f"[{name}] compare")
        if run(["cmp", "-s", our_n, ref_n]).returncode == 0:
            print(f"[{name}] OK")
        else:
            print(f"[{name}] FAIL")
            status = 1

    print("validate.py: all gating cases byte-exact" if status == 0 else "validate.py: failures above")
    return status


if __name__ == "__main__":
    raise SystemExit(main())
