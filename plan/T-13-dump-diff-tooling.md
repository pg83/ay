# T-13 plan: focused `dump diff` reconnaissance tooling

(Note: `plan/T-13.md` is already taken by an unrelated, merged ticket
"RESOURCE_FILES root-relative source resolution". Ticket numbers were reused
across workspaces, so this plan lives under a distinct name to avoid clobbering
that file.)

## Scope

Tooling-only change to `dump_diff.go`. No graph-generation or validation
semantics change, so the acceptance bar is `go test ./...` plus fixture-driven
unit tests; `validate.py` is not required (no emitter touched). This is the
"diff tooling gap" ticket called out at the end of the T-1 recon plan.

Three concrete gaps slowed sg7 triage:

1. `--by-token` counts token occurrences across *all* paired outputs. It cannot
   restrict the delta to the leaf-most divergent outputs (the "roots"), so a
   2.18M ref-only input-token count names common headers but not which root
   family to fix first.
2. `--by-token` produces one global ranking. It cannot group token deltas by
   node kind or output directory, so you cannot see which modules/dirs carry a
   given missing-include family.
3. `--pair` prints `[field cmds differs]` with no token lines when the
   `cmd_args` token *multiset* matches but command structure (cwd, env, stdout,
   cmd count, or per-cmd arg ordering) differs. That forces manual JSONL
   inspection.

There is no upstream `dump diff`; this is our own reconnaissance tool, so the
"reproduce upstream mechanism" rule does not apply. The constraint that does
apply: reuse existing machinery (the roots computation, the token-match index,
`tokenCategory`, `outputTopDir`, `writeTokenRanking`) — do not build a parallel
path.

## Changes (all in `dump_diff.go`)

### CLI parsing (`cmdDumpDiff`)

- Stop routing `--roots` through `setMode`; make it a boolean modifier
  `wantRoots`. Dispatch:
  - mode `""` + `wantRoots` → `diffRoots` (preserves today's `--roots`).
  - mode `"by-token"` + `wantRoots` → root-restricted by-token.
  - `wantRoots` with any other mode → error.
- Add `--group <dims>` (comma list over `kind`,`dir`); only valid with
  `--by-token`, else error.

### Roots computation reuse

Extract the leaf-most-divergent-output set from `diffRoots` into
`computeRootOutputs(leftPath, rightPath) (leafSet map[string]bool, divergent int)`.
`diffRoots` calls it and only formats; `diffByToken` uses `leafSet` to filter.

### `diffByToken(leftPath, rightPath, bw, byTokenOpts)`

`byTokenOpts{rootsOnly bool; groupBy []string}`.

- When `rootsOnly`, skip left nodes that produce no root output.
- Accumulate token deltas into `our[field][group][token]` /
  `ref[field][group][token]` where `group` is the join of the selected dims
  (`kind` -> `nodeKVP`, `dir` -> top dir of the node's lexically-first output).
  Empty `groupBy` -> single group `""`, printed exactly as today via
  `writeTokenRanking` (keeps existing output/tests stable).
- With groups, print one `writeTokenRanking` block per (group, field).

### `diffPair` structured command differences

Split the `cmds` field handling into `writePairCmds(bw, left, right)`:

1. Print the flat `cmd_args` multiset delta (today's behavior).
2. If that produced no lines (multiset equal) but the field still differs,
   print structured differences: cmd count, and per cmd index the cwd, env,
   stdout, and — when a single cmd's arg multiset matches but the ordered
   sequence differs — the ordered `ours:`/`ref:` arg lists.

## Tests (`dump_test.go`)

- `--by-token --roots`: two paired nodes (parent + leaf child), only the leaf
  diverges in tokens; assert the token appears and a parent-only token does not.
- `--by-token --group=kind,dir`: nodes of two kinds/dirs; assert per-group
  headers and that each token lands under its node's group.
- `--pair` structured cmds: equal `cmd_args` multiset but different `cwd` /
  arg order; assert structured lines appear (today prints nothing).

## Expected effect on the gate

No change to any gating case. `go test ./...` stays green and gains the three
tests above. sg7 metrics unaffected (no emitter touched).
