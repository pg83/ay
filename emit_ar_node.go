package main

// Shared implementation behind EmitAR / EmitARNamed / EmitARNamedTagged /
// EmitARGlobalNamedTagged. peerArchiveRefs go into DepRefs only (NOT
// cmd_args/inputs): ar(1) archives .o files; peer archives are link-time
// inputs for LD. objPaths (caller/declaration order) goes into cmd_args and
// `inputs`, then the link script and optional ar plugin, then the
// memberInputs union deduped against prior inputs. Node-input order is
// normalized away, so `inputs` is not sorted.
// peerArchiveRefs is nil in production (reference graph carries
// zero AR-on-AR deps); parameter retained for tests.
func emitARNode(
	instance ModuleInstance,
	archivePath VFS,
	tag *string,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	memberInputs []VFS,
	arPluginPath *VFS,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	scriptVFS := Source("build/scripts/link_lib.py")

	cmdEnv := hostP.ToolEnv()
	arTool, arType, arFormat := instance.Platform.ArchiverArgs()

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		scriptVFS.String(),
		arTool,
		arType,
		arFormat,
		"$(B)",
		"None",
		"--",
	}
	if arPluginPath != nil {
		cmdArgs = append(cmdArgs, "--plugin", arPluginPath.String())
	}
	cmdArgs = append(cmdArgs, "--", archivePath.String())

	for _, p := range objPaths {
		cmdArgs = append(cmdArgs, p.String())
	}

	inputs := make([]VFS, 0, len(objPaths)+2+len(memberInputs))
	inputs = append(inputs, objPaths...)
	inputs = append(inputs, scriptVFS)
	if arPluginPath != nil {
		inputs = append(inputs, *arPluginPath)
	}
	objSet := map[VFS]struct{}{}
	for _, v := range inputs {
		objSet[v] = struct{}{}
	}

	for _, pV := range memberInputs {
		if _, dup := objSet[pV]; dup {
			continue
		}
		if pV.IsBuild() && isBuildRootCodegenProductRel(pV.Rel) {
			continue
		}

		objSet[pV] = struct{}{}
		inputs = append(inputs, pV)
	}

	topEnv := hostP.ToolEnv()

	targetProperties := map[string]string{
		"module_dir":  instance.Path,
		"module_lang": "cpp",
		"module_type": "lib",
	}

	if instance.Language == LangPy {
		targetProperties["module_lang"] = "py3"
	}

	if tag != nil {
		targetProperties["module_tag"] = *tag
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
		Env:    topEnv,
		Inputs: inputs,
		KV: map[string]interface{}{
			"p":        "AR",
			"pc":       "light-red",
			"show_out": "yes",
		},
		Outputs:  []VFS{archivePath},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags:             tags,
		TargetProperties: targetProperties,
		DepRefs:          depRefs,
	}

	return emit.Emit(bindNodePlatform(n, instance.Platform))
}
