package main

var (
	evEventlogIncludePath     = evEventlogIncludeVFS.string()
	evExtraProtobufDirectives = quotedDirectives(evExtraProtobufHeaders)
	evAbseilCleanupDirectives = quotedDirectives(evAbseilCleanupHeaders)
	evKV                      = KV{P: pkEV, PC: pcYellow}
)

var evExtraProtobufHeaders = []VFS{
	source(pbRuntimeBase, "google/protobuf/io/printer.h"),
	source(pbRuntimeBase, "google/protobuf/io/zero_copy_sink.h"),
	source(pbRuntimeBase, "google/protobuf/stubs/hash.h"),
	source(pbRuntimeBase, "google/protobuf/stubs/stringpiece.h"),
	source(pbRuntimeBase, "google/protobuf/stubs/strutil.h"),
}

var evAbseilCleanupHeaders = []VFS{
	source("contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h"),
	source("contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h"),
}

func evWitnessExtras(evRelPath string) []IncludeDirective {
	out := make([]IncludeDirective, 0,
		3+len(pbDescriptorImporterDirectives)+len(evExtraProtobufDirectives)+len(evAbseilCleanupDirectives))

	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})
	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(pbDescriptorVFS.rel())})
	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(evRelPath)})
	out = append(out, pbDescriptorImporterDirectives...)
	out = append(out, evExtraProtobufDirectives...)
	out = append(out, evAbseilCleanupDirectives...)

	return out
}

func (e *EmitContext) emitLibraryEvSource(src STR) {
	ctx, instance, d := e.ctx, e.instance, e.d

	if d.unit.Tag == unitTagPy3Proto {
		throwFmt("gen: py-addressed PROTO_LIBRARY %s with .ev sources is not modelled", instance.Path.rel())
	}

	event2cppLDRef, event2cppBinary := ctx.tool(argToolsEvent2cpp)
	evRelPath := protoSourceRelPath(ctx.fs, instance, d, src.string())
	directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, protoCPPOutRoot(d))
	evExtras := evWitnessExtras(evRelPath)
	evHParsed := concat(directImports, protobufRuntimeDirectives, evExtras)

	e.emitCppProtoFamilySource(src, &ProtoSpec{
		kv:          &evKV,
		ccFirstOuts: true,
		optsTail: []STR{
			internV("--plugin=protoc-gen-event2cpp=", event2cppBinary.string()),
			argEvent2cppOutB.str(),
			internV("-I=", evEventlogIncludePath),
		},
		toolLDRef:  event2cppLDRef,
		toolBinary: event2cppBinary,
		genRefs:    []NodeRef{event2cppLDRef},
		genHParsed: evHParsed,
		genCCExtras: []IncludeDirective{
			{kind: includeQuoted, target: internStr(source(pbRuntimeBase, "google/protobuf/wire_format.h").rel())},
		},
		hLeaves: []VFS{build(evRelPath, ".pb.cc")},
	})
}
