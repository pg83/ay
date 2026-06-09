package main

func EmitJS(instance ModuleInstance, allName string, sources []string, closure []VFS, p *Platform, tc moduleToolchain, scripts scriptDeps, emit Emitter) (NodeRef, VFS) {
	joinSrcs := buildScriptsGenJoinSrcsPy

	outVFS := Build(instance.Path + "/" + allName)

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
		cmdArgs = append(cmdArgs, internStr(instance.Path+"/"+s))
	}

	cmdArgs = append(cmdArgs, argYaEndCommandFile.str())

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	inputs := make([]VFS, 0, 2+len(sources)+len(closure))
	inputs = append(inputs, scripts[joinSrcs]...)

	for _, s := range sources {
		inputs = append(inputs, Source(instance.Path+"/"+s))
	}

	inputs = append(inputs, closure...)

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
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		usesResources:    []string{resourcePatternYMakePython3},
	}

	return emit.Emit(node), outVFS
}
