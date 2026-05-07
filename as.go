package main

// as.go — emitter for AS assembly nodes.
//
// EmitAS produces a single Node matching the shape ymake itself produces
// for assembling one .S source into an object file. The reference node
// pinned for byte-exact tests is the chkstk.S node inside
// contrib/libs/cxxsupp/builtins in /home/pg/monorepo/yatool_orig/g.json.
//
// Output path convention (D29):
//
//	$(BUILD_ROOT)/<moduleDir>/_/<srcRel>.o
//
// The `_/` infix is unconditional for AS (unlike CC which omits it for
// flat sources). Every real .S source in the reference graph has a nested
// path (e.g. "aarch64/chkstk.S"), but D29 mandates `_/` always to keep
// the formula uniform and collision-free regardless of srcRel depth.
//
// cmd_args layout (94 args for builtins chkstk):
//
//	prologue (4):          clang --target= -march= -B
//	debugPrefixMapFlags (3)
//	xclangDebugCompilationDir (4)
//	commonCFlags (14)
//	asWnoEverything (1):   -Wno-everything  ← replaces CC's warningFlags
//	commonDefines (11)
//	noLibcUndebugBlock (22) — first copy
//	catboostOpenSourceDefine (1)
//	noLibcUndebugBlock (22) — second copy (ymake emits it twice)
//	-c -o <out> <in> (4)
//	module-specific -I includes (N)
//
// Differences from EmitCC:
//   - No warningFlags (-Werror/-Wall/-Wextra + the 3 companion -Wno-*);
//     only the single -Wno-everything sits in their place.
//   - No builtinMacroDateTime (3 args: -Wno-builtin-macro-redefined +
//     pinned __DATE__/__TIME__).
//   - No macroPrefixMapFlags (3 args: -fmacro-prefix-map=…).
//   - -c -o <out> <in> appears AFTER all flag bundles, not at position
//     [4-6] as in CC.
//   - Module-specific includes trail the source path.
//   - kv["p"]  = "AS", kv["pc"] = "light-green" (not "green").
//   - No kv["show_out"].
//   - target_properties has only module_dir (no module_lang/module_type).
//
// Why flag bundles are local vars rather than new entries in flags.go:
// PR-14 is editing flags.go concurrently in a separate worktree. Adding
// to flags.go here would create a merge conflict. The AS-specific
// fragment (asWnoEverything) is a single string and doesn't justify
// a shared bundle; it is declared locally and self-documenting.

// asWnoEverything is the single warning-suppression flag that AS nodes
// carry in place of CC's warningFlags bundle. The assembler pass does
// not need the strict warning-as-error discipline that C/C++ compilation
// uses; silencing all warnings avoids churn from clang version upgrades.
var asWnoEverything = []string{"-Wno-everything"}

// EmitAS emits an AS node for assembling `srcRel` (a path relative to
// the module dir, e.g. "aarch64/chkstk.S") inside `moduleDir`
// (e.g. "contrib/libs/cxxsupp/builtins") into an object file.
//
// The output path formula is:
//
//	$(BUILD_ROOT)/<moduleDir>/_/<srcRel>.o
//
// The `_/` infix is unconditional for AS (D29); all assembly sources in
// the reference graph have nested paths, but the formula is applied
// uniformly regardless of srcRel depth.
//
// `includes` is the ordered list of -I flags specific to this module
// (e.g. ["-I$(SOURCE_ROOT)/contrib/libs/musl/arch/aarch64", …]).
// They are appended after the source path, matching the reference layout.
// The caller supplies these because EmitAS has no access to the module's
// ya.make ADDINCL list; the gen driver (or test) must derive them from
// the module descriptor.
//
// Returns (NodeRef, outputPath) so the caller can wire the AS node as a
// dependency of the AR step and avoid re-deriving the output path.
func EmitAS(cfg PlatformConfig, moduleDir string, srcRel string, includes []string, emit Emitter) (NodeRef, string) {
	outputPath := "$(BUILD_ROOT)/" + moduleDir + "/_/" + srcRel + ".o"
	inputPath := "$(SOURCE_ROOT)/" + moduleDir + "/" + srcRel

	// Compose the cmd_args. The capacity hint is the fixed part (86)
	// plus the module-specific includes length, to avoid reallocation.
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

	// AS uses -Wno-everything instead of CC's -Werror/-Wall/-Wextra + companions.
	cmdArgs = append(cmdArgs, asWnoEverything...)
	cmdArgs = append(cmdArgs, commonDefines...)

	// noLibcUndebugBlock appears twice (once before and once after
	// catboostOpenSourceDefine), mirroring CC's composition exactly.
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)

	// Output and input: -c -o <out> <in>, trailing all flags.
	// (Contrast CC, which places -c -o at position [4-6]; AS places them here.)
	cmdArgs = append(cmdArgs, "-c", "-o", outputPath, inputPath)

	// Module-specific includes trail the source path.
	cmdArgs = append(cmdArgs, includes...)

	// The reference graph carries identical env maps at both the cmd
	// level and the node top level. A single map is constructed and
	// aliased to both; EmitAS is single-shot so the alias is safe.
	// Future PRs mutating post-emit nodes MUST clone before mutating.
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
			"p":  "AS",
			"pc": "light-green",
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir": moduleDir,
		},
		Platform: cfg.Name,
		// Numeric values use float64 to match encoding/json's default
		// unmarshalling of JSON numbers into interface{} targets.
		// Using int literals would cause reflect.DeepEqual to diverge
		// from the reference even though the on-disk JSON is identical.
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	return emit.Emit(node), outputPath
}
