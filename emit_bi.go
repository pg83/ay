package main

var (
	yieldLinePyVFS     = Intern("$(S)/build/scripts/yield_line.py")
	yieldLinePyPath    = yieldLinePyVFS.String()
	xargsPyVFS         = Intern("$(S)/build/scripts/xargs.py")
	xargsPyPath        = xargsPyVFS.String()
	buildInfoGenPyVFS  = Intern("$(S)/build/scripts/build_info_gen.py")
	buildInfoGenPyPath = buildInfoGenPyVFS.String()
)

func EmitBI(
	instance ModuleInstance,
	outputHeader string,
	cxxFlags []string,
	emit Emitter,
) NodeRef {
	outPrefix := instance.Path + "/"
	argsFileVFS := Build(outPrefix + "__args")
	outVFS := Build(outPrefix + outputHeader)
	argsFile := argsFileVFS.String()

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	cmd0Args := []string{
		instance.Platform.Tools.Python3,
		yieldLinePyPath,
		"--",
		argsFile,
		instance.Platform.Tools.CXX,
	}

	cmd1Args := make([]string, 0, 4+len(cxxFlags))
	cmd1Args = append(cmd1Args,
		instance.Platform.Tools.Python3,
		yieldLinePyPath,
		"--",
		argsFile,
	)
	cmd1Args = append(cmd1Args, cxxFlags...)

	cmd2Args := []string{
		instance.Platform.Tools.Python3,
		xargsPyPath,
		"--",
		argsFile,
		instance.Platform.Tools.Python3,
		buildInfoGenPyPath,
		outVFS.String(),
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

func biFlagsForInstance(targetP *Platform) []string {
	bundle := compileFlagBundleFor(targetP)
	flags := make([]string, 0, 100)
	cflagPrefix := append(muslCFlags(targetP.Flags[envMUSL] == strYes), sseBaseCFlags(targetP.ISA == ISAX8664)...)
	flags = appendCompileFlagPipeline(flags, bundle, warningFlags, bundle.Defines, targetP.CFlags, cflagPrefix)
	flags = append(flags, cxxStandardFlag)
	flags = append(flags,
		"-Wimport-preprocessor-directive-pedantic",
		"-Woverloaded-virtual",
		"-Wno-ambiguous-reversed-operator",
		"-Wno-defaulted-function-deleted",
		"-Wno-deprecated-anon-enum-enum-conversion",
		"-Wno-deprecated-enum-enum-conversion",
		"-Wno-deprecated-enum-float-conversion",
		"-Wno-deprecated-volatile",
		"-Wno-pessimizing-move",
		"-Wno-undefined-var-template",
	)
	flags = append(flags, "-nostdinc++")
	flags = append(flags, catboostOpenSourceDefine...)
	flags = append(flags, "-nostdinc++")
	return flags
}
