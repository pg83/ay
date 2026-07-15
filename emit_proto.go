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

var yaffBaseRuntimeDirectives = quotedDirectives([]VFS{
	source(yaffRuntimeBase + "yaff.h"),
	source(yaffRuntimeBase + "struct.h"),
	source(yaffRuntimeBase + "protobuf.h"),
	source(yaffRuntimeBase + "reflect.h"),
})

var yaffExperimentsRuntimeDirectives = quotedDirectives([]VFS{
	source(yaffRuntimeBase + "experiments/serializer.h"),
	source(yaffRuntimeBase + "experiments/column.h"),
	source(yaffRuntimeBase + "experiments/merge.h"),
})

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
	n := len(yaffBaseRuntimeDirectives) + 1

	if experimental {
		n += len(yaffExperimentsRuntimeDirectives)
	}

	dirs := na.dirs.alloc(n)[:0]
	dirs = append(dirs, yaffBaseRuntimeDirectives...)

	dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(pbHRel).any())})

	if experimental {
		dirs = append(dirs, yaffExperimentsRuntimeDirectives...)
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
		} else if outputRoot == "" {
			dst = append(dst, d)

			continue
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
		name := d.target.string()
		nameID := d.target.str()

		if nameID == 0 || name == "" || !pathIsClean(name) {
			name = filepath.ToSlash(filepath.Clean(name))
			nameID = internStr(name)
		}

		pbH, ok := pm.protoParser().inducedHeader(nameID)

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
	cppOutRoot string
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
	pendingCommon       *protoPBCommon
}

type protoPBCommon struct {
	emit                *EmitContext
	cppStyleguideLDRef  NodeRef
	protocLDRef         NodeRef
	grpcCppLDRef        NodeRef
	cppStyleguideBinary VFS
	protocBinary        VFS
	grpcCppBinary       VFS
	grpc                bool
	extraPlugins        []ResolvedCPPProtoPlugin
	blocks              PbArgBlocks
}

type protoPBPending struct {
	common                    *protoPBCommon
	imports                   Closure
	protoRel                  STR
	protoSrc                  VFS
	protoProducerSourceInputs []VFS
	outputs                   []VFS
	spec                      *ProtoSpec
	pbRef                     NodeRef
}

func (p *protoPBPending) emitPending() {
	s := *p

	*p = protoPBPending{}

	c := s.common

	c.emit.emitPB(
		s.protoRel, s.protoSrc, c.cppStyleguideLDRef, c.protocLDRef,
		c.grpcCppLDRef, c.cppStyleguideBinary, c.protocBinary, c.grpcCppBinary,
		c.grpc, c.extraPlugins, s.imports,
		s.protoProducerSourceInputs, c.blocks, s.spec, s.outputs, s.pbRef,
	)
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
	pe.pendingCommon = na.protoPBC.one()
	*pe.pendingCommon = protoPBCommon{
		emit:               e,
		cppStyleguideLDRef: pe.cppStyleguideLDRef, protocLDRef: pe.protocLDRef, grpcCppLDRef: pe.grpcCppLDRef,
		cppStyleguideBinary: pe.cppStyleguideBinary, protocBinary: pe.protocBinary, grpcCppBinary: pe.grpcCppBinary,
		grpc:         cfg.grpc,
		extraPlugins: pe.extraPlugins, blocks: pe.blocks,
	}

	return pe
}

func (e *EmitContext) emitProtoPB(srcRel string, cfg ProtoPBConfig, pe *PbModuleEmission, spec *ProtoSpec) []VFS {
	ctx, d := e.ctx, e.d
	na := ctx.na
	protoRel := e.protoSourceRel(srcRel)
	protoRelPath := protoRel.string()
	buildProto := protoRel.build()
	protoVFS := protoRel.source()
	scanCtx := d.scanCtx

	generatedProto := false
	var protoProducerSourceInputs []VFS
	var genProtoParsed []IncludeDirective

	if info := e.codegen.use(buildProto); info != nil {
		generatedProto = true
		protoVFS = buildProto
		protoProducerSourceInputs = info.SourceInputs
		genProtoParsed = info.ParsedIncludes.bucket(parsedIncludesLocal)
	}

	transitiveImports := e.scanner.walkClosure(protoVFS, scanCtx, scanDomainProto)
	pbRef := ctx.emit.reserve()
	pending := na.protoPB.one()

	*pending = protoPBPending{
		common: pe.pendingCommon, imports: transitiveImports,
		protoRel: protoRel, protoSrc: protoVFS,
		protoProducerSourceInputs: protoProducerSourceInputs,
		spec:                      spec, pbRef: pbRef,
	}
	pbPE := na.pendingEmitter(pending)

	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	pbH := build(protoBase, ".pb.h")
	pbCC := build(protoBase, ".pb.cc")
	pbDepsH := build(protoBase, ".deps.pb.h")
	grpcPbH := build(protoBase, ".grpc.pb.h")
	grpcPbCC := build(protoBase, ".grpc.pb.cc")

	needsGRPCParsed := cfg.grpc

	if !needsGRPCParsed {
		for _, plugin := range d.cppProtoPlugins {
			for _, suffix := range plugin.OutputSuffixes {
				if suffix == ".grpc.pb.h" || suffix == ".grpc.pb.cc" {
					needsGRPCParsed = true

					break
				}
			}

			if needsGRPCParsed {
				break
			}
		}
	}

	if spec.ccFirstOuts {
		pending.outputs = na.vfsList(pbCC, pbH)
	} else {
		pending.outputs = assembleProtoCmdOutputs(na, protoBase, pbH, pbCC, pbDepsH, grpcPbCC, grpcPbH, pe.extraPlugins, pe.liteHeaders, cfg.grpc)
	}

	directImports := protoInducedPbH(ctx.parsers, ctx.parsers.sourceParsedBuckets(protoRel.source(), nil).bucket(parsedIncludesLocal), e.dirScratch[:0])

	if generatedProto {
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
			OnUse:          pbPE,
		})

		ccParsed := na.dirs.alloc(len(spec.genHParsed) + len(spec.genCCExtras))
		cn := copy(ccParsed, spec.genHParsed)

		cn += copy(ccParsed[cn:], spec.genCCExtras)
		na.dirs.commit(cn)
		ccParsed = ccParsed[:cn:cn]

		e.register(GeneratedFileInfo{
			OutputPath:     pbCC,
			ProducerRef:    pbRef,
			GeneratorRefs:  spec.genRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: ccParsed},
			ClosureLeaves:  spec.ccLeaves,
			OnUse:          pbPE,
		})

		return e.protoCCOutputs(pending.outputs)
	}

	pbHImports := directImports

	if ext := e.d.cc.PbHCompanionExt; ext != "" {
		grown := appendPbHCompanions(directImports, directImports, ext)

		pbHImports = grown
		directImports = grown[:len(directImports):len(directImports)]
		e.dirScratch = grown
	}

	extras := pbHEmitsIncludesExtras()
	pbHCompile := na.dirs.alloc(len(pbHImports) + len(extras) + transitiveImports.len())[:0]

	pbHCompile = append(pbHCompile, pbHImports...)
	pbHCompile = append(pbHCompile, extras...)

	for _, bucket := range transitiveImports.bucketList() {
		if bucket[0].isBuild() {
			continue
		}

		for _, ti := range bucket {
			pbHCompile = append(pbHCompile, IncludeDirective{kind: includeQuoted, target: includeTarget(ti.rel().any())})
		}
	}

	na.dirs.commit(len(pbHCompile))

	pbHCompile = pbHCompile[:len(pbHCompile):len(pbHCompile)]

	pbGenRefs := pe.pbGenRefs
	pbHLeaves := na.vfsList(protoRel.source())

	if generatedProto {
		pbHLeaves = protoProducerSourceInputs
	}

	e.register(GeneratedFileInfo{
		OutputPath:     pbH,
		ProducerRef:    pbRef,
		GeneratorRefs:  pbGenRefs,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesCpp: pbHCompile},
		ClosureLeaves:  pbHLeaves,
		OnUse:          pbPE,
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
			OnUse:          pbPE,
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
			OnUse:          pbPE,
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
			OnUse:          pbPE,
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
		OnUse:          pbPE,
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
			OnUse:          pbPE,
		})

		e.register(GeneratedFileInfo{
			OutputPath:     grpcPbH,
			ProducerRef:    pbRef,
			GeneratorRefs:  pe.grpcHRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: grpcHParsed},
			OnUse:          pbPE,
		})
	}

	return e.protoCCOutputs(pending.outputs)
}

func (e *EmitContext) protoCCOutputs(outputs []VFS) []VFS {
	orderedCC := e.orderedCC[:0]

	for _, out := range outputs {
		if isCCSourceExt(out.relString()) {
			orderedCC = append(orderedCC, out)
		}
	}

	e.orderedCC = orderedCC[:0]

	return orderedCC
}

func (e *EmitContext) cppProtoPB(srcRel string, spec *ProtoSpec) []VFS {
	d := e.d

	cfg := ProtoPBConfig{
		cppOutRoot: protoCPPOutRoot(d),
		grpc:       d.grpc && spec.modulePlugins,
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
	for _, cc := range e.cppProtoPB(e.moduleSourceName(meta.Source), spec) {
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

func (e *EmitContext) emitPyProtoLibraryResult() *ProtoSrcsResult {
	ctx, instance, d := e.ctx, e.instance, e.d
	entries := e.collectAllPyProtoResEntries()

	if len(entries) == 0 {
		return nil
	}

	var cppSibling *ModuleEmitResult

	if !moduleExcludesTag(d, "CPP_PROTO") {
		cppInstance := instance

		cppInstance.Language = LangCPP

		if cppInstance.Demand != demandNone {
			cppInstance.Demand = demandLinked
		}

		cppSibling = genModule(ctx, cppInstance)
	}

	genRefs, genOuts := e.packPyProtoResEntries(entries)

	if len(genRefs) == 0 {
		return nil
	}

	protoLibName := ""

	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0].string()
	}

	globalBaseName := globalArchiveNameWithPrefixOrName(instance.Path.relString(), d.unit.ARPrefix, protoLibName)
	gRef := emitARGlobalNamedTagged(instance, globalBaseName, d.unit.GlobalARTag, genRefs, genOuts, d.tc, ctx.host, ctx.emit)
	globalPath := build(instance.Path.relString(), "/", globalBaseName)
	result := &ProtoSrcsResult{GlobalRef: &gRef, GlobalPath: &globalPath}

	if cppSibling != nil && cppSibling.ARPath != nil {
		result.WholeArchiveRefs = append(result.WholeArchiveRefs, cppSibling.ARRef)
		result.WholeArchivePaths = append(result.WholeArchivePaths, *cppSibling.ARPath)
	} else if moduleExcludesTag(d, "CPP_PROTO") {
		result.WholeArchiveCmdPaths = append(result.WholeArchiveCmdPaths, build(instance.Path.relString(), "/", e.arName(instance.Path.relString(), "lib", "")))
	}

	return result
}

type ResolvedCPPProtoPlugin struct {
	Spec   CppProtoPlugin
	LDRef  NodeRef
	Binary VFS
}

func (e *EmitContext) emitPB(
	protoRel STR,
	srcVFS VFS,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	grpcCppLDRef NodeRef,
	cppStyleguideBinary VFS,
	protocBinary VFS,
	grpcCppBinary VFS,
	grpc bool,
	extraPlugins []ResolvedCPPProtoPlugin,
	transitiveProtoImports Closure,
	producerSourceInputs []VFS,
	blocks PbArgBlocks,
	spec *ProtoSpec,
	outputs []VFS,
	id NodeRef,
) {
	instance := e.instance
	na := e.ctx.na

	outsChunk := na.anyChunkVFS(outputs)
	relChunk := na.anyList(protoRel.any())
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

	toolEnd := len(inputs)

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

	protocCwd := srcRootDirVFS

	if srcVFS.isBuild() {
		protocCwd = bldRootDirVFS
	}

	transitiveBuckets := transitiveProtoImports.bucketList()
	pbInputChunks := na.inputs.alloc(4 + len(transitiveBuckets))[:0]

	pbInputChunks = append(pbInputChunks, inputs[:toolEnd:toolEnd])

	if srcVFS.isBuild() {
		pbInputChunks = append(pbInputChunks, inputs[toolEnd:toolEnd+1:toolEnd+1], inputs[toolEnd+1:])
	} else {
		pbInputChunks = append(pbInputChunks, inputs[toolEnd:])
	}

	pbInputChunks = append(pbInputChunks, producerSourceInputs)
	pbInputChunks = append(pbInputChunks, transitiveBuckets...)
	na.inputs.commit(len(pbInputChunks))

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: protocCwd,
			Env: env}),
		Env: env,

		Inputs:         pbInputChunks,
		Outputs:        outputs,
		KV:             spec.kv,
		ForeignDepRefs: foreignDepRefs,
		Resources:      usesPython3,
	}

	e.emitReservedNode(node, id)
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

func protoSourceRel(fs FS, instance ModuleInstance, d *ModuleData, src string) STR {
	resolved := resolvePySrcRel(fs, d.srcDirs, instance.Path, src)
	raw := resolved.string()

	if raw != "" && pathIsClean(raw) {
		return resolved
	}

	clean := filepath.ToSlash(filepath.Clean(raw))

	if clean == raw {
		return resolved
	}

	return internStr(clean)
}

const protoPathCacheSize = 64

type protoPathEntry struct {
	src string
	rel STR
}

func (e *EmitContext) protoSourceRel(src string) STR {
	for i := range e.protoPaths {
		if e.protoPaths[i].src == src {
			return e.protoPaths[i].rel
		}
	}

	rel := protoSourceRel(e.ctx.fs, e.instance, e.d, src)
	entry := protoPathEntry{src: src, rel: rel}

	if len(e.protoPaths) < protoPathCacheSize {
		e.protoPaths = append(e.protoPaths, entry)

		return rel
	}

	e.protoPaths[e.protoPathPos] = entry
	e.protoPathPos = (e.protoPathPos + 1) & (protoPathCacheSize - 1)

	return rel
}

func (e *EmitContext) protoSourceRelPath(src string) string {
	return e.protoSourceRel(src).string()
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
		protocBinary.any(),
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
