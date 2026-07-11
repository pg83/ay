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

	scanner := e.scanner
	scanCtx := d.scanCtx

	pe := func() {
		cv := scanner.walkClosure(srcVFS, scanCtx, scanDomainCC)
		depRefs := resolveCodegenDepRefsInclView(ctx, instance, na, cv)
		block := na.vfs.alloc(2 + cv.len())
		k := 0

		block[k] = toolBin
		k++
		block[k] = srcVFS
		k++

		cv.each(func(p VFS) {
			if p.isSource() && p != srcVFS {
				block[k] = p
				k++
			}
		})

		na.vfs.commit(k)

		env := envVarsVCS

		node := Node{
			Platform:       instance.Platform,
			Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(toolBin.any(), srcVFS.any(), outVFS.any())), Env: env}),
			Env:            env,
			Inputs:         na.inputList(block[:k:k]),
			KV:             &htKV,
			Outputs:        na.vfsList(outVFS),
			Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			ForeignDepRefs: na.refList(toolRef),
			DepRefs:        depRefs,
		}

		ctx.emit.emitReservedNode(node, ref)
	}
	pending := e.ctx.na.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     outVFS,
		SourcePath:     srcVFS,
		ProducerRef:    ref,
		GeneratorRefs:  e.ctx.na.refList(toolRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		ClosureLeaves:  e.ctx.na.vfsList(srcVFS),
		OnUse:          pending,
	})

	meta.Source = outVFS.any()
	e.enqueueSrc(meta)
}
