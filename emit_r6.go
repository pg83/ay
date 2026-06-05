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

func EmitR6(instance ModuleInstance, srcRel string, ragel6LD NodeRef, ragel6BinaryPath VFS, ragel6Flags []string, closure []VFS, emit Emitter) (NodeRef, VFS) {
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
			effectiveFlags = []string{ragel6DefaultFlagOptimized}
		} else {
			effectiveFlags = []string{ragel6DefaultFlagDebug}
		}
	}

	cmdArgs := make([]string, 0, 5+len(effectiveFlags)+1)
	cmdArgs = append(cmdArgs, canonicalBinary.String())
	cmdArgs = append(cmdArgs, effectiveFlags...)
	cmdArgs = append(cmdArgs,
		"-L",
		"-I$(S)",
		"-o",
		outVFS.String(),
		inVFS.String(),
	)

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	inputs := make([]VFS, 0, 2+len(closure))
	inputs = append(inputs, canonicalBinary, inVFS)
	inputs = append(inputs, closure...)

	tags := instance.Platform.Tags

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		Outputs:          []VFS{outVFS},
		KV:               KV{P: "R6", PC: "yellow"},
		Tags:             tags,
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          []NodeRef{ragel6LD},
		ForeignDepRefs:   []NodeRef{ragel6LD},
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform)), outVFS
}
