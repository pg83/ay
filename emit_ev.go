package main

import (
	"slices"
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

// CPP_EV_CMDLINE reuses _CPP_PROTO_CMDLINE_BASE (ymake.core.conf:614,618), so
// the EV protoc include span is split around the `${pre=-I=:_PROTO__INCLUDE}`
// peer block exactly as the PB command is: evProtocConstHead is everything up to
// and including the leading PROTOBUF_INCLUDE_PATH, evProtocConstTail is the
// trailing $ARCADIA_BUILD_ROOT / PROTOBUF_INCLUDE_PATH pair plus the --out args.
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

// evPeerProtoIncludes renders the transitive _PROTO__INCLUDE peer block for an
// EV protoc command: the single ordered proto-include set in encounter order.
// _PROTO__INCLUDE is a set, and evProtocConstHead already renders its base
// members ($(B), $(S), contrib/libs/protobuf/src), so a token already present in
// the const head (or earlier in this block) is skipped — only the extra peer
// namespace tokens (e.g. -I=$(S)/yt) survive.
func evPeerProtoIncludes(protoInclude []VFS) []STR {
	if len(protoInclude) == 0 {
		return nil
	}

	out := make([]STR, 0, len(protoInclude))

	for _, p := range protoInclude {
		token := internStr("-I=" + p.string())
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
	tc ModuleToolchain,
	emit Emitter,
) NodeRef {
	na := emit.nodeArenas()

	// CPP_EV_OPTS (proto.conf:52): the event2cpp plugin block appended after the
	// rootrel source — distinct from cfgproto's proto_config plugin block.
	evOpts := na.strList(internStr("--plugin=protoc-gen-event2cpp="+event2cppBinary.string()),
		argEvent2cppOutB.str(),
		internStr("-I="+evEventlogIncludePath))

	return emitProtoWrapperPBNode(instance, evRelPath, pkEV,
		cppStyleguideLDRef, protocLDRef, event2cppLDRef,
		cppStyleguideBinary, protocBinary, event2cppBinary,
		evOpts, moduleTag, transitiveImports, protoInclude, tc, emit)
}

// emitProtoWrapperPBNode emits the cpp_proto_wrapper.py → protoc producer node
// shared by CPP_EV_CMDLINE consumers (.ev and .cfgproto, ymake.core.conf:616 /
// proto.conf:481,496). The command is byte-identical through the styleguide
// base and the rootrel source; pluginOpts is the per-variant trailing plugin
// block (event2cpp for .ev, proto_config for .cfgproto) and pluginBinary /
// pluginLDRef the variant plugin tool. Outputs keep the source extension
// (CPP_EV_OUTS).
func emitProtoWrapperPBNode(
	instance ModuleInstance,
	relPath string,
	kvP ProcKind,
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
	tc ModuleToolchain,
	emit Emitter,
) NodeRef {
	na := emit.nodeArenas()

	moduleDir := instance.Path.rel()

	genCC := build(relPath + ".pb.cc")
	genH := build(relPath + ".pb.h")
	srcVFS := source(relPath)

	peerIncludes := evPeerProtoIncludes(protoInclude)

	tail := na.strList(append([]STR{
		internStr("--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary.string()),
		internStr(relPath),
	}, pluginOpts...)...)

	cmdArgs := na.chunkList(na.strList(tc.Python3,
		internStr(pbWrapperPath),
		argOutputs.str(),
		(genCC).str(),
		(genH).str(),
		arg2.str(),
		(protocBinary).str()), evProtocConstHead, peerIncludes, evProtocConstTail, tail)

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	inputs := []VFS{
		cppStyleguideBinary,
		protocBinary,
		pluginBinary,
		pbWrapperVFS,
		srcVFS,
	}

	targetProps := TargetProperties{ModuleDir: moduleDir}

	if moduleTag != 0 {
		targetProps.ModuleTag = moduleTag
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: strS,
			Env: env}),
		Env:              env,
		Inputs:           na.inputList(inputs, transitiveImports),
		Outputs:          na.vfsList(genCC, genH),
		KV:               KV{P: kvP, PC: pcYellow},
		TargetProperties: targetProps,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs:   depRefs(cppStyleguideLDRef, protocLDRef, pluginLDRef),
		Resources:        usesPython3,
	}

	return emit.emit(node)
}

func emitLibraryEvSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	evSource := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
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
		d.tc, ctx.emit)

	evH := build(evRelPath + ".pb.h")
	evPbCC := build(evRelPath + ".pb.cc")

	{
		directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, "")
		evExtras := evWitnessExtras(evRelPath, evPbCC)
		evHParsed := make([]IncludeDirective, 0, len(directImports)+len(protobufRuntimeDirectives)+len(evExtras))
		evHParsed = append(evHParsed, directImports...)
		evHParsed = append(evHParsed, protobufRuntimeDirectives...)
		evHParsed = append(evHParsed, evExtras...)
		registerBoundGeneratedParsedOutput(ctx, instance, pkEV, evH, evHParsed, evRef, []NodeRef{event2cppLDRef})
		evCCParsed := make([]IncludeDirective, 0, 1+len(protobufRuntimeDirectives))
		evCCParsed = append(evCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(evH.rel())})
		evCCParsed = append(evCCParsed, protobufRuntimeDirectives...)
		registerBoundGeneratedParsedOutput(ctx, instance, pkEV, evPbCC, evCCParsed, evRef, []NodeRef{event2cppLDRef})
	}

	evPbCCSuffix := srcRel + ".pb.cc"
	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), evPbCC, in.ScanCfg)
	{
		filtered := make([]VFS, 0, len(ccIn.IncludeInputs))

		for _, v := range ccIn.IncludeInputs {
			if v == evH {
				continue
			}

			filtered = append(filtered, v)
		}

		ccIn.IncludeInputs = filtered
	}
	wireFormatVFS := source(pbRuntimeBase + "google/protobuf/wire_format.h")
	ccIn.IncludeInputs = append(ccIn.IncludeInputs, wireFormatVFS)
	ccIn.ExtraDepRefs = append([]NodeRef{evRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, evRef)...)
	ref, outPath, _ := emitCC(instance, evPbCCSuffix, evPbCC, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}
