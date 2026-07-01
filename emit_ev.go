package main

import (
	"slices"
	"sync"
)

var (
	evEventlogIncludePath     = evEventlogIncludeVFS.string()
	evExtraProtobufDirectives = sync.OnceValue(func() []IncludeDirective { return quotedDirectives(evExtraProtobufHeaders) })
	evAbseilCleanupDirectives = sync.OnceValue(func() []IncludeDirective { return quotedDirectives(evAbseilCleanupHeaders) })
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
	intern("$(S)/contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h"),
	intern("$(S)/contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h"),
}

var evProtocConstHead = []STR{
	argI2.str(),
	argIS2.str(),
	argIB2.str(),
	argIS3.str(),
	argISContribLibsProtobufSrc.str(),
}

var evProtocConstTail = []STR{
	argIB2.str(),
	argISContribLibsProtobufSrc.str(),
	argCppOutB.str(),
	argCppStyleguideOutB.str(),
}

var evProtocConstTailLite = []STR{
	argIB2.str(),
	argISContribLibsProtobufSrc.str(),
	argCppOutProtoHB.str(),
	argCppStyleguideOutB.str(),
}

func evPeerProtoIncludes(protoInclude []VFS) []STR {
	out := make([]STR, 0, len(protoInclude))

	for _, p := range protoInclude {
		token := internV("-I=", p.string())

		if slices.Contains(evProtocConstHead, token) || slices.Contains(out, token) {
			continue
		}

		out = append(out, token)
	}

	return out
}

func evWitnessExtras(evRelPath string) []IncludeDirective {
	evExtraProtobuf := evExtraProtobufDirectives()
	evAbseilCleanup := evAbseilCleanupDirectives()

	out := make([]IncludeDirective, 0,
		3+len(pbDescriptorImporterDirectives)+len(evExtraProtobuf)+len(evAbseilCleanup))

	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})
	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(pbDescriptorVFS.rel())})
	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(evRelPath)})
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
	protoInclude []VFS,
	liteHeaders bool,
	tc ModuleToolchain,
	emit *StreamingEmitter,
) NodeRef {
	na := emit.nodeArenas()

	evOpts := na.strList(internV("--plugin=protoc-gen-event2cpp=", event2cppBinary.string()),
		argEvent2cppOutB.str(),
		internV("-I=", evEventlogIncludePath))

	return emitProtoWrapperPBNode(instance, evRelPath, &evKV,
		cppStyleguideLDRef, protocLDRef, event2cppLDRef,
		cppStyleguideBinary, protocBinary, event2cppBinary,
		evOpts, moduleTag, transitiveImports, protoInclude, liteHeaders, tc, emit)
}

func emitProtoWrapperPBNode(
	instance ModuleInstance,
	relPath string,
	kv *KV,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	pluginLDRef NodeRef,
	cppStyleguideBinary VFS,
	protocBinary VFS,
	pluginBinary VFS,
	pluginOpts []STR,
	moduleTag STR,
	transitiveImports []VFS,
	protoInclude []VFS,
	liteHeaders bool,
	tc ModuleToolchain,
	emit *StreamingEmitter,
) NodeRef {
	na := emit.nodeArenas()
	genCC := build(relPath, ".pb.cc")
	genH := build(relPath, ".pb.h")
	srcVFS := source(relPath)
	peerIncludes := evPeerProtoIncludes(protoInclude)
	protocTail := evProtocConstTail

	if liteHeaders {
		protocTail = evProtocConstTailLite
	}

	tail := na.strList(append([]STR{
		internV("--plugin=protoc-gen-cpp_styleguide=", cppStyleguideBinary.string()),
		internStr(relPath),
	}, pluginOpts...)...)

	cmdArgs := na.chunkList(na.strList(tc.Python3,
		internStr(pbWrapperPath),
		argOutputs.str(),
		(genCC).str(),
		(genH).str(),
		arg2.str(),
		(protocBinary).str()), evProtocConstHead, peerIncludes, protocTail, tail)

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	inputs := []VFS{
		cppStyleguideBinary,
		protocBinary,
		pluginBinary,
		pbWrapperVFS,
		srcVFS,
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: strS,
			Env: env}),
		Env:            env,
		Inputs:         na.inputList(inputs, transitiveImports),
		Outputs:        na.vfsList(genCC, genH),
		KV:             kv,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(cppStyleguideLDRef, protocLDRef, pluginLDRef),
		Resources:      usesPython3,
	}

	return emit.emit(node)
}

func (e *EmitContext) emitLibraryEvSource(src STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	evSource := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	evRelPath := evSource.rel()
	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
	event2cppLDRef, event2cppBinary := ctx.tool(argToolsEvent2cpp)
	evImports := walkClosureTail(e.scanner, evSource, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()))

	evRef := emitEV(
		instance, evRelPath,
		cppStyleguideLDRef, protocLDRef, event2cppLDRef,
		cppStyleguideBinary, protocBinary, event2cppBinary,
		0, evImports, d.cc.ProtoInclude,
		!protoTransitiveHeadersEnabled(d),
		d.tc, ctx.emit)

	evH := build(evRelPath, ".pb.h")
	evPbCC := build(evRelPath, ".pb.cc")
	directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, "")
	evExtras := evWitnessExtras(evRelPath)
	evHParsed := make([]IncludeDirective, 0, len(directImports)+len(protobufRuntimeDirectives)+len(evExtras))

	evHParsed = append(evHParsed, directImports...)
	evHParsed = append(evHParsed, protobufRuntimeDirectives...)
	evHParsed = append(evHParsed, evExtras...)

	reg := e.codegen

	var psc []ARG
	if p := d.perSrcCFlagsFor(src); p != nil {
		psc = *p
	}

	reg.register(&GeneratedFileInfo{
		OutputPath:     evH,
		ProducerRef:    evRef,
		GeneratorRefs:  []NodeRef{event2cppLDRef},
		ParsedIncludes: evHParsed,
		ClosureLeaves:  []VFS{evPbCC},
	})

	evCCParsed := append(append([]IncludeDirective(nil), evHParsed...),
		IncludeDirective{kind: includeQuoted, target: internStr(source(pbRuntimeBase, "google/protobuf/wire_format.h").rel())})

	reg.register(&GeneratedFileInfo{
		OutputPath:     evPbCC,
		ProducerRef:    evRef,
		GeneratorRefs:  []NodeRef{event2cppLDRef},
		ParsedIncludes: evCCParsed,
		Compile:        &CompileSpec{FlatOutput: d.flatSrc(src), CFlags: psc},
	})

	meta := d.srcMetaOf(src)
	meta.Generated = true
	e.enqueueSrc(evPbCC.str(), meta)
}
