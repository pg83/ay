package main

import (
	"strings"
)

var yasmBinaryPath = yasmBinaryVFS.string()

var yasmConstHead = []STR{
	internStr(yasmBinaryPath),
	argF.str(), argElf64.str(),
	argD.str(), argUnix.str(),
	argReplaceBB.str(),
	argReplaceSS.str(),
	argReplaceToolRootT.str(),
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
		outVFS = build(instance.Path.rel(), "/_/", stem, suffix)
	} else {
		outVFS = build(instance.Path.rel(), "/", stem, suffix)
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
		argD.str(), internV("_", string(instance.Platform.ISA), "_"),
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

func (e *EmitContext) emitLibraryYasmSource(src STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcVFS := src.vfs()
	srcRel := src.string()

	if srcVFS == 0 {
		srcVFS = e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	} else {
		srcRel = strings.TrimPrefix(srcVFS.rel(), instance.Path.rel()+"/")
	}

	in := e.ccInputsFor(srcVFS)
	asIn := in
	scanIn := in

	if len(d.asmAddIncl) > 0 {
		scanIn.AddIncl = dedup(in.AddIncl, d.asmAddIncl)
		scanIn.ScanCfg = newScanContext(ctx.parsers, scanIn.AddIncl, scanIn.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

		asIn.AddIncl = scanIn.AddIncl
	}

	asIn.IncludeInputs = walkClosure(e.scanner, srcVFS, scanIn.ScanCfg)
	asIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, asIn.IncludeInputs)

	yasmLD, _ := ctx.tool(argContribToolsYasm)
	ref, outPath := emitASYasm(instance, srcRel, srcVFS, asIn, yasmLD, ctx.emit)

	e.collectObj(ref, outPath, e.metaForSrc(src))
}
