# Yatool — Recreate ymake build-graph generator — Task Ledger

Authoritative ledger of planned and completed work. Detailed plan: `./docs/drafts/20260507-0134-recreate-ymake-plan.md`.

Status: `[ ]` planned · `[~]` in progress · `[x]` done · `[!]` blocked

---

## Milestones (high-level)

- [~] **M1** — Single-leaf vertical slice: parse one zero-PEERDIR LIBRARY's ya.make, emit one CC + one AR node, comparator wired and reporting all 4 levels. Acceptance: L3 = 100% on the chosen 2-node subgraph.
- [ ] **M2** — `tools/archiver` static slice (CC + AR + LD, recursive PEERDIR, no host platform). Acceptance: L1 ≥ 85%, L3 ≥ 50%.
- [ ] **M3** — Streaming emitter + parallel ya.make parser. Acceptance: peak RSS < 200 MB, wall ≤ 1.5× sequential.
- [ ] **M4** — Full host platform (tool transitive closure + foreign_deps merge). Acceptance: L0 = 100%, L1 ≥ 95%.
- [ ] **M5** — Compile-flag fidelity (hand-translate gnu_compiler.conf + settings.conf). Acceptance: L3 ≥ 95%.
- [ ] **M6** — Final L3 = 100% on the entire reference graph.

---

## Milestone 1 — PR breakdown

Detail in `./docs/drafts/20260507-0134-recreate-ymake-plan.md`. One line per PR here; sub-tasks stay in the plan doc.

- [ ] **PR-01** — Bootstrap: `go.mod`, `main.go`, subcommand router (`gen`/`compare`/`inspect`/`help`). Stdlib-only.
- [ ] **PR-02** — `node.go`, `emitter.go`, `uid.go`: Node type, Emitter interface, BufferedEmitter, Merkle UID finalizer.
- [ ] **PR-03** — `gjson.go`: streaming reader for the reference g.json.
- [ ] **PR-04** — `compare.go` + `compare_topology.go`: comparator L0 (topology fingerprint).
- [ ] **PR-05** — `compare_props.go`: comparator L1 (kv.p, module_*, outputs) + L2 (inputs, tags, requirements).
- [ ] **PR-06** — `compare_cmd.go`: comparator L3 (cmd_args + env byte-exact).
- [ ] **PR-07** — `yamake.go`: minimal ya.make parser (LIBRARY/PROGRAM/PEERDIR/SRCS/SET/END).
- [ ] **PR-08** — `cc.go`, `flags.go`, `toolchain.go`: minimal CC rule with hardcoded flag bundles.
- [ ] **PR-09** — `ar.go`: minimal AR rule.
- [ ] **PR-10** — `gen.go`: gen subcommand wires PR-07/08/09; emits 2-node subgraph; integrated comparator pass shows L3 = 100% on the slice.

---

## Cross-cutting architectural notes (locked)

- [x] **D1** — Module path: `module yatool` in `go.mod`.
- [x] **D2** — Layout: `go.mod` + ALL `.go` files at `/home/pg/monorepo/yatool/*.go` (flat, `package main`). Only allowed new dirs under repo root: `docs/drafts/`, `docs/logs/`. No `internal/`, no `cmd/`. Lands in PR-01.
- [x] **D3** — Single binary, stdlib `flag.NewFlagSet` subcommand router. Subcommands: `gen`, `compare`, `inspect`, `help`. Lands in PR-01.
- [x] **D4** — JSON: `encoding/json` with `json.Decoder` for streaming reads. No goccy/simdjson until profiling proves need.
- [x] **D5** — `Node` mirrors on-disk JSON exactly; `omitempty` only where g.json itself omits (`host_platform`, `foreign_deps`). UIDs are placeholders until finalizer pass. Lands in PR-02.
- [x] **D6** — `Emitter` interface with `NodeRef` indirection so M1's BufferedEmitter is swappable for M3's StreamingEmitter without rewriting rules. Lands in PR-02.
- [x] **D7** — UID = `base64url(sha1(canonical-node-bytes))[:22]` Merkle-style. Comparator L0 uses topology fingerprints, never raw UID strings. Lands in PR-02.
- [x] **D8** — Two-pass engine: `Generate(cfg PlatformConfig, target string, emit Emitter)` invoked twice (Target/Host) + merge step. M1 stubs host pass.
- [x] **D9** — One Go file per op-type (`cc.go`, `ar.go`, `ld.go`, …). Flag bundles in `flags.go` as named `[]string`s composed at call time. Toolchain constants in `toolchain.go`. Parser in `yamake.go`.
- [x] **D10** — Comparator co-resident: `compare.go` + `compare_topology.go` + `compare_props.go` + `compare_cmd.go`.
- [x] **D11** — Reference data: `/home/pg/monorepo/yatool_orig/g.json` read in place. No fixture copy.
- [x] **D12** — `build/scripts/*.py` referenced by `$(SOURCE_ROOT)/build/scripts/<name>.py` in cmd_args, never re-implemented.
- [x] **D13** — Stdlib-only through M1. Vendor decision deferred to M3 (open question Q2).
- [x] **D14** — Determinism: NO `range` over `map[...]X` in any code path that emits node fields. `sort.Strings` before serialize. `json.Encoder.SetEscapeHTML(false)`.
- [x] **D15** — `inputs`/`conf` in our generator output may be empty; comparator only reads `graph` + `result`. Revocable.
- [ ] **Q1** — Pick the M1 leaf module (smallest LIBRARY with zero PEERDIR in the reference graph). Default: `build/cow/on` if it qualifies. To be decided when PR-10 starts; an inspection subagent will scan g.json and confirm.
- [ ] **Q2** — Vendor third-party Go deps from M3 onward, or stay stdlib-only through M6. Default: stdlib-only; revisit at M3.

---

## Completed

(none yet)
