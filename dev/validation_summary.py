#!/usr/bin/env python3
"""Aggregate independently cached validation case results."""

from __future__ import annotations

import argparse
from pathlib import Path

from validation_lib import format_result_lines, make_summary, read_result, write_json_atomic


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--json", required=True)
    parser.add_argument("--text", required=True)
    parser.add_argument("results", nargs="+")
    args = parser.parse_args()
    results = [read_result(path) for path in args.results]
    summary = make_summary(results)
    write_json_atomic(args.json, summary)
    lines = [line for result in sorted(results, key=lambda item: item["case"]) for line in format_result_lines(result)]
    counts = summary["counts"]
    lines.append(
        f"validation: OK={counts['OK']} XFAIL={counts['XFAIL']} FAIL={counts['FAIL']}"
    )
    text = "\n".join(lines) + "\n"
    destination = Path(args.text)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(text, encoding="utf-8")
    print(text, end="", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
