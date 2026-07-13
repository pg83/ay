#!/usr/bin/env python3
"""Sequential wall-time comparison for two ay binaries.

The arguments after ``--`` are passed to ``<binary> make -j0`` verbatim.
Measured rounds alternate their execution order: left/right, then right/left.
Mann-Whitney U is reported after every round, while stopping decisions are made
only at a precomputed set of group-sequential checkpoints.
"""

import argparse
import json
import math
import os
import shlex
import shutil
import statistics
import subprocess
import sys
import time
from dataclasses import asdict, dataclass


@dataclass(frozen=True)
class MWWResult:
    u_left: float
    p_two_sided: float
    rank_biserial: float
    method: str


@dataclass(frozen=True)
class Summary:
    runs: int
    left_mean: float
    right_mean: float
    left_median: float
    right_median: float
    right_vs_left_pct: float
    mww: MWWResult


def _average_ranks_twice(left, right):
    """Return twice the average rank for every value, preserving input order."""
    tagged = [(value, i) for i, value in enumerate(left)]
    tagged.extend((value, len(left) + i) for i, value in enumerate(right))
    tagged.sort(key=lambda item: item[0])

    ranks = [0] * len(tagged)
    pos = 0

    while pos < len(tagged):
        end = pos + 1

        while end < len(tagged) and tagged[end][0] == tagged[pos][0]:
            end += 1

        # The one-based ranks are pos+1 through end.  Their doubled average is
        # therefore pos+1+end, which remains integral even in the presence of ties.
        rank_twice = pos + 1 + end

        for _, original in tagged[pos:end]:
            ranks[original] = rank_twice

        pos = end

    return ranks


def _exact_two_sided_p(ranks_twice, n_left, observed_sum_twice):
    """Exact conditional permutation p-value for the average-rank statistic."""
    total_n = len(ranks_twice)
    center_twice = n_left * (total_n + 1)
    observed_distance = abs(observed_sum_twice - center_twice)
    ways = [dict() for _ in range(n_left + 1)]
    ways[0][0] = 1

    for seen, rank in enumerate(ranks_twice, 1):
        for count in range(min(seen, n_left), 0, -1):
            previous = ways[count - 1]
            current = ways[count]

            for rank_sum, combinations in previous.items():
                new_sum = rank_sum + rank
                current[new_sum] = current.get(new_sum, 0) + combinations

    total = 0
    extreme = 0

    for rank_sum, combinations in ways[n_left].items():
        total += combinations

        if abs(rank_sum - center_twice) >= observed_distance:
            extreme += combinations

    return extreme / total


def _asymptotic_two_sided_p(left, right, u_left):
    """Tie-corrected normal approximation with a continuity correction."""
    n_left = len(left)
    n_right = len(right)
    total_n = n_left + n_right
    counts = {}

    for value in (*left, *right):
        counts[value] = counts.get(value, 0) + 1

    tie_sum = sum(count**3 - count for count in counts.values())
    variance = n_left * n_right / 12 * (
        total_n + 1 - tie_sum / (total_n * (total_n - 1))
    )

    if variance == 0:
        return 1.0

    mean = n_left * n_right / 2
    distance = max(0.0, abs(u_left - mean) - 0.5)
    z = distance / math.sqrt(variance)

    return math.erfc(z / math.sqrt(2))


def mann_whitney(left, right, exact_max_total=60):
    if not left or not right:
        raise ValueError("Mann-Whitney U needs two non-empty samples")

    n_left = len(left)
    n_right = len(right)
    ranks_twice = _average_ranks_twice(left, right)
    rank_sum_left_twice = sum(ranks_twice[:n_left])
    u_left = rank_sum_left_twice / 2 - n_left * (n_left + 1) / 2

    if n_left + n_right <= exact_max_total:
        p_value = _exact_two_sided_p(ranks_twice, n_left, rank_sum_left_twice)
        method = "exact"
    else:
        p_value = _asymptotic_two_sided_p(left, right, u_left)
        method = "normal"

    rank_biserial = 1 - 2 * u_left / (n_left * n_right)

    return MWWResult(u_left, p_value, rank_biserial, method)


def summarize(left, right, exact_max_total=60):
    left_mean = statistics.fmean(left)
    right_mean = statistics.fmean(right)

    return Summary(
        runs=len(left),
        left_mean=left_mean,
        right_mean=right_mean,
        left_median=statistics.median(left),
        right_median=statistics.median(right),
        right_vs_left_pct=(right_mean - left_mean) / left_mean * 100,
        mww=mann_whitney(left, right, exact_max_total),
    )


def run_order(round_index):
    return ("left", "right") if round_index % 2 == 0 else ("right", "left")


def decision_checkpoints(min_runs, max_runs, growth):
    if min_runs < 1:
        raise ValueError("--min-runs must be positive")
    if max_runs < min_runs:
        raise ValueError("--max-runs must be at least --min-runs")
    if growth <= 1:
        raise ValueError("--growth must be greater than 1")

    result = []
    current = min_runs

    while current < max_runs:
        result.append(current)
        following = max(current + 1, math.ceil(current * growth))
        current = min(following, max_runs)

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
        description="Compare two ay binaries using alternating wall-time runs and MWW.",
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
    parser.add_argument("--warmup-cycles", type=int, default=1, help="unmeasured L/R/R/L cycles")
    parser.add_argument("--exact-max-total", type=int, default=60, help="largest pooled sample for exact MWW")
    parser.add_argument("--json", metavar="PATH", help="continuously write samples and summary as JSON")
    parser.add_argument("--show-output", action="store_true", help="do not suppress child stdout/stderr")
    args = parser.parse_args(own_args)

    if not command:
        parser.error("the command after `--` is empty")

    if not 0 < args.alpha < 1:
        parser.error("--alpha must be between 0 and 1")
    if args.warmup_cycles < 0:
        parser.error("--warmup-cycles cannot be negative")
    if args.exact_max_total < 2:
        parser.error("--exact-max-total must be at least 2")

    try:
        checkpoints = decision_checkpoints(args.min_runs, args.max_runs, args.growth)
        args.left = _resolve_binary(args.left)
        args.right = _resolve_binary(args.right)
    except ValueError as error:
        parser.error(str(error))

    args.cwd = os.path.abspath(args.cwd)

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
    decision_alpha = args.alpha / len(checkpoints)
    checkpoint_set = set(checkpoints)

    print(f"left : {args.left}")
    print(f"right: {args.right}")
    print(f"cwd  : {args.cwd}")
    print(f"cmd  : {shlex.join(command)}")
    print(f"looks: {checkpoints}; per-look p <= {decision_alpha:.6g} (FWER {args.alpha:g})")

    try:
        for cycle in range(args.warmup_cycles):
            print(f"[warmup {cycle + 1}/{args.warmup_cycles}] L R R L", flush=True)

            for name in ("left", "right", "right", "left"):
                _run_once(binaries[name], command, args.cwd, env, args.show_output)

        for round_index in range(args.max_runs):
            elapsed = {}
            order = run_order(round_index)

            for name in order:
                elapsed[name] = _run_once(
                    binaries[name], command, args.cwd, env, args.show_output
                )

            samples["left"].append(elapsed["left"])
            samples["right"].append(elapsed["right"])
            result = summarize(samples["left"], samples["right"], args.exact_max_total)
            n = result.runs
            marker = " look" if n in checkpoint_set else ""
            print(
                f"[{n:3d}/{args.max_runs}{marker}] "
                f"order={order[0][0].upper()}{order[1][0].upper()} "
                f"L={elapsed['left']:.3f}s R={elapsed['right']:.3f}s "
                f"mean={result.left_mean:.3f}/{result.right_mean:.3f}s "
                f"R-L={result.right_vs_left_pct:+.3f}% "
                f"U={result.mww.u_left:g} p={result.mww.p_two_sided:.6g} "
                f"({result.mww.method})",
                flush=True,
            )

            state = {
                "left": args.left,
                "right": args.right,
                "command": command,
                "checkpoints": checkpoints,
                "decision_alpha": decision_alpha,
                "samples": samples,
                "summary": asdict(result),
                "status": "running",
            }

            if n in checkpoint_set and result.mww.p_two_sided <= decision_alpha:
                winner = "left" if result.mww.rank_biserial > 0 else "right"
                state["status"] = "separated"
                state["winner"] = winner
                _write_json(args.json, state)
                print(
                    f"SEPARATED: {winner} is faster; n={n} each, "
                    f"p={result.mww.p_two_sided:.6g} <= {decision_alpha:.6g}, "
                    f"R-L={result.right_vs_left_pct:+.3f}%"
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
        f"p={result.mww.p_two_sided:.6g} > {decision_alpha:.6g}, "
        f"R-L={result.right_vs_left_pct:+.3f}%"
    )

    return 2


if __name__ == "__main__":
    sys.exit(main())
