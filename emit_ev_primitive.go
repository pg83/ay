package main

var evEventlogIncludePath = evEventlogIncludeVFS.String()

var evExtraProtobufHeaders = []VFS{
	Source(pbRuntimeBase + "google/protobuf/io/printer.h"),
	Source(pbRuntimeBase + "google/protobuf/io/zero_copy_sink.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/hash.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/stringpiece.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/strutil.h"),
}

var evAbseilCleanupHeaders = []VFS{
	Intern("$(S)/contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h"),
	Intern("$(S)/contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h"),
}

func evWitnessExtras(evRelPath string, evPbCC VFS) []includeDirective {
	out := make([]includeDirective, 0,
		3+len(pbDescriptorImporterHeaders)+len(evExtraProtobufHeaders)+len(evAbseilCleanupHeaders))
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.Rel())})
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(pbDescriptorVFS.Rel())})
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(evRelPath)})
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(evPbCC.Rel())})

	for _, v := range pbDescriptorImporterHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: internStr(v.Rel())})
	}

	for _, v := range evExtraProtobufHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: internStr(v.Rel())})
	}

	for _, v := range evAbseilCleanupHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: internStr(v.Rel())})
	}

	return out
}

func EmitEV(
	instance ModuleInstance,
	evRelPath string,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	event2cppLDRef NodeRef,
	cppStyleguideBinary VFS,
	protocBinary VFS,
	event2cppBinary VFS,
	moduleTag *string,
	transitiveImports []VFS,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path

	evCC := Build(evRelPath + ".pb.cc")
	evH := Build(evRelPath + ".pb.h")
	srcVFS := Source(evRelPath)

	cmdArgs := []ANY{
		internAny(instance.Platform.Tools.Python3),
		internAny(pbWrapperPath),
		anyOutputs,
		vfsAny(evCC),
		vfsAny(evH),
		any2,
		vfsAny(protocBinary),
		anyI2,
		anyIS2,
		anyIB2,
		anyIS3,
		anyISContribLibsProtobufSrc,
		anyIB2,
		anyISContribLibsProtobufSrc,
		anyCppOutB,
		anyCppStyleguideOutB,
		internAny("--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary.String()),
		internAny(evRelPath),
		internAny("--plugin=protoc-gen-event2cpp=" + event2cppBinary.String()),
		anyEvent2cppOutB,
		internAny("-I=" + evEventlogIncludePath),
	}

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	inputs := []VFS{
		cppStyleguideBinary,
		protocBinary,
		event2cppBinary,
		pbWrapperVFS,
		srcVFS,
	}

	inputs = append(inputs, transitiveImports...)

	targetProps := TargetProperties{ModuleDir: moduleDir}

	if moduleTag != nil {
		targetProps.ModuleTag = *moduleTag
	}

	var depRefs []NodeRef
	var foreignDepRefs []NodeRef

	{
		var toolRefs []NodeRef

		if cppStyleguideLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, cppStyleguideLDRef)
		}

		if protocLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, protocLDRef)
		}

		if event2cppLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, event2cppLDRef)
		}

		if len(toolRefs) > 0 {
			depRefs = append([]NodeRef(nil), toolRefs...)
			foreignDepRefs = toolRefs
		}
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(S)",
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		Outputs:          []VFS{evCC, evH},
		KV:               KV{P: pkEV, PC: pcYellow},
		Tags:             instance.Platform.Tags,
		TargetProperties: targetProps,
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform))
}
