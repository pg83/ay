package main

import (
	"path"
	"strings"
)

func composeRodataOutputs(instance ModuleInstance, srcRel string) (VFS, VFS) {
	base := instance.Path + "/" + srcRel

	if strings.Contains(srcRel, "/") {
		base = instance.Path + "/_/" + srcRel
	}

	return Build(base + ".asm"), Build(base + instance.Platform.ObjectSuffix())
}

func EmitRD(instance ModuleInstance, srcRel string, srcVFS VFS, yasmLD NodeRef, emit Emitter) (NodeRef, VFS, VFS) {
	asmVFS, outVFS := composeRodataOutputs(instance, srcRel)
	toolName := path.Base(strings.TrimSuffix(srcRel, ".rodata"))

	pythonEnv := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}
	yasmEnv := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}, {Name: "YASM_TEST_SUITE", Value: "1"}}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: []STR{
					internStr(instance.Platform.Tools.Python3),
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
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		Tags:             instance.Platform.Tags,
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		DepRefs:          []NodeRef{yasmLD},
		ForeignDepRefs:   []NodeRef{yasmLD},
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform)), asmVFS, outVFS
}
