package main

import (
	"strings"
)

var yasmBinaryPath = yasmBinaryVFS.string()

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

func EmitAS(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit Emitter) (NodeRef, VFS) {
	outVFS, inVFS := composeASPaths(instance, srcRel, srcVFS, in)

	cmdArgs := composeASCmdArgs(instance, outVFS, inVFS, in)
	env := hostP.toolEnv()

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: ArgChunks{cmdArgs},
				Cwd:     strB,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           InputChunks{in.IncludeInputs},
		Outputs:          []VFS{outVFS},
		KV:               KV{P: pkAS, PC: pcLightGreen},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		usesResources:    []string{resourcePatternClangTool + instance.Platform.ClangVer},
	}

	return emit.emit(node), outVFS
}

func composeASPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs) (out, input VFS) {
	if srcVFS.isSource() && srcVFS.rel() != instance.Path.rel()+"/"+srcRel {
		outputRel := composeSrcDirOutputRel(instance.Path.rel(), srcVFS.rel())
		return Build(instance.Path.rel() + "/" + outputRel + ".o"), srcVFS
	}

	var outRel string
	outName := srcRel + ".o"

	if strings.HasSuffix(srcRel, ".asm") {
		outName = strings.TrimSuffix(srcRel, ".asm") + ".o"
	}

	if strings.Contains(srcRel, "/") {
		outRel = instance.Path.rel() + "/_/" + outName
	} else {
		outRel = instance.Path.rel() + "/" + outName
	}

	return Build(outRel), srcVFS
}

func composeASCmdArgs(instance ModuleInstance, outVFS, inVFS VFS, in ModuleCCInputs) []STR {
	bundle := compileFlagBundleFor(instance.Platform)
	prologueArgs := 2 + len(bundle.ArchArgs) + len(instance.Platform.SysrootArgs)

	warnBundle := pickWarningFlags(in.Flags.NoCompilerWarnings, in.Flags.NoWShadow)

	ownCFlags := composeOwnAndPeerCFlagsAtOwnSlot(in, instance.Platform)

	includes := composeASIncludes(in)

	betweenBlocks := len(catboostOpenSourceDefine)
	betweenBlocks += len(in.ModuleScopeCFlags)

	fixed := prologueArgs + len(debugPrefixMapFlags) + len(xclangDebugCompilationDir) +
		len(bundle.CFlags) + len(warnBundle) + len(bundle.Defines) + len(ownCFlags) +
		len(bundle.NoLibcBlock) + betweenBlocks + len(bundle.NoLibcBlock) + len(in.SFlags) + 4
	cmdArgs := make([]STR, 0, fixed+len(includes))

	cmdArgs = append(cmdArgs, in.TC.CC, instance.Platform.TargetArg)
	cmdArgs = appendArgStr(cmdArgs, bundle.ArchArgs)
	cmdArgs = append(cmdArgs, instance.Platform.SysrootArgs...)
	cmdArgs = appendCompileFlagPipeline(cmdArgs, bundle, warnBundle, bundle.Defines, ownCFlags, in.ModuleScopeCFlags, catboostOpenSourceDefineFor(instance.Platform))
	cmdArgs = appendArgStr(cmdArgs, in.SFlags)
	cmdArgs = append(cmdArgs, argDashC.str(), argDashO.str(), (outVFS).str(), (inVFS).str())
	cmdArgs = append(cmdArgs, includes...)

	return cmdArgs
}

func composeASIncludes(in ModuleCCInputs) []STR {
	out := make([]STR, 0, len(ccIncludesPrefix)+len(in.AddIncl)+len(in.PeerAddInclGlobal))
	out = appendArgStr(out, ccIncludesPrefix)
	out = appendAddIncl(out, in.AddIncl, in.InclArgs)
	out = appendAddIncl(out, in.PeerAddInclGlobal, in.InclArgs)

	return out
}

func emitASYasm(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, yasmLD NodeRef, emit Emitter) (NodeRef, VFS) {
	stem := strings.TrimSuffix(srcRel, ".asm")
	suffix := ".o"

	if instance.Platform.PIC {
		suffix = ".pic.o"
	}

	var outVFS VFS

	if strings.Contains(srcRel, "/") {
		outVFS = Build(instance.Path.rel() + "/_/" + stem + suffix)
	} else {
		outVFS = Build(instance.Path.rel() + "/" + stem + suffix)
	}

	inVFS := srcVFS
	outputPath := outVFS.string()
	inputPath := inVFS.string()

	var predefinedFlags []string

	if !asmlibYasmModules[instance.Path.rel()] {
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
				CmdArgs: ArgChunks{cmdArgs},
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           InputChunks{{yasmBinaryVFS}, in.IncludeInputs},
		Outputs:          []VFS{outVFS},
		KV:               KV{P: pkAS, PC: pcLightGreen},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
	}

	node.ForeignDepRefs = []NodeRef{yasmLD}
	node.DepRefs = []NodeRef{yasmLD}
	return emit.emit(node), outVFS
}
