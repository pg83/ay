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
				CmdArgs: []ANY{
					internAny(instance.Platform.Tools.Python3),
					vfsAny(rodataScriptVFS),
					anyElf,
					internAny(toolName),
					vfsAny(srcVFS),
					vfsAny(asmVFS),
				},
				Env: pythonEnv,
			},
			{
				CmdArgs: []ANY{
					internAny(yasmBinaryPath),
					anyF, anyElf64,
					anyD, anyUnix,
					anyReplaceBB,
					anyReplaceSS,
					anyReplaceToolRootT,
					anyD, internAny("_" + string(instance.Platform.ISA) + "_"),
					anyDYasm,
					anyG, anyDwarf2,
					anyI, anyB,
					anyI, anyS,
					argDashO, vfsAny(outVFS),
					vfsAny(asmVFS),
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
