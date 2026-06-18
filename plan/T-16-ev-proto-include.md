# T-16 plan: propagate peer proto includes into EV (CPP_EV_CMDLINE) protoc commands

## Upstream mechanism

`build/ymake.core.conf`:

```
_CPP_PROTO_CMDLINE_BASE=${cwd;rootdir;input:File} $PROTOC -I=./$PROTO_NAMESPACE \
    -I=$ARCADIA_ROOT/$PROTO_NAMESPACE ${pre=-I=:_PROTO__INCLUDE} \
    -I=$ARCADIA_BUILD_ROOT -I=$PROTOBUF_INCLUDE_PATH --cpp_out=... ...
CPP_PROTO_CMDLINE=$_CPP_PROTO_WRAPPER_BASE --outputs $CPP_PROTO_OUTS -- $_CPP_PROTO_CMDLINE_BASE
CPP_EV_CMDLINE =$_CPP_PROTO_WRAPPER_BASE --outputs $CPP_EV_OUTS   -- $_CPP_PROTO_CMDLINE_BASE
```

`CPP_EV_CMDLINE` reuses the SAME `_CPP_PROTO_CMDLINE_BASE` as the C++
`PROTO_LIBRARY` PB command (T-9 / T-7). That base contains
`${pre=-I=:_PROTO__INCLUDE}` — the transitive proto include closure
(`PROTO_NAMESPACE` / `PROTO_ADDINCL` contributions from peers). `_CPP_EVLOG_CMD`
appends only `$CPP_EV_OPTS` (the event2cpp plugin + `-I=$ARCADIA_ROOT/library/cpp/eventlog`)
after the base; the include block itself is shared with the PB path.

Our `emit_ev.go` builds the protoc command from a fixed `evProtocConstArgs`
span and never inserts the `_PROTO__INCLUDE` peer block, so EV protoc commands
lack the peer proto includes that the PB path already emits.

## Reference evidence

`$(B)/extsearch/goods/plutometa/proto/events.ev.pb.cc` reference command
(`/home/pg/monorepo/4/sg7.json`), normalized:

```
-I=./ -I=$(S)/ -I=$(B) -I=$(S) -I=$(S)/contrib/libs/protobuf/src
-I=$(S)/yt -I=$(S)/contrib/libs/googleapis-common-protos
-I=$(S)/taxi/schemas/schemas/proto
-I=$(S)/market/ad_performance/product_vertical -I=$(S)/maps/doc/proto
-I=$(B) -I=$(S)/contrib/libs/protobuf/src --cpp_out=:$(B)/ ...
```

The peer block (`yt`, `googleapis-common-protos`, `taxi/schemas`, `market`,
`maps/doc`) sits between the FIRST `-I=$(S)/contrib/libs/protobuf/src` and the
trailing `-I=$(B) -I=$(S)/contrib/libs/protobuf/src` pair — i.e. exactly where
`${pre=-I=:_PROTO__INCLUDE}` expands relative to the EV const span.

## Code area

- `emit_ev.go`: `evProtocConstArgs`, `emitEV`, `emitLibraryEvSource`.
- `emit_proto.go`: `emitCPPProtoSrcs` EV arm (the cpp_proto path that produces
  `events.ev.pb.cc`).

Both EV call sites already have the peer proto include set in hand:
`emitCPPProtoSrcs` via `peerContribs.protoAddIncl` / `protoNamespaceTail`;
`emitLibraryEvSource` via `in.PeerProtoAddInclGlobal` / `in.ProtoNamespaceTail`
(the same values the PB path threads into `composePBArgBlocks`).

## Change

1. Split `evProtocConstArgs` into a head span (`-I=./ -I=$(S)/ -I=$(B) -I=$(S)
   -I=$(S)/contrib/libs/protobuf/src`) and a tail span (`-I=$(B)
   -I=$(S)/contrib/libs/protobuf/src --cpp_out=:$(B)/ --cpp_styleguide_out=:$(B)/`).
2. Add `peerProtoAddIncl []VFS, protoNamespaceTail []VFS` params to `emitEV`;
   build the `-I=<p>` peer block with the SAME order + dedup semantics the PB
   path uses (peer addincl first, then bare-namespace tail, skipping tokens
   already present), and splice it between head and tail.
3. Thread the peer sets from both EV call sites.

No new data path: the values are the exact ones already computed for the PB
command. This only routes them into the EV protoc include block.

## Verification

- New regression in `emit_pb_test.go`-style: PROTO_LIBRARY consumer with a
  `.ev` source PEERDIR-reaching a `PROTO_NAMESPACE(yt)` provider; assert the EV
  protoc command contains `-I=$(S)/yt` in the include block (before
  `--cpp_out`), no duplicate, and no `-I$(S)/...` C++ leakage. Must fail before
  the fix.
- `go test ./...`.
- `./dev/validate.py .out/digger-validate`: sg2..sg6 byte-exact, sg7 pair for
  `events.ev.pb.cc` no longer reports the peer proto include block as
  reference-only.
