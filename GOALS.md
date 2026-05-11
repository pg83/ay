# yatool — Goals

This document defines the target state of the yatool repository: **what** we want
to achieve, without prescribing **how**. Implementation plans live in
`docs/drafts/`; per-PR scope lives in `tasks.md`; the running defect ledger in
`defects.md`. This file is the north star.

The HOW is intentionally absent. Architectural decisions (memo-key shape,
emitter pipeline, scanner caching, etc.) belong in `tasks.md` cross-cutting
notes. This file describes only the destination.

---

## Mission

Reimplement Yandex's `ymake` build-graph generator in Go such that, for the
chosen target subgraph, the generated graph is **byte-exact identical** to the
upstream reference graph, with substantially better runtime performance and
without runtime parsing of upstream build rules.

---

## Acceptance target — primary

The generator must produce a graph that matches the canonical reference
**byte-exact at all comparator levels**, while staying within the runtime
budget.

### Metrics

| Metric | Definition | Current | Target |
|---|---|---|---|
| L0 | Per-node Merkle topology fingerprint multiset overlap with reference | 100.00% (3730 / 3730) | **100.00%** |
| L1 | Per-pair `(outputs[0], platform)` pairing yields matched `kv.p` / `target_properties` / `outputs` | 100.00% (3730 / 3730) | **100.00%** |
| L2 | Paired-node `inputs` / `tags` / `requirements` multiset equality | 100.00% (3730 / 3730) | **100.00%** |
| L3 | Paired-node `cmd_args` / `env` / `cwd` byte-exact equality | 100.00% (3730 / 3730) | **100.00%** |
| L4 | `sha256(our.json) == sha256(normalized-ref.json)` — byte-exact equality of the full on-disk graph file | not yet measured | **byte-exact equal** |
| `gen` wall time | `time ./yatool gen --target tools/archiver` warm-cache (3-run avg) | ~0.92 s | **≤ 5 s** (hard gate; deviations are emergency tickets) |

#### L4 — byte-exact on-disk equality

L4 promotes L0..L3 from "semantically equivalent" to "literally the same
bytes on disk". The intent is to surface remaining graph-construction
defects that the semantic comparators tolerate (UID collisions that
fingerprint-match but byte-differ; node-ordering in the `graph` array;
field-emission shape per node; struct-field ordering; `null` vs omitted
keys; trailing newlines; etc.).

**Allowed normalizations of the reference** (one-time, codified):

- Strip the top-level `conf` section before comparison. The `conf`
  block carries build-config metadata that is not part of the graph
  semantics; reproducing it byte-exact is out of scope.
- Re-canonicalize the reference for trivial-syntactic equivalence
  (key order within objects, whitespace, empty-section vs `null`
  vs omitted, etc.). Document the exact normalization rules; the
  normalized reference is checked in or reproducibly regenerated.
- Re-compute UIDs using our generator's fingerprint algorithm,
  cascading through `deps`, `result`, and `inputs` references.
  Rationale: the reference uses ymake's UID algorithm; reproducing it
  byte-exact is a separate effort and not load-bearing for graph
  correctness once L0..L3 have established semantic equivalence.
- Drop any per-node fields that carry upstream-only state and do not
  participate in graph semantics: `stats_uid`, `cache` (where present;
  single-node `BI` shape in M3's sg2.json — value `false`, no
  generator analogue), and any future fields the upstream ymake
  emits that have no semantic meaning for our regenerated graph.
- Drop the per-node `foreign_deps` key on both sides before re-UID.
  Rationale: `foreign_deps` is a non-semantic toolchain-routing hint.
  REF's `foreign_deps` contains dangling cross-subgraph UIDs (the upstream
  host tool LD lives outside the `tools/archiver` closure in sg.json);
  OUR `foreign_deps` contains real local UIDs for the same tools. The
  structural divergence is legitimate and not a graph-correctness defect.
  See `normalize.py::_strip_and_canonicalize` for the implementation.

**NOT allowed**:

- Stripping any semantic field from the reference (e.g., `sandboxing`,
  `target_properties`, `requirements`).
- Re-ordering the `graph` array of the reference. The order itself is
  semantic; if our generator emits a different order, our generator
  is wrong, not the reference.
- Round-trip normalization of OUR output. Our output must be produced
  byte-exact directly by the emitter. Only the reference is
  normalized.

### Reference graph

- Authoritative: `/home/pg/monorepo/yatool_orig/sg.json`.
- **Not** `g.json`. `sg.json` includes parsed `#include` paths in node
  `inputs`; that is the canonical shape.

### Initial target subgraphs

- **M2**: `tools/archiver` (full PEERDIR closure, both `default-linux-aarch64`
  target and `default-linux-x86_64` host platforms; reference graph = 3730
  nodes; reference at `/home/pg/monorepo/yatool_orig/sg.json`).
- **M3**: `devtools/ymake/bin` (the ymake binary itself — meta-bootstrap;
  8750 nodes; reference at `/home/pg/monorepo/yatool_orig/sg2.json`).
  Same `--musl --target-platform=default-linux-aarch64 --sandboxing` flag
  set; produced by `srun2.sh`. Same L0..L4 = 100% bar; same gen ≤ 5 s
  hard gate (no relaxation despite ×2.35 node growth).

### Regression-pin subgraph

- `build/cow/on` (a 2-node minimum vertical slice; CC + AR for `lib.c`).
- Must remain byte-exact at L0/L1/L2/L3 throughout every PR.

---

## Acceptance target — secondary

Beyond the headline metrics, the following invariants must hold:

| Invariant | Description |
|---|---|
| Determinism | Two consecutive `gen` runs against the same source tree produce byte-identical output (sha256 equal). |
| Cycle handling | PEERDIR cycles tolerated with a stderr diagnostic and a counter; no silent skips of unrelated cycles. |
| Cross-platform completeness | Both target and host instances are emitted for every module in the reference's host closure (no `--filter-platform` cheat). |
| `len(graph.Result)` | Equals reference (currently 1: target `tools/archiver` LD only). |

---

## Architectural goals

These are **what**-level constraints on the design, not how to build it.

| Goal | Statement |
|---|---|
| Reimplementation, not wrapping | yatool is a from-scratch Go binary; no runtime call into the upstream `ymake`. |
| Single binary, flat layout | One Go binary at the repo root; all `.go` files under `/`, no subdirectories. |
| Hand-translated build rules | `build/conf/*` and `build/ymake.core.conf` rules are hand-translated to Go, not parsed at runtime. |
| Hand-translated rule data is allowed at runtime — `build/sysincl/*.yml` only | The 11 K-line sysincl resolution tables are too large to hand-translate; runtime-parsed via a minimal hand-rolled YAML loader. This is the **only** documented exception. |
| Upstream Python scripts invoked as-is | `build/scripts/*.py` are referenced verbatim from the generated graph; not reimplemented. |
| Flag-driven configuration, not path-prefix dispatch | `musl`, libc choice, allocator choice, etc. are selected by CLI `-D` flags (`--define MUSL=yes`) that resolve to implicit PEERDIRs and flag bundles. **No production code may dispatch on path prefixes** like `HasPrefix("contrib/libs/musl/...")` for behavioral decisions. (Documented backward-compat shims may exist with a removal plan.) |
| Module addressing by tuple | A module is identified by `(Path, Language, Target, FlagSet)` — never by path alone. |
| Demand-driven host walk | Host PROGRAMs (ragel6, yasm) and their PEERDIR closures are emitted only when the target walk demands them; no unconditional host-mirror pass. |
| Throw-style error handling | Internal errors via `Throw`/`Throw2`/`ThrowFmt` per `throw.go`; `if err != nil { return err }` chains are forbidden inside generation logic. Errors caught at process / dispatch boundaries. |
| Style discipline | Source conforms to `STYLE.md` (blank-line discipline, minimal new comments, no path-prefix dispatch for upstream-config-driven concerns). |
| Reproducibility under upstream resync | When the upstream tree (`/home/pg/monorepo/yatool_orig/`) is updated, regeneration of the reference graph and re-run of yatool must continue to produce a byte-exact match without code changes for **data-driven** updates. Schema changes to upstream rules require code updates and are tracked in the ledger. |

---

## Out of scope (explicitly)

| Item | Why |
|---|---|
| Targets beyond `tools/archiver` | Closing the full archiver subgraph first proves the generator's correctness end-to-end; broader closure follows as a separate milestone. |
| Streaming / parallel emitter | Performance is currently within budget by 5–6× margin. M3 is reserved for streaming + parallelism if a future target demands it. |
| `--filter-platform` flag or any host/target cheat | Locked-out by D40. |
| Reimplementing build/scripts/*.py logic in Go | Out of scope; scripts are referenced as-is in cmd_args. |
| Alpha / beta / RC dependencies | Use stable, recent stdlib + zero third-party Go dependencies (the YAML loader is hand-rolled minimal). |

## Definition of done

The project is complete when **all of the following hold simultaneously** on
main, against the canonical reference graph at `/home/pg/monorepo/yatool_orig/sg.json`:

1. L0 = L1 = L2 = L3 = **100.00 %** for `tools/archiver`.
2. **L4 byte-exact**: `sha256(./yatool gen --target tools/archiver --out -)`
   equals `sha256(normalized-ref-for-tools-archiver.json)`, where the
   normalized reference is produced by the documented one-time
   normalization pass over `/home/pg/monorepo/yatool_orig/sg.json` (strip
   `conf`; re-canonicalize syntactic-equivalent JSON; re-UID via our
   fingerprint).
3. `time ./yatool gen --target tools/archiver` ≤ 5 s warm-cache, 3-run avg.
4. M1 (`build/cow/on`) byte-exact at all four levels — preserved.
5. `go test ./... -count=1` passes; `go build`, `go vet`, `gofmt -l *.go` clean.
6. `defects.md` has zero open entries (`resolved` or `resolved (deferred)` only).
7. `grep -E '(HasPrefix|HasSuffix|Contains).*"contrib/libs/(musl|cxxsupp)"' *.go`
   returns no matches in production code (architectural goal: no path-prefix
   dispatch for upstream-config-driven concerns).
8. Two consecutive `gen` runs produce sha256-identical JSON output
   (determinism).
