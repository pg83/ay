# DEBUG.md — graph diffing with `ay dump`

How to find and classify why our generated build graph differs from an
upstream reference (`sg*.json`), using the `ay dump` command family. This doc
is both a reference for those commands and a worked example: the actual
commands and (trimmed) output from classifying `ydb/apps/ydbd` (sg5) — our
graph vs the upstream `sg5.json` — and the conclusions drawn.

The graphs are large (sg5 ≈ 2.7 GB, ~26k nodes). Everything here streams; peak
RAM stays in the hundreds of MB.

---

## 1. The pipeline

A raw `-G` graph is one big JSON object (`{"conf":…, "graph":[…], …}`) with
non-semantic noise: upstream-specific uids, field order, `$(BUILD_ROOT)` vs
`$(B)`, sandbox-versioned resource roots, etc. Comparing two of them directly
is meaningless. The flow is:

```
raw graph ──(ay dump normalize)──▶ canonical JSONL ──(ay dump sort)──▶ sorted JSONL
                                                                          │
                                          two sorted JSONL files ─────────┤
                                                                          ▼
                                                          ay dump diff / ay dump grep
```

- **normalize** canonicalizes each node and emits **JSONL** — one node per
  line. `self_uid` becomes the node's *intrinsic content hash* (no deps);
  `uid` becomes the full Merkle hash (content + subtree). So a `self_uid`
  difference means the node's *own content* differs (root cause), not just
  cascade from a changed child.
- **sort** is a generic external-merge line sorter (bounded memory). After
  sorting, two semantically-equal graphs are byte-identical, so `cmp` is the
  acceptance check and ordering never matters for `diff`.
- **diff / grep** do the analysis.

### Produce two comparable graphs

```sh
# ours: generate the case graph, then normalize + sort
./dev/gen_sg5.sh .out/our.json
ay dump normalize --in .out/our.json --target ydb/apps/ydbd --out - | ay dump sort --out .out/our.jsonl

# reference: the upstream raw sg5.json, same treatment
ay dump normalize --in /home/pg/monorepo/ydb/sg5.json --target ydb/apps/ydbd --out - | ay dump sort --out .out/ref.jsonl
```

`--target` is the module whose closure to keep (the LD/AR/TS root). `--out -`
writes to stdout so it pipes into `sort`.

---

## 2. Command reference

### `ay dump normalize --in RAW --target DIR --out OUT`
Streams the raw graph, keeps the target closure, canonicalizes each node,
emits unsorted JSONL (`--out -` for stdout). Two passes over the file; parsing
and per-node work run in separate goroutines.

### `ay dump sort [--in F] [--out F] [--chunk-bytes N]`
External merge sort of lines (stdin/stdout by default). Bytewise order.
`--chunk-bytes` (default 256 MiB) bounds memory.

### `ay dump diff --left L --right R [--out F] [MODE]`
Compares two sorted JSONL graphs (`L`=ours, `R`=ref). Modes:

| mode | what it answers |
|------|-----------------|
| *(none)* | three lists: self_uids / outputs only on one side; outputs in both with a differing self_uid |
| `--summary` | the only-one-side outputs grouped by kind (`kv.p`) / ext / dir — **structural** gaps |
| `--by-field` | of the paired outputs, which content fields differ and how often (+ field combos) |
| `--by-kind` | content divergence per node kind: paired/divergent counts, per-field tallies |
| `--by-token` | rank `cmds`/`inputs`/`tags`/`outputs` tokens that are systematically only-ours / only-ref, by category (`-I`/`-D`/`-W`/`-f`/`-m`/path/`UNEXPANDED`/…) |
| `--roots` | leaf-most divergent outputs: content differs but every dependency child matches — the fix-first set |
| `--pair OUTPUT` | field-by-field diff of the single node producing `OUTPUT` |

### `ay dump grep --in G [--raw] [--substr|--regex] [KEYS…]`
Pretty-prints nodes matching a key. Default = exact match on `self_uid` or an
output path. `--substr` / `--regex` match against the *whole node* (so they
catch a flag or a `${VAR}` leak anywhere in `cmd_args`). Keys come from args or
stdin (so `diff` output pipes in). `--raw` reads a raw graph instead of JSONL.

---

## 3. Worked example: ours vs sg5

26197 nodes ours, 26259 ref; 26175 outputs present in both.

### Step 1 — structural gaps: what's missing / extra

```sh
ay dump diff --left .out/our.jsonl --right .out/ref.jsonl --summary
```
```
=== outputs only in LEFT (21) ===
  by kind:  6 AR  6 CC  5 PY  4 LD
  by ext:   10 .o  6 .a  3 .so  1 .pic.o  1 fix_elf
  by dir:   … 1 contrib/libs/libiconv  1 tools/fix_elf/fix_elf …
=== outputs only in RIGHT (248) ===
  by kind:  168 PB  27 CC  20 PY  9 EN  7 AR  5 CP  5 JV  2 CF …
  by ext:   168 .pb.h  40 .o  15 .cpp  8 .pic.o …
  by dir:   176 ydb/core/protos  22 yql/essentials/parser  14 ydb/public/api …
```
**Conclusion:** we *under-emit* 248 ref nodes, overwhelmingly **`.pb.h` proto
headers in `ydb/core/protos` (168 PB)** plus their enum-serialization (EN); and
we *over-emit* 21, mostly dynamic `.so` (libiconv) and the `fix_elf` tool.

### Step 2 — which fields drive the content divergence

```sh
ay dump diff --left .out/our.jsonl --right .out/ref.jsonl --by-field
```
```
=== by-field: 26175 outputs in both ===
[content field -> #nodes where it differs]
   22680 ( 86.6%)  cmds
   20287 ( 77.5%)  tags
    7514 ( 28.7%)  inputs
     433 (  1.7%)  host_platform
     339 (  1.3%)  outputs
      43 (  0.2%)  target_properties
[most common differing-field combinations]
   11333  cmds+tags
    5965  cmds+inputs+tags
    4637  cmds
    1343  tags
```
**Conclusion:** divergence is dominated by **cmds** and **tags**; `env`, `kv`,
`platform`, `requirements` never differ. So look at command args and tags.

### Step 3 — which node kinds, and how

```sh
ay dump diff --left .out/our.jsonl --right .out/ref.jsonl --by-kind
```
```
kind   paired diverge  top differing fields / combos
CC      21941   21941  cmds:21941 tags:17156 inputs:6084  [top combo: cmds+tags ×11248]
AR       1898    1773  tags:1759 inputs:1025 cmds:113 host_platform:97  [inputs+tags ×904]
PB        873     873  tags:873 cmds:381 outputs:335 host_platform:289  [tags ×352]
PY        750      74  tags:73 …                       (mostly matches)
EN        221     221  tags:221 inputs:93 …
LD         25      25  cmds:25 …
```
**Conclusion:** every CC compile diverges in `cmds` (100%); `tags` diverges
across nearly all kinds; PB nodes additionally differ in `outputs` (proto
output set). PY is essentially fine.

### Step 4 — name the exact tokens

```sh
ay dump diff --left .out/our.jsonl --right .out/ref.jsonl --by-token
```
```
[cmds tokens only in REF]  (token: #nodes, by category)
  totals:  31109 incl   22144 fflag   22070 warn   4232 path   845 def   564 march
   22062  [warn]  -Wno-unknown-argument
   22062  [fflag] -fno-omit-frame-pointer
    4241  [incl]  -I$(B)/yql/essentials/parser/proto_ast/gen/jsonpath/…
[cmds tokens only in OURS]
  totals:  3866 path   378 flag   377 UNEXPANDED   217 def   210 incl
     …     [UNEXPANDED] ${SSE41_CFLAGS}  ${AVX2_CFLAGS}  …_${ARCADIA_CURL_DNS_RESOLVER}
[inputs tokens only in OURS]
    4608  [path] $(S)/google/protobuf/any.pb.h
    4520  [path] $(S)/google/protobuf/timestamp.pb.h
[inputs tokens only in REF]
    4262  [path] $(S)/contrib/libs/protobuf/src/google/protobuf/struct.pb.h
    2977  [path] $(S)/ydb/core/control/lib/generated/control_board_proto.h.in
[tags tokens only in OURS]
   19951  [other] FAKEID=sandboxing
   19951  [other] SANDBOXING=yes
   19951  [other] debug
   19951  [other] default-linux-x86_64
[outputs tokens only in REF]
       2  [path] $(B)/ydb/core/protos/alloc.deps.pb.h …
```
**Conclusions:**
- **C1 tags (~19951 nodes):** we emit `FAKEID=sandboxing, SANDBOXING=yes,
  debug, default-linux-x86_64`; ref emits `[]`.
- **C2 cmds (~22062 CC):** we lack `-fno-omit-frame-pointer` and
  `-Wno-unknown-argument`.
- **C3 unexpanded `${VAR}` (~377):** literal `${SSE41_CFLAGS}…` (ref expands to
  `-msse4.1 -mavx2 …`, the `march` 564) and `${ARCADIA_CURL_DNS_RESOLVER}`.
- **C4 protobuf WKT path (~4500):** ours `$(S)/google/protobuf/*.pb.h`, ref
  `$(S)/contrib/libs/protobuf/src/google/protobuf/*.pb.h`.
- **C5/C6:** missing include closures (c-ares, libcxx `__format`, antlr) and
  `.in`-generated headers (`control_board_proto.h.in`) in inputs.
- **C7:** ref emits extra `.deps.pb.h` proto outputs.

### Step 5 — confirm a leak with grep

`--by-token` flagged `UNEXPANDED`. Find every node that leaks `${`:

```sh
ay dump grep --in .out/our.jsonl --substr '${' | grep -c '"self_uid"'   # → 482
ay dump grep --in .out/our.jsonl --substr '${SSE41_CFLAGS}' | head
```
```
        "${SSE41_CFLAGS}",
        "-DHAVE_SSE41",
        "${AVX2_CFLAGS}",
  "self_uid": "mgIGwMvZV7eSGddk_UQb0w",
```
**Conclusion:** 482 nodes carry unexpanded macro placeholders — a real macro-
expansion bug, not just a flag-order quirk.

### Step 6 — eyeball one node

```sh
ay dump diff --left .out/our.jsonl --right .out/ref.jsonl --pair '$(B)/build/cow/on/lib.c.o'
```
```
=== pair diff for $(B)/build/cow/on/lib.c.o ===
[field cmds differs]
  -ref  +-Wno-unknown-argument
  -ref  +-fno-omit-frame-pointer
[field tags differs]
  -ours +FAKEID=sandboxing
  -ours +SANDBOXING=yes
  -ours +debug
  -ours +default-linux-x86_64
```
**Conclusion:** a single representative node shows C1+C2 in isolation — exactly
the two pervasive causes, nothing else.

### Step 7 — the fix-first set

```sh
ay dump diff --left .out/our.jsonl --right .out/ref.jsonl --roots | head -1
# === roots: 14554 leaf-most divergent outputs (of 26398 divergent) ===
```
`--roots` lists outputs whose content differs but whose dependency children all
match — i.e. the divergence originates *here*. (To inspect them, pipe into
grep, skipping the two header lines:)

```sh
ay dump diff --left .out/our.jsonl --right .out/ref.jsonl --roots \
  | tail -n +3 | ay dump grep --in .out/ref.jsonl | head -60
```
While pervasive causes (C1/C2) are unfixed, almost everything looks like a
root; once they're fixed, re-running `--roots` and `--by-kind` collapses to the
genuine residual.

---

## 4. Final classification

| # | cause | scope | field | found by |
|---|-------|-------|-------|----------|
| A | structural: under-emit `ydb/core/protos` `.pb.h` (168) + EN; over-emit libiconv `.so`/`fix_elf` (21) | 248 / 21 nodes | — | `--summary` |
| C1 | spurious tags `FAKEID=sandboxing, SANDBOXING=yes, debug, default-linux-x86_64` (ref `[]`) | ~19951 | tags | `--by-token` |
| C2 | missing `-fno-omit-frame-pointer` + `-Wno-unknown-argument` | ~22062 CC | cmds | `--by-token` |
| C3 | unexpanded `${SSE*_CFLAGS}` / `${ARCADIA_CURL_DNS_RESOLVER}` | ~377 (482 w/ grep) | cmds | `--by-token` + `grep --substr` |
| C4 | protobuf WKT include root `google/…` vs `contrib/libs/protobuf/src/google/…` | ~4500 | inputs | `--by-token` |
| C5 | missing include closures (c-ares, libcxx `__format/__charconv/__chrono`, antlr) | large | inputs/cmds | `--by-token` |
| C6 | `.in`-generated headers (`control_board_proto.h.in`, …) absent from inputs | ~3k | inputs | `--by-token` |
| C7 | PB output set: ref emits extra `.deps.pb.h` | 335 + 168 | outputs | `--by-kind` / `--by-token` / `--summary` |
| C8 | host_platform / axis mismatch | ~430 (AR/PB/AS) | host_platform | `--by-kind` |

**Bottom line:** two trivial, pervasive causes (C1 tags, C2 two cflags) account
for ~77–84% of all content divergence. The remainder is a quality tail: macro
expansion (C3), include resolution (C4–C6), proto-codegen completeness (A/C7),
and host/target axis (C8). Fix C1+C2 first, then let `--roots` + `--by-kind`
surface the real residual.
