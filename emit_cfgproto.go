package main

var cfgprotoKV = KV{P: pkPB, PC: pcYellow}

func (e *EmitContext) emitLibraryCfgProtoSource(meta SrcMeta) {
	src := meta.Source
	ctx, instance, d := e.ctx, e.instance, e.d
	protocLDRef, _ := ctx.tool(argContribToolsProtoc)
	cppStyleguideLDRef, _ := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
	configPluginLDRef, configPluginBinary := ctx.tool(argLibraryCppProtoConfigPlugin)
	cfgRelPath := protoSourceRelPath(ctx.fs, instance, d, src.string())
	cfgSource := source(cfgRelPath)
	cfgImports := walkClosure(e.scanner, cfgSource, protoWalkInputs(ctx.parsers, nil, instance.Path.relString()))
	directImports := protoDirectPbHIncludes(ctx.parsers, cfgRelPath, protoCPPOutRoot(d))
	configIncludes := ctx.parsers.sourceParsedBuckets(cfgSource, nil).bucket(parsedIncludesProtoConfig)
	extras := pbHEmitsIncludesExtras()
	cfgHParsed := make([]IncludeDirective, 0, len(directImports)+len(configIncludes)+len(extras)+cfgImports.len())

	cfgHParsed = append(cfgHParsed, directImports...)
	cfgHParsed = append(cfgHParsed, configIncludes...)
	cfgHParsed = append(cfgHParsed, extras...)

	eachBucketVFS(cfgImports.buckets, func(ti VFS) {
		cfgHParsed = append(cfgHParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(ti.rel())})
	})

	e.emitCppProtoFamilySource(meta, &ProtoSpec{
		kv:          &cfgprotoKV,
		ccFirstOuts: true,
		optsTail: []ANY{
			internV("--plugin=protoc-gen-config=", configPluginBinary.prefix(), configPluginBinary.relString()).any(),
			argConfigOutB.any(),
		},
		toolLDRef:  configPluginLDRef,
		toolBinary: configPluginBinary,
		genRefs:    []NodeRef{protocLDRef, cppStyleguideLDRef, configPluginLDRef},
		genHParsed: cfgHParsed,
		genCCExtras: []IncludeDirective{
			{kind: includeQuoted, target: includeTarget(pbWrapperVFS.rel())},
		},
		hLeaves:  []VFS{cfgSource, build(cfgRelPath, ".pb.cc")},
		ccLeaves: []VFS{cfgSource},
	})
}
