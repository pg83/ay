#!/usr/bin/env python3
"""mismatch.py - canonical self_uid mismatch counter for normalized graphs.

This tool compares the two outputs already written by `dev/normalize.py`.
It intentionally preserves the shared output-keyed metric:

- nodes are keyed by `tuple(node["outputs"])`
- duplicate output tuples are resolved by "last node wins"
- root vs propagation uses "equal modulo dep-uid values"
"""
from __future__ import annotations

import argparse
import json
import sys
from collections import defaultdict
from typing import Any


OutputKey = tuple[str, ...]


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    raise SystemExit(2)


def load_bytes(path: str) -> bytes:
    try:
        with open(path, "rb") as fh:
            return fh.read()
    except OSError as exc:
        die(f"cannot read {path}: {exc}")


def load_graph(path: str) -> list[dict[str, Any]]:
    raw = load_bytes(path)
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        die(f"cannot load {path}: {exc}")

    if isinstance(data, dict):
        graph = data.get("graph")
    else:
        graph = data

    if not isinstance(graph, list):
        die(f"{path}: expected normalized graph object or graph array")

    for idx, node in enumerate(graph):
        if not isinstance(node, dict):
            die(f"{path}: graph[{idx}] is not an object")

    return graph


def output_key(node: dict[str, Any]) -> OutputKey:
    return tuple(node.get("outputs") or [])


def primary_output(node: dict[str, Any]) -> str:
    outs = node.get("outputs") or []
    return outs[0] if outs else "<no-output>"


def kind(node: dict[str, Any]) -> str:
    return (node.get("kv") or {}).get("p", "")


def index_by_output(graph: list[dict[str, Any]]) -> dict[OutputKey, dict[str, Any]]:
    by_output: dict[OutputKey, dict[str, Any]] = {}
    for node in graph:
        by_output[output_key(node)] = node
    return by_output


def index_by_uid(graph: list[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    by_uid: dict[str, dict[str, Any]] = {}
    for node in graph:
        uid = node.get("uid")
        if isinstance(uid, str):
            by_uid[uid] = node
    return by_uid


def project_deps_to_output_keys(
    node: dict[str, Any],
    uid_index: dict[str, dict[str, Any]],
) -> list[tuple[str, ...]]:
    projected: list[tuple[str, ...]] = []
    for dep_uid in node.get("deps") or []:
        dep_node = uid_index.get(dep_uid)
        if dep_node is None:
            projected.append(("<dangling>", dep_uid))
            continue
        projected.append(output_key(dep_node))
    return projected


def intrinsic_view(node: dict[str, Any]) -> dict[str, Any]:
    return {
        name: value
        for name, value in node.items()
        if name not in {"uid", "self_uid", "deps"}
    }


def classify(
    our_node: dict[str, Any],
    ref_node: dict[str, Any],
    our_uid_index: dict[str, dict[str, Any]],
    ref_uid_index: dict[str, dict[str, Any]],
) -> str:
    if intrinsic_view(our_node) != intrinsic_view(ref_node):
        return "root"

    our_deps = sorted(project_deps_to_output_keys(our_node, our_uid_index))
    ref_deps = sorted(project_deps_to_output_keys(ref_node, ref_uid_index))
    if our_deps == ref_deps:
        return "propagation"
    return "root"


def module_bucket(node: dict[str, Any], depth: int) -> str:
    path = primary_output(node)
    if path == "<no-output>":
        return path
    for prefix in ("$(B)/", "$(S)/"):
        if path.startswith(prefix):
            path = path[len(prefix):]
            break
    parts = [part for part in path.split("/") if part]
    if not parts:
        return "<no-output>"
    return "/".join(parts[:depth]) if len(parts) >= depth else "/".join(parts)


def record_for_node(
    our_node: dict[str, Any],
    ref_node: dict[str, Any],
    mismatch_class: str,
    module_depth: int,
) -> dict[str, Any]:
    node_kind = kind(our_node) or kind(ref_node)
    return {
        "outputs": list(output_key(our_node)),
        "primary_output": primary_output(our_node),
        "kind": node_kind,
        "class": mismatch_class,
        "module": module_bucket(our_node, module_depth),
        "our_self_uid": our_node.get("self_uid"),
        "ref_self_uid": ref_node.get("self_uid"),
    }


def summarized_counts(
    counts: dict[str, dict[str, int]],
) -> list[dict[str, Any]]:
    rows = []
    for name, row in counts.items():
        total = row["root"] + row["propagation"]
        rows.append(
            {
                "name": name,
                "root": row["root"],
                "propagation": row["propagation"],
                "total": total,
            }
        )
    rows.sort(key=lambda row: (-row["total"], row["name"]))
    return rows


def build_report(
    our_graph: list[dict[str, Any]],
    ref_graph: list[dict[str, Any]],
    byte_exact: bool,
    module_depth: int,
) -> dict[str, Any]:
    our_by_output = index_by_output(our_graph)
    ref_by_output = index_by_output(ref_graph)
    our_uid_index = index_by_uid(our_graph)
    ref_uid_index = index_by_uid(ref_graph)

    our_keys = set(our_by_output)
    ref_keys = set(ref_by_output)
    shared_keys = sorted(our_keys & ref_keys)
    missing_keys = sorted(ref_keys - our_keys)
    extra_keys = sorted(our_keys - ref_keys)

    by_kind_counts: dict[str, dict[str, int]] = defaultdict(lambda: {"root": 0, "propagation": 0})
    by_module_counts: dict[str, dict[str, int]] = defaultdict(lambda: {"root": 0, "propagation": 0})
    mismatch_records: list[dict[str, Any]] = []

    root_count = 0
    propagation_count = 0

    for key in shared_keys:
        our_node = our_by_output[key]
        ref_node = ref_by_output[key]
        if our_node.get("self_uid") == ref_node.get("self_uid"):
            continue

        mismatch_class = classify(our_node, ref_node, our_uid_index, ref_uid_index)
        if mismatch_class == "root":
            root_count += 1
        else:
            propagation_count += 1

        mismatch_records.append(record_for_node(our_node, ref_node, mismatch_class, module_depth))

        node_kind = kind(our_node) or kind(ref_node)
        module = module_bucket(our_node, module_depth)
        by_kind_counts[node_kind][mismatch_class] += 1
        by_module_counts[module][mismatch_class] += 1

    mismatch_records.sort(key=lambda record: (record["primary_output"], record["outputs"]))

    missing_records = [
        {
            "outputs": list(key),
            "primary_output": primary_output(ref_by_output[key]),
            "kind": kind(ref_by_output[key]),
            "module": module_bucket(ref_by_output[key], module_depth),
        }
        for key in missing_keys
    ]
    extra_records = [
        {
            "outputs": list(key),
            "primary_output": primary_output(our_by_output[key]),
            "kind": kind(our_by_output[key]),
            "module": module_bucket(our_by_output[key], module_depth),
        }
        for key in extra_keys
    ]

    return {
        "our_nodes": len(our_graph),
        "ref_nodes": len(ref_graph),
        "byte_exact": byte_exact,
        "missing_outputs": len(missing_keys),
        "extra_outputs": len(extra_keys),
        "mismatches": len(mismatch_records),
        "root": root_count,
        "propagation": propagation_count,
        "by_kind": summarized_counts(by_kind_counts),
        "by_module": summarized_counts(by_module_counts),
        "mismatch_records": mismatch_records,
        "missing_records": missing_records,
        "extra_records": extra_records,
    }


def print_report(report: dict[str, Any], top: int) -> None:
    print(f"our_nodes: {report['our_nodes']}")
    print(f"ref_nodes: {report['ref_nodes']}")
    print(f"byte_exact: {str(report['byte_exact']).lower()}")
    print(f"missing_outputs: {report['missing_outputs']}")
    print(f"extra_outputs: {report['extra_outputs']}")
    print(f"mismatches: {report['mismatches']}")
    print(f"root: {report['root']}")
    print(f"propagation: {report['propagation']}")

    print("by_kind:")
    by_kind = report["by_kind"]
    if not by_kind:
        print("  <none>")
    else:
        for row in by_kind[:top]:
            label = row["name"] or "<none>"
            print(
                f"  {label} root={row['root']} prop={row['propagation']} total={row['total']}"
            )

    print(f"by_module:")
    by_module = report["by_module"]
    if not by_module:
        print("  <none>")
    else:
        for row in by_module[:top]:
            print(
                f"  {row['name']} root={row['root']} prop={row['propagation']} total={row['total']}"
            )


def write_json_report(report: dict[str, Any], path: str) -> None:
    try:
        with open(path, "w", encoding="utf-8") as fh:
            json.dump(report, fh, indent=2, sort_keys=True, ensure_ascii=False)
            fh.write("\n")
    except OSError as exc:
        die(f"cannot write {path}: {exc}")


def load_json_report(path: str) -> dict[str, Any]:
    try:
        with open(path, encoding="utf-8") as fh:
            data = json.load(fh)
    except (OSError, json.JSONDecodeError) as exc:
        die(f"cannot load baseline {path}: {exc}")
    if not isinstance(data, dict):
        die(f"{path}: expected JSON object baseline report")
    records = data.get("mismatch_records")
    if not isinstance(records, list):
        die(f"{path}: expected mismatch_records[] in baseline report")
    return data


def print_baseline_delta(report: dict[str, Any], baseline_path: str) -> None:
    baseline = load_json_report(baseline_path)

    now_keys = {
        tuple(record.get("outputs") or [])
        for record in report["mismatch_records"]
    }
    baseline_keys = {
        tuple(record.get("outputs") or [])
        for record in baseline["mismatch_records"]
    }

    fixed = baseline_keys - now_keys
    new = now_keys - baseline_keys

    print(f"baseline: {baseline.get('mismatches', len(baseline_keys))}")
    print(f"now: {report['mismatches']}")
    print(f"fixed: {len(fixed)}")
    print(f"new: {len(new)}")
    print(f"subset_of_baseline: {str(now_keys <= baseline_keys).lower()}")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Compare normalize.py outputs with canonical output-keyed self_uid mismatch counts."
    )
    parser.add_argument("--our", required=True, help="normalized OUR graph JSON")
    parser.add_argument("--ref", required=True, help="normalized REF graph JSON")
    parser.add_argument("--module-depth", type=int, default=4, help="path depth for module buckets")
    parser.add_argument("--top", type=int, default=20, help="max rows to print in summary tables")
    parser.add_argument("--json", help="write machine-readable report to PATH")
    parser.add_argument("--baseline", help="compare mismatch keys to a prior --json report")
    return parser


def main(argv: list[str]) -> int:
    args = build_parser().parse_args(argv)
    if args.module_depth <= 0:
        die("--module-depth must be positive")
    if args.top <= 0:
        die("--top must be positive")

    our_raw = load_bytes(args.our)
    ref_raw = load_bytes(args.ref)
    our_graph = load_graph(args.our)
    ref_graph = load_graph(args.ref)

    report = build_report(
        our_graph=our_graph,
        ref_graph=ref_graph,
        byte_exact=(our_raw == ref_raw),
        module_depth=args.module_depth,
    )
    print_report(report, top=args.top)

    if args.json:
        write_json_report(report, args.json)
    if args.baseline:
        print_baseline_delta(report, args.baseline)

    if report["byte_exact"] and report["mismatches"] == 0:
        return 0
    return 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
