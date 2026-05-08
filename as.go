package main

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
	outputPath := "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + ".o"
	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

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
	if instance.Flags.PIC {
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
		HostPlatform: instance.Flags.PIC,
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
	isHost := instance.Flags.PIC
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
