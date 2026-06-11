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
		outVFS = Build(instance.Path.Rel() + "/_/" + stem + suffix)
	} else {
		outVFS = Build(instance.Path.Rel() + "/" + stem + suffix)
	}

	inVFS := srcVFS
	outputPath := outVFS.String()
	inputPath := inVFS.String()

	var predefinedFlags []string

	if !asmlibYasmModules[instance.Path.Rel()] {
		predefinedFlags = []string{"-g", "dwarf2"}
	}

	cmdArgs := make([]STR, 0, 20+len(predefinedFlags))
	cmdArgs = append(cmdArgs, yasmConstHead...)
	cmdArgs = append(cmdArgs,
		argD.str(), internStr("_"+string(instance.Platform.ISA)+"_"),
		argDYasm.str(),
	)
	cmdArgs = appendInternStrs(cmdArgs, predefinedFlags)
	cmdArgs = append(cmdArgs,
		argI.str(), argB.str(),
		argI.str(), argS.str(),
	)

	// Per-module `ADDINCL(FOR asm X)` entries arrive on in.AddIncl
	// (emit_sources.go merges them when the source is .asm). Append after
	// the base $(B)/$(S) pair so paths like
	// yt/yt/core/misc/isa_crc64/include precede `-o output input` and the
	// command shape matches REF.
	for _, p := range in.AddIncl {
		cmdArgs = append(cmdArgs, argI.str(), (p).str())
	}

	cmdArgs = append(cmdArgs,
		argDashO.str(), internStr(outputPath),
		internStr(inputPath),
	)

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envYASM_TEST_SUITE, Value: strOne}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: argChunks{cmdArgs},
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputChunks{{yasmBinaryVFS}, in.IncludeInputs},
		Outputs:          []VFS{outVFS},
		KV:               KV{P: pkAS, PC: pcLightGreen},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
	}

	node.ForeignDepRefs = []NodeRef{yasmLD}
	node.DepRefs = []NodeRef{yasmLD}
	return emit.Emit(node), outVFS
}

// yasmConstHead is the constant [yasm -f elf64 -D UNIX …replace…] lead of
// every yasm invocation (the AS-yasm and rodata nodes share it).
var yasmConstHead = []STR{
	internStr(yasmBinaryPath),
	argF.str(), argElf64.str(),
	argD.str(), argUnix.str(),
	argReplaceBB.str(),
	argReplaceSS.str(),
	argReplaceToolRootT.str(),
}
