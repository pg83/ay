package main

var jsNodeKV = KV{P: pkJS, PC: pcMagenta}

func emitJSReserved(instance ModuleInstance, allName string, sources []string, closure []VFS, p *Platform, tc ModuleToolchain, scripts ScriptDeps, emit *StreamingEmitter, id NodeRef) VFS {
	na := emit.nodeArenas()
	joinSrcs := buildScriptsGenJoinSrcsPy
	outVFS := build(instance.Path.relString(), "/", allName)
	statsPlatform := instance.Platform

	if p != nil {
		statsPlatform = p
	}

	cmdArgs := na.anys.alloc(5 + len(sources))[:0]

	cmdArgs = append(cmdArgs,
		tc.Python3.any(),
		(joinSrcs).any(),
		(outVFS).any(),
		argYaStartCommandFile.any(),
	)

	for _, s := range sources {
		cmdArgs = append(cmdArgs, internV(instance.Path.relString(), "/", s).any())
	}

	cmdArgs = append(cmdArgs, argYaEndCommandFile.any())
	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	env := envVarsVCS
	srcVFSs := na.vfs.alloc(len(sources))

	for i, s := range sources {
		srcVFSs[i] = source(instance.Path.relString(), "/", s)
	}

	na.vfs.commit(len(sources))

	srcVFSs = srcVFSs[:len(sources):len(sources)]

	inputs := na.inputList(scripts[joinSrcs.rel()], srcVFSs, closure)

	node := Node{
		Platform: statsPlatform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:          env,
		Inputs:       inputs,
		KV:           &jsNodeKV,
		Outputs:      na.vfsList(outVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	}

	emit.emitReservedNode(node, id)

	return outVFS
}

func (e *EmitContext) emitJoinSrcsStmt(js *JoinSrcsStmt) {
	ctx, instance, d := e.ctx, e.instance, e.d
	jsSources := anyStrs(js.Sources)
	joinClosure := e.joinSrcsIncludeClosure(instance.Platform, jsSources, d.cc.ScanCfg)
	ccClosure := joinClosure

	if instance.Platform.ISA == ISAX8664 {
		jsPeerAddInclGlobal := rebasePerArchPeerAddIncl(d.cc.PeerAddInclGlobal, instance.Platform.ISA, ctx.target.ISA)
		jsScanCfg := newScanContext(ctx.parsers, d.cc.AddIncl, jsPeerAddInclGlobal, includeScannerBasePaths(), instance.Path.relString())

		joinClosure = e.joinSrcsIncludeClosure(ctx.target, jsSources, jsScanCfg)
	}

	jsRef := ctx.emit.reserve()
	tc := d.tc
	outputName := js.OutputName

	pe := func() {
		emitJSReserved(instance, outputName, jsSources, joinClosure, ctx.target, tc, ctx.scripts, ctx.emit, jsRef)
	}

	joinOutVFS := build(instance.Path.relString(), "/", js.OutputName)
	ccIncl := jsCCIncludeInputs(instance, joinOutVFS, jsSources, ccClosure, ctx.scripts)

	var psc []ANY

	if p := d.perSrcCFlagsFor(joinOutVFS.any()); p != nil {
		psc = *p
	}

	e.register(GeneratedFileInfo{
		OutputPath:    joinOutVFS,
		ProducerRef:   jsRef,
		ClosureLeaves: ccIncl[1:],
		Compile:       e.ctx.na.compileSpec(CompileSpec{CFlags: psc}),
		OnUse:         &pe,
	})

	e.enqueueSrc(SrcMeta{Source: joinOutVFS.any(), Prio: stmtPrioDefault, Seq: js.Seq, Generated: true})
}

func (e *EmitContext) joinSrcsIncludeClosure(scanPlatform *Platform, sources []string, scanCfg ScanContext) []VFS {
	ctx, srcInstance, d := e.ctx, e.instance, e.d
	scanner := ctx.scannerForPlatform(scanPlatform)
	visited := scanner.visitedIDPool.Get().(*IdSet)

	visited.reset(vfsBound())

	defer scanner.visitedIDPool.Put(visited)

	modDirKey := srcInstance.Path.rel()
	srcRels := make([]string, len(sources))

	for i, src := range sources {
		srcRelOnDisk := srcInstance.Path.relString() + "/" + src

		if !ctx.fs.isFile(modDirKey, src) {
			for _, dir := range d.cc.SrcDirs {
				if dir.rel() != modDirKey && ctx.fs.isFile(dir.rel(), src) {
					srcRelOnDisk = dir.relString() + "/" + src

					break
				}
			}
		}

		srcRels[i] = srcRelOnDisk
		visited.add(source(srcRelOnDisk))
	}

	order := make([]VFS, 0, 1024)
	cfg := scanCfg

	for _, srcRelOnDisk := range srcRels {
		sc := scanner.getScanCtx(cfg, scanner.parsers.registry.registeredParserFor(srcRelOnDisk))

		sc.closureOf(source(srcRelOnDisk)).each(func(v VFS) {
			if visited.has(v) {
				return
			}

			visited.add(v)
			order = append(order, v)
		})

		scanner.putScanCtx(sc)
	}

	if len(order) == 0 {
		return nil
	}

	return ctx.na.vfsList(order...)
}

func jsCCIncludeInputs(srcInstance ModuleInstance, joinOut VFS, sources []string, closure []VFS, scripts ScriptDeps) []VFS {
	out := make([]VFS, 0, 3+len(sources)+len(closure))

	out = append(out, joinOut)
	out = append(out, scripts[buildScriptsGenJoinSrcsPy.rel()]...)

	for _, s := range sources {
		out = append(out, source(srcInstance.Path.relString(), "/", s))
	}

	out = append(out, closure...)

	return out
}
