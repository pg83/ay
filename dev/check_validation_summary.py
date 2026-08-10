#!/usr/bin/env python3
"""Enforce a complete validation summary after every case has produced data."""

from __future__ import annotations

import argparse
from pathlib import Path

from validation_lib import format_result_lines, read_summary


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("summary")
    parser.add_argument("stamp")
    args = parser.parse_args()
    summary = read_summary(args.summary)
    failed = []
    for case in sorted(summary["cases"]):
        result = summary["cases"][case]
        for line in format_result_lines(result):
            print(line, flush=True)
        if result["status"] == "FAIL":
            failed.append(case)
    if failed:
        print("validation: failing cases: " + ", ".join(failed), flush=True)
        return 1
    stamp = Path(args.stamp)
    stamp.parent.mkdir(parents=True, exist_ok=True)
    stamp.touch()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
