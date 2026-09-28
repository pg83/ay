#!/usr/bin/env python3
"""Pack and merge Go binary-coverage data for `./build -Dcoverage unit`.

`pack` archives one GOCOVERDIR into a single tar so a test node has a file
output. `merge` unpacks every archive, merges them with `go tool covdata`,
and writes the textfmt profile. `combine` adds up the textfmt profiles of
several runs, such as the CI jobs on different platforms. Both print a
per-file statement coverage table with a total to stdout and to the report
file, and fail below the given floor.
"""

from __future__ import annotations

import argparse
import collections
import re
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path


BLOCK_RE = re.compile(r"^(?P<file>.+):\d+\.\d+,\d+\.\d+ (?P<stmts>\d+) (?P<count>\d+)$")


def pack(args: argparse.Namespace) -> None:
    with tarfile.open(args.out, "w") as archive:
        for path in sorted(Path(args.dir).iterdir()):
            archive.add(path, arcname=path.name)


def covdata(*command: str) -> None:
    subprocess.run(["go", "tool", "covdata", *command], check=True)


def report(profile: str) -> tuple[str, int, int]:
    stmts: dict[str, int] = collections.defaultdict(int)
    covered: dict[str, int] = collections.defaultdict(int)
    with open(profile, encoding="utf-8") as stream:
        for line in stream:
            match = BLOCK_RE.match(line.strip())
            if match is None:
                continue
            count = int(match["stmts"])
            stmts[match["file"]] += count
            if int(match["count"]) > 0:
                covered[match["file"]] += count
    width = max(len("total"), *(len(name) for name in stmts))
    row = f"{{:<{width}}} {{:>8}} {{:>8}} {{:>8}} {{:>7}}"
    lines = [row.format("file", "stmts", "covered", "missed", "cover")]

    def emit(name: str, total: int, hit: int) -> None:
        percent = 100.0 * hit / total if total else 0.0
        lines.append(row.format(name, total, hit, total - hit, f"{percent:.1f}%"))

    for name in sorted(stmts):
        emit(name, stmts[name], covered[name])
    total_stmts = sum(stmts.values())
    total_covered = sum(covered.values())
    emit("total", total_stmts, total_covered)
    return "\n".join(lines) + "\n", total_covered, total_stmts


def finish(profile: str, report_path: str, minimum: float) -> None:
    text, covered, total = report(profile)
    Path(report_path).write_text(text, encoding="utf-8")
    sys.stdout.write(text)
    if not total or 100 * covered < minimum * total:
        sys.exit(f"coverage {covered}/{total} statements is below {minimum}%")


def merge(args: argparse.Namespace) -> None:
    with tempfile.TemporaryDirectory(prefix="covdata-") as scratch:
        inputs = []
        for index, archive_path in enumerate(args.archives):
            directory = Path(scratch, str(index))
            directory.mkdir()
            with tarfile.open(archive_path) as archive:
                archive.extractall(directory, filter="data")
            inputs.append(str(directory))
        merged = Path(scratch, "merged")
        merged.mkdir()
        covdata("merge", "-i=" + ",".join(inputs), "-o", str(merged))
        covdata("textfmt", "-i=" + str(merged), "-o", args.out)
    finish(args.out, args.report, args.minimum)


def read_profile(path: str) -> dict[str, tuple[int, int]]:
    """A textfmt profile as {block: (statements, count)}."""
    blocks: dict[str, tuple[int, int]] = {}
    for line in Path(path).read_text(encoding="utf-8").splitlines():
        if not line or line.startswith("mode:"):
            continue
        where, stmts, count = line.rsplit(" ", 2)
        held = blocks.get(where, (0, 0))
        blocks[where] = (int(stmts), max(int(count), held[1]))
    return blocks


def blocks_by_file(blocks: dict[str, tuple[int, int]]) -> dict[str, set[str]]:
    files: dict[str, set[str]] = collections.defaultdict(set)
    for where in blocks:
        files[where.split(":")[0]].add(where)
    return files


def combine(args: argparse.Namespace) -> None:
    merged: dict[str, tuple[int, int]] = {}
    for path in args.profiles:
        blocks = read_profile(path)
        if not blocks:
            sys.exit(f"{path} carries no measured block")
        # A file compiled on several platforms has the same blocks everywhere,
        # so differing blocks mean the runs measured different sources. A file
        # only one platform compiles is carried over as it stands.
        held = blocks_by_file(merged)
        for name, found in blocks_by_file(blocks).items():
            if name in held and held[name] != found:
                sys.exit(f"{path} was measured on other sources: {name} has other blocks")
        total = sum(stmts for stmts, _ in blocks.values())
        hit = sum(stmts for stmts, count in blocks.values() if count)
        print(f"{path}: {100.0 * hit / total:.1f}% ({hit}/{total} statements)")
        for where, (stmts, count) in blocks.items():
            merged[where] = (stmts, max(count, merged.get(where, (0, 0))[1]))
    Path(args.out).write_text(
        "mode: atomic\n" + "".join(f"{where} {stmts} {count}\n" for where, (stmts, count) in sorted(merged.items())),
        encoding="utf-8",
    )
    finish(args.out, args.report, args.minimum)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    pack_parser = commands.add_parser("pack", help="archive one GOCOVERDIR into a tar file")
    pack_parser.add_argument("--dir", required=True)
    pack_parser.add_argument("--out", required=True)
    pack_parser.set_defaults(run=pack)
    merge_parser = commands.add_parser("merge", help="merge archives into a textfmt profile")
    merge_parser.add_argument("--out", required=True)
    merge_parser.add_argument("--report", required=True)
    merge_parser.add_argument("--minimum", type=float, required=True)
    merge_parser.add_argument("archives", nargs="+")
    merge_parser.set_defaults(run=merge)
    combine_parser = commands.add_parser("combine", help="add up textfmt profiles of several runs")
    combine_parser.add_argument("--out", required=True)
    combine_parser.add_argument("--report", required=True)
    combine_parser.add_argument("--minimum", type=float, required=True)
    combine_parser.add_argument("profiles", nargs="+")
    combine_parser.set_defaults(run=combine)
    args = parser.parse_args()
    args.run(args)


if __name__ == "__main__":
    main()
