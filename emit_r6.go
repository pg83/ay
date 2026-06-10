package main

import "strings"

var (
	ragel6ArgOptimized = internArg(ragel6DefaultFlagOptimized)
	ragel6ArgDebug     = internArg(ragel6DefaultFlagDebug)
)

const (
	ragel6DefaultFlagOptimized = "-CG2"
	ragel6DefaultFlagDebug     = "-CT0"
)

func EmitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, ragel6BinaryPath VFS, ragel6Flags []ARG, closure []VFS, emit Emitter) (NodeRef, VFS) {
	var outVFS VFS

	if strings.Contains(srcRel, "/") {
		outVFS = Build(instance.Path.Rel() + "/_/" + srcRel + ".cpp")
	} else {
		outVFS = Build(instance.Path.Rel() + "/" + srcRel + ".cpp")
	}

	inVFS := Source(instance.Path.Rel() + "/" + srcRel)

	effectiveFlags := ragel6Flags

	if len(effectiveFlags) == 0 {
		if instance.Platform.Ragel6Optimized {
			effectiveFlags = []ARG{ragel6ArgOptimized}
		} else {
			effectiveFlags = []ARG{ragel6ArgDebug}
		}
	}

	cmdArgs := make([]STR, 0, 5+len(effectiveFlags)+1)
	cmdArgs = append(cmdArgs, (ragel6BinaryPath).str())
	cmdArgs = appendArgStr(cmdArgs, effectiveFlags)
	cmdArgs = append(cmdArgs,
		argL.str(),
		argIS.str(),
		argDashO.str(),
		(outVFS).str(),
		(inVFS).str(),
	)

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	inputs := make([]VFS, 0, 2+len(closure))
	inputs = append(inputs, ragel6BinaryPath, inVFS)
	inputs = append(inputs, closure...)

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		Outputs:          []VFS{outVFS},
		KV:               KV{P: pkR6, PC: pcYellow},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          []NodeRef{ragel6LD},
		ForeignDepRefs:   []NodeRef{ragel6LD},
	}

	return emit.Emit(node), outVFS
}
