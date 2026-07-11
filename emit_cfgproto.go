package main

var cfgprotoKV = KV{P: pkPB, PC: pcYellow}

var cfgprotoGenCCExtras = []IncludeDirective{
	{kind: includeQuoted, target: includeTarget(pbWrapperVFS.rel().any())},
}

func (e *EmitContext) emitLibraryCfgProtoSource(meta SrcMeta) {
	src := meta.Source
	ctx, instance, d := e.ctx, e.instance, e.d
	protocLDRef, _ := ctx.tool(argContribToolsProtoc)
	cppStyleguideLDRef, _ := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
	configPluginLDRef, configPluginBinary := ctx.tool(argLibraryCppProtoConfigPlugin)
	cfgRelPath := protoSourceRelPath(ctx.fs, instance, d, e.moduleSourceName(src))
	cfgSource := source(cfgRelPath)
	cfgImports := e.scanner.walkClosure(cfgSource, d.scanCtx, scanDomainProto)
	directImports := protoDirectPbHIncludes(ctx.parsers, cfgRelPath, protoCPPOutRoot(d), e.dirScratch[:0])

	e.dirScratch = directImports

	configIncludes := ctx.parsers.sourceParsedBuckets(cfgSource, nil).bucket(parsedIncludesProtoConfig)
	extras := pbHEmitsIncludesExtras()
	cfgHParsed := ctx.na.dirs.alloc(len(directImports) + len(configIncludes) + len(extras) + cfgImports.len())[:0]

	cfgHParsed = append(cfgHParsed, directImports...)
	cfgHParsed = append(cfgHParsed, configIncludes...)
	cfgHParsed = append(cfgHParsed, extras...)

	eachBucketVFS(cfgImports.buckets, func(ti VFS) {
		cfgHParsed = append(cfgHParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(ti.rel().any())})
	})

	ctx.na.dirs.commit(len(cfgHParsed))
	cfgHParsed = cfgHParsed[:len(cfgHParsed):len(cfgHParsed)]

	e.emitCppProtoFamilySource(meta, &ProtoSpec{
		kv:          &cfgprotoKV,
		ccFirstOuts: true,
		optsTail: ctx.na.anyList(
			internV("--plugin=protoc-gen-config=", configPluginBinary.prefix(), configPluginBinary.relString()).any(),
			argConfigOutB.any(),
		),
		toolLDRef:   configPluginLDRef,
		toolBinary:  configPluginBinary,
		genRefs:     ctx.na.refList(protocLDRef, cppStyleguideLDRef, configPluginLDRef),
		genHParsed:  cfgHParsed,
		genCCExtras: cfgprotoGenCCExtras,
		hLeaves:     ctx.na.vfsList(cfgSource, build(cfgRelPath, ".pb.cc")),
		ccLeaves:    ctx.na.vfsList(cfgSource),
	})
}
