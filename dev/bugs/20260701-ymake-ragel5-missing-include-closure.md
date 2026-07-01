# Upstream ymake bug: ragel5 generation node omits the include closure that ragel6 carries

**Status:** confirmed against the reference graph. `ay` reproduces ymake byte-for-byte, so we intentionally match the buggy behavior (ragel5 = no closure).

## Summary

Both `.rl` (ragel5, via `ragel` + `rlgen-cd`) and `.rl6` (ragel6) are grammar → C++ codegen. The generated `.cpp` `#include`s ordinary C++ headers. ymake attaches the **full transitive include closure of the generated `.cpp`** as `inputs` of the **ragel6 generation node**, but attaches **nothing** (only the two tool binaries + the `.rl` source) to the **ragel5 generation node**.

This is inconsistent: the two codegen steps have the same shape, yet only ragel6's generation node lists the headers its output pulls in. For ragel5 the header dependencies are still tracked — but only on the downstream `.rl5.cpp` **compile** node (via that output's registered parsed includes), not on the generation node.

Whether the missing inputs are a real correctness problem for ymake (incremental rebuilds of the `.rl.tmp`/`.rl5.cpp` when a transitively-included header changes) is upstream's call; for `ay` the reference graph is the spec, so ragel5 stays closure-free.

## Evidence — raw reference-graph nodes (pre-normalization)

Source: `graph.fuse.json` for `yabs/server/daemons/bs_static`.

### ragel5 node — `$(BUILD_ROOT)/kernel/urlnorm/urlhashval.rl.tmp` (3 inputs, NO closure)

```json
{
 "cmds": [
  {"cmd_args": ["$(BUILD_ROOT)/contrib/tools/ragel5/ragel/ragel5", "-o", "$(BUILD_ROOT)/kernel/urlnorm/urlhashval.rl.tmp", "$(SOURCE_ROOT)/kernel/urlnorm/urlhashval.rl"], "env": {"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)"}},
  {"cmd_args": ["$(BUILD_ROOT)/contrib/tools/ragel5/rlgen-cd/rlgen-cd", "-T0", "-o", "$(BUILD_ROOT)/kernel/urlnorm/urlhashval.rl5.cpp", "$(BUILD_ROOT)/kernel/urlnorm/urlhashval.rl.tmp"], "env": {"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)"}}
 ],
 "deps": ["TljSAuLe-qD9Cauj32EXXQ", "BDz5JDBsUJ6UY9d-WdFcyA"],
 "env": {"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)"},
 "foreign_deps": {"tool": ["0ic2ljK3KGGtV5ziv_bBTQ", "8v49XpYluiGKegiaRvdxhg"]},
 "inputs": [
  "$(BUILD_ROOT)/contrib/tools/ragel5/ragel/ragel5",
  "$(BUILD_ROOT)/contrib/tools/ragel5/rlgen-cd/rlgen-cd",
  "$(SOURCE_ROOT)/kernel/urlnorm/urlhashval.rl"
 ],
 "kv": {"p": "R5", "pc": "yellow"},
 "outputs": ["$(BUILD_ROOT)/kernel/urlnorm/urlhashval.rl.tmp", "$(BUILD_ROOT)/kernel/urlnorm/urlhashval.rl5.cpp"],
 "platform": "default-linux-x86_64",
 "requirements": {"cpu": 1, "network": "restricted", "ram": 32},
 "sandboxing": true,
 "self_uid": "t-Bd0uZgH2lan_3xi4bOQg",
 "target_properties": {"module_dir": "kernel/urlnorm"},
 "uid": "v4a3VfZHYgJ0qGa_ahw4bw"
}
```

### ragel6 node — `$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp` (963 inputs, incl. 884-header closure)

Inputs trimmed for length — the two constant entries are the tool binary and the `.rl6` source; the remaining **884** are the transitive header closure of the generated `.cpp`.

```json
{
 "cmds": [
  {"cmd_args": ["$(BUILD_ROOT)/contrib/tools/ragel6/ragel6", "-CG2", "-L", "-I$(SOURCE_ROOT)", "-o", "$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp", "$(SOURCE_ROOT)/util/datetime/parser.rl6"], "env": {"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)"}}
 ],
 "deps": ["_CM4wPfAI3m2SZoh-YlBcQ"],
 "foreign_deps": {"tool": ["_CM4wPfAI3m2SZoh-YlBcQ"]},
 "host_platform": true,
 "inputs": [
  "$(BUILD_ROOT)/contrib/tools/ragel6/ragel6",
  "$(SOURCE_ROOT)/util/datetime/parser.rl6",
  "$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/include/__config_epilogue.h",
  "$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/include/__configuration/abi.h",
  "$(SOURCE_ROOT)/contrib/libs/cxxsupp/libcxx/include/__configuration/compiler.h",
  "... 881 more header inputs ..."
 ],
 "kv": {"p": "R6", "pc": "yellow"},
 "outputs": ["$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp"],
 "platform": "default-linux-x86_64",
 "requirements": {"cpu": 1, "network": "restricted", "ram": 32},
 "sandboxing": true,
 "self_uid": "9r_cul7rAaIQPZLTwDAywA",
 "tags": ["tool"],
 "target_properties": {"module_dir": "util"},
 "uid": "dmpOj0Dg75tPr7-Ck8ujgg"
}
```

## Implication for `ay`

- `emitLibraryRagel6Source` computes `walkClosure` of the generated `.cpp` and passes the source-only closure as ragel6 node inputs — matches ymake.
- `emitLibraryRagel5Source` must **not** compute a closure — the ragel5 node's inputs are exactly `[ragel5, rlgen-cd, <src>.rl]`. Adding the ragel6-style closure broke `devtools_ymake_bin` and drifted `bs_static` 20→34 divergent nodes.
- The header dependencies of the ragel5 output are still tracked, on the `.rl5.cpp` compile node, via the parsed includes registered in the codegen registry.
