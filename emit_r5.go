package main

import "strings"

func emitR5(
	instance ModuleInstance,
	srcRel string,
	ragel5LD NodeRef,
	rlgenCdLD NodeRef,
	ragel5BinPath VFS,
	rlgenCdBinPath VFS,
	emit Emitter,
) (NodeRef, VFS, VFS) {
	na := emit.nodeArenas()

	srcVFS := source(instance.Path.rel() + "/" + srcRel)
	tmpVFS := build(instance.Path.rel() + "/" + srcRel + ".tmp")
	cppVFS := build(instance.Path.rel() + "/" + strings.TrimSuffix(srcRel, ".rl") + ".rl5.cpp")

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmd0 := Cmd{
		CmdArgs: na.chunkList(na.strList((ragel5BinPath).str(),
			argDashO.str(),
			(tmpVFS).str(),
			(srcVFS).str())),
		Env: env,
	}
	cmd1 := Cmd{
		CmdArgs: na.chunkList(na.strList((rlgenCdBinPath).str(),
			argG2.str(),
			argDashO.str(),
			(cppVFS).str(),
			(tmpVFS).str())),
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
		Platform:         instance.Platform,
		Cmds:             na.cmdList(cmd0, cmd1),
		Env:              env,
		Inputs:           na.inputList(inputs),
		Outputs:          na.vfsList(tmpVFS, cppVFS),
		KV:               KV{P: pkR5, PC: pcYellow},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   depRefs,
	}

	return emit.emit(node), tmpVFS, cppVFS
}
