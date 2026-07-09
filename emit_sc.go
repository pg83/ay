package main

var scKV = KV{P: pkSC, PC: pcYellow}

func emitSCReserved(instance ModuleInstance, srcVFS, headerVFS, domschemecBinary VFS, runtimeClosure Closure, domschemecLDRef NodeRef, id NodeRef, emit *StreamingEmitter) {
	na := emit.nodeArenas()
	env := envVarsVCS

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.anyList(domschemecBinary.any(), argDashIn.any(), srcVFS.any(), argDashOut.any(), headerVFS.any())),
			Env:     env,
		}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(domschemecBinary, srcVFS, runtimeClosure.self), runtimeClosure.buckets...),
		KV:             &scKV,
		Outputs:        na.vfsList(headerVFS),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: na.refList(domschemecLDRef),
	}

	emit.emitReservedNode(node, id)
}

func (e *EmitContext) emitLibrarySCSource(src ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	domRes := ctx.toolResult(argToolsDomschemec)
	domLDRef, domBinary := domRes.LDRef, *domRes.LDPath
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	headerVFS := build(srcVFS.relString(), ".h")
	scRef := ctx.emit.reserve()
	runtimeInclude := ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(domschemeRuntimeVFS.rel().any())})

	scanner := e.scanner
	scanCfg := snapshotScanCfg(ctx.na, d.cc.ScanCfg)

	pe := func() {
		runtimeClosure := walkClosure(scanner, domschemeRuntimeVFS, scanCfg)

		emitSCReserved(instance, srcVFS, headerVFS, domBinary, runtimeClosure, domLDRef, scRef, ctx.emit)
	}

	e.codegen.register(GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    scRef,
		GeneratorRefs:  e.ctx.na.refList(domLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: runtimeInclude},
		ClosureLeaves:  e.ctx.na.vfsList(srcVFS, domschemeRuntimeVFS),
		OnUse:          &pe,
	})
}
