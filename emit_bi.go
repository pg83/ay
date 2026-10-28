package main

var (
	yieldLinePyPath    = yieldLinePyVFS.string()
	xargsPyPath        = xargsPyVFS.string()
	buildInfoGenPyPath = buildInfoGenPyVFS.string()
)

func (e *EmitContext) emitBIReserved(
	outputHeader string,
	cxxFlags []ANY,
	tc ModuleToolchain,
	id NodeRef,
) {
	instance := e.instance
	na := e.ctx.na
	outPrefix := instance.Path.relString() + "/"
	argsFileVFS := build(outPrefix, "__args")
	outVFS := build(outPrefix, outputHeader)
	env := envVarsVCS

	cmd0Args := na.anyList(
		tc.Python3.any(),
		yieldLinePyVFS.any(),
		arg2.any(),
		argsFileVFS.any(),
		tc.CXX.any(),
	)

	cmd1Args := na.anys.alloc(4 + len(cxxFlags))[:0]

	cmd1Args = append(cmd1Args,
		tc.Python3.any(),
		yieldLinePyVFS.any(),
		arg2.any(),
		argsFileVFS.any(),
	)

	cmd1Args = append(cmd1Args, cxxFlags...)
	na.anys.commit(len(cmd1Args))

	cmd1Args = cmd1Args[:len(cmd1Args):len(cmd1Args)]

	cmd2Args := na.anyList(
		tc.Python3.any(),
		xargsPyVFS.any(),
		arg2.any(),
		argsFileVFS.any(),
		tc.Python3.any(),
		buildInfoGenPyVFS.any(),
		outVFS.any(),
	)

	inputs := na.vfsList(
		yieldLinePyVFS,
		xargsPyVFS,
		buildInfoGenPyVFS,
	)

	node := NodeSpec{Node: Node{Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmd0Args), Env: env}, Cmd{CmdArgs: na.chunkList(cmd1Args), Env: env}, Cmd{CmdArgs: na.chunkList(cmd2Args), Env: env}),
		Env:  env, Inputs: na.rootInputList(inputs), KV: &instance.Platform.BuildInfoKV,
		Outputs:   na.vfsList(outVFS),
		DepRefs:   na.refList(),
		Resources: instance.Platform.UsesPython3Clang},
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitBuildInfoStmt() {
	ctx, instance, d := e.ctx, e.instance, e.d
	outPrefix := instance.Path.relString() + "/"
	biRef := ctx.emit.reserve()
	createFor := d.createBuildInfoFor.string()
	tc := d.tc

	pe := func() {
		blocks := compilerCommandBlocks(&d.cc.CCBlocks, true)

		e.emitBIReserved(createFor, ctx.na.anyConcat(blocks.flags, blocks.cxxTail), tc, biRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileSpec{
		OutputPath:  build(outPrefix, d.createBuildInfoFor.string()),
		ProducerRef: biRef,
		OnUse:       pending,
	})
}
