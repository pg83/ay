package main

// The resource clang toolchain (build/platform/clang's llvm-ar) is always an
// LLVM archiver in gnu format — link_lib.py's archiver type / format arguments.
const (
	arTypeLLVM  = "LLVM_AR"
	arFormatGNU = "gnu"
)

func emitARNode(
	instance ModuleInstance,
	archivePath VFS,
	tag STR,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	tc moduleToolchain,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	scriptVFS := buildScriptsLinkLibPy

	cmdEnv := hostP.ToolEnv()

	cmdArgs := []STR{
		tc.Python3,
		(scriptVFS).str(),
		tc.AR,
		internStr(arTypeLLVM),
		internStr(arFormatGNU),
		argB.str(),
		argNone.str(),
		arg2.str(),
	}

	if arPluginPath != nil {
		cmdArgs = append(cmdArgs, argPlugin.str(), (*arPluginPath).str())
	}

	cmdArgs = append(cmdArgs, arg2.str(), (archivePath).str())

	for _, p := range objPaths {
		cmdArgs = append(cmdArgs, (p).str())
	}

	// objPaths is the caller's member slice — referenced as its own chunk,
	// never copied; only the script/plugin tail is built locally.
	inputTail := make([]VFS, 0, 2)
	inputTail = append(inputTail, scriptVFS)

	if arPluginPath != nil {
		inputTail = append(inputTail, *arPluginPath)
	}

	topEnv := hostP.ToolEnv()

	targetProperties := TargetProperties{ModuleDir: instance.Path.Rel(), ModuleLang: mlCPP, ModuleType: mtLib}

	if instance.Language == LangPy {
		targetProperties.ModuleLang = mlPy3
	}

	if tag != 0 {
		targetProperties.ModuleTag = tag
	}

	depRefs := make([]NodeRef, 0, len(objRefs)+len(peerArchiveRefs))
	depRefs = append(depRefs, objRefs...)
	depRefs = append(depRefs, peerArchiveRefs...)

	n := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: argChunks{cmdArgs},
				Env:     cmdEnv,
			},
		},
		Env:              topEnv,
		Inputs:           inputChunks{objPaths, inputTail},
		KV:               KV{P: pkAR, PC: pcLightRed, ShowOut: true},
		Outputs:          []VFS{archivePath},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: targetProperties,
		DepRefs:          depRefs,
		usesResources:    []string{resourcePatternYMakePython3, resourcePatternClangTool + instance.Platform.ClangVer},
	}

	return emit.Emit(n)
}
