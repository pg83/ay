package main

// cp.go — emitter for CP (file-copy) nodes.
//
// EmitCP produces a single Node matching the shape ymake itself
// produces for a CP macro invocation. The only CP node in the M2
// reference graph is contrib/libs/musl/include/musl.py →
// musl.py.pyplugin. The function is structurally correct for any
// src/dst pair; only the musl case is regression-tested byte-exact
// against the reference graph.
//
// PR-23 retrofitted the signature to take a `ModuleInstance`
// instead of a (platform, moduleDir) pair.

// EmitJVCPG4 emits a CP node that renames an ANTLR-generated .cpp file
// to its .g4.cpp form (e.g. CmdLexer.cpp → CmdLexer.g4.cpp).
//
// Differs from EmitCP: carries DepRefs = [jvRef] and an extended inputs
// list that prepends the JV primary output and JV inputs (grammar .g4
// files, stdout2stderr.py, antlr4.jar) before the include closure.
//
// Inputs layout (matching the reference sg2.json shape):
//
//	[jvPrimaryOutput, (srcAbsPath when != jvPrimaryOutput), fsToolsPath,
//	 procCmdFiles, jvInputs..., closure...]
//
// The cmd_args copy srcAbsPath (the specific .cpp being renamed).
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

	// Inputs: jvPrimary first, then src only when it differs from
	// jvPrimary (i.e. this is the parser output, not the lexer).
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
		KV: map[string]string{
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

	return emit.Emit(node)
}

// EmitCP emits a CP node copying srcAbsPath to dstAbsPath.
// Used today only by contrib/libs/musl/include/ya.make for the
// musl.py.pyplugin file.
//
// cmd_args shape (5 args, verified against reference):
//
//	/ix/realm/pg/bin/python3
//	$(S)/build/scripts/fs_tools.py
//	copy
//	<srcAbsPath>
//	<dstAbsPath>
func EmitCP(instance ModuleInstance, src VFS, dst VFS, emit Emitter) NodeRef {
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
		KV: map[string]string{
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
	}

	return emit.Emit(node)
}
