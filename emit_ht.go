package main

var htKV = KV{P: pkHT, PC: pcYellow}

func (e *EmitContext) emitLibraryAspSource(meta SrcMeta) {
	ctx, instance, d := e.ctx, e.instance, e.d
	src := meta.Source
	na := ctx.na
	module := instance.Path.relString()
	srcRel := e.moduleSourceRel(src)
	toolRef, toolBin := ctx.tool(argToolsHtml2cpp)
	srcVFS := e.resolveModuleSourceVFS(src, d.srcDirs)
	outVFS := build(module, "/", srcRel, ".cpp")
	ref := ctx.emit.reserve()
	parsed := e.scanner.parsedBucketForInput(srcVFS, parsedIncludesLocal, nil)

	pe := func() {
		env := envVarsVCS

		node := Node{
			Platform:        instance.Platform,
			Cmds:            na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(toolBin.any(), srcVFS.any(), outVFS.any())), Env: env}),
			Env:             env,
			Inputs:          na.inputList(na.vfsList(toolBin), na.vfsList(srcVFS)),
			BuildFromInputs: na.inputList(na.vfsList(srcVFS)),
			KV:              &htKV,
			Outputs:         na.vfsList(outVFS),
			ForeignDepRefs:  na.refList(toolRef),
		}

		e.emitReservedNode(node, ref)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileInfo{
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
