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

// EmitAS emits an AS node for assembling `srcRel` (relative to
// instance.Path) into an object file.
//
// `in` carries per-module compile knobs (own/peer ADDINCL, own CFLAGS,
// auto peer CFLAGS, transitive header closure); synthetic tests can
// pass ModuleCCInputs{} for "no per-module flags".
//
// `yasmLD` is the host yasm linker NodeRef (real for asmlib .pic.o, nil
// elsewhere); when non-nil, wired into BOTH ForeignDepRefs["tool"] and
// DepRefs (foreign-deps-only diverged on L0 fingerprint).
//
// cmd_args branches on two orthogonal flags:
//   - instance.Flags.PIC selects host (x86_64; --target=x86_64-linux-gnu,
//     no -march, hostCFlags/hostDefines/ndebugPicBlock×2 with
//     hostSseFeatures between) vs target (aarch64;
//     --target=aarch64-linux-gnu -march=armv8-a, commonCFlags/Defines/
//     noLibcUndebugBlock×2).
//   - instance.Flags.LibcMusl injects muslExtraDefines (incl. -D_musl_=1)
//     between defines and suppression blocks.
//
// Returns (NodeRef, outputPath).
func EmitAS(instance ModuleInstance, srcRel string, in ModuleCCInputs, yasmLD *NodeRef, hostP *Platform, emit Emitter) (NodeRef, VFS) {

	// x86_64 `.asm` sources use yasm, not clang — branch off before any
	// clang-shape composition. yasm is x86-specific (ISA dispatch, not
	// host/target).
	if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
		return emitASYasm(instance, srcRel, in, yasmLD, emit)
	}

	// Output/input path composition mirrors composeCCPaths (cc.go):
	//   1. flat srcRel: flat output, same-dir input.
	//   2. nested srcRel without SRCDIR: _/<srcRel>.o, same-dir input.
	//   3. SRCDIR set + source not local: __/<rel>.o (ancestor segments
	//      → __), SRCDIR input.
	outVFS, inVFS := composeASPaths(instance, srcRel, in)
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	cmdArgs := composeASCmdArgs(instance, outputPath, inputPath, in)

	// The reference graph carries identical env maps at both the cmd
	// level and the node top level. A single map is constructed and
	// aliased to both; EmitAS is single-shot so the alias is safe.
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"DYLD_LIBRARY_PATH":      hostP.MultiarchLibPath(),
	}

	allInputs := make([]VFS, 0, 1+len(in.IncludeInputs))
	allInputs = append(allInputs, inVFS)
	allInputs = append(allInputs, in.IncludeInputs...)

	// tags + host_platform from instance.Platform; non-nil empty Tags
	// keeps JSON `[]`, not `null`.
	tags := instance.Platform.Tags

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(B)",
				Env:     env,
			},
		},
		Env:          env,
		Inputs:       allInputs,
		Outputs:      []VFS{outVFS},
		HostPlatform: instance.Platform.IsHost,
		KV: map[string]string{
			"p":  "AS",
			"pc": "light-green",
		},
		Tags: tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	if yasmLD != nil {
		node.ForeignDepRefs = map[string][]NodeRef{
			"tool": {*yasmLD},
		}

		// Reference asmlib host AS nodes also list yasm in `deps`, not
		// just foreign_deps.tool. L0 reads only deps; foreign-deps wiring
		// alone leaves the AS fingerprint without a yasm child.
		node.DepRefs = []NodeRef{*yasmLD}
	}

	return emit.Emit(node), outVFS
}

// yasmBinaryVFS is the canonical $(B)-relative host yasm binary path;
// yasmBinaryPath is its String() form. Hardcoded because the only
// consumer is the asmlib host-PIC branch (gated by asmlibYasmModules);
// yasm's PROGRAM directory is stable.
var (
	yasmBinaryVFS  = Build("contrib/tools/yasm/yasm")
	yasmBinaryPath = yasmBinaryVFS.String()
)

// emitASYasm composes the yasm-shaped AS node for a host-PIC `.asm`
// source — the asmlib-only counterpart to the clang AS path.
func emitASYasm(instance ModuleInstance, srcRel string, in ModuleCCInputs, yasmLD *NodeRef, emit Emitter) (NodeRef, VFS) {
	// Output stem strips `.asm` (the only extension this branch sees;
	// asmlib's reference uses `.asm` exclusively per PR-30 D07).
	stem := strings.TrimSuffix(srcRel, ".asm")
	// Nested srcRel (e.g. util's "system/context_x86.asm") uses the
	// "_/" infix → util/_/system/context_x86.pic.o; flat srcRel
	// (asmlib's "cachesize64.asm") keeps the flat shape.
	var outVFS VFS
	if strings.Contains(srcRel, "/") {
		outVFS = Build(instance.Path + "/_/" + stem + ".pic.o")
	} else {
		outVFS = Build(instance.Path + "/" + stem + ".pic.o")
	}
	inVFS := Source(instance.Path + "/" + srcRel)
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	// _YASM_PREDEFINED_FLAGS_VALUE = "-g dwarf2" for all Linux non-asmlib
	// modules (default in ymake.core.conf). asmlib clears it via SET(""),
	// tracked here via the asmlibYasmModules sentinel.
	var predefinedFlags []string
	if !asmlibYasmModules[instance.Path] {
		predefinedFlags = []string{"-g", "dwarf2"}
	}

	cmdArgs := make([]string, 0, 20+len(predefinedFlags))
	cmdArgs = append(cmdArgs,
		yasmBinaryPath,
		"-f", "elf64",
		"-D", "UNIX",
		"--replace=$(B)=/-B",
		"--replace=$(S)=/-S",
		"--replace=$(TOOL_ROOT)=/-T",
		"-D", "_" + string(instance.Platform.ISA) + "_",
		"-D_YASM_",
	)
	cmdArgs = append(cmdArgs, predefinedFlags...)
	cmdArgs = append(cmdArgs,
		"-I", "$(B)",
		"-I", "$(S)",
		"-o", outputPath,
		inputPath,
	)

	// Env: ARCADIA_ROOT_DISTBUILD + YASM_TEST_SUITE. No DYLD_LIBRARY_PATH
	// — yasm has no host-clang runtime dep. Single map aliased to both
	// cmd-level and node-level Env.
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"YASM_TEST_SUITE":        "1",
	}

	// Inputs: yasm binary FIRST (reference inputs[0]), then source, then
	// transitive asm includes (e.g. `defs.asm` for asmlib).
	allInputs := make([]VFS, 0, 2+len(in.IncludeInputs))
	allInputs = append(allInputs, yasmBinaryVFS)
	allInputs = append(allInputs, inVFS)
	allInputs = append(allInputs, in.IncludeInputs...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				// Cwd intentionally empty: reference asmlib yasm AS nodes
				// omit `cwd` (clang AS path sets `Cwd: $(B)`).
				Env: env,
			},
		},
		Env:          env,
		Inputs:       allInputs,
		Outputs:      []VFS{outVFS},
		HostPlatform: instance.Platform.IsHost,
		KV: map[string]string{
			"p":  "AS",
			"pc": "light-green",
		},
		Tags: instance.Platform.Tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	if yasmLD != nil {
		node.ForeignDepRefs = map[string][]NodeRef{
			"tool": {*yasmLD},
		}
		node.DepRefs = []NodeRef{*yasmLD}
	}

	return emit.Emit(node), outVFS
}

// composeASPaths derives (outputPath, inputPath) for the clang AS path
// (mirror of composeCCPaths):
//   1. Flat srcRel: `$(B)/<path>/<srcRel>.o`, `$(S)/<path>/<srcRel>`.
//   2. Nested srcRel no SRCDIR: `$(B)/<path>/_/<srcRel>.o`, same input.
//   3. SRCDIR + non-local source: composeSrcDirOutputRel (`__/` ancestor
//      infix); input `$(S)/<srcDir>/<srcRel>`.
func composeASPaths(instance ModuleInstance, srcRel string, in ModuleCCInputs) (out, input VFS) {
	useSrcDir := in.SrcDir != "" && in.SrcDir != instance.Path && !sourceExistsLocally(in.SourceRoot, instance.Path, srcRel)

	if useSrcDir {
		outputRel := composeSrcDirOutputRel(instance.Path, in.SrcDir, srcRel)
		// path.Clean canonicalises ".." segments from srcRels that ascend
		// out of SrcDir (e.g. openssl's "crypto/../asm/aarch64/…").
		return Build(instance.Path + "/" + outputRel + ".o"),
			Source(path.Clean(in.SrcDir + "/" + srcRel))
	}

	var outRel string

	if strings.Contains(srcRel, "/") {
		outRel = instance.Path + "/_/" + srcRel + ".o"
	} else {
		outRel = instance.Path + "/" + srcRel + ".o"
	}

	return Build(outRel), Source(instance.Path + "/" + srcRel)
}

// composeASCmdArgs builds the cmd_args bundle. Three flavours dispatched
// on (Platform.IsHost, Flags.LibcMusl):
//   - Target: aarch64; commonCFlags+Defines (+muslExtraDefines if musl)
//     + noLibcUndebugBlock×2 with catboost between.
//   - Host non-musl: x86_64; hostCFlags+Defines + ndebugPicBlock×2 with
//     catboost + hostSseFeatures between.
//   - Host musl: host non-musl + muslExtraDefines between hostDefines
//     and the first ndebugPicBlock.
// Warning bundle honours Flags.NoCompilerWarnings (CC's pickWarningFlags
// rule); own/auto-peer CFLAGS and the includes tail thread via
// ModuleCCInputs (same struct CC consumes).
func composeASCmdArgs(instance ModuleInstance, outputPath, inputPath string, in ModuleCCInputs) []string {
	// Bundle flavour dispatch is on instance.Platform.IsHost: host →
	// release/PIC with hostCFlags+Defines+ndebugPicBlock+SSE fanout;
	// target → debug/noPIC with commonCFlags+Defines+noLibcUndebugBlock.
	isHost := instance.Platform.IsHost
	// instance.Flags.LibcMusl is the PER-MODULE flag (module is part of
	// the musl subtree), distinct from Platform.Flags["MUSL"] which is
	// the CLI-level "build everything in musl mode" toggle.
	isMusl := instance.Flags.LibcMusl

	var cFlags, defines, suppressionBlock []string

	if isHost {
		cFlags = hostCFlags
		defines = hostDefines
		suppressionBlock = ndebugPicBlock
	} else {
		cFlags = commonCFlags
		defines = commonDefines
		suppressionBlock = noLibcUndebugBlock
	}

	withMarch := instance.Platform.March != ""

	prologueArgs := 3
	if withMarch {
		prologueArgs = 4
	}

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
		ownCFlags = composeOwnAndPeerCFlagsAtOwnSlot(in)
		autoPeerCFlags = in.AutoPeerCFlags
	}

	includes := composeASIncludes(in, isMusl, instance.Platform.ISA)

	betweenBlocks := len(catboostOpenSourceDefine) + len(autoPeerCFlags)
	if isHost {
		betweenBlocks += len(hostSseFeatures)
	}

	fixed := prologueArgs + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(cFlags) + len(warnBundle) + len(defines) + len(musl) + len(ownCFlags) +
		len(suppressionBlock) + betweenBlocks + len(suppressionBlock) + len(in.SFlags) + 4
	cmdArgs := make([]string, 0, fixed+len(includes))

	// Prologue: compiler, target triple, optional -march, assembler search path.
	cmdArgs = append(cmdArgs, instance.Platform.Tools.CC, "--target="+instance.Platform.Triple)

	if withMarch {
		cmdArgs = append(cmdArgs, "-march="+instance.Platform.March)
	}

	cmdArgs = append(cmdArgs, "-B"+binPath)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, cFlags...)

	// PR-35i: NO_COMPILER_WARNINGS-gated warning bundle (mirror of CC).
	cmdArgs = append(cmdArgs, warnBundle...)
	cmdArgs = append(cmdArgs, defines...)
	cmdArgs = append(cmdArgs, musl...)

	// PR-35m: own non-GLOBAL CFLAGS slot between commonDefines and the
	// first noLibcUndebugBlock (mirror of composeTargetCC's ownCFlags
	// slot at cc.go:723).
	cmdArgs = append(cmdArgs, ownCFlags...)

	// Suppression block emitted twice flanking catboostOpenSourceDefine
	// (target) or catboost + hostSseFeatures (host). Mirror of
	// composeMuslCC / composeMuslHostCC.
	cmdArgs = append(cmdArgs, suppressionBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)

	// PR-35m: AutoPeerCFlags slot between catboost and the second
	// suppressionBlock copy (mirror of composeTargetCC at cc.go:726).
	cmdArgs = append(cmdArgs, autoPeerCFlags...)

	if isHost {
		cmdArgs = append(cmdArgs, hostSseFeatures...)
	}

	cmdArgs = append(cmdArgs, suppressionBlock...)

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
//   - musl-self (LibcMusl=true): muslCcIncludesFor(ISA) — the
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
