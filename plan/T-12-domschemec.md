# T-12 — Model the `.sc` (domschemec) source rule

## Upstream mechanism

`build/ymake.core.conf` declares the `.sc` source rule:

```
macro _SRC("sc", SRC, SRCFLAGS...) {
    .CMD=${tool:"tools/domschemec"} --in ${input:SRC} --out ${norel;output;suf=.h:SRC} \
         ${hide;output_include:"library/cpp/domscheme/runtime.h"} ${SRCFLAGS} \
         ${hide;kv:"p SC"} ${hide;kv:"pc yellow"}
    .PEERDIR=library/cpp/domscheme
}
```

A `SRCS(foo.sc)` entry therefore yields a single producer node:

- command: `$(B)/tools/domschemec/domschemec --in $(S)/<mod>/foo.sc --out $(B)/<mod>/foo.sc.h`
- output: `$(B)/<mod>/foo.sc.h` (SRC with `.h` appended)
- inputs: the domschemec tool binary, the `.sc` source, and the
  `library/cpp/domscheme/runtime.h` `output_include` together with its full
  scanned include closure (in the sg7 reference: 976 inputs, runtime.h + libcxx).
- kv: `p=SC`, `pc=yellow`
- tool foreign dep: the `tools/domschemec` LD node (rendered into both `deps`
  and `foreign_deps.tool`).
- implicit module PEERDIR: `library/cpp/domscheme`.

The generated `foo.sc.h` is a header consumed via `#include`; it carries the
`output_include` of `runtime.h`, so a consumer that includes it pulls runtime.h's
closure. There is no compile step — the `.sc` source produces only a header
(like `.h.in`).

Verified against `/home/pg/monorepo/4/sg7.json`: the node producing
`$(B)/kernel/reqbundle/scheme/options.sc.h` has exactly this shape
(kv p=SC/pc yellow, cmd `domschemec --in … --out …`, foreign_deps.tool =
domschemec LD, inputs lead with tool, options.sc, runtime.h).

## Current state in ay

`emitOneSource` has no `.sc` arm, so `SRCS(*.sc)` hits the
`WarnUnsupportedSource` fallthrough and is skipped under `--keep-going`. No SC
producer is emitted; `options.sc.h` is reference-only in sg7. `procKindStr` has
no `SC` entry. No implicit domscheme peer is added.

## Files to touch

1. `node_attrs.go` — add `pkSC` ProcKind + `"SC"` in `procKindStr`.
2. `vfs_consts.go` — add `domschemeRuntimeVFS = source("library/cpp/domscheme/runtime.h")`.
3. `arg_consts.go` — add `argToolsDomschemec`, `argDashIn`, `argDashOut`.
4. `str_intern.go` — add `srcExtSc` to `SrcExtClass`, classify `.sc`, include it
   in `isCodegenProducingSrcID` (pass-1 so the header registers before closure walks).
5. `emit_sc.go` (new) — `emitSC` node emitter + `emitLibrarySCSource` dispatch:
   resolve the domschemec tool, walk runtime.h closure, emit the SC node,
   register the generated `.sc.h` with its `output_include` runtime.h, return nil
   (header-only, no compile).
6. `emit_sources.go` — add the `.sc` dispatch arm.
7. `modules.go` — add `hasSc` to the src-class pass and append
   `library/cpp/domscheme` to `d.peerdirs` (implicit PEERDIR).
8. `emit_sc_test.go` (new) — regression test: a module `SRCS(options.sc)` whose
   SC node asserts command, tool dep, input `.sc`, output `.sc.h`, kv, runtime.h
   input, and the implicit domscheme peer.

## Expected gate effect

- `go test ./...` passes (new test green).
- sg2–sg6 byte-exact (no `.sc` sources in those targets — isolated change).
- sg7: `kernel/reqbundle/scheme/options.sc.h` becomes a produced SC node present
  in both graphs instead of reference-only; SPLIT_CODEGEN SC nodes untouched.

## Rework: no ADDINCL for the generated .sc.h

The upstream `_SRC("sc")` rule (build/ymake.core.conf:3398) is
`.CMD=...${norel;output;suf=.h:SRC}...` with **no `addincl` modifier** on the
generated header — unlike `CONFIGURE_FILE`'s `${addincl;output:Dst}`. An earlier
revision registered the `.sc.h` build dir via `addGeneratedOwnHeaderInclude`,
which feeds `addInclGlobal`/`addInclUserGlobal` and therefore leaked
`-I$(B)/<scheme-mod>` through the global ADDINCL channel into every PEERDIR
consumer's compile (e.g. `kernel/reqbundle`'s `block.cpp.o`). That diverged from
upstream and dropped sg7 normalized-node parity (matched 48428 → 48354, all 88
lost nodes explained purely by the extra `-I`). Removing the `.sc` collectStmts
arm — the `.sc.h` producer contributes no ADDINCL at any scope; the header
resolves via the global `$(B)` build root like any other generated output —
restores parity (matched 48428 → 48442, regressed=0) while keeping options.sc.h
present in both graphs and all 890 SC outputs paired.
