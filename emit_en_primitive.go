package main

func EmitEN(
	instance ModuleInstance,
	headerInput VFS,
	headerRel string,
	moduleTag *string,
	withHeader bool,
	enumParserLD NodeRef,
	enumParserBin VFS,
	depENRefs []NodeRef,
	headerIncludeClosure []VFS,
	emit Emitter,
) (NodeRef, []VFS) {
	serializedCPPVFS := Build(instance.Path + "/" + headerRel + "_serialized.cpp")

	cmdArgs := []string{
		enumParserBin.String(),
		headerInput.String(),
		"--include-path",
		headerInput.Rel(),
		"--output",
		serializedCPPVFS.String(),
	}
	outputs := []VFS{serializedCPPVFS}

	if withHeader {
		serializedHVFS := Build(instance.Path + "/" + headerRel + "_serialized.h")
		cmdArgs = append(cmdArgs, "--header", serializedHVFS.String())
		outputs = append(outputs, serializedHVFS)
	}

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	inputs := make([]VFS, 0, 2+len(headerIncludeClosure))
	inputs = append(inputs, enumParserBin)
	inputs = append(inputs, headerInput)
	inputs = append(inputs, headerIncludeClosure...)

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
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: "EN", PC: "yellow"},
		Outputs:          outputs,
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		Sandboxing:       true,
		Tags:             instance.Platform.Tags,
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	if moduleTag != nil {
		node.TargetProperties.ModuleTag = *moduleTag
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform)), outputs
}
