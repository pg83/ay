package main

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

func emitLibrarySCSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	domRes := ctx.toolResult(argToolsDomschemec)
	domLDRef, domBinary := domRes.LDRef, *domRes.LDPath

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)
	headerVFS := build(srcVFS.rel() + ".h")

	runtimeClosure := walkClosure(ctx.scannerFor(instance), domschemeRuntimeVFS, in.ScanCfg)

	scRef := emitSC(instance, srcVFS, headerVFS, domBinary, runtimeClosure, domLDRef, ctx.emit)

	runtimeInclude := []IncludeDirective{{kind: includeQuoted, target: internStr(domschemeRuntimeVFS.rel())}}
	registerBoundGeneratedParsedOutput(ctx, instance, pkSC, headerVFS, runtimeInclude, scRef, []NodeRef{domLDRef})

	reg := codegenRegForInstance(ctx, instance)
	reg.addClosureLeaf(headerVFS, srcVFS)
	reg.addClosureLeaf(headerVFS, domschemeRuntimeVFS)

	return nil
}

var (
	scKV = KV{P: pkSC, PC: pcYellow}
)
