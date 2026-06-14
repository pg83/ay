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
	na := emit.nodeArenas()

	moduleDir := instance.Path.rel()

	evCC := build(evRelPath + ".pb.cc")
	evH := build(evRelPath + ".pb.h")
	srcVFS := source(evRelPath)

	cmdArgs := na.chunkList(na.strList(tc.Python3,
		internStr(pbWrapperPath),
		argOutputs.str(),
		(evCC).str(),
		(evH).str(),
		arg2.str(),
		(protocBinary).str()), evProtocConstArgs, na.strList(internStr("--plugin=protoc-gen-cpp_styleguide="+cppStyleguideBinary.string()),
		internStr(evRelPath),
		internStr("--plugin=protoc-gen-event2cpp="+event2cppBinary.string()),
		argEvent2cppOutB.str(),
		internStr("-I="+evEventlogIncludePath)))

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

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: strS,
			Env: env}),
		Env:              env,
		Inputs:           na.inputList(inputs, transitiveImports),
		Outputs:          na.vfsList(evCC, evH),
		KV:               KV{P: pkEV, PC: pcYellow},
		TargetProperties: targetProps,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs:   depRefs(cppStyleguideLDRef, protocLDRef, event2cppLDRef),
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
		0, evImports, d.tc, ctx.emit)

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
