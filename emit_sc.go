package main

// emit_sc.go models the _SRC("sc") rule: domschemec turns a .sc schema into a
// <src>.sc.h header carrying the runtime.h output_include. No compile — the
// header is consumed via #include.

func emitSC(instance ModuleInstance, srcVFS, headerVFS, domschemecBinary VFS, runtimeClosure []VFS, domschemecLDRef NodeRef, emit Emitter) NodeRef {
	na := emit.nodeArenas()

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(na.strList(domschemecBinary.str(), argDashIn.str(), srcVFS.str(), argDashOut.str(), headerVFS.str())),
			Env:     env,
		}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(domschemecBinary, srcVFS), runtimeClosure),
		KV:               KV{P: pkSC, PC: pcYellow},
		Outputs:          na.vfsList(headerVFS),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		ForeignDepRefs:   []NodeRef{domschemecLDRef},
	}

	return emit.emit(node)
}

func emitLibrarySCSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	domRes := ctx.toolResult(argToolsDomschemec)
	domLDRef, domBinary := domRes.LDRef, *domRes.LDPath

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
	headerVFS := build(srcVFS.rel() + ".h")

	// output_include runtime.h: lead the inputs with runtime.h and its closure.
	runtimeClosure := walkClosure(ctx.scannerFor(instance), domschemeRuntimeVFS, in.ScanCfg)

	scRef := emitSC(instance, srcVFS, headerVFS, domBinary, runtimeClosure, domLDRef, ctx.emit)

	// Register the generated header so a consumer inherits the output_include.
	runtimeInclude := []IncludeDirective{{kind: includeQuoted, target: internStr(domschemeRuntimeVFS.rel())}}
	registerBoundGeneratedParsedOutput(ctx, instance, pkSC, headerVFS, runtimeInclude, scRef, []NodeRef{domLDRef})

	reg := codegenRegForInstance(ctx, instance)
	reg.addClosureLeaf(headerVFS, srcVFS)
	reg.addClosureLeaf(headerVFS, domschemeRuntimeVFS)

	// Header-only: no object file to archive.
	return nil
}
