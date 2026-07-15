package main

import "fmt"

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

var evGenCCExtras = []IncludeDirective{
	{kind: includeQuoted, target: includeTarget(internStr(source(pbRuntimeBase, "google/protobuf/wire_format.h").relString()).any())},
}

func evWitnessExtrasBound() int {
	return 3 + len(pbDescriptorImporterDirectives) + len(evExtraProtobufDirectives) + len(evAbseilCleanupDirectives)
}

func appendEvWitnessExtras(dst []IncludeDirective, evRel STR) []IncludeDirective {
	dst = append(dst, IncludeDirective{kind: includeQuoted, target: includeTarget(pbWrapperVFS.rel().any())})
	dst = append(dst, IncludeDirective{kind: includeQuoted, target: includeTarget(pbDescriptorVFS.rel().any())})
	dst = append(dst, IncludeDirective{kind: includeQuoted, target: includeTarget(evRel.any())})
	dst = append(dst, pbDescriptorImporterDirectives...)
	dst = append(dst, evExtraProtobufDirectives...)
	dst = append(dst, evAbseilCleanupDirectives...)

	return dst
}

func (e *EmitContext) emitLibraryEvSource(meta SrcMeta) {
	src := meta.Source
	ctx, instance, d := e.ctx, e.instance, e.d

	if d.unit.Tag == unitTagPy3Proto {
		ctx.onWarn(Warn{Kind: WarnUnsupportedSource, Message: fmt.Sprintf("py-addressed PROTO_LIBRARY %s with .ev sources is not modelled; source skipped", instance.Path.relString())})

		return
	}

	event2cppLDRef, event2cppBinary := ctx.tool(argToolsEvent2cpp)
	evRel := e.protoSourceRel(e.moduleSourceName(src))
	evRelPath := evRel.string()
	directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, protoCPPOutRoot(d), e.dirScratch[:0])

	e.dirScratch = directImports

	evHParsed := ctx.na.dirs.alloc(len(directImports) + len(protobufRuntimeDirectives) + evWitnessExtrasBound())[:0]

	evHParsed = append(evHParsed, directImports...)
	evHParsed = append(evHParsed, protobufRuntimeDirectives...)
	evHParsed = appendEvWitnessExtras(evHParsed, evRel)
	ctx.na.dirs.commit(len(evHParsed))
	evHParsed = evHParsed[:len(evHParsed):len(evHParsed)]

	e.emitCppProtoFamilySource(meta, &ProtoSpec{
		kv:          &evKV,
		ccFirstOuts: true,
		optsTail: ctx.na.anyList(
			internV("--plugin=protoc-gen-event2cpp=", event2cppBinary.prefix(), event2cppBinary.relString()).any(),
			argEvent2cppOutB.any(),
			internV("-I=", evEventlogIncludePath).any(),
		),
		toolLDRef:   event2cppLDRef,
		toolBinary:  event2cppBinary,
		genRefs:     ctx.na.refList(event2cppLDRef),
		genHParsed:  evHParsed,
		genCCExtras: evGenCCExtras,
		hLeaves:     ctx.na.vfsList(build(evRelPath, ".pb.cc")),
	})
}
