#!/usr/bin/env python3
"""Compatibility CLI for the build-integrated validation graph.

The build runner owns resource caching, case scheduling and dependencies.
This wrapper preserves the historical CLI and text output for humans and old
automation while reading structured per-case results from the build root.
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from pathlib import Path

from validation_lib import format_result_lines, read_result, read_summary


SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parent
CONFIG_PATH = SCRIPT_DIR / "config.json"


def slug(value: str) -> str:
    result = re.sub(r"[^A-Za-z0-9_]+", "_", value).strip("_")
    if not result:
        raise ValueError(f"cannot make target name from {value!r}")
    return result


def load_config() -> list[dict]:
    with CONFIG_PATH.open(encoding="utf-8") as stream:
        cases = json.load(stream)
    for case in cases:
        command = case.get("command", [])
        for flag in ("--target-platform", "--host-platform"):
            if not any(token == flag or token.startswith(flag + "=") for token in command):
                raise SystemExit(
                    f"config.json: case {case.get('id')!r} is missing {flag}; "
                    "both platforms must be pinned"
                )
    return cases


def parse_arguments(case_ids: set[str]) -> tuple[bool, str | None, list[str], Path | None]:
    warm = False
    cache = None
    selected: list[str] = []
    out_dir = None
    arguments = sys.argv[1:]
    index = 0
    while index < len(arguments):
        argument = arguments[index]
        if argument == "--warm":
            warm = True
        elif argument == "--cache":
            index += 1
            if index >= len(arguments):
                raise SystemExit("validate.py: --cache requires a directory")
            cache = arguments[index]
        elif argument.startswith("--cache="):
            cache = argument.split("=", 1)[1]
        elif argument in case_ids:
            selected.append(argument)
        elif argument.startswith("-"):
            raise SystemExit(f"validate.py: unknown option {argument}")
        elif out_dir is None:
            out_dir = Path(argument).resolve()
        else:
            raise SystemExit(f"validate.py: unexpected argument {argument}")
        index += 1
    return warm, cache, selected, out_dir


def run_build(build_root: Path, cache_root: Path, targets: list[str]) -> int:
    command = [
        str(REPO_ROOT / "build"),
        "-B", str(build_root),
        "--cache-dir", str(cache_root),
        "-k",
        *targets,
    ]
    print("[validate] $ " + " ".join(command), flush=True)
    return subprocess.run(command, cwd=REPO_ROOT).returncode


def main() -> int:
    cases = load_config()
    by_id = {case["id"]: case for case in cases}
    warm, cache_option, selected, out_dir = parse_arguments(set(by_id))

    configured_build_root = os.environ.get("AY_BUILD_ROOT")
    if configured_build_root:
        build_root = Path(configured_build_root).resolve()
    elif out_dir is not None:
        build_root = out_dir / "build"
    else:
        build_root = REPO_ROOT / ".build"

    configured_cache = os.environ.get("AY_BUILD_CACHE_DIR")
    cache_root = Path(configured_cache or cache_option or build_root).resolve()
    build_root.mkdir(parents=True, exist_ok=True)
    cache_root.mkdir(parents=True, exist_ok=True)

    if warm:
        rc = run_build(build_root, cache_root, ["validation_resources"])
        if rc == 0:
            print(
                f"validate.py: warmed {len(cases)} cases into build cache {cache_root}",
                flush=True,
            )
        return rc

    targets = (
        [f"validation_result_{slug(case_id)}" for case_id in selected]
        if selected
        else ["validation_results"]
    )
    rc = run_build(build_root, cache_root, targets)
    if rc != 0:
        print("validate.py: validation build graph failed", flush=True)
        return 1

    if selected:
        results = [
            read_result(build_root / "validation" / "cases" / case_id / "result.json")
            for case_id in selected
        ]
    else:
        summary = read_summary(build_root / "validation" / "summary.json")
        results = [summary["cases"][case_id] for case_id in sorted(summary["cases"])]

    failed = False
    for result in results:
        for line in format_result_lines(result):
            print(line, flush=True)
        failed = failed or result["status"] == "FAIL"

    if failed:
        print("validate.py: failures above", flush=True)
        return 1
    print("validate.py: all gating cases byte-exact", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
