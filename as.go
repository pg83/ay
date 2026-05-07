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
// passes a real ref for asmlib `.pic.o` nodes (they wire it via
// `ForeignDepRefs["tool"]`); callers without a yasm dep pass `nil`
// for yasmLD and EmitAS skips the foreign-dep wiring.
//
// Returns (NodeRef, outputPath) so the caller can wire the AS node
// as a dependency of the AR step and avoid re-deriving the output
// path.
func EmitAS(instance ModuleInstance, srcRel string, includes []string, yasmLD *NodeRef, emit Emitter) (NodeRef, string) {
	outputPath := "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + ".o"
	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

	// Compose the cmd_args. The capacity hint is the fixed part
	// (86) plus the module-specific includes length, to avoid
	// reallocation.
	fixed := 4 + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(commonCFlags) + len(asWnoEverything) + len(commonDefines) +
		len(noLibcUndebugBlock) + len(catboostOpenSourceDefine) +
		len(noLibcUndebugBlock) + 4
	cmdArgs := make([]string, 0, fixed+len(includes))

	// Prologue: compiler, target triple, arch, assembler search path.
	cmdArgs = append(cmdArgs,
		ccCompilerPath,
		"--target="+targetTriple,
		"-march="+archFlag,
		"-B"+binPath,
	)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)

	// AS uses -Wno-everything instead of CC's
	// -Werror/-Wall/-Wextra + companions.
	cmdArgs = append(cmdArgs, asWnoEverything...)
	cmdArgs = append(cmdArgs, commonDefines...)

	// noLibcUndebugBlock appears twice (once before and once after
	// catboostOpenSourceDefine), mirroring CC's composition exactly.
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)

	// Output and input: -c -o <out> <in>, trailing all flags.
	cmdArgs = append(cmdArgs, "-c", "-o", outputPath, inputPath)

	// Module-specific includes trail the source path.
	cmdArgs = append(cmdArgs, includes...)

	// The reference graph carries identical env maps at both the cmd
	// level and the node top level. A single map is constructed and
	// aliased to both; EmitAS is single-shot so the alias is safe.
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(BUILD_ROOT)",
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  []string{inputPath},
		Outputs: []string{outputPath},
		KV: map[string]string{
			"p":  "AS",
			"pc": "light-green",
		},
		Tags: []string{},
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
	}

	return emit.Emit(node), outputPath
}
