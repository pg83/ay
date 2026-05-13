package main

// cc.go — emitter for CC compilation nodes.
//
// PR-29 reshaped the signature: `EmitCC` now takes a
// `ModuleCCInputs` struct alongside `ModuleInstance` and `srcRel`.
// The struct carries per-module knobs that vary across modules in the
// same closure but stay constant for a single (instance, source)
// pair: ADDINCL paths, own CXXFLAGS, own CONLYFLAGS, the
// "is generated" bit (input lives under $(B) instead of
// $(S)), and a `Generator NodeRef` (PR-30 D04: wired to the
// upstream JS or R6 node so the CC's DepRefs carry the source-
// generator dep, matching the reference shape).
// Extending the struct does not require updating every call site —
// that is the whole point of switching from a positional signature.
//
// PR-23/25/32 history:
//
//   - PR-23 retrofitted the signature to take a `ModuleInstance`
//     instead of a (PlatformConfig, moduleDir) pair.
//   - PR-25 wired the walker into musl, where the third
//     `composeMuslCC` flavor activates.
//   - PR-29 adds a fourth flavor (`composeMuslHostCC`) for host-musl
//     PIC nodes — the dominant L3 lift lever (1297 nodes in the
//     archiver closure flip from "diverges" to "byte-exact").
//   - PR-32 flips the composer dispatch from path-prefix
//     (`HasPrefix(instance.Path, "contrib/libs/musl")`) to flag-
//     driven (`instance.Flags.LibcMusl`). The flag is the
//     architectural anchor — musl is just a libc flavour selected
//     by a CLI -D flag, not a special-cased module class.
//
// Output path convention is unchanged from PR-12:
//
//   - Flat source: `$(B)/<path>/<srcRel><.o|.pic.o>`
//   - Nested source (contains "/"): `$(B)/<path>/_/<srcRel><.o|.pic.o>`
//
// Suffix is `.o` for target builds, `.pic.o` for host (Flags.PIC=true).
//
// Four flavours of cmd_args composition:
//
//   - target-default (`commonCFlags` + `noLibcUndebugBlock` × 2): 101
//     args (M1 acceptance pin against `build/cow/on/lib.c.o`).
//   - host-PIC (`hostCFlags` + `ndebugPicBlock` × 2 + `hostSseFeatures`
//     between): 105 args (M1 acceptance pin against
//     `build/cow/on/lib.c.pic.o`).
//   - musl target (`muslCcIncludes` aarch64 + `muslExtraDefines`):
//     111 args.
//   - musl host (`muslCcIncludesX8664` + `hostCFlags` + `hostDefines`
//     + `muslExtraDefines` + `ndebugPicBlock` × 2 + `hostSseFeatures`
//     between): 115 args. Pinned byte-exact against
//     `contrib/libs/musl/_/src/string/strlen.c.pic.o`. PR-29-D01
//     dominant lever.

import (
	"os"
	"path/filepath"
	"strings"
)

// ModuleCCInputs carries per-module compile knobs that vary between
// modules in the same closure but stay constant per (instance,
// source). Threaded through EmitCC by the walker. Adding a new field
// here does not require updating call sites that do not consume it —
// the zero value is the historical "no per-module flags" behaviour.
//
// PR-29 wires:
//   - AddIncl: own ADDINCL paths (D03)
//   - CXXFlags: own CXXFLAGS (D02; C++ sources only)
//   - COnlyFlags: own CONLYFLAGS (D02; C/.S sources only)
//   - IsGenerated: source lives under $(B)/... (D07)
//   - Generator: NodeRef of the upstream generator node (JS for
//     JOIN_SRCS, R6 for ragel6) — PR-30 D04 wired this so EmitCC
//     populates DepRefs with the generator dep.
//   - SrcDir: PR-30 D06 — the module's SRCDIR setting; used to
//     compose output infix `__/<rel>` and SRCDIR-based input path
//     for sibling/non-local source files.
//
// D04 (peer-propagated GLOBAL ADDINCL/CXXFLAGS) is deferred to PR-31.
type ModuleCCInputs struct {
	AddIncl []string
	// PeerAddInclGlobal is the union of every PEERDIR's transitive
	// ADDINCL(GLOBAL ...) contributions in declaration order
	// (PR-31 D06). Slotted in cmd_args AFTER own AddIncl and BEFORE
	// the ccIncludesSuffix (linux-headers pair). The include scanner
	// also queries this slice as a search-path fallback when a
	// `<header>` does not resolve from own AddIncl. Empty for
	// modules whose PEERDIR closure declares no GLOBAL ADDINCL.
	PeerAddInclGlobal []string
	CXXFlags          []string
	COnlyFlags        []string
	IsGenerated       bool
	Generator         NodeRef
	// HasGenerator distinguishes "no generator" from "generator that
	// happens to have a zero-valued NodeRef.id" (BufferedEmitter
	// assigns ids starting at 0, so a nil-check on the bare struct is
	// unreliable for the very first emitted node). Set this true
	// alongside `Generator` whenever a JS or R6 ref is threaded.
	HasGenerator bool
	// ExtraDepRefs threads additional NodeRefs into the CC node's
	// DepRefs alongside `Generator` (when HasGenerator). PR-M3-codegen-
	// cc-enqueue: the EN-downstream CC's `deps` carries the consumer's
	// own EN ref (via Generator) AND the cross-EN dep refs (the EN
	// nodes whose `_serialized.h` outputs participate in the consumer's
	// header closure). The reference shape for a downstream CC of a
	// codegen producer with cross-codegen deps is two deps, not one.
	ExtraDepRefs []NodeRef
	// SrcDir is the module's `SRCDIR(...)` setting (empty when none).
	// PR-30 D06: when non-empty AND the source is non-local (resolves
	// under SRCDIR rather than instance.Path), the composer uses
	// `__/<rel>` as the output-path infix and `<srcdir>/<src>` as the
	// input path. The walker passes the original module SrcDir
	// uniformly; per-source local-vs-srcdir resolution happens inside
	// the composer via filesystem stat of the candidate local path.
	SrcDir string
	// SourceRoot is the walker's source root (genCtx.sourceRoot). The
	// composer needs it to stat candidate local source paths so flat
	// sources that exist locally (e.g. tcmalloc/no_percpu_cache's
	// aligned_alloc.c, musl_extra's all.c) keep their natural local
	// resolution rather than the SRCDIR-rebased one. Empty SourceRoot
	// disables the local-existence check entirely (used by synthetic
	// tests that pin the SRCDIR-rebased shape directly).
	SourceRoot string
	// IncludeInputs is the resolved transitive header set produced
	// by the include scanner (PR-31 D08). EmitCC appends this slice
	// to node.Inputs after the primary source path, in DFS-discovery
	// order. Empty for synthetic test paths that bypass the walker
	// or for IsGenerated CCs where the scanner is intentionally
	// skipped (generated CCs use a separate input shape).
	IncludeInputs []VFS
	// PeerCFlagsGlobal is the transitive union of every PEERDIR's
	// GLOBAL CFLAGS contribution (PR-32 D08). Applies to BOTH C and
	// C++ sources of the consumer, slotted in cmd_args AFTER own
	// CXXFLAGS / CONLYFLAGS and BEFORE builtinMacroDateTime. Empty
	// for modules whose PEERDIR closure declares no GLOBAL CFLAGS.
	PeerCFlagsGlobal []string
	// PeerCXXFlagsGlobal is the transitive union of every PEERDIR's
	// GLOBAL CXXFLAGS contribution (PR-32 D08). Applies to C++
	// sources only (.cpp/.cc/.cxx).
	PeerCXXFlagsGlobal []string
	// PeerCOnlyFlagsGlobal is the transitive union of every PEERDIR's
	// GLOBAL CONLYFLAGS contribution (PR-32 D08). Applies to C / .S
	// sources only.
	PeerCOnlyFlagsGlobal []string
	// AutoPeerCFlags is the auto-injected peer-CFLAG set the walker
	// adds based on cliDefines + module flags (PR-32 D09). The single
	// load-bearing entry today is `-D_musl_` (mirror of
	// `build/ymake.core.conf:781`'s
	// `when ($MUSL == "yes") { CFLAGS+=-D_musl_ }`). Kept separate
	// from PeerCFlagsGlobal so the source/from-where is auditable;
	// merged at cmd_args slot time.
	AutoPeerCFlags []string
	// CFlags is the module's own non-GLOBAL CFLAGS (PR-33 D03).
	// Applies to BOTH C and C++ sources of this module (mirror of
	// upstream's CFLAGS-applies-to-both rule). Slotted in cmd_args
	// between commonDefines and the first noLibcUndebugBlock copy —
	// empirical reference (libcxx algorithm.cpp.o cmd_args[51]:
	// `-DLIBCXXRT` between `commonDefines` and `noLibcUndebugBlock`).
	CFlags []string
	// OwnCFlagsGlobal is the module's own GLOBAL CFLAGS (PR-33 D02).
	// Emitted on the module's OWN compiles via the bucket model in
	// composeTargetCC / composeHostCC (`(own GLOBAL ∪ peer GLOBAL)`
	// slot, twice flanking the catboost-redux). Also peer-propagates
	// to consumers via PeerCFlagsGlobal — but the consumer-side
	// propagation is the responsibility of the walker's two-phase
	// aggregation (PR-32 D07), not this slot.
	OwnCFlagsGlobal []string
	// OwnCXXFlagsGlobal is the module's own GLOBAL CXXFLAGS (PR-33
	// D02). Same bucket-model emission as OwnCFlagsGlobal but C++
	// only. libcxx's `CXXFLAGS(GLOBAL -nostdinc++)` lands here.
	OwnCXXFlagsGlobal []string
	// OwnCOnlyFlagsGlobal is the module's own GLOBAL CONLYFLAGS
	// (PR-33 D02). C / .S sources only.
	OwnCOnlyFlagsGlobal []string
	// SFlags is the module's own SFLAGS bundle as appended by
	// `SET_APPEND(SFLAGS ...)` (PR-M3-openssl-as-cflags). Slotted by
	// composeASCmdArgs immediately before the trailing `-c -o <out>
	// <in>` block, mirroring upstream ymake's
	// `$CFLAGS $SFLAGS $SRCFLAGS -c -o ...` order at
	// `build/ymake.core.conf:3217`. Empty for every module that does
	// not declare `SET_APPEND(SFLAGS ...)`; only openssl-internal
	// modules carry a non-empty SFlags set in the M3 closure
	// (`contrib/libs/openssl/crypto/ya.make.inc:179-186`'s AVX512
	// bundle for x86_64).
	SFlags []string
	// PerSourceCFlags is the per-source extra CFLAGS bundle attached
	// to the current compile via the `SRC(filename extra_cflags...)`
	// macro (PR-35o). The composer slots these flags BETWEEN
	// `macroPrefixMapFlags` and the input path — matching the
	// empirical reference for `util/charset/wide_sse41.cpp.o` where
	// `-DSSE41_STUB` sits immediately before
	// `$(S)/util/charset/wide_sse41.cpp`. Empty for sources
	// declared via plain `SRCS` / `SRC_C_NO_LTO` / `JOIN_SRCS` /
	// `GLOBAL_SRCS`.
	PerSourceCFlags []string
	// FlatOutput selects a flat output-path layout for this source —
	// no `_/` infix even when the source contains a `/` (PR-35o). Set
	// for sources declared via the upstream `SRC(...)` and
	// `SRC_C_NO_LTO(...)` macros. Mirrors the empirical reference
	// distinction: `SRCS(digest/city.cpp)` →
	// `util/_/digest/city.cpp.o`, while `SRC_C_NO_LTO(system/compiler.cpp)`
	// → `util/system/compiler.cpp.o`. Default false preserves the
	// historical SRCS behaviour for every other source type.
	FlatOutput bool
	// DefaultVars is the per-module DEFAULT(name value) map collected
	// from the ya.make. Used by EmitCF to expand $CFG_VARS (PR-M3-E).
	// Keys are variable names; values are the DEFAULT-declared values.
	DefaultVars     map[string]string
	DefaultVarOrder []string
	// Py3Suffix selects ".py3.o" as the output suffix instead of the
	// default ".o". Set for PY23_NATIVE_LIBRARY modules whose reference
	// graph emits <src>.py3.o instead of plain <src>.o. Does not affect
	// PIC modules (".pic.o" suffix is still used when Flags.PIC is set).
	Py3Suffix bool
	// ModuleTag, when non-empty, is added to the emitted CC's
	// target_properties as `module_tag=<ModuleTag>`. PROTO_LIBRARY CCs
	// consuming protoc-generated .pb.cc / .ev.pb.cc sources carry
	// `module_tag=cpp_proto`; regular LIBRARY CCs leave this empty so
	// no module_tag key is emitted (the reference for a regular LIBRARY
	// CC of a generated .ev.pb.cc lacks the key).
	ModuleTag string
	// Variant marks this compile as a SIMD permutation of `srcRel`
	// emitted via one of the `SRC_C_AVX / SSE2 / SSE3 / SSSE3 / SSE4 /
	// SSE41 / XOP` macros (PR-M3-simd-permutations). When non-empty the
	// output path becomes `<srcRel>.<variant><suffix>` (flat, even for
	// srcRel with subdirs) and PerSourceCFlags carries the `-m<flag>`
	// bundle plus any extra `-DSUFFIX=…` from the macro arglist.
	Variant string
	// Ragel6Flags is the per-module `SET(RAGEL6_FLAGS <value>)` override
	// threaded into EmitR6 (PR-M3-ragel-flags-per-module). When empty the
	// platform-default fires inside EmitR6 (`-CG2` on x86_64 host /
	// `-CT0` on aarch64 target — mirroring upstream
	// build/ymake_conf.py:2271-2277 where `set_default_flags(optimized)`
	// branches on the release toolchain flag). Empirical M3 witness:
	// `devtools/ymake/lang/makelists/ya.make:6` sets `-lF1`, producing
	// the ragel6 cmd_args[1] observed in the reference graph's
	// `makefile_lang.rl6.cpp` node.
	Ragel6Flags []string
}

// EmitCC emits a CC node for compiling `srcRel` (a path relative to
// `instance.Path`, e.g. "lib.c" or "src/algorithm.cpp") into an
// object file. Returns the NodeRef so callers (typically the AR step)
// can wire it as a dependency, plus the output path so callers do
// not have to re-derive it (PR-10-D03).
//
// `in` carries per-module knobs (D02/D03/D05/D06/D07); pass
// `ModuleCCInputs{}` for the historical flag-less behaviour.
//
// The composed cmd_args length is 101 / 105 / 111 / 115 depending on
// the flavour (with D03 ADDINCL, D02 own flags, D05 -std=c++20, D06
// NoCompilerWarnings selector adding/removing args inline);
// reviewer-tracked tests pin each variant against the reference
// graph.
func EmitCC(instance ModuleInstance, srcRel string, in ModuleCCInputs, emit Emitter) (NodeRef, VFS) {

	suffix := ".o"
	if instance.Flags.PIC {
		suffix = ".pic.o"
	} else if in.Py3Suffix {
		suffix = ".py3.o"
	}

	// PR-M3-simd-permutations: prefix the suffix with `.<variant>` so the
	// output path becomes `<srcRel>.<variant><suffix>`. The reference
	// emits e.g. `<src>.avx.pic.o`, `<src>.sse41.pic.o`, etc.
	if in.Variant != "" {
		suffix = "." + in.Variant + suffix
	}

	outVFS, inVFS := composeCCPaths(instance, srcRel, in, suffix)
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	// PR-32 D02: dispatch via Flags.LibcMusl, not the path-prefix
	// test. The flag is seeded by `inferFlagsFromPath` for the M2
	// shim; macro-driven inference replaces the heuristic in M5+.
	isMusl := instance.Flags.LibcMusl
	isCxx := isCxxSource(srcRel)

	// Filter own per-source extras by source language. CXXFLAGS apply
	// to C++ sources only; CONLYFLAGS apply to C / .S sources. The
	// reference behaviour matches upstream ymake's CXXFLAGS / CONLYFLAGS
	// macros documented in build/ymake.core.conf.
	var ownExtras []string
	if isCxx {
		ownExtras = in.CXXFlags
	} else {
		ownExtras = in.COnlyFlags
	}

	var cmdArgs []string

	// For musl modules the include set in muslCcIncludes / muslCcIncludesX8664
	// already contains exactly the paths the musl ya.make declares via its
	// own ADDINCL block (arch/X, arch/generic, src/include, src/internal,
	// include, extra). Threading those again would duplicate. The caller's
	// ADDINCL slice for a musl module is dropped here — musl is the only
	// module whose own ADDINCL is structurally folded into the composer's
	// hardcoded include set. Same logic for COnlyFlags: muslExtraDefines
	// already carries the musl ya.make's own CFLAGS (minus the GLOBAL
	// -D_musl_=1 which is peer-propagated; D04 territory).
	muslOwnExtras := []string(nil)

	// PR-31 D06 + PR-32: own ADDINCL slots BEFORE ccIncludesSuffix
	// (linux-headers); peer-propagated GLOBAL ADDINCL slots AFTER it.
	// Empirical reference (util/charset/all_charset.cpp.o cmd_args[7..16])
	// shows this ordering: prefix → linux-headers suffix → libcxx-include
	// + libcxxrt-include + musl-arch (peer-GLOBAL paths). For musl
	// flavours BOTH own AddIncl and PeerAddInclGlobal are dropped:
	// musl's `-nostdinc` + `muslCcIncludes` set defines the entire
	// include search path by design, and adding peer-GLOBAL `-I` would
	// conflict with the musl-self-isolation invariant.

	// PR-32 D08/D09: split the peer/auto-CFLAG injection into two
	// slots, matching the empirical reference shape:
	//
	//   - autoPeerCFlags (`-D_musl_`) sits BETWEEN the catboost flag
	//     and the SECOND noLibcUndebugBlock copy. Verified against
	//     util/charset/all_charset.cpp.o cmd_args[78].
	//   - peerExtras (PeerCFlagsGlobal + per-language peer-GLOBAL set)
	//     sits at the existing cxx-extras tail (AFTER own CXXFLAGS and
	//     BEFORE builtinMacroDateTime). Verified against the
	//     `-nostdinc++` peer-propagation pattern.
	//
	// For musl flavours BOTH slots stay empty (musl-self-isolation
	// invariant — see Q6). The `-D_musl_=1` musl-self CFLAG comes from
	// `muslExtraDefines` inside composeMuslCC / composeMuslHostCC.
	var autoPeerCFlags, peerExtras, ownGlobalBucket, ownCFlags []string

	if !isMusl {
		autoPeerCFlags = in.AutoPeerCFlags
		peerExtras = composePeerExtras(in, isCxx)
		ownGlobalBucket = composeOwnAndPeerGlobalBucket(in, isCxx)
		// PR-M3-F-6: ownCFlags slot now carries in.CFlags +
		// OwnCFlagsGlobal + PeerCFlagsGlobal (all CFLAGS axes
		// concatenated). Empirical: antlr4 SetTransition.cpp.o
		// idx 52-54 (own GLOBAL) and python mysnprintf.c.pic.o
		// idx 76-78 (peer GLOBAL from lzma/openssl/libffi) both
		// land here, not in the bucket or peerExtras tail.
		ownCFlags = composeOwnAndPeerCFlagsAtOwnSlot(in)
	}

	// PR-M3-platform-pair-step10: compose-flavor dispatch is on
	// instance.Platform.Target (which toolchain to use) and instance.Flags.LibcMusl
	// (per-MODULE musl subtree membership — NOT instance.Platform.LibcMusl, which
	// is the platform-wide libc selector).
	targetX8664 := instance.Platform.ISA == ISAX8664
	switch {
	case isMusl && targetX8664:
		cmdArgs = composeMuslHostCC(instance.Platform, outputPath, inputPath, nil, muslOwnExtras, isCxx)
	case isMusl:
		cmdArgs = composeMuslCC(instance.Platform, outputPath, inputPath, nil, muslOwnExtras, isCxx)
	default:
		args := ccComposeArgs{
			Platform:           instance.Platform,
			OutputPath:         outputPath,
			InputPath:          inputPath,
			OwnAddIncl:         in.AddIncl,
			PeerAddIncl:        in.PeerAddInclGlobal,
			OwnCFlags:          ownCFlags,
			OwnExtras:          ownExtras,
			AutoPeerCFlags:     autoPeerCFlags,
			PeerExtras:         peerExtras,
			OwnGlobalBucket:    ownGlobalBucket,
			PerSrcCFlags:       in.PerSourceCFlags,
			IsCxx:              isCxx,
			NoCompilerWarnings: instance.Flags.NoCompilerWarnings,
		}
		if targetX8664 {
			cmdArgs = composeHostCC(args)
		} else {
			cmdArgs = composeTargetCC(args)
		}
	}

	// The reference graph carries the same env map at both the cmd
	// level and the top level of the Node. Build it once and reuse;
	// EmitCC is single-shot so the alias is safe today. Future PRs
	// that mutate emitted nodes post-emit MUST clone before mutating.
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	// PR-31 D09: prepend the resolved transitive header set to
	// node.Inputs after the primary source path. The order is
	// primary source first, then include-inputs in DFS-discovery
	// order (the scanner does no sorting; L2 compares as multiset).
	allInputs := make([]VFS, 0, 1+len(in.IncludeInputs))
	allInputs = append(allInputs, inVFS)
	allInputs = append(allInputs, in.IncludeInputs...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  allInputs,
		Outputs: []VFS{outVFS},
		KV: map[string]string{
			"p":  "CC",
			"pc": "green",
		},
		Tags: instance.Platform.Tags,
		TargetProperties: func() map[string]string {
			tp := map[string]string{"module_dir": instance.Path}
			if in.ModuleTag != "" {
				tp["module_tag"] = in.ModuleTag
			}
			return tp
		}(),
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		// Numeric values are stored as float64 to match what
		// encoding/json produces when unmarshalling the reference
		// graph into `map[string]interface{}` (Go's default JSON-
		// number type for `interface{}` targets). Constructing with
		// int literals would make a comparator using
		// reflect.DeepEqual against the reference fail spuriously
		// even though the on-disk JSON is identical.
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	// PR-M3-platform-pair-step10: HostPlatform + Tags are already
	// plumbed from targetP via the Node literal above; nothing left
	// to do here. The legacy `if targetIsX8664(instance) { … }` block
	// is retired.

	// PR-30 D04: when `HasGenerator` is set, thread `Generator` into
	// DepRefs so the CC node carries an explicit dep on its source-
	// generating JS/R6 node (matching the reference shape — every
	// JS-derived and R6-derived CC in the reference has Deps=[gen UID]).
	// The HasGenerator flag is required because BufferedEmitter assigns
	// NodeRef ids starting at 0; a bare NodeRef{} comparison would
	// false-negative on the very first emitted node.
	//
	// PR-M3-codegen-cc-enqueue: ExtraDepRefs additionally threads cross-
	// codegen deps (e.g. the cross-EN node refs for an EN-downstream CC)
	// so the resulting Deps multiset matches the reference shape.
	if in.HasGenerator {
		node.DepRefs = append([]NodeRef{in.Generator}, in.ExtraDepRefs...)
	} else if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = append([]NodeRef(nil), in.ExtraDepRefs...)
	}

	return emit.Emit(node), outVFS
}

// composeCCPaths derives the (outputPath, inputPath) pair per PR-30
// D06's SRCDIR-aware semantics. The composer distinguishes three
// shapes empirically observed in the reference graph:
//
//  1. No SRCDIR (the historical case): output is
//     `$(B)/<instance.Path>/<rel>.o` (with `_/` infix when
//     srcRel contains "/"); input is
//     `$(S)/<instance.Path>/<srcRel>` (or `$(B)/`
//     when IsGenerated).
//  2. SRCDIR set, source resolves locally (file exists at
//     `<sourceRoot>/<instance.Path>/<srcRel>`): SRCDIR is silently
//     ignored — same as case (1). Empirical examples: musl_extra's
//     `all.c`, tcmalloc/no_percpu_cache's `aligned_alloc.c`.
//  3. SRCDIR set, source does not resolve locally: input is
//     `$(S)/<srcdir>/<srcRel>`; output is
//     `$(B)/<instance.Path>/__/<rel>.o` where `<rel>` is the
//     relative path from instance.Path to (srcdir+srcRel), with `..`
//     segments rendered as `__`. Empirical examples: libcxxabi-parts's
//     `src/abort_message.cpp` (sibling SRCDIR), tcmalloc/no_percpu_cache's
//     `tcmalloc/want_hpaa.cc` (ancestor SRCDIR + nested src path).
//
// Generated sources (IsGenerated=true) skip case (3) — generators emit
// to `$(B)/<srcInstance.Path>/<rel>` where srcInstance is
// already SRCDIR-aware (the JS/R6 emitter rebased it before invocation).
func composeCCPaths(instance ModuleInstance, srcRel string, in ModuleCCInputs, suffix string) (out, input VFS) {
	if in.IsGenerated {
		// Generators (JS/R6) write under $(B)/<srcInstance.Path>/.
		// SrcDir handling for those branches is upstream (in gen.go's
		// JOIN_SRCS / .rl6 dispatch where srcInstance is constructed).
		var outRel string

		if strings.Contains(srcRel, "/") {
			outRel = instance.Path + "/_/" + srcRel + suffix
		} else {
			outRel = instance.Path + "/" + srcRel + suffix
		}

		return Build(outRel), Build(instance.Path + "/" + srcRel)
	}

	// PR-30 D06 SRCDIR routing.
	useSrcDir := in.SrcDir != "" && in.SrcDir != instance.Path && !sourceExistsLocally(in.SourceRoot, instance.Path, srcRel)

	if useSrcDir {
		outputRel := composeSrcDirOutputRel(instance.Path, in.SrcDir, srcRel)
		return Build(instance.Path + "/" + outputRel + suffix), Source(in.SrcDir + "/" + srcRel)
	}

	var outRel string

	switch {
	case in.FlatOutput:
		// PR-35o: SRC / SRC_C_NO_LTO emit a flat output path even when
		// `srcRel` contains a `/`. Empirical reference:
		// `SRC_C_NO_LTO(system/compiler.cpp)` →
		// `util/system/compiler.cpp.o` (no `_/` infix).
		outRel = instance.Path + "/" + srcRel + suffix
	case strings.Contains(srcRel, "/"):
		outRel = instance.Path + "/_/" + srcRel + suffix
	default:
		outRel = instance.Path + "/" + srcRel + suffix
	}

	return Build(outRel), Source(instance.Path + "/" + srcRel)
}

// sourceExistsLocally reports whether `<sourceRoot>/<modulePath>/<srcRel>`
// is a regular file. PR-30 D06 uses this to decide whether a flat
// source resolves at the LIBRARY's own dir (case 2 above) or under
// SRCDIR (case 3). When sourceRoot is empty (the synthetic-test path),
// the helper returns false so the caller falls into the SRCDIR branch
// — synthetic tests that want the local-resolution shape pass an
// empty SrcDir (or a SrcDir equal to instance.Path), not an empty
// SourceRoot.
func sourceExistsLocally(sourceRoot, modulePath, srcRel string) bool {
	if sourceRoot == "" {
		return false
	}

	candidate := filepath.Join(sourceRoot, modulePath, srcRel)
	info, err := os.Stat(candidate)

	if err != nil {
		return false
	}

	return !info.IsDir()
}

// composeSrcDirOutputRel computes the output-path infix for case (3)
// of composeCCPaths: relative path from `instancePath` to
// `srcDir/srcRel`, with `..` segments replaced by `__`.
//
// Empirical reference matches:
//   - libcxxabi-parts: instance=`contrib/libs/cxxsupp/libcxxabi-parts`,
//     srcDir=`contrib/libs/cxxsupp/libcxxabi`, srcRel=`src/abort_message.cpp`
//     → relpath = `../libcxxabi/src/abort_message.cpp`
//     → infix = `__/libcxxabi/src/abort_message.cpp`
//   - tcmalloc/no_percpu_cache: instance=`contrib/libs/tcmalloc/no_percpu_cache`,
//     srcDir=`contrib/libs/tcmalloc`, srcRel=`tcmalloc/want_hpaa.cc`
//     → relpath = `../tcmalloc/want_hpaa.cc`
//     → infix = `__/tcmalloc/want_hpaa.cc`
func composeSrcDirOutputRel(instancePath, srcDir, srcRel string) string {
	target := filepath.Join(srcDir, srcRel)
	rel, err := filepath.Rel(instancePath, target)

	if err != nil {
		// filepath.Rel only fails on absolute-vs-relative mismatch
		// or on Windows volume mismatch; both are unreachable for our
		// SOURCE_ROOT-relative inputs. Fall back to a defensive shape.
		return "_/" + srcRel
	}

	// Replace each `..` segment with `__` to match ymake's path
	// rendering (the same convention the reference graph uses for
	// SRCDIR-redirected outputs that go outside the module dir).
	// When there are NO `..` segments, the target is under instancePath.
	// ymake still uses a `_/` prefix for SRCDIR-based outputs that land
	// under the module directory, mirroring the non-SRCDIR `_/` infix
	// for sources with slashes (line 420 of composeCCPaths). Without the
	// `_/`, openssl's `SRCDIR(crypto)` + `../asm/aarch64/...` sources
	// would emit to `openssl/asm/...` instead of `openssl/_/asm/...`.
	parts := strings.Split(rel, string(filepath.Separator))

	hasParent := false
	for i, p := range parts {
		if p == ".." {
			parts[i] = "__"
			hasParent = true
		}
	}

	joined := strings.Join(parts, "/")

	// No parent traversal → target lands under instancePath: prepend `_/`
	// to match ymake's convention for SRCDIR-redirected subdirectory outputs.
	if !hasParent {
		return "_/" + joined
	}

	return joined
}

// isCxxSource returns true when `srcRel`'s extension marks it as a
// C++ source the reference compiles via clang++ + -std=c++20. R6's
// generated `.cpp` outputs flow through this branch; `.c` and `.S`
// stay on the C path.
func isCxxSource(srcRel string) bool {
	return strings.HasSuffix(srcRel, ".cpp") ||
		strings.HasSuffix(srcRel, ".cc") ||
		strings.HasSuffix(srcRel, ".cxx")
}

// pickCompiler dispatches between clang and clang++ per source
// language. PR-29-D05.
func pickCompiler(tools Toolchain, isCxx bool) string {
	if isCxx {
		return tools.CXX
	}

	return tools.CC
}

// pickWarningFlags substitutes `-Wno-everything` (the muslWarningFlags
// single-arg bundle) for the full `-Werror`/`-Wall`/... set when the
// module declares NO_COMPILER_WARNINGS. PR-29-D06.
func pickWarningFlags(noCompilerWarnings bool) []string {
	if noCompilerWarnings {
		return muslWarningFlags
	}

	return warningFlags
}

// appendCxxStdAndOwn appends the per-source-language tail that sits
// AFTER the second suppression-block copy and BEFORE the bucket /
// peerExtras / builtinMacroDateTime trailer: `-std=c++20` for C++
// sources (D05), then for C++ either the cxxStandardWarnings bundle
// (PR-33 D04) or its NoCompilerWarnings replacement
// `-Wno-everything`, then the module's own non-GLOBAL CXXFLAGS /
// CONLYFLAGS (D02).
//
// `injectCxxWarningBundle` controls whether the cxx-warning bundle
// (or its NoCompilerWarnings replacement) is injected. Pass true for
// target/host composers (current behaviour); pass false for musl
// composers that already added muslWarningFlags earlier to suppress
// duplicate injection.
//
// Slot ordering verified against:
//   - libcxx algorithm.cpp.o cmd_args[98..100]:
//     `-std=c++20 -Wno-everything -D_LIBCPP_BUILDING_LIBRARY`
//     (NoCompilerWarnings=true → -Wno-everything replaces the bundle).
//   - util/charset/all_charset.cpp.o cmd_args[101..111]:
//     `-std=c++20` then the 10-arg cxxStandardWarnings bundle
//     (NoCompilerWarnings=false → full bundle).
//   - getopt/small completer.cpp.o cmd_args[104..]: similar pattern.
func appendCxxStdAndOwn(cmdArgs []string, isCxx bool, noCompilerWarnings bool, injectCxxWarningBundle bool, ownExtras []string) []string {
	if isCxx {
		cmdArgs = append(cmdArgs, cxxStandardFlag)

		if injectCxxWarningBundle {
			if noCompilerWarnings {
				// libcxx-style slot: the cxx-warning-bundle is
				// replaced by `-Wno-everything` when the module sets
				// NO_COMPILER_WARNINGS. Reference: libcxx
				// algorithm.cpp.o cmd_args[99] = `-Wno-everything`.
				cmdArgs = append(cmdArgs, muslWarningFlags...)
			} else {
				// PR-33 D04: every clang C++ compile without
				// NO_COMPILER_WARNINGS gets the 10-arg
				// cxxStandardWarnings bundle. Reference:
				// util/charset/all_charset.cpp.o cmd_args[102..111].
				cmdArgs = append(cmdArgs, cxxStandardWarnings...)
			}
		}
	}

	cmdArgs = append(cmdArgs, ownExtras...)

	return cmdArgs
}

// composePeerExtras assembles the peer-propagated GLOBAL CXXFLAGS /
// CONLYFLAGS contribution per source-language axis (PR-32 D08, revised
// by PR-M3-F-6). Source-language filtering follows ymake's CFLAGS-axis
// rule, but the CFlags axis itself has been relocated to the ownCFlags
// slot (alongside `in.CFlags`) — see `composeOwnAndPeerCFlagsAtOwnSlot`.
//
//   - PeerCXXFlagsGlobal applies only to C++ sources.
//   - PeerCOnlyFlagsGlobal applies only to C / .S sources.
//
// PR-M3-F-6 rationale: empirical reference (python mysnprintf.c.pic.o
// idx 76, antlr4 SetTransition.cpp.o idx 52, devtools/ymake/bin/main.cpp.o
// idx 80) shows peer-propagated GLOBAL CFLAGS landing at the ownCFlags
// slot (immediately after in.CFlags and before the noLibcUndebugBlock /
// ndebugPicBlock), not at the cxx-extras tail. The peerExtras tail
// continues to carry CXXFLAGS / CONLYFLAGS only.
//
// AutoPeerCFlags (e.g. -D_musl_) is NOT included here — it slots at
// a different cmd_args position (between catboost and 2nd
// noLibcUndebugBlock); see the dedicated `autoPeerCFlags` argument
// to the composers. Mirror of the PR-31 `combinedAddIncl` ordering
// (peer contributions in declaration order; no own contributions
// here — those are appended via appendCxxStdAndOwn).
func composePeerExtras(in ModuleCCInputs, isCxx bool) []string {
	if isCxx {
		out := make([]string, 0, len(in.PeerCXXFlagsGlobal))
		out = append(out, in.PeerCXXFlagsGlobal...)

		return out
	}

	out := make([]string, 0, len(in.PeerCOnlyFlagsGlobal))
	out = append(out, in.PeerCOnlyFlagsGlobal...)

	return out
}

// composeOwnAndPeerCFlagsAtOwnSlot assembles the combined CFLAGS bundle
// that lands at the ownCFlags slot of composeTargetCC / composeHostCC
// (between commonDefines and the first noLibcUndebugBlock / ndebugPicBlock
// copy). PR-M3-F-6: this is where ALL CFLAGS go — own non-GLOBAL CFLAGS,
// own GLOBAL CFLAGS, and peer-propagated GLOBAL CFLAGS — applying to
// both C and C++ sources of the consumer.
//
// Order (concatenation; no dedup — the reference preserves duplicates,
// e.g. openssl's `-DOPENSSL_BUILD=1` appears twice via top-level CFLAGS
// and crypto/ya.make.inc): in.CFlags → in.OwnCFlagsGlobal →
// in.PeerCFlagsGlobal. Empirical anchors:
//
//   - lzma tuklib_cpucores.c.o idx 58-60: own non-GLOBAL `-DHAVE_CONFIG_H`,
//     `-DTUKLIB_SYMBOL_PREFIX=lzma_` (in.CFlags) precede own GLOBAL
//     `-DLZMA_API_STATIC` (OwnCFlagsGlobal).
//   - python mysnprintf.c.pic.o idx 73-78: in.CFlags `-DPLATFORM=...`
//     etc. precede peer-GLOBAL `-DLZMA_API_STATIC`,
//     `-DOPENSSL_RENAME_SYMBOLS=1`, `-DFFI_STATIC_BUILD`.
//   - devtools/ymake/bin/main.cpp.o idx 79-90: in.CFlags `-D_musl_=1`
//     (PROGRAM injection) precedes the peer chain (LZMA, OPENSSL, FFI,
//     USE_PYTHON3, ASIO_STANDALONE, …, ANTLR4CPP_STATIC, …).
//
// musl flavours (composeMuslCC / composeMuslHostCC) do not consult this
// helper — they fold CFLAGS into `muslExtraDefines` and zero out the
// peer-propagation slots upstream in EmitCC.
func composeOwnAndPeerCFlagsAtOwnSlot(in ModuleCCInputs) []string {
	// PR-M3-cmd-arg-slot-ordering: empirical (asio.cpp.o, lang/*.cpp.o,
	// idx ~53/71) shows peer-propagated GLOBAL CFLAGS slot AHEAD of own
	// GLOBAL CFLAGS. Own non-GLOBAL `in.CFlags` keeps its leading slot
	// (python mysnprintf.c.pic.o idx 73-78: `in.CFlags` first, then
	// peer-GLOBAL). So the rule is: [own non-GLOBAL, peer-GLOBAL, own
	// GLOBAL].
	out := make([]string, 0, len(in.CFlags)+len(in.PeerCFlagsGlobal)+len(in.OwnCFlagsGlobal))
	out = append(out, in.CFlags...)
	out = append(out, in.PeerCFlagsGlobal...)
	out = append(out, in.OwnCFlagsGlobal...)

	return out
}

// baseUnitCxxNostdinc is `_BASE_UNIT.CXXFLAGS += -nostdinc++` from
// `build/ymake.core.conf:807` — applied to every `_BASE_UNIT`-derived
// module in the default closure (USE_STL_SYSTEM != "yes" && MSVC !=
// "yes"). Empirically the injection lands ONLY at the post-catboost
// bucket slot, never at the pre-catboost or own-extras slots, even for
// modules whose own ya.make declares the same flag (libcxxrt:
// reference shows `-nostdinc++` at the ownExtras slot via its own
// `CXXFLAGS(-nostdinc++)` AND at the post-catboost bucket slot via
// this _BASE_UNIT injection — two distinct positions, not deduped
// across them).
//
// Musl flavours route through composeMuslCC / composeMuslHostCC and
// skip the catboost-redux entirely, so the injection is naturally
// excluded for musl. NoPlatform / NoRuntime / NoUtil do not gate the
// _BASE_UNIT body — all `_BASE_UNIT`-derived modules inherit the rule
// (libunwind has NO_RUNTIME and still receives the post-catboost
// `-nostdinc++` per its reference CC node).
const baseUnitCxxNostdinc = "-nostdinc++"

// composeOwnAndPeerGlobalBucket assembles the (own GLOBAL ∪ peer
// GLOBAL) CXXFLAGS / CONLYFLAGS contribution per source-language axis
// for the PR-33 D02 redux slot. C++ sources emit this bucket flanking a
// `-DCATBOOST_OPENSOURCE=yes` token (the catboost-redux); the
// post-catboost half is augmented with `baseUnitCxxNostdinc` per
// `composePostCatboostBucket` (PR-35f). C sources emit no redux.
//
// PR-M3-F-6: the CFlags axis (own + peer GLOBAL CFLAGS) was relocated
// out of this bucket and into the ownCFlags slot (see
// `composeOwnAndPeerCFlagsAtOwnSlot`). Empirical reference (antlr4
// SetTransition.cpp.o idx 103+105, libcxx algorithm.cpp.o idx 105+107)
// shows the bucket carries ONLY `-nostdinc++` and similar CXX-only /
// C-only-axis flags; antlr4's `-DANTLR4CPP_STATIC` (a CFLAGS GLOBAL)
// appears at the ownCFlags slot (idx 52-54), never in the bucket.
//
// Order: own/peer GLOBAL CXXFLAGS or CONLYFLAGS depending on source
// language. Deduplication is first-occurrence-wins (R14): an own GLOBAL
// flag also present in peer GLOBAL appears once, in the own slot.
//
// Empirical anchors:
//   - libcxx algorithm.cpp.o cmd_args[105] + [107]: bucket =
//     [-nostdinc++] (own GLOBAL CXXFLAGS = [-nostdinc++], peer GLOBAL
//     CXXFLAGS = [-nostdinc++ from libcxxabi-parts], deduped).
//   - util/charset/all_charset.cpp.o cmd_args[112] + [114]: bucket =
//     [-nostdinc++] (own GLOBAL = [], peer GLOBAL = [-nostdinc++]).
//   - abseil casts.cc.o cmd_args[99] + [101]: bucket = [-nostdinc++]
//     (own GLOBAL = [], peer GLOBAL = [-nostdinc++]).
//   - libcxxrt auxhelper.cc.o cmd_args[101] + post[103]: pre-bucket =
//     [] (no own/peer GLOBAL CXXFLAGS), post-bucket = [-nostdinc++]
//     via the _BASE_UNIT injection — closes PR-33 known gap (PR-35f).
func composeOwnAndPeerGlobalBucket(in ModuleCCInputs, isCxx bool) []string {
	out := make([]string, 0,
		len(in.OwnCXXFlagsGlobal)+len(in.PeerCXXFlagsGlobal)+
			len(in.OwnCOnlyFlagsGlobal)+len(in.PeerCOnlyFlagsGlobal))
	seen := make(map[string]struct{}, cap(out))

	addEach := func(src []string) {
		for _, x := range src {
			if _, dup := seen[x]; dup {
				continue
			}

			seen[x] = struct{}{}
			out = append(out, x)
		}
	}

	if isCxx {
		addEach(in.OwnCXXFlagsGlobal)
		addEach(in.PeerCXXFlagsGlobal)
	} else {
		addEach(in.OwnCOnlyFlagsGlobal)
		addEach(in.PeerCOnlyFlagsGlobal)
	}

	return out
}

// composePostCatboostBucket returns the post-catboost half of the
// bucket-twice slot. PR-35f closes PR-33-C2_04: the pre-catboost half
// is `preBucket` (own GLOBAL ∪ peer GLOBAL) as before, but the
// post-catboost half folds in the `_BASE_UNIT.CXXFLAGS += -nostdinc++`
// injection on top — for non-musl C++ compiles in the default closure.
//
// Dedup is first-occurrence-wins, so libcxx (preBucket already carries
// `-nostdinc++` via own GLOBAL) and abseil (via peer GLOBAL) keep the
// same content on both halves and stay byte-exact. libcxxrt /
// libcxxabi-parts / libunwind (preBucket empty) gain `-nostdinc++` on
// the post half only — matching the reference exactly.
//
// Caller responsibility: invoke ONLY for non-musl C++ compiles. Musl
// composers route through composeMuslCC / composeMuslHostCC which do
// not consult the bucket at all; C sources do not emit a catboost-redux.
func composePostCatboostBucket(preBucket []string) []string {
	for _, x := range preBucket {
		if x == baseUnitCxxNostdinc {
			return preBucket
		}
	}

	out := make([]string, 0, len(preBucket)+1)
	out = append(out, preBucket...)
	out = append(out, baseUnitCxxNostdinc)

	return out
}

// composeTargetCC composes the cmd_args bundle for a TARGET-flavoured
// no-libc CC compilation. Pinned byte-exact (101 args, no per-module
// extras) against build/cow/on/lib.c.o in
// /home/pg/monorepo/yatool_orig/sg.json.
//
// PR-32 D09: `autoPeerCFlags` slots BETWEEN the catboost flag and
// the SECOND noLibcUndebugBlock copy. Verified against
// util/charset/all_charset.cpp.o cmd_args[78] in the reference.
//
// PR-33 D02 + D03 + D04 (slot anchoring):
//
//   - `ownCFlags` is the module's own non-GLOBAL CFLAGS — slot
//     BETWEEN commonDefines and the first noLibcUndebugBlock copy.
//     Empirical: libcxx algorithm.cpp.o cmd_args[51] = `-DLIBCXXRT`.
//   - For C++ sources: emit `ownGlobalBucket` (own ∪ peer GLOBAL
//     CFLAGS/CXXFLAGS) twice flanking a second
//     `-DCATBOOST_OPENSOURCE=yes` (catboost-redux), AFTER own
//     CXXFLAGS / CONLYFLAGS and BEFORE builtinMacroDateTime.
//     Empirical: libcxx algorithm.cpp.o cmd_args[101..103] =
//     `-nostdinc++ -DCATBOOST_OPENSOURCE=yes -nostdinc++`.
//   - For C sources: emit `peerExtras` once (the existing single
//     peerExtras slot) — no catboost-redux. Empirical: tcmalloc
//     aligned_alloc.c.o has no second catboost.
//   - cxxStandardWarnings bundle (D04) is injected by
//     `appendCxxStdAndOwn` for C++ sources without
//     NoCompilerWarnings.
// ccComposeArgs packs the parameter bundle for the cmd_args-composing
// helpers composeTargetCC / composeHostCC. Was twelve positional
// parameters; one mismatched alias was the kind of bug that wouldn't
// surface from the type system (every parameter is `[]string` or
// `string`).
type ccComposeArgs struct {
	Platform           *Platform
	OutputPath         string
	InputPath          string
	OwnAddIncl         []string
	PeerAddIncl        []string
	OwnCFlags          []string
	OwnExtras          []string
	AutoPeerCFlags     []string
	PeerExtras         []string
	OwnGlobalBucket    []string
	PerSrcCFlags       []string
	IsCxx              bool
	NoCompilerWarnings bool
}

func composeTargetCC(a ccComposeArgs) []string {
	cmdArgs := make([]string, 0, 101+len(a.OwnAddIncl)+len(a.PeerAddIncl)+len(a.OwnCFlags)+len(a.OwnExtras)+len(a.AutoPeerCFlags)+len(a.PeerExtras)+2*len(a.OwnGlobalBucket)+len(a.PerSrcCFlags)+4)
	cmdArgs = append(cmdArgs,
		pickCompiler(a.Platform.Tools, a.IsCxx),
		"--target="+a.Platform.Triple,
		"-march="+a.Platform.March,
		"-B"+binPath,
		"-c",
		"-o",
		a.OutputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludesPrefix...)
	cmdArgs = appendAddIncl(cmdArgs, a.OwnAddIncl)
	cmdArgs = append(cmdArgs, ccIncludesSuffix...)
	cmdArgs = appendAddIncl(cmdArgs, a.PeerAddIncl)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)
	cmdArgs = append(cmdArgs, pickWarningFlags(a.NoCompilerWarnings)...)
	cmdArgs = append(cmdArgs, commonDefines...)
	cmdArgs = append(cmdArgs, a.OwnCFlags...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, a.AutoPeerCFlags...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)

	// For C sources, CONLYFLAGS (ownExtras) must trail AFTER
	// macroPrefixMapFlags — empirical reference: base64 neon32/neon64/
	// plain32/plain64 CC nodes show CONLYFLAGS at cmd_args[107..108],
	// after the three fmacro-prefix-map flags. Do NOT pass them to
	// appendCxxStdAndOwn here; hold them for the trailer below.
	// For C++ sources the slot order is correct as-is.
	var cOnlyExtras []string
	if a.IsCxx {
		cmdArgs = appendCxxStdAndOwn(cmdArgs, true, a.NoCompilerWarnings, true, a.OwnExtras)
	} else {
		cOnlyExtras = a.OwnExtras
	}

	if a.IsCxx {
		cmdArgs = append(cmdArgs, a.OwnGlobalBucket...)
		cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
		cmdArgs = append(cmdArgs, composePostCatboostBucket(a.OwnGlobalBucket)...)
	} else {
		// C source: empirical reference shows no catboost-redux for
		// C compiles (build/cow/on lib.c.o, tcmalloc aligned_alloc.c.o).
		// peerExtras is sufficient (own GLOBAL CFLAGS / CONLYFLAGS for
		// C are unused in the M2 closure; if a future closure
		// surfaces such a case, revisit).
		cmdArgs = append(cmdArgs, a.PeerExtras...)
	}

	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	// PR-35o: per-source extra CFLAGS (from `SRC(filename
	// extra_cflags...)`) slot BETWEEN macroPrefixMapFlags and the
	// input path. Empirical reference: util/charset/wide_sse41.cpp.o
	// cmd_args show `-DSSE41_STUB` immediately before the source path.
	cmdArgs = append(cmdArgs, a.PerSrcCFlags...)
	// PR-37: C-source CONLYFLAGS trail after macroPrefixMapFlags (and
	// after perSrcCFlags). Empirical: base64 plain32/neon64 CC nodes.
	cmdArgs = append(cmdArgs, cOnlyExtras...)
	cmdArgs = append(cmdArgs, a.InputPath)

	return cmdArgs
}

// composeHostCC composes the cmd_args bundle for a HOST-flavoured PIC
// CC compilation. Pinned byte-exact (105 args, no per-module extras)
// against build/cow/on/lib.c.pic.o in
// /home/pg/monorepo/yatool_orig/sg.json.
//
// Differs from target in:
//   - No `-march=` (host is generic x86_64; the architecture is
//     captured by `-m64` inside hostCFlags instead).
//   - Release-flavoured: `-O3` in hostCFlags (vs target's `-g`).
//   - `-fPIC` and `-DNDEBUG` (vs target's `-UNDEBUG`).
//   - Adds `-D_YNDX_LIBUNWIND_ENABLE_EXCEPTION_BACKTRACE` to the
//     define block (host libunwind shim).
//   - Inserts `hostSseFeatures` (7 args) between the two ndebugPicBlock
//     copies, in addition to `catboostOpenSourceDefine`.
//
// PR-33 D02 + D03 + D04: same own-CFLAGS / cxxStandardWarnings /
// own-GLOBAL-bucket × 2 redux pattern as composeTargetCC. C++
// sources emit the bucket twice flanking the catboost-redux; C
// sources emit peerExtras once.
func composeHostCC(a ccComposeArgs) []string {
	cmdArgs := make([]string, 0, 105+len(a.OwnAddIncl)+len(a.PeerAddIncl)+len(a.OwnCFlags)+len(a.OwnExtras)+len(a.AutoPeerCFlags)+len(a.PeerExtras)+2*len(a.OwnGlobalBucket)+len(a.PerSrcCFlags)+4)
	cmdArgs = append(cmdArgs,
		pickCompiler(a.Platform.Tools, a.IsCxx),
		"--target="+a.Platform.Triple,
		"-B"+binPath,
		"-c",
		"-o",
		a.OutputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludesPrefix...)
	cmdArgs = appendAddIncl(cmdArgs, a.OwnAddIncl)
	cmdArgs = append(cmdArgs, ccIncludesSuffix...)
	cmdArgs = appendAddIncl(cmdArgs, a.PeerAddIncl)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, hostCFlags...)
	cmdArgs = append(cmdArgs, pickWarningFlags(a.NoCompilerWarnings)...)
	cmdArgs = append(cmdArgs, hostDefines...)
	cmdArgs = append(cmdArgs, a.OwnCFlags...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	// PR-32 D09: autoPeerCFlags (-D_musl_) slots BETWEEN catboost
	// and the SECOND ndebugPicBlock; for host the `hostSseFeatures`
	// block sits between them so the precise slot is BEFORE the SSE
	// bundle on host. Empirical host probe via tools/archiver host
	// CC nodes confirms the same shape (catboost → -D_musl_ →
	// hostSseFeatures → 2nd ndebugPicBlock).
	//
	// PR-M3-cc-argv-slot-order: `-DUSE_PYTHON3` is also routed through
	// autoPeerCFlags by `defaultPeerCFlags`, but the host reference
	// places it AFTER `hostSseFeatures` (sitecustomize.cpp.pic.o ref:97
	// = -DUSE_PYTHON3, right after -mcx16 at ref:96 and before the 2nd
	// ndebugPicBlock at ref:98). Partition `autoPeerCFlags` here so
	// pre-SSE keeps `-D_musl_` and post-SSE picks up `-DUSE_PYTHON3`.
	preSse, postSse := partitionPython3FromAutoPeer(a.AutoPeerCFlags)
	cmdArgs = append(cmdArgs, preSse...)
	cmdArgs = append(cmdArgs, hostSseFeatures...)
	cmdArgs = append(cmdArgs, postSse...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	// PR-M3-cmd-arg-slot-ordering: mirror composeTargetCC's C-source
	// trailer — CONLYFLAGS slot AFTER macroPrefixMapFlags + perSrcCFlags,
	// not via appendCxxStdAndOwn's unconditional tail-append. Empirical:
	// base64 plain32/ssse3 host PIC nodes show -std=c11 (and -mssse3)
	// immediately before the source path.
	var cOnlyExtrasHost []string
	if a.IsCxx {
		cmdArgs = appendCxxStdAndOwn(cmdArgs, true, a.NoCompilerWarnings, true, a.OwnExtras)
	} else {
		cOnlyExtrasHost = a.OwnExtras
	}

	if a.IsCxx {
		cmdArgs = append(cmdArgs, a.OwnGlobalBucket...)
		cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
		cmdArgs = append(cmdArgs, composePostCatboostBucket(a.OwnGlobalBucket)...)
	} else {
		cmdArgs = append(cmdArgs, a.PeerExtras...)
	}

	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	// PR-35o: per-source extra CFLAGS slot (mirror of composeTargetCC).
	cmdArgs = append(cmdArgs, a.PerSrcCFlags...)
	cmdArgs = append(cmdArgs, cOnlyExtrasHost...)
	cmdArgs = append(cmdArgs, a.InputPath)

	return cmdArgs
}

// composeMuslCC composes the cmd_args bundle for a TARGET musl
// (`contrib/libs/musl/...` non-PIC) CC compilation. 111 args with no
// per-module extras. Differs from target in:
//   - `muslCcIncludes` (10 args) replaces `ccIncludes` (4 args)
//   - `muslWarningFlags` (1 arg) replaces `warningFlags` (6 args)
//   - `muslExtraDefines` (9 args) inserted after `commonDefines`,
//     before the noLibc block
func composeMuslCC(p *Platform, outputPath, inputPath string, addIncl, ownExtras []string, isCxx bool) []string {
	cmdArgs := make([]string, 0, 111+len(addIncl)+len(ownExtras)+2)
	cmdArgs = append(cmdArgs,
		pickCompiler(p.Tools, isCxx),
		"--target="+p.Triple,
		"-march="+p.March,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = appendMuslIncludes(cmdArgs, muslCcIncludesFor(p.ISA), addIncl)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)
	cmdArgs = append(cmdArgs, muslWarningFlags...) // musl always uses muslWarningFlags by definition.
	cmdArgs = append(cmdArgs, commonDefines...)
	cmdArgs = append(cmdArgs, muslExtraDefines...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	// musl already added muslWarningFlags above; suppress duplicate injection in helper.
	cmdArgs = appendCxxStdAndOwn(cmdArgs, isCxx, true, false, ownExtras)
	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	cmdArgs = append(cmdArgs, inputPath)

	return cmdArgs
}

// composeMuslHostCC composes the cmd_args bundle for a HOST musl
// (`contrib/libs/musl/...` PIC) CC compilation — PR-29-D01 dominant
// L3 lever. 115 args with no per-module extras. Pinned byte-exact
// against `$(B)/contrib/libs/musl/_/src/string/strlen.c.pic.o`
// (platform `default-linux-x86_64`) in the reference graph.
//
// Differs from composeMuslCC in:
//   - `muslCcIncludesX8664` replaces `muslCcIncludes` (arch/x86_64
//     in slot [8] instead of arch/aarch64).
//   - `--target=` is `hostTriple` (x86_64-linux-gnu).
//   - No `-march=` flag (host is generic x86_64).
//   - `hostCFlags` (11 args) replaces `commonCFlags` (14 args).
//   - `hostDefines` (12 args) replaces `commonDefines` (11 args) —
//     adds `-D_YNDX_LIBUNWIND_ENABLE_EXCEPTION_BACKTRACE`.
//   - `ndebugPicBlock` × 2 with `hostSseFeatures` between them
//     replaces `noLibcUndebugBlock` × 2 with just
//     `catboostOpenSourceDefine` between.
//
// Net: 111 + 4 = 115 args (one fewer prologue arg from no -march,
// one extra hostDefines arg, seven hostSseFeatures, three fewer
// hostCFlags = -3 + 1 + 7 - 1 + 0 = +4).
func composeMuslHostCC(p *Platform, outputPath, inputPath string, addIncl, ownExtras []string, isCxx bool) []string {
	cmdArgs := make([]string, 0, 115+len(addIncl)+len(ownExtras)+2)
	cmdArgs = append(cmdArgs,
		pickCompiler(p.Tools, isCxx),
		"--target="+p.Triple,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = appendMuslIncludes(cmdArgs, muslCcIncludesFor(p.ISA), addIncl)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, hostCFlags...)
	cmdArgs = append(cmdArgs, muslWarningFlags...) // musl always uses muslWarningFlags by definition.
	cmdArgs = append(cmdArgs, hostDefines...)
	cmdArgs = append(cmdArgs, muslExtraDefines...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, hostSseFeatures...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	// musl already added muslWarningFlags above; suppress duplicate injection in helper.
	cmdArgs = appendCxxStdAndOwn(cmdArgs, isCxx, true, false, ownExtras)
	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	cmdArgs = append(cmdArgs, inputPath)

	return cmdArgs
}

// partitionPython3FromAutoPeer splits `autoPeerCFlags` into a pre-SSE
// and post-SSE half for the HOST compose path (PR-M3-cc-argv-slot-order).
// `-DUSE_PYTHON3` is routed through `autoPeerCFlags` (see
// `defaultPeerCFlags`), but the host reference graph places it AFTER
// the `hostSseFeatures` block — between `-mcx16` and the second
// `ndebugPicBlock` copy. `-D_musl_` keeps its pre-SSE slot. Order is
// stable for every non-USE_PYTHON3 flag, so the split is safe to apply
// unconditionally.
func partitionPython3FromAutoPeer(autoPeer []string) ([]string, []string) {
	if len(autoPeer) == 0 {
		return autoPeer, nil
	}

	preSse := make([]string, 0, len(autoPeer))
	var postSse []string

	for _, f := range autoPeer {
		if f == "-DUSE_PYTHON3" {
			postSse = append(postSse, f)

			continue
		}

		preSse = append(preSse, f)
	}

	return preSse, postSse
}

// appendAddIncl prepends `-I$(S)/` to each ADDINCL path and
// appends them to `cmdArgs` (PR-29-D03). Paths are SOURCE_ROOT-relative
// in ya.make declarations; the composer adds the literal prefix and
// the `-I` flag at emit time. Order is preserved (R14 — declaration
// order matters for `include_next` chains).
//
// PR-M3-py3-buildroot-addincl: paths that already start with `$(B)/`
// (auto-injected by `${addincl;noauto;output:NAME}` in ymake.core.conf for
// ARCHIVE() consumers — e.g. library/python/runtime_py3's build-tree dir)
// pass through verbatim under a literal `-I` prefix; SOURCE_ROOT wrapping
// would produce `-I$(S)/$(B)/…` which mismatches REF.
func appendAddIncl(cmdArgs []string, addIncl []string) []string {
	for _, p := range addIncl {
		if strings.HasPrefix(p, "$(B)/") {
			cmdArgs = append(cmdArgs, "-I"+p)

			continue
		}

		cmdArgs = append(cmdArgs, "-I$(S)/"+p)
	}

	return cmdArgs
}

// appendMuslIncludes splices per-module ADDINCL paths into the musl
// include set. Slot is BETWEEN the prefix `-I$(B)
// -I$(S)` (entries [0..1] of muslCcIncludes*) and the body
// of musl arch/include/extra paths plus linux-headers suffix
// (entries [2..]). This matches what builtins fp_mode.c.o shows
// (cmd_args[7..14]: `-I$(B) -I$(S) -I<musl/arch/X>
// -I<musl/arch/generic> -I<musl/include> -I<musl/extra>
// -I<linux-headers> -I<linux-headers/_nf>`) — but note that builtins
// is NOT a musl module, so its composition routes through composeTargetCC
// where appendAddIncl produces those musl-arch entries from its own
// IF(MUSL) ADDINCL block. For genuine musl modules the per-module
// ADDINCL slot is the same byte position (between prefix [0..1] and
// the rest), achieved by appendMuslIncludes splicing.
func appendMuslIncludes(cmdArgs []string, muslSet []string, addIncl []string) []string {
	cmdArgs = append(cmdArgs, muslSet[:2]...)
	cmdArgs = appendAddIncl(cmdArgs, addIncl)
	cmdArgs = append(cmdArgs, muslSet[2:]...)

	return cmdArgs
}
