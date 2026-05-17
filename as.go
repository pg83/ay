package main

import (
	"path"
	"strings"
)

// as.go — emitter for AS assembly nodes.
//
// EmitAS produces a single Node for assembling one .S source into an
// object file. yasmLD (host yasm linker NodeRef) is non-nil for asmlib
// `.pic.o` AS nodes (reference shape: foreign_deps.tool=[yasm-host-LD]);
// non-yasm callers pass nil. When non-nil, yasmLD wires into BOTH
// ForeignDepRefs["tool"] and DepRefs — the L0 fingerprint reads only
// deps, so foreign-deps-only diverged for asmlib's 25 AS nodes.
//
// Byte-exact test anchor: cxxsupp/builtins chkstk.S.

// asmlib host-PIC `.asm` sources switch to yasm. Differences from the
// clang AS shape:
//   - Output: flat `<modulePath>/<srcStem>.pic.o` (no `_/` infix).
//   - cmd_args: 18-arg yasm invocation; no clang flags / warnings /
//     defines / ModuleCCInputs includes.
//   - Env: only `ARCADIA_ROOT_DISTBUILD` + `YASM_TEST_SUITE=1` (no
//     `DYLD_LIBRARY_PATH` — yasm has no host-clang library dep).
// Inputs: yasm binary FIRST (reference inputs[0]), then source, then
// transitive includes.

// AS warning-bundle slot tracks NO_COMPILER_WARNINGS like CC: modules
// declaring NO_COMPILER_WARNINGS() emit `-Wno-everything`; regular
// modules preserve the full `-Werror -Wall -Wextra -Wno-*` bundle.
//
// Per-module knobs (own/peer ADDINCL, own non-GLOBAL CFLAGS, auto peer
// CFLAGS) thread via ModuleCCInputs (same struct CC consumes).
// composeASCmdArgs derives:
//   - includes tail = ccIncludesPrefix + AddIncl + ccIncludesSuffix +
//     PeerAddInclGlobal (mirror of CC's includes layout, excluding the
//     musl-self structural override).
//   - own CFLAGS slot between commonDefines and the first
//     suppressionBlock copy.
//   - autoPeerCFlags slot between catboost and the second
//     suppressionBlock copy.

// yasmBinaryVFS is the canonical $(B)-relative host yasm binary path;
// yasmBinaryPath is its String() form. Hardcoded because the only
// consumer is the asmlib host-PIC branch (gated by asmlibYasmModules);
// yasm's PROGRAM directory is stable.
var (
	yasmBinaryVFS  = Build("contrib/tools/yasm/yasm")
	yasmBinaryPath = yasmBinaryVFS.String()
)

// composeASPaths derives (outputPath, inputPath) for the clang AS path
// (mirror of composeCCPaths):
//  1. Flat srcRel: `$(B)/<path>/<srcRel>.o`, `$(S)/<path>/<srcRel>`.
//  2. Nested srcRel no SRCDIR: `$(B)/<path>/_/<srcRel>.o`, same input.
//  3. SRCDIR + non-local source: composeSrcDirOutputRel (`__/` ancestor
//     infix); input `$(S)/<srcDir>/<srcRel>`.
func composeASPaths(instance ModuleInstance, srcRel string, in ModuleCCInputs) (out, input VFS) {
	useSrcDir := in.SrcDir != nil && *in.SrcDir != instance.Path && !sourceExistsLocally(in.SourceRoot, instance.Path, srcRel)

	if useSrcDir {
		outputRel := composeSrcDirOutputRel(instance.Path, *in.SrcDir, srcRel)
		// path.Clean canonicalises ".." segments from srcRels that ascend
		// out of SrcDir (e.g. openssl's "crypto/../asm/aarch64/…").
		return Build(instance.Path + "/" + outputRel + ".o"),
			Source(path.Clean(*in.SrcDir + "/" + srcRel))
	}

	var outRel string
	outName := srcRel + ".o"
	if strings.HasSuffix(srcRel, ".asm") {
		outName = strings.TrimSuffix(srcRel, ".asm") + ".o"
	}

	if strings.Contains(srcRel, "/") {
		outRel = instance.Path + "/_/" + outName
	} else {
		outRel = instance.Path + "/" + outName
	}

	return Build(outRel), Source(instance.Path + "/" + srcRel)
}

// composeASCmdArgs builds the cmd_args bundle. Three flavours dispatched
// on (Platform.IsHost, Flags.NoStdInc):
//   - Target: aarch64; commonCFlags+Defines (+muslExtraDefines if musl)
//   - noLibcUndebugBlock×2 with catboost between.
//   - Host non-musl: x86_64; hostCFlags+Defines + ndebugPicBlock×2 with
//     catboost + hostSseFeatures between.
//   - Host musl: host non-musl + muslExtraDefines between hostDefines
//     and the first ndebugPicBlock.
//
// Warning bundle honours Flags.NoCompilerWarnings (CC's pickWarningFlags
// rule); own/auto-peer CFLAGS and the includes tail thread via
// ModuleCCInputs (same struct CC consumes).
func composeASCmdArgs(instance ModuleInstance, outputPath, inputPath string, in ModuleCCInputs) []string {
	// instance.Flags.NoStdInc is the per-module compile-shape flag,
	// distinct from Platform.Flags["MUSL"] which is the CLI-level
	// "build everything in musl mode" toggle.
	isMusl := instance.Flags.NoStdInc

	bundle := compileFlagBundleFor(instance.Platform)
	prologueArgs := 3 + len(bundle.ArchArgs)

	musl := []string(nil)
	if isMusl {
		musl = muslExtraDefines
	}

	// warning bundle follows NoCompilerWarnings (mirror of CC's
	// pickWarningFlags): NO_COMPILER_WARNINGS modules → -Wno-everything;
	// regular modules → -Werror/-Wall/-Wextra set.
	warnBundle := pickWarningFlags(instance.Flags.NoCompilerWarnings)

	// Own non-GLOBAL + peer CFLAGS thread via ModuleCCInputs (same
	// struct CC consumes). Suppressed for musl-self (musl-self-isolation
	// invariant — muslExtraDefines carries the musl-self CFLAGS).
	// ownCFlags uses composeOwnAndPeerCFlagsAtOwnSlot, so the slot
	// carries CFlags+PeerCFlagsGlobal+OwnCFlagsGlobal — matches openssl's
	// CFLAGS(GLOBAL -DOPENSSL_RENAME_SYMBOLS=1) reaching peer AS consumers.
	var ownCFlags, autoPeerCFlags []string

	if !isMusl {
		ownCFlags = composeOwnAndPeerCFlagsAtOwnSlot(in, instance.Platform)
		autoPeerCFlags = in.AutoPeerCFlags
	}

	includes := composeASIncludes(in, isMusl, instance.Platform.ISA)

	betweenBlocks := len(catboostOpenSourceDefine) + len(autoPeerCFlags)
	betweenBlocks += len(bundle.CPUFeatures)

	fixed := prologueArgs + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warnBundle) + len(bundle.Defines) + len(musl) + len(ownCFlags) +
		len(bundle.NoLibcBlock) + betweenBlocks + len(bundle.NoLibcBlock) + len(in.SFlags) + 4
	cmdArgs := make([]string, 0, fixed+len(includes))

	// Prologue: compiler, target triple, optional -march, assembler search path.
	cmdArgs = append(cmdArgs, instance.Platform.Tools.CC, "--target="+instance.Platform.Triple)
	cmdArgs = append(cmdArgs, bundle.ArchArgs...)
	cmdArgs = append(cmdArgs, "-B"+binPath)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, bundle.CFlags...)

	// PR-35i: NO_COMPILER_WARNINGS-gated warning bundle (mirror of CC).
	cmdArgs = append(cmdArgs, warnBundle...)
	cmdArgs = append(cmdArgs, bundle.Defines...)
	cmdArgs = append(cmdArgs, musl...)

	// PR-35m: own non-GLOBAL CFLAGS slot between commonDefines and the
	// first noLibcUndebugBlock (mirror of composeTargetCC's ownCFlags
	// slot at cc.go:723).
	cmdArgs = append(cmdArgs, ownCFlags...)

	// Suppression block emitted twice flanking catboostOpenSourceDefine
	// (target) or catboost + hostSseFeatures (host). Mirror of
	// composeMuslCC / composeMuslHostCC.
	cmdArgs = append(cmdArgs, bundle.NoLibcBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)

	// PR-35m: AutoPeerCFlags slot between catboost and the second
	// suppressionBlock copy (mirror of composeTargetCC at cc.go:726).
	cmdArgs = appendAutoPeerAndCPUFeatures(cmdArgs, bundle, autoPeerCFlags)
	cmdArgs = append(cmdArgs, bundle.NoLibcBlock...)

	// SFLAGS slot: assembler-only bundle from SET_APPEND(SFLAGS …) in
	// the module's ya.make. Sits between 2nd suppressionBlock and
	// trailing `-c -o` — mirror of upstream
	// `$CFLAGS $SFLAGS $SRCFLAGS -c -o …` (build/ymake.core.conf:3217).
	cmdArgs = append(cmdArgs, in.SFlags...)

	// Output and input: -c -o <out> <in>, trailing all flags.
	cmdArgs = append(cmdArgs, "-c", "-o", outputPath, inputPath)

	// Module-specific includes trail the source path.
	cmdArgs = append(cmdArgs, includes...)

	return cmdArgs
}

// composeASIncludes derives the include-tail slice following the source
// path in cmd_args:
//   - no-stdinc: muslCcIncludesFor(ISA) — the
//     structurally-folded musl include set with arch/<isa>.
//   - non-musl: ccIncludesPrefix + AddIncl + ccIncludesSuffix +
//     PeerAddInclGlobal (mirror of CC). Own ADDINCL slots between the
//     baseline pair and linux-headers; peer-GLOBAL ADDINCL slots after.
//
// The musl-self override is structural: musl's own ya.make declares
// arch/include paths as own ADDINCL but the canonical musl-self
// isolation pattern interleaves them between SOURCE_ROOT and the
// linux-headers pair (matches CC).
func composeASIncludes(in ModuleCCInputs, isMusl bool, isa ISA) []string {
	if isMusl {
		return muslCcIncludesFor(isa)
	}

	out := make([]string, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(ccIncludesSuffix)+len(in.PeerAddInclGlobal))
	out = append(out, ccIncludesPrefix...)
	out = appendAddIncl(out, in.AddIncl)
	out = append(out, ccIncludesSuffix...)
	out = appendAddIncl(out, in.PeerAddInclGlobal)

	return out
}
