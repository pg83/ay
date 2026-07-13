#!/usr/bin/env python3
"""Sequential paired wall-time comparison for two ay binaries.

The arguments after ``--`` are passed to ``<binary> make -j0`` verbatim.
Measurements are made in randomized LRRL/RLLR blocks.  Each block contributes
one log(right/left) observation, and decisions use a paired sign-flip
permutation test at precomputed group-sequential checkpoints.
"""

import argparse
import json
import math
import os
import random
import shlex
import shutil
import statistics
import subprocess
import sys
import time
from dataclasses import asdict, dataclass


@dataclass(frozen=True)
class PermutationResult:
    mean_log_ratio: float
    p_two_sided: float
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


def _extreme(observed, candidate):
    tolerance = 1e-14 * max(1.0, observed)

    return abs(candidate) >= observed - tolerance


def _exact_sign_flip_p(values):
    observed = abs(math.fsum(values))
    extreme = 0

    def visit(pos, total):
        nonlocal extreme

        if pos == len(values):
            if _extreme(observed, total):
                extreme += 1

            return

        value = values[pos]
        visit(pos + 1, total - value)
        visit(pos + 1, total + value)

    visit(0, 0.0)

    return extreme / (1 << len(values)), 1 << len(values)


def _sampled_sign_flip_p(values, permutations, seed):
    observed = abs(math.fsum(values))
    rng = random.Random(seed)
    extreme = 0

    for _ in range(permutations):
        signs = rng.getrandbits(len(values))
        total = 0.0

        for value in values:
            total += value if signs & 1 else -value
            signs >>= 1

        if _extreme(observed, total):
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


def _run_once(binary, command, cwd, env, show_output):
    output = None if show_output else subprocess.DEVNULL
    started = time.perf_counter_ns()
    completed = subprocess.run(
        [binary, *command],
        cwd=cwd,
        env=env,
        stdin=subprocess.DEVNULL,
        stdout=output,
        stderr=output,
        check=False,
    )
    elapsed = (time.perf_counter_ns() - started) / 1_000_000_000

    if completed.returncode != 0:
        raise RuntimeError(f"{binary} exited with status {completed.returncode}")

    return elapsed


def _write_json(path, state):
    if path is None:
        return

    tmp = path + ".tmp"

    with open(tmp, "w", encoding="utf-8") as stream:
        json.dump(state, stream, indent=2, sort_keys=True)
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
        description="Compare two ay binaries using paired wall-time permutation tests.",
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
    parser.add_argument("--warmup-cycles", type=int, default=1, help="unmeasured four-run blocks")
    parser.add_argument("--seed", type=int, help="randomized block schedule seed")
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
    parser.add_argument("--json", metavar="PATH", help="continuously write samples and summary as JSON")
    parser.add_argument("--show-output", action="store_true", help="do not suppress child stdout/stderr")
    args = parser.parse_args(own_args)

    if not command:
        parser.error("the command after `--` is empty")

    if not 0 < args.alpha < 1:
        parser.error("--alpha must be between 0 and 1")
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
    except ValueError as error:
        parser.error(str(error))

    args.cwd = os.path.abspath(args.cwd)
    args.seed = args.seed if args.seed is not None else int.from_bytes(os.urandom(8), "little")

    if not os.path.isdir(args.cwd):
        parser.error(f"working directory not found: {args.cwd}")

    return args, ["make", "-j0", *command], checkpoints


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv

    try:
        args, command, checkpoints = _parse_args(argv)
    except ValueError as error:
        print(f"perf.py: {error}", file=sys.stderr)
        return 2

    env = os.environ.copy()
    env.setdefault("GOGC", "off")
    binaries = {"left": args.left, "right": args.right}
    samples = {"left": [], "right": []}
    blocks = []
    block_log_ratios = []
    decision_alpha = args.alpha / len(checkpoints)
    checkpoint_set = set(checkpoints)
    schedule_rng = random.Random(args.seed)

    print(f"left : {args.left}")
    print(f"right: {args.right}")
    print(f"cwd  : {args.cwd}")
    print(f"cmd  : {shlex.join(command)}")
    print(f"seed : {args.seed}")
    print(f"looks: {checkpoints}; per-look p <= {decision_alpha:.6g} (FWER {args.alpha:g})")

    try:
        for cycle in range(args.warmup_cycles):
            order = block_order(bool(schedule_rng.getrandbits(1)))
            order_label = "".join(name[0].upper() for name in order)
            print(
                f"[warmup {cycle + 1}/{args.warmup_cycles}] {order_label}",
                flush=True,
            )

            for name in order:
                _run_once(binaries[name], command, args.cwd, env, args.show_output)

        for block_index in range(args.max_runs // 2):
            elapsed = {"left": [], "right": []}
            order = block_order(bool(schedule_rng.getrandbits(1)))

            for name in order:
                elapsed[name].append(
                    _run_once(binaries[name], command, args.cwd, env, args.show_output)
                )

            samples["left"].extend(elapsed["left"])
            samples["right"].extend(elapsed["right"])
            log_ratio = block_log_ratio(elapsed["left"], elapsed["right"])
            block_log_ratios.append(log_ratio)
            order_label = "".join(name[0].upper() for name in order)
            blocks.append(
                {
                    "order": order_label,
                    "left": elapsed["left"],
                    "right": elapsed["right"],
                    "log_ratio": log_ratio,
                }
            )

            n = len(samples["left"])
            permutation = None

            if n in checkpoint_set:
                # Keep test randomness independent of the execution schedule and
                # reproducible for every checkpoint.
                test_seed = args.seed ^ (n * 0x9E3779B97F4A7C15)
                permutation = paired_permutation(
                    block_log_ratios,
                    args.exact_max_blocks,
                    args.permutations,
                    test_seed,
                )

            result = summarize(
                samples["left"], samples["right"], block_log_ratios, permutation
            )
            n = result.runs
            marker = " look" if n in checkpoint_set else ""
            p_text = ""

            if permutation is not None:
                p_text = (
                    f" p={permutation.p_two_sided:.6g}"
                    f" ({permutation.method}, {permutation.permutations:g})"
                )

            print(
                f"[{n:3d}/{args.max_runs}{marker}] "
                f"order={order_label} "
                f"L={elapsed['left'][0]:.3f},{elapsed['left'][1]:.3f}s "
                f"R={elapsed['right'][0]:.3f},{elapsed['right'][1]:.3f}s "
                f"mean={result.left_mean:.3f}/{result.right_mean:.3f}s "
                f"min={result.left_min:.3f}/{result.right_min:.3f}s "
                f"R/L={result.geometric_right_vs_left_pct:+.3f}% "
                f"mean-delta={result.right_vs_left_pct:+.3f}% "
                f"wins={result.right_faster_blocks}/{result.blocks}"
                f"{p_text}",
                flush=True,
            )

            state = {
                "left": args.left,
                "right": args.right,
                "command": command,
                "seed": args.seed,
                "checkpoints": checkpoints,
                "decision_alpha": decision_alpha,
                "samples": samples,
                "blocks": blocks,
                "summary": asdict(result),
                "status": "running",
            }

            if permutation is not None and permutation.p_two_sided <= decision_alpha:
                winner = "left" if permutation.mean_log_ratio > 0 else "right"
                state["status"] = "separated"
                state["winner"] = winner
                _write_json(args.json, state)
                print(
                    f"SEPARATED: {winner} is faster; n={n} each, "
                    f"blocks={result.blocks}, "
                    f"p={permutation.p_two_sided:.6g} <= {decision_alpha:.6g}, "
                    f"R/L={result.geometric_right_vs_left_pct:+.3f}%"
                )

                return 0

            _write_json(args.json, state)

    except KeyboardInterrupt:
        print("\nperf.py: interrupted", file=sys.stderr)
        return 130
    except (OSError, RuntimeError) as error:
        print(f"perf.py: {error}", file=sys.stderr)
        return 1

    state["status"] = "inconclusive"
    _write_json(args.json, state)
    print(
        f"INCONCLUSIVE: n={args.max_runs} each, "
        f"blocks={result.blocks}, "
        f"p={result.permutation.p_two_sided:.6g} > {decision_alpha:.6g}, "
        f"R/L={result.geometric_right_vs_left_pct:+.3f}%"
    )

    return 2


if __name__ == "__main__":
    sys.exit(main())
