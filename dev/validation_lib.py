"""Shared structured-result helpers for the build-integrated validator."""

from __future__ import annotations

import json
import os
import tempfile
from pathlib import Path


RESULT_SCHEMA = 1
SUMMARY_SCHEMA = 1
PASSING_STATUSES = frozenset({"OK", "XFAIL"})


def write_json_atomic(path: str | os.PathLike[str], value: object) -> None:
    destination = Path(path)
    destination.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(
        prefix="." + destination.name + ".",
        dir=destination.parent,
    )
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, indent=2, sort_keys=True)
            stream.write("\n")
        os.replace(temporary, destination)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass


def read_result(path: str | os.PathLike[str]) -> dict:
    with open(path, encoding="utf-8") as stream:
        result = json.load(stream)
    if result.get("schema") != RESULT_SCHEMA:
        raise ValueError(f"unsupported validation result schema in {path}")
    if not isinstance(result.get("case"), str) or not result["case"]:
        raise ValueError(f"validation result has no case id: {path}")
    if result.get("status") not in {"OK", "XFAIL", "FAIL"}:
        raise ValueError(f"invalid validation status in {path}: {result.get('status')!r}")
    return result


def normalized_node_parity_counts(
    left_path: str | os.PathLike[str],
    right_path: str | os.PathLike[str],
    *,
    sample_limit: int = 40,
    sample_width: int = 1000,
) -> tuple[dict[str, int], list[str]]:
    """Merge two sorted JSONL streams, returning exact parity and a small diff."""
    exact = left_only = right_only = left_total = right_total = 0
    sample: list[str] = []

    def record(prefix: str, line: str) -> None:
        if len(sample) < sample_limit:
            sample.append(prefix + line.rstrip("\n")[:sample_width])

    with open(left_path, encoding="utf-8") as left, open(right_path, encoding="utf-8") as right:
        left_line = left.readline() or None
        right_line = right.readline() or None
        if left_line is not None:
            left_total += 1
        if right_line is not None:
            right_total += 1

        while left_line is not None and right_line is not None:
            if left_line == right_line:
                exact += 1
                left_line = left.readline() or None
                right_line = right.readline() or None
                if left_line is not None:
                    left_total += 1
                if right_line is not None:
                    right_total += 1
            elif left_line < right_line:
                left_only += 1
                record("- ", left_line)
                left_line = left.readline() or None
                if left_line is not None:
                    left_total += 1
            else:
                right_only += 1
                record("+ ", right_line)
                right_line = right.readline() or None
                if right_line is not None:
                    right_total += 1

        while left_line is not None:
            left_only += 1
            record("- ", left_line)
            left_line = left.readline() or None
            if left_line is not None:
                left_total += 1

        while right_line is not None:
            right_only += 1
            record("+ ", right_line)
            right_line = right.readline() or None
            if right_line is not None:
                right_total += 1

    return {
        "matched": exact,
        "our_only": left_only,
        "ref_only": right_only,
        "our_total": left_total,
        "ref_total": right_total,
    }, sample


def format_result_lines(result: dict) -> list[str]:
    case = result["case"]
    status = result["status"]
    lines: list[str] = []
    parity = result.get("parity")
    if parity is not None:
        lines.append(
            f"[{case}] exact normalized-node parity: "
            f"matched={parity['matched']} our_only={parity['our_only']} "
            f"ref_only={parity['ref_only']} our_total={parity['our_total']} "
            f"ref_total={parity['ref_total']}"
        )
    detail = result.get("detail")
    suffix = f" ({detail})" if detail else ""
    if status == "XFAIL":
        suffix = " (not gating)" + (f" — {detail}" if detail else "")
    lines.append(f"[{case}] {status}{suffix}")
    return lines


def make_summary(results: list[dict]) -> dict:
    cases = {result["case"]: result for result in sorted(results, key=lambda item: item["case"])}
    counts = {
        status: sum(result["status"] == status for result in results)
        for status in ("OK", "XFAIL", "FAIL")
    }
    return {"schema": SUMMARY_SCHEMA, "counts": counts, "cases": cases}


def read_summary(path: str | os.PathLike[str]) -> dict:
    with open(path, encoding="utf-8") as stream:
        summary = json.load(stream)
    if summary.get("schema") != SUMMARY_SCHEMA or not isinstance(summary.get("cases"), dict):
        raise ValueError(f"invalid validation summary: {path}")
    return summary
