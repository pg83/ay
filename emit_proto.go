package main

import (
	"path/filepath"
	"sort"
	"strings"
)

var (
	protobufRuntimeDirectives      = quotedDirectives(protobufRuntimeHeaders)
	pbDescriptorImporterDirectives = quotedDirectives(pbDescriptorImporterHeaders)
	pbRuntimeBaseVFS               = source(strings.TrimSuffix(pbRuntimeBase, "/"))
)

var yaffBaseRuntimeHeaders = []string{
	yaffRuntimeBase + "yaff.h",
	yaffRuntimeBase + "struct.h",
	yaffRuntimeBase + "protobuf.h",
	yaffRuntimeBase + "reflect.h",
}

var yaffExperimentsRuntimeHeaders = []string{
	yaffRuntimeBase + "experiments/serializer.h",
	yaffRuntimeBase + "experiments/column.h",
	yaffRuntimeBase + "experiments/merge.h",
}

const yaffRuntimeBase = "library/cpp/yaff/"

func quotedDirectives(headers []VFS) []IncludeDirective {
	out := make([]IncludeDirective, len(headers))

	for i, h := range headers {
		out[i] = IncludeDirective{kind: includeQuoted, target: internStr(h.rel())}
	}

	return out
}

func yaffGeneratedHeaderIncludes(experimental bool, pbHRel string) []IncludeDirective {
	n := len(yaffBaseRuntimeHeaders) + 1

	if experimental {
		n += len(yaffExperimentsRuntimeHeaders)
	}

	dirs := make([]IncludeDirective, 0, n)

	for _, h := range yaffBaseRuntimeHeaders {
		dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: internStr(h)})
	}

	dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: internStr(pbHRel)})

	if experimental {
		for _, h := range yaffExperimentsRuntimeHeaders {
			dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: internStr(h)})
		}
	}

	return dirs
}

func protoPbHIncludes(pm *IncludeParserManager, srcRel, outputRoot string, bucket ParsedIncludeBucket) []IncludeDirective {
	hcpp := pm.sourceParsedBuckets(source(srcRel), nil).bucket(bucket)

	if len(hcpp) == 0 {
		return nil
	}

	out := make([]IncludeDirective, 0, len(hcpp))

	for _, d := range hcpp {
		target := d.target.string()

		if strings.HasPrefix(target, "google/protobuf/") && extIsPbH(target) {
			target = pbRuntimeBase + target
		} else {
			target = protoOutputRel(outputRoot, target)
		}

		out = append(out, IncludeDirective{kind: d.kind, target: internStr(target)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].target.string() < out[j].target.string() })

	return out
}

func protoDirectPbHIncludes(pm *IncludeParserManager, srcRel, outputRoot string) []IncludeDirective {
	return protoPbHIncludes(pm, srcRel, outputRoot, parsedIncludesHeader)
}

func protoInducedPbH(pm *IncludeParserManager, local []IncludeDirective) []IncludeDirective {
	if len(local) == 0 {
		return nil
	}

	out := make([]IncludeDirective, 0, len(local))

	for _, d := range local {
		name := filepath.ToSlash(filepath.Clean(d.target.string()))
		pbH, ok := pm.protoParser().inducedHeader(internStr(name))

		if !ok {
			continue
		}

		out = append(out, IncludeDirective{kind: d.kind, target: pbH})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].target.string() < out[j].target.string() })

	return out
}

func pbHEmitsIncludesExtras() []IncludeDirective {
	out := make([]IncludeDirective, 0, len(pbDescriptorImporterDirectives)+1)

	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})
	out = append(out, pbDescriptorImporterDirectives...)

	return out
}

func protoWalkInputs(pm *IncludeParserManager, peerProtoAddIncl []VFS, ownerModuleDir string) ScanContext {
	own := make([]VFS, 0, 1+len(peerProtoAddIncl))

	own = append(own, pbRuntimeBaseVFS)
	own = append(own, peerProtoAddIncl...)

	return newScanContext(pm, own, nil, includeScannerBasePaths(), ownerModuleDir)
}

func protoDirectImportNames(pm *IncludeParserManager, srcRel string) []string {
	direct := pm.sourceParsedBuckets(source(srcRel), nil).bucket(parsedIncludesLocal)

	if len(direct) == 0 {
		return nil
	}

	out := make([]string, 0, len(direct))

	for _, d := range direct {
		out = append(out, d.target.string())
	}

	return out
}

func protoOutputRel(outputRoot, rel string) string {
	if outputRoot == "" {
		return rel
	}

	return filepath.ToSlash(filepath.Clean(outputRoot + "/" + rel))
}

func protoTransitiveHeadersEnabled(d *ModuleData) bool {
	if d.setVars != nil {
		if v, ok := d.setVars[strProtocTransitiveHeaders]; ok {
			return v.string() != "no"
		}
	}

	if d.defaultVars != nil {
		if v, ok := d.defaultVars[strProtocTransitiveHeaders]; ok {
			return v.string() != "no"
		}
	}

	return true
}

type ProtoPBConfig struct {
	grpc       bool
	moduleTag  STR
	cppOutRoot string
}

type ProtoPBEmission struct {
	pbRef     NodeRef
	pbCC      VFS
	grpcPbCC  VFS
	orderedCC []VFS
	relPath   string
}

type PbModuleEmission struct {
	protocLDRef         NodeRef
	cppStyleguideLDRef  NodeRef
	grpcCppLDRef        NodeRef
	protocBinary        VFS
	cppStyleguideBinary VFS
	grpcCppBinary       VFS
	liteHeaders         bool
	extraPlugins        []ResolvedCPPProtoPlugin
	blocks              *PbArgBlocks
}

func newPBModuleEmission(ctx *GenCtx, d *ModuleData, cfg ProtoPBConfig, protoInclude []VFS) *PbModuleEmission {
	pe := &PbModuleEmission{
		liteHeaders:   !protoTransitiveHeadersEnabled(d),
		grpcCppBinary: pbGrpcCppVFS,
	}

	pe.protocLDRef, pe.protocBinary = ctx.tool(argContribToolsProtoc)
	pe.cppStyleguideLDRef, pe.cppStyleguideBinary = ctx.tool(argContribToolsProtocPluginsCppStyleguide)

	if cfg.grpc {
		pe.grpcCppLDRef, pe.grpcCppBinary = ctx.tool(argContribToolsProtocPluginsGrpcCpp)
	}

	pe.extraPlugins = make([]ResolvedCPPProtoPlugin, 0, len(d.cppProtoPlugins))

	for _, spec := range d.cppProtoPlugins {
		ldRef, binary := ctx.tool(internArg(spec.ToolPath))

		pe.extraPlugins = append(pe.extraPlugins, ResolvedCPPProtoPlugin{
			Spec:   spec,
			LDRef:  ldRef,
			Binary: binary,
		})
	}

	pe.blocks = composePBArgBlocks(d.tc, pe.protocBinary, pe.cppStyleguideBinary, pe.grpcCppBinary,
		cfg.grpc, cfg.cppOutRoot, pe.liteHeaders,
		d.protocFlags, pe.extraPlugins, protoInclude)

	return pe
}

func emitProtoPB(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, cfg ProtoPBConfig, pe *PbModuleEmission, peerProtoAddIncl []VFS, sprotoProduced map[string]struct{}) ProtoPBEmission {
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcRel)
	protoSearchPaths := peerProtoAddIncl

	if cfg.cppOutRoot != "" {
		protoSearchPaths = append([]VFS{source(cfg.cppOutRoot)}, peerProtoAddIncl...)
	}

	buildProto := build(protoRelPath)
	protoVFS := source(protoRelPath)

	var protoSrcOverride VFS
	var extraProtoDeps []NodeRef
	var protoProducerSourceInputs []VFS
	var genProtoParsed []IncludeDirective

	if info := ctx.codegenFor(instance).lookup(buildProto); info != nil {
		protoSrcOverride = buildProto
		protoVFS = buildProto
		extraProtoDeps = []NodeRef{info.ProducerRef}
		protoProducerSourceInputs = info.SourceInputs
		genProtoParsed = info.ParsedIncludes
	}

	transitiveImports := walkClosureTail(ctx.scannerFor(instance), protoVFS, protoWalkInputs(ctx.parsers, protoSearchPaths, instance.Path.rel()))

	extraProtoDeps = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, transitiveImports, extraProtoDeps...)

	pbRef := emitPB(
		instance, protoRelPath, protoSrcOverride, pe.cppStyleguideLDRef, pe.protocLDRef,
		pe.grpcCppLDRef, pe.cppStyleguideBinary, pe.protocBinary, pe.grpcCppBinary,
		cfg.grpc, cfg.moduleTag,
		pe.liteHeaders,
		pe.extraPlugins,
		transitiveImports,
		extraProtoDeps,
		protoProducerSourceInputs,
		pe.blocks,
		ctx.emit,
	)

	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	pbH := build(protoBase, ".pb.h")
	pbCC := build(protoBase, ".pb.cc")
	pbDepsH := build(protoBase, ".deps.pb.h")
	grpcPbH := build(protoBase, ".grpc.pb.h")
	grpcPbCC := build(protoBase, ".grpc.pb.cc")
	extraOutputPaths := make([]VFS, 0, 4)

	for _, plugin := range d.cppProtoPlugins {
		for _, suffix := range plugin.OutputSuffixes {
			extraOutputPaths = append(extraOutputPaths, build(protoBase, suffix))
		}
	}

	needsGRPCParsed := cfg.grpc

	if !needsGRPCParsed {
		for _, out := range extraOutputPaths {
			if out.rel() == grpcPbH.rel() || out.rel() == grpcPbCC.rel() {
				needsGRPCParsed = true

				break
			}
		}
	}

	directImports := protoInducedPbH(ctx.parsers, ctx.parsers.sourceParsedBuckets(source(protoRelPath), nil).bucket(parsedIncludesLocal))

	if protoSrcOverride != 0 {
		directImports = protoInducedPbH(ctx.parsers, genProtoParsed)
	}

	pbHImports := directImports

	if len(sprotoProduced) > 0 {
		pbHImports = concat(directImports, sprotoInducedHeaders(directImports))
	}

	extras := pbHEmitsIncludesExtras()
	pbHParsed := make([]IncludeDirective, 0, len(pbHImports)+len(extras)+len(transitiveImports))

	pbHParsed = append(pbHParsed, pbHImports...)
	pbHParsed = append(pbHParsed, extras...)

	for _, ti := range transitiveImports {
		if ti.isBuild() {
			continue
		}

		pbHParsed = append(pbHParsed, IncludeDirective{kind: includeQuoted, target: internStr(ti.rel())})
	}

	pbGenRefs := []NodeRef{pe.protocLDRef, pe.cppStyleguideLDRef}

	if cfg.grpc {
		pbGenRefs = append(pbGenRefs, pe.grpcCppLDRef)
	}

	for _, p := range pe.extraPlugins {
		pbGenRefs = append(pbGenRefs, depRefs(p.LDRef)...)
	}

	pbHLeaves := []VFS{source(protoRelPath)}

	if protoSrcOverride != 0 {
		pbHLeaves = protoProducerSourceInputs
	}

	reg := ctx.codegenFor(instance)

	reg.register(&GeneratedFileInfo{
		OutputPath:     pbH,
		ProducerRef:    pbRef,
		GeneratorRefs:  pbGenRefs,
		ParsedIncludes: pbHParsed,
		ClosureLeaves:  pbHLeaves,
	})

	protoBaseName := filepath.Base(protoRelPath)

	for _, plugin := range d.cppProtoPlugins {
		if !plugin.isYaff() || len(plugin.OutputSuffixes) != 2 {
			continue
		}

		yaffH := build(protoBase, plugin.OutputSuffixes[0])
		yaffCC := build(protoBase, plugin.OutputSuffixes[1])

		var yaffHParsed []IncludeDirective

		if plugin.processesFile(protoBaseName) {
			yaffHParsed = yaffGeneratedHeaderIncludes(plugin.isExperimental(protoBaseName), pbH.rel())
		}

		reg.register(&GeneratedFileInfo{
			OutputPath:     yaffH,
			ProducerRef:    pbRef,
			GeneratorRefs:  nil,
			ParsedIncludes: yaffHParsed,
		})

		yaffCCParsed := append(append([]IncludeDirective(nil), yaffHParsed...),
			IncludeDirective{kind: includeQuoted, target: internStr(pbH.rel())})

		reg.register(&GeneratedFileInfo{
			OutputPath:     yaffCC,
			ProducerRef:    pbRef,
			GeneratorRefs:  pbGenRefs,
			ParsedIncludes: yaffCCParsed,
		})
	}

	if pe.liteHeaders {
		depsParsed := make([]IncludeDirective, 0, 1+len(directImports))

		depsParsed = append(depsParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbH.rel())})
		depsParsed = append(depsParsed, directImports...)

		reg.register(&GeneratedFileInfo{
			OutputPath:     pbDepsH,
			ProducerRef:    pbRef,
			GeneratorRefs:  pbGenRefs,
			ParsedIncludes: depsParsed,
		})
	}

	pbCCParsed := make([]IncludeDirective, 0, 3+len(directImports))

	pbCCParsed = append(pbCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbH.rel())})

	if pe.liteHeaders {
		pbCCParsed = append(pbCCParsed, directImports...)
	}

	pbCCParsed = append(pbCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})

	reg.register(&GeneratedFileInfo{
		OutputPath:     pbCC,
		ProducerRef:    pbRef,
		GeneratorRefs:  pbGenRefs,
		ParsedIncludes: pbCCParsed,
	})

	var grpcCCParsed, grpcHParsed []IncludeDirective

	if needsGRPCParsed {
		grpcCCParsed = make([]IncludeDirective, 0, 2)
		grpcCCParsed = append(grpcCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbH.rel())})
		grpcCCParsed = append(grpcCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})

		grpcHParsed = make([]IncludeDirective, 0, 3+len(directImports))
		grpcHParsed = append(grpcHParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbH.rel())})
		grpcHParsed = append(grpcHParsed, directImports...)
		grpcHParsed = append(grpcHParsed, IncludeDirective{kind: includeQuoted, target: internV(pbRuntimeBase, "google/protobuf/port_def.inc")})
	}

	if cfg.grpc {
		reg.register(&GeneratedFileInfo{
			OutputPath:     grpcPbCC,
			ProducerRef:    pbRef,
			GeneratorRefs:  []NodeRef{pe.protocLDRef, pe.grpcCppLDRef},
			ParsedIncludes: grpcCCParsed,
		})

		reg.register(&GeneratedFileInfo{
			OutputPath:     grpcPbH,
			ProducerRef:    pbRef,
			GeneratorRefs:  []NodeRef{pe.grpcCppLDRef},
			ParsedIncludes: grpcHParsed,
		})
	}

	orderedCC := make([]VFS, 0, 2+len(extraOutputPaths))

	for _, out := range assembleProtoCmdOutputs(protoBase, pbH, pbCC, pbDepsH, grpcPbCC, grpcPbH, pe.extraPlugins, pe.liteHeaders, cfg.grpc) {
		if isCCSourceExt(out.rel()) {
			orderedCC = append(orderedCC, out)
		}
	}

	return ProtoPBEmission{
		pbRef:     pbRef,
		pbCC:      pbCC,
		grpcPbCC:  grpcPbCC,
		orderedCC: orderedCC,
		relPath:   protoRelPath,
	}
}

func emitProtoSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs) *ProtoSrcsResult {
	var protoSrcs, evSrcs, gztSrcs []string

	for _, src := range d.srcs {
		switch {
		case extIsProto(src.string()):
			protoSrcs = append(protoSrcs, src.string())
		case extIsEv(src.string()):
			evSrcs = append(evSrcs, src.string())
		case extIsGztproto(src.string()):
			gztSrcs = append(gztSrcs, src.string())
		}
	}

	if len(protoSrcs) == 0 && len(evSrcs) == 0 && len(gztSrcs) == 0 {
		return nil
	}

	switch instance.Language {
	case LangPy:
		return emitPyProtoSrcs(ctx, instance, d, peerContribs, protoSrcs, evSrcs)
	default:
		return emitCPPProtoSrcs(ctx, instance, d, peerContribs, protoSrcs, evSrcs, gztSrcs)
	}
}

func emitCPPProtoSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs, protoSrcs, evSrcs, gztSrcs []string) *ProtoSrcsResult {
	srcDeclIdx := make(map[string]int, len(d.srcs))

	for i, src := range d.srcs {
		if _, seen := srcDeclIdx[src.string()]; !seen {
			srcDeclIdx[src.string()] = i
		}
	}

	for i, gztSrc := range gztSrcs {
		_, genProtoSrc := emitLibraryGztProtoSource(ctx, instance, d, gztSrc, peerContribs.protoInclude, tagCppProto)

		protoSrcs = append(protoSrcs, genProtoSrc)
		srcDeclIdx[genProtoSrc] = len(d.srcs) + i
	}

	type protoCodegenOutput struct {
		genRef  NodeRef
		pbCC    VFS
		srcRel  string
		declIdx int
	}

	var codegenOutputs []protoCodegenOutput

	codegenOutputSeen := make(map[STR]struct{})

	appendCodegenOutput := func(genRef NodeRef, pbCC VFS, srcRel string, declIdx int) {
		if _, dup := codegenOutputSeen[internStr(pbCC.rel())]; dup {
			return
		}

		codegenOutputSeen[internStr(pbCC.rel())] = struct{}{}

		codegenOutputs = append(codegenOutputs, protoCodegenOutput{
			genRef:  genRef,
			pbCC:    pbCC,
			srcRel:  srcRel,
			declIdx: declIdx,
		})
	}

	cfg := ProtoPBConfig{
		grpc:       d.grpc,
		moduleTag:  tagCppProto,
		cppOutRoot: protoCPPOutRoot(d),
	}

	cppInstance := instance
	sprotoProduced := ymapsSprotoProducedBases(ctx, instance, d)
	pe := newPBModuleEmission(ctx, d, cfg, peerContribs.protoInclude)

	for _, src := range protoSrcs {
		pb := emitProtoPB(ctx, instance, d, src, cfg, pe, peerContribs.protoInclude, sprotoProduced)

		for _, cc := range pb.orderedCC {
			ccSrcRel := strings.TrimPrefix(cc.rel(), cppInstance.Path.rel()+"/")

			appendCodegenOutput(pb.pbRef, cc, ccSrcRel, srcDeclIdx[src])
		}
	}

	emitYmapsSprotoHeaders(ctx, instance, d, peerContribs, sprotoProduced)

	if len(evSrcs) > 0 {
		protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
		cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
		event2cppLDRef, event2cppBinary := ctx.tool(argToolsEvent2cpp)

		for _, src := range evSrcs {
			evRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
			evVFS := source(evRelPath)
			evImports := walkClosureTail(ctx.scannerFor(instance), evVFS, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()))

			evRef := emitEV(
				instance, evRelPath, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				tagCppProto, evImports, peerContribs.protoInclude,
				!protoTransitiveHeadersEnabled(d),
				d.tc, ctx.emit)

			evH := build(evRelPath, ".pb.h")
			evPbCC := build(evRelPath, ".pb.cc")
			directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, protoCPPOutRoot(d))
			evExtras := evWitnessExtras(evRelPath)
			evHParsed := make([]IncludeDirective, 0, len(directImports)+len(protobufRuntimeHeaders)+len(evExtras))

			evHParsed = append(evHParsed, directImports...)
			evHParsed = append(evHParsed, protobufRuntimeDirectives...)
			evHParsed = append(evHParsed, evExtras...)

			ctx.codegenFor(instance).register(&GeneratedFileInfo{
				OutputPath:     evH,
				ProducerRef:    evRef,
				GeneratorRefs:  []NodeRef{event2cppLDRef},
				ParsedIncludes: evHParsed,
				ClosureLeaves:  []VFS{evPbCC},
			})

			evCCParsed := append(append([]IncludeDirective(nil), evHParsed...),
				IncludeDirective{kind: includeQuoted, target: internStr(source(pbRuntimeBase, "google/protobuf/wire_format.h").rel())})

			ctx.codegenFor(instance).register(&GeneratedFileInfo{
				OutputPath:     evPbCC,
				ProducerRef:    evRef,
				GeneratorRefs:  []NodeRef{event2cppLDRef},
				ParsedIncludes: evCCParsed,
			})

			cppInstance := instance
			evSrcRel := strings.TrimPrefix(evRelPath+".pb.cc", cppInstance.Path.rel()+"/")

			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef:  evRef,
				pbCC:    evPbCC,
				srcRel:  evSrcRel,
				declIdx: srcDeclIdx[src],
			})
		}
	}

	sort.SliceStable(codegenOutputs, func(i, j int) bool {
		return codegenOutputs[i].declIdx < codegenOutputs[j].declIdx
	})

	if d.moduleStmt.Name != tokProtoLibrary || len(codegenOutputs) == 0 {
		return nil
	}

	ccRefs := make([]NodeRef, 0, len(codegenOutputs))
	ccOutputs := make([]VFS, 0, len(codegenOutputs))
	arDeclMeta := map[VFS]SrcMeta{}

	for _, co := range codegenOutputs {
		se := emitOneSource(ctx, cppInstance, d, co.pbCC.str())

		ccRefs = append(ccRefs, se.Ref)
		ccOutputs = append(ccOutputs, se.OutPath)
		arDeclMeta[se.OutPath] = SrcMeta{Prio: stmtPrioSrcs, Seq: co.declIdx, Generated: true}
	}

	enRes := emitEnumSrcs(ctx, instance, d, peerContribs.addIncl)

	if enRes != nil {
		for i := range enRes.CCRefs {
			ccRefs = append(ccRefs, enRes.CCRefs[i])
			ccOutputs = append(ccOutputs, enRes.CCOutputs[i])
			arDeclMeta[enRes.CCOutputs[i]] = SrcMeta{Prio: stmtPrioDefault, Seq: enRes.Seqs[i], Generated: true, SecondLevel: enRes.SecondLevel[i]}
		}
	}

	var antlrRefs []NodeRef
	var antlrOutputs []VFS

	reg := ctx.codegenFor(instance)

	for _, run := range d.antlrRuns {
		for _, outTok := range run.OUTFiles {
			if !isCCSourceExt(outTok.string()) {
				continue
			}

			outVFS := copyFileOutputVFS(instance.Path.rel(), outTok.string())
			info := reg.lookup(outVFS)

			if info == nil {
				continue
			}

			cppRel := antlrOutputModuleRel(instance.Path.rel(), outVFS)
			se := emitOneSource(ctx, cppInstance, d, copyFileOutputVFS(cppInstance.Path.rel(), cppRel).str())
			ccRef, ccOut := se.Ref, se.OutPath

			antlrRefs = append(antlrRefs, ccRef)
			antlrOutputs = append(antlrOutputs, ccOut)
			arDeclMeta[ccOut] = SrcMeta{Prio: stmtPrioDefault, Generated: true}
		}
	}

	if len(antlrRefs) > 0 {
		ccRefs = append(antlrRefs, ccRefs...)
		ccOutputs = append(antlrOutputs, ccOutputs...)
	}

	ccRefs, ccOutputs = reorderARMembers(ccRefs, ccOutputs, arDeclMeta)

	var protoLibName string

	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0].string()
	}

	arBaseName := archiveNameWithPrefixOrName(instance.Path.rel(), "lib", protoLibName)
	archivePath := build(instance.Path.rel(), "/", arBaseName)
	arRef := emitARNode(instance, archivePath, tagCppProto, ccRefs, ccOutputs, nil, nil, nil, d.tc, ctx.host, ctx.emit)

	return &ProtoSrcsResult{ARRef: arRef, ARPath: &archivePath}
}

func emitProtoProducer(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string) {
	cfg := ProtoPBConfig{
		cppOutRoot: protoCPPOutRoot(d),
		grpc:       d.grpc,
	}

	pe := newPBModuleEmission(ctx, d, cfg, d.cc.ProtoIncludePeers)

	emitProtoPB(ctx, instance, d, srcRel, cfg, pe, d.cc.ProtoInclude, nil)
}

func emitLibraryProtoSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR) *SourceEmit {
	srcRel := src.string()
	protoBase := strings.TrimSuffix(protoSourceRelPath(ctx.fs, instance, d, srcRel), ".proto")

	se := emitOneSource(ctx, instance, d, build(protoBase, ".pb.cc").str())

	if d.grpc {
		se.Extra = append(se.Extra, *emitOneSource(ctx, instance, d, build(protoBase, ".grpc.pb.cc").str()))
	}

	return se
}
