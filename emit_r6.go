package main

import "strings"

const (
	ragel6BinSubrel            = "contrib/tools/ragel6/bin/"
	ragel6CanonicalRel         = "contrib/tools/ragel6/"
	ragel6DefaultFlagOptimized = "-CG2"
	ragel6DefaultFlagDebug     = "-CT0"
)

func canonicalizeRagel6Binary(v VFS) VFS {
	if !v.IsBuild() || !strings.HasPrefix(v.Rel(), ragel6BinSubrel) {
		return v
	}

	return Build(ragel6CanonicalRel + v.Rel()[len(ragel6BinSubrel):])
}

func EmitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, ragel6BinaryPath VFS, ragel6Flags []ARG, closure []VFS, emit Emitter) (NodeRef, VFS) {
	var outVFS VFS

	if strings.Contains(srcRel, "/") {
		outVFS = Build(instance.Path + "/_/" + srcRel + ".cpp")
	} else {
		outVFS = Build(instance.Path + "/" + srcRel + ".cpp")
	}

	inVFS := Source(instance.Path + "/" + srcRel)
	canonicalBinary := canonicalizeRagel6Binary(ragel6BinaryPath)

	effectiveFlags := ragel6Flags

	if len(effectiveFlags) == 0 {
		if instance.Platform.Ragel6Optimized {
			effectiveFlags = []ARG{internArg(ragel6DefaultFlagOptimized)}
		} else {
			effectiveFlags = []ARG{internArg(ragel6DefaultFlagDebug)}
		}
	}

	cmdArgs := make([]STR, 0, 5+len(effectiveFlags)+1)
	cmdArgs = append(cmdArgs, (canonicalBinary).str())
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
	inputs = append(inputs, canonicalBinary, inVFS)
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
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          []NodeRef{ragel6LD},
		ForeignDepRefs:   []NodeRef{ragel6LD},
	}

	return emit.Emit(node), outVFS
}
