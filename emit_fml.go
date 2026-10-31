package main

var fmKV = KV{P: pkFM, PC: pcYellow}

func (e *EmitContext) emitLibraryFmlSource(meta SrcMeta) {
	src := meta.Source
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	srcRel := e.moduleSourceRel(src)
	toolRef, toolBin := e.tool(argToolsRelevFmlCodegen)
	srcVFS := e.sourceForEmission(meta)
	outVFS := build(instance.Path.relString(), "/", srcRel, ".inc")
	env := envVarsVCS
	ref := ctx.emit.reserve()

	pe := func() {
		node := NodeSpec{Node: Node{Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(
				toolBin.any(),
				strB2.any(),
				argDashO.any(),
				outVFS.any(),
				strT.any(),
				srcVFS.any(),
			)), Env: env}),
			Env: env, Inputs: na.inputList(na.vfsList(toolBin), na.vfsList(srcVFS)), KV: &fmKV,
			Outputs:        na.vfsList(outVFS),
			ForeignDepRefs: na.refList(toolRef)}, input: InputSpec{roots: na.inputList(na.vfsList(srcVFS))},
		}

		e.emitReservedNode(node, ref)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileSpec{
		OutputPath:    outVFS,
		ProducerRef:   ref,
		GeneratorRefs: e.ctx.na.refList(toolRef),

		OnUse: pending,
	})
}
