package main

func emitJS(instance ModuleInstance, allName string, sources []string, closure []VFS, p *Platform, tc ModuleToolchain, scripts ScriptDeps, emit *StreamingEmitter) (NodeRef, VFS) {
	na := emit.nodeArenas()

	joinSrcs := buildScriptsGenJoinSrcsPy

	outVFS := build(instance.Path.rel() + "/" + allName)

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
		cmdArgs = append(cmdArgs, internStr(instance.Path.rel()+"/"+s))
	}

	cmdArgs = append(cmdArgs, argYaEndCommandFile.str())

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	srcVFSs := make([]VFS, 0, len(sources))

	for _, s := range sources {
		srcVFSs = append(srcVFSs, source(instance.Path.rel()+"/"+s))
	}

	inputs := na.inputList(scripts[joinSrcs], srcVFSs, closure)

	node := &Node{
		Platform: statsPlatform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:          env,
		Inputs:       inputs,
		KV:           KV{P: pkJS, PC: pcMagenta},
		Outputs:      na.vfsList(outVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	}

	return emit.emit(node), outVFS
}
