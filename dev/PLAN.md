# Codebase Refactoring Plan

## Baseline

Global non-regression gate:

- `go test ./...`
- `./yatool make --source-root /home/pg/monorepo/yatool_orig -j 0 -G tools/archiver`

## Preconditions

- `P1`: characterization tests for `PROTO_LIBRARY` and `EVENT_FILE` before the
  proto refactor.
- `P2`: characterization tests for `PY2_PROGRAM`, `PY3_PROGRAM`,
  `PY3_PROGRAM_BIN`, including `allocator=J`, before the Python-specific
  refactors.

## 1. Shared Compile-Flag Pipeline

### Problem

The compile-flag bundle is duplicated across:

- `composeTargetCC`
- `composeHostCC`
- `composeMuslCC`
- `composeMuslHostCC`
- `composeASCmdArgs`
- `biFlagsForInstance`
- `composeLDCmdVcsCompile`
- `composeLDCmdVcsCompileHost`

The repeated core is the same ordered pipeline:

- `compileFlagBundleFor`
- `debugPrefixMapFlags`
- `xclangDebugCompilationDir`
- `bundle.CFlags`
- warning bundle
- defines bundle
- pre-block extras
- first `bundle.NoLibcBlock`
- `catboostOpenSourceDefine`
- auto-peer / CPU-feature slot
- second `bundle.NoLibcBlock`

### Goal

Make the flag pipeline data-driven and shared, while preserving byte-exact
output order.

### Expansion

#### 1.1 Extract a shared pipeline for the `compose*CC` quartet

Scope:

- `composeTargetCC`
- `composeHostCC`
- `composeMuslCC`
- `composeMuslHostCC`

Deliverable:

- one shared helper for the ordered flag pipeline before the language-specific
  tail;
- zero behavioral change;
- existing `emit_cc_test.go` remains green.

Reason for doing this first:

- smallest safe write set;
- best existing coverage;
- opens the path for `AS`, `BI`, and `LD-vcs` to reuse the same helper.

#### 1.2 Rebuild `composeASCmdArgs` on top of the shared pipeline

Scope:

- `emit_as_helpers.go`

Notes:

- keep AS-specific include-tail placement and `SFLAGS` tail;
- preserve musl/no-stdinc behavior.

#### 1.3 Rebuild `biFlagsForInstance` on top of the shared pipeline

Scope:

- `emit_bi.go`

Notes:

- BI is structurally close to C++ compile flags and should become mostly data
  passed into the same pipeline helper.

#### 1.4 Rebuild `composeLDCmdVcsCompile*` on top of the shared pipeline

Scope:

- `emit_ld.go`

Notes:

- target and host VCS compile nodes share the same flag backbone;
- keep the two LD-vcs-specific quirks explicit:
  - musl self define placement;
  - `moveFlagAfter(..., "-DPCRE_STATIC")` ordering fix.

#### 1.5 Cleanup pass

After 1.1-1.4:

- remove dead local helper logic;
- normalize helper naming;
- update comments so they describe the new shape rather than the old duplicated
  one.

## 2. Musl-Named Identifiers and Wrappers

Collapse musl-specific helper naming and wrapper layers after the shared
pipeline exists. Keep musl rule-data, remove musl-shaped control flow where it
is only naming and injection plumbing.

## 3. Host-vs-Target Propagation Shims

Replace narrow host/target propagation shims with explicit general mechanisms:

- `ModuleInstance.BinaryDir`
- target-axis propagation for include/search-path-sensitive joins

Main evidence:

- `jsTargetPeerAddIncl`
- PIC guard around join path
- `ldBinaryDir` switch

## 4. Python Module Traits Table

Replace trait-like Python module-name switches with a single
`pyModuleTraits` table.

Scope:

- language
- archive/global prefixes
- module/global tags

## 5. Implicit-Peer Self-Guards

Replace repeated exact/subtree self-guard logic with one helper that takes an
explicit mode:

- `ExactSelf`
- `SubtreeSelf`

Scope is only pure self-guard sites, not mixed policy suppressors.

## 6. Archive Reorder Policy

Replace Python archive-tail reorder branches with data-driven rules.

Scope:

- `PY2_PROGRAM`
- `PY3_PROGRAM`
- `PY3_PROGRAM_BIN`
- allocator-specific reorder rules

## 7. Shared Protoc Wrapper Builder

Extract the common protoc-wrapper node builder shared by `PB` and `EV`, keeping
per-kind policy hooks for:

- outputs
- include path policy
- extra plugins
- optional module tag

Blocked on `P1`.

## 8. Split `gen.go`

After items 4-6 simplify the file shape, split `gen.go` by cohesion instead of
moving duplication around prematurely.

Likely targets:

- `gen_python.go`
- `gen_cxx_runtime.go`

## Backlog

- `linuxMuslSysInclOrder`: rule-data, not a refactor priority.
- `ldStaticMuslTrailingFlags`: rule-data, not a refactor priority.
- mixed self-guard + policy suppressor at `gen.go:835-840`: not a pure item-5
  fold.
- archaeology and stale PR comments: hygiene, not structural priority.
