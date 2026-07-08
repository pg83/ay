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

func flatcDirectGeneratedHeaderIncludes(pm *IncludeParserManager, srcRel string) []IncludeDirective {
	direct := flatcDirectImportNames(pm, srcRel)

	if len(direct) == 0 {
		return nil
	}

	out := make([]IncludeDirective, 0, len(direct))

	for _, imp := range direct {
		out = append(out, IncludeDirective{
			kind:   includeQuoted,
			target: includeTarget(internV(imp, ".h")),
		})
	}

	return out
}

func emitFL(instance ModuleInstance, srcRel string, srcVFS VFS, flatcLDRef NodeRef, flatcBinary VFS, flatcFlags []ANY, transitiveImports Closure, moduleTag STR, tc ModuleToolchain, emit *StreamingEmitter, v *FlatcVariant, genDeps []NodeRef) (NodeRef, VFS, VFS, VFS) {
	na := emit.nodeArenas()
	headerVFS := build(srcRel, ".h")
	cppVFS := build(srcRel, ".cpp")
	bfbsVFS := build(strings.TrimSuffix(srcRel, v.srcExt), v.bfbsExt)
	cmdArgs := na.chunkList(na.anyList(tc.Python3.any(), (flatcWrapperVFS).any(), (flatcBinary).any()), v.constFlags)

	if len(flatcFlags) > 0 {
		cmdArgs = append(cmdArgs, na.anyConcat(flatcFlags))
	}

	cmdArgs = append(cmdArgs, v.ioLeadArgs, []ANY{headerVFS.any(), srcVFS.any()})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: bldRootDirVFS,
			Env: env}),
		Env:            env,
		DepRefs:        genDeps,
		ForeignDepRefs: depRefs(flatcLDRef),
		Inputs:         na.inputList(na.vfsList(flatcBinary, flatcWrapperVFS, srcVFS), transitiveImports.buckets...),
		KV:             v.kv,
		Outputs:        na.vfsList(headerVFS, cppVFS, bfbsVFS),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:      usesPython3,
	}

	return emit.emitNode(node), headerVFS, cppVFS, bfbsVFS
}

func (e *EmitContext) emitFlatcProducer(srcVFS VFS, v *FlatcVariant, genDeps []NodeRef) {
	ctx, instance, d := e.ctx, e.instance, e.d
	flatcRes := ctx.toolResult(v.toolArg)
	flatcLDRef, flatcBinary := flatcRes.LDRef, *flatcRes.LDPath
	transitiveImports := walkClosure(e.scanner, srcVFS, newScanContext(ctx.parsers, nil, nil, includeScannerBasePaths(), instance.Path.relString()))
	flRef, headerVFS, cppVFS, bfbsVFS := emitFL(instance, srcVFS.relString(), srcVFS, flatcLDRef, flatcBinary, d.flatcFlags, transitiveImports, d.unit.CCTag, d.tc, ctx.emit, v, genDeps)
	headerIncludes := flatcDirectGeneratedHeaderIncludes(ctx.parsers, srcVFS.relString())
	headerLeaves := []VFS{flatcWrapperVFS}

	if srcVFS.isSource() {
		headerLeaves = append(headerLeaves, srcVFS)
	}

	headerLeaves = append(headerLeaves, v.runtimeVFS)
	eachBucketVFS(transitiveImports.buckets, func(v VFS) { headerLeaves = append(headerLeaves, v) })

	reg := e.codegen

	reg.register(&GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    flRef,
		GeneratorRefs:  []NodeRef{flatcLDRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerIncludes},
		ClosureLeaves:  headerLeaves,
	})

	cppIncludes := []IncludeDirective{{kind: includeQuoted, target: includeTarget(headerVFS.rel())}}

	reg.register(&GeneratedFileInfo{
		OutputPath:     cppVFS,
		ProducerRef:    flRef,
		GeneratorRefs:  []NodeRef{flatcLDRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cppIncludes},
	})

	reg.register(&GeneratedFileInfo{
		OutputPath:    bfbsVFS,
		ProducerRef:   flRef,
		GeneratorRefs: []NodeRef{flatcLDRef},
	})
}

func (e *EmitContext) emitLibraryFlatcSource(meta SrcMeta, variant *FlatcVariant) {
	ctx, instance, d := e.ctx, e.instance, e.d

	var srcVFS VFS
	var genDeps []NodeRef

	if meta.Generated {
		srcVFS = meta.Source.vfs()
		genDeps = []NodeRef{e.codegen.mustInfo(srcVFS, "flatc generated source").ProducerRef}
	} else {
		srcVFS = resolveSourceVFS(ctx, instance, meta.Source.string(), d.srcDirs)
	}

	e.emitFlatcProducer(srcVFS, variant, genDeps)

	cpp := meta

	cpp.SecondLevel = meta.SecondLevel || meta.Generated
	cpp.Generated = true
	cpp.Source = build(srcVFS.relString(), ".cpp").any()
	e.enqueueSrc(cpp)
}
