# Upstream ymake over-emit: COPY_FILE source leaks into the consumer AR node's inputs

**Status:** confirmed against the reference graph and traced to the ymake C++. `ay` reproduces ymake byte-for-byte, so we intentionally reproduce this over-emit (via `cpMemberSrcs`, threaded from `emitCopyFiles` into `emitARNode`'s inputs). Same family as `20260615-upstream-resource-objcopy-overemit.md`.

## Summary

When a module `COPY_FILE`s a prebuilt archive member into its build dir and then archives it, ymake lists **both** the copied output *and* the original copy **source** in the archive (AR/`link_lib`) node's `inputs`. The AR command reads only the copied output; the source is redundant. It is not added by any macro — it is swept in by ymake's transitive "union all inputs upward" pass (`FillFullInputs`/`GetNodeInputs`), which runs only in `--add-inputs` / strict-inputs / `SANDBOXING=yes` mode and is annotated in-tree as a "really big hammer."

## Trigger

`contrib/python/pydantic-core/ya.make` (`PY3_LIBRARY`):

```
COPY_FILE(a/x86_64-unknown-linux-gnu/release/lib_pydantic_core.a lib_pydantic_core.a)
```

- CP node: `$(SOURCE_ROOT)/contrib/python/pydantic-core/a/x86_64-unknown-linux-gnu/release/lib_pydantic_core.a` → `$(BUILD_ROOT)/contrib/python/pydantic-core/lib_pydantic_core.a`
- AR node (`link_lib.py` + `llvm-ar`): archives the copied `lib_pydantic_core.a` → `$(BUILD_ROOT)/contrib/python/pydantic-core/libpy3contrib-python-pydantic-core.a`

## Evidence — raw reference node (pre-normalization)

The AR node's `inputs` contain both the copied output and the source:

```json
{
 "cmds": [{"cmd_args": ["$(YMAKE_PYTHON3-...)/bin/python3", "$(SOURCE_ROOT)/build/scripts/link_lib.py",
   "$(CLANG-...)/bin/llvm-ar", "LLVM_AR", "gnu", "$(BUILD_ROOT)", "None", "--", "--",
   "$(BUILD_ROOT)/contrib/python/pydantic-core/libpy3contrib-python-pydantic-core.a",
   "$(BUILD_ROOT)/contrib/python/pydantic-core/lib_pydantic_core.a"], "env": {...}}],
 "inputs": [
  "$(BUILD_ROOT)/contrib/python/pydantic-core/lib_pydantic_core.a",                                 // copied output — archived
  "$(SOURCE_ROOT)/build/scripts/link_lib.py",
  "$(SOURCE_ROOT)/build/internal/scripts/gen_sbom.py",
  "$(SOURCE_ROOT)/build/scripts/fs_tools.py",
  "$(SOURCE_ROOT)/build/scripts/process_command_files.py",
  "$(SOURCE_ROOT)/contrib/python/pydantic-core/a/x86_64-unknown-linux-gnu/release/lib_pydantic_core.a", // <-- OVER-EMIT: the copy SOURCE
  "$(SOURCE_ROOT)/contrib/python/pydantic-core/.dist-info/METADATA",
  "$(SOURCE_ROOT)/contrib/python/pydantic-core/pydantic_core/py.typed",
  ... (objcopy.py, __init__.py, core_schema.py, _pydantic_core.pyi, wrapcc.py, gen_py3_reg.py) ...
 ],
 "kv": {"p": "AR", "pc": "light-red", "show_out": "yes"},
 "outputs": ["$(BUILD_ROOT)/contrib/python/pydantic-core/libpy3contrib-python-pydantic-core.a"],
 "self_uid": "Ut6EV5f-qFCs2xmHga2_sw", "uid": "HN5OOr8ppuPCTIF-f6kRMg"
}
```

Removing the copy source from the AR inputs in `ay` diverges from ref on exactly this one node (`devtools_ya_bin_2`): `[inputs only in REF] $(S)/…/release/lib_pydantic_core.a`; cmds/outputs/tags identical.

## Root cause (paths under /home/pg/monorepo/yatool)

The macros are correct — the source is **not** added to the module or the AR command anywhere:

- `_LIBRARY`/`PY3_LIBRARY` link via `LINK_LIB`: `build/ymake.core.conf:1910` (`.CMD=$LINK_LIB`, `.EXTS=.o .obj .a …`), `build/conf/linkers/ld.conf:384`. The archived member set is `$AUTO_INPUT`: `build/conf/linkers/ld.conf:372-373` (`_LD_TAIL_LINK_LIB=$AUTO_INPUT …`). `AUTO_INPUT` is ymake-computed (`devtools/ymake/vardefs.h:26`) from the module's non-explicit `EDT_BuildFrom` file edges.
- `_COPY_FILE_IMPL`: `build/ymake.core.conf:2798-2799` declares the source as `${input:FILE}` of the **CP** node and the copy as its output; it never touches the module.

The source rides along only through the transitive input-dump:

1. `${input:FILE}` becomes a `BuildFrom` of the **copy output** node — `devtools/ymake/macro_processor.cpp:1161` (`actionNode.AddDepIface(EDT_BuildFrom, …)`).
2. The copied **output** (not the source) is re-added as a module `SRCS`/input — `devtools/ymake/module_builder.cpp:1184-1188` (`QueueCommandOutputs` → `AddSource` → `AddDep(..., EDT_BuildFrom)` at `:246-262`). This is the legitimate archived member.
3. Regular JSON inputs exclude the source — `FillRegularInputs` (`devtools/ymake/export_json.cpp:269-303`) uses `NodeDeps` + explicit direct edges only; a source with no build command is gated out of `NodeDeps` at `devtools/ymake/json_visitor.cpp:624` (`CurrData->HasBuildCmd || OutTogetherDependency`). In a plain `ya make` graph the AR inputs = {copy output} only.
4. **The over-emit**: with `DumpInputsInJSON` on, `FillFullInputs` (`devtools/ymake/export_json.cpp:216-234`, marked `// FIXME(spreis): This is really big hammer`) pushes `GetNodeInputs(node)` — a transitive union across `BuildFrom` edges (`devtools/ymake/json_visitor.cpp:472-473` seed, `:788-789` union into parent), gated by `NeedToPassInputs` which stops only at `Program`/`Library` peer boundaries (`:96-102`). Chain: source File (seeds itself) → CP output (unions source) → AR node (unions {copy output, source}).
5. Activation: `DumpInputsInJSON` default false (`devtools/ymake/options/commandline_options.h:25`; `--add-inputs`/`-N` sets it, `…/commandline_options.cpp:20`). `ya` passes `--add-inputs` when `strict_inputs` (`devtools/ya/build/ymake2/__init__.py:376-378`), and `strict_inputs = opts.strict_inputs or SANDBOXING=='yes'` (`devtools/ya/build/graph.py:2029-2030`).

## Assessment

Genuine over-approximation, conditional on `--add-inputs` (strict-inputs / sandboxing):

- `link_lib.py`/`llvm-ar` never opens the copy source; it only reads the copied output. The source content already reaches the AR via the module → CP-output → source edge chain, so rebuild correctness does not depend on this extra input.
- The source is not added for the AR node by intent; it is swept in by the blunt transitive union that over-declares every transitively reachable file for the sandbox.
- Net effect: bloats the AR node's declared input set and perturbs its strict-inputs fingerprint on a file that cannot change the AR output. Redundant, not load-bearing.
- The fix, if wanted upstream, belongs at the `NeedToPassInputs` / `FillFullInputs` boundary (treat a pure copy output as an input leaf; don't propagate a copied output's own `BuildFrom` sources into consumers), not in `COPY_FILE`/`LINK_LIB`.

## Reproduction in `ay`

`emitCopyFiles` records each COPY_FILE archive member's **source** VFS; `genModule` threads it (`cpMemberSrcs`) into `emitARNode`'s `extraInputs`, so our AR node carries the same redundant source input and stays byte-exact with ymake.
