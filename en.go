package main

// en.go — emitter for EN (enum serialization) nodes.
//
// EN nodes are emitted for GENERATE_ENUM_SERIALIZATION,
// GENERATE_ENUM_SERIALIZATION_WITH_HEADER, and
// GENERATE_ENUM_SERIALIZATION_NOUTF macro invocations. Each
// declaration produces one EN node that runs enum_parser over
// the named header, producing a _serialized.cpp and (with
// _WITH_HEADER) a _serialized.h.
//
// PR-M3-D scope: emit EN nodes with the correct cmd_args shape,
// sandboxing, foreign_deps.tool, and deps on the enum_parser
// host LD. Cross-EN header-inclusion deps (where one EN's
// serialized .h is listed in another EN's inputs/deps) are
// tracked via genCtx.enOutputs and wired at emit time.
//
// cmd_args shape:
//   [enumParserBinary, $(SOURCE_ROOT)/<path>/<header>.h,
//    --include-path, <path>/<header>.h,
//    --output, $(BUILD_ROOT)/<path>/<header>.h_serialized.cpp
//    [--header, $(BUILD_ROOT)/<path>/<header>.h_serialized.h]]
//
// inputs shape:
//   [dep-EN-outputs..., enumParserBinary,
//    $(SOURCE_ROOT)/<path>/<header>.h, ...headerIncludeClosure]

// enumParserBinaryPath is the canonical invocation path for the
// enum_parser host binary. Used in cmd_args[0] and inputs.
const enumParserBinaryPath = "$(BUILD_ROOT)/tools/enum_parser/enum_parser/enum_parser"

// EmitEN emits one EN node for a GENERATE_ENUM_SERIALIZATION(*)
// invocation.
//
//   - instance: the module that declared the macro.
//   - headerRel: the header path relative to instance.Path
//     (e.g. "stats_enums.h" or "config/config.h").
//   - withHeader: true when the macro variant is
//     _WITH_HEADER (adds --header + produces .h output).
//   - enumParserLD: NodeRef of the tools/enum_parser/enum_parser
//     host LD node; may be zero when the host walk failed.
//   - enumParserBin: $(BUILD_ROOT)-rooted path to the binary
//     (falls back to enumParserBinaryPath when walk succeeded but
//     the path is the same canonical form).
//   - depENRefs: NodeRefs of EN nodes whose outputs are inputs to
//     this EN node (cross-EN serialized header deps). May be empty.
//   - depENOutputs: the output paths of those dep EN nodes, in
//     the same order as depENRefs. These become the leading entries
//     in this node's inputs slice.
//   - headerIncludeClosure: SOURCE_ROOT-absolute paths of headers
//     transitively included by headerRel (from the include scanner).
//
// Returns the emitted NodeRef and the list of output paths (1 or 2).
func EmitEN(
	instance ModuleInstance,
	headerRel string,
	withHeader bool,
	enumParserLD NodeRef,
	enumParserBin string,
	depENRefs []NodeRef,
	depENOutputs []string,
	headerIncludeClosure []string,
	emit Emitter,
) (NodeRef, []string) {
	// Resolve the module-dir for this header. The header path may include
	// a subdirectory component (e.g. "config/config.h" in devtools/ymake).
	// The output path mirrors: $(BUILD_ROOT)/<instance.Path>/<headerRel>_serialized.cpp.
	headerSrc := "$(SOURCE_ROOT)/" + instance.Path + "/" + headerRel
	includePath := instance.Path + "/" + headerRel
	serializedCPP := "$(BUILD_ROOT)/" + instance.Path + "/" + headerRel + "_serialized.cpp"

	cmdArgs := []string{
		enumParserBin,
		headerSrc,
		"--include-path",
		includePath,
		"--output",
		serializedCPP,
	}

	outputs := []string{serializedCPP}

	if withHeader {
		serializedH := "$(BUILD_ROOT)/" + instance.Path + "/" + headerRel + "_serialized.h"
		cmdArgs = append(cmdArgs, "--header", serializedH)
		outputs = append(outputs, serializedH)
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	// inputs: dep-EN outputs (leading), then enum_parser binary,
	// then the source header, then its transitive include closure.
	inputs := make([]string, 0, len(depENOutputs)+2+len(headerIncludeClosure))
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
		Inputs: ToVFSSlice(inputs),
		KV: map[string]string{
			"p":  "EN",
			"pc": "yellow",
		},
		Outputs:      ToVFSSlice(outputs),
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Sandboxing: true,
		Tags: instance.Platform.Tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		DepRefs:        depRefs,
		ForeignDepRefs: foreignDepRefs,
	}

	return emit.Emit(node), outputs
}
