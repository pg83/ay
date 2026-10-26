#!/usr/bin/env python3
"""Extract the reference graph JSON from a resource tarball: graph.fuse.json
at the top if present, otherwise the first *.json by path."""

from __future__ import annotations

import argparse
import os
import posixpath
import shutil
import sys
import tarfile


def clean(name: str) -> str:
    name = posixpath.normpath("/" + name).lstrip("/")
    return "" if name == "." else name


def pick(archive: str) -> str:
    candidates = []
    with tarfile.open(archive, mode="r|*") as stream:
        for member in stream:
            name = clean(member.name)
            if member.isreg() and name.endswith(".json"):
                candidates.append(name)
    if "graph.fuse.json" in candidates:
        return "graph.fuse.json"
    if not candidates:
        raise SystemExit(f"reference_graph: no graph JSON in {archive}")
    return sorted(candidates)[0]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--in", dest="source", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    wanted = pick(args.source)
    temporary = args.out + ".tmp"
    with tarfile.open(args.source, mode="r|*") as stream:
        for member in stream:
            if member.isreg() and clean(member.name) == wanted:
                with stream.extractfile(member) as data, open(temporary, "wb") as out:
                    shutil.copyfileobj(data, out, 1 << 20)
                break
    os.replace(temporary, args.out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
