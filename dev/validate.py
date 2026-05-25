#!/usr/bin/env python3
"""validate.py — L4 byte-exact acceptance orchestrator.

For each case: run its gen_<graph>.sh generator, normalize both our output
and the raw upstream reference into canonical JSONL (streaming `ay dump
normalize | ay dump sort`), then either byte-compare (gating cases) or run
diff.py metrics plus exact normalized-node parity counts (xfail cases).
xfail cases never affect the exit code; the suite fails only when a gating
case diverges.

xfail values: False = gating (byte-compare); True = xfail (parity metrics only);
"auto" = gate when byte-exact, xfail otherwise (self-promoting once parity is reached).

Usage: validate.py [out-dir]   (default: <repo>/.out/validate)
"""
import fcntl
import os
import subprocess
import sys
import time
from dataclasses import dataclass

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(SCRIPT_DIR)
GO = os.path.join(REPO_ROOT, "go")
AY = os.path.join(REPO_ROOT, "ay")
NORMALIZE_PY = os.path.join(SCRIPT_DIR, "normalize.py")

# Host-wide, well-known lock path — shared across every worktree (all agents
# run as the same user, so ~ resolves identically). NOT under the repo (each
# worktree has its own .out) and NOT /tmp (read-only in some sandboxes).
LOCK_PATH = os.path.join(
    os.environ.get("XDG_CACHE_HOME", os.path.expanduser("~/.cache")), "ay", "validate.lock"
)


# Per-case generation wall-time budgets (seconds): the measured baseline for
# `gen_<case>.sh` (which includes writing the raw graph to disk — sg5 is ~2.2
# GB). A generation slower than GEN_TIME_SLACK * budget FAILs the case as a
# performance regression: the generator code got slower and must be optimized —
# do NOT bump the budget to silence it. Measured in-flow on the reference host
# with the run lock held (validate runs serialized). sg4 carries extra cushion
# because sub-second timing is dominated by process-startup noise.
GEN_TIME_BUDGET = {
    "sg2": 1.20,
    "sg2_x86_64": 1.20,
    "sg3": 2.00,
    "sg4": 0.50,
    "sg5": 8.80,
}
GEN_TIME_SLACK = 1.2


def acquire_global_lock():
    """Serialize validate.py runs across worktrees so concurrent agents don't
    collectively OOM — each run drives memory-heavy `ay make` + normalize over
    multi-GB graphs, and several at once exhaust host RAM (global_oom). flock is
    released automatically on exit, crash, or OOM SIGKILL, so a reaped holder
    cannot deadlock the next run. Returns the fd, which the caller must keep
    open (referenced) for the lifetime of the run."""
    os.makedirs(os.path.dirname(LOCK_PATH), exist_ok=True)
    fd = os.open(LOCK_PATH, os.O_CREAT | os.O_RDWR, 0o644)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        print(f"[lock] another validate.py holds {LOCK_PATH}; waiting…", flush=True)
        fcntl.flock(fd, fcntl.LOCK_EX)
    return fd

# name, normalize target, raw upstream reference, xfail (see docstring for values)
CASES = [
    ("sg2", "devtools/ymake/bin", "/home/pg/monorepo/yatool/sg2.json", False),
    ("sg2_x86_64", "devtools/ymake/bin", "/home/pg/monorepo/yatool/sg2_x86_64.json", False),
    ("sg3", "devtools/ya/bin", "/home/pg/monorepo/yatool/sg3.json", False),
    ("sg4", "util/ut", "/home/pg/monorepo/ydb/sg4.json", False),
    ("sg5", "ydb/apps/ydbd", "/home/pg/monorepo/ydb/sg5.json", "auto"),
]


@dataclass(frozen=True)
class ParityCounts:
    exact: int
    left_only: int
    right_only: int
    left_total: int
    right_total: int


def run(cmd):
    return subprocess.run(cmd, cwd=REPO_ROOT)


def _remove_if_exists(path):
    try:
        os.remove(path)
    except FileNotFoundError:
        pass


def _normalize_sort_go(raw, target, out):
    """ay dump normalize <raw> | ay dump sort > out (streaming, bounded mem)."""
    tmp = out + ".tmp"
    _remove_if_exists(tmp)
    p1 = subprocess.Popen(
        [AY, "dump", "normalize", "--in", raw, "--target", target, "--out", "-"],
        cwd=REPO_ROOT, stdout=subprocess.PIPE,
    )
    p2 = subprocess.Popen(
        [AY, "dump", "sort", "--out", tmp],
        cwd=REPO_ROOT, stdin=p1.stdout,
    )
    p1.stdout.close()
    p2.communicate()
    p1_rc = p1.wait()
    if p1_rc == 0 and p2.returncode == 0:
        os.replace(tmp, out)
        return True
    _remove_if_exists(tmp)
    return False


def _normalize_sort_py(raw, target, out):
    tmp = out + ".tmp"
    tmp_norm = out + ".py.tmp"
    _remove_if_exists(tmp)
    _remove_if_exists(tmp_norm)
    subprocess.run(
        [sys.executable, NORMALIZE_PY, "--in", raw, "--target", target, "--out", tmp_norm],
        cwd=REPO_ROOT,
        check=True,
    )
    with open(tmp_norm, "rb") as fh:
        subprocess.run(
            [AY, "dump", "sort", "--out", tmp],
            cwd=REPO_ROOT,
            stdin=fh,
            check=True,
        )
    _remove_if_exists(tmp_norm)
    os.replace(tmp, out)


def normalize_pair(name, our_raw, ref_raw, target, our_out, ref_out, *, allow_fallback):
    if _normalize_sort_go(our_raw, target, our_out) and _normalize_sort_go(ref_raw, target, ref_out):
        return True
    if not allow_fallback:
        print(f"[{name}] FAIL (normalize)")
        return False
    print(f"[{name}] normalize fallback: ay dump normalize failed; retrying with dev/normalize.py")
    _normalize_sort_py(our_raw, target, our_out)
    _normalize_sort_py(ref_raw, target, ref_out)
    return True


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


def main() -> int:
    # Held for the whole run (released on exit/crash/OOM); keep the fd
    # referenced so the OS does not drop the lock early.
    lock_fd = acquire_global_lock()  # noqa: F841

    out_dir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(REPO_ROOT, ".out", "validate")
    os.makedirs(out_dir, exist_ok=True)

    subprocess.run([GO, "build", "-o", "ay", "."], cwd=REPO_ROOT, check=True)

    status = 0
    for name, target, ref, xfail in CASES:
        raw = os.path.join(out_dir, f"{name}.our.json")
        our_n = os.path.join(out_dir, f"{name}.our.norm.jsonl")
        ref_n = os.path.join(out_dir, f"{name}.ref.norm.jsonl")
        gen = os.path.join(SCRIPT_DIR, f"gen_{name}.sh")

        print(f"[{name}] generate")
        gen_start = time.monotonic()
        if run([gen, raw]).returncode != 0:
            print(f"[{name}] FAIL (generate)")
            if not xfail:
                status = 1
            continue
        gen_secs = time.monotonic() - gen_start

        budget = GEN_TIME_BUDGET.get(name)
        if budget is None:
            print(f"[{name}] gen time {gen_secs:.2f}s (no budget)")
        else:
            limit = GEN_TIME_SLACK * budget
            print(f"[{name}] gen time {gen_secs:.2f}s (budget {budget:.2f}s, limit {limit:.2f}s)")
            if gen_secs > limit:
                print(
                    f"[{name}] FAIL (perf regression): generation took {gen_secs:.2f}s > "
                    f"{GEN_TIME_SLACK:g}x budget {budget:.2f}s = {limit:.2f}s — the generator "
                    f"got slower; optimize the code, do NOT raise the budget"
                )
                status = 1

        print(f"[{name}] normalize our + ref")
        if not normalize_pair(name, raw, ref, target, our_n, ref_n, allow_fallback=bool(xfail)):
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
            run([AY, "dump", "diff", "--left", our_n, "--right", ref_n, "--out", diff_file])
            print(f"[{name}] xfail — diff (not gating), first 200 lines:")
            with open(diff_file) as fh:
                for i, line in enumerate(fh):
                    if i >= 200:
                        break
                    sys.stdout.write(line)
            print(f"[{name}] full diff: {diff_file}")
            print(f"[{name}] XFAIL")
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
