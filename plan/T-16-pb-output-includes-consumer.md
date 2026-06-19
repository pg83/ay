# T-16 plan: generated .pb.h reach the run-program consumer; PR stops over-emitting OUTPUT_INCLUDES

(distinct filename: plan/T-16.md already holds a merged YAFF ticket.)

## Observed divergence (current trunk, baseline `.out/baseline`)

Two mirror-image defects on the caesar `features.gen.*` pair, both in the
generated-protobuf-header class:

- **CC consumer under-emits** — the node that compiles the RUN_PROGRAM-generated
  `features.gen.cpp` has, in ours, output
  `$(B)/.../caesar/$(B)/.../caesar/features.gen.cpp.pic.o` (double `$(B)`
  prefix) with **2 inputs**. REF's correctly-pathed
  `$(B)/.../caesar/features.gen.cpp.pic.o` has **3397 inputs**, including the
  517 Argus/YABS `*.pb.h` (e.g. `$(B)/ads/argus/proto/events/alice_music_search_event.pb.h`).
  Because of the malformed path our CC compiles a *non-registered* source VFS,
  so its include scan finds none of the OUTPUT_INCLUDES closure.

- **PR producer over-emits** — our PR node (producer of `features.gen.cpp`)
  carries **3395 inputs**; REF's carries **1** (just the tool
  `$(B)/.../caesar/codegen_bin/fs_codegen`). The caesar `RUN_PROGRAM` has **no
  IN**, only `OUTPUT_INCLUDES` + `OUT`.

The ticket text (written from a pre-T-5 state) describes the headers as
ref-only; on current trunk the same generated-protobuf-header class is the
delta, only the sign per node differs.

## Upstream mechanism

`macro RUN_PROGRAM` (`build/ymake.core.conf`):

    .CMD=... ${hide;input:IN} ... ${hide;output_include:OUTPUT_INCLUDES} ... ${hide;output:OUT} ...

- `IN` → `input` (scan-on-include): the IN files and their parsed-include
  closures are the PR node's inputs.
- `OUTPUT_INCLUDES` → `output_include`: induced deps recorded **on the OUT
  files**, surfaced on whoever *consumes* the OUT (the downstream CC that
  recompiles an auto cc-source OUT, or a peer that `#include`s a header OUT).
  They are **not** inputs of the RUN_PROGRAM node.

Verification against the byte-exact sg5 `control_board` pair:
`control_board_proto.cpp.in` (the **IN** template) literally
`#include <ydb/core/protos/tablet.pb.h>` / `config.pb.h` — so that PR's 320
proto/pb.h inputs arrive through the **IN-walk**, not through OUTPUT_INCLUDES.
caesar has no IN template, hence REF lists only the tool.

So: PR inputs = IN closure + tool. OUTPUT_INCLUDES ride the OUT to consumers.

## Root causes in our code

1. `emit_codegen_cc.go` `emitCodegenDownstreamCC` / `emitCodegenDownstreamAS`
   build the generated-source path as `build(instance.Path.rel()+"/"+rel)`.
   When `rel` is already a `$(B)/<mod>/...` path (the `${BINDIR}/...` OUT token
   after env expansion), this double-prefixes. The PR registers the OUT via
   `copyFileOutputVFS` (correct), so the CC compiles a *different*, unregistered
   VFS and misses the registered OUTPUT_INCLUDES.

2. `emit_pr.go` `prInputClosure` walks (a) every cc-source `OUT`/`STDOUT`
   closure and (b) each `OUTPUT_INCLUDES` target's `.proto` closure into the PR
   node's inputs. Both contradict upstream (`output_include` ≠ input). They were
   redundant for control_board (the IN template already #includes those headers)
   and pure over-emit for caesar (no IN).

## Changes

- **emit_codegen_cc.go**: use `copyFileOutputVFS(instance.Path.rel(), rel)` for
  both `cppPath` and `asmPath`. This equals `build(mod+"/"+rel)` for a bare
  relative OUT (unchanged common case) and resolves an already-rooted `$(B)/…`
  OUT verbatim — matching the VFS the PR registered, so include resolution finds
  the OUTPUT_INCLUDES closure.

- **emit_pr.go** `prInputClosure`: delete the cc-source OUT/STDOUT walk and the
  OUTPUT_INCLUDES `.proto` block; keep only the IN-walk + tool. Drop the now
  unused `walkOne`. Net-negative.

## Test (test-first)

`emit_pr_test.go`: a PROTO_LIBRARY `p` whose `a.proto` imports `q/b.proto`
(another PROTO_LIBRARY) so `a.pb.h` transitively pulls `b.pb.h`. A LIBRARY `gen`
with `RUN_PROGRAM(... OUTPUT_INCLUDES p/a.pb.h OUT ${BINDIR}/gen.cpp)` and a
PROGRAM peering it. Assert:
- the CC node `$(B)/gen/gen.cpp.pic.o` (no double `$(B)`) exists and lists
  `$(B)/p/a.pb.h` and `$(B)/q/b.pb.h` each exactly once;
- the PR node producing `$(B)/gen/gen.cpp` does **not** carry `a.pb.h`
  (OUTPUT_INCLUDES is not a PR input; no IN file).
Fails before the fix (malformed double-prefixed CC, pb.h absent on the consumer).

## Gate expectation

sg2–sg6 stay byte-exact (control_board covered by IN-walk). sg7 `matched` rises
as the caesar CC pairs and the PR over-emit clears; the Argus/YABS generated
`.pb.h` leave the caesar features.gen pair. Any residual caesar diff is the
separate YAFF / yt_proto / libcxx-util source-header classes.
