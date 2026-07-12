package main

import (
	"strings"
)

var (
	flatcKVFL   = KV{P: pkFL, PC: pcLightGreen}
	flatcKVFL64 = KV{P: pkFL64, PC: pcLightGreen}
)

var flatcConstFlags = []ANY{
	argNoWarnings.any(),
	argCpp.any(),
	argKeepPrefix.any(),
	argGenMutable.any(),
	argSchema.any(),
	argB2.any(),
	argGenObjectApi.any(),
	argFilenameSuffix.any(),
	argFbs.any(),
}

var flatcIOLeadArgs = []ANY{
	argI.any(), argB.any(),
	argI.any(), argS.any(),
	argDashO.any(),
}

var flatc64ConstFlags = []ANY{
	argNoWarnings.any(),
	argCpp.any(),
	argKeepPrefix.any(),
	argGenMutable.any(),
	argSchema.any(),
	argB2.any(),
	argFilenameSuffix.any(),
	argFbs64.any(),
}

var flatc64IOLeadArgs = []ANY{
	argI.any(), argS.any(),
	argI.any(), argB.any(),
	argDashO.any(),
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
	constFlags []ANY
	ioLeadArgs []ANY
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

func flatcDirectGeneratedHeaderIncludes(na *NodeArenas, pm *IncludeParserManager, srcRel string) []IncludeDirective {
	direct := flatcDirectImportNames(pm, srcRel)

	if len(direct) == 0 {
		return nil
	}

	out := na.dirs.alloc(len(direct))[:0]

	for _, imp := range direct {
		out = append(out, IncludeDirective{
			kind:   includeQuoted,
			target: includeTarget(internV(imp, ".h").any()),
		})
	}

	na.dirs.commit(len(out))

	return out[:len(out):len(out)]
}

func (e *EmitContext) emitFLReserved(srcRel string, srcVFS VFS, flatcLDRef NodeRef, flatcBinary VFS, flatcFlags []ANY, transitiveImports Closure, moduleTag STR, tc ModuleToolchain, v *FlatcVariant, id NodeRef) {
	instance := e.instance
	na := e.ctx.na
	headerVFS := build(srcRel, ".h")
	cppVFS := build(srcRel, ".cpp")
	bfbsVFS := build(strings.TrimSuffix(srcRel, v.srcExt), v.bfbsExt)
	flatcFlagsChunk := []ANY(nil)

	if len(flatcFlags) > 0 {
		flatcFlagsChunk = na.anyConcat(flatcFlags)
	}

	headChunk := na.anyList(tc.Python3.any(), flatcWrapperVFS.any(), flatcBinary.any())
	tailChunk := na.anyList(headerVFS.any(), srcVFS.any())
	chunks := na.chunks.alloc(5)[:0]

	chunks = append(chunks, headChunk, v.constFlags)

	if flatcFlagsChunk != nil {
		chunks = append(chunks, flatcFlagsChunk)
	}

	chunks = append(chunks, v.ioLeadArgs, tailChunk)
	na.chunks.commit(len(chunks))

	cmdArgs := ArgChunks(chunks[:len(chunks):len(chunks)])
	env := envVarsVCS
	inputs := na.inputs.alloc(3 + len(transitiveImports.bucketList()))[:3+len(transitiveImports.bucketList())]

	inputs[0] = na.vfsList(flatcBinary)
	inputs[1] = na.vfsList(flatcWrapperVFS)
	inputs[2] = na.vfsList(srcVFS)
	copy(inputs[3:], transitiveImports.bucketList())
	na.inputs.commit(len(inputs))

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: bldRootDirVFS,
			Env: env}),
		Env:            env,
		ForeignDepRefs: na.refList(flatcLDRef),
		Inputs:         InputChunks(inputs[:len(inputs):len(inputs)]),
		KV:             v.kv,
		Outputs:        na.vfsList(headerVFS, cppVFS, bfbsVFS),
		Resources:      usesPython3,
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitFlatcProducer(srcVFS VFS, v *FlatcVariant) {
	ctx, d := e.ctx, e.d
	flatcRes := ctx.toolResult(v.toolArg)
	flatcLDRef, flatcBinary := flatcRes.LDRef, *flatcRes.LDPath
	transitiveImports := e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainFlatc)
	flRef := ctx.emit.reserve()
	srcRel := srcVFS.relString()
	headerVFS := build(srcRel, ".h")
	cppVFS := build(srcRel, ".cpp")
	bfbsVFS := build(strings.TrimSuffix(srcRel, v.srcExt), v.bfbsExt)
	flatcFlags := ctx.na.anyList(d.flatcFlags...)
	ccTag := d.unit.CCTag
	tc := d.tc

	pe := func() {
		e.emitFLReserved(srcRel, srcVFS, flatcLDRef, flatcBinary, flatcFlags, transitiveImports, ccTag, tc, v, flRef)
	}

	headerIncludes := flatcDirectGeneratedHeaderIncludes(ctx.na, ctx.parsers, srcVFS.relString())
	headerLeaves := ctx.na.vfs.alloc(3 + transitiveImports.len())[:0]

	headerLeaves = append(headerLeaves, flatcWrapperVFS)

	if srcVFS.isSource() {
		headerLeaves = append(headerLeaves, srcVFS)
	}

	headerLeaves = append(headerLeaves, v.runtimeVFS)
	eachBucketVFS(transitiveImports.bucketList(), func(v VFS) { headerLeaves = append(headerLeaves, v) })
	ctx.na.vfs.commit(len(headerLeaves))
	headerLeaves = headerLeaves[:len(headerLeaves):len(headerLeaves)]

	pending := e.ctx.na.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    flRef,
		GeneratorRefs:  e.ctx.na.refList(flatcLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerIncludes},
		ClosureLeaves:  headerLeaves,
		OnUse:          pending,
	})

	cppIncludes := ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(headerVFS.rel().any())})

	e.register(GeneratedFileInfo{
		OutputPath:     cppVFS,
		SourcePath:     srcVFS,
		ProducerRef:    flRef,
		GeneratorRefs:  e.ctx.na.refList(flatcLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cppIncludes},
		OnUse:          pending,
	})

	e.register(GeneratedFileInfo{
		OutputPath:    bfbsVFS,
		ProducerRef:   flRef,
		GeneratorRefs: e.ctx.na.refList(flatcLDRef),
		OnUse:         pending,
	})
}

func (e *EmitContext) emitLibraryFlatcSource(meta SrcMeta, variant *FlatcVariant) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcVFS := meta.Source.vfs()

	if srcVFS == 0 {
		srcVFS = resolveSourceVFS(ctx, instance, meta.Source.string(), d.srcDirs)
	}

	e.emitFlatcProducer(srcVFS, variant)

	cpp := meta

	cpp.Source = build(srcVFS.relString(), ".cpp").any()
	e.enqueueSrc(cpp)
}
