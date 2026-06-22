package main

func emitJVCPG4(
	instance ModuleInstance,
	src VFS,
	dst VFS,
	jvRef NodeRef,
	jvPrimary VFS,
	jvInputs []VFS,
	closure []VFS,
	id NodeRef,
	tc ModuleToolchain,
	scripts ScriptDeps,
	emit Emitter,
) {
	na := emit.nodeArenas()

	fsTools := copyFsToolsVFS

	cmdArgs := []STR{
		tc.Python3,
		(fsTools).str(),
		argCopy.str(),
		(src).str(),
		(dst).str(),
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	head := make([]VFS, 0, 2)
	head = append(head, jvPrimary)

	if src != jvPrimary {
		head = append(head, src)
	}

	// fs_tools closure, jvInputs and closure are referenced as their own chunks,
	// never copied.
	inputs := na.inputList(head, scripts[fsTools], jvInputs, closure)

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkCP, PC: pcLightCyan},
		Outputs:          na.vfsList(dst),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		DepRefs:          []NodeRef{jvRef},
		Resources:        usesPython3,
	}

	emit.emitReserved(node, id)
}

func emitCP(instance ModuleInstance, src VFS, dst VFS, tc ModuleToolchain, scripts ScriptDeps, emit Emitter) NodeRef {
	id := emit.reserve()
	emitCPWithDeps(instance, src, dst, nil, nil, id, 0, tc, scripts, emit)

	return id
}

// EmitCPWithDeps emits a CP (copy) node. extraInputs is the additional input
// closure to attach (e.g. the source's #include closure under WITH_CONTEXT, so
// a header change retriggers the copy).
func emitCPWithDeps(instance ModuleInstance, src VFS, dst VFS, depRefs []NodeRef, extraInputs []VFS, id NodeRef, moduleTag STR, tc ModuleToolchain, scripts ScriptDeps, emit Emitter) {
	na := emit.nodeArenas()

	fsTools := copyFsToolsVFS

	cmdArgs := []STR{
		tc.Python3,
		(fsTools).str(),
		argCopy.str(),
		(src).str(),
		(dst).str(),
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// fs_tools closure rides as its own chunk; extraInputs is built locally
	// because its membership depends on the src/dst exclusion below.
	ownInputs := make([]VFS, 0, 1+len(extraInputs))
	ownInputs = append(ownInputs, src)

	for _, v := range extraInputs {
		if v == src || v == dst {
			continue
		}

		ownInputs = append(ownInputs, v)
	}

	inputs := na.inputList(scripts[fsTools], ownInputs)

	tp := TargetProperties{ModuleDir: instance.Path.rel()}

	if moduleTag != 0 {
		tp.ModuleTag = moduleTag
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkCP, PC: pcLightCyan},
		Outputs:          na.vfsList(dst),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: tp,
		DepRefs:          depRefs,
		Resources:        usesPython3,
	}

	emit.emitReserved(node, id)
}
