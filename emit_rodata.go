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

	return Build(base + ".asm"), Build(base + ".o")
}

func EmitRD(instance ModuleInstance, srcRel string, srcVFS VFS, yasmLD NodeRef, emit Emitter) (NodeRef, VFS, VFS) {
	asmVFS, outVFS := composeRodataOutputs(instance, srcRel)
	toolName := path.Base(strings.TrimSuffix(srcRel, ".rodata"))

	pythonEnv := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}
	yasmEnv := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"YASM_TEST_SUITE":        "1",
	}

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
		KV: map[string]interface{}{
			"p":  "RD",
			"pc": "light-green",
		},
		Outputs:  []VFS{asmVFS, outVFS},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: instance.Platform.Tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		DepRefs: []NodeRef{yasmLD},
		ForeignDepRefs: map[string][]NodeRef{
			"tool": []NodeRef{yasmLD},
		},
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform)), asmVFS, outVFS
}
