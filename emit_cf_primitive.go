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
	moduleTag STR,
	tc moduleToolchain,
	emit Emitter,
) (NodeRef, VFS) {
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmdArgs := []STR{
		tc.Python3,
		(configureFilePyVFS).str(),
		(srcVFS).str(),
		(outVFS).str(),
	}
	cmdArgs = appendInternStrs(cmdArgs, cfgVars)

	inputs := make([]VFS, 0, 2+len(includeInputs))
	inputs = append(inputs, configureFilePyVFS, srcVFS)
	inputs = append(inputs, includeInputs...)

	node := &Node{
		Platform: instance.Platform,
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
		TargetProperties: func() TargetProperties {
			tp := TargetProperties{ModuleDir: moduleDir}

			if moduleTag != 0 {
				tp.ModuleTag = moduleTag
			}

			return tp
		}(),
		Requirements:  Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:       []NodeRef{},
		usesResources: []string{resourcePatternYMakePython3},
	}

	return emit.Emit(node), outVFS
}
