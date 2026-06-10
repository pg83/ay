package main

import (
	"path"
	"strings"
)

func composeRodataOutputs(instance ModuleInstance, srcRel string) (VFS, VFS) {
	base := instance.Path.Rel() + "/" + srcRel

	if strings.Contains(srcRel, "/") {
		base = instance.Path.Rel() + "/_/" + srcRel
	}

	return Build(base + ".asm"), Build(base + instance.Platform.ObjectSuffix())
}

func EmitRD(instance ModuleInstance, srcRel string, srcVFS VFS, yasmLD NodeRef, tc moduleToolchain, emit Emitter) (NodeRef, VFS, VFS) {
	asmVFS, outVFS := composeRodataOutputs(instance, srcRel)
	toolName := path.Base(strings.TrimSuffix(srcRel, ".rodata"))

	pythonEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	yasmEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envYASM_TEST_SUITE, Value: strOne}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: []STR{
					tc.Python3,
					(rodataScriptVFS).str(),
					argElf.str(),
					internStr(toolName),
					(srcVFS).str(),
					(asmVFS).str(),
				},
				Env: pythonEnv,
			},
			{
				CmdArgs: []STR{
					internStr(yasmBinaryPath),
					argF.str(), argElf64.str(),
					argD.str(), argUnix.str(),
					argReplaceBB.str(),
					argReplaceSS.str(),
					argReplaceToolRootT.str(),
					argD.str(), internStr("_" + string(instance.Platform.ISA) + "_"),
					argDYasm.str(),
					argDashG.str(), argDwarf2.str(),
					argI.str(), argB.str(),
					argI.str(), argS.str(),
					argDashO.str(), (outVFS).str(),
					(asmVFS).str(),
				},
				Env: yasmEnv,
			},
		},
		Env: yasmEnv,
		Inputs: []VFS{
			yasmBinaryVFS,
			rodataScriptVFS,
			srcVFS,
		},
		KV:               KV{P: pkRD, PC: pcLightGreen},
		Outputs:          []VFS{asmVFS, outVFS},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		DepRefs:          []NodeRef{yasmLD},
		ForeignDepRefs:   []NodeRef{yasmLD},
		usesResources:    []string{resourcePatternYMakePython3},
	}

	return emit.Emit(node), asmVFS, outVFS
}
