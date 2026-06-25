package main

import (
	"strings"
)

var (
	yasmBinaryPath = yasmBinaryVFS.string()
	asKV           = KV{P: pkAS, PC: pcLightGreen}
)

var yasmConstHead = []STR{
	internStr(yasmBinaryPath),
	argF.str(), argElf64.str(),
	argD.str(), argUnix.str(),
	argReplaceBB.str(),
	argReplaceSS.str(),
	argReplaceToolRootT.str(),
}

func emitAS(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, hostP *Platform, emit *StreamingEmitter) (NodeRef, VFS) {
	na := emit.nodeArenas()

	outVFS, inVFS := composeASPaths(instance, srcRel, srcVFS, in)

	cmdArgs := composeASCmdArgs(instance, outVFS, inVFS, in)
	env := hostP.toolEnv()

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Cwd: strB,
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(in.IncludeInputs),
		Outputs:      na.vfsList(outVFS),
		KV:           &asKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    instance.Platform.UsesClangOnly,
	}

	if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = in.ExtraDepRefs
	}

	return emit.emit(node), outVFS
}

func composeASPaths(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs) (out, input VFS) {
	if srcVFS.isSource() && srcVFS.rel() != instance.Path.rel()+"/"+srcRel {
		outputRel := composeSrcDirOutputRel(instance.Path.rel(), srcVFS.rel())

		return build(instance.Path.rel() + "/" + outputRel + ".o"), srcVFS
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

	return build(outRel), srcVFS
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

func emitASYasm(instance ModuleInstance, srcRel string, srcVFS VFS, in ModuleCCInputs, yasmLD NodeRef, emit *StreamingEmitter) (NodeRef, VFS) {
	na := emit.nodeArenas()

	stem := strings.TrimSuffix(srcRel, ".asm")
	suffix := ".o"

	if instance.Platform.PIC {
		suffix = ".pic.o"
	}

	var outVFS VFS

	if strings.Contains(srcRel, "/") {
		outVFS = build(instance.Path.rel() + "/_/" + stem + suffix)
	} else {
		outVFS = build(instance.Path.rel() + "/" + stem + suffix)
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
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(na.vfsList(yasmBinaryVFS), in.IncludeInputs),
		Outputs:      na.vfsList(outVFS),
		KV:           &asKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
	}

	node.ForeignDepRefs = []NodeRef{yasmLD}

	if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = in.ExtraDepRefs
	}

	return emit.emit(node), outVFS
}

func emitLibraryAsmSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()

	asIn := in
	srcVFS := resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)

	scanIn := in

	if len(d.asmAddIncl) > 0 {
		scanIn.AddIncl = dedupVFS(in.AddIncl, d.asmAddIncl)
		scanIn.ScanCfg = newScanContext(ctx.parsers, scanIn.AddIncl, scanIn.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())
		asIn.AddIncl = scanIn.AddIncl
	}

	asIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), srcVFS, scanIn.ScanCfg)

	if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
		yasmLD, _ := ctx.tool(argContribToolsYasm)
		ref, outPath := emitASYasm(instance, srcRel, srcVFS, asIn, yasmLD, ctx.emit)

		return &SourceEmit{Ref: ref, OutPath: outPath}
	}

	ref, outPath := emitAS(instance, srcRel, srcVFS, asIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}
