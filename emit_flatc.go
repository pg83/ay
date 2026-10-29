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

func flatcDirectGeneratedHeaderIncludes(na *NodeArenas, scanner *IncludeScanner, src VFS, ext string) []IncludeDirective {
	direct := scanner.parsedBucketForInput(src, parsedIncludesLocal, nil)

	if len(direct) == 0 {
		return nil
	}

	out := na.dirs.alloc(len(direct))[:0]

	for _, imp := range direct {
		out = append(out, IncludeDirective{
			kind:   includeQuoted,
			target: includeTarget(internStr(strings.ReplaceAll(imp.target.string(), ext, ext+".h")).any()),
		})
	}

	na.dirs.commit(len(out))

	return out[:len(out):len(out)]
}

func flatcIncludeArgs(na *NodeArenas, paths []VFS) []ANY {
	if len(paths) == 0 {
		return nil
	}

	args := na.anys.alloc(2 * len(paths))[: 2*len(paths) : 2*len(paths)]

	for i, path := range paths {
		args[2*i] = argI.any()
		args[2*i+1] = path.any()
	}

	na.anys.commit(len(args))

	return args[:len(args):len(args)]
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
	includeArgs := e.peers.FlatcIncludeArgs

	if v != &flatcVariantFL {
		includeArgs = nil
	}

	chunkCap := 5

	if len(includeArgs) != 0 {
		chunkCap += 2
	}

	chunks := na.chunks.alloc(chunkCap)[:0]

	chunks = append(chunks, headChunk, v.constFlags)

	if flatcFlagsChunk != nil {
		chunks = append(chunks, flatcFlagsChunk)
	}

	if len(includeArgs) == 0 {
		chunks = append(chunks, v.ioLeadArgs)
	} else {
		chunks = append(chunks, v.ioLeadArgs[:len(v.ioLeadArgs)-1], includeArgs, v.ioLeadArgs[len(v.ioLeadArgs)-1:])
	}

	chunks = append(chunks, tailChunk)
	na.chunks.commit(len(chunks))

	cmdArgs := ArgChunks(chunks[:len(chunks):len(chunks)])
	env := envVarsVCS
	transitiveBuckets := transitiveImports.bucketList()
	inputs := na.inputs.alloc(3 + len(transitiveBuckets))[:3+len(transitiveBuckets)]

	inputs[0] = na.vfsList(flatcBinary)
	inputs[1] = na.vfsList(flatcWrapperVFS)
	inputs[2] = na.vfsList(srcVFS)
	copy(inputs[3:], transitiveBuckets)
	na.inputs.commit(len(inputs))

	node := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: bldRootDirVFS,
			Env: env}),
		Env:            env,
		ForeignDepRefs: na.refList(flatcLDRef), Inputs: InputChunks(inputs[:len(inputs):len(inputs)]), KV: v.kv,
		Outputs:   na.vfsList(headerVFS, cppVFS, bfbsVFS),
		Resources: usesPython3}, input: InputSpec{roots: InputChunks(inputs[1:len(inputs):len(inputs)])},
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitFlatcProducer(srcVFS VFS, v *FlatcVariant) {
	ctx, d := e.ctx, e.d
	flatcRes := ctx.toolResult(v.toolArg)
	flatcLDRef, flatcBinary := flatcRes.LDRef, *flatcRes.LDPath
	transitiveImports := e.scanner.walkClosure(srcVFS, d.scanCtx, scanDomainFlatc)
	srcRel := srcVFS.relString()
	headerVFS := build(srcRel, ".h")
	cppVFS := build(srcRel, ".cpp")
	bfbsVFS := build(strings.TrimSuffix(srcRel, v.srcExt), v.bfbsExt)
	flatcFlags := ctx.na.anyList(d.flatcFlags...)

	var recipe *FbsFlatcRecipe

	if v == &flatcVariantFL64 && d.multimodule == tokFbsLibrary {
		if info := e.codegen.lookup(headerVFS); info != nil {
			if recipe := info.fbsFlatc(); recipe != nil {
				recipe.flags = flatcFlags
			}

			return
		}

		recipe = &FbsFlatcRecipe{flags: flatcFlags}
	}

	flRef := ctx.emit.reserve()
	ccTag := d.unit.CCTag
	tc := d.tc

	pe := func() {
		if recipe != nil {
			flatcFlags = recipe.flags
		}

		e.emitFLReserved(srcRel, srcVFS, flatcLDRef, flatcBinary, flatcFlags, transitiveImports, ccTag, tc, v, flRef)
	}

	headerIncludes := flatcDirectGeneratedHeaderIncludes(ctx.na, e.scanner, srcVFS, v.srcExt)

	var pending *PendingEmit

	if recipe != nil {
		recipe.emit = e.pendingEmit(pe)
		pending = ctx.javaPendingEmit(func() { recipe.used = true })
	} else {
		pending = e.pendingEmit(pe)
	}

	e.register(GeneratedFileSpec{
		OutputPath:     headerVFS,
		ProducerRef:    flRef,
		FbsFlatc:       recipe,
		GeneratorRefs:  e.ctx.na.refList(flatcLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerIncludes},
		ClosureLeaves:  ctx.na.vfsList(v.runtimeVFS),
		OnUse:          pending,
	})

	var inherited ParsedIncludeSet

	if srcVFS.isBuild() {
		inherited = e.codegen.buildParsedFor(srcVFS)
		e.codegen.inheritParsedIncludes(headerVFS, flRef, inherited)
	}

	cppIncludes := ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(headerVFS.rel().any())})

	e.register(GeneratedFileSpec{
		OutputPath:     cppVFS,
		SourcePath:     srcVFS,
		ProducerRef:    flRef,
		GeneratorRefs:  e.ctx.na.refList(flatcLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cppIncludes},
		OnUse:          pending,
	})

	if srcVFS.isBuild() {
		e.codegen.inheritParsedIncludes(cppVFS, flRef, inherited)
	}

	e.register(GeneratedFileSpec{
		OutputPath:      bfbsVFS,
		ProducerRef:     flRef,
		ProducerMainOut: headerVFS,
		GeneratorRefs:   e.ctx.na.refList(flatcLDRef),
		OnUse:           pending,
	})
}

func (e *EmitContext) emitLibraryFlatcSource(meta SrcMeta, variant *FlatcVariant) {
	srcVFS := e.sourceForEmission(meta)

	e.emitFlatcProducer(srcVFS, variant)

	cpp := meta

	cpp.Source = build(srcVFS.relString(), ".cpp").any()
	e.enqueueSrc(cpp)
}
