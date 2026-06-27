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
	cxxFlags []STR,
	tc ModuleToolchain,
	emit *StreamingEmitter,
) NodeRef {
	na := emit.nodeArenas()
	outPrefix := instance.Path.rel() + "/"
	argsFileVFS := build(outPrefix, "__args")
	outVFS := build(outPrefix, outputHeader)
	argsFile := argsFileVFS.string()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmd0Args := []STR{
		tc.Python3,
		(yieldLinePyVFS).str(),
		arg2.str(),
		internStr(argsFile),
		tc.CXX,
	}

	cmd1Args := make([]STR, 0, 4+len(cxxFlags))

	cmd1Args = append(cmd1Args,
		tc.Python3,
		(yieldLinePyVFS).str(),
		arg2.str(),
		internStr(argsFile),
	)

	cmd1Args = append(cmd1Args, cxxFlags...)

	cmd2Args := []STR{
		tc.Python3,
		(xargsPyVFS).str(),
		arg2.str(),
		internStr(argsFile),
		tc.Python3,
		(buildInfoGenPyVFS).str(),
		(outVFS).str(),
	}

	inputs := []VFS{
		yieldLinePyVFS,
		xargsPyVFS,
		buildInfoGenPyVFS,
	}

	node := &Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmd0Args), Env: env}, Cmd{CmdArgs: na.chunkList(cmd1Args), Env: env}, Cmd{CmdArgs: na.chunkList(cmd2Args), Env: env}),
		Env:          env,
		Inputs:       na.inputList(inputs),
		KV:           &biKV,
		Outputs:      na.vfsList(outVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      []NodeRef{},
		Resources:    instance.Platform.UsesPython3Clang,
	}

	return emit.emit(node)
}

func biFlagsForInstance(targetP *Platform) []STR {
	bundle := compileFlagBundleFor(targetP)
	flags := make([]STR, 0, 100)
	cflagPrefix := append(muslCFlags(targetP.Flags[envMUSL] == strYes), sseBaseCFlags(targetP.ISA == ISAX8664)...)

	flags = appendCompileFlagPipeline(flags, bundle, warningFlags, bundle.Defines, targetP.CFlags, cflagPrefix, catboostOpenSourceDefineFor(targetP))
	flags = append(flags, (cxxStandardFlag).str())
	flags = appendArgStr(flags, cxxStandardWarnings)
	flags = append(flags, (baseUnitCxxNostdinc).str())
	flags = appendArgStr(flags, catboostOpenSourceDefineFor(targetP))
	flags = append(flags, (baseUnitCxxNostdinc).str())

	return flags
}
