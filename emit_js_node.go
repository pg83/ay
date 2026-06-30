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

	node := &Node{
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

	return emit.emit(node), outVFS
}

func emitJoinSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, moduleInputs ModuleCCInputs) ([]NodeRef, []VFS, map[VFS]SrcMeta) {
	refs := make([]NodeRef, 0, len(d.joinSrcs))
	outs := make([]VFS, 0, len(d.joinSrcs))
	meta := make(map[VFS]SrcMeta, len(d.joinSrcs))

	for _, js := range d.joinSrcs {
		jsSources := strStrings(js.Sources)
		joinClosure := joinSrcsIncludeClosure(ctx, instance.Platform, instance, jsSources, moduleInputs)
		ccClosure := joinClosure

		if instance.Platform.ISA == ISAX8664 {
			jsModuleInputs := moduleInputs

			jsModuleInputs.PeerAddInclGlobal = rebasePerArchPeerAddIncl(moduleInputs.PeerAddInclGlobal, instance.Platform.ISA, ctx.target.ISA)
			jsModuleInputs.ScanCfg = newScanContext(ctx.parsers, jsModuleInputs.AddIncl, jsModuleInputs.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

			joinClosure = joinSrcsIncludeClosure(ctx, ctx.target, instance, jsSources, jsModuleInputs)
		}

		jsRef, joinOutVFS := emitJS(instance, js.OutputName, jsSources, joinClosure, ctx.target, d.tc, ctx.scripts, ctx.emit)
		ccIncl := jsCCIncludeInputs(instance, joinOutVFS, jsSources, ccClosure, ctx.scripts)

		ctx.codegenFor(instance).register(&GeneratedFileInfo{
			OutputPath:    joinOutVFS,
			ProducerRef:   jsRef,
			ClosureLeaves: ccIncl[1:],
			Compile:       &CompileSpec{FlatOutput: moduleInputs.FlatOutput, CFlags: moduleInputs.PerSourceCFlags},
		})

		if se := emitOneSource(ctx, instance, d, joinOutVFS.str(), moduleInputs); se != nil {
			refs = append(refs, se.Ref)
			outs = append(outs, se.OutPath)
			meta[se.OutPath] = SrcMeta{Prio: stmtPrioDefault, Seq: js.Seq, Generated: true}
		}
	}

	return refs, outs, meta
}

func joinSrcsIncludeClosure(ctx *GenCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)
	visited := scanner.visitedIDPool.Get().(*IdSet)

	visited.reset(vfsBound())

	defer scanner.visitedIDPool.Put(visited)

	modDirKey := dirKey(srcInstance.Path.rel())
	srcRels := make([]string, len(sources))

	for i, src := range sources {
		srcRelOnDisk := srcInstance.Path.rel() + "/" + src

		if !ctx.fs.isFile(modDirKey, src) {
			for _, dir := range in.SrcDirs {
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
	cfg := in.ScanCfg

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
