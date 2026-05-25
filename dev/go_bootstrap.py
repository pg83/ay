#!/usr/bin/env python3
"""Locate a usable Go binary for fresh worktrees with a minimal PATH."""

import glob
import os
import shutil
from functools import lru_cache

AY_GO_ENV = "AY_GO"
DEFAULT_GO_BINARY_CANDIDATES = (
    "/ix/realm/llm/bin/go",
    "/usr/local/go/bin/go",
    "/opt/homebrew/bin/go",
    "/home/linuxbrew/.linuxbrew/bin/go",
    "/run/current-system/sw/bin/go",
)
DEFAULT_GO_GLOB_PATTERNS = (
    "/ix/realm/*/bin/go",
    "/nix/var/nix/profiles/default/bin/go",
)


def _is_executable_file(path):
    return os.path.isfile(path) and os.access(path, os.X_OK)


def _iter_candidates(candidates, glob_patterns):
    seen = set()

    for candidate in candidates:
        absolute = os.path.abspath(candidate)
        if absolute in seen:
            continue
        seen.add(absolute)
        yield absolute

    for pattern in glob_patterns:
        for candidate in sorted(glob.glob(pattern)):
            absolute = os.path.abspath(candidate)
            if absolute in seen:
                continue
            seen.add(absolute)
            yield absolute


def resolve_go_binary(
    *,
    env=None,
    path=None,
    candidates=DEFAULT_GO_BINARY_CANDIDATES,
    glob_patterns=DEFAULT_GO_GLOB_PATTERNS,
):
    env = os.environ if env is None else env

    override = env.get(AY_GO_ENV)
    if override:
        override = os.path.abspath(override)
        if _is_executable_file(override):
            return override
        raise FileNotFoundError(f"{AY_GO_ENV} points to a non-executable go binary: {override}")

    found = shutil.which("go", path=path)
    if found:
        return os.path.abspath(found)

    for candidate in _iter_candidates(candidates, glob_patterns):
        if _is_executable_file(candidate):
            return candidate

    checked = ", ".join([*map(os.path.abspath, candidates), *glob_patterns])
    raise FileNotFoundError(
        f"go binary not found; checked PATH, {AY_GO_ENV}, and fallback candidates: {checked}. "
        f"Set {AY_GO_ENV}=/abs/path/to/go if your host installs it elsewhere."
    )


@lru_cache(maxsize=1)
def repo_go_binary():
    return resolve_go_binary()
