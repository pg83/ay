# L4 Byte-Exact — Implementation Roadmap

**Date:** 2026-05-11

**Acceptance criterion (verbatim from `GOALS.md` §Definition-of-done item 2):**

> **L4 byte-exact**: `sha256(./yatool make -j 0 -G tools/archiver > -)`
> equals `sha256(normalized-ref-for-tools-archiver.json)`, where the
> normalized reference is produced by the documented one-time
> normalization pass over `/home/pg/monorepo/yatool_orig/sg.json` (strip
> `conf`; re-canonicalize syntactic-equivalent JSON; re-UID via our
> fingerprint).

**One-line approach:** Build a one-time deterministic normalizer
(in-repo `./yatool normalize` subcommand) that extracts the
3730-node `tools/archiver` closure from `sg.json`, strips
`conf`/`stats_uid`, mirrors our struct shape, re-UIDs via our
fingerprint, and emits canonical bytes (4-space indent,
HTML-unescaped, **no trailing newline**, DFS-preorder graph
ordering, sandboxing preserved). Concurrently change our emitter
to emit the same canonical bytes (sandboxing field, indent=4, no
trailing newline, DFS-preorder graph, source-order `deps` for
LD/AR, REF-order `inputs`, real `self_uid`, drop `stats_uid`).
After both passes agree on the byte schema, `sha256` equality is
the L4 gate.

---

## 1. Empirical baseline

All commands assume working directory `/home/pg/monorepo/yatool`.
Reference is `/home/pg/monorepo/yatool_orig/sg.json`; OUR latest output
is `./.out/sg.json` (produced by `./yatool make -j 0 -G tools/archiver`,
73 MB vs 63 MB on disk).

### 1.1 File metadata

```bash
ls -la /home/pg/monorepo/yatool_orig/sg.json /home/pg/monorepo/yatool/.out/sg.json
```

Observed (2026-05-11):

```
73,479,604  /home/pg/monorepo/yatool_orig/sg.json     (REF)
63,044,547  /home/pg/monorepo/yatool/.out/sg.json     (OUR)
```

Delta: **~10.4 MB** (14.2 %).

### 1.2 Top-level structure (both files)

```bash
python3 -c "import json; d=json.load(open('/home/pg/monorepo/yatool_orig/sg.json')); print(list(d.keys()), len(d['graph']))"
```

Both have keys `["conf", "graph", "inputs", "result"]`, both have
3730 graph nodes, both have a single-entry `result` array,
both have `inputs == {}` (empty dict).

```
REF result: ["Ze_eMOLqyMsa6WlbbMhgvQ"]
OUR result: ["3tPpxZznv8FPTzwjXlf_iQ"]
```

### 1.3 Indentation, key escape, trailing newline

```bash
head -c 60 /home/pg/monorepo/yatool_orig/sg.json | od -c | head -2
head -c 60 /home/pg/monorepo/yatool/.out/sg.json | od -c | head -2
tail -c 8 /home/pg/monorepo/yatool_orig/sg.json | od -c
tail -c 8 /home/pg/monorepo/yatool/.out/sg.json | od -c
```

| | REF | OUR |
|---|---|---|
| Indent | **4 spaces** | 2 spaces |
| Trailing | `]\n}` (no terminal `\n`) | `]\n}\n` (terminal `\n`) |
| HTML escape | off | off (`gjson_write.go:31-33`) |

### 1.4 Per-node key set

```bash
python3 -c "import json; print(sorted(json.load(open('/home/pg/monorepo/yatool_orig/sg.json'))['graph'][0].keys()))"
```

REF node keys (15): `cmds, deps, env, host_platform?, inputs, kv,
outputs, platform, requirements, sandboxing, self_uid, stats_uid,
tags, target_properties, uid` (+ `foreign_deps?` on 26 nodes).

OUR node keys (14): same minus `sandboxing`.

Optional fields:

| Field | REF presence | OUR presence |
|---|---|---|
| `host_platform` | 1797 / 3730 | 1797 / 3730 — matches |
| `foreign_deps` | 26 / 3730 | 25 / 3730 — **off by 1** |
| `sandboxing` | 3730 / 3730 (always `true`) | 0 / 3730 — **missing** |

The 1 missing `foreign_deps` is on
`$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp`
(`default-linux-aarch64`), an R6 emit; tracked in §4 / PR-L4-C.

### 1.5 UID, SelfUID, StatsUID lengths

```bash
python3 -c "
import json
d = json.load(open('/home/pg/monorepo/yatool_orig/sg.json'))
import collections
print(collections.Counter(len(n['uid']) for n in d['graph']))
print(collections.Counter(len(n['self_uid']) for n in d['graph']))
print(collections.Counter(len(n['stats_uid']) for n in d['graph']))
"
```

| Field | REF | OUR |
|---|---|---|
| `uid` length | 22 chars (uniform) | 22 chars (uniform) |
| `self_uid` length | 22 chars (uniform) | 22 chars (uniform) |
| `stats_uid` length | **32 chars (hex, uniform)** | **empty string** |
| `self_uid == uid` count | 3629 / 3730 | 3730 / 3730 (placeholder per `uid.go:395-400`) |

REF carries a distinct `self_uid` for 101 nodes (LD + AR — order-sensitive aggregators), while OUR currently sets `SelfUID := UID` for every node.

### 1.6 Bytes-budget decomposition (sums to within ~180 KB of the 10.4 MB delta)

```bash
python3 -c "
import json
d = json.load(open('/home/pg/monorepo/yatool_orig/sg.json'))
print('conf size (indent=4):', len(json.dumps(d['conf'], indent=4)))
print('sandboxing total:', sum(len(json.dumps(n.get('sandboxing'))) for n in d['graph']))
print('stats_uid total chars:', sum(len(n.get('stats_uid','')) for n in d['graph']))
"
python3 -c "
import json
o = json.load(open('/home/pg/monorepo/yatool/.out/sg.json'))
print('OUR re-serialized indent=4:', len(json.dumps(o, indent=4, ensure_ascii=False)))
"
```

Measured contributions to the 10.4 MB delta:

| Source | Bytes |
|---|---|
| Whitespace (indent 2 → indent 4 re-render of OUR) | **~10,115,782** (≈ 96.5 %) |
| REF `conf` block | 2,442 |
| REF per-node `sandboxing: true,\n<pad>` (3730 × ~32 incl. indent) | ~15,000 raw value + indent surrounding |
| REF per-node `stats_uid` value (32 chars × 3730) | 119,360 |
| REF nodes are slightly larger (sandboxing line + non-empty stats_uid string) | ~140,000 |
| Trailing-newline difference | 1 |
| Sum (rounded) | ~10,250,000 |

Remaining unexplained: ~150 KB — accounted for by:
- the missing R6 `foreign_deps` field (one node, ~80 bytes),
- 1797 `host_platform: true,\n<pad>` lines at deeper indent in REF (negligible per line, sum ~negligible),
- subtle per-node variance from inputs that differ slightly (multi-set equal, byte-different — see §1.8).

This confirms **the 14 % size delta is dominated by indentation choice** (factor ~10 MB), not by missing graph content. The structural deltas (sandboxing, stats_uid, conf) are bookkeeping.

### 1.7 Per-node `inputs` ordering

```bash
python3 -c "
import json
ref = json.load(open('/home/pg/monorepo/yatool_orig/sg.json'))
our = json.load(open('/home/pg/monorepo/yatool/.out/sg.json'))
ref_by = {(n['outputs'][0], n['platform']): n for n in ref['graph']}
order_eq = sort_eq = 0
for n in our['graph']:
    rn = ref_by.get((n['outputs'][0], n['platform']))
    if rn is None: continue
    if rn['inputs'] == n['inputs']: order_eq += 1
    if sorted(rn['inputs']) == sorted(n['inputs']): sort_eq += 1
print('inputs byte-order match:', order_eq, '/ 3730')
print('inputs multiset match:', sort_eq, '/ 3730')
"
```

- Multiset equal: **3730 / 3730** (L2 = 100 % already proved this).
- Byte-order equal: **132 / 3730** — the `inputs` array order differs on
  3598 nodes. REF preserves the include-scan walk order (not lexical);
  OUR currently emits source-first-then-scan-order, with the scanner's
  internal traversal not byte-identical to ymake's.
- Neither side has `inputs == sorted(inputs)` for most nodes (79 / 3730
  on each side, mostly trivially-small-input nodes).

This is the **principal node-content L4 gap**, and the most expensive
to close. See §3 and §4 for the design.

### 1.8 `deps` ordering

```bash
python3 -c "
import json, collections
ref = json.load(open('/home/pg/monorepo/yatool_orig/sg.json'))
unsorted = collections.Counter()
for n in ref['graph']:
    if n['deps'] != sorted(n['deps']):
        unsorted[n['kv'].get('p')] += 1
print('REF nodes with deps not in lex order:', dict(unsorted))
"
```

REF: 30 / 3730 nodes have `deps` in non-lex order, namely all 3 LDs
and 27 of 48 ARs. OUR sorts `deps` unconditionally (`emitter.go:328-342`).
For L4 the LD/AR `deps` arrays must preserve emit (link) order, not lex
sort. CC/AS/JS/R6/CP `deps` are either empty or coincidentally sorted.

### 1.9 Top-level `graph[]` array order

```bash
python3 <<'PY'
import json, sys; sys.setrecursionlimit(20000)
ref = json.load(open('/home/pg/monorepo/yatool_orig/sg.json'))
nodes = {n['uid']: n for n in ref['graph']}
result = ref['result'][0]
seen = set(); order_dfs = []
def dfs(u):
    if u in seen: return
    seen.add(u); order_dfs.append(u)
    for d in nodes[u]['deps']: dfs(d)
dfs(result)
ref_order = [n['uid'] for n in ref['graph']]
print('DFS-preorder-from-result match REF graph[] order?', order_dfs == ref_order)
PY
```

**REF `graph[]` order is DFS preorder rooted at `result[0]`, visiting
children in declared `deps[]` order.** Verified byte-exact (3730 / 3730
positions match). OUR currently emits in Kahn-topo-leaves-first order
(`emitter.go:283-308`): tested with the same script — OUR's order is
exactly the reverse of REF's (children-before-parents). Critical L4 gap.

### 1.10 Cmd-level structure

REF cmd keys: `cmd_args, env` (no `cwd` on most); 61 / 3739 cmds carry
`cwd`. OUR matches: 61 / 3739 cmds carry non-empty `cwd`
(`node.go:34-38`'s `omitempty`).

### 1.11 Top-level `inputs` field

REF: `{}` (empty dict). OUR: `{}` (empty dict). **No emission gap**; the
field is intentionally empty in both ymake's sg.json *and* our emitter
(`emitter.go:417`, `gjson_write.go:89-90`). The risk in §8 about
`inputs` semantics is resolved: this is a stub object in both.

### 1.12 `result` array

REF and OUR each carry exactly 1 entry — the tools/archiver LD UID.
After the normalizer re-UIDs REF via our fingerprint algorithm, REF's
`result[0]` becomes our LD's UID; the comparator confirms equality.

---

## 2. Empirical baseline — summary table

| Aspect | REF | OUR | L4 gap? |
|---|---|---|---|
| Top-level keys | `conf, graph, inputs, result` | same | strip `conf` from REF |
| `conf` content | populated (cache, gsid, resources, …) | `{}` | strip (allowed) |
| `inputs` content | `{}` | `{}` | none |
| `graph[]` length | 3730 | 3730 | none |
| `graph[]` order | DFS preorder from `result[0]` | Kahn topo (leaves first) | **YES** — change OUR |
| Indent | 4 spaces | 2 spaces | **YES** — change OUR |
| Trailing newline | none | `\n` | **YES** — change OUR |
| Per-node `sandboxing` | always `true` | absent | **YES** — emit `true` |
| Per-node `stats_uid` | 32-char hex (MD5-shape) | empty | drop from REF (option a) |
| Per-node `self_uid` | distinct on 101 LD/AR nodes | == `uid` everywhere | **YES** — derive properly OR strip-and-mirror |
| Per-node `foreign_deps` | 26 / 3730 | 25 / 3730 (R6 gap) | **YES** — close R6 emit |
| `inputs[]` order | include-scan walk order | source-first-then-scan | **YES** — close ordering |
| `deps[]` order | LD/AR preserve link order | sorted everywhere | **YES** — skip sort on LD/AR |
| UID values | ymake-derived | our-fingerprint-derived | normalizer re-UIDs REF |

---

## 3. Normalization pass — design

The normalizer is a one-time deterministic transform from
`/home/pg/monorepo/yatool_orig/sg.json` to
`/home/pg/monorepo/yatool/.out/normalized-ref-for-tools-archiver.json`.
Re-run on demand; never run during `gen`; output checked into git as
the L4 fixture (or regenerated reproducibly via the subcommand below).

### 3.1 Implementation form: `./yatool normalize` subcommand

**Decision: in-repo Go subcommand, NOT a Python script.**

Rationale:
- Reproducibility: pinned to the same Go version as `gen`, no
  Python-version drift.
- Code reuse: the normalizer needs `canonicalNodeBytes`, `computeUID`,
  the `Node` struct shape, and the `gjson_write.go` byte-exact
  serializer. A Python script would duplicate the JSON canonicalisation
  layer (the most error-prone part of L4) — guaranteed drift.
- CI: a Go subcommand is naturally testable via `go test ./...`. A
  Python script lives outside `go test` and creates a second toolchain
  dependency.
- File-size: ~250 LOC Go (single new file `normalize.go`) is no larger
  than the equivalent Python and is fully type-checked.

Subcommand surface:

```
./yatool normalize \
    --target tools/archiver \
    --in    /home/pg/monorepo/yatool_orig/sg.json \
    --out   ./.out/normalized-ref-for-tools-archiver.json
```

Flags:
- `--target` — required; archiver-LD's `outputs[0]` is computed from this
  (`$(BUILD_ROOT)/tools/archiver/archiver`) and used to find the
  root UID inside the input `result[]` (matched by output path on the
  target platform — `default-linux-aarch64`).
- `--in` — required; path to upstream sg.json.
- `--out` — required; path to write normalized output. `-` for stdout.

Throw on any structural anomaly (missing keys, multiple roots, dangling
deps); the normalizer must be deterministic and fail-fast.

### 3.2 Step 1 — subgraph extraction

REF contains the full ymake graph (well beyond 3730 nodes for our
target — verify by inspection; if it's already 3730 the step is a
no-op, but the code must still handle the general case).

```bash
python3 -c "import json; d=json.load(open('/home/pg/monorepo/yatool_orig/sg.json')); print('REF graph nodes:', len(d['graph']))"
# 3730 — empirically the file is already pre-trimmed to the archiver closure.
```

Even with the empirical 3730 (which already equals our target), the
extraction code is non-optional: it is the deterministic projection
that makes the normalizer's output a function of `--target` rather
than of REF's accidental contents.

Algorithm:
1. Locate the root: the unique node whose `outputs[0]` matches
   `$(BUILD_ROOT)/<target>/<basename of target>` on
   `default-linux-aarch64`. For `--target tools/archiver` that is
   `$(BUILD_ROOT)/tools/archiver/archiver`. **Throw if zero or
   multiple matches.**
2. BFS from the root, following `deps[]` only (not `foreign_deps[]`,
   which already point into the closure for our archiver target —
   verify in code by counting reachable-via-deps vs total).
3. Collect all reachable UIDs into a `set`.
4. Filter the input `graph[]` to nodes with UIDs in the set,
   preserving the input's relative order (we will re-order in Step 7).

### 3.3 Step 2 — strip `conf`

Top-level key removal. Trivial: `delete(graph, "conf")` equivalent.
Documented as allowed in `GOALS.md` §L4 / "Allowed normalizations".

### 3.4 Step 3 — re-UID via our fingerprint

This is the hardest step. The algorithm mirrors `emitter.go::Finalize`:

1. Build the dependency graph: edges from each node to its deps + its
   foreign_deps values (parsed as UIDs of other nodes in the
   extracted set). Throw if a dep points outside the set.
2. Forward-topo-sort (children-first), so each parent's UID can be
   computed from already-resolved children.
3. For each node in topo order:
   - Drop `uid`, `self_uid`, `stats_uid`, `sandboxing` from the node
     into a temporary "canonical view" matching our `Node` struct's
     JSON shape.
   - Rewrite `deps[]` entries: every old-UID is replaced with the
     newly-computed UID of the corresponding child. **Preserve order**
     (LD / AR depend on it). For nodes that L4 would otherwise
     sort, the comparator does not care about `deps[]` order on
     the cascading-UID pass — we sort/match the same way our
     emitter does. The order-preserve rule is for the *output* node;
     the *input* to the UID hash uses our canonical bytes
     (which sort `deps[]` per `emitter.go:328-342` for the hash
     denominator).
   - **Critical subtlety**: our `canonicalNodeBytes` already runs `sort.Strings(deps)` BEFORE hashing
     (`emitter.go:328-342`). The normalizer must do the same — i.e.,
     compute the UID over the sorted-deps canonical form, but emit
     the deps in REF's preserve-order shape post-hash. This matches
     OUR emitter's behaviour (see `emitter.go:331-342` where Deps is
     sorted before `canonicalNodeBytes` is called — note: this means
     OUR currently sorts deps in the emitted node too; PR-L4-C must
     change that for LD/AR).
4. Rewrite `foreign_deps[]` similarly (re-UID values, preserve order).
5. Rewrite top-level `result[]`: old-UIDs → new-UIDs in order.

After this step, every UID/SelfUID/dep reference is in our
fingerprint space.

### 3.5 Step 4 — normalize `stats_uid`

**Decision: option (a) — drop entirely from REF; emit empty in OUR (or drop from OUR too).**

Rationale:
- REF's `stats_uid` is a 32-char hex (MD5 shape), derivation unknown
  upstream and not load-bearing for graph correctness (per
  `GOALS.md` allowed-normalization clause "Where REF UIDs have a
  different length than OUR UIDs, normalize by truncation OR
  re-deriving. Document the rule.").
- Implementing ymake's stats_uid would require chasing a separate
  derivation path through ymake's stats computation, decoupled from
  graph semantics. Out of scope for L4.

Implementation:
- Normalizer drops the `stats_uid` key from each node entirely.
- Emitter (`gjson_write.go:182-186`) drops the `stats_uid` line
  unconditionally (PR-L4-C); update Node struct's tag to
  `omitempty` and zero-init `StatsUID` everywhere (it's already empty
  in practice — `emitter.go:401`).

Either both sides emit `stats_uid: ""` (with the key present) OR both
omit the key. Picking **omit** to halve the size penalty of the field;
key-present-empty-string is equivalent for L4 but wastes bytes.

### 3.6 Step 5 — normalize `sandboxing`

**Decision: option (a) — emit it in OUR.**

Rationale:
- `sandboxing` is a uniform `true` boolean in REF (3730 / 3730 nodes —
  verified §1.4). Adding a constant `true` line per node to OUR is
  ~30 LOC in `gjson_write.go::appendNode` between `requirements` and
  `self_uid`, plus a `bool Sandboxing` field with tag `json:"sandboxing"`
  in `node.go`.
- The field's content is generator-derivable trivially (`true`
  unconditionally for this target subgraph); no upstream-toolchain
  data is needed.
- Stripping it from REF would invoke the "NOT allowed" rule of
  `GOALS.md` ("Stripping any semantic field from the reference (e.g.,
  `sandboxing`, `target_properties`, `requirements`)"). **Stripping is forbidden.**

Implementation:
- `node.go`: add `Sandboxing bool \`json:"sandboxing"\`` between
  `Requirements` and `SelfUID` (alphabetical preservation).
- Default to `true` in every rule emitter (cc.go, ar.go, ld.go, etc.) —
  or set unconditionally in a single post-`Emit` pass in `gen.go`.
- `gjson_write.go::appendNode`: add a `"sandboxing": true,\n<pad>` block
  between requirements and self_uid emission. Do NOT use `omitempty` —
  field is always emitted.
- Risk: if a future target sets `sandboxing: false` somewhere, this
  must accommodate. Leave the bool field truthful; for the M2
  archiver target, it's `true` everywhere.

### 3.7 Step 6 — JSON canonicalisation (byte rules)

Both normalizer and emitter must produce the same bytes for the same
canonical model. Rules:

- **Indent: 4 spaces** (matches REF — §1.3). Change
  `gjson_write.go:51-99` from `"  "` (2 sp) to `"    "` (4 sp). All
  callers of `appendNode` already pass an outer pad string — only the
  top-level driver needs updating. The hand-rolled writer's correctness
  test (`gjson_write_test.go`) must be re-pinned at indent=4.
- **HTML escape: off**. Already the case
  (`gjson_write.go:31-33`, `uid.go:59-60`); no change.
- **Key order: alphabetical**. Already the case in `node.go:46-67`'s
  field declaration order, with one insertion: `sandboxing` between
  `requirements` and `self_uid` (Step 5).
- **Trailing newline: none**. Change `gjson_write.go` so the final
  `}` is not followed by `\n`. Concretely: after the existing
  closing-brace write, remove the `'\n'` append (search for the
  outermost-object close). Match REF's terminal byte `}` exactly.
- **Map keys: alphabetical** within `env`, `kv`, `requirements`,
  `target_properties`, `foreign_deps`. Already the case
  (`gjson_write.go:27`).
- **Slice ordering rules**:
  - `deps[]`: REF order. For LD/AR nodes (where REF preserves
    link/archive order), the emitter must NOT sort. For CC/AS/JS/R6/CP,
    REF's order is coincidentally lex (or single-entry), so emit
    in REF's order — equivalent to emit-time order.
  - `inputs[]`: REF order. **OUR scanner currently produces a different
    order on 3598 / 3730 nodes** — this is the hardest gap. See §4.4.
  - `outputs[]`, `tags[]`: REF order. Already byte-equal on every
    paired node (L1/L2 / L3 = 100 %).
  - `cmds[]` and per-cmd `cmd_args[]`: byte-equal already (L3 = 100 %).

### 3.8 Step 7 — `graph[]` array ordering

REF: DFS preorder rooted at `result[0]`, visiting children in declared
`deps[]` order (§1.9). The orchestrator's hypothesis "normalize REF to
match OUR's order" is **wrong** per `GOALS.md` NOT-allowed clause
("Re-ordering the graph array of the reference. The order itself is
semantic; if our generator emits a different order, our generator is
wrong, not the reference.").

**Decision: OUR emitter changes to emit DFS preorder rooted at
`Result[0]`, visiting `Deps[]` in their as-emitted order.**

Implementation (lands in PR-L4-C):
- Replace `emitter.go::Finalize`'s Kahn-leaves-first traversal
  (`emitter.go:283-308`) — the Merkle-hash topo-sort step itself must
  remain leaves-first (required for child-UIDs-before-parent), but the
  *final output order* `out.Graph` is built in a second pass:
  - Topo-sort and assign UIDs as today (`emitter.go:325-409`).
  - In the output build (currently `emitter.go:416-433`), replace the
    "iterate `order` in topo order" loop with a DFS preorder rooted at
    each `Result()` UID, in result-call order.
- `Deps[]` on each node should be in REF order to preserve the DFS
  outcome. For LD/AR, the emit-time order is the canonical link
  order (already implemented — `emitter.go:328-342`'s sort must be
  bypassed for those nodes; see §4.5).
- Performance: DFS is O(N+E). E ≈ 50K edges across the archiver
  closure; cost is negligible compared to the topo-sort.

The normalizer also emits in this same DFS order (Step 1's BFS already
materialised the closure; replace with DFS preorder for the final
emit). Both sides produce byte-identical orderings.

---

## 4. Emitter changes — design

PR-L4-C ships the union of these changes. Each is a separate sub-task
trackable in `tasks.md` with its own acceptance signal (L4 % climbing).

### 4.1 Node struct: add `Sandboxing` field

`node.go:46-67`: insert between `Requirements` and `SelfUID`:

```go
Sandboxing bool `json:"sandboxing"`
```

No `omitempty` — emit always. Wire in every rule emitter, or set
in `gen.go::genModule` post-`Emit`. Easier: set in `gen.go` once.

Default value `true` for the archiver target. Future targets that
need `false` will plumb through `ModuleInstance.Flags.Sandboxing` or
similar.

### 4.2 Indent: 2 → 4 spaces

`gjson_write.go:51`: change initial pad. Update `appendNode`, `appendCmdSlice`, `appendStringSliceMap` outermost pad to 4 spaces. The "innerPad = pad + 2sp" recursion logic stays — only the seed changes (effectively double the seed and the increment, keeping nesting visually consistent).

Update `gjson_write_test.go` fixtures to match. The byte-exactness test
(`gjson_write_test.go` parallel test vs json.Encoder) must use
`SetIndent("", "    ")` after the change.

### 4.3 Trailing newline: drop

`gjson_write.go`: locate the final closing `}` (after the `result` array
closer in `writeGraphIndented`'s body); remove the `'\n'` that follows
the final `}`. Update tests.

### 4.4 `inputs[]` ordering: REF-equivalent

This is the heavyweight change. REF's order is dictated by ymake's
include-scan walk; OUR's order is dictated by our `scanner.go`'s
walk. Bringing them together has three sub-options; pick (b).

**(a) Sort both sides on emit.** Trivial — `sort.Strings(inputs)` in
emitter. But Section 2's table marks this DISallowed: re-ordering the
inputs array of the reference is not explicitly listed in GOALS.md's
NOT-allowed clause, but "Re-ordering the graph array of the reference"
is. Asking the reference for `inputs[]` re-order is not the same as
the graph[] re-order — but it does lose information (ymake's scan
order encodes precedence). **Reject** for L4 fidelity; accept as a
fallback if (b) proves intractable.

**(b) Make OUR scanner emit REF-equivalent order, and bring REF's order
through the normalizer unchanged.** This is the canonical L4 close.
Required investigations (must land in PR-L4-C, sub-task L4-C/inputs):
- Profile OUR `scanner.go` walk order vs REF inputs[] for 10
  representative CC nodes (musl src/math/*, base64 avx2/*, util/system/*).
- Determine REF's order rule. Hypothesis from §1.7 spot-checks:
  "the primary source first, then includes in include-graph DFS order
  with sorted-children-per-node tie-break". Validate empirically.
- Patch `scanner.go` to match. The fix is likely in
  `IncludeScanner::collect`'s child-order: switch from depth-first
  with our current ordering to ymake's specific preorder.

**(c) Hybrid: Sort inputs in REF via normalizer (Step 5b) AND sort in OUR.**
This is the same as (a) but enforced via a normalizer pass. Same
objection: information loss. **Reject.**

**Pick (b).** Expected effort: 2-3 review rounds; the include-scan
order is THE remaining unknown. Risk: medium-high. Fallback: (a)
if (b) blocks for >2 review rounds.

### 4.5 `deps[]` ordering: skip sort for LD and AR

`emitter.go:328-342`: today the resolver always sorts. Change to:
- For LD nodes (3 / 3730): preserve emit order. The peer-collection
  walk in `gen.go` already emits in link order; just don't sort.
- For AR nodes (27 / 48 with non-sorted deps): preserve emit order.
- For all other nodes: keep sorting.

Detection: branch on `node.KV["p"]`. LD == "LD", AR == "AR".
Alternatively, expose a per-emit flag `Node.PreserveDepOrder bool` (set
by `ld.go`/`ar.go`); cleaner.

Currently `Deps` is built from `DepRefs` and de-duped via a `map[string]struct{}` —
removing the sort while keeping dedup needs an ordered set (slice + seen-map). Trivial.

### 4.6 `self_uid` derivation

REF: 101 LD/AR nodes have `self_uid != uid`. OUR: every node has
`self_uid == uid` (placeholder per `uid.go:395-400`).

Two options:

**(a) Hand-translate ymake's SelfUID derivation rule.** Out of scope —
require chasing ymake's selfUID-without-children semantics through
upstream code. Likely "hash of canonical node bytes with deps cleared,
not with deps resolved". Empirically verifiable but separate effort.

**(b) Normalize REF: replace each REF `self_uid` with `uid`.** Then OUR's
placeholder behaviour matches. Documented as allowed by GOALS.md (the
re-canonicalize-syntactic-equivalent clause covers UID-shape
normalization). The 101 nodes lose information, but it's recoverable
from upstream if ever needed.

**Pick (b)** for PR-L4-A scope. Document trade-off:
the normalized fixture has `self_uid == uid` for every node; OUR
emitter matches with no code change.

If a later milestone needs distinct `self_uid` (M5/M6), revisit (a).

### 4.7 R6 `foreign_deps` emission

The R6 emit at `$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp`
(`default-linux-aarch64`) is missing `foreign_deps` in OUR while REF
carries it (§1.4). Inspect `r6.go` and locate the emit path; trace
why the host build emits `foreign_deps` (presumably correct) but the
target's aarch64 build does not. Likely a one-line fix in r6.go.

### 4.8 `graph[]` order: DFS preorder

Already covered §3.8. Implementation note: keep the topo-sort UID pass;
add a second pass to build `out.Graph` in DFS-preorder-from-result
order. Approximately 30 LOC in `emitter.go`.

### 4.9 Top-level `inputs`: no change

Already `{}` in both REF and OUR. Verified §1.11.

### 4.10 `result[]`: no change

Already 1-entry in both. The normalizer re-UIDs it via Step 3 so the
single entry matches OUR's archiver-LD UID. No emitter code change.

---

## 5. L4 comparator — design

New file `compare_bytes.go`, ~80 LOC. Folds into the existing
`Compare(want, got, maxLevel)` dispatch.

### 5.1 Subcommand surface

```
./yatool compare --level=4 our.json normalized-ref.json
```

`main.go::cmdCompare` already accepts `--level`. Bump default? **No** —
default stays 3 to avoid surprising existing callers. L4 is opt-in
until M3+ rolls it into CI gating.

### 5.2 Algorithm

`compareBytes(wantPath, gotPath string) (l4 float64, note string)`:

1. Read both files via `os.ReadFile` (~73 MB each; well within RAM).
2. `if bytes.Equal(want, got) { return 1.0, "byte-exact" }`.
3. On mismatch: find the first differing byte offset; emit a hexdump
   window of ±32 bytes around it; return `(0.0, "differ at offset N: ref=<hex> got=<hex>")`.
4. Optionally compute and emit `sha256(want)`, `sha256(got)` for the
   diff-log line.

### 5.3 What the comparator does NOT do

- Does not normalize on the fly. Both inputs must be already-canonical
  (i.e., one is `normalized-ref-for-tools-archiver.json`, the other is
  `./yatool make -j 0 -G tools/archiver > -`).
- Does not parse the JSON; pure byte compare.
- Does not load `*Graph`. The L0..L3 comparators load both as Graphs; L4
  is path-by-path bytes only.

### 5.4 Integration

In `compare.go::Compare`:
- Bump `highestImplementedLevel` from 3 to 4 (`compare.go:57`).
- In the `Compare` body, add an `if maxLevel >= 4 { ... }` branch that
  calls `compareBytes(wantPath, gotPath)`. The current `Compare`
  signature takes `*Graph` already-loaded — extend to accept the
  source paths too (a third tuple param or two extra params), OR
  factor the byte-compare into `cmdCompare` directly. The latter is
  simpler.
- Update `CompareReport` struct: add `L4 float64`, `L4Note string`
  fields.
- `main.go::cmdCompare` reports L4 with `fmt.Printf("L4: %s\n", l4note)`.

---

## 6. Test plan

### 6.1 PR-L4-A normalizer determinism

```go
// normalize_test.go
func TestNormalize_Deterministic(t *testing.T) {
    out1 := runNormalize(...)
    out2 := runNormalize(...)
    if sha256(out1) != sha256(out2) {
        t.Fatalf("normalizer is non-deterministic")
    }
}
```

### 6.2 PR-L4-B comparator self-match

```go
// compare_bytes_test.go
func TestCompareBytes_SelfEqual(t *testing.T) {
    // Two byte-identical files report L4 == 1.0
}

func TestCompareBytes_OneByteDiff(t *testing.T) {
    // Files differing in one byte report L4 == 0.0 + correct offset
}
```

### 6.3 PR-L4-C regression-pin (M1 + M2)

- **M2 archiver** L4: load `normalized-ref-for-tools-archiver.json` (a
  fixture checked in OR regenerated reproducibly), run
  `./yatool make -j 0 -G tools/archiver > -`, `bytes.Equal` them.
- **M1 build/cow/on** L4: same shape, 2-node subgraph. The fixture is
  much smaller (~6 KB); check in.

```go
// gen_l4_test.go
func TestGen_L4_BuildCowOn(t *testing.T) {
    out := capture(cmdMake([]string{"--target", "build/cow/on", "--out", "-"}))
    fixture := mustRead("testdata/normalized-build-cow-on.json")
    if !bytes.Equal(out, fixture) { t.Fail() }
}

func TestGen_L4_ToolsArchiver(t *testing.T) {
    // Larger; skip with -short for quick CI runs.
    if testing.Short() { t.Skip() }
    out := capture(cmdMake([]string{"--target", "tools/archiver", "--out", "-"}))
    fixture := mustRead("testdata/normalized-ref-for-tools-archiver.json")
    if !bytes.Equal(out, fixture) { t.Fail() }
}
```

### 6.4 Determinism (DoD item 8)

```go
func TestGen_Deterministic(t *testing.T) {
    a := capture(cmdMake([]string{"--target", "tools/archiver", "--out", "-"}))
    b := capture(cmdMake([]string{"--target", "tools/archiver", "--out", "-"}))
    if sha256(a) != sha256(b) { t.Fatal("non-deterministic") }
}
```

---

## 7. PR breakdown

Four PRs. PR-L4-A and PR-L4-C are largely independent (different files); PR-L4-B depends on PR-L4-A's output existing; PR-L4-D depends on both PR-L4-A and PR-L4-C reaching their acceptance criteria.

### PR-L4-A — Normalizer subcommand

**Scope:**
- New file `normalize.go` (~250 LOC).
- New file `normalize_test.go` (~100 LOC) — determinism + small fixture.
- Update `main.go::dispatch` to route `normalize` → `cmdNormalize`.
- Update `main.go::printUsage` to mention `normalize`.
- Generate the artifact `./.out/normalized-ref-for-tools-archiver.json`
  as part of the test run; check it in OR regenerate-on-demand.

**Files touched:** `normalize.go` (new), `normalize_test.go` (new),
`main.go` (~10 LOC), possibly small additions to `gjson.go`
(reader for the closure subset; reuse `LoadReference`).

**Acceptance:**
- `./yatool normalize --target tools/archiver --in /home/pg/monorepo/yatool_orig/sg.json --out -` writes valid JSON to stdout.
- Two consecutive runs produce sha256-identical output.
- The output contains 3730 nodes (matches the closure size).
- Top-level `conf` is absent; per-node `stats_uid` is absent;
  `sandboxing` is preserved; `self_uid == uid` everywhere; all UIDs
  use the 22-char base64url shape.

**Parallelism:** Can run concurrently with PR-L4-C (no shared files
other than `main.go::dispatch`, which is a trivial merge).

**Risks:**
- Step 3 (re-UID cascade) requires exactly mirroring
  `canonicalNodeBytes`. Drift between normalizer-internal canonical
  bytes and emitter canonical bytes would silently break L4.
  **Mitigation:** factor `canonicalNodeBytes` into a shared helper
  consumed by both `uid.go` and `normalize.go`; do not duplicate.
- The DFS preorder rule (Step 7) must exactly match the emitter's
  output rule (§3.8). Same mitigation: shared helper.

### PR-L4-B — L4 comparator + CLI

**Scope:**
- New file `compare_bytes.go` (~80 LOC).
- New file `compare_bytes_test.go` (~60 LOC).
- Update `compare.go::Compare` and `CompareReport` (add L4 fields;
  bump `highestImplementedLevel` to 4).
- Update `main.go::cmdCompare` to thread paths through to L4.
- Update `printCompareUsage` to mention L4.

**Files touched:** `compare_bytes.go` (new), `compare.go` (~20 LOC),
`main.go` (~5 LOC), `compare_bytes_test.go` (new).

**Acceptance:**
- `./yatool compare --level=4 a.json b.json` works for both byte-equal
  and byte-differ cases.
- Reports `L4: byte-exact` or `L4: differ at offset N (first 64 bytes ref=… got=…)`.
- Returns exit 0 either way (observational; matches L0..L3 behaviour).

**Parallelism:** Depends on PR-L4-A only for the test fixture. The
comparator itself can be implemented in parallel; tests stub the
fixture with a 2-line inline file.

**Risks:** Low. Pure byte compare with a hexdump on diff.

### PR-L4-C — Emitter changes (sub-task list)

**Scope (each sub-task is independent enough to parallelise across worktrees):**

- **L4-C/01 — `Sandboxing bool` field** (`node.go`, `gjson_write.go`,
  every emitter rule). +1 line per node; default `true`.
- **L4-C/02 — Indent 2 → 4** (`gjson_write.go`, `gjson_write_test.go`).
- **L4-C/03 — Drop trailing newline** (`gjson_write.go`,
  `gjson_write_test.go`).
- **L4-C/04 — Drop `stats_uid` emission** (`node.go` tag adjustment,
  `gjson_write.go::appendNode` line removal). Mirrors the normalizer's
  drop.
- **L4-C/05 — `deps[]` skip-sort for LD/AR** (`emitter.go:328-342`).
  Branch on `node.KV["p"]`.
- **L4-C/06 — `graph[]` DFS preorder from `Result[0]`** (`emitter.go`).
  New second pass after topo-sort/UID assignment.
- **L4-C/07 — R6 `foreign_deps` emission** (`r6.go`). Single missing
  emit path for `parser.rl6.cpp` aarch64.
- **L4-C/08 — `inputs[]` REF-equivalent order** (`scanner.go`, possibly
  `gen.go`). The hardest sub-task. **Branchable into its own commit**
  if it lingers; fallback to "sort both sides" (option (a) §4.4) if
  >2 review rounds stall it.

**Files touched:** `node.go`, `emitter.go`, `gjson_write.go`,
`gjson_write_test.go`, `r6.go`, `scanner.go`, possibly `gen.go`.

**Acceptance:**
- All sub-tasks land.
- `./yatool compare --level=4 ./.out/sg.json ./.out/normalized-ref-for-tools-archiver.json` → `L4: byte-exact`.
- L0=L1=L2=L3=100 % preserved on `tools/archiver` and `build/cow/on`.
- `time ./yatool make -j 0 -G tools/archiver` ≤ 5 s (DoD item 3).
- `go test ./...` passes; `go build`, `go vet`, `gofmt -l *.go` clean.

**Parallelism (within PR-L4-C):**
- L4-C/01..04 are independent of each other (different functions in
  different files). Parallel worktrees safe.
- L4-C/05 and L4-C/06 both touch `emitter.go`. **Serial.**
- L4-C/07 is standalone (r6.go only).
- L4-C/08 is standalone (scanner.go) but expensive; start early.

**Risks:**
- L4-C/08 (inputs ordering) is the headline risk. If REF's order rule
  is more complex than "DFS preorder with sorted children" or "DFS
  preorder with declaration-order children", multiple review rounds
  may be needed. **Mitigation:** time-box at 2 rounds, fall back to
  option (a) "sort both" with a documented L4 deviation noted in
  `tasks.md` Cross-cutting.
- L4-C/06 may interact with the determinism test (DoD item 8). DFS
  with declaration-order children is deterministic given deterministic
  `deps[]` — verify.

### PR-L4-D — L4 regression-pin tests

**Scope:**
- `testdata/normalized-build-cow-on.json` (regenerated fixture, ~6 KB).
- `testdata/normalized-ref-for-tools-archiver.json` (regenerated
  fixture, ~73 MB — too large to check in; instead, regenerate at
  test-time via the normalizer subcommand; cache in `./.out/`).
- `gen_l4_test.go` (~60 LOC): the M1 and M2 L4 regression pins.
- `gen_determinism_test.go` (~30 LOC): two-run sha256 equality.

**Files touched:** `gen_l4_test.go` (new), `gen_determinism_test.go`
(new), `testdata/` directory (new).

**Acceptance:**
- `go test -run L4 ./...` passes; `go test -run L4_Archiver -count=2 ./...` also passes (caches the regenerated fixture).
- Tests reliably pass after `git clean`-ing `./.out/` and `./testdata/`.

**Parallelism:** Depends on PR-L4-A + PR-L4-C. Serial.

**Risks:**
- Fixture-size discipline: checking in the 73 MB normalized REF
  would balloon the repo. The test harness regenerates it on first
  run, caches in `./.out/`. Document this in the test file header.

---

## 8. Risks / open questions

### 8.1 `inputs[]` ordering — known unknown

The principal L4 closure risk. REF's order is dictated by ymake's
internal include-scan walk, which we have not yet characterised
byte-exactly. Three outcomes:

- **(best)** REF's rule is "primary source, then DFS preorder over
  the include graph with declaration-order children". OUR scanner
  is close; <50 LOC fix.
- **(median)** REF's rule has some quirk (specific edge case in
  re-entry, transitive include precedence, `#include_next` handling)
  that requires multiple probes.
- **(worst)** REF's order is not deterministically reproducible from
  pure scan output — it carries some upstream ordering side-channel
  (e.g., insertion order in a hash table). **Fallback: sort
  both sides at emit time**, accepting the documented L4 deviation.

Pre-decided fallback: if PR-L4-C/08 exceeds 3 review rounds with no
convergence, switch to the sort-both fallback and document in
`tasks.md` as a future M5/M6 follow-up.

### 8.2 `sandboxing` always `true` — future-proofing

For the archiver target, `sandboxing == true` for every node. If a
future target introduces `false` or per-node variation, our `gen.go`
hard-coded `true` would silently miss. **Mitigation:**
plumb `Sandboxing` via `ModuleInstance` (D30 addressing tuple) — set
to `true` in M2 PROGRAM/LIBRARY defaults; future code can override.

### 8.3 `result[]` after re-UID

When the normalizer cascades UIDs, REF's `Ze_eMOLqyMsa6WlbbMhgvQ`
becomes our archiver-LD's UID (`3tPpxZznv8FPTzwjXlf_iQ` or whatever
the current run produces). The normalizer's `result[0]` is the
new-UID of the original `Ze_eMOLqyMsa6WlbbMhgvQ`'s node post-cascade.

**Verify after PR-L4-A lands:** the normalizer's `result[0]` matches
OUR `make -j 0 -G tools/archiver`'s `result[0]` exactly. If not, the
DFS-preorder rule (Step 7) is the likely culprit — debug first.

### 8.4 Determinism after `graph[]` reorder (DoD item 8)

Two consecutive `gen` runs must produce sha256-identical output. The
DFS preorder rule (§4.8) is deterministic given deterministic
`deps[]` — but if any rule emitter computes `DepRefs` in
non-deterministic order (e.g., iterating a `map` per D14 violation),
DFS will be non-deterministic. **Mitigation:** PR-L4-C/06 lands with
a determinism test (§6.4) that proves the property at the time of
landing.

### 8.5 Performance budget after `graph[]` reorder

Current gen wall time: ~0.92 s. Budget: ≤ 5 s (hard gate). The DFS
preorder pass is O(N+E) ≈ O(50K) ops — negligible (<10 ms expected).
Re-UID + canonical-bytes recompute is already in the hot path. **No
risk** unless L4-C/08 (inputs ordering) introduces an O(N²) scanner
re-walk. Verify with `time` at each PR-L4-C/* landing.

### 8.6 Subagent execution order

- **PR-L4-A** and **PR-L4-C** are largely independent (`normalize.go`
  vs `emitter.go`+`gjson_write.go`+`scanner.go`+`r6.go`). The shared
  helper for `canonicalNodeBytes` (referenced in §3.4 mitigation)
  should land first, in either PR; if it lands in PR-L4-A, PR-L4-C
  rebases over it.
- **PR-L4-B** has a soft dependency on PR-L4-A (test fixture) but the
  comparator itself can implement and test against inline strings.
  Land in parallel.
- **PR-L4-D** is strictly sequenced after both PR-L4-A and PR-L4-C
  (depends on the actual L4 close).

Recommended landing order:
1. **PR-L4-A** (normalizer) — produces the L4 target artifact.
2. **PR-L4-B** (comparator) — gives us a yes/no signal for PR-L4-C.
3. **PR-L4-C** (emitter changes) — drives L4 to 100 %. Use PR-L4-B's
   comparator throughout the inner-loop review cycles.
4. **PR-L4-D** (regression tests) — pins the achieved state.

### 8.7 `self_uid` deviation documentation

Per §4.6 decision (b), the normalizer rewrites REF's `self_uid := uid`
for 101 LD/AR nodes. This is documented in `GOALS.md`'s allowed
normalizations as "Where REF UIDs have a different length than OUR
UIDs... normalize by truncation OR by re-deriving. Document the rule."
Our derivation rule: **`self_uid := uid` everywhere**, both in OUR
emitter and the normalized REF. If a later milestone requires
distinct `self_uid`, file an M5/M6 PR (`PR-M5-selfuid`) to implement
ymake's actual derivation.

### 8.8 Top-level `inputs` semantics — pre-resolved

§1.11 confirmed both REF and OUR have `inputs == {}`. No risk. The
GOALS.md "inputs references" cascade language refers to
**per-node** `inputs[]`, not top-level `inputs`.

---

## 9. Summary of decisions

| Decision | Choice |
|---|---|
| Normalizer form | In-repo `./yatool normalize` Go subcommand |
| `sandboxing` handling | Emit `true` in OUR (mirror REF) |
| `stats_uid` handling | Drop from REF; drop from OUR |
| `self_uid` handling | Normalize REF: `self_uid := uid`; OUR's placeholder matches |
| `graph[]` order | DFS preorder from `result[0]` with declaration-order children; both sides emit it |
| `deps[]` order | Preserve emit order for LD/AR; sort for others (matches REF) |
| `inputs[]` order | Match REF; pre-decided fallback to sort-both after 3 stalled review rounds |
| Indent | 4 spaces |
| Trailing newline | None |
| HTML escape | Off (unchanged) |
| PR count | 4 (A: normalizer, B: comparator, C: emitter, D: tests) |

---

## 10. Pointers

- `/home/pg/monorepo/yatool/GOALS.md` — L4 acceptance criterion + allowed/not-allowed normalizations.
- `/home/pg/monorepo/yatool/emitter.go:158-460` — `Finalize` and the topo-sort to be replaced with DFS preorder.
- `/home/pg/monorepo/yatool/uid.go:31-72` — `canonicalNodeBytes` + `computeUID`; the shared helper between emitter and normalizer.
- `/home/pg/monorepo/yatool/gjson_write.go:47-210` — the hand-rolled writer to be updated for indent=4, drop trailing newline, add sandboxing.
- `/home/pg/monorepo/yatool/node.go:46-67` — Node struct to add `Sandboxing bool`.
- `/home/pg/monorepo/yatool/main.go:39-58` — subcommand dispatch to add `normalize`.
- `/home/pg/monorepo/yatool/compare.go:55-119` — `Compare` to extend with L4 dispatch.
- `/home/pg/monorepo/yatool_orig/sg.json` — REF.
- `/home/pg/monorepo/yatool/.out/sg.json` — OUR latest output (post-PR-43, L0=L1=L2=L3=100 %).
