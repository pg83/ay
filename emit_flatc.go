package main

import (
	"strings"
)

var flatcConstFlags = []STR{
	argNoWarnings.str(),
	argCpp.str(),
	argKeepPrefix.str(),
	argGenMutable.str(),
	argSchema.str(),
	argB2.str(),
	argGenObjectApi.str(),
	argFilenameSuffix.str(),
	argFbs.str(),
}

var flatcIOLeadArgs = []STR{
	argI.str(), argB.str(),
	argI.str(), argS.str(),
	argDashO.str(),
}

var flatc64ConstFlags = []STR{
	argNoWarnings.str(),
	argCpp.str(),
	argKeepPrefix.str(),
	argGenMutable.str(),
	argSchema.str(),
	argB2.str(),
	argFilenameSuffix.str(),
	argFbs64.str(),
}

var flatc64IOLeadArgs = []STR{
	argI.str(), argS.str(),
	argI.str(), argB.str(),
	argDashO.str(),
}

var flatcKVFL = KV{P: pkFL, PC: pcLightGreen}

var flatcKVFL64 = KV{P: pkFL64, PC: pcLightGreen}

var flatcVariantFL = flatcVariant{
	toolArg:    argContribLibsFlatbuffersFlatc,
	constFlags: flatcConstFlags,
	ioLeadArgs: flatcIOLeadArgs,
	procKind:   pkFL,
	kv:         &flatcKVFL,
	srcExt:     ".fbs",
	bfbsExt:    ".bfbs",
	runtimeVFS: flatcRuntimeVFS,
}

var flatcVariantFL64 = flatcVariant{
	toolArg:    argContribLibsFlatbuffers64Flatc,
	constFlags: flatc64ConstFlags,
	ioLeadArgs: flatc64IOLeadArgs,
	procKind:   pkFL64,
	kv:         &flatcKVFL64,
	srcExt:     ".fbs64",
	bfbsExt:    ".bfbs64",
	runtimeVFS: flatc64RuntimeVFS,
}

type flatcVariant struct {
	toolArg    ARG
	constFlags []STR
	ioLeadArgs []STR
	procKind   ProcKind
	kv         *KV
	srcExt     string
	bfbsExt    string
	runtimeVFS VFS
}

func flatcDirectImportNames(pm *IncludeParserManager, srcRel string) []string {
	direct := pm.sourceParsedBuckets(source(srcRel), nil).bucket(parsedIncludesLocal)

	if len(direct) == 0 {
		return nil
	}

	out := make([]string, 0, len(direct))

	for _, d := range direct {
		out = append(out, d.target.string())
	}

	return out
}

func flatcDirectGeneratedHeaderIncludes(pm *IncludeParserManager, srcRel string) []IncludeDirective {
	direct := flatcDirectImportNames(pm, srcRel)

	if len(direct) == 0 {
		return nil
	}

	out := make([]IncludeDirective, 0, len(direct))

	for _, imp := range direct {
		out = append(out, IncludeDirective{
			kind:   includeQuoted,
			target: internStr(imp + ".h"),
		})
	}

	return out
}

func emitFL(instance ModuleInstance, srcRel string, srcVFS VFS, flatcLDRef NodeRef, flatcBinary VFS, flatcFlags []ARG, transitiveImports []VFS, moduleTag STR, tc ModuleToolchain, emit *StreamingEmitter, v *flatcVariant, genDeps []NodeRef) (NodeRef, VFS, VFS, VFS) {
	na := emit.nodeArenas()

	headerVFS := build(srcRel + ".h")
	cppVFS := build(srcRel + ".cpp")
	bfbsVFS := build(strings.TrimSuffix(srcRel, v.srcExt) + v.bfbsExt)

	cmdArgs := na.chunkList(na.strList(tc.Python3, (flatcWrapperVFS).str(), (flatcBinary).str()), v.constFlags)

	if len(flatcFlags) > 0 {
		cmdArgs = append(cmdArgs, appendArgStr(nil, flatcFlags))
	}

	cmdArgs = append(cmdArgs, v.ioLeadArgs, []STR{(headerVFS).str(), (srcVFS).str()})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: strB,
			Env: env}),
		Env:            env,
		DepRefs:        genDeps,
		ForeignDepRefs: depRefs(flatcLDRef),
		Inputs:         na.inputList(na.vfsList(flatcBinary, flatcWrapperVFS, srcVFS), transitiveImports),
		KV:             v.kv,
		Outputs:        na.vfsList(headerVFS, cppVFS, bfbsVFS),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:      usesPython3,
	}

	return emit.emit(node), headerVFS, cppVFS, bfbsVFS
}

func emitFlatcProducer(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcVFS VFS, v *flatcVariant, genDeps []NodeRef) {
	flatcRes := ctx.toolResult(v.toolArg)
	flatcLDRef, flatcBinary := flatcRes.LDRef, *flatcRes.LDPath
	transitiveImports := walkClosureTail(ctx.scannerFor(instance), srcVFS, newScanContext(ctx.parsers, nil, nil, includeScannerBasePaths(), instance.Path.rel()))
	flRef, headerVFS, cppVFS, bfbsVFS := emitFL(instance, srcVFS.rel(), srcVFS, flatcLDRef, flatcBinary, d.flatcFlags, transitiveImports, moduleCCTag(d.moduleStmt.Name), d.tc, ctx.emit, v, genDeps)

	headerIncludes := flatcDirectGeneratedHeaderIncludes(ctx.parsers, srcVFS.rel())

	registerBoundGeneratedParsedOutput(ctx, instance, v.procKind, headerVFS, headerIncludes, flRef, []NodeRef{flatcLDRef})

	reg := codegenRegForInstance(ctx, instance)
	reg.addClosureLeaf(headerVFS, flatcWrapperVFS)

	if srcVFS.isSource() {
		reg.addClosureLeaf(headerVFS, srcVFS)
	}

	reg.addClosureLeaf(headerVFS, v.runtimeVFS)

	for _, imp := range transitiveImports {
		reg.addClosureLeaf(headerVFS, imp)
	}

	cppIncludes := []IncludeDirective{{kind: includeQuoted, target: internStr(headerVFS.rel())}}

	registerBoundGeneratedParsedOutput(ctx, instance, v.procKind, cppVFS, cppIncludes, flRef, []NodeRef{flatcLDRef})
	registerBoundGeneratedParsedOutput(ctx, instance, v.procKind, bfbsVFS, nil, flRef, []NodeRef{flatcLDRef})
}

func emitLibraryFlatcSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()

	cppVFS := build(resolveSourceVFS(ctx, instance, srcRel, d.srcDirs).rel() + ".cpp")

	return emitFlatcCppCompile(ctx, instance, cppVFS, in)
}

func emitFlatcCppCompile(ctx *GenCtx, instance ModuleInstance, cppVFS VFS, in ModuleCCInputs) *SourceEmit {
	flRef := codegenRegForInstance(ctx, instance).lookup(cppVFS).ProducerRef

	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), cppVFS, in.ScanCfg)

	ccIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, ccIn.IncludeInputs, flRef)
	ccRef, ccOut, _ := emitCC(instance, cppVFS.str(), cppVFS, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}
