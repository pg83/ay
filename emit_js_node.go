package main

var jsNodeKV = KV{P: pkJS, PC: pcMagenta}

func (e *EmitContext) emitJSReserved(allName string, sources []string, closure []VFS, p *Platform, tc ModuleToolchain, scripts ScriptDeps, id NodeRef) VFS {
	instance := e.instance
	na := e.ctx.na
	joinSrcs := buildScriptsGenJoinSrcsPy
	outVFS := build(instance.Path.relString(), "/", allName)
	statsPlatform := instance.Platform

	if p != nil {
		statsPlatform = p
	}

	cmdArgs := na.anys.alloc(5 + len(sources))[:0]

	cmdArgs = append(cmdArgs,
		tc.Python3.any(),
		joinSrcs.any(),
		outVFS.any(),
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

	e.emitReservedNode(node, id)

	return outVFS
}

func (e *EmitContext) emitJoinSrcsStmt(js *JoinSrcsStmt) {
	ctx, instance, d := e.ctx, e.instance, e.d
	jsSources := anyStrs(js.Sources)
	joinClosure := e.joinSrcsIncludeClosure(instance.Platform, jsSources, scanDomainCC)
	ccClosure := joinClosure

	if instance.Platform.ISA == ISAX8664 {
		joinClosure = e.joinSrcsIncludeClosure(ctx.target, jsSources, scanDomainJoinTarget)
	}

	jsRef := ctx.emit.reserve()
	tc := d.tc
	outputName := js.OutputName

	pe := func() {
		e.emitJSReserved(outputName, jsSources, joinClosure, ctx.target, tc, ctx.scripts, jsRef)
	}
	pending := e.ctx.na.pendingEmit(pe)

	joinOutVFS := build(instance.Path.relString(), "/", js.OutputName)
	ccIncl := jsCCIncludeInputs(instance, joinOutVFS, jsSources, ccClosure, ctx.scripts)

	e.register(GeneratedFileInfo{
		OutputPath:    joinOutVFS,
		ProducerRef:   jsRef,
		ClosureLeaves: ccIncl[1:],
		OnUse:         pending,
	})

	e.enqueueSrc(SrcMeta{Source: joinOutVFS.any(), Prio: stmtPrioDefault, Seq: js.Seq})
}

func (e *EmitContext) joinSrcsIncludeClosure(scanPlatform *Platform, sources []string, domain ScanDomain) []VFS {
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
	for _, srcRelOnDisk := range srcRels {
		sc := scanner.getScanCtx(d.scanCtx, domain, scanner.parsers.registry.registeredParserFor(srcRelOnDisk))

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
