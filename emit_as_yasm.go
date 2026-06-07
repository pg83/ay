package main

import "strings"

func emitASYasm(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, yasmLD NodeRef, emit Emitter) (NodeRef, VFS) {
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

	inVFS := srcVFS
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	var predefinedFlags []string

	if !asmlibYasmModules[instance.Path] {
		predefinedFlags = []string{"-g", "dwarf2"}
	}

	cmdArgs := make([]ANY, 0, 20+len(predefinedFlags))
	cmdArgs = append(cmdArgs,
		internAny(yasmBinaryPath),
		anyF, anyElf64,
		anyD, anyUnix,
		anyReplaceBB,
		anyReplaceSS,
		anyReplaceToolRootT,
		anyD, internAny("_"+string(instance.Platform.ISA)+"_"),
		anyDYasm,
	)
	cmdArgs = appendStringAny(cmdArgs, predefinedFlags)
	cmdArgs = append(cmdArgs,
		anyI, anyB,
		anyI, anyS,
	)

	// Per-module `ADDINCL(FOR asm X)` entries arrive on in.AddIncl
	// (emit_sources.go merges them when the source is .asm). Append after
	// the base $(B)/$(S) pair so paths like
	// yt/yt/core/misc/isa_crc64/include precede `-o output input` and the
	// command shape matches REF.
	for _, p := range in.AddIncl {
		cmdArgs = append(cmdArgs, anyI, vfsAny(p))
	}

	cmdArgs = append(cmdArgs,
		argDashO, internAny(outputPath),
		internAny(inputPath),
	)

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}, {Name: "YASM_TEST_SUITE", Value: "1"}}

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
		Env:              env,
		Inputs:           allInputs,
		Outputs:          []VFS{outVFS},
		KV:               KV{P: pkAS, PC: pcLightGreen},
		Tags:             instance.Platform.Tags,
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
	}

	node.ForeignDepRefs = []NodeRef{yasmLD}
	node.DepRefs = []NodeRef{yasmLD}
	return emit.Emit(bindNodePlatform(node, instance.Platform)), outVFS
}
