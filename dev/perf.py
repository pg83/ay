#!/usr/bin/env python3
"""Sequential paired performance comparison for two ay binaries.

The arguments after ``--`` are passed to ``<binary> make -j0`` verbatim.
Measurements are made in randomized LRRL/RLLR blocks.  Each block contributes
one log(right/left) observation per metric, and decisions use a one-sided
paired sign-flip permutation test at precomputed group-sequential checkpoints.
"""

import argparse
import csv
import io
import json
import math
import os
import random
import resource
import shlex
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from statistics import NormalDist


PERF_EVENTS = ("task-clock:u", "cycles:u", "instructions:u")
PERF_METRICS = ("task_clock", "cycles", "instructions")
BASE_METRICS = ("wall", "user", "system")
DECISION_METRICS = ("wall", "task_clock", "cycles", "instructions")


@dataclass(frozen=True)
class PermutationResult:
    mean_log_ratio: float
    p_right_faster: float
    method: str
    permutations: int


@dataclass(frozen=True)
class Summary:
    runs: int
    blocks: int
    left_mean: float
    right_mean: float
    left_min: float
    right_min: float
    left_median: float
    right_median: float
    right_vs_left_pct: float
    geometric_right_vs_left_pct: float
    right_faster_blocks: int
    permutation: PermutationResult | None


def _right_faster_extreme(observed, candidate):
    tolerance = 1e-14 * max(1.0, abs(observed))

    return candidate <= observed + tolerance


def _exact_sign_flip_p(values):
    observed = math.fsum(values)
    extreme = 0

    def visit(pos, total):
        nonlocal extreme

        if pos == len(values):
            if _right_faster_extreme(observed, total):
                extreme += 1

            return

        value = values[pos]
        visit(pos + 1, total - value)
        visit(pos + 1, total + value)

    visit(0, 0.0)

    return extreme / (1 << len(values)), 1 << len(values)


def _sampled_sign_flip_p(values, permutations, seed):
    observed = math.fsum(values)
    rng = random.Random(seed)
    extreme = 0

    for _ in range(permutations):
        signs = rng.getrandbits(len(values))
        total = 0.0

        for value in values:
            total += value if signs & 1 else -value
            signs >>= 1

        if _right_faster_extreme(observed, total):
            extreme += 1

    # Including the observed assignment makes this a valid randomized p-value,
    # rather than a possibly zero estimate of the exact p-value.
    return (extreme + 1) / (permutations + 1), permutations


def paired_permutation(values, exact_max_blocks=20, permutations=200_000, seed=0):
    if not values:
        raise ValueError("paired permutation test needs a non-empty sample")
    if exact_max_blocks < 1:
        raise ValueError("exact_max_blocks must be positive")
    if permutations < 1:
        raise ValueError("permutations must be positive")

    if len(values) <= exact_max_blocks:
        p_value, count = _exact_sign_flip_p(values)
        method = "exact"
    else:
        p_value, count = _sampled_sign_flip_p(values, permutations, seed)
        method = "sampled"

    return PermutationResult(statistics.fmean(values), p_value, method, count)


def holm_rejections(p_values, alpha):
    rejected = set()
    ordered = sorted(p_values.items(), key=lambda item: item[1])

    for rank, (name, p_value) in enumerate(ordered):
        if p_value > alpha / (len(ordered) - rank):
            break

        rejected.add(name)

    return rejected


def minimum_detectable_effect(values, blocks, alpha, power):
    if len(values) < 2:
        return None
    if blocks < 1:
        raise ValueError("MDE needs a positive block count")

    z = NormalDist().inv_cdf(1 - alpha) + NormalDist().inv_cdf(power)
    log_effect = z * statistics.stdev(values) / math.sqrt(blocks)

    return math.expm1(log_effect) * 100


def required_runs_per_side(values, target_effect_pct, alpha, power):
    if len(values) < 2:
        return None

    target = math.log1p(target_effect_pct / 100)

    if target <= 0:
        raise ValueError("target effect must be positive")

    z = NormalDist().inv_cdf(1 - alpha) + NormalDist().inv_cdf(power)
    blocks = math.ceil((z * statistics.stdev(values) / target) ** 2)
    blocks = max(blocks, math.ceil(math.log2(1 / alpha)))

    return blocks * 2


def block_log_ratio(left, right):
    if len(left) != 2 or len(right) != 2:
        raise ValueError("a measurement block needs two runs per binary")

    return (math.log(right[0]) + math.log(right[1]) - math.log(left[0]) - math.log(left[1])) / 2


def summarize(left, right, block_log_ratios, permutation=None):
    left_mean = statistics.fmean(left)
    right_mean = statistics.fmean(right)
    mean_log_ratio = statistics.fmean(block_log_ratios)

    return Summary(
        runs=len(left),
        blocks=len(block_log_ratios),
        left_mean=left_mean,
        right_mean=right_mean,
        left_min=min(left),
        right_min=min(right),
        left_median=statistics.median(left),
        right_median=statistics.median(right),
        right_vs_left_pct=(right_mean - left_mean) / left_mean * 100,
        geometric_right_vs_left_pct=math.expm1(mean_log_ratio) * 100,
        right_faster_blocks=sum(value < 0 for value in block_log_ratios),
        permutation=permutation,
    )


def block_order(swapped):
    if swapped:
        return ("right", "left", "left", "right")

    return ("left", "right", "right", "left")


def decision_checkpoints(min_runs, max_runs, growth):
    if min_runs < 1:
        raise ValueError("--min-runs must be positive")
    if max_runs < min_runs:
        raise ValueError("--max-runs must be at least --min-runs")
    if min_runs % 2 != 0 or max_runs % 2 != 0:
        raise ValueError("--min-runs and --max-runs must be even for complete blocks")
    if growth <= 1:
        raise ValueError("--growth must be greater than 1")

    result = []
    current = min_runs // 2
    max_blocks = max_runs // 2

    while current < max_blocks:
        result.append(current * 2)
        following = max(current + 1, math.ceil(current * growth))
        current = min(following, max_blocks)

    result.append(max_runs)

    return result


def _resolve_binary(value):
    if os.path.exists(value):
        result = os.path.abspath(value)
    else:
        result = shutil.which(value)

    if result is None:
        raise ValueError(f"binary not found: {value}")
    if not os.access(result, os.X_OK):
        raise ValueError(f"binary is not executable: {result}")

    return result


def _parse_perf_stat(data):
    result = {}

    for row in csv.reader(io.StringIO(data)):
        if len(row) < 3:
            continue

        raw_value, unit, raw_event = row[:3]
        event = raw_event.removesuffix(":u")

        if event not in ("task-clock", "cycles", "instructions"):
            continue
        if raw_value.startswith("<"):
            raise RuntimeError(f"perf could not count {raw_event}: {raw_value}")

        try:
            value = float(raw_value)
        except ValueError as error:
            raise RuntimeError(f"invalid perf value for {raw_event}: {raw_value}") from error

        if event == "task-clock":
            if unit != "msec":
                raise RuntimeError(f"unexpected task-clock unit: {unit}")

            result["task_clock"] = value / 1000
        else:
            result[event] = int(value)

    missing = set(PERF_METRICS) - result.keys()

    if missing:
        raise RuntimeError(f"perf output is missing: {', '.join(sorted(missing))}")

    return result


def _run_once(binary, command, cwd, env, show_output, perf):
    output = None if show_output else subprocess.DEVNULL
    usage_before = resource.getrusage(resource.RUSAGE_CHILDREN)
    started = time.perf_counter_ns()

    with tempfile.TemporaryFile(mode="w+b") as perf_output:
        perf_command = [
            perf,
            "stat",
            "--no-big-num",
            "-x,",
            "--log-fd",
            str(perf_output.fileno()),
            "-e",
            ",".join(PERF_EVENTS),
            "--",
            binary,
            *command,
        ]
        completed = subprocess.run(
            perf_command,
            cwd=cwd,
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=output,
            stderr=output,
            pass_fds=(perf_output.fileno(),),
            check=False,
        )
        perf_output.seek(0)
        counters = _parse_perf_stat(perf_output.read().decode("utf-8"))

    elapsed = (time.perf_counter_ns() - started) / 1_000_000_000
    usage_after = resource.getrusage(resource.RUSAGE_CHILDREN)

    if completed.returncode != 0:
        raise RuntimeError(f"{binary} exited with status {completed.returncode}")

    return {
        "wall": elapsed,
        "user": usage_after.ru_utime - usage_before.ru_utime,
        "system": usage_after.ru_stime - usage_before.ru_stime,
        **counters,
    }


def _check_perf(perf, cwd, env):
    sample = _run_once(sys.executable, ["-c", "pass"], cwd, env, False, perf)

    for metric in PERF_METRICS:
        if sample[metric] <= 0:
            raise RuntimeError(f"perf returned a non-positive {metric} counter")


def raw_block_log_ratios(samples, metric):
    ratios = []

    for start in range(0, len(samples)-3, 4):
        block = samples[start:start+4]

        if any(metric not in sample for sample in block):
            continue

        left = [sample[metric] for sample in block if sample.get("side") == "L"]
        right = [sample[metric] for sample in block if sample.get("side") == "R"]

        if len(left) == 2 and len(right) == 2:
            ratios.append(block_log_ratio(left, right))

    return ratios


def _load_calibration(path, metrics):
    if path is None:
        return {}

    with open(path, encoding="utf-8") as stream:
        samples = json.load(stream)

    if not isinstance(samples, list):
        raise ValueError("calibration JSON must contain a raw sample list")

    result = {metric: raw_block_log_ratios(samples, metric) for metric in metrics}

    if any(len(values) < 2 for values in result.values()):
        missing = [metric for metric, values in result.items() if len(values) < 2]

        raise ValueError(f"calibration JSON has fewer than two complete blocks for: {', '.join(missing)}")

    return result


def _write_json(path, samples):
    if path is None:
        return

    tmp = path + ".tmp"

    with open(tmp, "w", encoding="utf-8") as stream:
        json.dump(samples, stream, indent=2)
        stream.write("\n")

    os.replace(tmp, path)


def _parse_args(argv):
    if "--" in argv:
        separator = argv.index("--")
        own_args = argv[:separator]
        command = argv[separator + 1:]
    elif "--help" in argv or "-h" in argv:
        own_args = argv
        command = []
    else:
        raise ValueError("missing `--` before the command passed to the binaries")

    parser = argparse.ArgumentParser(
        description="Compare two ay binaries using paired performance permutation tests.",
        epilog=(
            "Example: ./dev/perf.py --left ./ay1 --right ./ay2 -- "
            "--target-platform default-linux-x86_64 --source-root /path/to/slice target"
        ),
    )
    parser.add_argument("--left", required=True, help="left binary")
    parser.add_argument("--right", required=True, help="right binary")
    parser.add_argument("--cwd", default=os.getcwd(), help="working directory for both binaries")
    parser.add_argument("--min-runs", type=int, default=30, help="first decision checkpoint per binary")
    parser.add_argument("--max-runs", type=int, default=120, help="hard run limit per binary")
    parser.add_argument("--growth", type=float, default=1.5, help="checkpoint growth factor")
    parser.add_argument("--alpha", type=float, default=0.05, help="family-wise false-positive budget")
    parser.add_argument("--power", type=float, default=0.8, help="power used for MDE estimates")
    parser.add_argument(
        "--target-effect",
        type=float,
        default=0.1,
        help="effect percentage used for required-run estimates",
    )
    parser.add_argument("--warmup-cycles", type=int, default=1, help="unmeasured four-run blocks")
    parser.add_argument("--seed", type=int, help="randomized block schedule seed")
    parser.add_argument(
        "--cpu",
        type=int,
        required=True,
        help="pin the harness and all measured children to this CPU",
    )
    parser.add_argument(
        "--calibration-json",
        metavar="PATH",
        help="raw A/A samples used to estimate MDE and required run count",
    )
    parser.add_argument(
        "--exact-max-blocks",
        type=int,
        default=20,
        help="largest block sample for exhaustive sign flips",
    )
    parser.add_argument(
        "--permutations",
        type=int,
        default=200_000,
        help="sampled sign flips after --exact-max-blocks",
    )
    parser.add_argument(
        "--json",
        metavar="PATH",
        help="continuously write chronological raw measurements as JSON",
    )
    parser.add_argument("--show-output", action="store_true", help="do not suppress child stdout/stderr")
    args = parser.parse_args(own_args)

    if not command:
        parser.error("the command after `--` is empty")

    if not 0 < args.alpha < 1:
        parser.error("--alpha must be between 0 and 1")
    if not 0 < args.power < 1:
        parser.error("--power must be between 0 and 1")
    if args.target_effect <= 0:
        parser.error("--target-effect must be positive")
    if args.warmup_cycles < 0:
        parser.error("--warmup-cycles cannot be negative")
    if args.exact_max_blocks < 1:
        parser.error("--exact-max-blocks must be positive")
    if args.permutations < 1:
        parser.error("--permutations must be positive")

    try:
        checkpoints = decision_checkpoints(args.min_runs, args.max_runs, args.growth)
        args.left = _resolve_binary(args.left)
        args.right = _resolve_binary(args.right)
        args.perf = _resolve_binary("perf")
    except ValueError as error:
        parser.error(str(error))

    args.cwd = os.path.abspath(args.cwd)
    args.seed = args.seed if args.seed is not None else int.from_bytes(os.urandom(8), "little")
    if args.calibration_json is not None:
        args.calibration_json = os.path.abspath(args.calibration_json)

        if not os.path.isfile(args.calibration_json):
            parser.error(f"calibration JSON not found: {args.calibration_json}")

    if not os.path.isdir(args.cwd):
        parser.error(f"working directory not found: {args.cwd}")

    if not hasattr(os, "sched_getaffinity"):
        parser.error("CPU affinity is not supported on this platform")
    if args.cpu not in os.sched_getaffinity(0):
        parser.error(f"CPU {args.cpu} is outside the allowed affinity set")

    return args, ["make", "-j0", *command], checkpoints


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv

    try:
        args, command, checkpoints = _parse_args(argv)
    except ValueError as error:
        print(f"perf.py: {error}", file=sys.stderr)
        return 2

    try:
        os.sched_setaffinity(0, {args.cpu})
    except OSError as error:
        print(f"perf.py: cannot pin to CPU {args.cpu}: {error}", file=sys.stderr)
        return 2

    env = os.environ.copy()
    env.setdefault("GOGC", "off")

    try:
        _check_perf(args.perf, args.cwd, env)
    except (OSError, RuntimeError) as error:
        print(f"perf.py: counter environment is unusable: {error}", file=sys.stderr)
        return 2

    binaries = {"left": args.left, "right": args.right}
    metrics = (*BASE_METRICS, *PERF_METRICS)
    measurements = {"left": [], "right": []}
    raw_samples = []
    block_log_ratios = {metric: [] for metric in metrics}
    decision_alpha = args.alpha / len(checkpoints)
    checkpoint_set = set(checkpoints)
    schedule_rng = random.Random(args.seed)
    calibration_mode = os.path.samefile(args.left, args.right)

    try:
        calibration = _load_calibration(args.calibration_json, metrics)
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print(f"perf.py: cannot load calibration: {error}", file=sys.stderr)
        return 2

    print(f"left : {args.left}")
    print(f"right: {args.right}")
    print(f"cwd  : {args.cwd}")
    print(f"cmd  : {shlex.join(command)}")
    print(f"seed : {args.seed}")
    print(f"cpu  : {args.cpu}")
    print(f"perf : {args.perf}")
    print(f"gate : Holm over {', '.join(DECISION_METRICS)}; any faster, none slower")
    print(f"looks: {checkpoints}; per-look p <= {decision_alpha:.6g} (FWER {args.alpha:g})")

    if calibration_mode:
        print("mode : A/A calibration; separation is disabled")
    elif calibration:
        print(f"noise: A/A raw samples from {args.calibration_json}")
    else:
        print("noise: current A/B blocks (run A/A once for an independent MDE estimate)")

    _write_json(args.json, raw_samples)
    improved = set()
    regressed = set()

    try:
        for cycle in range(args.warmup_cycles):
            order = block_order(bool(schedule_rng.getrandbits(1)))
            order_label = "".join(name[0].upper() for name in order)
            print(
                f"[warmup {cycle + 1}/{args.warmup_cycles}] {order_label}",
                flush=True,
            )

            for name in order:
                _run_once(binaries[name], command, args.cwd, env, args.show_output, args.perf)

        for _ in range(args.max_runs // 2):
            measured = {"left": [], "right": []}
            order = block_order(bool(schedule_rng.getrandbits(1)))

            for name in order:
                sample = _run_once(
                    binaries[name], command, args.cwd, env, args.show_output, args.perf
                )
                measured[name].append(sample)
                raw_samples.append({"side": name[0].upper(), **sample})
                _write_json(args.json, raw_samples)

            measurements["left"].extend(measured["left"])
            measurements["right"].extend(measured["right"])

            for metric in metrics:
                left = [sample[metric] for sample in measured["left"]]
                right = [sample[metric] for sample in measured["right"]]

                block_log_ratios[metric].append(block_log_ratio(left, right))

            order_label = "".join(name[0].upper() for name in order)

            n = len(measurements["left"])
            permutations = {}
            regression_permutations = {}

            if n in checkpoint_set:
                # Keep test randomness independent of the execution schedule and
                # reproducible for every checkpoint.
                test_seed = args.seed ^ (n * 0x9E3779B97F4A7C15)

                for offset, metric in enumerate(metrics):
                    permutations[metric] = paired_permutation(
                        block_log_ratios[metric],
                        args.exact_max_blocks,
                        args.permutations,
                        test_seed ^ (offset * 0xD1B54A32D192ED03),
                    )

                for offset, metric in enumerate(DECISION_METRICS):
                    regression_permutations[metric] = paired_permutation(
                        [-value for value in block_log_ratios[metric]],
                        args.exact_max_blocks,
                        args.permutations,
                        test_seed ^ (offset * 0x94D049BB133111EB) ^ 0xA24BAED4963EE407,
                    )

                improved = holm_rejections(
                    {
                        metric: permutations[metric].p_right_faster
                        for metric in DECISION_METRICS
                    },
                    decision_alpha,
                )
                regressed = holm_rejections(
                    {
                        metric: regression_permutations[metric].p_right_faster
                        for metric in DECISION_METRICS
                    },
                    decision_alpha,
                )

            result = summarize(
                [sample["wall"] for sample in measurements["left"]],
                [sample["wall"] for sample in measurements["right"]],
                block_log_ratios["wall"],
                permutations.get("wall"),
            )
            n = result.runs
            marker = " look" if n in checkpoint_set else ""
            left_wall = [sample["wall"] for sample in measured["left"]]
            right_wall = [sample["wall"] for sample in measured["right"]]

            print(
                f"[{n:3d}/{args.max_runs}{marker}] "
                f"order={order_label} "
                f"L={left_wall[0]:.3f},{left_wall[1]:.3f}s "
                f"R={right_wall[0]:.3f},{right_wall[1]:.3f}s "
                f"mean={result.left_mean:.3f}/{result.right_mean:.3f}s "
                f"min={result.left_min:.3f}/{result.right_min:.3f}s "
                f"wall={result.geometric_right_vs_left_pct:+.3f}% "
                f"mean-delta={result.right_vs_left_pct:+.3f}% "
                f"wins={result.right_faster_blocks}/{result.blocks}",
                flush=True,
            )

            if permutations:
                for metric in metrics:
                    permutation = permutations[metric]
                    effect = math.expm1(permutation.mean_log_ratio) * 100
                    noise = calibration.get(metric, block_log_ratios[metric])
                    mde = minimum_detectable_effect(
                        noise,
                        len(block_log_ratios[metric]),
                        decision_alpha,
                        args.power,
                    )
                    required = required_runs_per_side(
                        noise,
                        args.target_effect,
                        decision_alpha,
                        args.power,
                    )
                    mde_text = "n/a" if mde is None else f"{mde:.3f}%"
                    required_text = "n/a" if required is None else str(required)
                    slow_text = ""
                    status = ""

                    if metric in regression_permutations:
                        slow_text = (
                            f" p_slow={regression_permutations[metric].p_right_faster:.6g}"
                        )
                    if metric in improved:
                        status = " BETTER"
                    if metric in regressed:
                        status = " WORSE"

                    print(
                        f"  {metric:12s} R/L={effect:+.4f}% "
                        f"p_fast={permutation.p_right_faster:.6g}"
                        f"{slow_text} "
                        f"MDE{args.power:.0%}={mde_text} "
                        f"runs/side@{args.target_effect:g}%={required_text}"
                        f"{status}",
                        flush=True,
                    )

            if regressed and not calibration_mode:
                print(
                    f"REGRESSED: {', '.join(sorted(regressed))}; "
                    f"n={n} each, blocks={result.blocks}"
                )

                return 3

            if improved and not calibration_mode:
                print(
                    f"IMPROVED: {', '.join(sorted(improved))}; "
                    f"n={n} each, blocks={result.blocks}; no detected regressions"
                )

                return 0

    except KeyboardInterrupt:
        print("\nperf.py: interrupted", file=sys.stderr)
        return 130
    except (OSError, RuntimeError) as error:
        print(f"perf.py: {error}", file=sys.stderr)
        return 1

    if calibration_mode:
        print(
            f"CALIBRATION: n={args.max_runs} each, blocks={result.blocks}"
        )

        return 0

    print(
        f"INCONCLUSIVE: n={args.max_runs} each, blocks={result.blocks}, "
        "no Holm-significant improvement or regression"
    )

    return 2


if __name__ == "__main__":
    sys.exit(main())
