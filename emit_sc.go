package main

var scKV = KV{P: pkSC, PC: pcYellow}

func (e *EmitContext) emitSCReserved(srcVFS, headerVFS, domschemecBinary VFS, runtimeClosure Closure, domschemecLDRef NodeRef, id NodeRef) {
	na := e.ctx.na
	env := envVarsVCS

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.anyList(domschemecBinary.any(), argDashIn.any(), srcVFS.any(), argDashOut.any(), headerVFS.any())),
			Env:     env,
		}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(domschemecBinary, srcVFS, runtimeClosure.self), runtimeClosure.buckets...),
		KV:             &scKV,
		Outputs:        na.vfsList(headerVFS),
		ForeignDepRefs: na.refList(domschemecLDRef),
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitLibrarySCSource(src ANY) {
	ctx, d := e.ctx, e.d
	domRes := ctx.toolResult(argToolsDomschemec)
	domLDRef, domBinary := domRes.LDRef, *domRes.LDPath
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	headerVFS := build(srcVFS.relString(), ".h")
	scRef := ctx.emit.reserve()
	runtimeInclude := ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(domschemeRuntimeVFS.rel().any())})

	scanner := e.scanner
	scanCtx := d.scanCtx

	pe := func() {
		runtimeClosure := scanner.walkClosure(domschemeRuntimeVFS, scanCtx, scanDomainCC)

		e.emitSCReserved(srcVFS, headerVFS, domBinary, runtimeClosure, domLDRef, scRef)
	}
	pending := e.ctx.na.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    scRef,
		GeneratorRefs:  e.ctx.na.refList(domLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: runtimeInclude},
		ClosureLeaves:  e.ctx.na.vfsList(srcVFS, domschemeRuntimeVFS),
		OnUse:          pending,
	})
}
