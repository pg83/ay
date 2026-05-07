package main

// r6.go — emitter for R6 (ragel6) generated-source nodes.
//
// PR-23 rewrites the PR-17 r6.go to take a real `ragel6LD NodeRef`
// (D31 — cross-platform recursion replaces stub-host UIDs). The R6
// node depends on the host ragel6 linker via
// `ForeignDepRefs["tool"]`; PR-25's walker will recurse into
// `contrib/tools/ragel6` with the host instance and pass the
// resulting LD ref forward.
//
// Reference R6 node: `$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp`.
// 7 cmd_args, kv={p:R6, pc:yellow}, tags=[],
// requirements={cpu:1,network:restricted,ram:32},
// foreign_deps.tool=[<ragel6 host LD UID>].

// EmitR6 emits an R6 node generating `<srcRel>.cpp` from
// `<instance.Path>/<srcRel>` using the host ragel6 binary referenced
// by `ragel6LD`.
//
// Output path: `$(BUILD_ROOT)/<instance.Path>/_/<srcRel>.cpp`. Note
// the `_/` infix matches the AS convention (D29) — generated sources
// are nested-output regardless of srcRel depth.
//
// Returns (NodeRef, outputPath) so the caller can wire the R6 node as
// the input of a downstream EmitCC.
func EmitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, emit Emitter) (NodeRef, string) {
	outputPath := "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + ".cpp"
	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

	cmdArgs := []string{
		"$(BUILD_ROOT)/contrib/tools/ragel6/ragel6",
		"-CT0",
		"-L",
		"-I$(SOURCE_ROOT)",
		"-o",
		outputPath,
		inputPath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
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
			"p":  "R6",
			"pc": "yellow",
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
		ForeignDepRefs: map[string][]NodeRef{
			"tool": {ragel6LD},
		},
	}

	return emit.Emit(node), outputPath
}
