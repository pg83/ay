package main

var cfgprotoKV = KV{P: pkPB, PC: pcYellow}

func (e *EmitContext) emitLibraryCfgProtoSource(src STR) *SourceEmit {
	ctx, instance, d := e.ctx, e.instance, e.d
	cfgSource := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	cfgRelPath := cfgSource.rel()
	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
	configPluginLDRef, configPluginBinary := ctx.tool(argLibraryCppProtoConfigPlugin)
	na := ctx.emit.nodeArenas()

	configOpts := na.strList(internV("--plugin=protoc-gen-config=", configPluginBinary.string()),
		argConfigOutB.str())

	cfgImports := walkClosureTail(ctx.scannerFor(instance), cfgSource, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()))

	cfgRef := emitProtoWrapperPBNode(
		instance, cfgRelPath, &cfgprotoKV,
		cppStyleguideLDRef, protocLDRef, configPluginLDRef,
		cppStyleguideBinary, protocBinary, configPluginBinary,
		configOpts, 0, cfgImports, d.cc.ProtoInclude,
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
		OutputPath:     cfgH,
		ProducerRef:    cfgRef,
		GeneratorRefs:  cfgGenRefs,
		ParsedIncludes: cfgHParsed,
		ClosureLeaves:  []VFS{cfgSource, cfgPbCC},
	})

	cfgCCParsed := append(append([]IncludeDirective(nil), cfgHParsed...),
		IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})

	reg.register(&GeneratedFileInfo{
		OutputPath:     cfgPbCC,
		ProducerRef:    cfgRef,
		GeneratorRefs:  cfgGenRefs,
		ParsedIncludes: cfgCCParsed,
		ClosureLeaves:  []VFS{cfgSource},
	})

	return e.emitOneSource(cfgPbCC.str())
}
