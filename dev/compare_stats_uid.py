#!/usr/bin/env python3
"""Compare raw-graph stats_uid values by normalized node identity."""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


def _load(path: Path) -> list[dict]:
    with path.open("r", encoding="utf-8") as fh:
        return json.load(fh)["graph"]


def _normalize_output(s: str) -> str:
    return s.replace("$(BUILD_ROOT)", "$(B)").replace("$(SOURCE_ROOT)", "$(S)")


def _key(node: dict) -> tuple[tuple[str, ...], str, bool, str]:
    return (
        tuple(sorted(_normalize_output(out) for out in node.get("outputs", []))),
        node.get("kv", {}).get("p", ""),
        bool(node.get("host_platform")),
        node.get("platform", ""),
    )


def _wants_node(node: dict, host_mode: str) -> bool:
    is_host = bool(node.get("host_platform"))
    if host_mode == "host":
        return is_host
    if host_mode == "target":
        return not is_host
    return True


def _index(nodes: list[dict], host_mode: str) -> dict[tuple[tuple[str, ...], str, bool, str], dict]:
    out: dict[tuple[tuple[str, ...], str, bool, str], dict] = {}
    for node in nodes:
        if not _wants_node(node, host_mode):
            continue
        key = _key(node)
        if key in out:
            raise ValueError(
                "duplicate node key for outputs="
                f"{list(key[0])}, kind={key[1]!r}, host_platform={key[2]}, platform={key[3]!r}"
            )
        out[key] = node
    return out


def _compare(our_path: Path, ref_path: Path, sample_limit: int, host_mode: str) -> int:
    our = _index(_load(our_path), host_mode)
    ref = _index(_load(ref_path), host_mode)

    common_keys = sorted(set(our) & set(ref))
    only_our = sorted(set(our) - set(ref))
    only_ref = sorted(set(ref) - set(our))

    mismatches: list[tuple[tuple[tuple[str, ...], str, bool, str], str, str]] = []
    for key in common_keys:
        our_uid = our[key].get("stats_uid", "")
        ref_uid = ref[key].get("stats_uid", "")
        if our_uid != ref_uid:
            mismatches.append((key, our_uid, ref_uid))

    print(f"host_mode: {host_mode}")
    print(f"common_nodes: {len(common_keys)}")
    print(f"only_our: {len(only_our)}")
    print(f"only_ref: {len(only_ref)}")
    print(f"stats_uid_mismatches: {len(mismatches)}")

    for key, our_uid, ref_uid in mismatches[:sample_limit]:
        print("mismatch_outputs:", json.dumps(list(key[0])))
        print(f"  kind: {key[1]}")
        print(f"  host_platform: {str(key[2]).lower()}")
        print(f"  platform: {key[3]}")
        print(f"  our: {our_uid}")
        print(f"  ref: {ref_uid}")

    return 0 if not only_our and not only_ref and not mismatches else 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--our", required=True, help="our raw graph JSON")
    parser.add_argument("--ref", required=True, help="reference raw graph JSON")
    parser.add_argument(
        "--host-mode",
        choices=("all", "host", "target"),
        default="all",
        help="compare all nodes, only host nodes, or only non-host target/shared nodes",
    )
    parser.add_argument("--sample-limit", type=int, default=5, help="max mismatch samples to print")
    args = parser.parse_args()

    try:
        return _compare(Path(args.our), Path(args.ref), args.sample_limit, args.host_mode)
    except Exception as exc:  # pragma: no cover - CLI guard
        print(f"error: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
