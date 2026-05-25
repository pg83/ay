package main

// en.go — emitter for EN (enum serialization) nodes.
//
// One EN node per GENERATE_ENUM_SERIALIZATION[_WITH_HEADER|_NOUTF] macro
// invocation; runs enum_parser over the named header, producing
// _serialized.cpp and (with _WITH_HEADER) _serialized.h.
//
// cmd_args: [enumParserBin, $(S)/<hdr>, --include-path, <hdr>,
//            --output, $(B)/<hdr>_serialized.cpp,
//            [--header, $(B)/<hdr>_serialized.h]]
// inputs: [dep-EN-outputs..., enumParserBin, $(S)/<hdr>, ...includeClosure]

// EmitEN emits one EN node for a GENERATE_ENUM_SERIALIZATION(*) invocation.
// headerInput is the canonical source input VFS; headerRel is the raw macro
// argument that upstream reuses for serialized output layout under
// $(B)/<instance.Path>/...
//
// withHeader adds --header + .h output. enumParserLD may be zero when the
// host walk failed. depENRefs/depENOutputs wire cross-EN serialized-header
// deps; headerIncludeClosure is the include-scanner result for headerInput.
// Returns NodeRef and output paths (1 or 2).
func EmitEN(
	instance ModuleInstance,
	headerInput VFS,
	headerRel string,
	moduleTag *string,
	withHeader bool,
	enumParserLD NodeRef,
	enumParserBin VFS,
	depENRefs []NodeRef,
	depENOutputs []VFS,
	headerIncludeClosure []VFS,
	emit Emitter,
) (NodeRef, []VFS) {
	// The output path mirrors:
	//   $(B)/<instance.Path>/<headerRel>_serialized.cpp
	//   [ .h with _WITH_HEADER ]
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

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// inputs: dep-EN outputs (leading), then enum_parser binary,
	// then the source header, then its transitive include closure.
	inputs := make([]VFS, 0, len(depENOutputs)+2+len(headerIncludeClosure))
	inputs = append(inputs, depENOutputs...)
	inputs = append(inputs, enumParserBin)
	inputs = append(inputs, headerInput)
	inputs = append(inputs, headerIncludeClosure...)

	depRefs := make([]NodeRef, 0, len(depENRefs)+1)
	if enumParserLD != (NodeRef{}) {
		depRefs = append(depRefs, enumParserLD)
	}
	depRefs = append(depRefs, depENRefs...)

	var foreignDepRefs map[string][]NodeRef
	if enumParserLD != (NodeRef{}) {
		foreignDepRefs = map[string][]NodeRef{
			"tool": {enumParserLD},
		}
	}

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
			"p":  "EN",
			"pc": "yellow",
		},
		Outputs:  outputs,
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Sandboxing: true,
		Tags:       instance.Platform.Tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		DepRefs:        depRefs,
		ForeignDepRefs: foreignDepRefs,
	}
	if moduleTag != nil {
		node.TargetProperties["module_tag"] = *moduleTag
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform)), outputs
}
