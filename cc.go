package main

// cc.go — emitter for CC compilation nodes.
//
// PR-29 reshaped the signature: `EmitCC` now takes a
// `ModuleCCInputs` struct alongside `ModuleInstance` and `srcRel`.
// The struct carries per-module knobs that vary across modules in the
// same closure but stay constant for a single (instance, source)
// pair: ADDINCL paths, own CXXFLAGS, own CONLYFLAGS, the
// "is generated" bit (input lives under $(BUILD_ROOT) instead of
// $(SOURCE_ROOT)), and a `Generator NodeRef` (PR-30 D04: wired to the
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
//   - Flat source: `$(BUILD_ROOT)/<path>/<srcRel><.o|.pic.o>`
//   - Nested source (contains "/"): `$(BUILD_ROOT)/<path>/_/<srcRel><.o|.pic.o>`
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
//   - IsGenerated: source lives under $(BUILD_ROOT)/... (D07)
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
	IncludeInputs []string
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
func EmitCC(instance ModuleInstance, srcRel string, in ModuleCCInputs, emit Emitter) (NodeRef, string) {
	suffix := ".o"
	if instance.Flags.PIC {
		suffix = ".pic.o"
	}

	outputPath, inputPath := composeCCPaths(instance, srcRel, in, suffix)

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
	var autoPeerCFlags, peerExtras []string

	if !isMusl {
		autoPeerCFlags = in.AutoPeerCFlags
		peerExtras = composePeerExtras(in, isCxx)
	}

	switch {
	case isMusl && instance.Flags.PIC:
		cmdArgs = composeMuslHostCC(outputPath, inputPath, nil, muslOwnExtras, isCxx)
	case isMusl:
		cmdArgs = composeMuslCC(outputPath, inputPath, nil, muslOwnExtras, isCxx)
	case instance.Flags.PIC:
		cmdArgs = composeHostCC(outputPath, inputPath, in.AddIncl, in.PeerAddInclGlobal, ownExtras, autoPeerCFlags, peerExtras, isCxx, instance.Flags.NoCompilerWarnings)
	default:
		cmdArgs = composeTargetCC(outputPath, inputPath, in.AddIncl, in.PeerAddInclGlobal, ownExtras, autoPeerCFlags, peerExtras, isCxx, instance.Flags.NoCompilerWarnings)
	}

	// The reference graph carries the same env map at both the cmd
	// level and the top level of the Node. Build it once and reuse;
	// EmitCC is single-shot so the alias is safe today. Future PRs
	// that mutate emitted nodes post-emit MUST clone before mutating.
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	// PR-31 D09: prepend the resolved transitive header set to
	// node.Inputs after the primary source path. The order is
	// primary source first, then include-inputs in DFS-discovery
	// order (the scanner does no sorting; L2 compares as multiset).
	allInputs := make([]string, 0, 1+len(in.IncludeInputs))
	allInputs = append(allInputs, inputPath)
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
		Outputs: []string{outputPath},
		KV: map[string]string{
			"p":  "CC",
			"pc": "green",
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Target),
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

	if instance.Flags.PIC {
		// Host build: reference nodes carry `host_platform=true`
		// and `tags=["tool"]`. The "tool" tag distinguishes host
		// nodes that are built specifically to be invoked at
		// build-time (per the reference graph's classification).
		node.HostPlatform = true
		node.Tags = []string{"tool"}
	}

	// PR-30 D04: when `HasGenerator` is set, thread `Generator` into
	// DepRefs so the CC node carries an explicit dep on its source-
	// generating JS/R6 node (matching the reference shape — every
	// JS-derived and R6-derived CC in the reference has Deps=[gen UID]).
	// The HasGenerator flag is required because BufferedEmitter assigns
	// NodeRef ids starting at 0; a bare NodeRef{} comparison would
	// false-negative on the very first emitted node.
	if in.HasGenerator {
		node.DepRefs = []NodeRef{in.Generator}
	}

	return emit.Emit(node), outputPath
}

// composeCCPaths derives the (outputPath, inputPath) pair per PR-30
// D06's SRCDIR-aware semantics. The composer distinguishes three
// shapes empirically observed in the reference graph:
//
//  1. No SRCDIR (the historical case): output is
//     `$(BUILD_ROOT)/<instance.Path>/<rel>.o` (with `_/` infix when
//     srcRel contains "/"); input is
//     `$(SOURCE_ROOT)/<instance.Path>/<srcRel>` (or `$(BUILD_ROOT)/`
//     when IsGenerated).
//  2. SRCDIR set, source resolves locally (file exists at
//     `<sourceRoot>/<instance.Path>/<srcRel>`): SRCDIR is silently
//     ignored — same as case (1). Empirical examples: musl_extra's
//     `all.c`, tcmalloc/no_percpu_cache's `aligned_alloc.c`.
//  3. SRCDIR set, source does not resolve locally: input is
//     `$(SOURCE_ROOT)/<srcdir>/<srcRel>`; output is
//     `$(BUILD_ROOT)/<instance.Path>/__/<rel>.o` where `<rel>` is the
//     relative path from instance.Path to (srcdir+srcRel), with `..`
//     segments rendered as `__`. Empirical examples: libcxxabi-parts's
//     `src/abort_message.cpp` (sibling SRCDIR), tcmalloc/no_percpu_cache's
//     `tcmalloc/want_hpaa.cc` (ancestor SRCDIR + nested src path).
//
// Generated sources (IsGenerated=true) skip case (3) — generators emit
// to `$(BUILD_ROOT)/<srcInstance.Path>/<rel>` where srcInstance is
// already SRCDIR-aware (the JS/R6 emitter rebased it before invocation).
func composeCCPaths(instance ModuleInstance, srcRel string, in ModuleCCInputs, suffix string) (string, string) {
	if in.IsGenerated {
		// Generators (JS/R6) write under $(BUILD_ROOT)/<srcInstance.Path>/.
		// SrcDir handling for those branches is upstream (in gen.go's
		// JOIN_SRCS / .rl6 dispatch where srcInstance is constructed).

		var outputPath string

		if strings.Contains(srcRel, "/") {
			outputPath = "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + suffix
		} else {
			outputPath = "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel + suffix
		}

		inputPath := "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel

		return outputPath, inputPath
	}

	// PR-30 D06 SRCDIR routing.
	useSrcDir := in.SrcDir != "" && in.SrcDir != instance.Path && !sourceExistsLocally(in.SourceRoot, instance.Path, srcRel)

	if useSrcDir {
		outputRel := composeSrcDirOutputRel(instance.Path, in.SrcDir, srcRel)
		outputPath := "$(BUILD_ROOT)/" + instance.Path + "/" + outputRel + suffix
		inputPath := "$(SOURCE_ROOT)/" + in.SrcDir + "/" + srcRel

		return outputPath, inputPath
	}

	var outputPath string

	if strings.Contains(srcRel, "/") {
		outputPath = "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + suffix
	} else {
		outputPath = "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel + suffix
	}

	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

	return outputPath, inputPath
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
	// SRCDIR-redirected outputs).
	parts := strings.Split(rel, string(filepath.Separator))

	for i, p := range parts {
		if p == ".." {
			parts[i] = "__"
		}
	}

	return strings.Join(parts, "/")
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
func pickCompiler(isCxx bool) string {
	if isCxx {
		return cxxCompilerPath
	}

	return ccCompilerPath
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
// AFTER the second suppression-block copy and BEFORE
// builtinMacroDateTime: `-std=c++20` for C++ sources (D05) followed by
// the module's own CXXFLAGS / CONLYFLAGS (D02). For libcxx-style
// modules the reference graph also injects `-Wno-everything` here as
// the cxx-warning-bundle slot's NoCompilerWarnings replacement; the
// helper handles both shapes.
//
// `injectCxxWarningBundle` controls whether the `-Wno-everything`
// cxx-warning-bundle is injected when isCxx && noCompilerWarnings.
// Pass true for target/host composers (current behaviour); pass false
// for musl composers that already added muslWarningFlags earlier to
// suppress duplicate injection.
//
// Slot ordering verified against:
//   - libcxx algorithm.cpp.o cmd_args[98..100]:
//     `-std=c++20 -Wno-everything -D_LIBCPP_BUILDING_LIBRARY`
//     (NoCompilerWarnings=true; cxxStandardFlag, then cxx warning
//     bundle replacement, then own CXXFLAGS).
//   - getopt/small completer.cpp.o cmd_args[104..]:
//     `-std=c++20` followed by peer-propagated cxx warning extension
//     bundle (D04 deferred) then peer-propagated GLOBAL CXXFLAGS
//     (D04 deferred). Own CXXFLAGS slot is between -std=c++20 and
//     the peer-GLOBAL pieces.
//
// The peer-GLOBAL pieces (libcxx's cxx warning extension bundle,
// `-nostdinc++`, second `-DCATBOOST_OPENSOURCE=yes`) are D04 and
// out-of-scope here. Their absence keeps libcxx own-CXXFLAGS at L3
// divergent until PR-30.
func appendCxxStdAndOwn(cmdArgs []string, isCxx bool, noCompilerWarnings bool, injectCxxWarningBundle bool, ownExtras []string) []string {
	if isCxx {
		cmdArgs = append(cmdArgs, cxxStandardFlag)

		if injectCxxWarningBundle && noCompilerWarnings {
			// libcxx slot: cxx-warning-bundle peer-GLOBAL (D04
			// deferred) is replaced by `-Wno-everything` when the
			// consumer module sets NO_COMPILER_WARNINGS. Reference:
			// libcxx algorithm.cpp.o cmd_args[99] = `-Wno-everything`.
			cmdArgs = append(cmdArgs, muslWarningFlags...)
		}
	}

	cmdArgs = append(cmdArgs, ownExtras...)

	return cmdArgs
}

// composePeerExtras assembles the peer-propagated GLOBAL CFLAGS /
// CXXFLAGS / CONLYFLAGS contribution per source-language axis (PR-32
// D08). Source-language filtering follows ymake's CFLAGS-applies-to-
// both rule:
//
//   - PeerCFlagsGlobal applies to both C and C++ sources.
//   - PeerCXXFlagsGlobal applies only to C++ sources.
//   - PeerCOnlyFlagsGlobal applies only to C / .S sources.
//
// AutoPeerCFlags (e.g. -D_musl_) is NOT included here — it slots at
// a different cmd_args position (between catboost and 2nd
// noLibcUndebugBlock); see the dedicated `autoPeerCFlags` argument
// to the composers. Mirror of the PR-31 `combinedAddIncl` ordering
// (peer contributions in declaration order; no own contributions
// here — those are appended via appendCxxStdAndOwn).
func composePeerExtras(in ModuleCCInputs, isCxx bool) []string {
	out := make([]string, 0, len(in.PeerCFlagsGlobal)+len(in.PeerCXXFlagsGlobal)+len(in.PeerCOnlyFlagsGlobal))
	out = append(out, in.PeerCFlagsGlobal...)

	if isCxx {
		out = append(out, in.PeerCXXFlagsGlobal...)
	} else {
		out = append(out, in.PeerCOnlyFlagsGlobal...)
	}

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
// PR-32 D08: `peerExtras` is the peer-propagated GLOBAL CFLAGS /
// CXXFLAGS / CONLYFLAGS slice (composed by composePeerExtras). It
// slots AFTER own CXXFLAGS / CONLYFLAGS and BEFORE
// builtinMacroDateTime — matches the empirical reference slot for
// `-nostdinc++` propagation in libcxx-consuming CC nodes.
func composeTargetCC(outputPath, inputPath string, ownAddIncl, peerAddIncl, ownExtras, autoPeerCFlags, peerExtras []string, isCxx, noCompilerWarnings bool) []string {
	cmdArgs := make([]string, 0, 101+len(ownAddIncl)+len(peerAddIncl)+len(ownExtras)+len(autoPeerCFlags)+len(peerExtras)+2)
	cmdArgs = append(cmdArgs,
		pickCompiler(isCxx),
		"--target="+targetTriple,
		"-march="+archFlag,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludesPrefix...)
	cmdArgs = appendAddIncl(cmdArgs, ownAddIncl)
	cmdArgs = append(cmdArgs, ccIncludesSuffix...)
	cmdArgs = appendAddIncl(cmdArgs, peerAddIncl)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)
	cmdArgs = append(cmdArgs, pickWarningFlags(noCompilerWarnings)...)
	cmdArgs = append(cmdArgs, commonDefines...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, autoPeerCFlags...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = appendCxxStdAndOwn(cmdArgs, isCxx, noCompilerWarnings, true, ownExtras)
	cmdArgs = append(cmdArgs, peerExtras...)
	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	cmdArgs = append(cmdArgs, inputPath)

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
func composeHostCC(outputPath, inputPath string, ownAddIncl, peerAddIncl, ownExtras, autoPeerCFlags, peerExtras []string, isCxx, noCompilerWarnings bool) []string {
	cmdArgs := make([]string, 0, 105+len(ownAddIncl)+len(peerAddIncl)+len(ownExtras)+len(autoPeerCFlags)+len(peerExtras)+2)
	cmdArgs = append(cmdArgs,
		pickCompiler(isCxx),
		"--target="+hostTriple,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludesPrefix...)
	cmdArgs = appendAddIncl(cmdArgs, ownAddIncl)
	cmdArgs = append(cmdArgs, ccIncludesSuffix...)
	cmdArgs = appendAddIncl(cmdArgs, peerAddIncl)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, hostCFlags...)
	cmdArgs = append(cmdArgs, pickWarningFlags(noCompilerWarnings)...)
	cmdArgs = append(cmdArgs, hostDefines...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	// PR-32 D09: autoPeerCFlags (-D_musl_) slots BETWEEN catboost
	// and the SECOND ndebugPicBlock; for host the `hostSseFeatures`
	// block sits between them so the precise slot is BEFORE the SSE
	// bundle on host. Empirical host probe via tools/archiver host
	// CC nodes confirms the same shape (catboost → -D_musl_ →
	// hostSseFeatures → 2nd ndebugPicBlock).
	cmdArgs = append(cmdArgs, autoPeerCFlags...)
	cmdArgs = append(cmdArgs, hostSseFeatures...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = appendCxxStdAndOwn(cmdArgs, isCxx, noCompilerWarnings, true, ownExtras)
	cmdArgs = append(cmdArgs, peerExtras...)
	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	cmdArgs = append(cmdArgs, inputPath)

	return cmdArgs
}

// composeMuslCC composes the cmd_args bundle for a TARGET musl
// (`contrib/libs/musl/...` non-PIC) CC compilation. 111 args with no
// per-module extras. Differs from target in:
//   - `muslCcIncludes` (10 args) replaces `ccIncludes` (4 args)
//   - `muslWarningFlags` (1 arg) replaces `warningFlags` (6 args)
//   - `muslExtraDefines` (9 args) inserted after `commonDefines`,
//     before the noLibc block
func composeMuslCC(outputPath, inputPath string, addIncl, ownExtras []string, isCxx bool) []string {
	cmdArgs := make([]string, 0, 111+len(addIncl)+len(ownExtras)+2)
	cmdArgs = append(cmdArgs,
		pickCompiler(isCxx),
		"--target="+targetTriple,
		"-march="+archFlag,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = appendMuslIncludes(cmdArgs, muslCcIncludes, addIncl)
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
// against `$(BUILD_ROOT)/contrib/libs/musl/_/src/string/strlen.c.pic.o`
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
func composeMuslHostCC(outputPath, inputPath string, addIncl, ownExtras []string, isCxx bool) []string {
	cmdArgs := make([]string, 0, 115+len(addIncl)+len(ownExtras)+2)
	cmdArgs = append(cmdArgs,
		pickCompiler(isCxx),
		"--target="+hostTriple,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = appendMuslIncludes(cmdArgs, muslCcIncludesX8664, addIncl)
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

// appendAddIncl prepends `-I$(SOURCE_ROOT)/` to each ADDINCL path and
// appends them to `cmdArgs` (PR-29-D03). Paths are SOURCE_ROOT-relative
// in ya.make declarations; the composer adds the literal prefix and
// the `-I` flag at emit time. Order is preserved (R14 — declaration
// order matters for `include_next` chains).
func appendAddIncl(cmdArgs []string, addIncl []string) []string {
	for _, p := range addIncl {
		cmdArgs = append(cmdArgs, "-I$(SOURCE_ROOT)/"+p)
	}

	return cmdArgs
}

// appendMuslIncludes splices per-module ADDINCL paths into the musl
// include set. Slot is BETWEEN the prefix `-I$(BUILD_ROOT)
// -I$(SOURCE_ROOT)` (entries [0..1] of muslCcIncludes*) and the body
// of musl arch/include/extra paths plus linux-headers suffix
// (entries [2..]). This matches what builtins fp_mode.c.o shows
// (cmd_args[7..14]: `-I$(BUILD_ROOT) -I$(SOURCE_ROOT) -I<musl/arch/X>
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
