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
	if len(protoInclude) == 0 {
		return nil
	}

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

func emitLibraryEvSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()
	evSource := resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)
	evRelPath := evSource.rel()
	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
	event2cppLDRef, event2cppBinary := ctx.tool(argToolsEvent2cpp)
	evImports := walkClosureTail(ctx.scannerFor(instance), evSource, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()).ScanCfg)

	evRef := emitEV(
		instance, evRelPath,
		cppStyleguideLDRef, protocLDRef, event2cppLDRef,
		cppStyleguideBinary, protocBinary, event2cppBinary,
		0, evImports, in.ProtoInclude,
		!protoTransitiveHeadersEnabled(d),
		d.tc, ctx.emit)

	evH := build(evRelPath, ".pb.h")
	evPbCC := build(evRelPath, ".pb.cc")
	directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, "")
	evExtras := evWitnessExtras(evRelPath, evPbCC)
	evHParsed := make([]IncludeDirective, 0, len(directImports)+len(protobufRuntimeDirectives)+len(evExtras))

	evHParsed = append(evHParsed, directImports...)
	evHParsed = append(evHParsed, protobufRuntimeDirectives...)
	evHParsed = append(evHParsed, evExtras...)

	reg := ctx.codegenFor(instance)

	reg.register(&GeneratedFileInfo{
		ProducerKvP:    pkEV,
		OutputPath:     evH,
		ProducerRef:    evRef,
		GeneratorRefs:  []NodeRef{event2cppLDRef},
		ParsedIncludes: evHParsed,
	})

	evCCParsed := make([]IncludeDirective, 0, 1+len(protobufRuntimeDirectives))

	evCCParsed = append(evCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(evH.rel())})
	evCCParsed = append(evCCParsed, protobufRuntimeDirectives...)
	reg.register(&GeneratedFileInfo{
		ProducerKvP:    pkEV,
		OutputPath:     evPbCC,
		ProducerRef:    evRef,
		GeneratorRefs:  []NodeRef{event2cppLDRef},
		ParsedIncludes: evCCParsed,
	})

	evPbCCSuffix := srcRel + ".pb.cc"
	ccIn := in

	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), evPbCC, in.ScanCfg)

	filtered := make([]VFS, 0, len(ccIn.IncludeInputs))

	for _, v := range ccIn.IncludeInputs {
		if v == evH {
			continue
		}

		filtered = append(filtered, v)
	}

	ccIn.IncludeInputs = filtered

	wireFormatVFS := source(pbRuntimeBase, "google/protobuf/wire_format.h")

	ccIn.IncludeInputs = append(ccIn.IncludeInputs, wireFormatVFS)
	ccIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, ccIn.IncludeInputs, evRef)

	ref, outPath, _ := emitCC(instance, internStr(evPbCCSuffix), evPbCC, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}
