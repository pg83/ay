package main

// EmitAS emits an AS node for assembling a GNU/clang-as `.s`/`.S` (or
// non-x86 `.asm`) source `srcRel` (relative to instance.Path) into an
// object file. The x86_64 `.asm` yasm flavour is emitASYasm — only that
// path depends on the yasm tool, so this one never wires it. Synthetic
// tests can pass ModuleCCInputs{} for "no per-module flags".
//
// Returns (NodeRef, outputPath).
func EmitAS(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit Emitter) (NodeRef, VFS) {
	outVFS, inVFS := composeASPaths(instance, srcRel, srcVFS, in)
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	cmdArgs := composeASCmdArgs(instance, outputPath, inputPath, in)
	env := hostP.ToolEnv()

	allInputs := make([]VFS, 0, 1+len(in.IncludeInputs))
	allInputs = append(allInputs, inVFS)
	allInputs = append(allInputs, in.IncludeInputs...)

	tags := instance.Platform.Tags

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(B)",
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  allInputs,
		Outputs: []VFS{outVFS},
		KV: map[string]interface{}{
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

	return emit.Emit(bindNodePlatform(node, instance.Platform)), outVFS
}
