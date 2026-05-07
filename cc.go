package main

// cc.go — emitter for CC compilation nodes.
//
// PR-29 reshaped the signature: `EmitCC` now takes a
// `ModuleCCInputs` struct alongside `ModuleInstance` and `srcRel`.
// The struct carries per-module knobs that vary across modules in the
// same closure but stay constant for a single (instance, source)
// pair: ADDINCL paths, own CXXFLAGS, own CONLYFLAGS, the
// "is generated" bit (input lives under $(BUILD_ROOT) instead of
// $(SOURCE_ROOT)), and a `Generator NodeRef` reserved for PR-30; not wired today.
// Extending the struct does not require updating every call site —
// that is the whole point of switching from a positional signature.
//
// PR-23/25 history:
//
//   - PR-23 retrofitted the signature to take a `ModuleInstance`
//     instead of a (PlatformConfig, moduleDir) pair.
//   - PR-25 wired the walker into musl, where the third
//     `composeMuslCC` flavor activates.
//   - PR-29 adds a fourth flavor (`composeMuslHostCC`) for host-musl
//     PIC nodes — the dominant L3 lift lever (1297 nodes in the
//     archiver closure flip from "diverges" to "byte-exact").
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
//   - Generator: NodeRef forward-scaffolding for PR-30; no caller sets this
//     in PR-29 — see cc.go:196-201 for L0-deferral rationale.
//
// D04 (peer-propagated GLOBAL ADDINCL/CXXFLAGS) is deferred to PR-30.
type ModuleCCInputs struct {
	AddIncl     []string
	CXXFlags    []string
	COnlyFlags  []string
	IsGenerated bool
	Generator   NodeRef
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

	var outputPath string
	if strings.Contains(srcRel, "/") {
		outputPath = "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + suffix
	} else {
		outputPath = "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel + suffix
	}

	rootPrefix := "$(SOURCE_ROOT)/"
	if in.IsGenerated {
		rootPrefix = "$(BUILD_ROOT)/"
	}

	inputPath := rootPrefix + instance.Path + "/" + srcRel

	isMusl := instance.Path == "contrib/libs/musl" || strings.HasPrefix(instance.Path, "contrib/libs/musl/")
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

	switch {
	case isMusl && instance.Flags.PIC:
		cmdArgs = composeMuslHostCC(outputPath, inputPath, nil, muslOwnExtras, isCxx)
	case isMusl:
		cmdArgs = composeMuslCC(outputPath, inputPath, nil, muslOwnExtras, isCxx)
	case instance.Flags.PIC:
		cmdArgs = composeHostCC(outputPath, inputPath, in.AddIncl, ownExtras, isCxx, instance.Flags.NoCompilerWarnings)
	default:
		cmdArgs = composeTargetCC(outputPath, inputPath, in.AddIncl, ownExtras, isCxx, instance.Flags.NoCompilerWarnings)
	}

	// The reference graph carries the same env map at both the cmd
	// level and the top level of the Node. Build it once and reuse;
	// EmitCC is single-shot so the alias is safe today. Future PRs
	// that mutate emitted nodes post-emit MUST clone before mutating.
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  []string{inputPath},
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

	// PR-29-D07: when `Generator` is non-zero, thread it into DepRefs
	// so the CC node carries an explicit dep on its source-generating
	// JS/R6 node (the reference shape). The walker passes
	// IsGenerated=true alone to flip ONLY inputPath without touching
	// the topology — see gen.go's JS/R6 branches for why threading
	// the Generator costs L0 multiset matches in the current closure.
	if in.IsGenerated && in.Generator != (NodeRef{}) {
		node.DepRefs = []NodeRef{in.Generator}
	}

	return emit.Emit(node), outputPath
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

// composeTargetCC composes the cmd_args bundle for a TARGET-flavoured
// no-libc CC compilation. Pinned byte-exact (101 args, no per-module
// extras) against build/cow/on/lib.c.o in
// /home/pg/monorepo/yatool_orig/g.json.
func composeTargetCC(outputPath, inputPath string, addIncl, ownExtras []string, isCxx, noCompilerWarnings bool) []string {
	cmdArgs := make([]string, 0, 101+len(addIncl)+len(ownExtras)+2)
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
	cmdArgs = appendAddIncl(cmdArgs, addIncl)
	cmdArgs = append(cmdArgs, ccIncludesSuffix...)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)
	cmdArgs = append(cmdArgs, pickWarningFlags(noCompilerWarnings)...)
	cmdArgs = append(cmdArgs, commonDefines...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = appendCxxStdAndOwn(cmdArgs, isCxx, noCompilerWarnings, true, ownExtras)
	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	cmdArgs = append(cmdArgs, inputPath)

	return cmdArgs
}

// composeHostCC composes the cmd_args bundle for a HOST-flavoured PIC
// CC compilation. Pinned byte-exact (105 args, no per-module extras)
// against build/cow/on/lib.c.pic.o in
// /home/pg/monorepo/yatool_orig/g.json.
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
func composeHostCC(outputPath, inputPath string, addIncl, ownExtras []string, isCxx, noCompilerWarnings bool) []string {
	cmdArgs := make([]string, 0, 105+len(addIncl)+len(ownExtras)+2)
	cmdArgs = append(cmdArgs,
		pickCompiler(isCxx),
		"--target="+hostTriple,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludesPrefix...)
	cmdArgs = appendAddIncl(cmdArgs, addIncl)
	cmdArgs = append(cmdArgs, ccIncludesSuffix...)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, hostCFlags...)
	cmdArgs = append(cmdArgs, pickWarningFlags(noCompilerWarnings)...)
	cmdArgs = append(cmdArgs, hostDefines...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, hostSseFeatures...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = appendCxxStdAndOwn(cmdArgs, isCxx, noCompilerWarnings, true, ownExtras)
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
