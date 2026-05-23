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
import os
import subprocess
import sys
from dataclasses import dataclass

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(SCRIPT_DIR)
AY = os.path.join(REPO_ROOT, "ay")

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


def normalize_sort(raw, target, out):
    """ay dump normalize <raw> | ay dump sort > out (streaming, bounded mem)."""
    p1 = subprocess.Popen(
        [AY, "dump", "normalize", "--in", raw, "--target", target, "--out", "-"],
        cwd=REPO_ROOT, stdout=subprocess.PIPE,
    )
    p2 = subprocess.Popen(
        [AY, "dump", "sort", "--out", out],
        cwd=REPO_ROOT, stdin=p1.stdout,
    )
    p1.stdout.close()
    p2.communicate()
    p1.wait()


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
    out_dir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(REPO_ROOT, ".out", "validate")
    os.makedirs(out_dir, exist_ok=True)

    subprocess.run(["go", "build", "-o", "ay", "."], cwd=REPO_ROOT, check=True)

    status = 0
    for name, target, ref, xfail in CASES:
        raw = os.path.join(out_dir, f"{name}.our.json")
        our_n = os.path.join(out_dir, f"{name}.our.norm.jsonl")
        ref_n = os.path.join(out_dir, f"{name}.ref.norm.jsonl")
        gen = os.path.join(SCRIPT_DIR, f"gen_{name}.sh")

        print(f"[{name}] generate")
        if run([gen, raw]).returncode != 0:
            print(f"[{name}] FAIL (generate)")
            if not xfail:
                status = 1
            continue

        print(f"[{name}] normalize our + ref")
        normalize_sort(raw, target, our_n)
        normalize_sort(ref, target, ref_n)

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
