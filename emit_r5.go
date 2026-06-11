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
	srcVFS := Source(instance.Path.Rel() + "/" + srcRel)
	tmpVFS := Build(instance.Path.Rel() + "/" + srcRel + ".tmp")
	cppVFS := Build(instance.Path.Rel() + "/" + strings.TrimSuffix(srcRel, ".rl") + ".rl5.cpp")

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmd0 := Cmd{
		CmdArgs: argChunks{[]STR{
			(ragel5BinPath).str(),
			argDashO.str(),
			(tmpVFS).str(),
			(srcVFS).str(),
		}},
		Env: env,
	}
	cmd1 := Cmd{
		CmdArgs: argChunks{[]STR{
			(rlgenCdBinPath).str(),
			argG2.str(),
			argDashO.str(),
			(cppVFS).str(),
			(tmpVFS).str(),
		}},
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
		Cmds:             []Cmd{cmd0, cmd1},
		Env:              env,
		Inputs:           inputChunks{inputs},
		Outputs:          []VFS{tmpVFS, cppVFS},
		KV:               KV{P: pkR5, PC: pcYellow},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   depRefs,
	}

	return emit.Emit(node), tmpVFS, cppVFS
}
