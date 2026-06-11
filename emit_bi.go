package main

var (
	yieldLinePyPath    = yieldLinePyVFS.string()
	xargsPyPath        = xargsPyVFS.string()
	buildInfoGenPyPath = buildInfoGenPyVFS.string()
)

func emitBI(
	instance ModuleInstance,
	outputHeader string,
	cxxFlags []STR,
	tc ModuleToolchain,
	emit Emitter,
) NodeRef {
	outPrefix := instance.Path.rel() + "/"
	argsFileVFS := build(outPrefix + "__args")
	outVFS := build(outPrefix + outputHeader)
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

	cacheFalse := false
	node := &Node{
		Platform: instance.Platform,
		Cache:    &cacheFalse,
		Cmds: []Cmd{
			{CmdArgs: ArgChunks{cmd0Args}, Env: env},
			{CmdArgs: ArgChunks{cmd1Args}, Env: env},
			{CmdArgs: ArgChunks{cmd2Args}, Env: env},
		},
		Env:              env,
		Inputs:           InputChunks{inputs},
		KV:               KV{P: pkBI, PC: pcYellow, ShowOut: true, DisableCache: "yes"},
		Outputs:          []VFS{outVFS},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          []NodeRef{},
		usesResources:    []string{resourcePatternYMakePython3, resourcePatternClangTool + instance.Platform.ClangVer},
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
