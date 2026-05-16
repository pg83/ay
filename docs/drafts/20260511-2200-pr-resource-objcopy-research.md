# PR-M3-resource-objcopy — research report (implementation deferred)

Date: 2026-05-11.  Author: Claude executor agent (worktree `agent-aba6d88709be7a3b2`).
Replaces: implementation phase of PR-M3-resource-objcopy as briefed.
Successor PR: see "Recommended next steps" below.

## Status

**Implementation aborted** before any code change in this worktree.

Reason: the 127-node REF cluster is **not** dominated by direct `RESOURCE()`
macro invocations as the original probe (`20260511-2100-unpaired-got-cluster.md`)
implied.  Only 1 of 127 nodes (the `certs/objcopy_*.o` entry) maps to a
syntactic `RESOURCE(path key)` pair.  The remaining 126 are emitted by
`build/plugins/pybuild.py:onpy_srcs` and a fan-out from `build/plugins/res.py`,
flowing through the same `TObjCopyResourcePacker` packer that handles direct
`RESOURCE()` calls.  Implementing a faithful emitter therefore requires:

  - reverse-engineering the `pybuild.py` PY_SRCS → `resfs/file/...` /
    `resfs/src/...` expansion path (paths + keys + kvs);
  - reverse-engineering the `py/namespace/<md5(sorted_modlist)>/...` kvs
    cluster (7 nodes);
  - implementing the cmd-line-length chunking (`MAX_CMD_LEN = 8000`,
    `EstimatedCmdLen_` accumulator) that splits a single PY_SRCS into
    multiple objcopy nodes;
  - threading the resulting `objcopy_<26hex>.o` outputs into the owning
    module's `.global.a` archive AR (`AR` node mass already emitted, but
    its `srcs[]` must include these new outputs in a deterministic order);
  - the hash formula closes the output-naming question (derived below) but
    does **not** close the inputs/keys/kvs question.

Within a 60-minute budget this scope cannot deliver +1.0pp metric lift
without high risk of regressing existing AR/AR.global.a inputs lists
(L2/L3 byte-equality).  Hash derivation succeeded in 15 min; the remaining
plumbing is several engineering days.  Per the brief's contingency
("If the hash derivation fails within 15 min, ABORT … return a research
report"), this report extends the contingency to "If the broader scope
cannot land in the budget" and ships findings instead of half-emitted code.

## Hash formula — derived and verified

Source: `yatool_orig/devtools/ymake/plugins/resource_handler/packer.h:73-85`.

```
GetHashForOutput(list):
    list.append("$S/" + UnitPath())          # unit_path prefixed with "$S/"
    Sort(list)
    stringify = ",".join(list) + MODULE_TAG  # MODULE_TAG ∈ {"", "RESOURCE_LIB", ...}
    return MD5(stringify).hexdigest()[:LEN_LIMIT].lower()
```

Where `LEN_LIMIT = 26` (not 24 as documented in
`20260511-2100-unpaired-got-cluster.md` §4 — the doc states "24-hex"; REF
samples confirm 26).

The `list` argument consists of, in append order before sort:

  - all `--inputs` paths as they appear in the cmd (relative form, NOT
    `$(SOURCE_ROOT)/`-prefixed; e.g. `cacert.pem`);
  - all `--keys` values (already base64-encoded; e.g.
    `L2J1aWx0aW4vY2FjZXJ0`);
  - all `--kvs` values (raw `key=value` strings).

The unit path is appended **last** and is `"$S/" + module_dir`.  Module
tag is empty for plain `LIBRARY` / `PY3_LIBRARY`; non-empty values include
`RESOURCE_LIB`, `STATIC_LIB`, etc.

### Verified example (certs)

```
ya.make: LIBRARY() RESOURCE(cacert.pem /builtin/cacert) END()
```

  - paths = `["cacert.pem"]`
  - keys (b64) = `["L2J1aWx0aW4vY2FjZXJ0"]`
  - kvs = `[]`
  - unit_path = `"$S/certs"`
  - module_tag = `""`
  - sorted list = `["$S/certs", "L2J1aWx0aW4vY2FjZXJ0", "cacert.pem"]`
  - stringify = `"$S/certs,L2J1aWx0aW4vY2FjZXJ0,cacert.pem"`
  - md5 = `c27c99b2d9d5eade92fd72d0aa1d4e51`, hex[:26] = `c27c99b2d9d5eade92fd72d0aa`

Matches REF: `$(BUILD_ROOT)/certs/objcopy_c27c99b2d9d5eade92fd72d0aa.o`.

## Cluster shape inventory (sg2.json, all 127 nodes)

| shape | nodes | example |
|---|---:|---|
| simple (`--inputs` source paths + `--keys`, no `--kvs`) | 1 | `certs` |
| kv_only (no `--inputs`, only `--kvs`) | 7 | `py/namespace/<md5>/<module_dir>=<dotted_name>.` |
| build_inputs (`--inputs` contain `$(BUILD_ROOT)/...yapyc3` paths) | 41 | `contrib/tools/python3/Lib` |
| mixed (source + key + kvs) | 78 | `contrib/tools/python3/lib2/py`, `library/python/runtime_py3/entry_points.py`, etc. |

### Per-module breakdown

| module_dir | count |
|---|---:|
| `certs` | 1 |
| `contrib/tools/python3/Lib` | 40 |
| `contrib/tools/python3/lib2/py` | 76 |
| `devtools/ymake/contrib/python-rapidjson` | 1 |
| `library/python/runtime_py3` | 4 |
| `library/python/symbols/module` | 2 |
| `tools/py3cc/slow` | 3 |

### Sampled REF entries (full cmd_args, abbreviated)

#### A. `certs/objcopy_c27c99b2d9d5eade92fd72d0aa.o` (simple)
```
cmd: build/scripts/objcopy.py --compiler clang++ --objcopy llvm-objcopy
     --compressor $(BUILD_ROOT)/tools/rescompressor/rescompressor
     --rescompiler $(BUILD_ROOT)/tools/rescompiler/rescompiler
     --output_obj $(BUILD_ROOT)/certs/objcopy_c27c99b2d9d5eade92fd72d0aa.o
     --target aarch64-linux-gnu
     --inputs $(SOURCE_ROOT)/certs/cacert.pem
     --keys   L2J1aWx0aW4vY2FjZXJ0
```
- `kv = {p:PY, pc:yellow, show_out:yes}`, `target_properties = {module_dir: certs}`.
- inputs[] = `[rescompiler, rescompressor, build/scripts/objcopy.py, certs/cacert.pem]`.
- platform = `default-linux-aarch64`.

#### B. `library/python/runtime_py3/objcopy_3b0561f75631281b973aa8b64e.o` (kv_only — namespace)
```
--kvs py/namespace/bd17cfe3d9af11d01ff7b15ebc3786a7/library/python/runtime_py3=library.python.runtime_py3.
```
- No `--inputs`, no `--keys`.  Single `--kvs` is a namespace registration.
- The md5 prefix `bd17cfe3…` is `md5(joined_module_names)` per
  `pybuild.py:587-594`.  Module list = the PY_SRCS module-name list for
  `library/python/runtime_py3`.

#### C. `tools/py3cc/slow/objcopy_4b1c18d0dc6973976969ad23be.o` (kv_only — PY_MAIN)
```
--kvs PY_MAIN=tools.py3cc.slow.main:main
```

#### D. `contrib/tools/python3/Lib/objcopy_0299ac47a84f85e85182c986c0.o` (build_inputs)
```
--inputs $(BUILD_ROOT)/contrib/tools/python3/Lib/multiprocessing/popen_fork.py.3kp2.yapyc3
         $(BUILD_ROOT)/contrib/tools/python3/Lib/multiprocessing/popen_forkserver.py.3kp2.yapyc3
         ... (12 yapyc3 outputs, chunked) ...
--keys   cmVzZnMvZmlsZS9weS9tdWx0aXByb2Nlc3NpbmcvcG9wZW5fZm9yay5weS55YXB5YzM=
         ... (one per input, b64 of `resfs/file/py/<dotted-mod>.py.yapyc3`) ...
--kvs    resfs/src/resfs/file/py/multiprocessing/popen_fork.py.yapyc3=contrib/tools/python3/Lib/multiprocessing/popen_fork.py.3kp2.yapyc3
         ... (one per input, mapping resfs/src to the real BUILD_ROOT path) ...
```
- Chunked: 40 nodes for python3/Lib, each carrying ~12 inputs.  `EstimatedCmdLen_`
  flushes near `MAX_CMD_LEN = 8000` (`objcopy.h:32`,
  `packer.h:98`).  `ROOT_CMD_LEN = 200` baseline.

#### E. `devtools/ymake/contrib/python-rapidjson/objcopy_55c44b1fdbfda511798cd895e2.o` (mixed)
- 4 inputs (METADATA, top_level.txt, license.txt — RESOURCE_FILES outputs).
- 4 keys (b64 of `resfs/file/<full-path>`).
- 4 kvs (`resfs/src/resfs/file/<full-path>=<full-path>`).
- This is the `RESOURCE_FILES(...)` shape from `build/plugins/res.py:67`.

## Upstream call graph (canonical citations)

  - **Macro definition**: `build/ymake.core.conf:522` defines `RESOURCE(Args...)`
    as a passthrough that only does `PEERDIR(library/cpp/resource)`.  Actual
    work is in the plugin handler.
  - **Plugin handler**: `devtools/ymake/plugins/resource_handler/impl.cpp:17-92`
    parses pairs and dispatches to packers.  `objCopy` flag gates
    `TObjCopyResourcePacker::CanHandle` (rejects `${ARCADIA_BUILD_ROOT}`,
    `${ARCADIA_SOURCE_ROOT}`, `conftest.py` substrings).
  - **Packer**: `devtools/ymake/plugins/resource_handler/objcopy.h:10-127`.
    `HandleResource` accumulates into `Objects_.{paths, keys, kvs}` and
    increments `EstimatedCmdLen_`.  `Finalize(force)` flushes when len
    exceeds `MAX_CMD_LEN` or `force=true`.
  - **Hash**: `devtools/ymake/plugins/resource_handler/packer.h:73-85`.
  - **PY_SRCS bridge**: `build/plugins/pybuild.py:558-617` populates `res`
    list and calls `unit.onresource(['DONT_COMPRESS'] + ...)`
    (line 585, 594) and `unit.onresource_files(...)` (line 597).
  - **RESOURCE_FILES bridge**: `build/plugins/res.py:12-72` expands
    `[DEST | PREFIX | STRIP] path` into `RESOURCE(DONT_PARSE … path key
    - resfs/src/key=rootrel_arc_src(path))`.
  - **Cmd_args template**: `RUN_PYTHON3` in `build/ymake.core.conf:4812-4815`
    sets `${hide;kv:"p PY"} ${hide;kv:"pc yellow"} ${hide;kv:"show_out"}`,
    explaining why all objcopy nodes have `kv.p = PY` and `show_out = yes`.
  - **--target value**: `objcopy.h:108-124` — extracted from `C_FLAGS_PLATFORM`
    by parsing `--target=...` substring; aarch64 → `aarch64-linux-gnu`,
    x86_64 → `x86_64-linux-gnu`.

## Hypotheses requiring user confirmation before implementation

1. **`unit_path` form in hash**.  Verified for `certs` (`"$S/certs"`).
   Likely identical for all module_dirs (consistent with
   `Unit_.UnitPath()` returning the `$S/...`-rooted module path), but
   should be re-verified against one more sampled REF entry from a
   nested path (e.g. `library/python/runtime_py3` with kvs).

2. **`MODULE_TAG` value per module type**.  `RESOURCE_LIB` is set for
   `GEN_LIBRARY` (`ymake.core.conf:598`).  For plain `LIBRARY` /
   `PY3_LIBRARY` / `PY3_PROGRAM` the tag is empty (verified for `certs`).
   `library/python/symbols/module` is multi-module and may have a
   non-empty tag for the `py3_native_global` sub-module — verify before
   emitting.

3. **`py/namespace/<md5>` kv hash input**.  `pybuild.py:560` constructs
   `mod_list_md5` by feeding each `mod` (dotted module name, e.g.
   `entry_points`) UTF-8 encoded into a streaming md5; key format is
   `py/namespace/<md5_hex>/<unit_path>=<dotted_namespace>.` (trailing
   dot).  Should be straightforward to reproduce from
   the existing `d.pySrcs` list, but the ordering must match — likely
   the order PY_SRCS arguments are seen (after `TOP_LEVEL` /
   `NAMESPACE` modifier stripping).

4. **Cmd-length chunking determinism**.  `EstimatedCmdLen_` accumulates
   `ROOT_CMD_LEN (200) + path.len() + key.len()` per entry, flushes at
   `MAX_CMD_LEN (8000)`.  For 1:1 reproducibility we must compute the
   identical cumulative count and partition identically.

5. **AR aggregation**.  Each objcopy_*.o output is a global object that
   feeds into the module's `lib<name>.global.a`.  Verify the
   ordering: REF likely appends them in flush-order
   (first flush before later flushes); our existing AR emitter would
   need a new `globalSrcsExtra []string` field on `moduleData`.

## Recommended next steps (split into 3 PRs)

### PR-M3-resource-objcopy-A: simple RESOURCE (1 + 1 = 2 nodes)

Scope: `certs/objcopy_*.o` + `devtools/ymake/contrib/python-rapidjson/objcopy_*.o`
(the latter is RESOURCE_FILES-driven but produces only one flushed group).

  - Parser: capture `RESOURCE(path1 key1 ...)` and `RESOURCE_FILES(...)`
    statements into `moduleData.resources []ResourceEntry` and
    `moduleData.resourceFiles []ResourceFilesEntry`.
  - Emitter: one objcopy node per flushed group, with the hash formula
    above, kv = `{p:PY, pc:yellow, show_out:yes}`, full cmd_args matching
    sampled REF.
  - AR integration: append the objcopy output paths to the module's
    `.global.a` `srcs[]` in flush order.

Expected metric lift: +0.02 pp (2/8750).  **Validation gate:**
M2 byte-exact must not regress (M2 ref has 0 objcopy nodes;
implementation must therefore guard with "only emit when at least one
RESOURCE / RESOURCE_FILES entry is parsed in the module").

### PR-M3-resource-objcopy-B: PY_SRCS kv_only namespaces (7 nodes)

Scope: `py/namespace/<md5>/...` and `PY_MAIN=...` kv_only nodes.

  - Compute `md5(joined_module_names)` from `d.pySrcs`.
  - Emit one objcopy node per PY3_LIBRARY / PY3_PROGRAM_BIN with
    `--kvs py/namespace/<md5>/<unit_path>=<dotted>.`.
  - PY_MAIN: requires a new `pyMain string` field on `moduleData`
    populated from `PY_MAIN(module:func)`.

Expected metric lift: +0.08 pp (7/8750).

### PR-M3-resource-objcopy-C: PY_SRCS resfs embeddings (78 + 41 = 119 nodes)

Scope: the lion's share — `contrib/tools/python3/Lib` (40),
`contrib/tools/python3/lib2/py` (76), `library/python/symbols/module` (2),
`tools/py3cc/slow` (1).

  - For each `(srcRel, dottedMod)` PY_SRCS entry, produce:
    - input path = `$(BUILD_ROOT)/<unit_path>/<srcRel>.{3kp2,}.yapyc3`
      (build_inputs mode, requires the yapyc3 nodes from existing
      `emitPySrcs` to be emitted **first**).
    - key (b64) = `b64encode("resfs/file/py/" + dotted_with_slashes + ".py.yapyc3")`.
    - kv = `resfs/src/resfs/file/py/<dotted_with_slashes>.py.yapyc3=<unit_path>/<srcRel>.<.3kp2 if subdir else "">.yapyc3`.
  - Chunk by accumulated `200 + path.len + key.len` ≤ 8000.
  - For `contrib/tools/python3/lib2/py` specifically: `ENABLE(PYBUILD_NO_PY)`
    is set, so yapyc3 nodes are suppressed (already handled by
    `d.pyBuildNoPYC` in `gen.go:3570`); the resource embeds the raw `.py`
    source paths from `$(SOURCE_ROOT)/...` instead.  Need a different
    code path (mixed shape, sourceroot inputs + resfs kvs).

Expected metric lift: +1.36 pp (119/8750), modulo AR-srcs and
PY-input-list byte-equality.

## Files implementing this would touch

  - `yamake.go` — RESOURCE / RESOURCE_FILES parser; new
    `ResourceStmt` AST node.
  - `gen.go` — `moduleData.resources`, `moduleData.resourceFiles`,
    `moduleData.pyMain` (new fields); call site after `emitPySrcs`.
  - **New file** `resource.go` — `emitResourceObjcopy(ctx, instance, d)`
    with hash function, chunker, packer (mirrors `TObjCopyResourcePacker`).
  - `ar.go` — extend `EmitAR` to accept extra global members from the
    objcopy emitter.
  - `gen.go:424` — remove RESOURCE/RESOURCE_FILES "deferred" comments.
  - `py.go:19-21` — remove the deferral comment when scope C lands.

## Acceptance gates (per PR)

For any PR landing under this banner:

  - M1 sha = `bf30a6676a660ce3b416edc2ea1c2956bbf93ae1f94f0f773d2a62e4c77ca7f6` (no regression).
  - M2 sha = `c1c750215f476d121a5815096cd06353a79d9942ab1565298fde5d8d9c0898df` (no regression).
  - `./yatool make -j 0 -G tools/archiver` wall time ≤ 5s.
  - M3 pairs strictly non-decreasing; per-level L0/L1/L2/L3 strictly
    non-decreasing.

## What this report does NOT cover

  - Whether the objcopy emitter must run on **target** or **host**
    platform.  REF entries have `platform = default-linux-{aarch64,x86_64}`
    (target axis).  But cmd_args[0] = `/ix/realm/pg/bin/python3` (host
    interpreter path) — same shape as our existing yapyc3 nodes.  Likely
    target-platform with host-tool deps, consistent with `emitPySrcs`.
  - The exact `requirements` block.  Sampled REF: `{cpu:1, network:restricted, ram:32}`.
  - The `env` block.  Sampled REF: `{ARCADIA_ROOT_DISTBUILD: $(SOURCE_ROOT)}`.
    Same as our existing yapyc3 nodes.
  - Whether `--arch_32_bits` or `--large_resource_thr` are needed.  All
    sampled REF entries omit them.

## Closing

Hash formula is fully derived and verified.  Implementation requires
~1-2 engineer-days split across three PRs.  The 60-minute single-agent
budget is insufficient.  Returning research instead of code preserves
M2-sha and avoids a half-emitted node-set that would corrupt AR srcs[].
