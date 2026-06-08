package main

var (
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
	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	cmdArgs := []STR{
		internStr(instance.Platform.Tools.Python3),
		(configureFilePyVFS).str(),
		(srcVFS).str(),
		(outVFS).str(),
	}
	cmdArgs = appendInternStrs(cmdArgs, cfgVars)

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
		Env:     env,
		Inputs:  inputs,
		KV:      KV{P: pkCF, PC: pcYellow},
		Outputs: []VFS{outVFS},
		Tags:    []string{},
		TargetProperties: func() TargetProperties {
			tp := TargetProperties{ModuleDir: moduleDir}

			if moduleTag != "" {
				tp.ModuleTag = moduleTag
			}

			return tp
		}(),
		Platform:     string(instance.Platform.Target),
		Requirements: Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:      []NodeRef{},
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform)), outVFS
}
