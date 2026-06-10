package main

func EmitJS(instance ModuleInstance, allName string, sources []string, closure []VFS, p *Platform, tc moduleToolchain, scripts scriptDeps, emit Emitter) (NodeRef, VFS) {
	joinSrcs := buildScriptsGenJoinSrcsPy

	outVFS := Build(instance.Path.Rel() + "/" + allName)

	statsPlatform := instance.Platform

	if p != nil {
		statsPlatform = p
	}

	cmdArgs := make([]STR, 0, 4+len(sources))
	cmdArgs = append(cmdArgs,
		tc.Python3,
		(joinSrcs).str(),
		(outVFS).str(),
		argYaStartCommandFile.str(),
	)

	for _, s := range sources {
		cmdArgs = append(cmdArgs, internStr(instance.Path.Rel()+"/"+s))
	}

	cmdArgs = append(cmdArgs, argYaEndCommandFile.str())

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	srcVFSs := make([]VFS, 0, len(sources))

	for _, s := range sources {
		srcVFSs = append(srcVFSs, Source(instance.Path.Rel()+"/"+s))
	}

	// Chunked: the join closure (a shared cached slice) is referenced, not copied.
	inputs := inputChunks{scripts[joinSrcs], srcVFSs, closure}

	node := &Node{
		Platform: statsPlatform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkJS, PC: pcMagenta},
		Outputs:          []VFS{outVFS},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		usesResources:    []string{resourcePatternYMakePython3},
	}

	return emit.Emit(node), outVFS
}
