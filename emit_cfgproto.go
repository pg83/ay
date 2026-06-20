package main

// emitLibraryCfgProtoSource emits a `.cfgproto` source: the PB/yellow producer
// (_CPP_CFGPROTO_CMD, proto.conf:494-497 — the EV protoc wrapper with the
// proto_config plugin instead of event2cpp, outputs keeping the source
// extension), the generated `.cfgproto.pb.h`/`.pb.cc` parsed-output
// registration (standard protoc --cpp_out header: the import-induced .pb.h plus
// the file-level NProtoConfig.Include headers the plugin inserts, with the
// protobuf runtime riding protoc's GeneratorRefs as for any .proto), and the
// downstream `.pb.cc` compile archived as the module's codegen object.
func emitLibraryCfgProtoSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	cfgSource := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
	cfgRelPath := cfgSource.rel()

	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
	configPluginLDRef, configPluginBinary := ctx.tool(argLibraryCppProtoConfigPlugin)

	na := ctx.emit.nodeArenas()
	// CPP_CFGPROTO_CMD trailing plugin block: proto_config plugin + --config_out
	// (proto.conf:496), in place of CPP_EV_OPTS.
	configOpts := na.strList(internStr("--plugin=protoc-gen-config="+configPluginBinary.string()),
		argConfigOutB.str())

	cfgImports := walkClosureTail(ctx.scannerFor(instance), cfgSource, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()).ScanCfg)

	cfgRef := emitProtoWrapperPBNode(
		instance, cfgRelPath, pkPB,
		cppStyleguideLDRef, protocLDRef, configPluginLDRef,
		cppStyleguideBinary, protocBinary, configPluginBinary,
		configOpts, 0, cfgImports, in.ProtoInclude,
		d.tc, ctx.emit)

	cfgH := build(cfgRelPath + ".pb.h")
	cfgPbCC := build(cfgRelPath + ".pb.cc")

	outputRoot := protoCPPOutRoot(d)
	cfgGenRefs := []NodeRef{protocLDRef, cppStyleguideLDRef, configPluginLDRef}

	{
		directImports := protoDirectPbHIncludes(ctx.parsers, cfgRelPath, outputRoot)
		configIncludes := ctx.parsers.sourceParsedBuckets(cfgSource, nil).bucket(parsedIncludesProtoConfig)
		extras := pbHEmitsIncludesExtras()

		cfgHParsed := make([]IncludeDirective, 0, len(directImports)+len(configIncludes)+len(extras)+len(cfgImports)+1)
		cfgHParsed = append(cfgHParsed, directImports...)
		cfgHParsed = append(cfgHParsed, configIncludes...)
		cfgHParsed = append(cfgHParsed, extras...)
		// CPP_EV_OUTS marks no `main` output, so a unit that #includes this
		// generated .pb.h must also reach its sibling .pb.cc (EDT_OutTogether) —
		// the way an importer's .pb.cc.o reaches the imported module's .pb.cc.
		// Mirrors EV's evWitnessExtras adding evPbCC. The self .pb.cc.o filters
		// its own .pb.h below, so this ride only affects importers.
		cfgHParsed = append(cfgHParsed, IncludeDirective{kind: includeQuoted, target: internStr(cfgPbCC.rel())})

		for _, ti := range cfgImports {
			cfgHParsed = append(cfgHParsed, IncludeDirective{kind: includeQuoted, target: internStr(ti.rel())})
		}

		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, cfgH, cfgHParsed, cfgRef, cfgGenRefs)
		codegenRegForInstance(ctx, instance).addClosureLeaf(cfgH, cfgSource)

		cfgCCParsed := []IncludeDirective{
			{kind: includeQuoted, target: internStr(cfgH.rel())},
			{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())},
		}
		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, cfgPbCC, cfgCCParsed, cfgRef, cfgGenRefs)
	}

	ccSrcRel := srcRel + ".pb.cc"
	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), cfgPbCC, in.ScanCfg)
	// CPP_EV_OUTS marks neither .pb.cc nor .pb.h as the protoc `main` output
	// (unlike CPP_PROTO_OUTS), so the generated .pb.h does not ride the .pb.cc.o
	// as a direct input even though the .pb.cc #includes it — drop it from the
	// walked closure exactly as the .ev path does (emitLibraryEvSource).
	{
		filtered := make([]VFS, 0, len(ccIn.IncludeInputs))

		for _, v := range ccIn.IncludeInputs {
			if v == cfgH {
				continue
			}

			filtered = append(filtered, v)
		}

		ccIn.IncludeInputs = filtered
	}
	ccIn.ExtraDepRefs = append([]NodeRef{cfgRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, cfgRef)...)
	ref, outPath, _ := emitCC(instance, ccSrcRel, cfgPbCC, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}
