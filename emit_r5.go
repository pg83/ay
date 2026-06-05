package main

import "strings"

func EmitR5(
	instance ModuleInstance,
	srcRel string,
	ragel5LD NodeRef,
	rlgenCdLD NodeRef,
	ragel5BinPath VFS,
	rlgenCdBinPath VFS,
	emit Emitter,
) (NodeRef, VFS, VFS) {
	srcVFS := Source(instance.Path + "/" + srcRel)
	tmpVFS := Build(instance.Path + "/" + srcRel + ".tmp")
	cppVFS := Build(instance.Path + "/" + strings.TrimSuffix(srcRel, ".rl") + ".rl5.cpp")

	tmpPath := tmpVFS.String()
	cppPath := cppVFS.String()
	srcPath := srcVFS.String()

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	cmd0 := Cmd{
		CmdArgs: []string{
			ragel5BinPath.String(),
			"-o",
			tmpPath,
			srcPath,
		},
		Env: env,
	}
	cmd1 := Cmd{
		CmdArgs: []string{
			rlgenCdBinPath.String(),
			"-G2",
			"-o",
			cppPath,
			tmpPath,
		},
		Env: env,
	}
	inputs := []VFS{ragel5BinPath, rlgenCdBinPath, srcVFS}
	depRefs := make([]NodeRef, 0, 2)

	if ragel5LD != (NodeRef(0)) {
		depRefs = append(depRefs, ragel5LD)
	}

	if rlgenCdLD != (NodeRef(0)) {
		depRefs = append(depRefs, rlgenCdLD)
	}

	node := &Node{
		Cmds:             []Cmd{cmd0, cmd1},
		Env:              env,
		Inputs:           inputs,
		Outputs:          []VFS{tmpVFS, cppVFS},
		KV:               KV{P: "R5", PC: "yellow"},
		Tags:             []string{"tool"},
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   depRefs,
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform)), tmpVFS, cppVFS
}
