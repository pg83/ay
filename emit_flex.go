package main

import "strings"

var (
	flexOutputInclude = IncludeDirective{kind: includeQuoted, target: includeTarget(internStr("util/system/compiler.h").any())}
	flexKV            = KV{P: pkLX, PC: pcYellow}
)

const flexDefaultGenExt = ".cpp"

func flexGeneratedVFS(instance ModuleInstance, srcRel string) VFS {
	if strings.Contains(srcRel, "/") {
		return build(instance.Path.relString(), "/_/", srcRel, flexDefaultGenExt)
	}

	return build(instance.Path.relString(), "/", srcRel, flexDefaultGenExt)
}

func (e *EmitContext) emitLibraryFlexSource(src ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()
	flexRef, flexBin := ctx.tool(argContribToolsFlexOld)
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	outVFS := flexGeneratedVFS(instance, srcRel)
	na := ctx.na
	localBucket := e.scanner.parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)
	parsed := na.dirs.alloc(2 + len(localBucket))[:0]

	parsed = append(parsed, flexOutputInclude)
	parsed = append(parsed, localBucket...)

	var psc []ANY

	if p := d.perSrcCFlagsFor(src); p != nil {
		psc = *p
	}

	cflags := psc

	if extIsFlexL(srcRel) {
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(srcVFS.rel().any())})

		cf := na.anys.alloc(len(psc) + 1)
		cn := copy(cf, psc)

		cf[cn] = argWnoUnusedVariable.any()
		na.anys.commit(cn + 1)
		cflags = cf[: cn+1 : cn+1]
	}

	na.dirs.commit(len(parsed))
	parsed = parsed[:len(parsed):len(parsed)]

	lxRef := ctx.emit.reserve()

	info := e.codegen.register(GeneratedFileInfo{
		OutputPath:     outVFS,
		ProducerRef:    lxRef,
		GeneratorRefs:  e.ctx.na.refList(flexRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		Compile:        e.ctx.na.compileSpec(CompileSpec{FlatOutput: d.flatSrc(src), CFlags: cflags}),
	})

	meta := d.srcMetaOf(src)

	meta.Generated = true
	meta.Source = outVFS.any()
	e.enqueueSrc(meta)

	scanner := e.scanner
	scanCfg := snapshotScanCfg(ctx.na, d.cc.ScanCfg)

	pe := &PendingEmit{fn: func() {
		lxClosure := walkClosure(scanner, outVFS, scanCfg).collect(ctx.na, func(v VFS) bool { return v.isSource() })

		emitFlexLX(instance, flexRef, flexBin, srcVFS, outVFS, lxClosure, lxRef, ctx.emit)
	}}

	info.pending = pe

}

func emitFlexLX(instance ModuleInstance, flexRef NodeRef, flexBin VFS, srcVFS, outVFS VFS, closure []VFS, id NodeRef, emit *StreamingEmitter) {
	na := emit.nodeArenas()

	cmdArgs := na.chunkList(na.anyList(
		flexBin.any(),
		internV(argDashO.string(), outVFS.prefix(), outVFS.relString()).any(),
		srcVFS.any(),
	))

	env := envVarsVCS

	emit.emitReservedNode(Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(flexBin, srcVFS), closure),
		Outputs:        na.vfsList(outVFS),
		KV:             &flexKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: na.refList(flexRef),
	}, id)
}
