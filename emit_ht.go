package main

var htKV = KV{P: pkHT, PC: pcYellow}

func (e *EmitContext) emitLibraryAspSource(meta SrcMeta) {
	ctx, instance, d := e.ctx, e.instance, e.d
	src := meta.Source
	na := ctx.na
	module := instance.Path.relString()
	srcRel := src.string()
	toolRef, toolBin := ctx.tool(argToolsHtml2cpp)
	srcVFS := resolveSourceVFS(ctx, instance, srcRel, d.srcDirs)
	outVFS := build(module, "/", srcRel, ".cpp")
	ref := ctx.emit.reserve()
	parsed := e.scanner.parsers.sourceParsedBuckets(srcVFS, nil)

	scanner := e.scanner
	scanCfg := snapshotScanCfg(ctx.na, d.cc.ScanCfg)

	pe := func() {
		cv := walkClosure(scanner, srcVFS, scanCfg)
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
		}

		ctx.emit.emitReservedNode(node, ref)
	}

	e.register(GeneratedFileInfo{
		OutputPath:     outVFS,
		SourcePath:     srcVFS,
		ProducerRef:    ref,
		GeneratorRefs:  e.ctx.na.refList(toolRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed[parsedIncludesLocal]},
		ClosureLeaves:  e.ctx.na.vfsList(srcVFS),
		OnUse:          &pe,
	})

	meta.Generated = true
	meta.Source = outVFS.any()
	e.enqueueSrc(meta)
}
