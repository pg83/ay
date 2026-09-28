#!/usr/bin/env python3
"""Pack and merge Go binary-coverage data for `./build -Dcoverage unit`.

`pack` archives one GOCOVERDIR into a single tar so a test node has a file
output. `merge` unpacks every archive, merges them with `go tool covdata`,
writes the textfmt profile, and prints a per-file statement coverage table
with a total to stdout and to the report file.
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


def report(profile: str) -> str:
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
    emit("total", sum(stmts.values()), sum(covered.values()))
    return "\n".join(lines) + "\n"


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
    text = report(args.out)
    Path(args.report).write_text(text, encoding="utf-8")
    sys.stdout.write(text)


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
    merge_parser.add_argument("archives", nargs="+")
    merge_parser.set_defaults(run=merge)
    args = parser.parse_args()
    args.run(args)


if __name__ == "__main__":
    main()
