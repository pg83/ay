#!/usr/bin/env python3
"""Turn a structured validation result into a passing/failing stamp target."""

from __future__ import annotations

import argparse
from pathlib import Path

from validation_lib import PASSING_STATUSES, format_result_lines, read_result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("result")
    parser.add_argument("stamp")
    args = parser.parse_args()
    result = read_result(args.result)
    for line in format_result_lines(result):
        print(line, flush=True)
    if result["status"] not in PASSING_STATUSES:
        return 1
    stamp = Path(args.stamp)
    stamp.parent.mkdir(parents=True, exist_ok=True)
    stamp.touch()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
