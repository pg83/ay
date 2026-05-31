package main

func EmitJS(instance ModuleInstance, allName string, sources []string, closure []VFS, p *Platform, scripts scriptDeps, emit Emitter) (NodeRef, VFS) {
	joinSrcs := Intern("$(S)/build/scripts/gen_join_srcs.py")

	outVFS := Build(instance.Path + "/" + allName)
	platformID := instance.Platform.Target
	tags := []string{}
	if p != nil {
		platformID = p.Target
		tags = append(tags, p.Tags...)
	}
	statsPlatform := instance.Platform
	if p != nil {
		statsPlatform = p
	}

	cmdArgs := make([]string, 0, 4+len(sources))
	cmdArgs = append(cmdArgs,
		instance.Platform.Tools.Python3,
		joinSrcs.String(),
		outVFS.String(),
		"--ya-start-command-file",
	)

	for _, s := range sources {
		cmdArgs = append(cmdArgs, instance.Path+"/"+s)
	}

	cmdArgs = append(cmdArgs, "--ya-end-command-file")

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	inputs := make([]VFS, 0, 2+len(sources)+len(closure))
	inputs = append(inputs, scripts[joinSrcs]...)

	for _, s := range sources {
		inputs = append(inputs, Source(instance.Path+"/"+s))
	}

	inputs = append(inputs, closure...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]interface{}{
			"p":  "JS",
			"pc": "magenta",
		},
		Outputs:  []VFS{outVFS},
		Platform: string(platformID),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
	}

	return emit.Emit(bindNodePlatform(node, statsPlatform)), outVFS
}
