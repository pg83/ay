package main

// emitLibraryCfgProtoSource emits a `.cfgproto` source: the PB/yellow producer
// (protoc wrapper with the proto_config plugin), the generated `.pb.h`/`.pb.cc`
// parsed-output registration, and the downstream `.pb.cc` compile archived as the
// module's codegen object.
func emitLibraryCfgProtoSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	cfgSource := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
	cfgRelPath := cfgSource.rel()

	protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
	cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
	configPluginLDRef, configPluginBinary := ctx.tool(argLibraryCppProtoConfigPlugin)

	na := ctx.emit.nodeArenas()
	// Trailing plugin block: proto_config plugin + --config_out.
	configOpts := na.strList(internStr("--plugin=protoc-gen-config="+configPluginBinary.string()),
		argConfigOutB.str())

	cfgImports := walkClosureTail(ctx.scannerFor(instance), cfgSource, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()).ScanCfg)

	cfgRef := emitProtoWrapperPBNode(
		instance, cfgRelPath, pkPB,
		cppStyleguideLDRef, protocLDRef, configPluginLDRef,
		cppStyleguideBinary, protocBinary, configPluginBinary,
		configOpts, 0, cfgImports, in.ProtoInclude,
		!protoTransitiveHeadersEnabled(d),
		d.tc, ctx.emit)

	cfgH := build(cfgRelPath + ".pb.h")
	cfgPbCC := build(cfgRelPath + ".pb.cc")

	outputRoot := protoCPPOutRoot(d)
	cfgGenRefs := []NodeRef{protocLDRef, cppStyleguideLDRef, configPluginLDRef}

	{
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

		reg := codegenRegForInstance(ctx, instance)
		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, cfgH, cfgHParsed, cfgRef, cfgGenRefs)
		reg.addClosureLeaf(cfgH, cfgSource)
		// No `main` output is marked, so a unit including this .pb.h must also
		// reach its sibling .pb.cc. The sibling rides as a bare, non-expanded
		// closure leaf so the scanner never descends into it to re-resolve its
		// cpp-only induced deps into the consumer.
		reg.addClosureLeaf(cfgH, cfgPbCC)

		cfgCCParsed := []IncludeDirective{
			{kind: includeQuoted, target: internStr(cfgH.rel())},
			{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())},
		}
		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, cfgPbCC, cfgCCParsed, cfgRef, cfgGenRefs)
	}

	ccSrcRel := srcRel + ".pb.cc"
	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), cfgPbCC, in.ScanCfg)
	// Neither output is marked `main`, so the .pb.h does not ride the .pb.cc.o as
	// a direct input — drop it from the walked closure.
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
