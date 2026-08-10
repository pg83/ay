#!/usr/bin/env python3
"""Execute one configured graph-equivalence validation case."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import shlex
import shutil
import subprocess
import tempfile
import time
from pathlib import Path

from validation_lib import (
    RESULT_SCHEMA,
    format_result_lines,
    normalized_node_parity_counts,
    write_json_atomic,
)


TOOLCHAIN_ENV_VARS = (
    "CC", "CXX", "CPP", "LD", "AR", "NM", "RANLIB", "STRIP", "OBJCOPY",
    "CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS", "LDLIBS",
    "CGO_CFLAGS", "CGO_CXXFLAGS", "CGO_CPPFLAGS", "CGO_LDFLAGS",
    "NIX_CFLAGS_COMPILE", "NIX_CFLAGS_LINK", "NIX_LDFLAGS",
)


def clean_env() -> dict[str, str]:
    return {key: value for key, value in os.environ.items() if key not in TOOLCHAIN_ENV_VARS}


def resource_id(url: str) -> str:
    return url.rstrip("/").rsplit("/", 1)[-1]


def resolve_slice_root(directory: Path) -> Path:
    if (directory / ".arcadia.root").exists():
        return directory
    for root, _directories, files in os.walk(directory):
        if ".arcadia.root" in files:
            return Path(root)
    raise RuntimeError(f"no .arcadia.root below extracted slice {directory}")


def resolve_reference_graph(directory: Path) -> Path:
    direct = directory / "graph.fuse.json"
    if direct.is_file():
        return direct
    candidates = sorted(directory.rglob("*.json"))
    if not candidates:
        raise RuntimeError(f"no reference JSON below extracted resource {directory}")
    return candidates[0]


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        while chunk := stream.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


class CaseRunner:
    def __init__(self, ay: Path, spec: dict, output: Path, timeout: int):
        self.ay = ay
        self.spec = spec
        self.output = output
        self.timeout = timeout
        self.log = None

    def run_command(self, command: list[str], *, cwd: Path, stdout: Path | None = None) -> tuple[int, float]:
        assert self.log is not None
        self.log.write("$ cd " + shlex.quote(str(cwd)) + " && ")
        self.log.write(" ".join(shlex.quote(item) for item in command) + "\n")
        self.log.flush()
        started = time.monotonic()
        try:
            if stdout is None:
                result = subprocess.run(
                    command,
                    cwd=cwd,
                    env=clean_env(),
                    stdout=self.log,
                    stderr=subprocess.STDOUT,
                    timeout=self.timeout,
                )
            else:
                with stdout.open("wb") as stream:
                    result = subprocess.run(
                        command,
                        cwd=cwd,
                        env=clean_env(),
                        stdout=stream,
                        stderr=self.log,
                        timeout=self.timeout,
                    )
            return result.returncode, time.monotonic() - started
        except subprocess.TimeoutExpired:
            self.log.write(f"TIMEOUT after {self.timeout}s\n")
            self.log.flush()
            return 124, time.monotonic() - started

    def phase_failure(self, phase: str, returncode: int, **extra: object) -> dict:
        allow_failure = self.spec.get("xfail") is True
        return {
            "schema": RESULT_SCHEMA,
            "case": self.spec["id"],
            "target": self.spec["target"],
            "status": "XFAIL" if allow_failure else "FAIL",
            "exact": False,
            "phase": phase,
            "detail": f"{phase} exited {returncode}",
            "returncode": returncode,
            **extra,
        }

    def execute(self, slice_archive: Path, graph_archive: Path) -> dict:
        self.output.mkdir(parents=True, exist_ok=True)
        with (self.output / "commands.txt").open("w", encoding="utf-8") as log:
            self.log = log
            with tempfile.TemporaryDirectory(prefix="ay-validation-") as temporary:
                work = Path(temporary)
                slice_dir = work / "slice"
                graph_dir = work / "reference"
                for phase, archive, destination, url in (
                    ("unpack-slice", slice_archive, slice_dir, self.spec["slice_url"]),
                    ("unpack-reference", graph_archive, graph_dir, self.spec["graph_url"]),
                ):
                    rc, _ = self.run_command([
                        str(self.ay), "fetch", "sandbox",
                        "--resource-id", resource_id(url),
                        "--resource-file", str(archive),
                        "--untar-to", str(destination),
                    ], cwd=work)
                    if rc != 0:
                        return self.phase_failure(phase, rc)

                source_root = resolve_slice_root(slice_dir)
                reference_raw = resolve_reference_graph(graph_dir)
                our_raw = work / "our.json"
                our_unsorted = work / "our.norm.unsorted"
                ref_unsorted = work / "ref.norm.unsorted"
                our_sorted = work / "our.norm.jsonl"
                ref_sorted = work / "ref.norm.jsonl"

                generation = [str(self.ay), *self.spec["command"][1:], "--source-root", str(source_root)]
                rc, generation_seconds = self.run_command(generation, cwd=work, stdout=our_raw)
                if rc != 0:
                    return self.phase_failure("generate", rc, generation_seconds=generation_seconds)

                commands = (
                    ("normalize-our", [str(self.ay), "dev", "dump", "normalize", "--in", str(our_raw), "--target", self.spec["target"], "--out", str(our_unsorted)]),
                    ("sort-our", [str(self.ay), "dev", "dump", "sort", "--in", str(our_unsorted), "--out", str(our_sorted)]),
                    ("normalize-reference", [str(self.ay), "dev", "dump", "normalize", "--in", str(reference_raw), "--target", self.spec["target"], "--out", str(ref_unsorted), "--ref-graph"]),
                    ("sort-reference", [str(self.ay), "dev", "dump", "sort", "--in", str(ref_unsorted), "--out", str(ref_sorted)]),
                )
                for phase, command in commands:
                    rc, _ = self.run_command(command, cwd=work)
                    if rc != 0:
                        return self.phase_failure(phase, rc, generation_seconds=generation_seconds)

                parity, sample = normalized_node_parity_counts(our_sorted, ref_sorted)
                exact = parity["our_only"] == 0 and parity["ref_only"] == 0
                xfail = self.spec.get("xfail", False)
                status = "OK" if exact else ("XFAIL" if xfail in (True, "auto") else "FAIL")
                detail = "byte-exact" if exact else "normalized graphs differ"

                budget = self.spec.get("budget")
                if budget is not None and generation_seconds > 1.2 * float(budget):
                    status = "FAIL"
                    detail = (
                        f"generation {generation_seconds:.2f}s exceeds "
                        f"1.2x budget {float(budget):.2f}s"
                    )

                if sample:
                    (self.output / "diff-sample.txt").write_text("\n".join(sample) + "\n", encoding="utf-8")

                return {
                    "schema": RESULT_SCHEMA,
                    "case": self.spec["id"],
                    "target": self.spec["target"],
                    "status": status,
                    "exact": exact,
                    "xfail": xfail,
                    "phase": "compare",
                    "detail": detail,
                    "generation_seconds": round(generation_seconds, 6),
                    "ay_sha256": file_sha256(self.ay),
                    "slice_resource": resource_id(self.spec["slice_url"]),
                    "graph_resource": resource_id(self.spec["graph_url"]),
                    "parity": parity,
                }


def decode_spec(encoded: str) -> dict:
    value = json.loads(base64.urlsafe_b64decode(encoded.encode()).decode())
    required = ("id", "target", "command", "slice_url", "graph_url", "xfail")
    missing = [key for key in required if key not in value]
    if missing:
        raise ValueError(f"validation case is missing: {', '.join(missing)}")
    return value


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--ay", required=True)
    parser.add_argument("--spec-base64", required=True)
    parser.add_argument("--slice-archive", required=True)
    parser.add_argument("--graph-archive", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--timeout", type=int, default=2400)
    args = parser.parse_args()
    output = Path(args.out)
    if output.exists():
        shutil.rmtree(output)
    runner = CaseRunner(Path(args.ay), decode_spec(args.spec_base64), output, args.timeout)
    result = runner.execute(Path(args.slice_archive), Path(args.graph_archive))
    write_json_atomic(output / "result.json", result)
    for line in format_result_lines(result):
        print(line, flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
