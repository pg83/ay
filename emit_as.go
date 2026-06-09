package main

func EmitAS(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit Emitter) (NodeRef, VFS) {
	outVFS, inVFS := composeASPaths(instance, srcRel, srcVFS, in)

	cmdArgs := composeASCmdArgs(instance, outVFS, inVFS, in)
	env := hostP.ToolEnv()

	allInputs := make([]VFS, 0, 1+len(in.IncludeInputs))
	allInputs = append(allInputs, inVFS)
	allInputs = append(allInputs, in.IncludeInputs...)

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     strB,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           allInputs,
		Outputs:          []VFS{outVFS},
		KV:               KV{P: pkAS, PC: pcLightGreen},
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
	}

	return emit.Emit(withResources(node, resourcePatternClangTool+instance.Platform.ClangVer)), outVFS
}
