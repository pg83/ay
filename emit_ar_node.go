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
	tag *string,
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

	inputs := make([]VFS, 0, len(objPaths)+2)
	inputs = append(inputs, objPaths...)
	inputs = append(inputs, scriptVFS)

	if arPluginPath != nil {
		inputs = append(inputs, *arPluginPath)
	}

	topEnv := hostP.ToolEnv()

	targetProperties := TargetProperties{ModuleDir: instance.Path, ModuleLang: "cpp", ModuleType: "lib"}

	if instance.Language == LangPy {
		targetProperties.ModuleLang = "py3"
	}

	if tag != nil {
		targetProperties.ModuleTag = *tag
	}

	depRefs := make([]NodeRef, 0, len(objRefs)+len(peerArchiveRefs))
	depRefs = append(depRefs, objRefs...)
	depRefs = append(depRefs, peerArchiveRefs...)

	n := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     cmdEnv,
			},
		},
		Env:              topEnv,
		Inputs:           inputs,
		KV:               KV{P: pkAR, PC: pcLightRed, ShowOut: "yes"},
		Outputs:          []VFS{archivePath},
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		TargetProperties: targetProperties,
		DepRefs:          depRefs,
	}

	return emit.Emit(bindNodePlatform(withResources(n, resourcePatternYMakePython3, resourcePatternClangTool+instance.Platform.ClangVer), instance.Platform))
}
