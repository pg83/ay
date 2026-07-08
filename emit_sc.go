package main

var scKV = KV{P: pkSC, PC: pcYellow}

func emitSC(instance ModuleInstance, srcVFS, headerVFS, domschemecBinary VFS, runtimeClosure Closure, domschemecLDRef NodeRef, emit *StreamingEmitter) NodeRef {
	na := emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}}

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
		ForeignDepRefs: []NodeRef{domschemecLDRef},
	}

	return emit.emitNode(node)
}

func (e *EmitContext) emitLibrarySCSource(src ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	domRes := ctx.toolResult(argToolsDomschemec)
	domLDRef, domBinary := domRes.LDRef, *domRes.LDPath
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	headerVFS := build(srcVFS.relString(), ".h")
	runtimeClosure := walkClosure(e.scanner, domschemeRuntimeVFS, d.cc.ScanCfg)
	scRef := emitSC(instance, srcVFS, headerVFS, domBinary, runtimeClosure, domLDRef, ctx.emit)
	runtimeInclude := []IncludeDirective{{kind: includeQuoted, target: includeTarget(domschemeRuntimeVFS.rel())}}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    scRef,
		GeneratorRefs:  []NodeRef{domLDRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: runtimeInclude},
		ClosureLeaves:  []VFS{srcVFS, domschemeRuntimeVFS},
	})
}
