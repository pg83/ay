package main

// cc.go — emitter for CC compilation nodes.
//
// Composer dispatch is flag-driven (`instance.Flags.NoStdInc`) — musl
// is a no-stdinc libc flavour selected by a CLI -D flag, not a
// special-cased module class.
//
// Output path convention:
//   - Flat source: `$(B)/<path>/<srcRel><.o|.pic.o>`
//   - Nested source (contains "/"): `$(B)/<path>/_/<srcRel><.o|.pic.o>`
//
// Suffix is `.o` for target, `.pic.o` for host (Flags.PIC=true).

import (
	"os"
	"path/filepath"
	"strings"
)

// ModuleCCInputs carries per-module compile knobs threaded through
// EmitCC by the walker. The zero value is the "no per-module flags"
// behaviour.
type ModuleCCInputs struct {
	AddIncl []VFS
	// PeerAddInclGlobal is the union of every PEERDIR's transitive
	// ADDINCL(GLOBAL ...) contributions in declaration order. Slotted
	// AFTER own AddIncl and BEFORE ccIncludesSuffix (linux-headers).
	// Also queried by the include scanner as a search-path fallback
	// when a `<header>` does not resolve from own AddIncl.
	PeerAddInclGlobal []VFS
	CXXFlags          []string
	COnlyFlags        []string
	IsGenerated       bool
	Generator         NodeRef
	// HasGenerator distinguishes "no generator" from "generator with
	// zero-valued NodeRef.id" — BufferedEmitter ids start at 0 so a
	// bare-struct nil check false-negatives on the first emitted node.
	HasGenerator bool
	// ExtraDepRefs threads additional NodeRefs into DepRefs alongside
	// `Generator` (when HasGenerator). An EN-downstream CC carries its
	// consumer EN ref (via Generator) plus cross-EN dep refs (EN nodes
	// whose `_serialized.h` participates in the consumer's header
	// closure) — two deps, not one.
	ExtraDepRefs []NodeRef
	// SrcDir is the module's `SRCDIR(...)` setting (nil when none).
	// When non-nil AND the source is non-local, the composer uses
	// `__/<rel>` as the output-path infix and `<srcdir>/<src>` as the
	// input path. Per-source local-vs-srcdir resolution happens via
	// filesystem stat of the candidate local path.
	SrcDir *string
	// SourceRoot is the walker's source root (genCtx.sourceRoot),
	// needed to stat candidate local source paths so flat sources that
	// exist locally (e.g. musl_extra's all.c) keep local resolution
	// rather than SRCDIR-rebased. Empty disables the check (synthetic
	// tests pinning the SRCDIR-rebased shape directly).
	SourceRoot string
	// IncludeInputs is the transitive header set produced by the
	// include scanner. Appended to node.Inputs after the primary
	// source path in DFS-discovery order. Empty for synthetic paths
	// bypassing the walker or for IsGenerated CCs (scanner skipped;
	// generated CCs use a separate input shape).
	IncludeInputs []VFS
	// PeerCFlagsGlobal: transitive union of every PEERDIR's GLOBAL
	// CFLAGS. Applies to BOTH C and C++ sources; slotted at the
	// ownCFlags slot (see composeOwnAndPeerCFlagsAtOwnSlot).
	PeerCFlagsGlobal []string
	// PeerCXXFlagsGlobal: transitive union of every PEERDIR's GLOBAL
	// CXXFLAGS. C++ sources only (.cpp/.cc/.cxx).
	PeerCXXFlagsGlobal []string
	// PeerCOnlyFlagsGlobal: transitive union of every PEERDIR's GLOBAL
	// CONLYFLAGS. C / .S sources only.
	PeerCOnlyFlagsGlobal []string
	// AutoPeerCFlags is the auto-injected peer-CFLAG set derived from
	// cliDefines + module flags. The load-bearing entry today is
	// `-D_musl_` (mirror of `build/ymake.core.conf:781`'s
	// `when ($MUSL == "yes") { CFLAGS+=-D_musl_ }`). Kept separate
	// from PeerCFlagsGlobal so the source/from-where is auditable.
	AutoPeerCFlags []string
	// CFlags is the module's own non-GLOBAL CFLAGS. Applies to BOTH C
	// and C++ sources (mirror of upstream's CFLAGS-applies-to-both
	// rule). Slotted between commonDefines and the first
	// noLibcUndebugBlock copy.
	CFlags []string
	// OwnCFlagsGlobal is the module's own GLOBAL CFLAGS. Emitted via
	// the bucket model in composeTargetCC / composeHostCC. Also
	// peer-propagates to consumers via PeerCFlagsGlobal through the
	// walker's two-phase aggregation, not this slot.
	OwnCFlagsGlobal []string
	// OwnCXXFlagsGlobal is the module's own GLOBAL CXXFLAGS (C++
	// only). libcxx's `CXXFLAGS(GLOBAL -nostdinc++)` lands here.
	OwnCXXFlagsGlobal []string
	// OwnCOnlyFlagsGlobal is the module's own GLOBAL CONLYFLAGS
	// (C / .S sources only).
	OwnCOnlyFlagsGlobal []string
	// SFlags is the module's own SFLAGS bundle from `SET_APPEND(SFLAGS
	// ...)`. Slotted by composeASCmdArgs immediately before the
	// trailing `-c -o <out> <in>` block, mirroring upstream
	// `$CFLAGS $SFLAGS $SRCFLAGS -c -o ...` at
	// `build/ymake.core.conf:3217`.
	SFlags []string
	// PerSourceCFlags is the per-source extra CFLAGS attached via the
	// `SRC(filename extra_cflags...)` macro. Slotted BETWEEN
	// `macroPrefixMapFlags` and the input path. Empty for plain SRCS /
	// SRC_C_NO_LTO / JOIN_SRCS / GLOBAL_SRCS.
	PerSourceCFlags []string
	// FlatOutput selects a flat output-path layout (no `_/` infix even
	// when srcRel contains `/`). Set for upstream `SRC(...)` and
	// `SRC_C_NO_LTO(...)`. Empirical: `SRCS(digest/city.cpp)` →
	// `util/_/digest/city.cpp.o`; `SRC_C_NO_LTO(system/compiler.cpp)` →
	// `util/system/compiler.cpp.o`.
	FlatOutput bool
	// DefaultVars is the per-module DEFAULT(name value) map collected
	// from the ya.make. Used by EmitCF to expand $CFG_VARS. Keys are
	// variable names; values are the DEFAULT-declared values.
	DefaultVars     map[string]string
	DefaultVarOrder []string
	// Py3Suffix selects ".py3.o" as output suffix. Set for
	// PY23_NATIVE_LIBRARY modules whose reference emits <src>.py3.o.
	// PIC combines with it as ".py3.pic.o".
	Py3Suffix bool
	// ForceCxx routes generated sources with non-standard extensions
	// through the C++ compile pipeline. Upstream SRCS(GLOBAL *.auxcpp)
	// generated by RESOURCE raw packer compiles with clang++ and
	// trailing "-x c++" even though ".auxcpp" is not a normal C++ suffix.
	ForceCxx bool
	// ModuleTag, when present, adds `module_tag=<ModuleTag>` to
	// target_properties. PROTO_LIBRARY CCs consuming .pb.cc / .ev.pb.cc
	// carry `cpp_proto`; regular LIBRARY CCs leave this nil.
	ModuleTag *string
	// Variant marks this compile as a SIMD permutation of `srcRel`
	// emitted via one of the `SRC_C_AVX / SSE2 / SSE3 / SSSE3 / SSE4 /
	// SSE41 / XOP` macros. When present the output path becomes
	// `<srcRel>.<variant><suffix>` (flat) and PerSourceCFlags carries
	// the `-m<flag>` bundle plus any extra `-DSUFFIX=…`.
	Variant *string
	// Ragel6Flags is the per-module `SET(RAGEL6_FLAGS <value>)`
	// override threaded into EmitR6. When empty the platform default
	// fires (`-CG2` on x86_64 host / `-CT0` on aarch64 target — mirror
	// of `build/ymake_conf.py:2271-2277`'s
	// `set_default_flags(optimized)`).
	Ragel6Flags []string
	// BisonGenExt is ".c" for BISON_GEN_C and ".cpp" by default.
	BisonGenExt string
}

// EmitCC emits a CC node for compiling `srcRel` (relative to
// `instance.Path`, e.g. "lib.c" or "src/algorithm.cpp") into an object
// file. Returns the NodeRef (so callers — typically AR — can wire it
// as a dependency) plus the output path. `in` carries per-module
// knobs; pass `ModuleCCInputs{}` for flag-less behaviour.
func EmitCC(instance ModuleInstance, srcRel string, in ModuleCCInputs, hostP *Platform, emit Emitter) (NodeRef, VFS) {

	suffix := ".o"
	if instance.Flags.PIC {
		suffix = ".pic.o"
	}
	if in.Py3Suffix {
		if instance.Flags.PIC {
			suffix = ".py3.pic.o"
		} else {
			suffix = ".py3.o"
		}
	}

	// SIMD permutation: prefix the suffix with `.<variant>` so the
	// output path becomes `<srcRel>.<variant><suffix>`
	// (e.g. `<src>.avx.pic.o`, `<src>.sse41.pic.o`).
	if in.Variant != nil {
		suffix = "." + *in.Variant + suffix
	}

	outVFS, inVFS := composeCCPaths(instance, srcRel, in, suffix)
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	// No-stdinc modules own the full include set and libc CFLAGS via
	// their ya.make; they take a dedicated composer path with
	// composeNoStdIncIncludes instead of the ccIncludesPrefix/suffix
	// pair, and dispatch through composeNoStdIncCC{,Host}.
	noStdInc := instance.Flags.NoStdInc
	isCxx := in.ForceCxx || isCxxSource(srcRel)

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
	if isCxx && len(instance.Platform.CXXFlags) > 0 {
		ownExtras = append(append([]string{}, ownExtras...), instance.Platform.CXXFlags...)
	}

	var cmdArgs []string

	// ADDINCL slot order: own ADDINCL BEFORE ccIncludesSuffix
	// (linux-headers); peer-propagated GLOBAL ADDINCL AFTER it.

	// One composer for every CC: host, target, no-stdinc all funnel
	// through composeTargetCC with platform-specific differences
	// expressed via Platform / ccComposeArgs fields.
	var autoPeerCFlags, peerExtras, ownGlobalBucket, ownCFlags []string

	if noStdInc {
		// No-stdinc modules' own CFLAGS + GLOBAL CFLAGS (e.g.
		// contrib/libs/musl/ya.make's 8 own flags + GLOBAL -D_musl_=1)
		// occupy the same own-CFLAGS slot as a regular module's
		// composeOwnAndPeerCFlagsAtOwnSlot output.
		ownCFlags = make([]string, 0, len(in.CFlags)+len(in.OwnCFlagsGlobal))
		ownCFlags = append(ownCFlags, in.CFlags...)
		ownCFlags = append(ownCFlags, in.OwnCFlagsGlobal...)
	} else {
		autoPeerCFlags = in.AutoPeerCFlags
		peerExtras = composePeerExtras(in, isCxx)
		ownGlobalBucket = composeOwnAndPeerGlobalBucket(in, isCxx)
		ownCFlags = composeOwnAndPeerCFlagsAtOwnSlot(in, instance.Platform)
	}

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
	cmdArgs = composeTargetCC(args)

	// Reference graph carries the same env map at both cmd-level and
	// Node top-level. EmitCC is single-shot so the alias is safe;
	// future mutators MUST clone before mutating.
	env := hostP.ToolEnv()

	// node.Inputs order: primary source first, then include-inputs in
	// DFS-discovery order (scanner does no sorting; L2 is multiset).
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
			if in.ModuleTag != nil {
				tp["module_tag"] = *in.ModuleTag
			}
			return tp
		}(),
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		// Numeric values are float64 to match encoding/json's default
		// when unmarshalling into `map[string]interface{}`. Int
		// literals would make reflect.DeepEqual against the reference
		// spuriously false-fail even though the on-disk JSON matches.
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	// When HasGenerator is set, thread Generator into DepRefs so the
	// CC carries an explicit dep on its source-generating JS/R6 node
	// (every JS/R6-derived CC in the reference has Deps=[gen UID]).
	// ExtraDepRefs threads additional cross-EN dep refs so the Deps
	// multiset matches the reference for codegen-downstream CCs.
	if in.HasGenerator {
		node.DepRefs = append([]NodeRef{in.Generator}, in.ExtraDepRefs...)
	} else if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = append([]NodeRef(nil), in.ExtraDepRefs...)
	}

	return emit.Emit(node), outVFS
}

// composeCCPaths derives (outputPath, inputPath) per SRCDIR-aware
// semantics. Three shapes:
//  1. No SRCDIR: output `$(B)/<instance.Path>/<rel>.o` (`_/` infix when
//     srcRel contains "/"); input `$(S)/<instance.Path>/<srcRel>` (or
//     `$(B)/...` when IsGenerated).
//  2. SRCDIR set, source resolves locally: SRCDIR ignored (same as 1).
//  3. SRCDIR set, non-local: input `$(S)/<srcdir>/<srcRel>`; output
//     `$(B)/<instance.Path>/__/<rel>.o` with `..` rendered as `__`.
//
// IsGenerated skips case (3) — generators emit to
// `$(B)/<srcInstance.Path>/<rel>` where srcInstance is already
// SRCDIR-aware.
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

	// SRCDIR routing.
	useSrcDir := in.SrcDir != nil && *in.SrcDir != instance.Path && !sourceExistsLocally(in.SourceRoot, instance.Path, srcRel)

	if useSrcDir {
		outputRel := composeSrcDirOutputRel(instance.Path, *in.SrcDir, srcRel)
		return Build(instance.Path + "/" + outputRel + suffix), Source(*in.SrcDir + "/" + srcRel)
	}

	var outRel string

	switch {
	case in.FlatOutput:
		// SRC / SRC_C_NO_LTO emit a flat output path even when
		// `srcRel` contains a `/` (no `_/` infix).
		outRel = instance.Path + "/" + srcRel + suffix
	case strings.Contains(srcRel, "/"):
		outRel = instance.Path + "/_/" + srcRel + suffix
	default:
		outRel = instance.Path + "/" + srcRel + suffix
	}

	return Build(outRel), Source(instance.Path + "/" + srcRel)
}

// sourceExistsLocally reports whether `<sourceRoot>/<modulePath>/<srcRel>`
// is a regular file — distinguishes composeCCPaths cases (2) and (3).
// Empty sourceRoot returns false (synthetic-test path); tests wanting
// local-resolution shape leave SrcDir nil, not SourceRoot empty.
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

// composeSrcDirOutputRel computes case-3 output infix: relative path
// from `instancePath` to `srcDir/srcRel` with `..` segments rendered
// as `__`. Empirical matches: libcxxabi-parts (`__/libcxxabi/src/
// abort_message.cpp`), tcmalloc/no_percpu_cache (`__/tcmalloc/
// want_hpaa.cc`).
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
	// rendering for SRCDIR-redirected outputs that go outside the
	// module dir. When there are NO `..` segments, the target is
	// under instancePath; ymake still uses a `_/` prefix
	// (mirroring the non-SRCDIR `_/` infix). Without it, openssl's
	// `SRCDIR(crypto)` + `../asm/aarch64/...` would emit to
	// `openssl/asm/...` instead of `openssl/_/asm/...`.
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
// language.
func pickCompiler(tools Toolchain, isCxx bool) string {
	if isCxx {
		return tools.CXX
	}

	return tools.CC
}

// pickWarningFlags substitutes the 1-arg `-Wno-everything` bundle for
// the full `-Werror`/`-Wall`/... set when the module declares
// NO_COMPILER_WARNINGS.
func pickWarningFlags(noCompilerWarnings bool) []string {
	if noCompilerWarnings {
		return noWarningsBundle
	}

	return warningFlags
}

// appendCxxStdAndOwn appends the per-source-language tail AFTER the
// 2nd suppression-block copy and BEFORE the bucket / peerExtras /
// builtinMacroDateTime trailer: `-std=c++20` for C++, then for C++
// either the cxxStandardWarnings bundle (or its NoCompilerWarnings
// replacement `-Wno-everything`), then own non-GLOBAL CXXFLAGS /
// CONLYFLAGS.
//
// `injectCxxWarningBundle` gates the warning bundle injection. Pass
// true for target/host composers; false for no-stdinc composers (they
// emitted the warning bundle earlier in the pipeline).
func appendCxxStdAndOwn(cmdArgs []string, isCxx bool, noCompilerWarnings bool, injectCxxWarningBundle bool, ownExtras []string) []string {
	if isCxx {
		cmdArgs = append(cmdArgs, cxxStandardFlag)

		if injectCxxWarningBundle {
			if noCompilerWarnings {
				// `-Wno-everything` replaces the cxx-warning-bundle
				// when NO_COMPILER_WARNINGS is set (libcxx
				// algorithm.cpp.o cmd_args[99]).
				cmdArgs = append(cmdArgs, noWarningsBundle...)
			} else {
				// Every clang C++ compile without NO_COMPILER_WARNINGS
				// gets the 10-arg cxxStandardWarnings bundle
				// (util/charset/all_charset.cpp.o cmd_args[102..111]).
				cmdArgs = append(cmdArgs, cxxStandardWarnings...)
			}
		}
	}

	cmdArgs = append(cmdArgs, ownExtras...)

	return cmdArgs
}

// composePeerExtras returns the peer-propagated GLOBAL CXXFLAGS /
// CONLYFLAGS contribution per source-language axis. The CFlags axis
// itself lives in the ownCFlags slot (see
// composeOwnAndPeerCFlagsAtOwnSlot). AutoPeerCFlags (e.g. -D_musl_)
// is NOT included here — it slots separately via the
// `autoPeerCFlags` composer argument.
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

// composeOwnAndPeerCFlagsAtOwnSlot assembles the combined CFLAGS
// bundle landing at the ownCFlags slot (between commonDefines and the
// 1st noLibcUndebugBlock / ndebugPicBlock copy). Carries ALL CFLAGS
// axes — own non-GLOBAL, own GLOBAL, peer-propagated GLOBAL — applying
// to both C and C++ sources of the consumer.
//
// Order: [own non-GLOBAL, peer-GLOBAL, own GLOBAL]. No dedup — the
// reference preserves duplicates (e.g. openssl's `-DOPENSSL_BUILD=1`
// from top-level CFLAGS and crypto/ya.make.inc).
func composeOwnAndPeerCFlagsAtOwnSlot(in ModuleCCInputs, p *Platform) []string {
	out := make([]string, 0, len(in.CFlags)+len(p.CFlags)+len(in.PeerCFlagsGlobal)+len(in.OwnCFlagsGlobal))
	out = append(out, in.CFlags...)
	out = append(out, p.CFlags...)
	out = append(out, in.PeerCFlagsGlobal...)
	out = append(out, in.OwnCFlagsGlobal...)

	return out
}

func platformCompilerFlags(p *Platform, isCxx bool) []string {
	if len(p.CFlags) == 0 && (!isCxx || len(p.CXXFlags) == 0) {
		return nil
	}

	out := make([]string, 0, len(p.CFlags)+len(p.CXXFlags))
	out = append(out, p.CFlags...)
	if isCxx {
		out = append(out, p.CXXFlags...)
	}

	return out
}

// baseUnitCxxNostdinc is `_BASE_UNIT.CXXFLAGS += -nostdinc++` from
// `build/ymake.core.conf:807`. Applies to every _BASE_UNIT-derived
// module in the default closure (USE_STL_SYSTEM != "yes" && MSVC !=
// "yes"). The injection lands ONLY at the post-catboost bucket slot,
// NEVER deduped against own-extras: a module with its own
// `CXXFLAGS(-nostdinc++)` emits the flag at both slots.
const baseUnitCxxNostdinc = "-nostdinc++"

// composeOwnAndPeerGlobalBucket assembles the (own GLOBAL ∪ peer
// GLOBAL) CXXFLAGS / CONLYFLAGS bucket per source-language axis. C++
// sources emit this bucket flanking `-DCATBOOST_OPENSOURCE=yes` (the
// catboost-redux); the post-catboost half is augmented with
// `baseUnitCxxNostdinc` via composePostCatboostBucket. C sources emit
// no redux. The CFlags axis lives in the ownCFlags slot, NOT here
// (composeOwnAndPeerCFlagsAtOwnSlot).
//
// Dedup is first-occurrence-wins: an own-GLOBAL flag also present in
// peer-GLOBAL appears once, in the own slot.
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
// bucket-twice slot: preBucket (own GLOBAL ∪ peer GLOBAL) plus the
// `_BASE_UNIT.CXXFLAGS += -nostdinc++` injection (deduped first-wins).
// libcxx / abseil keep identical halves (preBucket already carries it);
// libcxxrt / libcxxabi-parts / libunwind gain `-nostdinc++` on the
// post half only.
//
// Caller MUST invoke only for non-musl C++ compiles — musl skips the
// bucket entirely and C sources have no catboost-redux.
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

// composeTargetCC composes the cmd_args bundle for a TARGET no-libc
// CC. Pinned byte-exact against the reference graph.
//
// Slot layout (in addition to the static blocks):
//   - `ownCFlags`: own non-GLOBAL CFLAGS, between commonDefines and
//     the 1st noLibcUndebugBlock.
//   - `autoPeerCFlags`: between catboost and 2nd noLibcUndebugBlock.
//   - C++ only: `ownGlobalBucket` twice flanking a second
//     `-DCATBOOST_OPENSOURCE=yes`, AFTER own CXXFLAGS / CONLYFLAGS.
//   - C only: `peerExtras` once, no catboost-redux.
//   - cxxStandardWarnings bundle injected by appendCxxStdAndOwn for
//     C++ without NoCompilerWarnings.
//
// ccComposeArgs is the parameter bundle — every entry is []string or
// string, so type mismatch wouldn't surface as a compile error if
// passed positionally.
type ccComposeArgs struct {
	Platform           *Platform
	OutputPath         string
	InputPath          string
	OwnAddIncl         []VFS
	PeerAddIncl        []VFS
	OwnCFlags          []string
	OwnExtras          []string
	AutoPeerCFlags     []string
	PeerExtras         []string
	OwnGlobalBucket    []string
	PerSrcCFlags       []string
	IsCxx              bool
	NoCompilerWarnings bool
}

func appendAutoPeerAndCPUFeatures(cmdArgs []string, bundle compileFlagBundle, autoPeerCFlags []string) []string {
	if !bundle.SplitAutoPeerAroundCPU {
		cmdArgs = append(cmdArgs, autoPeerCFlags...)
		cmdArgs = append(cmdArgs, bundle.CPUFeatures...)

		return cmdArgs
	}

	preSse, postSse := partitionPython3FromAutoPeer(autoPeerCFlags)
	cmdArgs = append(cmdArgs, preSse...)
	cmdArgs = append(cmdArgs, bundle.CPUFeatures...)
	cmdArgs = append(cmdArgs, postSse...)

	return cmdArgs
}

// appendCompileFlagPipeline appends the shared ordered compile-flag
// backbone used by the compose*CC variants. Callers keep ownership of
// prologue/include slots and the language-specific tail.
func appendCompileFlagPipeline(cmdArgs []string, bundle compileFlagBundle, warningBundle, defineBundle, preNoLibcExtras, autoPeerCFlags []string) []string {
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, bundle.CFlags...)
	cmdArgs = append(cmdArgs, warningBundle...)
	cmdArgs = append(cmdArgs, defineBundle...)
	cmdArgs = append(cmdArgs, preNoLibcExtras...)
	cmdArgs = append(cmdArgs, bundle.NoLibcBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = appendAutoPeerAndCPUFeatures(cmdArgs, bundle, autoPeerCFlags)
	cmdArgs = append(cmdArgs, bundle.NoLibcBlock...)

	return cmdArgs
}

func composeTargetCC(a ccComposeArgs) []string {
	bundle := compileFlagBundleFor(a.Platform)
	cmdArgs := make([]string, 0, 101+len(a.OwnAddIncl)+len(a.PeerAddIncl)+len(a.OwnCFlags)+len(a.OwnExtras)+len(a.AutoPeerCFlags)+len(a.PeerExtras)+2*len(a.OwnGlobalBucket)+len(a.PerSrcCFlags)+4)
	cmdArgs = append(cmdArgs,
		pickCompiler(a.Platform.Tools, a.IsCxx),
		"--target="+a.Platform.Triple,
	)
	cmdArgs = append(cmdArgs, bundle.ArchArgs...)
	cmdArgs = append(cmdArgs,
		"-B"+binPath,
		"-c",
		"-o",
		a.OutputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludesPrefix...)
	cmdArgs = appendAddIncl(cmdArgs, a.OwnAddIncl)
	cmdArgs = append(cmdArgs, ccIncludesSuffix...)
	cmdArgs = appendAddIncl(cmdArgs, a.PeerAddIncl)
	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, pickWarningFlags(a.NoCompilerWarnings), bundle.Defines, a.OwnCFlags, a.AutoPeerCFlags)

	// C sources: CONLYFLAGS (ownExtras) trails AFTER
	// macroPrefixMapFlags — base64 neon32/64/plain32/64 CC nodes show
	// CONLYFLAGS at cmd_args[107..108], after the three fmacro-prefix-
	// map flags. Hold for the trailer; do NOT pass to
	// appendCxxStdAndOwn. C++ slot order is correct as-is.
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
		// C source: no catboost-redux. peerExtras is sufficient;
		// own GLOBAL CFLAGS / CONLYFLAGS for C are unused in the
		// current closure.
		cmdArgs = append(cmdArgs, a.PeerExtras...)
	}

	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	// Per-source extra CFLAGS (from `SRC(filename extra_cflags...)`)
	// slot BETWEEN macroPrefixMapFlags and the input path.
	cmdArgs = append(cmdArgs, a.PerSrcCFlags...)
	// C-source CONLYFLAGS trail after macroPrefixMapFlags and after
	// perSrcCFlags.
	cmdArgs = append(cmdArgs, cOnlyExtras...)
	cmdArgs = append(cmdArgs, a.InputPath)

	return cmdArgs
}

// partitionPython3FromAutoPeer splits autoPeerCFlags into pre-SSE and
// post-SSE halves for the HOST compose path. `-DUSE_PYTHON3` is
// routed via defaultPeerCFlags but the host reference places it
// AFTER hostSseFeatures (between -mcx16 and the 2nd ndebugPicBlock).
// `-D_musl_` keeps its pre-SSE slot.
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

// appendAddIncl prepends `-I$(S)/` to each ADDINCL path and appends
// to cmdArgs. Paths are SOURCE_ROOT-relative; order is preserved
// (declaration order matters for `include_next` chains).
//
// Paths already starting with `$(B)/` (auto-injected by
// `${addincl;noauto;output:NAME}` for ARCHIVE() consumers — e.g.
// library/python/runtime_py3's build-tree dir) pass through verbatim
// under a literal `-I` prefix; SOURCE_ROOT wrapping would produce
// `-I$(S)/$(B)/…` which mismatches REF.
func appendAddIncl(cmdArgs []string, addIncl []VFS) []string {
	for _, p := range addIncl {
		cmdArgs = append(cmdArgs, includeArg(p))
	}

	return cmdArgs
}

func includeArg(path VFS) string {
	return "-I" + path.String()
}

// composeNoStdIncIncludes builds the no-stdinc include tail:
// `-I$(B) -I$(S)` + OWN ADDINCL + linux-headers.
//
// For musl-self this means the six paths declared directly in
// contrib/libs/musl/ya.make arrive through `addIncl`, rather than via
// a separate hardcoded list in the composer.
func composeNoStdIncIncludes(addIncl []VFS) []string {
	out := make([]string, 0, len(ccIncludesPrefix)+len(addIncl)+len(ccIncludesSuffix))
	out = append(out, ccIncludesPrefix...)
	out = appendAddIncl(out, addIncl)
	out = append(out, ccIncludesSuffix...)

	return out
}
