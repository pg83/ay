package main

import (
	"sync"
)

var (
	evEventlogIncludePath = evEventlogIncludeVFS.string()
	// Lazy (not init-time like protobufRuntimeDirectives): these lists are only
	// reached for .ev sources, so eager interning would grow the intern table on
	// targets that never build them.
	evExtraProtobufDirectives = sync.OnceValue(func() []IncludeDirective { return quotedDirectives(evExtraProtobufHeaders) })
	evAbseilCleanupDirectives = sync.OnceValue(func() []IncludeDirective { return quotedDirectives(evAbseilCleanupHeaders) })
)

var evExtraProtobufHeaders = []VFS{
	source(pbRuntimeBase + "google/protobuf/io/printer.h"),
	source(pbRuntimeBase + "google/protobuf/io/zero_copy_sink.h"),
	source(pbRuntimeBase + "google/protobuf/stubs/hash.h"),
	source(pbRuntimeBase + "google/protobuf/stubs/stringpiece.h"),
	source(pbRuntimeBase + "google/protobuf/stubs/strutil.h"),
}

var evAbseilCleanupHeaders = []VFS{
	intern("$(S)/contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h"),
	intern("$(S)/contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h"),
}

// evProtocConstArgs is the constant -I/--out span of every EV protoc command.
var evProtocConstArgs = []STR{
	argI2.str(),
	argIS2.str(),
	argIB2.str(),
	argIS3.str(),
	argISContribLibsProtobufSrc.str(),
	argIB2.str(),
	argISContribLibsProtobufSrc.str(),
	argCppOutB.str(),
	argCppStyleguideOutB.str(),
}

func evWitnessExtras(evRelPath string, evPbCC VFS) []IncludeDirective {
	evExtraProtobuf := evExtraProtobufDirectives()
	evAbseilCleanup := evAbseilCleanupDirectives()
	out := make([]IncludeDirective, 0,
		3+len(pbDescriptorImporterDirectives)+len(evExtraProtobuf)+len(evAbseilCleanup))
	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})
	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(pbDescriptorVFS.rel())})
	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(evRelPath)})
	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(evPbCC.rel())})
	out = append(out, pbDescriptorImporterDirectives...)
	out = append(out, evExtraProtobuf...)
	out = append(out, evAbseilCleanup...)

	return out
}

func emitEV(
	instance ModuleInstance,
	evRelPath string,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	event2cppLDRef NodeRef,
	cppStyleguideBinary VFS,
	protocBinary VFS,
	event2cppBinary VFS,
	moduleTag STR,
	transitiveImports []VFS,
	tc ModuleToolchain,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path.rel()

	evCC := build(evRelPath + ".pb.cc")
	evH := build(evRelPath + ".pb.h")
	srcVFS := source(evRelPath)

	cmdArgs := ArgChunks{
		{
			tc.Python3,
			internStr(pbWrapperPath),
			argOutputs.str(),
			(evCC).str(),
			(evH).str(),
			arg2.str(),
			(protocBinary).str(),
		},
		evProtocConstArgs,
		{
			internStr("--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary.string()),
			internStr(evRelPath),
			internStr("--plugin=protoc-gen-event2cpp=" + event2cppBinary.string()),
			argEvent2cppOutB.str(),
			internStr("-I=" + evEventlogIncludePath),
		},
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	inputs := []VFS{
		cppStyleguideBinary,
		protocBinary,
		event2cppBinary,
		pbWrapperVFS,
		srcVFS,
	}

	targetProps := TargetProperties{ModuleDir: moduleDir}

	if moduleTag != 0 {
		targetProps.ModuleTag = moduleTag
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
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     strS,
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           InputChunks{inputs, transitiveImports},
		Outputs:          []VFS{evCC, evH},
		KV:               KV{P: pkEV, PC: pcYellow},
		TargetProperties: targetProps,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
		usesResources:    usesPython3,
	}

	return emit.emit(node)
}
