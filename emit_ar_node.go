package main

func emitARNode(
	instance ModuleInstance,
	archivePath VFS,
	tag *string,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	arPluginPath *VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	scriptVFS := buildScriptsLinkLibPy

	cmdEnv := hostP.ToolEnv()
	arTool, arType, arFormat := instance.Platform.ArchiverArgs()

	cmdArgs := []ANY{
		internAny(instance.Platform.Tools.Python3),
		vfsAny(scriptVFS),
		internAny(arTool),
		internAny(arType),
		internAny(arFormat),
		anyB,
		anyNone,
		any2,
	}

	if arPluginPath != nil {
		cmdArgs = append(cmdArgs, anyPlugin, vfsAny(*arPluginPath))
	}

	cmdArgs = append(cmdArgs, any2, vfsAny(archivePath))

	for _, p := range objPaths {
		cmdArgs = append(cmdArgs, vfsAny(p))
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

	tags := instance.Platform.Tags

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
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		Tags:             tags,
		TargetProperties: targetProperties,
		DepRefs:          depRefs,
	}

	return emit.Emit(bindNodePlatform(withResources(n, resourcePatternYMakePython3, resourcePatternClangTool), instance.Platform))
}
