package main

var scKV = KV{P: pkSC, PC: pcYellow}

func emitSC(instance ModuleInstance, srcVFS, headerVFS, domschemecBinary VFS, runtimeClosure []VFS, domschemecLDRef NodeRef, emit *StreamingEmitter) NodeRef {
	na := emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.strList(domschemecBinary.str(), argDashIn.str(), srcVFS.str(), argDashOut.str(), headerVFS.str())),
			Env:     env,
		}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(domschemecBinary, srcVFS), runtimeClosure),
		KV:             &scKV,
		Outputs:        na.vfsList(headerVFS),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: []NodeRef{domschemecLDRef},
	}

	return emit.emit(node)
}

func (e *EmitContext) emitLibrarySCSource(src STR) *SourceEmit {
	ctx, instance, d := e.ctx, e.instance, e.d
	domRes := ctx.toolResult(argToolsDomschemec)
	domLDRef, domBinary := domRes.LDRef, *domRes.LDPath
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	headerVFS := build(srcVFS.rel(), ".h")
	runtimeClosure := walkClosure(e.scanner, domschemeRuntimeVFS, d.cc.ScanCfg)
	scRef := emitSC(instance, srcVFS, headerVFS, domBinary, runtimeClosure, domLDRef, ctx.emit)
	runtimeInclude := []IncludeDirective{{kind: includeQuoted, target: internStr(domschemeRuntimeVFS.rel())}}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    scRef,
		GeneratorRefs:  []NodeRef{domLDRef},
		ParsedIncludes: runtimeInclude,
		ClosureLeaves:  []VFS{srcVFS, domschemeRuntimeVFS},
	})

	return nil
}
