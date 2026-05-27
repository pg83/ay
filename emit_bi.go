package main

var yieldLinePyVFS = Intern("$(S)/build/scripts/yield_line.py")
var yieldLinePyPath = yieldLinePyVFS.String()

var xargsPyVFS = Intern("$(S)/build/scripts/xargs.py")
var xargsPyPath = xargsPyVFS.String()

var buildInfoGenPyVFS = Intern("$(S)/build/scripts/build_info_gen.py")
var buildInfoGenPyPath = buildInfoGenPyVFS.String()

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

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

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
		Env:    env,
		Inputs: inputs,
		KV: map[string]interface{}{
			"disable_cache": "yes",
			"p":             "BI",
			"pc":            "yellow",
			"show_out":      "yes",
		},
		Outputs: []VFS{outVFS},
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform))
}

func biFlagsForInstance(targetP *Platform) []string {
	bundle := compileFlagBundleFor(targetP)
	flags := make([]string, 0, 100)
	flags = appendCompileFlagPipeline(flags, bundle, warningFlags, bundle.Defines, nil, assembleModuleScopeCFlags(targetP, targetP.Musl(), nil))
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
