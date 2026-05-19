package main

import "strings"

// emitASYasm composes the yasm-shaped AS node for a host-PIC `.asm`
// source — the asmlib-only counterpart to the clang AS path.
func emitASYasm(instance ModuleInstance, srcRel string, in ModuleCCInputs, yasmLD *NodeRef, emit Emitter) (NodeRef, VFS) {
	stem := strings.TrimSuffix(srcRel, ".asm")
	suffix := ".o"
	if instance.Platform.PIC {
		suffix = ".pic.o"
	}
	var outVFS VFS
	if strings.Contains(srcRel, "/") {
		outVFS = Build(instance.Path + "/_/" + stem + suffix)
	} else {
		outVFS = Build(instance.Path + "/" + stem + suffix)
	}
	inVFS := Source(instance.Path + "/" + srcRel)
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	var predefinedFlags []string
	if !asmlibYasmModules[instance.Path] {
		predefinedFlags = []string{"-g", "dwarf2"}
	}

	cmdArgs := make([]string, 0, 20+len(predefinedFlags))
	cmdArgs = append(cmdArgs,
		yasmBinaryPath,
		"-f", "elf64",
		"-D", "UNIX",
		"--replace=$(B)=/-B",
		"--replace=$(S)=/-S",
		"--replace=$(TOOL_ROOT)=/-T",
		"-D", "_"+string(instance.Platform.ISA)+"_",
		"-D_YASM_",
	)
	cmdArgs = append(cmdArgs, predefinedFlags...)
	cmdArgs = append(cmdArgs,
		"-I", "$(B)",
		"-I", "$(S)",
		"-o", outputPath,
		inputPath,
	)

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"YASM_TEST_SUITE":        "1",
	}

	allInputs := make([]VFS, 0, 2+len(in.IncludeInputs))
	allInputs = append(allInputs, yasmBinaryVFS)
	allInputs = append(allInputs, inVFS)
	allInputs = append(allInputs, in.IncludeInputs...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:          env,
		Inputs:       allInputs,
		Outputs:      []VFS{outVFS},
		HostPlatform: instance.Platform.IsHost,
		KV: map[string]string{
			"p":  "AS",
			"pc": "light-green",
		},
		Tags: instance.Platform.Tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	if yasmLD != nil {
		node.ForeignDepRefs = map[string][]NodeRef{
			"tool": {*yasmLD},
		}
		node.DepRefs = []NodeRef{*yasmLD}
	}

	return emit.Emit(node), outVFS
}
