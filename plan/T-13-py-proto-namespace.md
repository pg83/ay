# T-13 plan: transitive PROTO_NAMESPACE proto-include propagation into PY3_PROTO protoc commands

## Upstream mechanism

`build/conf/proto.conf`:

```
macro PROTO_ADDINCL(GLOBAL?"GLOBAL":"", Path, WITH_GEN?"BUILD":"") {
    _ORDER_ADDINCL($WITH_GEN $GLOBAL FOR proto ${ARCADIA_BUILD_ROOT}/$Path
                   SOURCE $GLOBAL FOR proto ${ARCADIA_ROOT}/$Path)
    ADDINCL($GLOBAL ${ARCADIA_BUILD_ROOT}/$Path)
}
macro PROTO_NAMESPACE(GLOBAL?"GLOBAL":"", WITH_GEN?"WITH_GEN":"", Namespace) {
    SET(PROTO_NAMESPACE $Namespace)
    PROTO_ADDINCL(GLOBAL $WITH_GEN $Namespace)
}
```

`PROTO_NAMESPACE(yt)` (no WITH_GEN) expands to a `GLOBAL FOR proto $(S)/yt`
addincl that rides the proto peer closure into `_PROTO__INCLUDE` of every
transitive consumer — the Python protoc command line included. The PY proto
cmdline (`_PY_PROTO_CMDLINE` / gen_py_protos wrapper) renders the same
`${pre=-I=:_PROTO__INCLUDE}` set, so a py3_proto consumer reaching a
PROTO_NAMESPACE(yt) declarer must carry `-I=$(S)/yt`.

## Divergence (reproduced from reference sg7.json)

Reference protoc include block for
`$(B)/ads/autobudget/protos/brandformance__intpy3___pb2.py`:

```
-I=./ -I=$(S)/ -I=$(B) -I=$(S)
-I=$(S)/contrib/libs/protobuf/src
-I=$(S)/yt                          <- missing in ours
-I=$(S)/contrib/libs/protoc/src
-I=$(B)
-I=$(S)/contrib/libs/protobuf/src
--python_out=$(B)/
```

The contributor chain is the same as the C++ case (T-9): ads/autobudget/protos
-> grut/libs/proto/public/metadata -> yt/yt_proto/yt/core (PROTO_NAMESPACE(yt)).
The `-I=$(S)/yt` namespace token sits AFTER the protobuf-src include and BEFORE
the NEED_GOOGLE_PROTO_PEERDIRS protoc-src include (protos_from_protoc is a
PY-only implicit peer added after the real proto peers in the closure order).

## Our defect

`emit_py_proto.go:newPyPBModuleEmission` assembles the py protoc `-I` block by
hand and never appends the collected transitive `protoNamespaceTail`
(non-GLOBAL PROTO_NAMESPACE source-root contributions, already collected in
gen.go through `ProtoNamespaceTail` / `peerContribs.protoNamespaceTail`). The
C++ side threads this through `composePBArgBlocks` (T-9); the PY side dropped it.

## Change

- Thread `peerContribs.protoNamespaceTail` into `newPyPBModuleEmission` and emit
  one `-I=$(S)/<ns>` per tail entry, positioned immediately after the
  `argISContribLibsProtobufSrc` include and before the
  `needGoogleProtoPeerdirs` protoc-src include — the reference order. Dedup each
  token against the already-rendered `mid` set (mirrors emit_pb.go) so a module
  whose own protoRoot already rendered the namespace does not duplicate it.
- No new transport: reuse the existing `ProtoNamespaceTail` carrier already
  computed for the C++ path. Scope limited to the protoc `-I` block.

Files touched: `emit_py_proto.go`, `emit_proto.go` (pass-through),
`emit_proto_test.go` (regression).

## Expected gate effect

- sg7: `ads/autobudget/protos/brandformance__intpy3___pb2.py` pair no longer
  reports `-I=$(S)/yt` as reference-only.
- sg2-sg6 stay byte-exact (no py3_proto module in those contours reaches a bare
  PROTO_NAMESPACE through the proto peer closure, or the reference carries the
  same token — confirmed by validate.py).

## Verification

1. `go test ./...` (new regression fails before, passes after).
2. `./dev/validate.py .out/digger-validate` — gating OK counts hold, no new
   failures, sg2-sg6 byte-exact.
3. sg7 pair check for brandformance__intpy3___pb2.py: `-I=$(S)/yt` no longer
   reference-only, no duplicate token.
