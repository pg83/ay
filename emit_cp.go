package main

// cp.go — emitter for CP (file-copy) nodes.
//
// EmitCP produces a Node matching the shape ymake produces for a CP macro
// invocation. Structurally correct for any src/dst; the byte-exact
// regression pin covers the contrib/libs/musl/include musl.py → .pyplugin
// case.

// EmitJVCPG4 emits a CP node that renames an ANTLR-generated .cpp to its
// .g4.cpp form (e.g. CmdLexer.cpp → CmdLexer.g4.cpp). Carries DepRefs=[jvRef]
// and inputs [jvPrimary, (src if != jvPrimary), fsTools, procCmdFiles,
// jvInputs..., closure...]; cmd_args copy srcAbsPath.
func EmitJVCPG4(
	instance ModuleInstance,
	src VFS,
	dst VFS,
	jvRef NodeRef,
	jvPrimary VFS,
	jvInputs []VFS,
	closure []VFS,
	emit Emitter,
) NodeRef {
	fsTools := Source("build/scripts/fs_tools.py")
	procCmdFiles := Source("build/scripts/process_command_files.py")

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		fsTools.String(),
		"copy",
		src.String(),
		dst.String(),
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	inputCap := 2 + len(jvInputs) + len(closure) + 2
	inputs := make([]VFS, 0, inputCap)
	inputs = append(inputs, jvPrimary)
	if src != jvPrimary {
		inputs = append(inputs, src)
	}
	inputs = append(inputs, fsTools, procCmdFiles)
	inputs = append(inputs, jvInputs...)
	inputs = append(inputs, closure...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]interface{}{
			"p":  "CP",
			"pc": "light-cyan",
		},
		Outputs:  []VFS{dst},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		DepRefs: []NodeRef{jvRef},
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform))
}

// EmitCP emits a CP node copying srcAbsPath to dstAbsPath. Today exercised
// only by contrib/libs/musl/include/ya.make (musl.py.pyplugin).
// cmd_args: [python3, $(S)/build/scripts/fs_tools.py, copy, src, dst].
func EmitCP(instance ModuleInstance, src VFS, dst VFS, emit Emitter) NodeRef {
	return EmitCPWithDeps(instance, src, dst, nil, emit)
}

func EmitCPWithDeps(instance ModuleInstance, src VFS, dst VFS, depRefs []NodeRef, emit Emitter) NodeRef {
	fsTools := Source("build/scripts/fs_tools.py")
	procCmdFiles := Source("build/scripts/process_command_files.py")

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		fsTools.String(),
		"copy",
		src.String(),
		dst.String(),
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	inputs := []VFS{
		fsTools,
		procCmdFiles,
		src,
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]interface{}{
			"p":  "CP",
			"pc": "light-cyan",
		},
		Outputs:  []VFS{dst},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		DepRefs: depRefs,
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform))
}
