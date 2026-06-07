package main

var (
	yieldLinePyPath    = yieldLinePyVFS.String()
	xargsPyPath        = xargsPyVFS.String()
	buildInfoGenPyPath = buildInfoGenPyVFS.String()
)

func EmitBI(
	instance ModuleInstance,
	outputHeader string,
	cxxFlags []ANY,
	emit Emitter,
) NodeRef {
	outPrefix := instance.Path + "/"
	argsFileVFS := Build(outPrefix + "__args")
	outVFS := Build(outPrefix + outputHeader)
	argsFile := argsFileVFS.String()

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	cmd0Args := []ANY{
		internAny(instance.Platform.Tools.Python3),
		vfsAny(yieldLinePyVFS),
		any2,
		internAny(argsFile),
		instance.Platform.CXXArg,
	}

	cmd1Args := make([]ANY, 0, 4+len(cxxFlags))
	cmd1Args = append(cmd1Args,
		internAny(instance.Platform.Tools.Python3),
		vfsAny(yieldLinePyVFS),
		any2,
		internAny(argsFile),
	)
	cmd1Args = append(cmd1Args, cxxFlags...)

	cmd2Args := []ANY{
		internAny(instance.Platform.Tools.Python3),
		vfsAny(xargsPyVFS),
		any2,
		internAny(argsFile),
		internAny(instance.Platform.Tools.Python3),
		vfsAny(buildInfoGenPyVFS),
		vfsAny(outVFS),
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
		Tags:             []string{},
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          []NodeRef{},
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3, resourcePatternClangTool), instance.Platform))
}

func biFlagsForInstance(targetP *Platform) []ANY {
	bundle := compileFlagBundleFor(targetP)
	flags := make([]ANY, 0, 100)
	cflagPrefix := append(muslCFlags(targetP.Flags[envMUSL] == strYes), sseBaseCFlags(targetP.ISA == ISAX8664)...)
	flags = appendCompileFlagPipeline(flags, bundle, warningFlags, bundle.Defines, targetP.CFlags, cflagPrefix)
	flags = append(flags, argAny(cxxStandardFlag))
	flags = appendArgAny(flags, cxxStandardWarnings)
	flags = append(flags, argAny(baseUnitCxxNostdinc))
	flags = appendArgAny(flags, catboostOpenSourceDefine)
	flags = append(flags, argAny(baseUnitCxxNostdinc))
	return flags
}
