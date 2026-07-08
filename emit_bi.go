package main

var (
	yieldLinePyPath    = yieldLinePyVFS.string()
	xargsPyPath        = xargsPyVFS.string()
	buildInfoGenPyPath = buildInfoGenPyVFS.string()
	biKV               = KV{P: pkBI, PC: pcYellow, ShowOut: true, DisableCache: true}
)

func emitBI(
	instance ModuleInstance,
	outputHeader string,
	cxxFlags []ANY,
	tc ModuleToolchain,
	emit *StreamingEmitter,
) NodeRef {
	na := emit.nodeArenas()
	outPrefix := instance.Path.relString() + "/"
	argsFileVFS := build(outPrefix, "__args")
	outVFS := build(outPrefix, outputHeader)
	env := envVarsVCS

	cmd0Args := na.anyList(
		tc.Python3.any(),
		(yieldLinePyVFS).any(),
		arg2.any(),
		argsFileVFS.any(),
		tc.CXX.any(),
	)

	cmd1Args := na.anys.alloc(4 + len(cxxFlags))[:0]

	cmd1Args = append(cmd1Args,
		tc.Python3.any(),
		(yieldLinePyVFS).any(),
		arg2.any(),
		argsFileVFS.any(),
	)

	cmd1Args = append(cmd1Args, cxxFlags...)
	na.anys.commit(len(cmd1Args))

	cmd1Args = cmd1Args[:len(cmd1Args):len(cmd1Args)]

	cmd2Args := na.anyList(
		tc.Python3.any(),
		(xargsPyVFS).any(),
		arg2.any(),
		argsFileVFS.any(),
		tc.Python3.any(),
		(buildInfoGenPyVFS).any(),
		(outVFS).any(),
	)

	inputs := na.vfsList(
		yieldLinePyVFS,
		xargsPyVFS,
		buildInfoGenPyVFS,
	)

	node := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmd0Args), Env: env}, Cmd{CmdArgs: na.chunkList(cmd1Args), Env: env}, Cmd{CmdArgs: na.chunkList(cmd2Args), Env: env}),
		Env:          env,
		Inputs:       na.inputList(inputs),
		KV:           &biKV,
		Outputs:      na.vfsList(outVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      na.refList(),
		Resources:    instance.Platform.UsesPython3Clang,
	}

	return emit.emitNode(node)
}

func biFlagsForInstance(targetP *Platform) []ANY {
	bundle := compileFlagBundleFor(targetP)
	flags := make([]ANY, 0, 100)
	cflagPrefix := append(muslCFlags(targetP.Flags[envMUSL] == strYes), sseBaseCFlags(targetP.ISA == ISAX8664)...)

	flags = appendCompileFlagPipeline(flags, bundle, warningFlags, bundle.Defines, targetP.CFlags, cflagPrefix, catboostOpenSourceDefineFor(targetP))
	flags = append(flags, (cxxStandardFlag).any())
	flags = appendAnyLists(flags, cxxStandardWarnings)
	flags = append(flags, (baseUnitCxxNostdinc).any())
	flags = appendAnyLists(flags, catboostOpenSourceDefineFor(targetP))
	flags = append(flags, (baseUnitCxxNostdinc).any())

	return flags
}

func (e *EmitContext) emitBuildInfoStmt() {
	ctx, instance, d := e.ctx, e.instance, e.d
	outPrefix := instance.Path.relString() + "/"
	biRef := emitBI(instance, d.createBuildInfoFor.string(), biFlagsForInstance(instance.Platform), d.tc, ctx.emit)

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:    build(outPrefix, d.createBuildInfoFor.string()),
		ProducerRef:   biRef,
		GeneratorRefs: nil,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: []IncludeDirective{
			{kind: includeQuoted, target: includeTarget(buildInfoGenPyVFS.rel().any())},
			{kind: includeQuoted, target: includeTarget(xargsPyVFS.rel().any())},
			{kind: includeQuoted, target: includeTarget(yieldLinePyVFS.rel().any())},
		}},
	})
}
