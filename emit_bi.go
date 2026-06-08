package main

var (
	yieldLinePyPath    = yieldLinePyVFS.String()
	xargsPyPath        = xargsPyVFS.String()
	buildInfoGenPyPath = buildInfoGenPyVFS.String()
)

func EmitBI(
	instance ModuleInstance,
	outputHeader string,
	cxxFlags []STR,
	tc moduleToolchain,
	emit Emitter,
) NodeRef {
	outPrefix := instance.Path + "/"
	argsFileVFS := Build(outPrefix + "__args")
	outVFS := Build(outPrefix + outputHeader)
	argsFile := argsFileVFS.String()

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

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
		Cache: &cacheFalse,
		Cmds: []Cmd{
			{CmdArgs: cmd0Args, Env: env},
			{CmdArgs: cmd1Args, Env: env},
			{CmdArgs: cmd2Args, Env: env},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkBI, PC: pcYellow, ShowOut: "yes", DisableCache: "yes"},
		Outputs:          []VFS{outVFS},
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          []NodeRef{},
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3, resourcePatternClangTool), instance.Platform))
}

func biFlagsForInstance(targetP *Platform) []STR {
	bundle := compileFlagBundleFor(targetP)
	flags := make([]STR, 0, 100)
	cflagPrefix := append(muslCFlags(targetP.Flags[envMUSL] == strYes), sseBaseCFlags(targetP.ISA == ISAX8664)...)
	flags = appendCompileFlagPipeline(flags, bundle, warningFlags, bundle.Defines, targetP.CFlags, cflagPrefix)
	flags = append(flags, (cxxStandardFlag).str())
	flags = appendArgStr(flags, cxxStandardWarnings)
	flags = append(flags, (baseUnitCxxNostdinc).str())
	flags = appendArgStr(flags, catboostOpenSourceDefine)
	flags = append(flags, (baseUnitCxxNostdinc).str())
	return flags
}
