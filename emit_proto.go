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
	pbWrapperPath                  = pbWrapperVFS.string()
	pbKV                           = KV{P: pkPB, PC: pcYellow}
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

var protobufRuntimeHeaders = []VFS{
	source(pbRuntimeBase, "google/protobuf/arena.h"),
	source(pbRuntimeBase, "google/protobuf/arenastring.h"),
	source(pbRuntimeBase, "google/protobuf/extension_set.h"),
	source(pbRuntimeBase, "google/protobuf/generated_message_reflection.h"),
	source(pbRuntimeBase, "google/protobuf/generated_message_util.h"),
	source(pbRuntimeBase, "google/protobuf/io/coded_stream.h"),
	source(pbRuntimeBase, "google/protobuf/message.h"),
	source(pbRuntimeBase, "google/protobuf/metadata_lite.h"),
	source(pbRuntimeBase, "google/protobuf/port_def.inc"),
	source(pbRuntimeBase, "google/protobuf/port_undef.inc"),
	source(pbRuntimeBase, "google/protobuf/repeated_field.h"),
	source(pbRuntimeBase, "google/protobuf/unknown_field_set.h"),
}

var pbDescriptorImporterHeaders = []VFS{
	source(pbRuntimeBase, "google/protobuf/generated_message_bases.h"),
	source(pbRuntimeBase, "google/protobuf/map_entry.h"),
	source(pbRuntimeBase, "google/protobuf/map_entry_lite.h"),
	source(pbRuntimeBase, "google/protobuf/map_field.h"),
	source(pbRuntimeBase, "google/protobuf/map_field_inl.h"),
	source(pbRuntimeBase, "google/protobuf/map_field_lite.h"),
	source(pbRuntimeBase, "google/protobuf/reflection_ops.h"),
}

const (
	yaffRuntimeBase = "library/cpp/yaff/"
	pbRuntimeBase   = "contrib/libs/protobuf/src/"
)

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

func (e *EmitContext) emitProtoPB(srcRel string, cfg ProtoPBConfig, pe *PbModuleEmission, peerProtoAddIncl []VFS, sprotoProduced map[string]struct{}) ProtoPBEmission {
	ctx, instance, d := e.ctx, e.instance, e.d
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

	if info := e.codegen.lookup(buildProto); info != nil {
		protoSrcOverride = buildProto
		protoVFS = buildProto
		extraProtoDeps = []NodeRef{info.ProducerRef}
		protoProducerSourceInputs = info.SourceInputs
		genProtoParsed = info.ParsedIncludes
	}

	transitiveImports := walkClosureTail(e.scanner, protoVFS, protoWalkInputs(ctx.parsers, protoSearchPaths, instance.Path.rel()))

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

	reg := e.codegen

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

func (e *EmitContext) emitProtoSrcs(peerContribs PeerGlobalContribs) *ProtoSrcsResult {
	_, instance, d := e.ctx, e.instance, e.d

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
		return e.emitPyProtoSrcs(peerContribs, protoSrcs, evSrcs)
	default:
		return e.emitCPPProtoSrcs(peerContribs, protoSrcs, evSrcs, gztSrcs)
	}
}

func (e *EmitContext) emitCPPProtoSrcs(peerContribs PeerGlobalContribs, protoSrcs, evSrcs, gztSrcs []string) *ProtoSrcsResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcDeclIdx := make(map[string]int, len(d.srcs))

	for i, src := range d.srcs {
		if _, seen := srcDeclIdx[src.string()]; !seen {
			srcDeclIdx[src.string()] = i
		}
	}

	for i, gztSrc := range gztSrcs {
		_, genProtoSrc := e.emitLibraryGztProtoSource(gztSrc, peerContribs.protoInclude, tagCppProto)

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
	sprotoProduced := e.ymapsSprotoProducedBases()
	pe := newPBModuleEmission(ctx, d, cfg, peerContribs.protoInclude)

	for _, src := range protoSrcs {
		pb := e.emitProtoPB(src, cfg, pe, peerContribs.protoInclude, sprotoProduced)

		for _, cc := range pb.orderedCC {
			ccSrcRel := strings.TrimPrefix(cc.rel(), cppInstance.Path.rel()+"/")

			appendCodegenOutput(pb.pbRef, cc, ccSrcRel, srcDeclIdx[src])
		}
	}

	e.emitYmapsSprotoHeaders(peerContribs, sprotoProduced)

	if len(evSrcs) > 0 {
		protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
		cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
		event2cppLDRef, event2cppBinary := ctx.tool(argToolsEvent2cpp)

		for _, src := range evSrcs {
			evRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
			evVFS := source(evRelPath)
			evImports := walkClosureTail(e.scanner, evVFS, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()))

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

			e.codegen.register(&GeneratedFileInfo{
				OutputPath:     evH,
				ProducerRef:    evRef,
				GeneratorRefs:  []NodeRef{event2cppLDRef},
				ParsedIncludes: evHParsed,
				ClosureLeaves:  []VFS{evPbCC},
			})

			evCCParsed := append(append([]IncludeDirective(nil), evHParsed...),
				IncludeDirective{kind: includeQuoted, target: internStr(source(pbRuntimeBase, "google/protobuf/wire_format.h").rel())})

			e.codegen.register(&GeneratedFileInfo{
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

	for _, co := range codegenOutputs {
		e.enqueueSrc(co.pbCC.str(), SrcMeta{Prio: stmtPrioSrcs, Seq: co.declIdx, Generated: true})
	}

	e.emitEnumSrcs(peerContribs.addIncl)

	protoLibName := ""

	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0].string()
	}

	return &ProtoSrcsResult{PendingAR: true, ProtoLibName: protoLibName}
}

func (e *EmitContext) emitProtoProducer(srcRel string) {
	ctx, _, d := e.ctx, e.instance, e.d

	cfg := ProtoPBConfig{
		cppOutRoot: protoCPPOutRoot(d),
		grpc:       d.grpc,
	}

	pe := newPBModuleEmission(ctx, d, cfg, d.cc.ProtoIncludePeers)

	e.emitProtoPB(srcRel, cfg, pe, d.cc.ProtoInclude, nil)
}

func (e *EmitContext) emitLibraryProtoSource(src STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()

	e.emitProtoProducer(srcRel)

	protoBase := strings.TrimSuffix(protoSourceRelPath(ctx.fs, instance, d, srcRel), ".proto")
	meta := e.metaForSrc(src)

	meta.Generated = true

	e.enqueueSrc(build(protoBase, ".pb.cc").str(), meta)

	if d.grpc {
		e.enqueueSrc(build(protoBase, ".grpc.pb.cc").str(), meta)
	}
}

type ResolvedCPPProtoPlugin struct {
	Spec   CppProtoPlugin
	LDRef  NodeRef
	Binary VFS
}

func emitPB(
	instance ModuleInstance,
	protoRelPath string,
	protoSrcOverride VFS,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	grpcCppLDRef NodeRef,
	cppStyleguideBinary VFS,
	protocBinary VFS,
	grpcCppBinary VFS,
	grpc bool,
	moduleTag STR,
	liteHeaders bool,
	extraPlugins []ResolvedCPPProtoPlugin,
	transitiveProtoImports []VFS,
	extraDepRefs []NodeRef,
	producerSourceInputs []VFS,
	blocks *PbArgBlocks,
	emit *StreamingEmitter,
) NodeRef {
	na := emit.nodeArenas()
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	pbH := build(protoBase, ".pb.h")
	pbCC := build(protoBase, ".pb.cc")
	pbDepsH := build(protoBase, ".deps.pb.h")
	grpcPbCC := build(protoBase, ".grpc.pb.cc")
	grpcPbH := build(protoBase, ".grpc.pb.h")
	srcVFS := source(protoRelPath)

	if protoSrcOverride != 0 {
		srcVFS = protoSrcOverride
	}

	outputs := assembleProtoCmdOutputs(protoBase, pbH, pbCC, pbDepsH, grpcPbCC, grpcPbH, extraPlugins, liteHeaders, grpc)
	outsChunk := make([]STR, 0, len(outputs))

	for _, output := range outputs {
		outsChunk = append(outsChunk, (output).str())
	}

	cmdArgs := na.chunkList(blocks.head, outsChunk, blocks.mid, na.strList(internStr(protoRelPath)))

	if len(blocks.tail) > 0 {
		cmdArgs = append(cmdArgs, blocks.tail)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	inputs := []VFS{
		cppStyleguideBinary,
	}

	if grpc {
		inputs = append(inputs, grpcCppBinary)
	}

	inputs = append(inputs, protocBinary)

	for _, plugin := range extraPlugins {
		inputs = append(inputs, plugin.Binary)
	}

	inputs = append(inputs, pbWrapperVFS)
	inputs = append(inputs, srcVFS)

	foreignDepRefs := depRefs(cppStyleguideLDRef, grpcCppLDRef, protocLDRef)

	for _, plugin := range extraPlugins {
		foreignDepRefs = append(foreignDepRefs, depRefs(plugin.LDRef)...)
	}

	foreignDepRefs = dedupRefs(foreignDepRefs)

	deps := append([]NodeRef(nil), extraDepRefs...)
	protocCwd := "$(S)"

	if protoSrcOverride != 0 {
		protocCwd = "$(B)"
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: internStr(protocCwd),
			Env: env}),
		Env: env,

		Inputs:         na.inputList(inputs, transitiveProtoImports, producerSourceInputs),
		Outputs:        outputs,
		KV:             &pbKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        deps,
		ForeignDepRefs: foreignDepRefs,
		Resources:      usesPython3,
	}

	return emit.emit(node)
}

func assembleProtoCmdOutputs(protoBase string, pbH, pbCC, pbDepsH, grpcPbCC, grpcPbH VFS, extraPlugins []ResolvedCPPProtoPlugin, liteHeaders, grpc bool) []VFS {
	outputs := []VFS{pbH}

	for _, plugin := range extraPlugins {
		if !pluginOutputsPrecedeCppGroup(plugin, liteHeaders) {
			continue
		}

		for _, suffix := range plugin.Spec.OutputSuffixes {
			outputs = append(outputs, build(protoBase, suffix))
		}
	}

	outputs = append(outputs, pbCC)

	if liteHeaders {
		outputs = append(outputs, pbDepsH)
	}

	if grpc {
		outputs = append(outputs, grpcPbCC, grpcPbH)
	}

	for _, plugin := range extraPlugins {
		if pluginOutputsPrecedeCppGroup(plugin, liteHeaders) {
			continue
		}

		for _, suffix := range plugin.Spec.OutputSuffixes {
			outputs = append(outputs, build(protoBase, suffix))
		}
	}

	return outputs
}

func pluginOutputsPrecedeCppGroup(plugin ResolvedCPPProtoPlugin, liteHeaders bool) bool {
	return liteHeaders && plugin.Spec.DeclaredBeforeLiteHeaders
}

func protoCPPOutRoot(d *ModuleData) string {
	if d.protoNamespace == nil {
		return ""
	}

	root := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())), "/")

	if root == "." {
		return ""
	}

	return root
}

type ProtoSrcsResult struct {
	ARRef                NodeRef
	ARPath               *VFS
	GlobalRef            *NodeRef
	GlobalPath           *VFS
	WholeArchiveRefs     []NodeRef
	WholeArchivePaths    []VFS
	WholeArchiveCmdPaths []VFS
	PendingAR            bool
	ProtoLibName         string
}

func protoSourceRelPath(fs FS, instance ModuleInstance, d *ModuleData, src string) string {
	return filepath.ToSlash(filepath.Clean(resolvePySrcRel(fs, d.srcDirs, instance.Path.rel(), src)))
}

type PbArgBlocks struct {
	head []STR
	mid  []STR
	tail []STR
}

func composePBArgBlocks(tc ModuleToolchain, protocBinary, cppStyleguideBinary, grpcCppBinary VFS,
	grpc bool, cppOutRoot string, liteHeaders bool,
	extraProtocFlags []ARG, extraPlugins []ResolvedCPPProtoPlugin,
	protoInclude []VFS) *PbArgBlocks {
	head := []STR{
		tc.Python3,
		internStr(pbWrapperPath),
		argOutputs.str(),
	}

	includeRoot := ""

	if cppOutRoot != "" {
		includeRoot = cppOutRoot
	}

	cppOutArg := ":$(B)/" + cppOutRoot

	if liteHeaders {
		cppOutArg = "proto_h=true" + cppOutArg
	}

	mid := make([]STR, 0, 12+len(protoInclude)+len(extraProtocFlags))

	mid = append(mid,
		arg2.str(),
		(protocBinary).str(),
		internV("-I=./", includeRoot),
		internV("-I=$(S)/", includeRoot),
		argIB2.str(),
		argIS3.str(),
	)

	if cppOutRoot != "" {
		mid = append(mid, internV("-I=$(S)/", cppOutRoot))
	}

	for _, p := range protoInclude {
		mid = append(mid, internV("-I=", p.string()))
	}

	mid = append(mid,
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		internV("--cpp_out=", cppOutArg),
	)

	mid = appendArgStr(mid, extraProtocFlags)

	mid = append(mid,
		internV("--cpp_styleguide_out=:$(B)/", cppOutRoot),
		internV("--plugin=protoc-gen-cpp_styleguide=", cppStyleguideBinary.string()),
	)

	var tail []STR

	if grpc {
		tail = append(tail,
			internV("--plugin=protoc-gen-grpc_cpp=", grpcCppBinary.string()),
			internV("--grpc_cpp_out=$(B)/", cppOutRoot),
		)
	}

	for _, plugin := range extraPlugins {
		tail = append(tail,
			internV("--plugin=protoc-gen-", plugin.Spec.Name, "=", plugin.Binary.string()),
			internV("--", plugin.Spec.Name, "_out=$(B)/", cppOutRoot),
		)

		for _, piece := range strings.Split(plugin.Spec.ExtraOutFlag, ",") {
			if piece == "" {
				continue
			}

			tail = append(tail, internV("--", plugin.Spec.Name, "_opt=", piece))
		}
	}

	return &PbArgBlocks{head: head, mid: mid, tail: tail}
}
