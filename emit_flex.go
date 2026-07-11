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

func (e *EmitContext) emitLibraryFlexSource(meta SrcMeta) {
	ctx, instance, d := e.ctx, e.instance, e.d
	src := meta.Source
	srcRel := e.moduleSourceRel(src)
	flexRef, flexBin := ctx.tool(argContribToolsFlexOld)
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	outVFS := flexGeneratedVFS(instance, srcRel)
	na := ctx.na
	localBucket := e.scanner.parsedBucketForInput(srcVFS, parsedIncludesLocal, nil)
	parsed := na.dirs.alloc(2 + len(localBucket))[:0]

	parsed = append(parsed, flexOutputInclude)
	parsed = append(parsed, localBucket...)

	var cflags []ANY

	if extIsFlexL(srcRel) {
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(srcVFS.rel().any())})

		cf := na.anys.alloc(1)

		cf[0] = argWnoUnusedVariable.any()
		na.anys.commit(1)
		cflags = cf[:1:1]
	}

	na.dirs.commit(len(parsed))
	parsed = parsed[:len(parsed):len(parsed)]

	lxRef := ctx.emit.reserve()

	meta.Source = outVFS.any()
	meta.Compile.CFlags = concat(meta.Compile.CFlags, cflags)
	e.enqueueSrc(meta)

	scanner := e.scanner
	scanCtx := d.scanCtx

	pe := func() {
		lxClosure := scanner.walkClosure(outVFS, scanCtx, scanDomainCC).collect(ctx.na, func(v VFS) bool { return v.isSource() })

		e.emitFlexLX(flexRef, flexBin, srcVFS, outVFS, lxClosure, lxRef)
	}
	pending := e.ctx.na.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     outVFS,
		ProducerRef:    lxRef,
		GeneratorRefs:  e.ctx.na.refList(flexRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		OnUse:          pending,
	})
}

func (e *EmitContext) emitFlexLX(flexRef NodeRef, flexBin VFS, srcVFS, outVFS VFS, closure []VFS, id NodeRef) {
	na := e.ctx.na

	cmdArgs := na.chunkList(na.anyList(
		flexBin.any(),
		internV(argDashO.string(), outVFS.prefix(), outVFS.relString()).any(),
		srcVFS.any(),
	))

	env := envVarsVCS

	e.emitReservedNode(Node{
		Platform:       e.instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(flexBin, srcVFS), closure),
		Outputs:        na.vfsList(outVFS),
		KV:             &flexKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: na.refList(flexRef),
	}, id)
}
