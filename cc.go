package main

import (
	"path/filepath"
)

// cc.go — emitter for CC compilation nodes.
//
// EmitCC produces a single Node matching the shape ymake itself produces
// for compiling one C/C++ source into an object file. For M1 the only
// target the loop pins byte-exact is `build/cow/on/lib.c`; the function
// is structurally correct for other inputs but only that one is
// regression-tested against the reference graph.
//
// Why a function and not a struct + method: there is no ambient state to
// carry. A future PR introducing a real toolchain will likely invert
// this to `(*Toolchain).EmitCC(emit, mod)` — for M1 the platform name
// and the hardcoded flag bundles are the only inputs, so a free
// function suffices.

// EmitCC emits a CC node for compiling `srcRel` (a path relative to the
// module dir, e.g. "lib.c") inside `moduleDir` (e.g. "build/cow/on")
// into `$(BUILD_ROOT)/<moduleDir>/<basename(srcRel)>.o`. It returns the
// NodeRef so the caller (typically the AR/link step) can wire it as an
// input.
//
// The emitted node mirrors the reference graph for `build/cow/on/lib.c`
// byte-for-byte:
//   - cmd_args: 101 entries, composed by chaining the bundles in
//     flags.go with the input/output paths.
//   - env (per-cmd and top-level): the two ARCADIA_ROOT_DISTBUILD /
//     DYLD_LIBRARY_PATH entries.
//   - kv: {"p": "CC", "pc": "green"}.
//   - tags: empty.
//   - target_properties: {"module_dir": <moduleDir>}.
//   - platform: <cfg.Name>.
//   - requirements: {"cpu": 1, "network": "restricted", "ram": 32}.
//   - inputs: ["$(SOURCE_ROOT)/<moduleDir>/<srcRel>"].
//   - outputs: ["$(BUILD_ROOT)/<moduleDir>/<basename(srcRel)>.o"].
//   - host_platform: false (target-side compile, not a host tool).
//   - foreign_deps: nil (CC node has no host-tool deps).
//   - DepRefs: empty (leaf compile, no upstream nodes).
//
// TODO(future-PR): the no-libc bundle is currently applied
// unconditionally because build/cow/on is the only M1 leaf and it is a
// LIBRARY() with NO_UTIL/NO_LIBC/NO_RUNTIME. When a leaf without those
// macros lands, gate noLibcUndebugBlock + catboostOpenSourceDefine on a
// module flag.
func EmitCC(cfg PlatformConfig, moduleDir string, srcRel string, emit Emitter) NodeRef {
	outputPath := "$(BUILD_ROOT)/" + moduleDir + "/" + filepath.Base(srcRel) + ".o"
	inputPath := "$(SOURCE_ROOT)/" + moduleDir + "/" + srcRel

	// Compose the 101-element cmd_args. Each `append` corresponds to a
	// named bundle in flags.go; the structure is fixed for the M1
	// no-libc target. The capacity hint avoids re-allocation during
	// composition.
	cmdArgs := make([]string, 0, 101)
	cmdArgs = append(cmdArgs,
		ccCompilerPath,
		"--target="+targetTriple,
		"-march="+archFlag,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludes...)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)
	cmdArgs = append(cmdArgs, warningFlags...)
	cmdArgs = append(cmdArgs, commonDefines...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	cmdArgs = append(cmdArgs, inputPath)

	// The reference graph carries the same env map at both the cmd
	// level and the top level of the Node. Build it once and reuse.
	// env is intentionally a single map literal aliased to both Cmds[0].Env and node.Env;
	// the reference's two fields are identical maps, and EmitCC is single-shot so the
	// alias is safe today. Future PRs that mutate emitted nodes post-emit MUST clone
	// both fields before mutating either.
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
			"module_dir": moduleDir,
		},
		Platform: cfg.Name,
		// Numeric values are stored as float64 to match what
		// encoding/json produces when unmarshalling the reference graph
		// into `map[string]interface{}` (Go's default JSON-number type
		// for `interface{}` targets). Constructing with int literals
		// would make a comparator using reflect.DeepEqual against the
		// reference fail spuriously even though the on-disk JSON is
		// identical.
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	return emit.Emit(node)
}
