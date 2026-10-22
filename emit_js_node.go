package main

var jsNodeKV = KV{P: pkJS, PC: pcMagenta}

func (e *EmitContext) emitJSReserved(allName string, sources []string, closure []VFS, p *Platform, tc ModuleToolchain, scripts ScriptDeps, id NodeRef) VFS {
	instance := e.instance
	na := e.ctx.na
	joinSrcs := buildScriptsGenJoinSrcsPy
	outVFS := copyFileOutputVFS(instance.Path.relString(), allName)
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
		srcVFSs[i] = resolveSourceVFS(e.ctx, instance, s, e.d.srcDirs)
	}

	na.vfs.commit(len(sources))

	srcVFSs = srcVFSs[:len(sources):len(sources)]

	inputs := na.inputList(scripts[joinSrcs.rel()], srcVFSs, closure)

	node := Node{
		Platform: statsPlatform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:       env,
		Inputs:    inputs,
		KV:        &jsNodeKV,
		Outputs:   na.vfsList(outVFS),
		Resources: usesPython3,
	}

	e.emitReservedNode(node, id)

	return outVFS
}

func (e *EmitContext) emitJoinSrcsStmt(js *JoinSrcsStmt) {
	ctx, instance, d := e.ctx, e.instance, e.d
	jsSources := anyStrs(js.Sources)
	jsRef := ctx.emit.reserve()
	tc := d.tc
	outputName := js.OutputName

	pe := func() {
		e.emitJSReserved(outputName, jsSources, nil, ctx.target, tc, ctx.scripts, jsRef)
	}

	pending := e.pendingEmit(pe)
	joinOutVFS := copyFileOutputVFS(instance.Path.relString(), js.OutputName)

	info := GeneratedFileInfo{
		OutputPath:  joinOutVFS,
		ProducerRef: jsRef,
		OnUse:       pending,
	}

	includes := ctx.na.dirs.alloc(len(jsSources))

	for i, s := range jsSources {
		e.cppStyleSource(internStr(s).any())
		includes[i] = IncludeDirective{kind: includeQuoted, target: includeTarget(resolveSourceVFS(ctx, instance, s, d.srcDirs).any())}
	}

	ctx.na.dirs.commit(len(jsSources))
	info.ParsedIncludes = ParsedIncludeSet{parsedIncludesLocal: includes[:len(jsSources):len(jsSources)]}

	e.register(info)

	e.enqueueSrc(SrcMeta{Source: joinOutVFS.any(), Prio: stmtPrioDefault, Seq: js.Seq})
}
