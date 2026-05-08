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

// asWnoEverything is the single warning-suppression flag that AS
// nodes carry in place of CC's warningFlags bundle. The assembler
// pass does not need the strict warning-as-error discipline that
// C/C++ compilation uses; silencing all warnings avoids churn from
// clang version upgrades.
var asWnoEverything = []string{"-Wno-everything"}

// EmitAS emits an AS node for assembling `srcRel` (a path relative
// to `instance.Path`) into an object file.
//
// `includes` is the ordered list of -I flags specific to this
// module. They are appended after the source path, matching the
// reference layout. The caller supplies these because EmitAS has
// no access to the module's ya.make ADDINCL list.
//
// `yasmLD` is the NodeRef of the host yasm linker. The caller
// passes a real ref for asmlib `.pic.o` nodes; callers without a
// yasm dep pass `nil` for yasmLD and EmitAS skips the wiring.
// yasmLD when non-nil is wired BOTH into ForeignDepRefs["tool"] (per
// the reference shape) AND into DepRefs (PR-30 D02 — L0 fingerprint
// reads only deps; foreign-deps-only shape diverged for asmlib's 25
// AS nodes).
//
// `includeInputs` (PR-31 D11) is the resolved transitive header
// closure for assembly sources that #include `.h`/`.inc` files
// (e.g. cxxsupp/builtins/chkstk.S → assembly.h). Empty for the
// common case where the source has no transitive headers.
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
func EmitAS(instance ModuleInstance, srcRel string, includes []string, yasmLD *NodeRef, includeInputs []string, emit Emitter) (NodeRef, string) {
	outputPath := "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + ".o"
	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

	// PR-35a: musl-self assembly nodes get the full musl include set
	// emitted at the cmd_args tail (matching the reference shape:
	// host musl ceill.s uses muslCcIncludesX8664; target musl uses
	// muslCcIncludes). The walker passes nil for `includes` to AS,
	// so the default-derived set lands here. Callers that already
	// supplied module-specific includes (e.g. the byte-exact test
	// for cxxsupp/builtins) keep their explicit slice.
	if includes == nil && instance.Flags.LibcMusl {
		if instance.Flags.PIC {
			includes = muslCcIncludesX8664
		} else {
			includes = muslCcIncludes
		}
	}

	cmdArgs := composeASCmdArgs(instance, outputPath, inputPath, includes)

	// The reference graph carries identical env maps at both the cmd
	// level and the node top level. A single map is constructed and
	// aliased to both; EmitAS is single-shot so the alias is safe.
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	allInputs := make([]string, 0, 1+len(includeInputs))
	allInputs = append(allInputs, inputPath)
	allInputs = append(allInputs, includeInputs...)

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
// `contrib/libs/cxxsupp/builtins/_/x86_64/chkstk.S.o` (prologue 0..89).
//
// Host musl (PIC=true, LibcMusl=true): same as host non-musl plus
// muslExtraDefines slotted between hostDefines and the first
// ndebugPicBlock copy. Pinned 109-arg byte-exact against
// `contrib/libs/musl/_/src/math/x86_64/ceill.s.o`.
//
// Mirrors composeMuslHostCC's slot ordering — the only AS-specific
// substitution is `asWnoEverything` in place of `warningFlags` /
// `muslWarningFlags` / `cxxStandardWarnings`. PR-33 C2_06 (per-module
// CFLAGS threading) is out-of-scope for PR-35a.
func composeASCmdArgs(instance ModuleInstance, outputPath, inputPath string, includes []string) []string {
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

	betweenBlocks := len(catboostOpenSourceDefine)
	if isHost {
		betweenBlocks += len(hostSseFeatures)
	}

	fixed := prologueArgs + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(cFlags) + len(asWnoEverything) + len(defines) + len(musl) +
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

	// AS uses -Wno-everything in place of CC's warningFlags / cxxStandardWarnings.
	cmdArgs = append(cmdArgs, asWnoEverything...)
	cmdArgs = append(cmdArgs, defines...)
	cmdArgs = append(cmdArgs, musl...)

	// Suppression block emitted twice flanking catboostOpenSourceDefine
	// (target) or catboost + hostSseFeatures (host). Mirror of
	// composeMuslCC / composeMuslHostCC.
	cmdArgs = append(cmdArgs, suppressionBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)

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
