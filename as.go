package main

import "strings"

// as.go — emitter for AS assembly nodes.
//
// PR-23 retrofitted the signature: `EmitAS` now takes a
// `ModuleInstance` and a `yasmLD *NodeRef` (the host yasm linker
// node). The yasm ref is wired into `ForeignDepRefs["tool"]` so the
// 25 host-asmlib `.pic.o` AS nodes in the reference graph (with
// `foreign_deps.tool = [yasm-host-LD-uid]`) are emittable end-to-end
// in PR-25's full PEERDIR closure.
//
// PR-23's only AS test (`TestEmitAS_CxxsuppBuiltinsChkstk_ByteExact`)
// emits a target-side AS that does NOT use yasm; the test passes
// `nil` as `yasmLD` and the body skips the
// `ForeignDepRefs["tool"]` wiring when yasmLD is nil
// (sentinel for "no yasm dep, target build"). Callers that DO need
// yasm wiring pass `&realYasmRef`.
//
// PR-30 D02: yasmLD when non-nil is wired BOTH into
// ForeignDepRefs["tool"] (per the reference shape) AND into DepRefs
// (L0 fingerprint reads only deps; foreign-deps-only shape diverged
// for asmlib's 25 AS nodes).
//
// EmitAS produces a single Node matching the shape ymake itself
// produces for assembling one .S source into an object file. The
// reference node pinned for byte-exact tests is the chkstk.S node
// inside contrib/libs/cxxsupp/builtins.

// PR-35q: when the module is an asmlib host-PIC consumer
// (`asmlibYasmModules[instance.Path] && instance.Flags.PIC`), EmitAS
// switches to the yasm toolchain shape rather than the clang AS shape.
// The reference graph's 25 asmlib `.asm` AS nodes diverge from clang
// AS in three ways:
//
//   - Output: flat `<modulePath>/<srcStem>.pic.o` (no `_/` infix; stem
//     = srcRel with `.asm` suffix stripped). The clang AS path is
//     `<modulePath>/_/<srcRel>.o` which neither matches the suffix nor
//     omits the `_/` infix.
//   - cmd_args: 18-arg yasm invocation
//     (`$(BUILD_ROOT)/contrib/tools/yasm/yasm -f elf64 -D UNIX
//     --replace=$(BUILD_ROOT)=/-B --replace=$(SOURCE_ROOT)=/-S
//     --replace=$(TOOL_ROOT)=/-T -D _x86_64_ -D_YASM_ -I $(BUILD_ROOT)
//     -I $(SOURCE_ROOT) -o <out> <in>`). No clang flags, no warning
//     bundle, no defines, no includes from `ModuleCCInputs`.
//   - Env: only `ARCADIA_ROOT_DISTBUILD` + `YASM_TEST_SUITE=1`
//     (NO `DYLD_LIBRARY_PATH` — yasm has no host-clang library
//     dependency).
//
// Inputs ordering: yasm binary FIRST, then source path, then transitive
// includes. The reference shape places the yasm binary at index 0 of
// the inputs slice (verified across cachesize64 / cpuid64 / memcpy64 /
// memset64 / strlen64).
//
// Empirically pinned against `contrib/libs/asmlib/cachesize64.pic.o` in
// `as_test.go::TestEmitAS_AsmlibYasm_ByteExact`. The asmlib host
// closure (25 AS nodes) was identified by the PR-35p probe; this
// branch closes 25 L1 pairs + 25 L3 nodes simultaneously.

// PR-35i (PR-33-C2_06 closure): the warning-bundle slot in AS cmd_args
// follows the same NO_COMPILER_WARNINGS discriminator as CC. Modules
// that declare `NO_COMPILER_WARNINGS()` (musl-self, libcxx, libcxxrt,
// abseil-cpp, tcmalloc, cxxsupp/builtins, …) emit the single-arg
// `muslWarningFlags` (`-Wno-everything`); regular modules (util,
// libunwind, asmglibc) preserve the full `warningFlags` bundle
// (`-Werror -Wall -Wextra -Wno-parentheses ...`). Empirical reference:
//
//   - cxxsupp/builtins/_/aarch64/chkstk.S.o cmd_args[25] = "-Wno-everything"
//     (NO_COMPILER_WARNINGS=true).
//   - util/_/system/context_aarch64.S.o cmd_args[25..30] = warningFlags
//     (NO_COMPILER_WARNINGS=false; warning bundle preserved).
//
// Prior to PR-35i, AS unconditionally substituted `-Wno-everything`
// regardless of the module's `NoCompilerWarnings` flag. The change is
// equivalent to CC's `pickWarningFlags(noCompilerWarnings)` call.
//
// PR-35m (PR-35i stopgap retirement): the per-module compile knobs
// AS needs — own ADDINCL, peer-GLOBAL ADDINCL, own non-GLOBAL CFLAGS,
// auto peer CFLAGS — now thread via `ModuleCCInputs` (the same struct
// CC consumes), supplied by gen.go's AS dispatch. The util-specific
// path-sniff stopgap (`asUtilOwnCFlags` / `asUtilAutoPeerCFlags` /
// `asUtilTailIncludes`) is retired. `composeASCmdArgs` derives:
//   - includes tail = `ccIncludesPrefix + AddIncl + ccIncludesSuffix +
//     PeerAddInclGlobal` (mirror of CC's includes layout, excluding the
//     musl-self structural override). Empirical anchors:
//       - util/_/system/context_aarch64.S.o cmd_args[93..105] (own
//         AddIncl empty, peer-GLOBAL = libcxx/libcxxrt/musl-arch×4 +
//         user-PEERDIR zlib/double-conversion/libc_compat).
//       - libunwind/_/src/UnwindRegistersRestore.S.o cmd_args[98..106]
//         (own AddIncl = libunwind/include, peer-GLOBAL = musl-arch×4).
//       - cxxsupp/builtins/_/aarch64/chkstk.S.o cmd_args[86..93] (own
//         AddIncl = musl-arch×4, peer-GLOBAL empty — NO_PLATFORM).
//   - own CFLAGS slot between commonDefines and the first
//     suppressionBlock copy (mirror of CC's `ownCFlags` slot at
//     cc.go:723 / cc.go:788).
//   - autoPeerCFlags slot between catboost and the second
//     suppressionBlock copy (mirror of CC at cc.go:726 / cc.go:797).

// EmitAS emits an AS node for assembling `srcRel` (a path relative
// to `instance.Path`) into an object file.
//
// `in` carries the per-module compile knobs the walker collected for
// CC/AS (own ADDINCL, peer-GLOBAL ADDINCL, own CFLAGS, auto peer
// CFLAGS, transitive header closure). For synthetic tests that bypass
// the walker, pass `ModuleCCInputs{}` for the historical "no per-
// module flags" behaviour.
//
// `yasmLD` is the NodeRef of the host yasm linker. The caller
// passes a real ref for asmlib `.pic.o` nodes; callers without a
// yasm dep pass `nil` for yasmLD and EmitAS skips the wiring.
// yasmLD when non-nil is wired BOTH into ForeignDepRefs["tool"] (per
// the reference shape) AND into DepRefs (PR-30 D02 — L0 fingerprint
// reads only deps; foreign-deps-only shape diverged for asmlib's 25
// AS nodes).
//
// PR-35a: cmd_args composition branches on two orthogonal flags:
//
//   - `instance.Flags.PIC` selects host (x86_64) vs target (aarch64)
//     toolchain. Host emits `--target=x86_64-linux-gnu` with no
//     `-march` and uses hostCFlags / hostDefines / ndebugPicBlock × 2
//     with hostSseFeatures between (mirror of composeMuslHostCC's
//     non-musl-aware layout). Target keeps the historical
//     `--target=aarch64-linux-gnu -march=armv8-a` + commonCFlags /
//     commonDefines / noLibcUndebugBlock × 2 shape.
//   - `instance.Flags.LibcMusl` injects muslExtraDefines (incl.
//     `-D_musl_=1`) between the defines block and the suppression
//     block, matching composeMuslCC / composeMuslHostCC's slot.
//
// Returns (NodeRef, outputPath) so the caller can wire the AS node
// as a dependency of the AR step and avoid re-deriving the output
// path.
func EmitAS(instance ModuleInstance, srcRel string, in ModuleCCInputs, yasmLD *NodeRef, emit Emitter) (NodeRef, string) {
	// PR-35q: asmlib host-PIC AS nodes use yasm, not clang. Branch off
	// before any clang-shape composition runs.
	// D41: dispatch on Target, not Flags.PIC; x86_64 IS the host axis in M2/M3.
	if targetIsX8664(instance) && asmlibYasmModules[instance.Path] {
		return emitASYasm(instance, srcRel, in, yasmLD, emit)
	}

	// PR-35r: output/input path composition mirrors composeCCPaths (cc.go).
	// Three cases:
	//   1. srcRel has no "/": flat output (no _/ infix), same-dir input.
	//      Empirical: contrib/libs/asmglibc/memchr.S.o.
	//   2. srcRel has "/" and no SRCDIR override: _/<srcRel>.o output,
	//      same-dir input (e.g. cxxsupp/builtins/_/aarch64/chkstk.S.o).
	//   3. SRCDIR set and source doesn't exist locally: __/<rel>.o output
	//      (ancestor-dir segments rendered as __), SRCDIR input.
	//      Empirical: tcmalloc/no_percpu_cache/__/tcmalloc/internal/percpu_rseq_asm.S.o.
	outputPath, inputPath := composeASPaths(instance, srcRel, in)

	cmdArgs := composeASCmdArgs(instance, outputPath, inputPath, in)

	// The reference graph carries identical env maps at both the cmd
	// level and the node top level. A single map is constructed and
	// aliased to both; EmitAS is single-shot so the alias is safe.
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	allInputs := make([]string, 0, 1+len(in.IncludeInputs))
	allInputs = append(allInputs, inputPath)
	allInputs = append(allInputs, in.IncludeInputs...)

	tags := []string{}
	// D41: dispatch on Target, not Flags.PIC; x86_64 IS the host axis in M2/M3.
	if targetIsX8664(instance) {
		// PR-35a: host-built AS nodes carry `host_platform=true` and
		// `tags=["tool"]` per the reference shape (asmlib pic.o,
		// cxxsupp/builtins/x86_64/chkstk.S.o, musl host pic.o).
		tags = []string{"tool"}
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(BUILD_ROOT)",
				Env:     env,
			},
		},
		Env:          env,
		Inputs:       allInputs,
		Outputs:      []string{outputPath},
		HostPlatform: targetIsX8664(instance),
		KV: map[string]string{
			"p":  "AS",
			"pc": "light-green",
		},
		Tags: tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Target),
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

		// PR-30 D02: the reference asmlib host AS nodes also list yasm
		// in `deps` (not just `foreign_deps.tool`). The L0 fingerprint
		// reads only `deps`, so the foreign-deps wiring alone leaves
		// the AS node fingerprint without a yasm child — diverging
		// from the reference shape. Threading yasmLD into DepRefs
		// brings the AS node's L0 fingerprint into alignment.
		node.DepRefs = []NodeRef{*yasmLD}
	}

	return emit.Emit(node), outputPath
}

// yasmBinaryPath is the canonical $(BUILD_ROOT)-relative path of the
// host yasm binary, as observed across all 25 reference asmlib AS
// nodes in the M2 closure (cmd_args[0]). Hardcoded here because the
// only consumer is the asmlib host-PIC branch, which is itself gated
// by `asmlibYasmModules`. If a future host module joins
// `asmlibYasmModules`, the path remains the same — yasm's PROGRAM
// directory is stable.
const yasmBinaryPath = "$(BUILD_ROOT)/contrib/tools/yasm/yasm"

// emitASYasm composes the yasm-shaped AS node for an asmlib host-PIC
// `.asm` source. See the PR-35q docstring above EmitAS for the
// rationale and the byte-exact reference shape. The function is the
// asmlib-only counterpart to the clang AS path the rest of EmitAS
// implements.
func emitASYasm(instance ModuleInstance, srcRel string, in ModuleCCInputs, yasmLD *NodeRef, emit Emitter) (NodeRef, string) {
	// Output stem strips `.asm` (the only extension this branch sees;
	// asmlib's reference uses `.asm` exclusively per PR-30 D07).
	stem := strings.TrimSuffix(srcRel, ".asm")
	outputPath := "$(BUILD_ROOT)/" + instance.Path + "/" + stem + ".pic.o"
	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

	cmdArgs := []string{
		yasmBinaryPath,
		"-f", "elf64",
		"-D", "UNIX",
		"--replace=$(BUILD_ROOT)=/-B",
		"--replace=$(SOURCE_ROOT)=/-S",
		"--replace=$(TOOL_ROOT)=/-T",
		"-D", "_x86_64_",
		"-D_YASM_",
		"-I", "$(BUILD_ROOT)",
		"-I", "$(SOURCE_ROOT)",
		"-o", outputPath,
		inputPath,
	}

	// Env shape: `ARCADIA_ROOT_DISTBUILD` + `YASM_TEST_SUITE`. NO
	// `DYLD_LIBRARY_PATH` — yasm doesn't link against host clang's
	// runtime, so the reference omits the slot. Single map aliased to
	// both the cmd-level and node-level Env (mirror of EmitAS's
	// clang-path treatment).
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"YASM_TEST_SUITE":        "1",
	}

	// Inputs: yasm binary FIRST, then the source, then any transitive
	// asm includes (e.g. `defs.asm` for the asmlib set). The reference
	// shape places the yasm binary at index 0; the per-source includes
	// the scanner discovers (PR-31 D11) follow.
	allInputs := make([]string, 0, 2+len(in.IncludeInputs))
	allInputs = append(allInputs, yasmBinaryPath)
	allInputs = append(allInputs, inputPath)
	allInputs = append(allInputs, in.IncludeInputs...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				// Cwd intentionally empty: the reference asmlib yasm AS
				// nodes omit the `cwd` field (verified across all 25
				// nodes in `/home/pg/monorepo/yatool_orig/sg.json`).
				// The clang AS path sets `Cwd: $(BUILD_ROOT)` because
				// 58/83 reference clang AS nodes carry it; the yasm AS
				// nodes are part of the 25 that don't.
				Env: env,
			},
		},
		Env:          env,
		Inputs:       allInputs,
		Outputs:      []string{outputPath},
		HostPlatform: true,
		KV: map[string]string{
			"p":  "AS",
			"pc": "light-green",
		},
		Tags: []string{"tool"},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Target),
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

	return emit.Emit(node), outputPath
}

// composeASPaths derives (outputPath, inputPath) for the clang AS path.
// Mirrors composeCCPaths (cc.go:361-402) with three cases:
//
//  1. srcRel has no "/": flat output `$(BUILD_ROOT)/<path>/<srcRel>.o`;
//     input `$(SOURCE_ROOT)/<path>/<srcRel>`. Empirical: asmglibc/memchr.S.
//  2. srcRel has "/" and no SRCDIR override: `$(BUILD_ROOT)/<path>/_/<srcRel>.o`;
//     input `$(SOURCE_ROOT)/<path>/<srcRel>`. Empirical: cxxsupp/builtins/_/aarch64/chkstk.S.
//  3. SRCDIR set and source does not resolve locally: output uses
//     composeSrcDirOutputRel infix (cc.go) → `__/` for ancestor SRCDIR;
//     input `$(SOURCE_ROOT)/<srcDir>/<srcRel>`. Empirical: tcmalloc/no_percpu_cache →
//     `__/tcmalloc/internal/percpu_rseq_asm.S.o`.
func composeASPaths(instance ModuleInstance, srcRel string, in ModuleCCInputs) (string, string) {
	useSrcDir := in.SrcDir != "" && in.SrcDir != instance.Path && !sourceExistsLocally(in.SourceRoot, instance.Path, srcRel)

	if useSrcDir {
		outputRel := composeSrcDirOutputRel(instance.Path, in.SrcDir, srcRel)
		outputPath := "$(BUILD_ROOT)/" + instance.Path + "/" + outputRel + ".o"
		inputPath := "$(SOURCE_ROOT)/" + in.SrcDir + "/" + srcRel

		return outputPath, inputPath
	}

	var outputPath string

	if strings.Contains(srcRel, "/") {
		outputPath = "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + ".o"
	} else {
		outputPath = "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel + ".o"
	}

	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

	return outputPath, inputPath
}

// composeASCmdArgs builds the cmd_args bundle for an AS node. Three
// flavours, dispatched on `instance.Flags.PIC` (host vs target axis)
// and `instance.Flags.LibcMusl` (musl-self extra-defines block):
//
// Target (PIC=false): aarch64 toolchain, commonCFlags + commonDefines +
// (optional muslExtraDefines for LibcMusl) + noLibcUndebugBlock × 2 with
// catboost between. Pinned 94-arg byte-exact against
// `contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o`.
//
// Host non-musl (PIC=true, LibcMusl=false): x86_64 toolchain, hostCFlags
// + hostDefines + ndebugPicBlock × 2 with catboost + hostSseFeatures
// between. Pinned 98-arg byte-exact against
// `contrib/libs/cxxsupp/builtins/_/x86_64/chkstk.S.o`.
//
// Host musl (PIC=true, LibcMusl=true): same as host non-musl plus
// muslExtraDefines slotted between hostDefines and the first
// ndebugPicBlock copy. Pinned 109-arg byte-exact against
// `contrib/libs/musl/_/src/math/x86_64/ceill.s.o`.
//
// Mirrors composeMuslHostCC's slot ordering. PR-35i lifts the warning
// bundle to honour `instance.Flags.NoCompilerWarnings` (CC's
// `pickWarningFlags` rule); modules without NO_COMPILER_WARNINGS keep
// their `-Werror`/`-Wall`/`-Wextra` set. PR-35m threads own non-GLOBAL
// CFLAGS (`in.CFlags`), auto peer CFLAGS (`in.AutoPeerCFlags`), and
// the includes tail (`ccIncludesPrefix + in.AddIncl + ccIncludesSuffix
// + in.PeerAddInclGlobal`) generically via the same struct CC
// consumes.
func composeASCmdArgs(instance ModuleInstance, outputPath, inputPath string, in ModuleCCInputs) []string {
	// D41: dispatch on Target, not Flags.PIC; x86_64 IS the host axis in M2/M3.
	isHost := targetIsX8664(instance)
	isMusl := instance.Flags.LibcMusl

	var cFlags, defines, suppressionBlock []string
	var triple string
	var withMarch bool

	if isHost {
		triple = hostTriple
		cFlags = hostCFlags
		defines = hostDefines
		suppressionBlock = ndebugPicBlock
	} else {
		triple = targetTriple
		withMarch = true
		cFlags = commonCFlags
		defines = commonDefines
		suppressionBlock = noLibcUndebugBlock
	}

	prologueArgs := 3
	if withMarch {
		prologueArgs = 4
	}

	musl := []string(nil)
	if isMusl {
		musl = muslExtraDefines
	}

	// PR-35i: warning bundle follows the NoCompilerWarnings discriminator
	// (mirror of CC's pickWarningFlags). musl-self / libcxx-style
	// modules keep `-Wno-everything`; util / libunwind preserve the full
	// `-Werror`/`-Wall`/`-Wextra` set.
	warnBundle := pickWarningFlags(instance.Flags.NoCompilerWarnings)

	// PR-35m: own non-GLOBAL CFLAGS (`in.CFlags`) and auto peer CFLAGS
	// (`in.AutoPeerCFlags`) thread through `ModuleCCInputs` from the
	// walker (same struct CC consumes). Suppressed for musl-self per
	// the musl-self-isolation invariant (Q6) — `muslExtraDefines`
	// already carries the musl-self CFLAGS, and the peer-consumer
	// `-D_musl_` does not apply to musl-self builds.
	var ownCFlags, autoPeerCFlags []string

	if !isMusl {
		ownCFlags = in.CFlags
		autoPeerCFlags = in.AutoPeerCFlags
	}

	includes := composeASIncludes(in, isMusl, isHost)

	betweenBlocks := len(catboostOpenSourceDefine) + len(autoPeerCFlags)
	if isHost {
		betweenBlocks += len(hostSseFeatures)
	}

	fixed := prologueArgs + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(cFlags) + len(warnBundle) + len(defines) + len(musl) + len(ownCFlags) +
		len(suppressionBlock) + betweenBlocks + len(suppressionBlock) + 4
	cmdArgs := make([]string, 0, fixed+len(includes))

	// Prologue: compiler, target triple, optional -march, assembler search path.
	cmdArgs = append(cmdArgs, ccCompilerPath, "--target="+triple)

	if withMarch {
		cmdArgs = append(cmdArgs, "-march="+archFlag)
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

	// Output and input: -c -o <out> <in>, trailing all flags.
	cmdArgs = append(cmdArgs, "-c", "-o", outputPath, inputPath)

	// Module-specific includes trail the source path.
	cmdArgs = append(cmdArgs, includes...)

	return cmdArgs
}

// composeASIncludes derives the include-tail slice that follows the
// source path in cmd_args. Three flavours:
//
//   - musl-self target (LibcMusl=true, PIC=false): `muslCcIncludes`
//     (the structurally-folded aarch64 musl include set).
//   - musl-self host (LibcMusl=true, PIC=true): `muslCcIncludesX8664`
//     (the same set with `arch/aarch64` swapped for `arch/x86_64`).
//   - non-musl (the common case): `ccIncludesPrefix + AddIncl +
//     ccIncludesSuffix + PeerAddInclGlobal`, mirror of the CC composer
//     (cc.go:714-717 / cc.go:779-782). Own ADDINCL slots BETWEEN the
//     baseline `BUILD_ROOT/SOURCE_ROOT` pair and the linux-headers
//     pair; peer-GLOBAL ADDINCL slots AFTER the linux-headers pair.
//     Empirical anchors: util context_aarch64.S.o cmd_args[93..105]
//     (own empty, peer = libcxx/libcxxrt/musl/zlib/double-conversion/
//     libc_compat); libunwind UnwindRegistersRestore.S.o
//     cmd_args[98..106] (own = libunwind/include, peer = musl-arch×4);
//     cxxsupp/builtins chkstk.S.o cmd_args[86..93] (own = musl-arch×4,
//     peer empty — NO_PLATFORM).
//
// The musl-self override is structural (musl's own ya.make declares
// the arch/include paths as own ADDINCL but the reference shape
// interleaves them between `SOURCE_ROOT` and the linux-headers pair,
// which is the canonical musl-self isolation pattern). The override
// matches the CC behaviour (cc.go:218-228).
func composeASIncludes(in ModuleCCInputs, isMusl, isHost bool) []string {
	if isMusl {
		if isHost {
			return muslCcIncludesX8664
		}

		return muslCcIncludes
	}

	out := make([]string, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(ccIncludesSuffix)+len(in.PeerAddInclGlobal))
	out = append(out, ccIncludesPrefix...)
	out = appendAddIncl(out, in.AddIncl)
	out = append(out, ccIncludesSuffix...)
	out = appendAddIncl(out, in.PeerAddInclGlobal)

	return out
}
