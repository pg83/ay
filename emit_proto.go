package main

import (
	"path/filepath"
	"slices"
	"strings"
)

var (
	protobufRuntimeDirectives      = quotedDirectives(protobufRuntimeHeaders)
	pbDescriptorImporterDirectives = quotedDirectives(pbDescriptorImporterHeaders)
	pbRuntimeBaseVFS               = source(strings.TrimSuffix(pbRuntimeBase, "/"))
	pbKV                           = KV{P: pkPB, PC: pcYellow}
	cppProtoSpec                   = &ProtoSpec{kv: &pbKV, modulePlugins: true}
	pbHEmitsIncludesExtrasChunk    = concat([]IncludeDirective{{kind: includeQuoted, target: includeTarget(pbWrapperVFS.rel().any())}}, pbDescriptorImporterDirectives)
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

func yaffGeneratedHeaderIncludes(na *NodeArenas, experimental bool, pbHRel string) []IncludeDirective {
	n := len(yaffBaseRuntimeHeaders) + 1

	if experimental {
		n += len(yaffExperimentsRuntimeHeaders)
	}

	dirs := na.dirs.alloc(n)[:0]

	for _, h := range yaffBaseRuntimeHeaders {
		dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(h).any())})
	}

	dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(pbHRel).any())})

	if experimental {
		for _, h := range yaffExperimentsRuntimeHeaders {
			dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(h).any())})
		}
	}

	na.dirs.commit(len(dirs))

	return dirs[:len(dirs):len(dirs)]
}

func protoPbHIncludes(pm *IncludeParserManager, srcRel, outputRoot string, bucket ParsedIncludeBucket, dst []IncludeDirective) []IncludeDirective {
	hcpp := pm.sourceParsedBuckets(source(srcRel), nil).bucket(bucket)

	if len(hcpp) == 0 {
		return dst
	}

	start := len(dst)

	for _, d := range hcpp {
		target := d.target.string()

		if strings.HasPrefix(target, "google/protobuf/") && extIsPbH(target) {
			target = pbRuntimeBase + target
		} else {
			target = protoOutputRel(outputRoot, target)
		}

		dst = append(dst, IncludeDirective{kind: d.kind, target: includeTarget(internStr(target).any())})
	}

	slices.SortFunc(dst[start:], func(a, b IncludeDirective) int { return strings.Compare(a.target.string(), b.target.string()) })

	return dst
}

func protoDirectPbHIncludes(pm *IncludeParserManager, srcRel, outputRoot string, dst []IncludeDirective) []IncludeDirective {
	return protoPbHIncludes(pm, srcRel, outputRoot, parsedIncludesHeader, dst)
}

func protoInducedPbH(pm *IncludeParserManager, local, dst []IncludeDirective) []IncludeDirective {
	start := len(dst)

	for _, d := range local {
		name := filepath.ToSlash(filepath.Clean(d.target.string()))
		pbH, ok := pm.protoParser().inducedHeader(internStr(name))

		if !ok {
			continue
		}

		dst = append(dst, IncludeDirective{kind: d.kind, target: includeTarget(pbH.any())})
	}

	slices.SortFunc(dst[start:], func(a, b IncludeDirective) int { return strings.Compare(a.target.string(), b.target.string()) })

	return dst
}

func pbHEmitsIncludesExtras() []IncludeDirective {
	return pbHEmitsIncludesExtrasChunk
}

func protoWalkInputs(pm *IncludeParserManager, peerProtoAddIncl []VFS, ownerModuleDir string) ScanContext {
	own := make([]VFS, 0, 1+len(peerProtoAddIncl))

	own = append(own, pbRuntimeBaseVFS)
	own = append(own, peerProtoAddIncl...)

	return newScanContext(pm, own, nil, includeScannerBasePaths(), ownerModuleDir)
}

func protoEachDirectImportName(pm *IncludeParserManager, srcRel string, fn func(string)) {
	for _, d := range pm.sourceParsedBuckets(source(srcRel), nil).bucket(parsedIncludesLocal) {
		fn(d.target.string())
	}
}

func protoOutputRel(outputRoot, rel string) string {
	if outputRoot == "" {
		return rel
	}

	if pathIsClean(outputRoot) && pathIsClean(rel) {
		return internV(outputRoot, "/", rel).string()
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

type ProtoSpec struct {
	kv            *KV
	modulePlugins bool
	ccFirstOuts   bool
	optsTail      []ANY
	toolLDRef     NodeRef
	toolBinary    VFS
	genRefs       []NodeRef
	genHParsed    []IncludeDirective
	genCCExtras   []IncludeDirective
	hLeaves       []VFS
	ccLeaves      []VFS
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
	pbGenRefs           []NodeRef
	grpcCCRefs          []NodeRef
	grpcHRefs           []NodeRef
	blocks              PbArgBlocks
	searchPaths         []VFS
	scanCfg             ScanContext
}

func (e *EmitContext) pbModuleEmission(cfg ProtoPBConfig, protoInclude []VFS, spec *ProtoSpec) *PbModuleEmission {
	idx := 0

	if spec.modulePlugins {
		idx = 1
	}

	if e.pbEmissionOk[idx] {
		return &e.pbEmission[idx]
	}

	e.pbEmissionOk[idx] = true

	pe := &e.pbEmission[idx]
	ctx, d := e.ctx, e.d

	*pe = PbModuleEmission{
		liteHeaders:   !protoTransitiveHeadersEnabled(d),
		grpcCppBinary: pbGrpcCppVFS,
	}

	pe.protocLDRef, pe.protocBinary = ctx.tool(argContribToolsProtoc)
	pe.cppStyleguideLDRef, pe.cppStyleguideBinary = ctx.tool(argContribToolsProtocPluginsCppStyleguide)

	if cfg.grpc {
		pe.grpcCppLDRef, pe.grpcCppBinary = ctx.tool(argContribToolsProtocPluginsGrpcCpp)
	}

	if spec.modulePlugins {
		pe.extraPlugins = make([]ResolvedCPPProtoPlugin, 0, len(d.cppProtoPlugins))

		for _, plugin := range d.cppProtoPlugins {
			ldRef, binary := ctx.tool(internArg(plugin.ToolPath))

			pe.extraPlugins = append(pe.extraPlugins, ResolvedCPPProtoPlugin{
				Spec:   plugin,
				LDRef:  ldRef,
				Binary: binary,
			})
		}
	}

	na := ctx.na
	refs := na.noderefs.alloc(3 + len(pe.extraPlugins))[:0]

	refs = append(refs, pe.protocLDRef, pe.cppStyleguideLDRef)

	if cfg.grpc {
		refs = append(refs, pe.grpcCppLDRef)
	}

	for _, p := range pe.extraPlugins {
		if p.LDRef != 0 {
			refs = append(refs, p.LDRef)
		}
	}

	na.noderefs.commit(len(refs))
	pe.pbGenRefs = refs[:len(refs):len(refs)]

	if cfg.grpc {
		pe.grpcCCRefs = na.refList(pe.protocLDRef, pe.grpcCppLDRef)
		pe.grpcHRefs = na.refList(pe.grpcCppLDRef)
	}

	pe.blocks = composePBArgBlocks(ctx.emit.nodeArenas(), d.tc, pe.protocBinary, pe.cppStyleguideBinary, pe.grpcCppBinary,
		cfg.grpc, cfg.cppOutRoot, pe.liteHeaders,
		d.protocFlags, pe.extraPlugins, protoInclude)

	pe.searchPaths = d.cc.ProtoInclude

	if cfg.cppOutRoot != "" {
		pe.searchPaths = append([]VFS{source(cfg.cppOutRoot)}, d.cc.ProtoInclude...)
	}

	pe.scanCfg = protoWalkInputs(ctx.parsers, pe.searchPaths, e.instance.Path.relString())

	return pe
}

func (e *EmitContext) emitProtoPB(srcRel string, cfg ProtoPBConfig, pe *PbModuleEmission, spec *ProtoSpec) ProtoPBEmission {
	ctx, instance, d := e.ctx, e.instance, e.d
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcRel)
	buildProto := build(protoRelPath)
	protoVFS := source(protoRelPath)

	var protoSrcOverride VFS
	var extraProtoDeps []NodeRef
	var protoProducerSourceInputs []VFS
	var genProtoParsed []IncludeDirective

	if info := e.codegen.use(buildProto); info != nil {
		protoSrcOverride = buildProto
		protoVFS = buildProto
		extraProtoDeps = []NodeRef{info.ProducerRef}
		protoProducerSourceInputs = info.SourceInputs
		genProtoParsed = info.ParsedIncludes.bucket(parsedIncludesLocal)
	}

	transitiveImports := walkClosure(e.scanner, protoVFS, pe.scanCfg)
	pbRef := ctx.emit.reserve()
	pbm := *pe
	scanner := e.scanner

	pbPE := func() {
		imports := walkClosure(scanner, protoVFS, pbm.scanCfg)
		depRefs := resolveCodegenDepRefsInclView(ctx, instance, ctx.na, imports, extraProtoDeps...)

		emitPB(
			instance, protoRelPath, protoSrcOverride, pbm.cppStyleguideLDRef, pbm.protocLDRef,
			pbm.grpcCppLDRef, pbm.cppStyleguideBinary, pbm.protocBinary, pbm.grpcCppBinary,
			cfg.grpc, cfg.moduleTag,
			pbm.liteHeaders,
			pbm.extraPlugins,
			imports,
			depRefs,
			protoProducerSourceInputs,
			&pbm.blocks,
			spec,
			pbRef,
			ctx.emit,
		)
	}

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
			if out.relString() == grpcPbH.relString() || out.relString() == grpcPbCC.relString() {
				needsGRPCParsed = true

				break
			}
		}
	}

	directImports := protoInducedPbH(ctx.parsers, ctx.parsers.sourceParsedBuckets(source(protoRelPath), nil).bucket(parsedIncludesLocal), e.dirScratch[:0])

	if protoSrcOverride != 0 {
		directImports = protoInducedPbH(ctx.parsers, genProtoParsed, directImports[:0])
	}

	e.dirScratch = directImports

	if spec.genHParsed != nil {
		e.register(GeneratedFileInfo{
			OutputPath:     pbH,
			ProducerRef:    pbRef,
			GeneratorRefs:  spec.genRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: spec.genHParsed},
			ClosureLeaves:  spec.hLeaves,
			OnUse:          &pbPE,
		})

		ccParsed := e.ctx.na.dirs.alloc(len(spec.genHParsed) + len(spec.genCCExtras))
		cn := copy(ccParsed, spec.genHParsed)

		cn += copy(ccParsed[cn:], spec.genCCExtras)
		e.ctx.na.dirs.commit(cn)
		ccParsed = ccParsed[:cn:cn]

		var psc []ANY

		if p := d.perSrcCFlagsFor(internStr(srcRel).any()); p != nil {
			psc = *p
		}

		e.register(GeneratedFileInfo{
			OutputPath:     pbCC,
			ProducerRef:    pbRef,
			GeneratorRefs:  spec.genRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: ccParsed},
			ClosureLeaves:  spec.ccLeaves,
			Compile:        e.ctx.na.compileSpec(CompileSpec{CFlags: psc}),
			OnUse:          &pbPE,
		})

		return ProtoPBEmission{
			pbRef:     pbRef,
			pbCC:      pbCC,
			orderedCC: []VFS{pbCC},
			relPath:   protoRelPath,
		}
	}

	pbHImports := directImports

	if ext := e.d.cc.PbHCompanionExt; ext != "" {
		grown := appendPbHCompanions(directImports, directImports, ext)

		pbHImports = grown
		directImports = grown[:len(directImports):len(directImports)]
		e.dirScratch = grown
	}

	na := e.ctx.na
	extras := pbHEmitsIncludesExtras()
	pbHCompile := na.dirs.alloc(len(pbHImports) + len(extras) + transitiveImports.len())[:0]

	pbHCompile = append(pbHCompile, pbHImports...)
	pbHCompile = append(pbHCompile, extras...)

	eachBucketVFS(transitiveImports.buckets, func(ti VFS) {
		if ti.isBuild() {
			return
		}

		pbHCompile = append(pbHCompile, IncludeDirective{kind: includeQuoted, target: includeTarget(ti.rel().any())})
	})

	na.dirs.commit(len(pbHCompile))

	pbHCompile = pbHCompile[:len(pbHCompile):len(pbHCompile)]

	pbGenRefs := pe.pbGenRefs
	pbHLeaves := na.vfsList(source(protoRelPath))

	if protoSrcOverride != 0 {
		pbHLeaves = protoProducerSourceInputs
	}

	e.register(GeneratedFileInfo{
		OutputPath:     pbH,
		ProducerRef:    pbRef,
		GeneratorRefs:  pbGenRefs,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesCpp: pbHCompile},
		ClosureLeaves:  pbHLeaves,
		OnUse:          &pbPE,
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
			yaffHParsed = yaffGeneratedHeaderIncludes(na, plugin.isExperimental(protoBaseName), pbH.relString())
		}

		e.register(GeneratedFileInfo{
			OutputPath:     yaffH,
			ProducerRef:    pbRef,
			GeneratorRefs:  nil,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: yaffHParsed},
			OnUse:          &pbPE,
		})

		yaffCCParsed := na.dirs.alloc(len(yaffHParsed) + 1)
		yn := copy(yaffCCParsed, yaffHParsed)

		yaffCCParsed[yn] = IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())}
		na.dirs.commit(yn + 1)
		yaffCCParsed = yaffCCParsed[: yn+1 : yn+1]

		e.register(GeneratedFileInfo{
			OutputPath:     yaffCC,
			ProducerRef:    pbRef,
			GeneratorRefs:  pbGenRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: yaffCCParsed},
			OnUse:          &pbPE,
		})
	}

	if pe.liteHeaders {
		depsParsed := na.dirs.alloc(1 + len(directImports))[:0]

		depsParsed = append(depsParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())})
		depsParsed = append(depsParsed, directImports...)
		na.dirs.commit(len(depsParsed))
		depsParsed = depsParsed[:len(depsParsed):len(depsParsed)]

		e.register(GeneratedFileInfo{
			OutputPath:     pbDepsH,
			ProducerRef:    pbRef,
			GeneratorRefs:  pbGenRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: depsParsed},
			OnUse:          &pbPE,
		})
	}

	pbCCParsed := na.dirs.alloc(3 + len(directImports))[:0]

	pbCCParsed = append(pbCCParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())})

	if pe.liteHeaders {
		pbCCParsed = append(pbCCParsed, directImports...)
	}

	pbCCParsed = append(pbCCParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbWrapperVFS.rel().any())})
	na.dirs.commit(len(pbCCParsed))
	pbCCParsed = pbCCParsed[:len(pbCCParsed):len(pbCCParsed)]

	e.register(GeneratedFileInfo{
		OutputPath:     pbCC,
		ProducerRef:    pbRef,
		GeneratorRefs:  pbGenRefs,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: pbCCParsed},
		OnUse:          &pbPE,
	})

	var grpcCCParsed, grpcHParsed []IncludeDirective

	if needsGRPCParsed {
		grpcCCParsed = na.dirList(
			IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())},
			IncludeDirective{kind: includeQuoted, target: includeTarget(pbWrapperVFS.rel().any())})

		grpcHParsed = na.dirs.alloc(2 + len(directImports))[:0]
		grpcHParsed = append(grpcHParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())})
		grpcHParsed = append(grpcHParsed, directImports...)
		grpcHParsed = append(grpcHParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(internV(pbRuntimeBase, "google/protobuf/port_def.inc").any())})
		na.dirs.commit(len(grpcHParsed))
		grpcHParsed = grpcHParsed[:len(grpcHParsed):len(grpcHParsed)]
	}

	if cfg.grpc {
		e.register(GeneratedFileInfo{
			OutputPath:     grpcPbCC,
			ProducerRef:    pbRef,
			GeneratorRefs:  pe.grpcCCRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: grpcCCParsed},
			OnUse:          &pbPE,
		})

		e.register(GeneratedFileInfo{
			OutputPath:     grpcPbH,
			ProducerRef:    pbRef,
			GeneratorRefs:  pe.grpcHRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: grpcHParsed},
			OnUse:          &pbPE,
		})
	}

	orderedCC := e.orderedCC[:0]

	defer func() { e.orderedCC = orderedCC[:0] }()

	for _, out := range assembleProtoCmdOutputs(ctx.emit.nodeArenas(), protoBase, pbH, pbCC, pbDepsH, grpcPbCC, grpcPbH, pe.extraPlugins, pe.liteHeaders, cfg.grpc) {
		if isCCSourceExt(out.relString()) {
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

func (e *EmitContext) cppProtoPB(srcRel string, spec *ProtoSpec) ProtoPBEmission {
	d := e.d

	cfg := ProtoPBConfig{
		cppOutRoot: protoCPPOutRoot(d),
		grpc:       d.grpc && spec.modulePlugins,
		moduleTag:  d.cc.ModuleTag,
	}

	pe := e.pbModuleEmission(cfg, d.cc.ProtoIncludePeers, spec)

	return e.emitProtoPB(srcRel, cfg, pe, spec)
}

func appendPbHCompanions(dst []IncludeDirective, pbhImports []IncludeDirective, ext string) []IncludeDirective {
	for _, dir := range pbhImports {
		base, ok := strings.CutSuffix(dir.target.string(), ".pb.h")

		if !ok {
			continue
		}

		dst = append(dst, IncludeDirective{kind: dir.kind, target: includeTarget(internV(base, ext).any())})
	}

	return dst
}

func (e *EmitContext) emitCppProtoFamilySource(meta SrcMeta, spec *ProtoSpec) {
	pb := e.cppProtoPB(meta.Source.string(), spec)

	meta.Generated = true

	for _, cc := range pb.orderedCC {
		child := meta

		child.Source = cc.any()

		e.enqueueSrc(child)
	}

	e.markProtoPendingAR()
}

func (e *EmitContext) markProtoPendingAR() {
	d := e.d

	if d.moduleStmt.Name != tokProtoLibrary || e.protoRes != nil {
		return
	}

	protoLibName := ""

	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0].string()
	}

	e.protoResVal = ProtoSrcsResult{PendingAR: true, ProtoLibName: protoLibName}
	e.protoRes = &e.protoResVal
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
	transitiveProtoImports Closure,
	extraDepRefs []NodeRef,
	producerSourceInputs []VFS,
	blocks *PbArgBlocks,
	spec *ProtoSpec,
	id NodeRef,
	emit *StreamingEmitter,
) {
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

	var outputs []VFS

	if spec.ccFirstOuts {
		outputs = na.vfsList(pbCC, pbH)
	} else {
		outputs = assembleProtoCmdOutputs(na, protoBase, pbH, pbCC, pbDepsH, grpcPbCC, grpcPbH, extraPlugins, liteHeaders, grpc)
	}

	outsChunk := na.anyChunkVFS(outputs)
	relChunk := na.anyList(internStr(protoRelPath).any())
	chunks := na.chunks.alloc(6)[:0]

	chunks = append(chunks, blocks.head, outsChunk, blocks.mid, relChunk)

	if len(blocks.tail) > 0 {
		chunks = append(chunks, blocks.tail)
	}

	if len(spec.optsTail) > 0 {
		chunks = append(chunks, spec.optsTail)
	}

	na.chunks.commit(len(chunks))

	cmdArgs := ArgChunks(chunks[:len(chunks):len(chunks)])
	env := envVarsVCS
	inputs := na.vfs.alloc(5 + len(extraPlugins))[:0]

	inputs = append(inputs, cppStyleguideBinary)

	if grpc {
		inputs = append(inputs, grpcCppBinary)
	}

	inputs = append(inputs, protocBinary)

	for _, plugin := range extraPlugins {
		inputs = append(inputs, plugin.Binary)
	}

	if spec.toolBinary != 0 {
		inputs = append(inputs, spec.toolBinary)
	}

	inputs = append(inputs, pbWrapperVFS)
	inputs = append(inputs, srcVFS)
	na.vfs.commit(len(inputs))

	inputs = inputs[:len(inputs):len(inputs)]

	foreignDepRefs := na.noderefs.alloc(4 + len(extraPlugins))[:0]

	for _, r := range [3]NodeRef{cppStyleguideLDRef, grpcCppLDRef, protocLDRef} {
		if r != 0 {
			foreignDepRefs = append(foreignDepRefs, r)
		}
	}

	for _, plugin := range extraPlugins {
		if plugin.LDRef != 0 {
			foreignDepRefs = append(foreignDepRefs, plugin.LDRef)
		}
	}

	if spec.toolLDRef != 0 {
		foreignDepRefs = append(foreignDepRefs, spec.toolLDRef)
	}

	foreignDepRefs = dedupRefs(foreignDepRefs)
	na.noderefs.commit(len(foreignDepRefs))

	foreignDepRefs = foreignDepRefs[:len(foreignDepRefs):len(foreignDepRefs)]

	deps := na.noderefs.list(extraDepRefs...)
	protocCwd := "$(S)"

	if protoSrcOverride != 0 {
		protocCwd = "$(B)"
	}

	pbInputChunks := na.inputs.alloc(2 + len(transitiveProtoImports.buckets))[:0]

	pbInputChunks = append(pbInputChunks, inputs, producerSourceInputs)
	pbInputChunks = append(pbInputChunks, transitiveProtoImports.buckets...)
	na.inputs.commit(len(pbInputChunks))

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: cwdVFS(protocCwd),
			Env: env}),
		Env: env,

		Inputs:         pbInputChunks,
		Outputs:        outputs,
		KV:             spec.kv,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        deps,
		ForeignDepRefs: foreignDepRefs,
		Resources:      usesPython3,
	}

	emit.emitReservedNode(node, id)
}

func assembleProtoCmdOutputs(na *NodeArenas, protoBase string, pbH, pbCC, pbDepsH, grpcPbCC, grpcPbH VFS, extraPlugins []ResolvedCPPProtoPlugin, liteHeaders, grpc bool) []VFS {
	bound := 4

	for _, plugin := range extraPlugins {
		bound += len(plugin.Spec.OutputSuffixes)
	}

	outputs := na.vfs.alloc(bound)[:0]

	outputs = append(outputs, pbH)

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

	na.vfs.commit(len(outputs))

	return outputs[:len(outputs):len(outputs)]
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
	return filepath.ToSlash(filepath.Clean(resolvePySrcRel(fs, d.srcDirs, instance.Path, src).string()))
}

type PbArgBlocks struct {
	head []ANY
	mid  []ANY
	tail []ANY
}

func composePBArgBlocks(na *NodeArenas, tc ModuleToolchain, protocBinary, cppStyleguideBinary, grpcCppBinary VFS,
	grpc bool, cppOutRoot string, liteHeaders bool,
	extraProtocFlags []ANY, extraPlugins []ResolvedCPPProtoPlugin,
	protoInclude []VFS) PbArgBlocks {
	head := na.anyList(
		tc.Python3.any(),
		pbWrapperVFS.any(),
		argOutputs.any(),
	)

	includeRoot := ""

	if cppOutRoot != "" {
		includeRoot = cppOutRoot
	}

	mid := na.anys.alloc(12 + len(protoInclude) + len(extraProtocFlags))[:0]

	mid = append(mid,
		arg2.any(),
		(protocBinary).any(),
		internV("-I=./", includeRoot).any(),
		internV("-I=$(S)/", includeRoot).any(),
		argIB2.any(),
		argIS3.any(),
	)

	if cppOutRoot != "" {
		mid = append(mid, internV("-I=$(S)/", cppOutRoot).any())
	}

	for _, p := range protoInclude {
		mid = append(mid, internV("-I=", p.prefix(), p.relString()).any())
	}

	if liteHeaders {
		mid = append(mid,
			argIB2.any(),
			argISContribLibsProtobufSrc.any(),
			internV("--cpp_out=proto_h=true:$(B)/", cppOutRoot).any(),
		)
	} else {
		mid = append(mid,
			argIB2.any(),
			argISContribLibsProtobufSrc.any(),
			internV("--cpp_out=:$(B)/", cppOutRoot).any(),
		)
	}

	mid = appendAnyLists(mid, extraProtocFlags)

	mid = append(mid,
		internV("--cpp_styleguide_out=:$(B)/", cppOutRoot).any(),
		internV("--plugin=protoc-gen-cpp_styleguide=", cppStyleguideBinary.prefix(), cppStyleguideBinary.relString()).any(),
	)

	na.anys.commit(len(mid))

	mid = mid[:len(mid):len(mid)]

	tailBound := 2

	for _, plugin := range extraPlugins {
		tailBound += 2 + strings.Count(plugin.Spec.ExtraOutFlag, ",") + 1
	}

	tail := na.anys.alloc(tailBound)[:0]

	if grpc {
		tail = append(tail,
			internV("--plugin=protoc-gen-grpc_cpp=", grpcCppBinary.prefix(), grpcCppBinary.relString()).any(),
			internV("--grpc_cpp_out=$(B)/", cppOutRoot).any(),
		)
	}

	for _, plugin := range extraPlugins {
		tail = append(tail,
			internV("--plugin=protoc-gen-", plugin.Spec.Name, "=", plugin.Binary.prefix(), plugin.Binary.relString()).any(),
			internV("--", plugin.Spec.Name, "_out=$(B)/", cppOutRoot).any(),
		)

		for _, piece := range strings.Split(plugin.Spec.ExtraOutFlag, ",") {
			if piece == "" {
				continue
			}

			tail = append(tail, internV("--", plugin.Spec.Name, "_opt=", piece).any())
		}
	}

	na.anys.commit(len(tail))

	return PbArgBlocks{head: head, mid: mid, tail: tail[:len(tail):len(tail)]}
}
