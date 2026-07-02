package main

var cfgprotoKV = KV{P: pkPB, PC: pcYellow}

func (e *EmitContext) emitLibraryCfgProtoSource(src STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	protocLDRef, _ := ctx.tool(argContribToolsProtoc)
	cppStyleguideLDRef, _ := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
	configPluginLDRef, configPluginBinary := ctx.tool(argLibraryCppProtoConfigPlugin)
	cfgRelPath := protoSourceRelPath(ctx.fs, instance, d, src.string())
	cfgSource := source(cfgRelPath)
	cfgImports := walkClosureTail(e.scanner, cfgSource, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()))
	directImports := protoDirectPbHIncludes(ctx.parsers, cfgRelPath, protoCPPOutRoot(d))
	configIncludes := ctx.parsers.sourceParsedBuckets(cfgSource, nil).bucket(parsedIncludesProtoConfig)
	extras := pbHEmitsIncludesExtras()
	cfgHParsed := make([]IncludeDirective, 0, len(directImports)+len(configIncludes)+len(extras)+len(cfgImports))

	cfgHParsed = append(cfgHParsed, directImports...)
	cfgHParsed = append(cfgHParsed, configIncludes...)
	cfgHParsed = append(cfgHParsed, extras...)

	for _, ti := range cfgImports {
		cfgHParsed = append(cfgHParsed, IncludeDirective{kind: includeQuoted, target: internStr(ti.rel())})
	}

	e.emitCppProtoFamilySource(src, &ProtoSpec{
		kv:          &cfgprotoKV,
		ccFirstOuts: true,
		optsTail: []STR{
			internV("--plugin=protoc-gen-config=", configPluginBinary.string()),
			argConfigOutB.str(),
		},
		toolLDRef:  configPluginLDRef,
		toolBinary: configPluginBinary,
		genRefs:    []NodeRef{protocLDRef, cppStyleguideLDRef, configPluginLDRef},
		genHParsed: cfgHParsed,
		genCCExtras: []IncludeDirective{
			{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())},
		},
		hLeaves:  []VFS{cfgSource, build(cfgRelPath, ".pb.cc")},
		ccLeaves: []VFS{cfgSource},
	})
}
