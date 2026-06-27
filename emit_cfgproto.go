package main

var cfgprotoKV = KV{P: pkPB, PC: pcYellow}

func emitLibraryCfgProtoSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()
	cfgSource := resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)
	cfgRelPath := cfgSource.rel()
	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
	configPluginLDRef, configPluginBinary := ctx.tool(argLibraryCppProtoConfigPlugin)
	na := ctx.emit.nodeArenas()

	configOpts := na.strList(internV("--plugin=protoc-gen-config=", configPluginBinary.string()),
		argConfigOutB.str())

	cfgImports := walkClosureTail(ctx.scannerFor(instance), cfgSource, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()).ScanCfg)

	cfgRef := emitProtoWrapperPBNode(
		instance, cfgRelPath, &cfgprotoKV,
		cppStyleguideLDRef, protocLDRef, configPluginLDRef,
		cppStyleguideBinary, protocBinary, configPluginBinary,
		configOpts, 0, cfgImports, in.ProtoInclude,
		!protoTransitiveHeadersEnabled(d),
		d.tc, ctx.emit)

	cfgH := build(cfgRelPath, ".pb.h")
	cfgPbCC := build(cfgRelPath, ".pb.cc")
	outputRoot := protoCPPOutRoot(d)
	cfgGenRefs := []NodeRef{protocLDRef, cppStyleguideLDRef, configPluginLDRef}
	directImports := protoDirectPbHIncludes(ctx.parsers, cfgRelPath, outputRoot)
	configIncludes := ctx.parsers.sourceParsedBuckets(cfgSource, nil).bucket(parsedIncludesProtoConfig)
	extras := pbHEmitsIncludesExtras()
	cfgHParsed := make([]IncludeDirective, 0, len(directImports)+len(configIncludes)+len(extras)+len(cfgImports))
	cfgHParsed = append(cfgHParsed, directImports...)
	cfgHParsed = append(cfgHParsed, configIncludes...)
	cfgHParsed = append(cfgHParsed, extras...)

	for _, ti := range cfgImports {
		cfgHParsed = append(cfgHParsed, IncludeDirective{kind: includeQuoted, target: internStr(ti.rel())})
	}

	reg := ctx.codegenFor(instance)
	reg.register(&GeneratedFileInfo{
		ProducerKvP:    pkPB,
		OutputPath:     cfgH,
		ProducerRef:    cfgRef,
		GeneratorRefs:  cfgGenRefs,
		ParsedIncludes: cfgHParsed,
		ClosureLeaves:  []VFS{cfgSource, cfgPbCC},
	})

	cfgCCParsed := []IncludeDirective{
		{kind: includeQuoted, target: internStr(cfgH.rel())},
		{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())},
	}

	reg.register(&GeneratedFileInfo{
		ProducerKvP:    pkPB,
		OutputPath:     cfgPbCC,
		ProducerRef:    cfgRef,
		GeneratorRefs:  cfgGenRefs,
		ParsedIncludes: cfgCCParsed,
	})

	ccSrcRel := srcRel + ".pb.cc"
	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), cfgPbCC, in.ScanCfg)

	filtered := make([]VFS, 0, len(ccIn.IncludeInputs))

	for _, v := range ccIn.IncludeInputs {
		if v == cfgH {
			continue
		}

		filtered = append(filtered, v)
	}

	ccIn.IncludeInputs = filtered
	ccIn.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, ccIn.IncludeInputs, cfgRef)
	ref, outPath, _ := emitCC(instance, internStr(ccSrcRel), cfgPbCC, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ref, OutPath: outPath}
}
