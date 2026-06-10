package main

import (
	"sync"
)

var (
	evEventlogIncludePath = evEventlogIncludeVFS.String()
	// Lazy (not init-time like protobufRuntimeDirectives): these lists are only
	// reached for .ev sources, so eager interning would grow the intern table on
	// targets that never build them.
	evExtraProtobufDirectives = sync.OnceValue(func() []includeDirective { return quotedDirectives(evExtraProtobufHeaders) })
	evAbseilCleanupDirectives = sync.OnceValue(func() []includeDirective { return quotedDirectives(evAbseilCleanupHeaders) })
)

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
	evExtraProtobuf := evExtraProtobufDirectives()
	evAbseilCleanup := evAbseilCleanupDirectives()
	out := make([]includeDirective, 0,
		3+len(pbDescriptorImporterDirectives)+len(evExtraProtobuf)+len(evAbseilCleanup))
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.Rel())})
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(pbDescriptorVFS.Rel())})
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(evRelPath)})
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(evPbCC.Rel())})
	out = append(out, pbDescriptorImporterDirectives...)
	out = append(out, evExtraProtobuf...)
	out = append(out, evAbseilCleanup...)

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
	moduleTag STR,
	transitiveImports []VFS,
	tc moduleToolchain,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path.Rel()

	evCC := Build(evRelPath + ".pb.cc")
	evH := Build(evRelPath + ".pb.h")
	srcVFS := Source(evRelPath)

	cmdArgs := []STR{
		tc.Python3,
		internStr(pbWrapperPath),
		argOutputs.str(),
		(evCC).str(),
		(evH).str(),
		arg2.str(),
		(protocBinary).str(),
		argI2.str(),
		argIS2.str(),
		argIB2.str(),
		argIS3.str(),
		argISContribLibsProtobufSrc.str(),
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		argCppOutB.str(),
		argCppStyleguideOutB.str(),
		internStr("--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary.String()),
		internStr(evRelPath),
		internStr("--plugin=protoc-gen-event2cpp=" + event2cppBinary.String()),
		argEvent2cppOutB.str(),
		internStr("-I=" + evEventlogIncludePath),
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
		Inputs:           inputChunks{inputs, transitiveImports},
		Outputs:          []VFS{evCC, evH},
		KV:               KV{P: pkEV, PC: pcYellow},
		TargetProperties: targetProps,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
		usesResources:    []string{resourcePatternYMakePython3},
	}

	return emit.Emit(node)
}
