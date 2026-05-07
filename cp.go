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

// EmitCP emits a CP node copying srcAbsPath to dstAbsPath.
// Used today only by contrib/libs/musl/include/ya.make for the
// musl.py.pyplugin file.
//
// cmd_args shape (5 args, verified against reference):
//
//	/ix/realm/pg/bin/python3
//	$(SOURCE_ROOT)/build/scripts/fs_tools.py
//	copy
//	<srcAbsPath>
//	<dstAbsPath>
func EmitCP(instance ModuleInstance, srcAbsPath, dstAbsPath string, emit Emitter) NodeRef {
	const (
		python3Path  = "/ix/realm/pg/bin/python3"
		fsToolsPath  = "$(SOURCE_ROOT)/build/scripts/fs_tools.py"
		procCmdFiles = "$(SOURCE_ROOT)/build/scripts/process_command_files.py"
	)

	cmdArgs := []string{
		python3Path,
		fsToolsPath,
		"copy",
		srcAbsPath,
		dstAbsPath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	inputs := []string{
		fsToolsPath,
		procCmdFiles,
		srcAbsPath,
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
		Outputs:  []string{dstAbsPath},
		Platform: string(instance.Target),
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
