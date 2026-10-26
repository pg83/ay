package main

var bundleKV = KV{P: pkBN, PC: pcLightCyan}

func (e *EmitContext) emitBundle(b BundleEntry) {
	ctx, instance, d := e.ctx, e.instance, e.d
	reg := e.codegen
	dst := copyFileOutputVFS(instance.Path.relString(), b.Name)

	if reg.lookup(dst) != nil {
		e.collectScriptOutput(dst)

		return
	}

	src, srcRef, resolved := e.resolveBundleSource(b.Target)
	ref := ctx.emit.reserve()

	if b.Output != "" {
		script := buildScriptsBundleOutputPy
		args := ctx.na.anyList(d.tc.Python3.any(), script.any(), argResult.any(), src.any(), argName.any(), internStr(b.Output).any(), strOutput.any(), dst.any())

		e.emitReservedNode(Node{Platform: instance.Platform, Cmds: ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(args), Env: envVarsVCS}), Env: envVarsVCS, Inputs: NodeInputs{files: ctx.na.inputList(ctx.na.vfsList(src, script)), roots: ctx.na.inputList(ctx.na.vfsList(script))}, Outputs: ctx.na.vfsList(dst), DepRefs: ctx.na.refList(srcRef), KV: &bundleKV, Resources: usesPython3}, ref)
	} else {
		e.emitBundleNode(d.tc.Python3, src, dst, b.Suffix, srcRef, resolved, ref)
	}

	e.register(GeneratedFileInfo{
		OutputPath:    dst,
		ProducerRef:   ref,
		GeneratorRefs: nil,
	})
}

func (e *EmitContext) resolveBundleSource(target string) (VFS, NodeRef, bool) {
	ctx := e.ctx

	if !peerYaMakeExists(ctx.fs, dirKey(target).source()) {
		return 0, 0, false
	}

	bundleInstance := ctx.rootInstance(target, demandNone)

	bundleInstance.Platform = e.instance.Platform

	if e.instance.Demand != demandNone {
		bundleInstance.Demand = demandLinked
	}

	res := genModule(ctx, bundleInstance)

	if res.LDPath != nil {
		return *res.LDPath, res.LDRef, true
	}

	if res.ARPath != nil {
		return *res.ARPath, res.ARRef, true
	}

	return 0, 0, false
}

func (e *EmitContext) emitBundleNode(python3 VFS, src, dst VFS, suffix string, srcRef NodeRef, resolved bool, id NodeRef) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	fsTools := copyFsToolsVFS
	cmdArgs := na.anys.alloc(5)[:0]

	cmdArgs = append(cmdArgs, python3.any(), fsTools.any(), argRename.any())

	if resolved {
		cmdArgs = append(cmdArgs, internV(src.prefix(), src.relString(), suffix).any())
	}

	cmdArgs = append(cmdArgs, dst.any())
	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	env := envVarsVCS

	var inputHead []VFS
	var depRefs []NodeRef

	if resolved {
		inputHead = na.vfsList(src)
		depRefs = na.refList(srcRef)
	}

	node := Node{
		Platform:  instance.Platform,
		Cmds:      na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:       env,
		Inputs:    NodeInputs{files: na.inputList(inputHead, ctx.scripts[fsTools.rel()]), roots: na.inputList(ctx.scripts[fsTools.rel()])},
		KV:        &bundleKV,
		Outputs:   na.vfsList(dst),
		DepRefs:   depRefs,
		Resources: usesPython3,
	}

	e.emitReservedNode(node, id)
}
