#!/usr/bin/env python3
"""diff.py - inspect already-normalized yatool graphs.

Typical use:
    ./dev/diff.py --our .out/sg3.our.norm.json --ref .out/sg3.ref.norm.json \
        --root-output /devtools/ya/bin/ya-bin

This script expects inputs produced by normalize.py. It does not normalize
raw sg.json files itself; keeping the stages separate makes expensive closure
normalization explicit and lets this diff tool stay quick while iterating.
"""
from __future__ import annotations

import argparse
import json
import sys
from collections import Counter, defaultdict
from difflib import unified_diff
from typing import Any


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(2)


def load_graph(path: str) -> list[dict[str, Any]]:
    try:
        with open(path, encoding="utf-8") as fh:
            data = json.load(fh)
    except (OSError, json.JSONDecodeError) as exc:
        die(f"cannot load {path}: {exc}")

    if isinstance(data, dict):
        graph = data.get("graph")
    else:
        graph = data

    if not isinstance(graph, list):
        die(f"{path}: expected normalized graph object or graph array")

    return graph


def kind(node: dict[str, Any]) -> str:
    return (node.get("kv") or {}).get("p", "")


def outputs(node: dict[str, Any]) -> list[str]:
    return node.get("outputs") or []


def primary_output(node: dict[str, Any]) -> str:
    outs = outputs(node)
    return outs[0] if outs else ""


def output_index(graph: list[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    out: dict[str, dict[str, Any]] = {}
    for node in graph:
        for path in outputs(node):
            out[path] = node
    return out


def first_by_output_suffix(graph: list[dict[str, Any]], suffix: str) -> dict[str, Any] | None:
    matches = [
        node for node in graph
        if any(out.endswith(suffix) or suffix in out for out in outputs(node))
    ]
    if len(matches) > 1:
        sample = "\n".join(primary_output(n) for n in matches[:10])
        die(f"root-output {suffix!r} matched {len(matches)} nodes; narrow it down:\n{sample}")
    return matches[0] if matches else None


def print_kind_breakdown(our: list[dict[str, Any]], ref: list[dict[str, Any]]) -> None:
    our_c = Counter(kind(n) for n in our)
    ref_c = Counter(kind(n) for n in ref)
    keys = sorted(set(our_c) | set(ref_c))

    print("Kinds:")
    print("  kind  our    ref    delta")
    for key in keys:
        print(f"  {key or '-':4}  {our_c[key]:5}  {ref_c[key]:5}  {our_c[key] - ref_c[key]:+6}")


def print_output_delta(
    our: list[dict[str, Any]],
    ref: list[dict[str, Any]],
    limit: int,
    contains: str,
) -> None:
    our_out = output_index(our)
    ref_out = output_index(ref)

    missing = sorted(set(ref_out) - set(our_out))
    extra = sorted(set(our_out) - set(ref_out))

    if contains:
        missing = [x for x in missing if contains in x]
        extra = [x for x in extra if contains in x]

    print()
    print(f"Outputs: missing={len(missing)} extra={len(extra)}")

    def show(title: str, items: list[str], idx: dict[str, dict[str, Any]]) -> None:
        print(f"{title}:")
        if not items:
            print("  <none>")
            return
        for out in items[:limit]:
            print(f"  {kind(idx[out]):3} {out}")
        if len(items) > limit:
            print(f"  ... {len(items) - limit} more")

    show("Missing from our", missing, ref_out)
    show("Extra in our", extra, our_out)


def summarize_root_field(name: str, our_node: dict[str, Any], ref_node: dict[str, Any], limit: int) -> None:
    our_vals = set(our_node.get(name) or [])
    ref_vals = set(ref_node.get(name) or [])
    missing = sorted(ref_vals - our_vals)
    extra = sorted(our_vals - ref_vals)

    print()
    print(f"Root {name}: our={len(our_vals)} ref={len(ref_vals)} missing={len(missing)} extra={len(extra)}")
    for x in missing[:limit]:
        print(f"  M {x}")
    if len(missing) > limit:
        print(f"  M ... {len(missing) - limit} more")
    for x in extra[:limit]:
        print(f"  E {x}")
    if len(extra) > limit:
        print(f"  E ... {len(extra) - limit} more")


def cmd_args(node: dict[str, Any]) -> list[list[str]]:
    return [cmd.get("cmd_args") or [] for cmd in (node.get("cmds") or [])]


def print_cmd_summary(our_node: dict[str, Any], ref_node: dict[str, Any], show_diff: bool) -> None:
    our_cmds = cmd_args(our_node)
    ref_cmds = cmd_args(ref_node)
    print()
    print(f"Root cmds: our={len(our_cmds)} ref={len(ref_cmds)}")
    for i in range(max(len(our_cmds), len(ref_cmds))):
        o = our_cmds[i] if i < len(our_cmds) else []
        r = ref_cmds[i] if i < len(ref_cmds) else []
        print(f"  cmd[{i}]: our_args={len(o)} ref_args={len(r)} delta={len(o) - len(r):+}")

        if show_diff and o != r:
            diff = unified_diff(
                [x + "\n" for x in o],
                [x + "\n" for x in r],
                fromfile=f"our cmd[{i}]",
                tofile=f"ref cmd[{i}]",
                lineterm="",
            )
            for line in diff:
                print("    " + line.rstrip("\n"))


def bucket_missing_by_dir(paths: list[str], depth: int) -> list[tuple[str, int]]:
    counts: Counter[str] = Counter()
    for path in paths:
        p = path
        for prefix in ("$(B)/", "$(S)/"):
            if p.startswith(prefix):
                p = p[len(prefix):]
                break
        parts = p.split("/")
        key = "/".join(parts[:depth]) if len(parts) >= depth else p
        counts[key] += 1
    return counts.most_common()


def print_buckets(our: list[dict[str, Any]], ref: list[dict[str, Any]], depth: int, limit: int) -> None:
    our_out = set(output_index(our))
    ref_out = set(output_index(ref))
    missing = sorted(ref_out - our_out)
    extra = sorted(our_out - ref_out)

    print()
    print(f"Missing output buckets by first {depth} path parts:")
    for key, count in bucket_missing_by_dir(missing, depth)[:limit]:
        print(f"  {count:5} {key}")

    print()
    print(f"Extra output buckets by first {depth} path parts:")
    for key, count in bucket_missing_by_dir(extra, depth)[:limit]:
        print(f"  {count:5} {key}")


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description="Diff already-normalized yatool graphs")
    p.add_argument("--our", required=True, help="normalized OUR graph JSON")
    p.add_argument("--ref", required=True, help="normalized REF graph JSON")
    p.add_argument("--root-output", help="substring or suffix identifying root output")
    p.add_argument("--contains", default="", help="filter missing/extra outputs by substring")
    p.add_argument("--limit", type=int, default=40, help="max rows per section")
    p.add_argument("--bucket-depth", type=int, default=4, help="path depth for bucket summary")
    p.add_argument("--show-cmd-diff", action="store_true", help="print unified diff for root cmd_args")
    return p


def main(argv: list[str]) -> int:
    args = build_parser().parse_args(argv)
    our = load_graph(args.our)
    ref = load_graph(args.ref)

    print(f"Nodes: our={len(our)} ref={len(ref)} delta={len(our) - len(ref):+}")
    print_kind_breakdown(our, ref)
    print_buckets(our, ref, args.bucket_depth, args.limit)
    print_output_delta(our, ref, args.limit, args.contains)

    if args.root_output:
        our_root = first_by_output_suffix(our, args.root_output)
        ref_root = first_by_output_suffix(ref, args.root_output)
        if our_root is None or ref_root is None:
            print()
            print(f"Root {args.root_output!r}: our={our_root is not None} ref={ref_root is not None}")
            return 1

        print()
        print(f"Root: {primary_output(our_root)}")
        print(f"Root kind: our={kind(our_root)} ref={kind(ref_root)}")
        summarize_root_field("deps", our_root, ref_root, args.limit)
        summarize_root_field("inputs", our_root, ref_root, args.limit)
        print_cmd_summary(our_root, ref_root, args.show_cmd_diff)

    return 1 if len(our) != len(ref) or output_index(our).keys() != output_index(ref).keys() else 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
