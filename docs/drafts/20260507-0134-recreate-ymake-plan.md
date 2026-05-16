# Plan: Recreate ymake build-graph generator in Go

## 1. Project summary

We are reimplementing `ymake`'s build-graph generator as a Go binary at the root of `/home/pg/monorepo/yatool`. Given the source tree, our binary must produce a JSON document whose `graph` and `result` sections are equivalent (modulo content-derived UID values) to the existing `g.json` for the target `tools/archiver`. Build rules from `build/conf/*` and `build/ymake.core.conf` are explicitly hand-translated into Go code, never parsed at runtime; `build/scripts/*.py` are invoked as-is from generated cmd_args. Success is measured by a fuzzy comparator with four levels (L0 topology → L1 outputs/op-type → L2 inputs/tags/requirements → L3 byte-exact cmd_args/env), reported as a percentage that strictly increases as milestones land. The final milestone reaches L3 100% on the entire archiver graph (the planner observed ~3,730 nodes locally; the original brief mentioned 17,433 — discrepancy noted as R1 below; the on-disk g.json is the authoritative comparison target).

## 2. Ground truth from g.json (verified by planner, 2026-05-07)

These numbers refine the brief; the plan uses these.

- File: `/home/pg/monorepo/yatool_orig/g.json`. Per planner's read: ~26 MB, 3,730 nodes, 1 result. (Orchestrator note: previous read showed 108 MB / 17,433 / 7 results — file may have been regenerated between reads. Treat as R1 risk; re-verify before M1 starts.)
- Top-level keys: `conf`, `graph`, `inputs`, `result`. `inputs` is `{}` (empty). `conf` is scalars only (no UID refs). **Both are out of scope** for our generator beyond emitting a stable shell.
- Per-node fields uniformly present: `uid, self_uid, stats_uid, cmds, deps, env, inputs, kv, outputs, platform, requirements, tags, target_properties`. Optional: `host_platform`, `foreign_deps`.
- `kv.p` distribution (per planner read): **CC 3571, AS 83, AR 48, JS 23, LD 3, CP 1, R6 1**. (Original recon also saw PY/PB/EN/CY/etc; depends on snapshot.) M1 must cover CC + AR + LD; AS/JS/CP/R6 deferred.
- Platforms: `default-linux-aarch64` (target) and `default-linux-x86_64` (host). `host_platform: true` ⇔ `tags: ["tool"]`.
- Modules in planner's snapshot: 51 (48 lib + 3 bin), all `module_lang: cpp`.
- `foreign_deps`: only the `tool` key seen. **1 of 26 tool UIDs points outside the graph** (resolved externally via toolchain fetcher); we must accept and emit dangling refs.
- Multi-cmd nodes: only LD nodes (4 cmds each: `vcs_info.py` → `clang` link → `link_exe.py` → `fs_tools.py link_or_copy_to_dir`). Every other node has exactly 1 cmd.
- Median CC cmd_args ~2,777 bytes; max ~81,346; total ~10 MB across the graph. Compile-flag de-duplication into Go slices is essential.

## 3. Cross-cutting architectural decisions (locked unless flagged)

All decisions land in PR-01 unless noted.

- **D1 — Module path.** `module yatool` in `go.mod`.
- **D2 — Layout.** `go.mod` + ALL `.go` files at `/home/pg/monorepo/yatool/*.go` (flat, `package main`). Only new dirs allowed under repo root: `docs/drafts/`, `docs/logs/` (per CLAUDE.md). No `internal/`, no `cmd/`. Grep-from-one-place is non-negotiable.
- **D3 — Single binary, subcommand router.** Hand-written `switch` on `os.Args[1]` in `main.go`; each subcommand uses stdlib `flag.NewFlagSet`. Subcommands: `gen`, `compare`, `inspect`, `help`. No third-party CLI lib.
- **D4 — JSON.** `encoding/json`. `json.Decoder` for streaming reads when comparator wants to avoid two graphs in memory. No `goccy/go-json` / `simdjson` until profiling demands.
- **D5 — Internal Node type.** Single `Node` struct mirroring on-disk JSON exactly, stable field ordering, `omitempty` only where g.json itself omits (`host_platform`, `foreign_deps`). UIDs are `PendingUID` placeholders until finalizer pass; rest is content-complete the moment a rule emits. **Streaming-friendly**: rules write `Node` to a channel-typed `Emitter`, finalizer drains, computes UIDs in topological order over dependency edges (separate `NodeRef` placeholder type), writes JSON incrementally.
- **D6 — Emitter interface.**
  ```go
  type NodeRef struct{ id int64 } // local, monotonic; resolved to UID in finalize
  type Emitter interface {
      Emit(n *Node) NodeRef // Node.deps/foreign_deps use []NodeRef, not strings
      Result(NodeRef)
  }
  ```
  Two impls in plan: `BufferedEmitter` (slice-backed, M1) and `StreamingEmitter` (writes nodes to disk as they arrive, finalizes UIDs in second pass over scratch file, M3). M1 ships with `BufferedEmitter`; M3 is a swap, not a rewrite.
- **D7 — UID strategy.** Our UIDs do NOT match real ymake UIDs. We use `base64url(sha1(canonical-node-bytes))[:22]`, where canonical bytes have `deps` substituted to our sorted child-UIDs (Merkle-style). Comparator's L0 step computes a topology fingerprint per node from `(kv.p, len(deps), sorted(child fingerprints))`; UIDs themselves are never compared.
- **D8 — Two-pass / host-target.** Engine = single function `Generate(cfg PlatformConfig, target string, emit Emitter)`. Invoked twice (TargetCfg, HostCfg). Merge step walks target graph, resolves `TOOL(...)` refs to host node UIDs, populates `foreign_deps.tool`, unions both `graph` slices. M1 lands engine + stub host pass.
- **D9 — Hardcoded-rules organization.** One Go file per op-type: `cc.go`, `ar.go`, `ld.go` for M1; `as.go`, `js.go`, `cp.go`, `r6.go` later. Compile-flag bundles in `flags.go` as named `[]string` constants composed at call time. Toolchain paths/platform constants in `toolchain.go`. Parser: `yamake.go`. Macro evaluator: `macros.go`.
- **D10 — Comparator location.** Same binary, `compare` subcommand. Sources: `compare.go`, `compare_topology.go` (L0), `compare_props.go` (L1+L2), `compare_cmd.go` (L3 with diff).
- **D11 — Test data.** Reference is `/home/pg/monorepo/yatool_orig/g.json` read in place. No fixture copy. Unit tests use small synthetic graphs in `*_test.go`. Integration tests (M2+) compare full output vs reference.
- **D12 — Build scripts (Python).** `build/scripts/*.py` referenced by `$(SOURCE_ROOT)/build/scripts/<name>.py` in our cmd_args, exactly as g.json does. Never re-implement.
- **D13 — Dependencies.** Stdlib-only target. Acceptable third-party in M3+: `golang.org/x/sync/errgroup` (parallel parser); a JSON-diff library for L3 human-readable mismatches (deferred). Both vendored under `/home/pg/monorepo/yatool/vendor/` if added.
- **D14 — Determinism.** Map iteration forbidden in any code path that emits node fields. `sort.Strings` on every list before serialize. `json.Encoder` with `SetEscapeHTML(false)` to match g.json's encoding.
- **D15 — `inputs`/`conf` in our output.** Both can be empty `{}` / minimal scalars; the comparator only reads `graph` + `result`. Locked, revocable.

## 4. Milestones

- **M1 — Single-leaf vertical slice (CC for one .c → AR → minimal LD).** Pick smallest module from g.json with zero PEERDIR, parse its `ya.make`, expand hardcoded `LIBRARY()` macro, emit one CC node + one AR node. Wire comparator. Acceptance: `go run . compare g.json our.json` reports **L3 = 100%** for the chosen subgraph and **L0 ≥ 5%** against full reference.
- **M2 — `tools/archiver` static slice.** Same target, ignoring host tooling. Parse `tools/archiver/ya.make` + 3 PEERDIRed libraries recursively. Emit CC for every `.cpp/.c/.S`, AR per library, LD root. Stub host platform. Acceptance: L1 ≥ 85% on archiver subgraph, L3 ≥ 50%, binary < 5 s.
- **M3 — Streaming emitter + parallel parser.** Replace `BufferedEmitter` with `StreamingEmitter` (tmp file). Parse `ya.make` files in parallel via `errgroup`. Acceptance: peak RSS < 200 MB on full archiver graph; wall time ≤ 1.5× sequential.
- **M4 — Full host platform (tool transitive closure + foreign_deps merge).** Run engine twice (target + host), merge with `foreign_deps`. Acceptance: L0 = 100%; L1 ≥ 95%.
- **M5 — Compile-flag fidelity (the long pole).** Hand-translate every flag bundle from `/home/pg/monorepo/yatool_orig/build/conf/compilers/gnu_compiler.conf` + `/home/pg/monorepo/yatool_orig/build/conf/settings.conf` into `flags.go`. Acceptance: L3 ≥ 95%.
- **M6 — Final L3 = 100%.** Close residual diffs. Acceptance: `compare g.json our.json` prints `L0=100% L1=100% L2=100% L3=100%`.

## 5. M1 PR-level breakdown

All paths absolute. Concurrency assumes orchestrator's worktree mechanism.

- **PR-01 — Bootstrap module + main + subcommand router.** [Sonnet]
  - Files: `go.mod`, `main.go`.
  - Scope: `module yatool`, Go 1.25; `main.go` dispatches `gen`, `compare`, `inspect`, `help`.
  - Success: `cd /home/pg/monorepo/yatool && go build ./... && ./yatool help` exits 0 and lists three subcommands.
  - Deps: none. Must merge before all others.
- **PR-02 — Node type + Emitter + UID finalizer.** [Opus — design-heavy]
  - Files: `node.go`, `emitter.go`, `uid.go`.
  - Scope: `Node`, `NodeRef`, `Emitter` per D5/D6/D7; `BufferedEmitter`; `Finalize` topo-orders, computes Merkle UIDs, replaces `NodeRef` placeholders, emits `*Graph`.
  - Success: `go test -run TestEmitter` passes a hand-built 3-node DAG round-trip. Re-run produces byte-identical output.
  - Deps: PR-01.
- **PR-03 — g.json reader + canonical types.** [Sonnet]
  - Files: `gjson.go`, `gjson_test.go`.
  - Scope: define on-disk JSON types, `LoadReference(path) (*Graph, error)` with `json.Decoder` streaming + `LooseDecode` mode (skip `conf`, `inputs`).
  - Success: `go test -run TestLoadReference` parses `/home/pg/monorepo/yatool_orig/g.json` in ≤2 s, asserts node count and result count from the snapshot.
  - Deps: PR-02. Parallel with PR-04, PR-05 (different files).
- **PR-04 — Comparator L0 (topology).** [Opus]
  - Files: `compare.go`, `compare_topology.go`, `compare_topology_test.go`.
  - Scope: per-node fingerprint `f(n) = hash(kv.p || sorted(f(child) for child in deps))`; multiset compare; `L0 = matched / total`.
  - Success: `compare -level=0 g.json g.json` prints `L0=100%`. Synthetic renumber-test reports 100%; mutated edge drops below 100%.
  - Deps: PR-03.
- **PR-05 — Comparator L1+L2 (props, inputs, tags, requirements).** [Sonnet]
  - Files: `compare_props.go`, `compare_props_test.go`.
  - Scope: pair via L0 fingerprint; diff `kv.p`, `target_properties`, `outputs` (L1); `inputs`, `tags`, `requirements` (L2); percentages.
  - Success: g.json vs itself = `L1=100% L2=100%`. Mutation of one `tags` drops L2.
  - Deps: PR-04.
- **PR-06 — Comparator L3 (cmd_args + env byte-exact).** [Sonnet]
  - Files: `compare_cmd.go`, `compare_cmd_test.go`.
  - Scope: deep-equal `cmds[*].cmd_args`, `cmds[*].env`, top-level `env`. With `-v`, dump up to 50 unified diffs. Tool exits 0 always (report-only) unless `-strict` and any level < 100%.
  - Success: g.json vs itself = `L3=100%`.
  - Deps: PR-05.
- **PR-07 — Minimal `ya.make` parser.** [Opus]
  - Files: `yamake.go`, `yamake_test.go`.
  - Scope: hand-rolled lexer + recursive-descent. M1 supports `LIBRARY()`, `PROGRAM()`, `PEERDIR(...)`, `SRCS(...)`, `SET(name "value")`, `END()`, comments. Returns `*MakeFile` AST.
  - Success: parses M1 reference files (chosen leaf module's `ya.make`, `tools/archiver/ya.make`, `library/cpp/archive/ya.make`) and asserts AST shape.
  - Deps: PR-01. Parallel with PR-02..06.
- **PR-08 — Toolchain constants + minimal CC rule.** [Opus]
  - Files: `toolchain.go`, `flags.go`, `cc.go`, `cc_test.go`.
  - Scope: `TargetCfg` for `default-linux-aarch64`; `EmitCC(cfg, src, module, emit)` produces one CC node with bundled flags observed in g.json. Flags grouped: `commonCFlags`, `aarch64Flags`, `wnoFlags`, `defines`, `arcadiaIncludes`. Flag list extracted from actual reference node, not from `gnu_compiler.conf`.
  - Success: `EmitCC` for the leaf module's source produces `cmd_args` byte-equal to reference (assert by loading g.json, locating node by `outputs[0]`).
  - Deps: PR-02, PR-07. Parallel with PR-09.
- **PR-09 — Minimal AR rule.** [Sonnet]
  - Files: `ar.go`, `ar_test.go`.
  - Scope: `EmitAR(cfg, module, objRefs, emit)` produces AR node matching observed shape (`link_lib.py ar GNU_AR ...`).
  - Success: AR node for leaf module byte-matches reference.
  - Deps: PR-02. Parallel with PR-08.
- **PR-10 — `gen` driver + first vertical slice.** [Opus — integration]
  - Files: `gen.go`, `gen_test.go`. Updates `main.go`.
  - Scope: `make -j 0 -G <module-dir> > our.json` parses one `ya.make` (no recursion in M1), emits CC per SRCS + AR closing module, finalizes, writes JSON. Single platform pass.
  - Success: `make -j 0 -G <leaf-module-dir> > /tmp/our.json && compare g.json /tmp/our.json` prints `L3=100%-of-2-nodes` (full L3 on the 2 emitted; small percentages overall — that's expected).
  - Deps: PR-06, PR-08, PR-09. Last PR of M1.

**Concurrency map.**
- PR-01 → PR-02 (serial).
- After PR-02: PR-03 + PR-07 in parallel.
- After PR-03: PR-04 → PR-05 → PR-06 (serial chain).
- PR-08 + PR-09 parallel after PR-07.
- PR-10 after PR-06, PR-08, PR-09.

## 6. Risks and assumptions

- **R1.** Brief said 17,433 / 7 results / 108 MB; planner's read showed 3,730 / 1 / 26 MB. **Re-verify g.json before M1 starts.** Plan numbers may need adjustment.
- **R2.** Dangling foreign_deps tool UIDs exist; merge step must allow them. M4 needs an "external tool" registry.
- **R3.** Hand-translating compile flags is the biggest unknown. M5 may slip; M6 is the buffer.
- **R4.** No PY/PB/EN nodes in planner's snapshot. M-final genuinely needs only CC/AS/AR/LD/JS/CP/R6 if snapshot holds. R1 may invalidate.
- **R5.** UID determinism depends on `encoding/json` output stability across Go versions. Pin Go 1.25 in `go.mod`.
- **R6.** Multi-cmd LD (4 cmds) is subtler than 1-cmd model. M2's LD must implement all four; M1 ends at AR (no LD exercised).
- **R7.** `BufferedEmitter` may pressure memory on full graph. M3's streaming swap mitigates.
- **R8.** Map iteration in Go is randomized → easy non-determinism. D14 mandates sort-before-serialize. Reviewers grep for `range .*map\[`.
- **R9.** `ya.make` parser may meet unplanned macros. M1 subset only; M2 extends.

## 7. Open questions for the user

- **Q1.** Pick the leaf module for M1's vertical slice. **Default if no answer:** smallest LIBRARY with zero PEERDIR found in g.json (e.g. `build/cow/on` or whatever the planner's later inspection reveals).
- **Q2.** Is `vendor/` acceptable for future Go third-party deps, or stdlib-only through M6? **Default:** stdlib-only for M1; revisit at M3.

## 8. Critical files for implementation

- `/home/pg/monorepo/yatool/main.go` — subcommand dispatcher; touches every milestone.
- `/home/pg/monorepo/yatool/node.go` — canonical Node type; defines what every rule emits.
- `/home/pg/monorepo/yatool/emitter.go` — streaming-friendly Emitter contract; architectural pivot.
- `/home/pg/monorepo/yatool/compare.go` — drives all four comparator levels; project's progress meter.
- `/home/pg/monorepo/yatool/cc.go` — largest hand-translated rule; sets pattern every other rule follows.
