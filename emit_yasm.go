package main

import (
	"strings"
)

var yasmBinaryPath = yasmBinaryVFS.string()

var yasmConstHead = []ANY{
	internStr(yasmBinaryPath).any(),
	argF.any(), argElf64.any(),
	argD.any(), argUnix.any(),
	argReplaceBB.any(),
	argReplaceSS.any(),
	argReplaceToolRootT.any(),
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
		outVFS = build(instance.Path.relString(), "/_/", stem, suffix)
	} else {
		outVFS = build(instance.Path.relString(), "/", stem, suffix)
	}

	inVFS := srcVFS

	var predefinedFlags []string

	if !asmlibYasmModules[instance.Path.relString()] {
		predefinedFlags = []string{"-g", "dwarf2"}
	}

	cmdArgs := make([]ANY, 0, 20+len(predefinedFlags))

	cmdArgs = append(cmdArgs, yasmConstHead...)

	cmdArgs = append(cmdArgs,
		argD.any(), internV("_", string(instance.Platform.ISA), "_").any(),
		argDYasm.any(),
	)

	cmdArgs = appendInternAnys(cmdArgs, predefinedFlags)

	cmdArgs = append(cmdArgs,
		argI.any(), argB.any(),
		argI.any(), argS.any(),
	)

	for _, p := range in.AddIncl {
		cmdArgs = append(cmdArgs, argI.any(), (p).any())
	}

	cmdArgs = append(cmdArgs,
		argDashO.any(), outVFS.any(),
		inVFS.any(),
	)

	env := envVarsVCSYasm

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:          env,
		Inputs:       na.inputList(na.vfsList(yasmBinaryVFS, in.IncludeView.self), in.IncludeView.buckets...),
		Outputs:      na.vfsList(outVFS),
		KV:           &asKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
	}

	node.ForeignDepRefs = []NodeRef{yasmLD}

	if len(in.ExtraDepRefs) > 0 {
		node.DepRefs = in.ExtraDepRefs
	}

	return emit.emitNode(node), outVFS
}

func (e *EmitContext) emitLibraryYasmSource(meta SrcMeta) {
	src := meta.Source
	ctx, instance, d := e.ctx, e.instance, e.d
	srcVFS := src.vfs()
	srcRel := src.string()

	if srcVFS == 0 {
		srcVFS = e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	} else {
		srcRel = trimModulePrefix(srcVFS.relString(), instance.Path.relString())
	}

	in := e.ccInputsFor(srcVFS)
	asIn := in
	scanIn := in

	if len(d.asmAddIncl) > 0 {
		scanIn.AddIncl = dedup(in.AddIncl, d.asmAddIncl)
		scanIn.ScanCfg = newScanContext(ctx.parsers, scanIn.AddIncl, scanIn.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.relString())

		asIn.AddIncl = scanIn.AddIncl
	}

	asIn.IncludeView = walkClosure(e.scanner, srcVFS, scanIn.ScanCfg)
	asIn.ExtraDepRefs = resolveCodegenDepRefsInclView(ctx, instance, ctx.na, asIn.IncludeView)

	yasmLD, _ := ctx.tool(argContribToolsYasm)
	ref, outPath := emitASYasm(instance, srcRel, srcVFS, asIn, yasmLD, ctx.emit)

	e.collectObj(ref, outPath, meta)
}
