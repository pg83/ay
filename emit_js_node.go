package main

var jsNodeKV = KV{P: pkJS, PC: pcMagenta}

func emitJS(instance ModuleInstance, allName string, sources []string, closure []VFS, p *Platform, tc ModuleToolchain, scripts ScriptDeps, emit *StreamingEmitter) (NodeRef, VFS) {
	na := emit.nodeArenas()
	joinSrcs := buildScriptsGenJoinSrcsPy
	outVFS := build(instance.Path.rel(), "/", allName)
	statsPlatform := instance.Platform

	if p != nil {
		statsPlatform = p
	}

	cmdArgs := make([]STR, 0, 4+len(sources))

	cmdArgs = append(cmdArgs,
		tc.Python3,
		(joinSrcs).str(),
		(outVFS).str(),
		argYaStartCommandFile.str(),
	)

	for _, s := range sources {
		cmdArgs = append(cmdArgs, internV(instance.Path.rel(), "/", s))
	}

	cmdArgs = append(cmdArgs, argYaEndCommandFile.str())

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	srcVFSs := make([]VFS, 0, len(sources))

	for _, s := range sources {
		srcVFSs = append(srcVFSs, source(instance.Path.rel(), "/", s))
	}

	inputs := na.inputList(scripts[joinSrcs], srcVFSs, closure)

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

	return emit.emitNode(node), outVFS
}

func (e *EmitContext) emitJoinSrcsStmt(js *JoinSrcsStmt) {
	ctx, instance, d := e.ctx, e.instance, e.d
	jsSources := strStrings(js.Sources)
	joinClosure := e.joinSrcsIncludeClosure(instance.Platform, jsSources, d.cc.ScanCfg)
	ccClosure := joinClosure

	if instance.Platform.ISA == ISAX8664 {
		jsPeerAddInclGlobal := rebasePerArchPeerAddIncl(d.cc.PeerAddInclGlobal, instance.Platform.ISA, ctx.target.ISA)
		jsScanCfg := newScanContext(ctx.parsers, d.cc.AddIncl, jsPeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

		joinClosure = e.joinSrcsIncludeClosure(ctx.target, jsSources, jsScanCfg)
	}

	jsRef, joinOutVFS := emitJS(instance, js.OutputName, jsSources, joinClosure, ctx.target, d.tc, ctx.scripts, ctx.emit)
	ccIncl := jsCCIncludeInputs(instance, joinOutVFS, jsSources, ccClosure, ctx.scripts)

	var psc []ARG

	if p := d.perSrcCFlagsFor(joinOutVFS.str()); p != nil {
		psc = *p
	}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:    joinOutVFS,
		ProducerRef:   jsRef,
		ClosureLeaves: ccIncl[1:],
		Compile:       &CompileSpec{FlatOutput: d.flatSrc(joinOutVFS.str()), CFlags: psc},
	})

	e.enqueueSrc(SrcMeta{Source: joinOutVFS.str(), Prio: stmtPrioDefault, Seq: js.Seq, Generated: true})
}

func (e *EmitContext) joinSrcsIncludeClosure(scanPlatform *Platform, sources []string, scanCfg ScanContext) []VFS {
	ctx, srcInstance, d := e.ctx, e.instance, e.d
	scanner := ctx.scannerForPlatform(scanPlatform)
	visited := scanner.visitedIDPool.Get().(*IdSet)

	visited.reset(vfsBound())

	defer scanner.visitedIDPool.Put(visited)

	modDirKey := dirKey(srcInstance.Path.rel())
	srcRels := make([]string, len(sources))

	for i, src := range sources {
		srcRelOnDisk := srcInstance.Path.rel() + "/" + src

		if !ctx.fs.isFile(modDirKey, src) {
			for _, dir := range d.cc.SrcDirs {
				if dir != modDirKey && ctx.fs.isFile(dir, src) {
					srcRelOnDisk = dir.rel() + "/" + src

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

		for _, v := range sc.closureOf(source(srcRelOnDisk)) {
			if visited.has(v) {
				continue
			}

			visited.add(v)
			order = append(order, v)
		}

		scanner.putScanCtx(sc)
	}

	if len(order) == 0 {
		return nil
	}

	return order
}

func jsCCIncludeInputs(srcInstance ModuleInstance, joinOut VFS, sources []string, closure []VFS, scripts ScriptDeps) []VFS {
	out := make([]VFS, 0, 3+len(sources)+len(closure))

	out = append(out, joinOut)
	out = append(out, scripts[buildScriptsGenJoinSrcsPy]...)

	for _, s := range sources {
		out = append(out, source(srcInstance.Path.rel(), "/", s))
	}

	out = append(out, closure...)

	return out
}
