#!/usr/bin/env python3
"""Pack and compare canonical validate refs."""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import lzma
import sys
from pathlib import Path
from typing import Any


SCRIPT_DIR = Path(__file__).resolve().parent
NORMALIZE_PATH = SCRIPT_DIR / "normalize.py"


def _load_normalize_module() -> Any:
    spec = importlib.util.spec_from_file_location("validate_normalize", NORMALIZE_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {NORMALIZE_PATH}")

    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)

    return module


def _canonicalize_graph(path: Path, target: str) -> tuple[bytes, dict[str, Any]]:
    normalize = _load_normalize_module()
    data = normalize._load(str(path))

    return normalize._normalize(data, target)


def _write_bytes(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)


def _pack(raw: Path, target: str, out: Path) -> int:
    canon_bytes, stats = _canonicalize_graph(raw, target)
    out.parent.mkdir(parents=True, exist_ok=True)

    with lzma.open(out, "wb", preset=6) as fh:
        fh.write(canon_bytes)

    print(f"nodes: {stats['output_nodes']}")
    print(f"sha256: {hashlib.sha256(canon_bytes).hexdigest()}")

    return 0


def _compare(our: Path, ref: Path, target: str, our_out: Path | None, ref_out: Path | None) -> int:
    our_bytes, our_stats = _canonicalize_graph(our, target)
    ref_bytes = lzma.open(ref, "rb").read()

    if our_out is not None:
        _write_bytes(our_out, our_bytes)
    if ref_out is not None:
        _write_bytes(ref_out, ref_bytes)

    print(f"our_nodes: {our_stats['output_nodes']}")
    print(f"our_sha256: {hashlib.sha256(our_bytes).hexdigest()}")
    print(f"ref_sha256: {hashlib.sha256(ref_bytes).hexdigest()}")

    if our_bytes == ref_bytes:
        print("L4: byte-exact")
        return 0

    first_diff = next(
        (i for i, (a, b) in enumerate(zip(our_bytes, ref_bytes)) if a != b),
        min(len(our_bytes), len(ref_bytes)),
    )
    print(f"L4: differ (first diff at byte {first_diff})")

    return 1


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Pack and compare compressed canonical validate refs",
    )
    subparsers = parser.add_subparsers(dest="cmd", required=True)

    pack = subparsers.add_parser("pack", help="canonicalize a raw ref and compress it")
    pack.add_argument("--raw", required=True, help="raw upstream graph JSON")
    pack.add_argument("--target", required=True, help="target module path")
    pack.add_argument("--out", required=True, help="output .json.xz path")

    compare = subparsers.add_parser("compare", help="canonicalize OUR graph and compare against a packed ref")
    compare.add_argument("--our", required=True, help="our raw graph JSON")
    compare.add_argument("--ref", required=True, help="packed canonical ref (.json.xz)")
    compare.add_argument("--target", required=True, help="target module path")
    compare.add_argument("--our-out", help="write canonicalized OUR graph here")
    compare.add_argument("--ref-out", help="write unpacked canonical REF graph here")

    return parser


def main() -> int:
    parser = _build_parser()
    args = parser.parse_args()

    try:
        if args.cmd == "pack":
            return _pack(Path(args.raw), args.target, Path(args.out))

        return _compare(
            Path(args.our),
            Path(args.ref),
            args.target,
            Path(args.our_out) if args.our_out else None,
            Path(args.ref_out) if args.ref_out else None,
        )
    except Exception as exc:  # pragma: no cover - CLI guard
        print(f"error: {exc}", file=sys.stderr)

        return 2


if __name__ == "__main__":
    sys.exit(main())
