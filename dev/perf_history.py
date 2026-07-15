#!/usr/bin/env python3
"""Run the ay performance gate for every first-parent commit in a range.

Each selected commit is compared with its first parent.  The runner builds
detached revisions without touching the caller's worktree and keeps every
gate attempt, raw sample file, and complete stdout/stderr log.  Completed
pairs are skipped on restart.
"""

import argparse
import contextlib
import datetime
import hashlib
import importlib.util
import json
import math
import os
import pathlib
import signal
import shlex
import shutil
import subprocess
import sys
from dataclasses import asdict, dataclass


SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
PERF_PATH = SCRIPT_DIR / "perf.py"
PERF_SPEC = importlib.util.spec_from_file_location("ay_perf", PERF_PATH)
PERF = importlib.util.module_from_spec(PERF_SPEC)
PERF_SPEC.loader.exec_module(PERF)

SCHEMA_VERSION = 1
COMPLETED_GATE_CODES = {0, 2, 3}


@dataclass(frozen=True)
class CommitPair:
    index: int
    left: str
    right: str
    date: str
    subject: str

    @property
    def name(self):
        return f"{self.index:03d}-{self.right[:12]}"


def _now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def _atomic_json(path, value):
    path = pathlib.Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + ".tmp")

    with tmp.open("w", encoding="utf-8") as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write("\n")

    os.replace(tmp, path)


def _run_text(command, cwd=None):
    return subprocess.run(
        command,
        cwd=cwd,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=True,
    ).stdout.strip()


def _git(repo, *args):
    return _run_text(["git", "-C", str(repo), *args])


def history_pairs(repo, since, until):
    raw = _git(
        repo,
        "log",
        "--first-parent",
        "--reverse",
        f"--since={since}",
        "--format=%H%x00%P%x00%cI%x00%s",
        until,
    )
    result = []

    if not raw:
        return result

    for index, line in enumerate(raw.splitlines(), 1):
        fields = line.split("\0", 3)

        if len(fields) != 4:
            raise RuntimeError(f"cannot parse git log record: {line!r}")

        commit, parents, date, subject = fields
        parent_list = parents.split()

        if not parent_list:
            raise RuntimeError(f"commit {commit} has no parent")

        result.append(CommitPair(index, parent_list[0], commit, date, subject))

    return result


def _sha256_file(path):
    digest = hashlib.sha256()

    with pathlib.Path(path).open("rb") as stream:
        while block := stream.read(1024 * 1024):
            digest.update(block)

    return digest.hexdigest()


def _pair_seed(pair):
    digest = hashlib.sha256(f"{pair.left}\0{pair.right}".encode()).digest()

    return int.from_bytes(digest[:8], "little")


def _capture(command):
    try:
        return _run_text(command)
    except (OSError, subprocess.CalledProcessError) as error:
        return f"unavailable: {error}"


def _manifest_config(args, pairs, until):
    return {
        "schema": SCHEMA_VERSION,
        "repo": str(args.repo),
        "since": args.since,
        "until": until,
        "cpu": args.cpu,
        "cwd": str(args.cwd),
        "command": args.command,
        "perf_options": {
            "min_runs": args.min_runs,
            "max_runs": args.max_runs,
            "growth": args.growth,
            "alpha": args.alpha,
            "power": args.power,
            "target_effect": args.target_effect,
            "warmup_cycles": args.warmup_cycles,
            "exact_max_blocks": args.exact_max_blocks,
            "permutations": args.permutations,
        },
        "pairs": [asdict(pair) for pair in pairs],
    }


def _prepare_manifest(args, pairs, until):
    output = args.output
    output.mkdir(parents=True, exist_ok=True)
    harness = output / "harness"
    harness.mkdir(exist_ok=True)
    manifest_path = output / "manifest.json"
    config = _manifest_config(args, pairs, until)

    if manifest_path.exists():
        with manifest_path.open(encoding="utf-8") as stream:
            manifest = json.load(stream)

        stored = {key: manifest[key] for key in config}

        if stored != config:
            raise RuntimeError(
                "existing output directory has a different benchmark configuration"
            )

        return manifest

    perf_snapshot = harness / "perf.py"
    calibration_snapshot = harness / "calibration.json"
    shutil.copy2(PERF_PATH, perf_snapshot)
    shutil.copy2(args.calibration_json, calibration_snapshot)
    manifest = {
        **config,
        "created_at": _now(),
        "host": {
            "uname": _capture(["uname", "-a"]),
            "go": _capture(["go", "version"]),
            "git": _capture(["git", "--version"]),
            "perf": _capture(["perf", "--version"]),
        },
        "harness": {
            "perf": str(perf_snapshot),
            "perf_sha256": _sha256_file(perf_snapshot),
            "calibration": str(calibration_snapshot),
            "calibration_sha256": _sha256_file(calibration_snapshot),
        },
    }
    _atomic_json(manifest_path, manifest)

    return manifest


def _binary_path(output, revision):
    return output / "binaries" / revision / "ay"


@contextlib.contextmanager
def _build_worktree(repo, path, revision):
    path = pathlib.Path(path)

    if path.exists():
        subprocess.run(
            ["git", "-C", str(repo), "worktree", "remove", "--force", str(path)],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )

    subprocess.run(
        ["git", "-C", str(repo), "worktree", "prune"],
        stdin=subprocess.DEVNULL,
        check=True,
    )
    subprocess.run(
        ["git", "-C", str(repo), "worktree", "add", "--detach", str(path), revision],
        stdin=subprocess.DEVNULL,
        check=True,
    )

    try:
        yield path
    finally:
        subprocess.run(
            ["git", "-C", str(repo), "worktree", "remove", "--force", str(path)],
            stdin=subprocess.DEVNULL,
            check=False,
        )


def _tee_process(command, cwd, log_path, env=None):
    log_path = pathlib.Path(log_path)
    log_path.parent.mkdir(parents=True, exist_ok=True)

    with log_path.open("w", encoding="utf-8") as log:
        log.write(f"started: {_now()}\n")
        log.write(f"cwd: {cwd}\n")
        log.write(f"command: {shlex.join(str(arg) for arg in command)}\n")
        log.write("\n")
        log.flush()
        process = subprocess.Popen(
            command,
            cwd=cwd,
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            errors="replace",
            bufsize=1,
            start_new_session=True,
        )

        try:
            for line in process.stdout:
                sys.stdout.write(line)
                sys.stdout.flush()
                log.write(line)
                log.flush()

            code = process.wait()
        except BaseException:
            os.killpg(process.pid, signal.SIGINT)

            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()

            log.write(f"\ninterrupted: {_now()}\n")
            log.flush()

            raise

        log.write(f"\nfinished: {_now()}\nexit_code: {code}\n")

        return code


def _build_revisions(args, revisions):
    missing = [rev for rev in revisions if not os.access(_binary_path(args.output, rev), os.X_OK)]

    if not missing:
        return

    worktree_path = args.output / "build-worktree"

    with _build_worktree(args.repo, worktree_path, missing[0]) as worktree:
        for position, revision in enumerate(missing, 1):
            binary = _binary_path(args.output, revision)
            metadata_path = binary.parent / "build.json"
            log_path = binary.parent / "build.log"
            binary.parent.mkdir(parents=True, exist_ok=True)
            print(f"[build {position}/{len(missing)}] {revision[:12]}", flush=True)
            subprocess.run(
                ["git", "checkout", "--detach", "--quiet", revision],
                cwd=worktree,
                stdin=subprocess.DEVNULL,
                check=True,
            )
            tmp = binary.with_name("ay.tmp")
            started = _now()
            code = _tee_process(["go", "build", "-o", str(tmp), "."], worktree, log_path)
            metadata = {
                "revision": revision,
                "started_at": started,
                "finished_at": _now(),
                "exit_code": code,
            }

            if code == 0:
                os.replace(tmp, binary)
                metadata["sha256"] = _sha256_file(binary)
                metadata["size"] = binary.stat().st_size
            elif tmp.exists():
                tmp.unlink()

            _atomic_json(metadata_path, metadata)

            if code != 0:
                raise RuntimeError(f"go build failed for {revision}; see {log_path}")


def _latest_completed_attempt(pair_dir):
    attempts = sorted(pair_dir.glob("attempt-*")) if pair_dir.exists() else []

    for attempt in reversed(attempts):
        result_path = attempt / "result.json"

        if not result_path.exists():
            continue

        with result_path.open(encoding="utf-8") as stream:
            result = json.load(stream)

        if result.get("exit_code") in COMPLETED_GATE_CODES:
            return attempt, result

    return None, None


def _next_attempt(pair_dir):
    numbers = []

    for path in pair_dir.glob("attempt-*"):
        try:
            numbers.append(int(path.name.removeprefix("attempt-")))
        except ValueError:
            pass

    attempt = pair_dir / f"attempt-{max(numbers, default=0) + 1:03d}"
    attempt.mkdir(parents=True)

    return attempt


def _effect_summary(samples, seed, options):
    metrics = (*PERF.BASE_METRICS, *PERF.PERF_METRICS)
    ratios = {metric: PERF.raw_block_log_ratios(samples, metric) for metric in metrics}
    blocks = len(ratios["wall"])
    runs = 2 * blocks

    if blocks == 0:
        return {"runs_per_side": 0, "blocks": 0, "metrics": {}}

    test_seed = seed ^ (runs * 0x9E3779B97F4A7C15)
    metric_results = {}

    for offset, metric in enumerate(metrics):
        values = ratios[metric]
        faster = PERF.paired_permutation(
            values,
            options["exact_max_blocks"],
            options["permutations"],
            test_seed ^ (offset * 0xD1B54A32D192ED03),
        )
        metric_results[metric] = {
            "effect_pct": math.expm1(faster.mean_log_ratio) * 100,
            "p_faster": faster.p_right_faster,
        }

    regression = {}

    for offset, metric in enumerate(PERF.DECISION_METRICS):
        slower = PERF.paired_permutation(
            [-value for value in ratios[metric]],
            options["exact_max_blocks"],
            options["permutations"],
            test_seed ^ (offset * 0x94D049BB133111EB) ^ 0xA24BAED4963EE407,
        )
        metric_results[metric]["p_slower"] = slower.p_right_faster
        regression[metric] = slower.p_right_faster

    checkpoints = PERF.decision_checkpoints(
        options["min_runs"], options["max_runs"], options["growth"]
    )
    decision_alpha = options["alpha"] / len(checkpoints)
    improved = PERF.holm_rejections(
        {metric: metric_results[metric]["p_faster"] for metric in PERF.DECISION_METRICS},
        decision_alpha,
    )
    regressed = PERF.holm_rejections(regression, decision_alpha)

    return {
        "runs_per_side": runs,
        "blocks": blocks,
        "metrics": metric_results,
        "improved": sorted(improved),
        "regressed": sorted(regressed),
    }


def _run_pair(args, manifest, pair):
    pair_dir = args.output / "pairs" / pair.name
    pair_dir.mkdir(parents=True, exist_ok=True)
    _atomic_json(pair_dir / "pair.json", asdict(pair))
    completed_attempt, completed = _latest_completed_attempt(pair_dir)

    if completed is not None:
        print(
            f"[pair {pair.index:03d}] {pair.right[:12]} already complete: "
            f"{completed['status']} ({completed_attempt.name})",
            flush=True,
        )

        return completed

    attempt = _next_attempt(pair_dir)
    left = _binary_path(args.output, pair.left)
    right = _binary_path(args.output, pair.right)
    samples = attempt / "samples.json"
    log = attempt / "perf.log"
    seed = _pair_seed(pair)
    options = manifest["perf_options"]
    gate = pathlib.Path(manifest["harness"]["perf"])
    calibration = pathlib.Path(manifest["harness"]["calibration"])
    command = [
        sys.executable,
        str(gate),
        "--left",
        str(left),
        "--right",
        str(right),
        "--cwd",
        str(args.cwd),
        "--cpu",
        str(args.cpu),
        "--calibration-json",
        str(calibration),
        "--json",
        str(samples),
        "--seed",
        str(seed),
        "--min-runs",
        str(options["min_runs"]),
        "--max-runs",
        str(options["max_runs"]),
        "--growth",
        str(options["growth"]),
        "--alpha",
        str(options["alpha"]),
        "--power",
        str(options["power"]),
        "--target-effect",
        str(options["target_effect"]),
        "--warmup-cycles",
        str(options["warmup_cycles"]),
        "--exact-max-blocks",
        str(options["exact_max_blocks"]),
        "--permutations",
        str(options["permutations"]),
        "--",
        *args.command,
    ]
    print(
        f"[pair {pair.index:03d}/{len(manifest['pairs'])}] "
        f"{pair.left[:12]}..{pair.right[:12]} {pair.subject}",
        flush=True,
    )
    started = _now()
    env = os.environ.copy()
    env["PYTHONUNBUFFERED"] = "1"

    try:
        code = _tee_process(command, args.cwd, log, env)
    except KeyboardInterrupt:
        result = {
            "status": "interrupted",
            "exit_code": 130,
            "started_at": started,
            "finished_at": _now(),
            "seed": seed,
            "log": str(log),
            "samples": str(samples),
        }
        _atomic_json(attempt / "result.json", result)

        raise

    if samples.exists():
        with samples.open(encoding="utf-8") as stream:
            raw_samples = json.load(stream)
    else:
        raw_samples = []

    status = {0: "improved", 2: "inconclusive", 3: "regressed"}.get(code, "failed")
    result = {
        "status": status,
        "exit_code": code,
        "started_at": started,
        "finished_at": _now(),
        "seed": seed,
        "log": str(log),
        "samples": str(samples),
        "left_binary_sha256": _sha256_file(left),
        "right_binary_sha256": _sha256_file(right),
        "binary_identical": _sha256_file(left) == _sha256_file(right),
        "summary": _effect_summary(raw_samples, seed, options),
    }
    _atomic_json(attempt / "result.json", result)

    if code not in COMPLETED_GATE_CODES:
        raise RuntimeError(f"perf gate failed for {pair.right}; see {log}")

    return result


def _collect_results(output, pairs):
    rows = []

    for pair in pairs:
        pair_dir = output / "pairs" / pair.name
        attempt, result = _latest_completed_attempt(pair_dir)

        if result is None:
            rows.append({"pair": asdict(pair), "status": "pending"})

            continue

        rows.append(
            {
                "pair": asdict(pair),
                "attempt": str(attempt),
                **result,
            }
        )

    return rows


def _fmt_effect(row, metric):
    summary = row.get("summary", {})
    value = summary.get("metrics", {}).get(metric, {}).get("effect_pct")

    return "—" if value is None else f"{value:+.3f}%"


def _write_reports(output, rows):
    _atomic_json(output / "summary.json", rows)
    headers = ("#", "commit", "status", "n", "wall", "task", "cycles", "instructions", "subject")
    lines = [
        "| " + " | ".join(headers) + " |",
        "|" + "|".join("---" for _ in headers) + "|",
    ]

    for row in rows:
        pair = row["pair"]
        summary = row.get("summary", {})
        lines.append(
            "| "
            + " | ".join(
                (
                    str(pair["index"]),
                    pair["right"][:12],
                    row["status"],
                    str(summary.get("runs_per_side", "—")),
                    _fmt_effect(row, "wall"),
                    _fmt_effect(row, "task_clock"),
                    _fmt_effect(row, "cycles"),
                    _fmt_effect(row, "instructions"),
                    pair["subject"].replace("|", "\\|"),
                )
            )
            + " |"
        )

    report = "\n".join(lines) + "\n"
    (output / "summary.md").write_text(report, encoding="utf-8")

    print(report)


def _split_args(argv):
    if "--" in argv:
        separator = argv.index("--")

        return argv[:separator], argv[separator + 1 :]

    if "--help" in argv or "-h" in argv or "--report-only" in argv:
        return argv, []

    raise ValueError("missing `--` before the command passed to the binaries")


def _parse_args(argv):
    own, command = _split_args(argv)
    parser = argparse.ArgumentParser(
        description="Compare every first-parent commit with its parent using dev/perf.py.",
        epilog=(
            "Example: ./dev/perf_history.py --since '2026-07-12 00:00:00 +0300' "
            "--until HEAD --output .out/perf-history --cpu 20 "
            "--calibration-json /tmp/aa.json -- --target-platform ... target"
        ),
    )
    parser.add_argument("--repo", type=pathlib.Path, default=pathlib.Path.cwd())
    parser.add_argument("--since")
    parser.add_argument("--until", default="HEAD")
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--cwd", type=pathlib.Path)
    parser.add_argument("--cpu", type=int)
    parser.add_argument("--calibration-json", type=pathlib.Path)
    parser.add_argument("--min-runs", type=int, default=30)
    parser.add_argument("--max-runs", type=int, default=120)
    parser.add_argument("--growth", type=float, default=1.5)
    parser.add_argument("--alpha", type=float, default=0.05)
    parser.add_argument("--power", type=float, default=0.8)
    parser.add_argument("--target-effect", type=float, default=0.1)
    parser.add_argument("--warmup-cycles", type=int, default=1)
    parser.add_argument("--exact-max-blocks", type=int, default=20)
    parser.add_argument("--permutations", type=int, default=200_000)
    parser.add_argument("--max-pairs", type=int, help="run only the first N pairs")
    parser.add_argument("--report-only", action="store_true")
    args = parser.parse_args(own)
    args.repo = args.repo.resolve()
    args.output = args.output.resolve()
    args.cwd = (args.cwd or args.repo).resolve()
    args.command = command

    if args.report_only:
        return args

    if args.since is None:
        parser.error("--since is required")
    if args.cpu is None:
        parser.error("--cpu is required")
    if args.calibration_json is None:
        parser.error("--calibration-json is required")

    args.calibration_json = args.calibration_json.resolve()

    if not args.repo.is_dir():
        parser.error(f"repository not found: {args.repo}")
    if not args.cwd.is_dir():
        parser.error(f"working directory not found: {args.cwd}")
    if not args.calibration_json.is_file():
        parser.error(f"calibration JSON not found: {args.calibration_json}")
    if not args.command:
        parser.error("the command after `--` is empty")
    if args.max_pairs is not None and args.max_pairs < 1:
        parser.error("--max-pairs must be positive")

    try:
        PERF.decision_checkpoints(args.min_runs, args.max_runs, args.growth)
    except ValueError as error:
        parser.error(str(error))

    if not hasattr(os, "sched_getaffinity"):
        parser.error("CPU affinity is not supported on this platform")
    if args.cpu not in os.sched_getaffinity(0):
        parser.error(f"CPU {args.cpu} is outside the allowed affinity set")
    if shutil.which("perf") is None:
        parser.error("perf is required")

    return args


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv

    try:
        args = _parse_args(argv)

        if args.report_only:
            manifest_path = args.output / "manifest.json"

            with manifest_path.open(encoding="utf-8") as stream:
                manifest = json.load(stream)

            pairs = [CommitPair(**item) for item in manifest["pairs"]]
            rows = _collect_results(args.output, pairs)
            _write_reports(args.output, rows)

            return 0

        until = _git(args.repo, "rev-parse", args.until)
        pairs = history_pairs(args.repo, args.since, until)

        if not pairs:
            raise RuntimeError("the selected history contains no commits")

        manifest = _prepare_manifest(args, pairs, until)
        selected = pairs[: args.max_pairs] if args.max_pairs is not None else pairs
        revisions = []

        for pair in selected:
            for revision in (pair.left, pair.right):
                if revision not in revisions:
                    revisions.append(revision)

        _build_revisions(args, revisions)

        for pair in selected:
            _run_pair(args, manifest, pair)

        rows = _collect_results(args.output, pairs)
        _write_reports(args.output, rows)

        return 0
    except KeyboardInterrupt:
        print("\nperf_history.py: interrupted; completed attempts are resumable", file=sys.stderr)

        return 130
    except (OSError, RuntimeError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        print(f"perf_history.py: {error}", file=sys.stderr)

        return 1


if __name__ == "__main__":
    sys.exit(main())
