package main

func EmitJVCPG4(
	instance ModuleInstance,
	src VFS,
	dst VFS,
	jvRef NodeRef,
	jvPrimary VFS,
	jvInputs []VFS,
	closure []VFS,
	scripts scriptDeps,
	emit Emitter,
) NodeRef {
	fsTools := copyFsToolsVFS

	cmdArgs := []STR{
		internStr(instance.Platform.Tools.Python3),
		(fsTools).str(),
		argCopy.str(),
		(src).str(),
		(dst).str(),
	}

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	inputCap := 2 + len(jvInputs) + len(closure) + 2
	inputs := make([]VFS, 0, inputCap)
	inputs = append(inputs, jvPrimary)

	if src != jvPrimary {
		inputs = append(inputs, src)
	}

	inputs = append(inputs, scripts[fsTools]...)
	inputs = append(inputs, jvInputs...)
	inputs = append(inputs, closure...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkCP, PC: pcLightCyan},
		Outputs:          []VFS{dst},
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		DepRefs:          []NodeRef{jvRef},
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform))
}

func EmitCP(instance ModuleInstance, src VFS, dst VFS, scripts scriptDeps, emit Emitter) NodeRef {
	return EmitCPWithDeps(instance, src, dst, nil, nil, scripts, emit)
}

// EmitCPWithDeps emits a CP (copy) node. extraInputs is the additional input
// closure to attach (e.g. the source's transitive #include closure when the
// COPY macro was declared WITH_CONTEXT, so that any header change retriggers
// the copy).
func EmitCPWithDeps(instance ModuleInstance, src VFS, dst VFS, depRefs []NodeRef, extraInputs []VFS, scripts scriptDeps, emit Emitter) NodeRef {
	fsTools := copyFsToolsVFS

	cmdArgs := []STR{
		internStr(instance.Platform.Tools.Python3),
		(fsTools).str(),
		argCopy.str(),
		(src).str(),
		(dst).str(),
	}

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	inputs := make([]VFS, 0, 3+len(extraInputs))
	inputs = append(inputs, scripts[fsTools]...)
	inputs = append(inputs, src)

	for _, v := range extraInputs {
		if v == src || v == dst {
			continue
		}

		inputs = append(inputs, v)
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkCP, PC: pcLightCyan},
		Outputs:          []VFS{dst},
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		DepRefs:          depRefs,
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform))
}
