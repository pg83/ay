package main

func EmitEN(
	instance ModuleInstance,
	headerInput VFS,
	headerRel string,
	moduleTag STR,
	withHeader bool,
	enumParserLD NodeRef,
	enumParserBin VFS,
	depENRefs []NodeRef,
	headerIncludeClosure []VFS,
	emit Emitter,
) (NodeRef, []VFS) {
	serializedCPPVFS := Build(instance.Path.Rel() + "/" + headerRel + "_serialized.cpp")

	cmdArgs := []STR{
		(enumParserBin).str(),
		(headerInput).str(),
		argIncludePath.str(),
		internStr(headerInput.Rel()),
		argOutput.str(),
		(serializedCPPVFS).str(),
	}
	outputs := []VFS{serializedCPPVFS}

	if withHeader {
		serializedHVFS := Build(instance.Path.Rel() + "/" + headerRel + "_serialized.h")
		cmdArgs = append(cmdArgs, argHeader.str(), (serializedHVFS).str())
		outputs = append(outputs, serializedHVFS)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	depRefs := make([]NodeRef, 0, len(depENRefs)+1)

	if enumParserLD != (NodeRef(0)) {
		depRefs = append(depRefs, enumParserLD)
	}

	depRefs = append(depRefs, depENRefs...)

	var foreignDepRefs []NodeRef

	if enumParserLD != (NodeRef(0)) {
		foreignDepRefs = []NodeRef{enumParserLD}
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputChunks{{enumParserBin, headerInput}, headerIncludeClosure},
		KV:               KV{P: pkEN, PC: pcYellow},
		Outputs:          outputs,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	if moduleTag != 0 {
		node.TargetProperties.ModuleTag = moduleTag
	}

	return emit.Emit(node), outputs
}
