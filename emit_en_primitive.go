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
		serializedHVFS := Build(instance.Path + "/" + headerRel + "_serialized.h")
		cmdArgs = append(cmdArgs, argHeader.str(), (serializedHVFS).str())
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
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		KV:               KV{P: pkEN, PC: pcYellow},
		Outputs:          outputs,
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	if moduleTag != nil {
		node.TargetProperties.ModuleTag = *moduleTag
	}

	return emit.Emit(node), outputs
}
