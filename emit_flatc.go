package main

import (
	"strings"
)

var (
	flatcKVFL   = KV{P: pkFL, PC: pcLightGreen}
	flatcKVFL64 = KV{P: pkFL64, PC: pcLightGreen}
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

var flatcVariantFL = FlatcVariant{
	toolArg:    argContribLibsFlatbuffersFlatc,
	constFlags: flatcConstFlags,
	ioLeadArgs: flatcIOLeadArgs,
	kv:         &flatcKVFL,
	srcExt:     ".fbs",
	bfbsExt:    ".bfbs",
	runtimeVFS: flatcRuntimeVFS,
}

var flatcVariantFL64 = FlatcVariant{
	toolArg:    argContribLibsFlatbuffers64Flatc,
	constFlags: flatc64ConstFlags,
	ioLeadArgs: flatc64IOLeadArgs,
	kv:         &flatcKVFL64,
	srcExt:     ".fbs64",
	bfbsExt:    ".bfbs64",
	runtimeVFS: flatc64RuntimeVFS,
}

type FlatcVariant struct {
	toolArg    ARG
	constFlags []STR
	ioLeadArgs []STR
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
			target: internV(imp, ".h"),
		})
	}

	return out
}

func emitFL(instance ModuleInstance, srcRel string, srcVFS VFS, flatcLDRef NodeRef, flatcBinary VFS, flatcFlags []ARG, transitiveImports []VFS, moduleTag STR, tc ModuleToolchain, emit *StreamingEmitter, v *FlatcVariant, genDeps []NodeRef) (NodeRef, VFS, VFS, VFS) {
	na := emit.nodeArenas()
	headerVFS := build(srcRel, ".h")
	cppVFS := build(srcRel, ".cpp")
	bfbsVFS := build(strings.TrimSuffix(srcRel, v.srcExt), v.bfbsExt)
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

func (e *EmitContext) emitFlatcProducer(srcVFS VFS, v *FlatcVariant, genDeps []NodeRef) {
	ctx, instance, d := e.ctx, e.instance, e.d
	flatcRes := ctx.toolResult(v.toolArg)
	flatcLDRef, flatcBinary := flatcRes.LDRef, *flatcRes.LDPath
	transitiveImports := walkClosureTail(e.scanner, srcVFS, newScanContext(ctx.parsers, nil, nil, includeScannerBasePaths(), instance.Path.rel()))
	flRef, headerVFS, cppVFS, bfbsVFS := emitFL(instance, srcVFS.rel(), srcVFS, flatcLDRef, flatcBinary, d.flatcFlags, transitiveImports, d.unit.CCTag, d.tc, ctx.emit, v, genDeps)
	headerIncludes := flatcDirectGeneratedHeaderIncludes(ctx.parsers, srcVFS.rel())
	headerLeaves := []VFS{flatcWrapperVFS}

	if srcVFS.isSource() {
		headerLeaves = append(headerLeaves, srcVFS)
	}

	headerLeaves = append(headerLeaves, v.runtimeVFS)
	headerLeaves = append(headerLeaves, transitiveImports...)

	reg := e.codegen

	reg.register(&GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    flRef,
		GeneratorRefs:  []NodeRef{flatcLDRef},
		ParsedIncludes: headerIncludes,
		ClosureLeaves:  headerLeaves,
	})

	cppIncludes := []IncludeDirective{{kind: includeQuoted, target: internStr(headerVFS.rel())}}

	reg.register(&GeneratedFileInfo{
		OutputPath:     cppVFS,
		ProducerRef:    flRef,
		GeneratorRefs:  []NodeRef{flatcLDRef},
		ParsedIncludes: cppIncludes,
	})

	reg.register(&GeneratedFileInfo{
		OutputPath:     bfbsVFS,
		ProducerRef:    flRef,
		GeneratorRefs:  []NodeRef{flatcLDRef},
		ParsedIncludes: nil,
	})
}

func (e *EmitContext) emitLibraryFlatcSource32(src STR) {
	e.emitLibraryFlatcSource(src, &flatcVariantFL)
}

func (e *EmitContext) emitLibraryFlatcSource64(src STR) {
	e.emitLibraryFlatcSource(src, &flatcVariantFL64)
}

func (e *EmitContext) emitLibraryFlatcSource(src STR, variant *FlatcVariant) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcVFS := resolveSourceVFS(ctx, instance, src.string(), d.srcDirs)

	e.emitFlatcProducer(srcVFS, variant, nil)

	meta := d.srcMetaOf(src)

	meta.Generated = true
	e.enqueueSrc(build(srcVFS.rel(), ".cpp").str(), meta)
}
