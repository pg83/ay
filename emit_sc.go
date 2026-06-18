package main

// emit_sc.go models the upstream _SRC("sc") rule (build/ymake.core.conf):
//
//	.CMD=${tool:"tools/domschemec"} --in ${input:SRC} --out ${norel;output;suf=.h:SRC}
//	     ${hide;output_include:"library/cpp/domscheme/runtime.h"}
//	     ${hide;kv:"p SC"} ${hide;kv:"pc yellow"}
//	.PEERDIR=library/cpp/domscheme
//
// A SRCS(*.sc) entry yields a single SC producer node: domschemec turns the .sc
// schema into a <src>.sc.h header. The header carries the runtime.h
// output_include, so the producer's inputs are the tool, the .sc source, and
// runtime.h together with its scanned include closure. There is no compile —
// the generated header is consumed via #include (like .h.in).

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

	// output_include library/cpp/domscheme/runtime.h: the producer's inputs lead
	// with runtime.h and its full include closure (the sg7 reference carries 976
	// entries: runtime.h + libcxx). Walk it with the module's scan context.
	runtimeClosure := walkClosure(ctx.scannerFor(instance), domschemeRuntimeVFS, in.ScanCfg)

	scRef := emitSC(instance, srcVFS, headerVFS, domBinary, runtimeClosure, domLDRef, ctx.emit)

	// Register the generated header so a consumer's include-closure resolves it
	// and inherits the runtime.h output_include.
	runtimeInclude := []IncludeDirective{{kind: includeQuoted, target: internStr(domschemeRuntimeVFS.rel())}}
	registerBoundGeneratedParsedOutput(ctx, instance, pkSC, headerVFS, runtimeInclude, scRef, []NodeRef{domLDRef})

	reg := codegenRegForInstance(ctx, instance)
	reg.addClosureLeaf(headerVFS, srcVFS)
	reg.addClosureLeaf(headerVFS, domschemeRuntimeVFS)

	// Header-only: no object file to archive.
	return nil
}
