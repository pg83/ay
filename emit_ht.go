package main

var htKV = KV{P: pkHT, PC: pcYellow}

func (e *EmitContext) emitLibraryAspSource(meta SrcMeta) {
	ctx, instance := e.ctx, e.instance
	src := meta.Source
	na := ctx.na
	module := instance.Path.relString()
	srcRel := e.moduleSourceRel(src)
	toolRef, toolBin := e.tool(argToolsHtml2cpp)
	srcVFS := e.sourceForEmission(meta)
	outVFS := build(module, "/", srcRel, ".cpp")
	ref := ctx.emit.reserve()
	parsed := e.scanner.parsedBucketForInput(srcVFS, parsedIncludesCpp, nil)

	pe := func() {
		env := envVarsVCS

		node := NodeSpec{Node: Node{Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(toolBin.any(), srcVFS.any(), outVFS.any())), Env: env}),
			Env:  env, Inputs: na.inputList(na.vfsList(toolBin), na.vfsList(srcVFS)), KV: &htKV,
			Outputs:        na.vfsList(outVFS),
			ForeignDepRefs: na.refList(toolRef)}, input: InputSpec{roots: na.inputList(na.vfsList(srcVFS))},
		}

		e.emitReservedNode(node, ref)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileSpec{
		OutputPath:     outVFS,
		SourcePath:     srcVFS,
		ProducerRef:    ref,
		GeneratorRefs:  e.ctx.na.refList(toolRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},

		OnUse: pending,
	})

	meta.Source = outVFS.any()
	e.enqueueSrc(meta)
}
