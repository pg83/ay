#!/usr/bin/env python3
"""normalize.py — bidirectional canonical normalizer + L4 comparator.

Applies an identical canonicalization pipeline to both OUR gen output and
the REF sg.json, then byte-compares the results.  L4 is a SEMANTIC equality
check (after canonicalization), not a syntactic one: whitespace, UID values,
and field-insertion order in the emitter become irrelevant; only graph
structure and build-content matter.

CLI (primary mode — canonicalize both + compare):
    ./normalize.py --our OUR.json --ref REF.json --target tools/archiver
                   [--our-out OUR-CANON.json] [--ref-out REF-CANON.json]

CLI (single-file debug mode):
    ./normalize.py --in SOME.json --target tools/archiver --out CANON.json

Exit codes:
    0 — L4 byte-exact match (or single-file mode succeeded)
    1 — L4 divergence
    2 — internal / argument error
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import sys
import time
from collections import deque
from typing import Any


# ---------------------------------------------------------------------------
# Step 1 — JSON parsing
# ---------------------------------------------------------------------------

def _load(path: str) -> dict[str, Any]:
    """Parse a JSON file and return the top-level object.

    The yatool emitter writes paths as `$(S)/<rel>` / `$(B)/<rel>` to
    keep node strings short; the reference graph uses the legacy
    `$(SOURCE_ROOT)/<rel>` / `$(BUILD_ROOT)/<rel>`. Compress the
    long forms to the short ones at file-load time (textual,
    pre-parse) so downstream comparison is uniform without
    sprinkling translation through the normaliser body.
    """
    try:
        with open(path, "rb") as fh:
            raw = fh.read()
        raw = raw.replace(b"$(BUILD_ROOT)", b"$(B)").replace(b"$(SOURCE_ROOT)", b"$(S)")
        return json.loads(raw)
    except (OSError, json.JSONDecodeError) as exc:
        _die(f"cannot load {path}: {exc}")


# ---------------------------------------------------------------------------
# Step 2 — Subgraph extraction
# ---------------------------------------------------------------------------

def _find_root(graph: list[dict], target: str) -> dict:
    """Return the LD (or AR fallback) root node for *target*.

    Search rules:
    1. LD node: kv['p'] == 'LD' AND outputs[0] is anchored under the
       target's build directory, i.e. starts with '$(BUILD_ROOT)/<target>/'.
       This accepts both `tools/archiver` (binary == 'archiver', LD out
       endswith '/archiver') and `devtools/ymake/bin` (binary == 'ymake',
       LD out endswith '/ymake' — the binary name differs from the last
       target-path component).
    2. AR node: kv['p'] == 'AR' AND outputs[0] contains '/<target>/'.
       Multiple AR candidates → prefer host_platform==false (target platform).
    """
    ld_prefix = "$(B)/" + target + "/"
    ar_infix = "/" + target + "/"

    ld_candidates: list[dict] = []
    ar_candidates: list[dict] = []

    for node in graph:
        kv_p = node.get("kv", {}).get("p", "")
        outputs = node.get("outputs") or []
        if not outputs:
            continue
        out0 = outputs[0]
        if kv_p == "LD" and out0.startswith(ld_prefix):
            ld_candidates.append(node)
        elif kv_p == "AR" and ar_infix in out0:
            ar_candidates.append(node)

    if len(ld_candidates) == 1:
        return ld_candidates[0]
    if len(ld_candidates) > 1:
        _die(
            f"found {len(ld_candidates)} LD nodes for target {target!r} "
            f"(outputs[0] ends with {ld_suffix!r}); expected exactly 1"
        )

    # AR fallback
    if len(ar_candidates) == 1:
        return ar_candidates[0]
    if len(ar_candidates) > 1:
        non_host = [n for n in ar_candidates if not n.get("host_platform")]
        if len(non_host) == 1:
            return non_host[0]
        _die(
            f"found {len(ar_candidates)} AR nodes for target {target!r} "
            f"(outputs[0] contains {ar_infix!r}); expected exactly 1"
        )

    _die(f"no LD or AR root node found for target {target!r}")


def _bfs_closure(
    by_uid: dict[str, dict],
    root: dict,
) -> dict[str, dict]:
    """BFS over deps[] and foreign_deps values from *root*.

    UIDs absent from *by_uid* are silently skipped (dangling foreign_deps
    references — sg.json has one such case: host ragel6 LD
    'SmqNjjowyeQt5wyQY6C8BA' lives outside the archiver subgraph).
    """
    closure: dict[str, dict] = {}
    queue: deque[str] = deque([root["uid"]])

    while queue:
        uid = queue.popleft()
        if uid in closure:
            continue
        node = by_uid.get(uid)
        if node is None:
            # Dangling UID — silently skip.
            continue
        closure[uid] = node
        for dep in node.get("deps") or []:
            if dep not in closure:
                queue.append(dep)
        # foreign_deps is not walked — dropped from normalization.

    return closure


# ---------------------------------------------------------------------------
# Steps 3–5 — Per-node field strip, canonicalization, dangling filter
# ---------------------------------------------------------------------------

def _strip_and_canonicalize(node: dict, closure: dict[str, dict]) -> dict:
    """Return a new, canonicalized copy of *node* suitable for re-UID hashing.

    Mutations applied (identical pipeline for OUR and REF):
    3. Drop stats_uid, cache, host_platform==false; prune empty foreign_deps.
    4. Sort inputs[], deps[], each foreign_deps[k] list, tags[].
       Set sandboxing=True if absent (REF always has it; our emitter now emits
       it via PR-L4-C — after normalization both sides must agree).
    5. Dangling foreign_deps filter: remove UIDs not in closure; prune empty
       keys and empty map.
    """
    out: dict[str, Any] = {}

    # cmds — preserved as-is (content, not order)
    out["cmds"] = node.get("cmds") or []

    # deps — sorted (resolves LD/AR insertion-order difference)
    out["deps"] = sorted(node.get("deps") or [])

    # env — preserved
    out["env"] = node.get("env") or {}

    # foreign_deps — dropped entirely (GOALS.md §L4 "Allowed normalizations":
    # non-semantic toolchain hint; REF has a dangling cross-subgraph UID,
    # OUR has a real local UID — structurally legitimate divergence).

    # host_platform — omit when false (omitempty equivalent)
    if node.get("host_platform"):
        out["host_platform"] = True

    # inputs — sorted (resolves 3598/3730 byte-order issue)
    out["inputs"] = sorted(node.get("inputs") or [])

    # kv
    out["kv"] = node.get("kv") or {}

    # outputs
    out["outputs"] = node.get("outputs") or []

    # platform
    out["platform"] = node.get("platform") or ""

    # requirements
    out["requirements"] = node.get("requirements") or {}

    # sandboxing — normalize to True (REF always has it; OUR emits it per PR-L4-C)
    out["sandboxing"] = True

    # self_uid — cleared for re-UID, set to uid post-cascade
    out["self_uid"] = ""

    # stats_uid — dropped (step 3)
    # cache — dropped (step 3)

    # tags — sorted if non-empty
    tags = node.get("tags") or []
    out["tags"] = sorted(tags) if tags else tags

    # target_properties
    out["target_properties"] = node.get("target_properties") or {}

    # uid — cleared for re-UID
    out["uid"] = ""

    return out


# ---------------------------------------------------------------------------
# Step 7 — Bottom-up re-UID (Merkle cascade with sha256)
# ---------------------------------------------------------------------------

_UID_LEN = 22


def _sha256_uid(canonical_bytes: bytes) -> str:
    """Compute base64url(sha256(canonical_bytes))[:22]."""
    digest = hashlib.sha256(canonical_bytes).digest()
    return base64.urlsafe_b64encode(digest).decode("ascii")[:_UID_LEN]


def _canonical_bytes(node: dict) -> bytes:
    """Stable JSON bytes for hashing: sort_keys=True, compact separators."""
    # uid and self_uid must already be "" at call time (cleared by caller).
    return json.dumps(node, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")


def _postorder_dfs(root_uid: str, closure: dict[str, dict]) -> list[str]:
    """Iterative post-order DFS (leaves first).

    Visits each node in *closure* reachable from *root_uid* via deps[].
    Handles cycles by treating back-edges as already-finished.
    """
    finished: set[str] = set()
    order: list[str] = []

    # Stack frames: (uid, children_iter, children_sorted)
    # Use a list-based stack to avoid Python recursion depth limits on
    # the ~3730-node archiver closure.
    stack: list[tuple[str, int, list[str]]] = []
    on_stack: set[str] = set()

    def push(uid: str) -> None:
        node = closure[uid]
        # Children = deps in closure (after sorting for determinism).
        # foreign_deps is not walked — dropped from normalization.
        children = sorted(
            d for d in (node.get("deps") or []) if d in closure
        )
        stack.append((uid, 0, children))
        on_stack.add(uid)

    push(root_uid)

    while stack:
        uid, idx, children = stack[-1]

        if idx < len(children):
            child = children[idx]
            stack[-1] = (uid, idx + 1, children)
            if child not in finished and child not in on_stack:
                push(child)
            # If already finished or on_stack (cycle), skip.
        else:
            stack.pop()
            on_stack.discard(uid)
            if uid not in finished:
                finished.add(uid)
                order.append(uid)

    return order


def _reuid_closure(
    closure: dict[str, dict],
    root_uid: str,
) -> dict[str, str]:
    """Compute new UIDs for all nodes in *closure*.

    Processes in post-order (leaves first) so each child's new UID is known
    before its parent is hashed.  Returns old_uid -> new_uid mapping.

    After substituting new UIDs in deps[] and foreign_deps, each list is
    re-sorted by the new UID values.  This is essential: original UIDs differ
    between OUR and REF for semantically identical children, so sorting by
    original UIDs (done in _strip_and_canonicalize) produces different
    orderings.  Re-sorting by new UIDs after substitution gives a canonical
    order independent of the original UID assignment.
    """
    postorder = _postorder_dfs(root_uid, closure)

    old_to_new: dict[str, str] = {}

    for old_uid in postorder:
        node = closure[old_uid]
        # Build canonical version with children's NEW UIDs substituted.
        canon = dict(node)  # shallow copy of already-canonicalized dict
        canon["uid"] = ""
        canon["self_uid"] = ""

        # Rewrite deps[] to new UIDs, then re-sort by new UID values so the
        # canonical order is independent of the original UID assignment.
        raw_deps = canon.get("deps") or []
        if raw_deps:
            canon["deps"] = sorted(old_to_new.get(d, d) for d in raw_deps)

        # foreign_deps is dropped — no rewrite needed.

        cb = _canonical_bytes(canon)
        new_uid = _sha256_uid(cb)
        old_to_new[old_uid] = new_uid

    return old_to_new


# ---------------------------------------------------------------------------
# Step 8 — DFS preorder for graph[] array ordering
# ---------------------------------------------------------------------------

def _dfs_preorder(
    root_uid: str,
    closure: dict[str, dict],
    old_to_new: dict[str, str],
) -> list[str]:
    """DFS preorder from *root_uid*, visiting deps[] children in sorted order
    of their NEW UIDs (so the traversal order is identical for OUR and REF
    after re-UID assigns the same new UIDs to semantically equal nodes).
    Returns old UIDs in preorder.
    """
    visited: set[str] = set()
    order: list[str] = []

    stack = [root_uid]

    while stack:
        uid = stack.pop()
        if uid in visited:
            continue
        visited.add(uid)
        order.append(uid)
        node = closure[uid]
        # Sort children by their NEW UID so traversal order is canonical.
        children_old = [
            d for d in (node.get("deps") or []) if d in closure
        ]
        children_old.sort(key=lambda d: old_to_new.get(d, d))
        # Push in REVERSE sorted order so leftmost (lex-first new UID) is
        # processed first.
        for child in reversed(children_old):
            if child not in visited:
                stack.append(child)

    return order


# ---------------------------------------------------------------------------
# Step 9 — Canonical JSON emission
# ---------------------------------------------------------------------------

def _emit_canonical(obj: dict) -> bytes:
    """Canonical JSON bytes: 4-space indent, sort_keys, no trailing newline."""
    return json.dumps(
        obj,
        indent=4,
        sort_keys=True,
        ensure_ascii=False,
    ).encode("utf-8")


# ---------------------------------------------------------------------------
# Top-level pipeline
# ---------------------------------------------------------------------------

def _normalize(data: dict[str, Any], target: str) -> tuple[bytes, dict]:
    """Run the full normalization pipeline on *data* for *target*.

    Returns (canonical_bytes, stats_dict).
    """
    t0 = time.monotonic()

    graph: list[dict] = data.get("graph") or []
    by_uid = {n["uid"]: n for n in graph}

    # Step 2 — find root + BFS closure
    root = _find_root(graph, target)
    root_uid = root["uid"]
    closure_raw = _bfs_closure(by_uid, root)

    t1 = time.monotonic()

    # Steps 3–5 — strip + canonicalize each node in closure
    closure: dict[str, dict] = {}
    for uid, node in closure_raw.items():
        closure[uid] = _strip_and_canonicalize(node, closure_raw)

    t2 = time.monotonic()

    # Step 7 — bottom-up re-UID
    old_to_new = _reuid_closure(closure, root_uid)

    t3 = time.monotonic()

    # Step 8 — DFS preorder ordering.  Children are visited in sorted order of
    # their NEW UIDs so the traversal order is canonical and identical between
    # OUR and REF (whose original UIDs differ for semantically equal nodes).
    preorder_old = _dfs_preorder(root_uid, closure, old_to_new)

    # Step 6 — build output graph[]
    result_graph: list[dict] = []
    for old_uid in preorder_old:
        canon = dict(closure[old_uid])
        new_uid = old_to_new[old_uid]

        # Substitute new UIDs in deps[] and re-sort by new values (same
        # canonical sort used in _reuid_closure so final bytes match the hash
        # input exactly).
        raw_deps = canon.get("deps") or []
        if raw_deps:
            canon["deps"] = sorted(old_to_new.get(d, d) for d in raw_deps)

        # foreign_deps is dropped — no substitution needed.

        canon["uid"] = new_uid
        canon["self_uid"] = new_uid  # step: self_uid := uid

        result_graph.append(canon)

    # Build result[] using new UIDs; fall back to root if original result
    # UIDs are outside the closure (e.g. build/cow/on against full sg.json).
    orig_result: list[str] = data.get("result") or []
    result_uids: list[str] = [
        old_to_new[u] for u in orig_result if u in closure_raw
    ]
    if not result_uids:
        result_uids = [old_to_new[root_uid]]

    # Step 6 — keep only graph and result at top level
    output = {
        "graph": result_graph,
        "result": result_uids,
    }

    t4 = time.monotonic()
    canon_bytes = _emit_canonical(output)
    t5 = time.monotonic()

    stats = {
        "closure_nodes": len(closure_raw),
        "output_nodes": len(result_graph),
        "t_bfs_ms": round((t1 - t0) * 1000, 1),
        "t_strip_ms": round((t2 - t1) * 1000, 1),
        "t_reuid_ms": round((t3 - t2) * 1000, 1),
        "t_emit_ms": round((t5 - t4) * 1000, 1),
    }

    return canon_bytes, stats


# ---------------------------------------------------------------------------
# Error helpers
# ---------------------------------------------------------------------------

def _die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(2)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def _build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        description="Bidirectional canonical normalizer + L4 comparator",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument("--target", required=True, help="target module path (e.g. tools/archiver)")

    grp = p.add_mutually_exclusive_group(required=True)
    grp.add_argument("--our", metavar="OUR_JSON", help="our gen output (primary mode)")
    grp.add_argument("--in", dest="single_in", metavar="JSON", help="single-file debug mode")

    p.add_argument("--ref", metavar="REF_JSON", help="reference sg.json (primary mode)")
    p.add_argument("--our-out", metavar="PATH", help="write canonicalized OUR to PATH")
    p.add_argument("--ref-out", metavar="PATH", help="write canonicalized REF to PATH")
    p.add_argument("--out", metavar="PATH", help="write canonicalized output (single-file mode)")
    return p


def _write(path: str, data: bytes) -> None:
    try:
        with open(path, "wb") as fh:
            fh.write(data)
    except OSError as exc:
        _die(f"cannot write {path}: {exc}")


def main() -> None:
    parser = _build_parser()
    args = parser.parse_args()

    t_start = time.monotonic()

    # --- Single-file debug mode ---
    if args.single_in is not None:
        if args.out is None:
            _die("single-file mode: --out is required")
        data = _load(args.single_in)
        canon_bytes, stats = _normalize(data, args.target)
        _write(args.out, canon_bytes)
        print(f"nodes: {stats['output_nodes']}")
        print(f"t_total_ms: {round((time.monotonic() - t_start) * 1000, 1)}")
        sys.exit(0)

    # --- Primary mode: compare OUR vs REF ---
    if args.ref is None:
        _die("primary mode: --ref is required when --our is given")

    our_data = _load(args.our)
    ref_data = _load(args.ref)

    our_bytes, our_stats = _normalize(our_data, args.target)
    ref_bytes, ref_stats = _normalize(ref_data, args.target)

    t_total = time.monotonic() - t_start

    # Metrics output
    print(f"our_nodes: {our_stats['output_nodes']}")
    print(f"ref_nodes: {ref_stats['output_nodes']}")
    print(f"t_total_s: {round(t_total, 2)}")
    print(f"our_sha256: {hashlib.sha256(our_bytes).hexdigest()}")
    print(f"ref_sha256: {hashlib.sha256(ref_bytes).hexdigest()}")

    # Optional output files
    if args.our_out:
        _write(args.our_out, our_bytes)
    if args.ref_out:
        _write(args.ref_out, ref_bytes)

    # L4 comparison
    if our_bytes == ref_bytes:
        print("L4: byte-exact")
        sys.exit(0)
    else:
        # Find first differing byte
        first_diff = next(
            (i for i, (a, b) in enumerate(zip(our_bytes, ref_bytes)) if a != b),
            min(len(our_bytes), len(ref_bytes)),
        )
        our_sha = hashlib.sha256(our_bytes).hexdigest()
        ref_sha = hashlib.sha256(ref_bytes).hexdigest()
        print(
            f"L4: differ (first diff at byte {first_diff}, "
            f"sha256 ours={our_sha[:16]}... refs={ref_sha[:16]}...)"
        )
        sys.exit(1)


if __name__ == "__main__":
    main()
