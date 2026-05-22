#!/usr/bin/env python3
"""diff.py — L4 divergence metrics over two canonical JSONL graphs.

STUB: metrics not yet implemented. Reads OUR and REF JSONL (one canonical
node per line, as emitted by normalize.py) and will report divergence
metrics. Kept as a no-op hook so validate.py's xfail path is wired; the
metric set will be filled in later.
"""
import argparse


def main() -> int:
    p = argparse.ArgumentParser(description="L4 divergence metrics over two JSONL graphs")
    p.add_argument("our", help="our canonical JSONL")
    p.add_argument("ref", help="reference canonical JSONL")
    p.parse_args()

    print("diff.py: metrics not yet implemented")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
