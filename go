#!/usr/bin/env python3
"""Repo-local Go shim for worktrees whose PATH does not include Go."""

import os
import sys

REPO_ROOT = os.path.dirname(os.path.abspath(__file__))
DEV_DIR = os.path.join(REPO_ROOT, "dev")
if DEV_DIR not in sys.path:
    sys.path.insert(0, DEV_DIR)

import go_bootstrap


def main():
    try:
        go_binary = go_bootstrap.repo_go_binary()
    except FileNotFoundError as exc:
        print(exc, file=sys.stderr)
        return 1
    os.execv(go_binary, [go_binary, *sys.argv[1:]])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
