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

- [x] **PR-01** — Bootstrap: `go.mod`, `main.go`, subcommand router (`gen`/`compare`/`inspect`/`help`). Stdlib-only.
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

- **PR-01** (2026-05-07) — Bootstrap of the Go module and subcommand router. Files added: `go.mod` (`module yatool`, `go 1.25`, no `require`/`toolchain`); `main.go` (single file, `package main`, hand-written switch dispatcher on `os.Args[1]` per D3, four subcommands `gen`/`compare`/`inspect`/`help`, three stub bodies that print `<name>: not implemented yet` and return 1, top-level help on stdout exit 0, no-args/unknown-subcommand exit 2 with usage on stderr).
  Verification: `go build ./...` 0; `go vet ./...` clean; `gofmt -l *.go` empty; `./yatool help`→0; `./yatool`→2; `./yatool gen`→1; `./yatool gen -h`→1 (post-fix); `./yatool gen --help`→1; `./yatool gen --foobar`→1; `./yatool gen foo bar`→1; `./yatool compare -h`→1; `./yatool inspect --foobar`→1; `./yatool wat`→2; `./yatool --help`→0; `./yatool -h`→0.
  Two review rounds. Round 1 (4 defects):
  - **D01 (major)** + **D02 (minor)** + **D03 (nit)** — `flag.NewFlagSet(name, flag.ExitOnError).Parse(args)` in stubs short-circuited `-h`/`--help`/unknown-flag paths to flag's auto-usage and exited 0/2 BEFORE the stub message printed. **Fixed** by removing `fs.Parse(args)` from all three stubs and switching the parameter to `_ []string` (`main.go:50-69`).
  - **D04 (nit)** — orchestrator's `tasks.md` `[ ]` → `[~]` flip was uncommitted at review time; reviewer flagged correctly per brief. **Resolved** by staging `tasks.md` with the PR commit (this entry).
  Round 2 (1 nit):
  - **D05 (nit)** — surviving `_ = flag.NewFlagSet(...)` in stubs is dead ceremony; `"flag"` import is dead-loaded. **Deferred to PR-10** which will rewrite stubs to register real flags and naturally remove the ceremony.
  Surprises / constraints future PRs must respect:
  - **D3 letter vs. spirit:** "each subcommand uses `flag.NewFlagSet`" was preserved in PR-01 only as `_ = flag.NewFlagSet(...)`. PR-10 (`gen` driver) MUST replace these three lines with real flag registration + Parse, and either keep `"flag"` as a load-bearing import or drop it. Failure to do so leaves dead code in `main.go`.
  - **Flag-mode choice for stubs vs. real subcommands:** `flag.ExitOnError` shipped to satisfy spec letter, but is dangerous for stubs because it short-circuits `-h` to `os.Exit(0)`. PR-10 should use `flag.ContinueOnError` (or override `fs.Usage` + `SetOutput`) so the subcommand body retains control of exit semantics. This pattern will recur for every subcommand that adds flags — codify it in the PR-10 brief.
  - **Exit-code domain split:** `1` = recognised-but-not-implemented; `2` = yatool's argument-level errors (no subcommand, unknown subcommand). Future subcommands' real argument errors should also exit `2`; their domain-specific failures should exit `1` or `>2` (TBD). Keep the split visible.
  - **Review-brief hygiene:** Future review briefs MUST enumerate expected ledger churn (`tasks.md` `[~]` flip, `defects.md` round entries) so reviewers don't flag legitimate orchestrator bookkeeping as undeclared scope drift (D04).
