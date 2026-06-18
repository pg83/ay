# T-8 plan: transitive C++ GLOBAL ADDINCL into cookie_cleaner (CPP_EVLOG peer edge)

(Note: `plan/T-8.md` already exists from an unrelated older ticket batch —
FROM_SANDBOX/RESOURCE — so this plan uses a distinct filename to avoid
clobbering committed work.)

## Reproduction and root cause (divergence from the T-5 plan hypothesis)

The T-5 plan (Class A) framed `cookie_cleaner.cpp.o`'s missing
`-I$(S)/contrib/libs/brotli/c/include`, `…/re2/include`, `…/snappy/include`
as a *generic* break in ordinary C++ `ADDINCL(GLOBAL)` peer-closure
propagation, and proposed a synthetic regression: consumer → intermediate
PEERDIR → leaf `ADDINCL(GLOBAL contrib/libs/snappy/include)`.

That synthetic case **already passes** on current code (verified by adding it
and running `go test`). The generic propagation mechanism is intact:
`splitAddInclPaths` / `walkPeersForGlobalAddIncl` / `peerAddInclGlobal` /
`emit_cc.go` all carry transitive GLOBAL addincl correctly — snappy's GLOBAL
include reaches 8488 CC commands in our sg7 output, and proto `*.pb.cc.o`
commands receive transitive GLOBAL addincl too (e.g. `bs_hit.pb.cc.o` already
has `-I$(S)/contrib/libs/re2/include`).

The real cause, found by bisecting the closure with the reproduced sg7 graphs:

```
cookie_cleaner  --PEERDIR-->  yabs/adfox/amacs/proto/config (PROTO_LIBRARY)
                --PEERDIR-->  yabs/adfox/amacs/proto/enums   (PROTO_LIBRARY)
                --PEERDIR-->  yabs/server/proto/log/options  (PROTO_LIBRARY, CPP_EVLOG())
```

`yabs/server/proto/log/options/options.pb.cc.o`:
- REF: has brotli/snappy/re2 `-I$(S)` tokens.
- OUR: has none.

`log/options/ya.make` declares **no** explicit `PEERDIR`; its only C++ peer
edge comes from `CPP_EVLOG()`. Upstream (`build/conf/proto.conf`):

```
macro CPP_EVLOG() {
    CPP_PROTO_PLUGIN0(event2cpp tools/event2cpp DEPS library/cpp/eventlog)
    ENABLE(_BUILD_PROTO_AS_EVLOG)
}
```

`CPP_PROTO_PLUGIN0(... DEPS X)` accumulates `X` into `CPP_PROTOBUF_PEERS`, and
`ymake.core.conf` does `PEERDIR+=$CPP_PROTOBUF_PEERS` on the CPP_PROTO
submodule. (The `_CPP_PROTO_EVLOG_CMD` body independently carries
`.PEERDIR=library/cpp/eventlog contrib/libs/protobuf`.) Net effect:
`library/cpp/eventlog` becomes a C++ peer of the proto module.

`library/cpp/eventlog`'s own CC command already carries brotli/snappy/re2 in
OUR graph (its GLOBAL addincl closure is correct). So once the eventlog peer
edge exists, the *existing* GLOBAL propagation carries those three includes up
through `log/options → enums → config → cookie_cleaner`.

In OUR code `CPP_EVLOG` is a stubbed no-op (`gen.go` `acknowledgedMacros`), so
the eventlog peer edge is never created — hence the missing includes.

## Upstream mechanism reproduced

`CPP_EVLOG()` contributes `library/cpp/eventlog` as a C++ PEERDIR of the
module (the `DEPS library/cpp/eventlog` of its `CPP_PROTO_PLUGIN0`). This is
exactly the existing `tokCppProtoPlugin0` handling
(`modules.go: d.peerdirs = append(d.peerdirs, plugin.Deps...)`).

Scope, per ticket: model ONLY this peer edge so ordinary C++ GLOBAL ADDINCL
propagation reaches the consumer. Do NOT model the event2cpp codegen / the
`_BUILD_PROTO_AS_EVLOG` output-naming or generated-header includes (that is the
out-of-scope proto codegen / generated-header work). The `options.pb.cc.o` node
keeps an `inputs`-field diff (the evlog-generated `#include`s) — a separate,
deliberately-unmodeled concern that does not affect `cookie_cleaner.cpp.o`,
whose only divergence is the three `cmds` `-I` tokens.

## Files touched

- `modules.go`: add a typed `case tokCppEvlog` in `applyUnknownStmt` that
  appends `library/cpp/eventlog` (via `strLibraryCppEventlog`) to `d.peerdirs`,
  mirroring the `CPP_PROTO_PLUGIN0 DEPS` path.
- `gen.go`: drop `"CPP_EVLOG"` from `acknowledgedMacros` (now typed).
- `gen_test.go`: focused regression — a PROTO_LIBRARY with `CPP_EVLOG()` whose
  `library/cpp/eventlog` peer reaches a leaf `ADDINCL(GLOBAL leaf/include)`; a
  consumer PEERDIRing the proto must compile with `-I$(S)/leaf/include`. Fails
  before the change (eventlog not peered), passes after.

## Expected effect on the gate

- `cookie_cleaner.cpp.o` sg7 pair no longer reports brotli/re2/snappy as
  reference-only `cmds` tokens.
- sg2–sg6 unaffected: `CPP_EVLOG` does not appear in any of their source roots
  (yatool / ydb / monorepo/3) — verified by grep — so they stay byte-exact.
- No new `validate.py` failure; generation time unchanged (one extra peer edge
  on CPP_EVLOG proto modules, all in sg7 only).
