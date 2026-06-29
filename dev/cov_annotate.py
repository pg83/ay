#!/usr/bin/env python3
"""Annotate the source tree with runtime-coverage markers.

Reads a Go binary-coverage textfmt profile (go tool covdata textfmt) and, for
every .go file it mentions, writes an annotated copy whose lines are prefixed:

    ---   line belongs to a count==0 block and to no covered block (NEVER RUN)
    (4 spaces)  covered or neutral (blank / comment / brace / signature)

Line numbers are preserved 1:1, so `grep -rn '^---' <outdir>` yields
`file:lineno:--- <source>` for every never-executed line, with -C for context.

Also emits a flat table of dead lines grouped by bucket (CORE first), so the
graph-generation dead branches are separated from dev-tooling / execution /
platform code that the `-G` corpus structurally never exercises.

Usage:
    python3 dev/cov_annotate.py --profile cover.out --src . \
        --out .out/cov-annotated --table .out/dead-lines.txt
"""
import argparse
import collections
import os
import re

# Files the `ay make -G` corpus can never reach (dev subcommands) or that are
# execution-time only (the gate stops at graph generation). Their zero coverage
# is expected, not dead code — bucketed out of the CORE review set.
DEV = {
    "refac.go", "refac_case.go", "linters.go", "macro_audit.go",
    "macro_audit_known.go", "probe.go", "probe_callsite.go",
    "probe_mapinstr.go", "probe_strinstr.go", "perf.go", "perf_darts.go",
    "cas_analyze.go", "dump.go", "dump_diff.go", "dump_normalize.go",
    "dump_sort.go", "dump_grep.go", "merge_heap.go",
}
EXEC = {"executor.go", "ssh_agent.go", "fetch.go"}
MIXED = {"fs_os.go", "fs_linux.go", "fs_mem.go", "fs_other.go", "main.go"}


def bucket(f):
    if f.endswith("_test.go"):
        return "TEST"
    if f in DEV:
        return "DEV"
    if f in EXEC:
        return "EXEC"
    if f in MIXED:
        return "MIXED"
    return "CORE"


LINE_RE = re.compile(r"^([^:]+):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)$")


def parse_profile(path, module_prefix):
    covered = collections.defaultdict(set)
    dead = collections.defaultdict(set)
    with open(path) as fh:
        for ln in fh:
            ln = ln.strip()
            if not ln or ln.startswith("mode:"):
                continue
            m = LINE_RE.match(ln)
            if not m:
                continue
            f, sl, el, _ns, cnt = m.groups()
            if f.startswith(module_prefix):
                f = f[len(module_prefix):]
            rng = range(int(sl), int(el) + 1)
            tgt = covered[f] if int(cnt) > 0 else dead[f]
            tgt.update(rng)
    return covered, dead


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--profile", required=True)
    ap.add_argument("--src", default=".")
    ap.add_argument("--out")
    ap.add_argument("--table")
    ap.add_argument("--module", default="ay/")
    ap.add_argument("--inplace", action="store_true",
                    help="rewrite the source files, prefixing dead lines with "
                         "the marker and leaving every other line verbatim")
    ap.add_argument("--prefix", default="---")
    ap.add_argument("--buckets", default="CORE",
                    help="comma list of buckets to touch (default CORE)")
    args = ap.parse_args()

    want = set(args.buckets.split(",")) if args.buckets else None
    covered, dead = parse_profile(args.profile, args.module)
    files = sorted(set(covered) | set(dead))

    table = []
    for f in files:
        b = bucket(f)

        if want and b not in want:
            continue

        deadlines = dead[f] - covered[f]
        src_path = os.path.join(args.src, f)

        if not os.path.exists(src_path):
            continue

        lines = open(src_path, errors="replace").read().splitlines()

        if args.inplace:
            with open(src_path, "w") as w:
                for i, text in enumerate(lines, 1):
                    w.write((args.prefix + text if i in deadlines else text) + "\n")
        elif args.out:
            out_path = os.path.join(args.out, f)
            os.makedirs(os.path.dirname(out_path) or ".", exist_ok=True)

            with open(out_path, "w") as w:
                for i, text in enumerate(lines, 1):
                    mark = "---" if i in deadlines else "   "
                    w.write(f"{mark} {text}\n")

        for i in sorted(deadlines):
            src = lines[i - 1] if i - 1 < len(lines) else ""
            table.append((b, f, i, src))

    if not args.table:
        print("marked files in buckets:", args.buckets, "| inplace:", args.inplace)
        print("dead lines:", len(table))
        return

    order = {"CORE": 0, "MIXED": 1, "EXEC": 2, "DEV": 3, "TEST": 4}
    table.sort(key=lambda r: (order.get(r[0], 9), r[1], r[2]))

    counts = collections.Counter(r[0] for r in table)
    with open(args.table, "w") as w:
        w.write("# runtime-never-executed lines (count==0), grouped by bucket\n")
        w.write("# " + "  ".join(f"{k}={counts[k]}" for k in
                                 ("CORE", "MIXED", "EXEC", "DEV", "TEST")) + "\n\n")
        cur = None
        for b, f, i, src in table:
            if b != cur:
                w.write(f"\n##### bucket {b} #####\n")
                cur = b
            w.write(f"{f}:{i}: {src.strip()}\n")

    print("annotated tree:", args.out)
    print("dead-line table:", args.table)
    print("counts:", dict(counts))


if __name__ == "__main__":
    main()
