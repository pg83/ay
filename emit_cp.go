package main

func EmitJVCPG4(
	instance ModuleInstance,
	src VFS,
	dst VFS,
	jvRef NodeRef,
	jvPrimary VFS,
	jvInputs []VFS,
	closure []VFS,
	tc moduleToolchain,
	scripts scriptDeps,
	emit Emitter,
) NodeRef {
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

	// The fs_tools closure (shared table slice), jvInputs and closure (caller's
	// slices) are referenced as their own chunks, never copied.
	inputs := inputChunks{head, scripts[fsTools], jvInputs, closure}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: argChunks{cmdArgs},
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkCP, PC: pcLightCyan},
		Outputs:          []VFS{dst},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		DepRefs:          []NodeRef{jvRef},
		usesResources:    []string{resourcePatternYMakePython3},
	}

	return emit.Emit(node)
}

func EmitCP(instance ModuleInstance, src VFS, dst VFS, tc moduleToolchain, scripts scriptDeps, emit Emitter) NodeRef {
	return EmitCPWithDeps(instance, src, dst, nil, nil, tc, scripts, emit)
}

// EmitCPWithDeps emits a CP (copy) node. extraInputs is the additional input
// closure to attach (e.g. the source's transitive #include closure when the
// COPY macro was declared WITH_CONTEXT, so that any header change retriggers
// the copy).
func EmitCPWithDeps(instance ModuleInstance, src VFS, dst VFS, depRefs []NodeRef, extraInputs []VFS, tc moduleToolchain, scripts scriptDeps, emit Emitter) NodeRef {
	fsTools := copyFsToolsVFS

	cmdArgs := []STR{
		tc.Python3,
		(fsTools).str(),
		argCopy.str(),
		(src).str(),
		(dst).str(),
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// The fs_tools closure (shared table slice) is referenced as its own chunk;
	// extraInputs stays a locally built chunk — its membership depends on the
	// per-node src/dst exclusion below.
	ownInputs := make([]VFS, 0, 1+len(extraInputs))
	ownInputs = append(ownInputs, src)

	for _, v := range extraInputs {
		if v == src || v == dst {
			continue
		}

		ownInputs = append(ownInputs, v)
	}

	inputs := inputChunks{scripts[fsTools], ownInputs}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: argChunks{cmdArgs},
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkCP, PC: pcLightCyan},
		Outputs:          []VFS{dst},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		DepRefs:          depRefs,
		usesResources:    []string{resourcePatternYMakePython3},
	}

	return emit.Emit(node)
}
