package main

var fmKV = KV{P: pkFM, PC: pcYellow}

func (e *EmitContext) emitLibraryFmlSource(src ANY) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	srcRel := src.string()
	toolRef, toolBin := ctx.tool(argToolsRelevFmlCodegen)
	srcVFS := source(instance.Path.relString(), "/", srcRel)
	outVFS := build(instance.Path.relString(), "/", srcRel, ".inc")
	env := envVarsVCS

	ref := ctx.emit.reserve()

	info := e.codegen.register(GeneratedFileInfo{
		OutputPath:    outVFS,
		ProducerRef:   ref,
		GeneratorRefs: e.ctx.na.refList(toolRef),
		ClosureLeaves: e.ctx.na.vfsList(srcVFS),
	})

	pe := &PendingEmit{owner: ctx.instanceKey(instance), fn: func() {
		node := Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(
				toolBin.any(),
				strB2.any(),
				argDashO.any(),
				outVFS.any(),
				strT.any(),
				srcVFS.any(),
			)), Env: env}),
			Env:            env,
			Inputs:         na.inputList(na.vfsList(toolBin, srcVFS)),
			KV:             &fmKV,
			Outputs:        na.vfsList(outVFS),
			Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			ForeignDepRefs: na.refList(toolRef),
		}

		ctx.emit.emitReservedNode(node, ref)
	}}

	info.pending = pe

	e.noteOwn(pe)
}
