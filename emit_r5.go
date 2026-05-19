package main

import "strings"

// EmitR5 emits an R5 node (two cmds: ragel5 → .tmp; rlgen-cd → .rl5.cpp).
// tmpPath = $(B)/<modulePath>/<srcRel>.tmp
// cppPath = $(B)/<modulePath>/<srcRel without .rl>.rl5.cpp
// Returns (R5 NodeRef, tmpPath, cppPath).
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

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

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
	if ragel5LD != (NodeRef{}) {
		depRefs = append(depRefs, ragel5LD)
	}
	if rlgenCdLD != (NodeRef{}) {
		depRefs = append(depRefs, rlgenCdLD)
	}

	node := &Node{
		Cmds:    []Cmd{cmd0, cmd1},
		Env:     env,
		Inputs:  inputs,
		Outputs: []VFS{tmpVFS, cppVFS},
		KV: map[string]string{
			"p":  "R5",
			"pc": "yellow",
		},
		Tags: []string{"tool"},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs:        depRefs,
		ForeignDepRefs: map[string][]NodeRef{"tool": depRefs},
	}

	return emit.Emit(node), tmpVFS, cppVFS
}
