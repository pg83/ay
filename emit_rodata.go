package main

import (
	"path"
	"strings"
)

var rodataScriptVFS = Intern("$(S)/build/scripts/rodata2asm.py")

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
				CmdArgs: []string{
					instance.Platform.Tools.Python3,
					rodataScriptVFS.String(),
					"--elf",
					toolName,
					srcVFS.String(),
					asmVFS.String(),
				},
				Env: pythonEnv,
			},
			{
				CmdArgs: []string{
					yasmBinaryPath,
					"-f", "elf64",
					"-D", "UNIX",
					"--replace=$(B)=/-B",
					"--replace=$(S)=/-S",
					"--replace=$(TOOL_ROOT)=/-T",
					"-D", "_" + string(instance.Platform.ISA) + "_",
					"-D_YASM_",
					"-g", "dwarf2",
					"-I", "$(B)",
					"-I", "$(S)",
					"-o", outVFS.String(),
					asmVFS.String(),
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
		KV:               KV{P: "RD", PC: "light-green"},
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
