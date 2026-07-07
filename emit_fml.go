package main

var fmKV = KV{P: pkFM, PC: pcYellow}

func (e *EmitContext) emitLibraryFmlSource(src STR) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na
	srcRel := src.string()
	toolRef, toolBin := ctx.tool(argToolsRelevFmlCodegen)
	srcVFS := source(instance.Path.relString(), "/", srcRel)
	outVFS := build(instance.Path.relString(), "/", srcRel, ".inc")
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkListSTR(na.strList(
			toolBin.fullSTR(),
			strB2,
			argDashO.str(),
			outVFS.fullSTR(),
			strT,
			srcVFS.fullSTR(),
		)), Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(toolBin, srcVFS)),
		KV:             &fmKV,
		Outputs:        na.vfsList(outVFS),
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(toolRef),
	}

	ref := ctx.emit.emitNode(node)

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:    outVFS,
		ProducerRef:   ref,
		GeneratorRefs: []NodeRef{toolRef},
		ClosureLeaves: []VFS{srcVFS},
	})
}
