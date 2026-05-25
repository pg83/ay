package main

// yieldLinePyPath is the source-relative path to the yield_line.py script.
var yieldLinePyVFS = Intern("$(S)/build/scripts/yield_line.py")
var yieldLinePyPath = yieldLinePyVFS.String()

// xargsPyPath is the source-relative path to the xargs.py script.
var xargsPyVFS = Intern("$(S)/build/scripts/xargs.py")
var xargsPyPath = xargsPyVFS.String()

// buildInfoGenPyPath is the source-relative path to the build_info_gen.py
// script invoked by xargs.py in the BI node.
var buildInfoGenPyVFS = Intern("$(S)/build/scripts/build_info_gen.py")
var buildInfoGenPyPath = buildInfoGenPyVFS.String()

// EmitBI emits a BI node for CREATE_BUILDINFO_FOR(outputHeader).
// cmd[0] and cmd[1] stage the compiler invocation into <module>/__args via
// yield_line.py; cmd[2] feeds those args to build_info_gen.py through xargs.py.
// Flags come from the target CXX bundle (same as a target CC for this module,
// minus -c, -o, input path). cache:false is required at top level; the
// normalizer strips it during canonicalization so it doesn't affect hashes.
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

// biFlagsForInstance composes the CXX flag bundle for a BI node.
func biFlagsForInstance(targetP *Platform) []string {
	bundle := compileFlagBundleFor(targetP)
	flags := make([]string, 0, 100)
	flags = appendCompileFlagPipeline(flags, bundle, warningFlags, bundle.Defines, nil, []string{"-D_musl_"})
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
