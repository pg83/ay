# T-7 plan: model DECIMAL_MD5_LOWER_32_BITS as a generated-source producer

(Note: plan/T-7.md is pre-existing unrelated trunk content (BUNDLE); this
ticket's plan uses a distinct filename to avoid clobbering it.)

## Upstream mechanism (verified)

`build/ymake.core.conf:4236-4244`:

```conf
DECIMAL_MD5_SCRIPT=build/scripts/decimal_md5.py
DECIMAL_MD5_FIXED=

macro DECIMAL_MD5_LOWER_32_BITS(File, FUNCNAME="", Opts...) {
    .CMD=$YMAKE_PYTHON3 ${input:DECIMAL_MD5_SCRIPT} --fixed-output=${DECIMAL_MD5_FIXED} \
        --func-name=${FUNCNAME} --lower-bits 32 --source-root=$ARCADIA_ROOT \
        ${input=TEXT:Opts} ${stdout;output:File} \
        ${hide;kv:"p SV"} ${hide;kv:"pc yellow"} ${hide;kv:"show_out"}
}
```

The macro produces a build-root `.cpp` via `${stdout;output:File}`, marked
`kv.p=SV pc=yellow show_out`. The output carries a `.cpp` extension and no
`noauto` modifier, so ymake captures it as a module `AUTO_INPUT` and compiles it
in the same module; the resulting `.o` joins the library archive.

Verified against `sg7.json`, `adfox/atlas/atlas`:

- SV node `$(B)/adfox/atlas/atlas/atlas_hash.auto.cpp`: kv `{p:SV,pc:yellow,
  show_out:yes}`, env `ARCADIA_ROOT_DISTBUILD=$(S)`, requirements
  `{cpu:1,network:restricted,ram:32}`, no deps, 399 inputs (398 resolved Opts +
  `$(S)/build/scripts/decimal_md5.py`). cmd = `python3 decimal_md5.py
  --fixed-output= --func-name=get_atlas_sources_hash --lower-bits 32
  --source-root=$(S) <398 opt VFSs>`.
- CC node `$(B)/.../atlas_hash.auto.cpp.o`: 401 inputs (gen src + 398 opts +
  decimal_md5.py + wrapcc.py), 1 dep (the SV producer).
- AR `libadfox-atlas-atlas.a`: `atlas_hash.auto.cpp.o` is the last member,
  after the direct module-dir compiles (generated-source placement).

ay today lists `DECIMAL_MD5_LOWER_32_BITS` in `acknowledgedMacros`, so the macro
is ignored: no SV node, no registration, no CC, no archive member.

## Files to touch

- `node_attrs.go`: add `pkSV` ProcKind + `"SV"` string.
- `vfs_consts.go`: add `decimalMD5PyVFS = source("build/scripts/decimal_md5.py")`.
- `modules.go`: add `DecimalMD5Lower32BitsStmt` type + `decimalMD5
  []*DecimalMD5Lower32BitsStmt` field on `ModuleData`; parse it in a typed
  `case tokDecimalMd5Lower32Bits:` in `applyUnknownStmt` (args already expanded).
- `gen.go`: drop `"DECIMAL_MD5_LOWER_32_BITS"` from `acknowledgedMacros`; call
  `emitDecimalMD5ForAR` next to `emitRunProgramsForAR` and wire its CC results
  through the existing generated-source `genCC` path.
- `emit_decimal_md5.go` (new): emit the SV producer + downstream CC.
- `emit_decimal_md5_test.go` (new): regression fixture.

## Emitter behavior

`emitDecimalMD5ForAR(ctx, instance, d, in)` for each stmt:

1. `outVFS = copyFileOutputVFS(modulePath, File)`.
2. For each Opt: `copyFileInputVFS(ctx.fs, modulePath, opt)` → optVFSs.
3. SV cmd = `[d.tc.Python3, decimalMD5PyVFS, --fixed-output=,
   --func-name=<FuncName>, --lower-bits, 32, --source-root=$(S), optVFSs...]`.
4. SV inputs = optVFSs + decimalMD5PyVFS. KV `{pkSV,pcYellow,ShowOut}`, env
   `ARCADIA_ROOT_DISTBUILD=$(S)`, requirements cpu1/restricted/ram32,
   Resources usesPython3, no deps.
5. Reserve ref, emit, register output (`registerBoundGeneratedParsedOutput`,
   nil parsed includes), `setSourceInputs` + `addClosureLeaf` for each optVFS and
   decimalMD5PyVFS — the established codegen vehicle (emit_pr/emit_cf), so the
   downstream CC's `walkClosure` lists the opts + script (401 inputs).
6. If `isCCSourceExt(File)`: `emitCodegenDownstreamCC(ctx, instance, File,
   []NodeRef{svRef}, in)` → return CC ref/out for the genCC archive path.

Scope limited to DECIMAL_MD5_LOWER_32_BITS; no AR member-order patching, no
generalization to other macros.

## Test-first

Fixture `LIBRARY()` with `SET(HASH_INPUTS data.txt helper.hpp)`,
`DECIMAL_MD5_LOWER_32_BITS(hash.auto.cpp FUNCNAME get_hash ${HASH_INPUTS})`,
`SRCS(main.cpp)`. Assert: one SV node out `$(B)/mod/hash.auto.cpp` with the
command flags + resolved inputs + decimal_md5.py; a CC node compiling it to
`.o` depending on the SV node; archive cmd + inputs include the `.o`. Fails
before the fix (macro ignored).

## Validation

`go test ./...`, then `./dev/validate.py .out/digger-validate` must PASS without
dropping sg2/sg3/sg4 OK counts, growing XFAIL, or lowering sg5 matched. Focused
sg7 atlas pair must stop reporting atlas_hash.auto.cpp / .o as reference-only.
