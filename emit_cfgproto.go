package main

var cfgprotoKV = KV{P: pkPB, PC: pcYellow}

func (e *EmitContext) emitLibraryCfgProtoSource(meta SrcMeta) {
	src := meta.Source
	ctx, d := e.ctx, e.d
	protocLDRef, _ := e.tool(argContribToolsProtoc)
	cppStyleguideLDRef, _ := e.tool(argContribToolsProtocPluginsCppStyleguide)
	configPluginLDRef, configPluginBinary := e.tool(argLibraryCppProtoConfigPlugin)
	cfgRel := e.protoSourceRel(e.moduleSourceName(src))
	cfgRelPath := cfgRel.string()
	cfgSource := cfgRel.source()
	directImports := protoDirectPbHIncludes(ctx.parsers, cfgRelPath, protoCPPOutRoot(d), e.dirScratch[:0])

	e.dirScratch = directImports

	configIncludes := ctx.parsers.sourceParsedBuckets(cfgSource, nil).bucket(parsedIncludesProtoConfig)
	extras := pbDescriptorImporterDirectives
	cfgHParsed := ctx.na.dirs.alloc(len(directImports) + len(configIncludes) + len(extras))[:0]

	cfgHParsed = append(cfgHParsed, directImports...)
	cfgHParsed = append(cfgHParsed, configIncludes...)
	cfgHParsed = append(cfgHParsed, extras...)

	ctx.na.dirs.commit(len(cfgHParsed))
	cfgHParsed = cfgHParsed[:len(cfgHParsed):len(cfgHParsed)]

	e.emitCppProtoFamilySource(meta, &ProtoSpec{
		kv:          &cfgprotoKV,
		ccFirstOuts: true,
		optsTail: ctx.na.anyList(
			internV("--plugin=protoc-gen-config=", configPluginBinary.prefix(), configPluginBinary.relString()).any(),
			argConfigOutB.any(),
		),
		toolLDRef:  configPluginLDRef,
		toolBinary: configPluginBinary,
		genRefs:    ctx.na.refList(protocLDRef, cppStyleguideLDRef, configPluginLDRef),
		genHParsed: cfgHParsed,
	})
}
