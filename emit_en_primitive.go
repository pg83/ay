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

// enumParserBinary is the canonical invocation path for the
// enum_parser host binary. Used in cmd_args[0] and inputs.
var (
	enumParserBinaryVFS  = Build("tools/enum_parser/enum_parser/enum_parser")
	enumParserBinaryPath = enumParserBinaryVFS.String()
)

// EmitEN emits one EN node for a GENERATE_ENUM_SERIALIZATION(*) invocation.
// headerSrc.Rel drives serialized output paths and --include-path.
// withHeader adds --header + .h output. enumParserLD may be zero when the
// host walk failed. depENRefs/depENOutputs wire cross-EN serialized-header
// deps; headerIncludeClosure is the include-scanner result for headerSrc.
// Returns NodeRef and output paths (1 or 2).
func EmitEN(
	instance ModuleInstance,
	headerSrc VFS,
	withHeader bool,
	enumParserLD NodeRef,
	enumParserBin VFS,
	depENRefs []NodeRef,
	depENOutputs []VFS,
	headerIncludeClosure []VFS,
	emit Emitter,
) (NodeRef, []VFS) {
	// The output path mirrors:
	//   $(B)/<headerSrc.Rel>_serialized.cpp[ .h with _WITH_HEADER]
	serializedCPPVFS := Build(headerSrc.Rel + "_serialized.cpp")

	cmdArgs := []string{
		enumParserBin.String(),
		headerSrc.String(),
		"--include-path",
		headerSrc.Rel,
		"--output",
		serializedCPPVFS.String(),
	}

	outputs := []VFS{serializedCPPVFS}

	if withHeader {
		serializedHVFS := Build(headerSrc.Rel + "_serialized.h")
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
	inputs = append(inputs, headerSrc)
	inputs = append(inputs, headerIncludeClosure...)

	depRefs := make([]NodeRef, 0, len(depENRefs)+1)
	depRefs = append(depRefs, depENRefs...)

	if enumParserLD != (NodeRef{}) {
		depRefs = append(depRefs, enumParserLD)
	}

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
		KV: map[string]string{
			"p":  "EN",
			"pc": "yellow",
		},
		Outputs:      outputs,
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
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

	return emit.Emit(node), outputs
}
