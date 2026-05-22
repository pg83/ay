#!/usr/bin/env python3
"""normalize.py — canonical L4 normalizer, emitting JSONL.

Applies the canonicalization pipeline to one raw graph (OUR gen output or a
REF sg.json) and writes the target closure as JSONL: one canonical node per
line, sorted by canonical uid. L4 canonicalization makes whitespace, UID
values, and emitter field order irrelevant; only graph structure and
build-content survive. JSONL output lets downstream processors (cmp for
byte-exact cases, diff.py for metrics) stream node-by-node.

CLI:
    ./dev/normalize.py --in RAW.json --target devtools/ymake/bin --out CANON.jsonl

Exit codes:
    0 — success
    2 — internal / argument error
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import re
import sys
import time
from collections import deque
from typing import Any


# ---------------------------------------------------------------------------
# Step 1 — JSON parsing
# ---------------------------------------------------------------------------

def _load(path: str) -> dict[str, Any]:
    """Parse a JSON file and return the top-level object.

    The ay emitter writes paths as `$(S)/<rel>` / `$(B)/<rel>` to
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
        raw = re.sub(rb"\$\((CLANG|LLD_ROOT|YMAKE_PYTHON3)-[0-9]+\)", rb"$(\1)", raw)
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


def _find_roots(graph: list[dict], target: str) -> list[dict]:
    """Return all closure roots for *target*.

    This is the binary LD/AR root, plus any TS test-run nodes whose
    kv.path lives under "<target>/". For non-test targets this remains a
    single-root pipeline.
    """
    ld_prefix = "$(B)/" + target + "/"
    ar_infix = "/" + target + "/"

    has_binary = False
    for node in graph:
        kv_p = node.get("kv", {}).get("p", "")
        outputs = node.get("outputs") or []
        if not outputs:
            continue
        out0 = outputs[0]
        if kv_p == "LD" and out0.startswith(ld_prefix):
            has_binary = True
            break
        if kv_p == "AR" and ar_infix in out0:
            has_binary = True

    ts_roots = [
        node for node in graph
        if node.get("kv", {}).get("p") == "TS"
        and (node.get("kv", {}).get("path") or "").startswith(target + "/")
    ]

    roots: list[dict] = []
    if has_binary:
        roots.append(_find_root(graph, target))
    roots.extend(ts_roots)

    if not roots:
        _die(f"no LD/AR/TS root node found for target {target!r}")

    seen: set[str] = set()
    deduped: list[dict] = []
    for node in roots:
        uid = node["uid"]
        if uid in seen:
            continue
        seen.add(uid)
        deduped.append(node)

    return deduped


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


def _drop_fetch_nodes(closure: dict[str, dict]) -> dict[str, dict]:
    """Remove ay-local resource fetch nodes from OUR graphs.

    Reference graphs materialize tool resources outside the build graph.
    ay intentionally models them as explicit FETCH nodes so local
    executor UIDs depend on the downloaded resources.  For L4 graph
    comparison they are non-reference scaffolding, so drop them and
    remove corresponding deps from consumers.
    """
    fetch_uids = {
        uid
        for uid, node in closure.items()
        if (node.get("kv") or {}).get("p") == "FETCH"
    }

    if not fetch_uids:
        return closure

    out: dict[str, dict] = {}

    for uid, node in closure.items():
        if uid in fetch_uids:
            continue

        copy = dict(node)
        copy["deps"] = [
            dep for dep in (node.get("deps") or [])
            if dep not in fetch_uids
        ]
        out[uid] = copy

    return out


# ---------------------------------------------------------------------------
# Step 7 — Bottom-up re-UID (Merkle cascade with sha256)
# ---------------------------------------------------------------------------

_UID_LEN = 22
_SYNTH_ROOT = "__ay_synthetic_superroot__"


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

    # Step 2 — find root(s) + BFS closure. -ttt TS nodes sit above the LD
    # binary, so include the binary root and any target-local test-run roots.
    roots = _find_roots(graph, target)
    root_uids = [root["uid"] for root in roots]
    if len(roots) == 1:
        root_uid = root_uids[0]
        closure_raw = _bfs_closure(by_uid, roots[0])
    else:
        closure_raw = {}
        for root in roots:
            closure_raw.update(_bfs_closure(by_uid, root))
        closure_raw[_SYNTH_ROOT] = {
            "uid": _SYNTH_ROOT,
            "deps": sorted(root_uids),
            "kv": {},
        }
        root_uid = _SYNTH_ROOT
    closure_raw = _drop_fetch_nodes(closure_raw)

    t1 = time.monotonic()

    # Steps 3–5 — strip + canonicalize each node in closure
    closure: dict[str, dict] = {}
    for uid, node in closure_raw.items():
        closure[uid] = _strip_and_canonicalize(node, closure_raw)

    t2 = time.monotonic()

    # Step 7 — bottom-up re-UID
    old_to_new = _reuid_closure(closure, root_uid)

    t3 = time.monotonic()

    # Step 8 — DFS preorder gives a deterministic base ordering (children
    # visited in sorted order of their NEW UIDs).
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

    # graph[] array order is non-semantic: nodes reference their
    # dependencies by UID (deps[]), never by array position, so any
    # permutation of graph[] denotes the same graph. DFS preorder alone is
    # NOT a complete canonicalization — the relative position of two
    # identical-content nodes reachable through different ancestors depends
    # on those ancestors' UIDs, so while OUR and REF are not yet isomorphic
    # (some cluster still diverging) the same node can land at different
    # indices in OUR vs REF. Positional / keep-last analyses then misread
    # that as a content change, most visibly for dual-variant (host+target)
    # outputs that share one output path (e.g. $(B)/certs/objcopy_*.o
    # emitted by both the x86_64 host-tool and aarch64 target builds). A
    # final total-order sort by the canonical NEW UID removes that degree of
    # freedom; at convergence OUR and REF still serialize byte-identically.
    result_graph.sort(key=lambda canon: canon["uid"])

    if root_uid == _SYNTH_ROOT:
        synth_new = old_to_new[root_uid]
        result_graph = [node for node in result_graph if node["uid"] != synth_new]
        result_uids = sorted(old_to_new[uid] for uid in root_uids)
    else:
        # Build result[] using new UIDs; fall back to root if original result
        # UIDs are outside the closure (e.g. build/cow/on against full sg.json).
        orig_result: list[str] = data.get("result") or []
        result_uids: list[str] = [
            old_to_new[u] for u in orig_result if u in closure_raw
        ]
        if not result_uids:
            result_uids = [old_to_new[root_uid]]

    # Step 6 — return the canonical node list (sorted by uid). The JSONL
    # writer emits one node per line so downstream metrics processors can
    # stream node-by-node. result_uids is dropped: only the node bodies
    # ("нодовая часть") are emitted.
    stats = {
        "closure_nodes": len(closure_raw),
        "output_nodes": len(result_graph),
        "t_bfs_ms": round((t1 - t0) * 1000, 1),
        "t_strip_ms": round((t2 - t1) * 1000, 1),
        "t_reuid_ms": round((t3 - t2) * 1000, 1),
    }

    return result_graph, stats


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
        description="Canonical L4 normalizer → JSONL (one canonical node per line)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument("--in", dest="single_in", metavar="JSON", required=True, help="raw graph JSON")
    p.add_argument("--target", required=True, help="target module path (e.g. devtools/ymake/bin)")
    p.add_argument("--out", metavar="PATH", required=True, help="write canonical JSONL here")
    return p


def _write_jsonl(path: str, graph: list[dict]) -> None:
    try:
        with open(path, "w", encoding="utf-8") as fh:
            for node in graph:
                fh.write(json.dumps(node, sort_keys=True, ensure_ascii=False, separators=(",", ":")))
                fh.write("\n")
    except OSError as exc:
        _die(f"cannot write {path}: {exc}")


def main() -> None:
    parser = _build_parser()
    args = parser.parse_args()

    t_start = time.monotonic()
    data = _load(args.single_in)
    graph, stats = _normalize(data, args.target)
    _write_jsonl(args.out, graph)

    print(f"nodes: {stats['output_nodes']}")
    print(f"t_total_ms: {round((time.monotonic() - t_start) * 1000, 1)}")


if __name__ == "__main__":
    main()
