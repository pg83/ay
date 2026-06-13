package main

import "strings"

var (
	ragel6ArgOptimized = internArg(ragel6DefaultFlagOptimized)
	ragel6ArgDebug     = internArg(ragel6DefaultFlagDebug)
	// ragel6ConstArgs is the constant [-L -I$(S) -o] span of every R6 command.
	ragel6ConstArgs = []STR{argL.str(), argIS.str(), argDashO.str()}
)

const (
	ragel6DefaultFlagOptimized = "-CG2"
	ragel6DefaultFlagDebug     = "-CT0"
)

// ragel6OutVFS is the generated-cpp path of a ragel source — shared with the
// emit_sources.go caller, which registers the output's induced includes and
// walks its closure before the R6 node exists.
func ragel6OutVFS(instance ModuleInstance, srcRel string) VFS {
	if strings.Contains(srcRel, "/") {
		return build(instance.Path.rel() + "/_/" + srcRel + ".cpp")
	}

	return build(instance.Path.rel() + "/" + srcRel + ".cpp")
}

func emitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, ragel6BinaryPath VFS, ragel6Flags []ARG, closure []VFS, emit Emitter) (NodeRef, VFS) {
	outVFS := ragel6OutVFS(instance, srcRel)

	inVFS := source(instance.Path.rel() + "/" + srcRel)

	effectiveFlags := ragel6Flags

	if len(effectiveFlags) == 0 {
		if instance.Platform.Ragel6Optimized {
			effectiveFlags = []ARG{ragel6ArgOptimized}
		} else {
			effectiveFlags = []ARG{ragel6ArgDebug}
		}
	}

	head := make([]STR, 0, 1+len(effectiveFlags))
	head = append(head, (ragel6BinaryPath).str())
	head = appendArgStr(head, effectiveFlags)
	cmdArgs := chunkList(head, ragel6ConstArgs, strList((outVFS).str(), (inVFS).str()))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: cmdList(Cmd{CmdArgs: cmdArgs,
			Env: env}),
		Env:              env,
		Inputs:           inputList(vfsList(ragel6BinaryPath), closure),
		Outputs:          vfsList(outVFS),
		KV:               KV{P: pkR6, PC: pcYellow},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          []NodeRef{ragel6LD},
		ForeignDepRefs:   []NodeRef{ragel6LD},
	}

	return emit.emit(node), outVFS
}
