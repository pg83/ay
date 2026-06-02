package main

var (
	configureFilePyVFS  = Intern("$(S)/build/scripts/configure_file.py")
	configureFilePyPath = configureFilePyVFS.String()
)

const buildTypeDebug = "BUILD_TYPE=DEBUG"

func EmitCF(
	instance ModuleInstance,
	srcVFS VFS,
	outVFS VFS,
	cfgVars []string,
	includeInputs []VFS,
	moduleDir string,
	moduleTag string,
	emit Emitter,
) (NodeRef, VFS) {
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		configureFilePyPath,
		srcVFS.String(),
		outVFS.String(),
	}
	cmdArgs = append(cmdArgs, cfgVars...)

	inputs := make([]VFS, 0, 2+len(includeInputs))
	inputs = append(inputs, configureFilePyVFS, srcVFS)
	inputs = append(inputs, includeInputs...)

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
			"p":  "CF",
			"pc": "yellow",
		},
		Outputs: []VFS{outVFS},
		Tags:    []string{},
		TargetProperties: func() map[string]string {
			tp := map[string]string{"module_dir": moduleDir}

			if moduleTag != "" {
				tp["module_tag"] = moduleTag
			}

			return tp
		}(),
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform)), outVFS
}
