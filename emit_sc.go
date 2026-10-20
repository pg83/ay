package main

var scKV = KV{P: pkSC, PC: pcYellow}

func (e *EmitContext) emitSCReserved(srcVFS, headerVFS, domschemecBinary VFS, domschemecLDRef NodeRef, id NodeRef) {
	na := e.ctx.na
	env := envVarsVCS

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.anyList(domschemecBinary.any(), argDashIn.any(), srcVFS.any(), argDashOut.any(), headerVFS.any())),
			Env:     env,
		}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(domschemecBinary), na.vfsList(srcVFS)),
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

	pe := func() {
		e.emitSCReserved(srcVFS, headerVFS, domBinary, domLDRef, scRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    scRef,
		GeneratorRefs:  e.ctx.na.refList(domLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: runtimeInclude},

		OnUse: pending,
	})
}
