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

func yaffGeneratedHeaderIncludes(experimental bool, pbHRel string) []IncludeDirective {
	n := len(yaffBaseRuntimeHeaders) + 1

	if experimental {
		n += len(yaffExperimentsRuntimeHeaders)
	}

	dirs := make([]IncludeDirective, 0, n)

	for _, h := range yaffBaseRuntimeHeaders {
		dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(h).any())})
	}

	dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(pbHRel).any())})

	if experimental {
		for _, h := range yaffExperimentsRuntimeHeaders {
			dirs = append(dirs, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(h).any())})
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

		out = append(out, IncludeDirective{kind: d.kind, target: includeTarget(internStr(target).any())})
	}

	slices.SortFunc(out, func(a, b IncludeDirective) int { return strings.Compare(a.target.string(), b.target.string()) })

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

		out = append(out, IncludeDirective{kind: d.kind, target: includeTarget(pbH.any())})
	}

	slices.SortFunc(out, func(a, b IncludeDirective) int { return strings.Compare(a.target.string(), b.target.string()) })

	return out
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
	blocks              *PbArgBlocks
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

	if info := e.codegen.lookup(buildProto); info != nil {
		protoSrcOverride = buildProto
		protoVFS = buildProto
		extraProtoDeps = []NodeRef{info.ProducerRef}
		protoProducerSourceInputs = info.SourceInputs
		genProtoParsed = info.ParsedIncludes.bucket(parsedIncludesLocal)
	}

	transitiveImports := walkClosure(e.scanner, protoVFS, pe.scanCfg)

	extraProtoDeps = resolveCodegenDepRefsInclView(ctx, instance, ctx.na, transitiveImports, extraProtoDeps...)

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
		spec,
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
			if out.relString() == grpcPbH.relString() || out.relString() == grpcPbCC.relString() {
				needsGRPCParsed = true

				break
			}
		}
	}

	directImports := protoInducedPbH(ctx.parsers, ctx.parsers.sourceParsedBuckets(source(protoRelPath), nil).bucket(parsedIncludesLocal))

	if protoSrcOverride != 0 {
		directImports = protoInducedPbH(ctx.parsers, genProtoParsed)
	}

	if spec.genHParsed != nil {
		reg := e.codegen

		reg.register(&GeneratedFileInfo{
			OutputPath:     pbH,
			ProducerRef:    pbRef,
			GeneratorRefs:  spec.genRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: spec.genHParsed},
			ClosureLeaves:  spec.hLeaves,
		})

		ccParsed := concat(spec.genHParsed, spec.genCCExtras)

		var psc []ANY

		if p := d.perSrcCFlagsFor(internStr(srcRel).any()); p != nil {
			psc = *p
		}

		reg.register(&GeneratedFileInfo{
			OutputPath:     pbCC,
			ProducerRef:    pbRef,
			GeneratorRefs:  spec.genRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: ccParsed},
			ClosureLeaves:  spec.ccLeaves,
			Compile:        &CompileSpec{FlatOutput: d.flatSrc(internStr(srcRel).any()), CFlags: psc},
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
		pbHImports = concat(directImports, pbHCompanionDirectives(directImports, ext))
	}

	extras := pbHEmitsIncludesExtras()
	pbHCompile := make([]IncludeDirective, 0, len(pbHImports)+len(extras)+transitiveImports.len())

	pbHCompile = append(pbHCompile, pbHImports...)
	pbHCompile = append(pbHCompile, extras...)

	eachBucketVFS(transitiveImports.buckets, func(ti VFS) {
		if ti.isBuild() {
			return
		}

		pbHCompile = append(pbHCompile, IncludeDirective{kind: includeQuoted, target: includeTarget(ti.rel().any())})
	})

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
		ParsedIncludes: ParsedIncludeSet{parsedIncludesCpp: pbHCompile},
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
			yaffHParsed = yaffGeneratedHeaderIncludes(plugin.isExperimental(protoBaseName), pbH.relString())
		}

		reg.register(&GeneratedFileInfo{
			OutputPath:     yaffH,
			ProducerRef:    pbRef,
			GeneratorRefs:  nil,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: yaffHParsed},
		})

		yaffCCParsed := append(append([]IncludeDirective(nil), yaffHParsed...),
			IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())})

		reg.register(&GeneratedFileInfo{
			OutputPath:     yaffCC,
			ProducerRef:    pbRef,
			GeneratorRefs:  pbGenRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: yaffCCParsed},
		})
	}

	if pe.liteHeaders {
		depsParsed := make([]IncludeDirective, 0, 1+len(directImports))

		depsParsed = append(depsParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())})
		depsParsed = append(depsParsed, directImports...)

		reg.register(&GeneratedFileInfo{
			OutputPath:     pbDepsH,
			ProducerRef:    pbRef,
			GeneratorRefs:  pbGenRefs,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: depsParsed},
		})
	}

	pbCCParsed := make([]IncludeDirective, 0, 3+len(directImports))

	pbCCParsed = append(pbCCParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())})

	if pe.liteHeaders {
		pbCCParsed = append(pbCCParsed, directImports...)
	}

	pbCCParsed = append(pbCCParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbWrapperVFS.rel().any())})

	reg.register(&GeneratedFileInfo{
		OutputPath:     pbCC,
		ProducerRef:    pbRef,
		GeneratorRefs:  pbGenRefs,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: pbCCParsed},
	})

	var grpcCCParsed, grpcHParsed []IncludeDirective

	if needsGRPCParsed {
		grpcCCParsed = make([]IncludeDirective, 0, 2)
		grpcCCParsed = append(grpcCCParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())})
		grpcCCParsed = append(grpcCCParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbWrapperVFS.rel().any())})

		grpcHParsed = make([]IncludeDirective, 0, 3+len(directImports))
		grpcHParsed = append(grpcHParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(pbH.rel().any())})
		grpcHParsed = append(grpcHParsed, directImports...)
		grpcHParsed = append(grpcHParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(internV(pbRuntimeBase, "google/protobuf/port_def.inc").any())})
	}

	if cfg.grpc {
		reg.register(&GeneratedFileInfo{
			OutputPath:     grpcPbCC,
			ProducerRef:    pbRef,
			GeneratorRefs:  []NodeRef{pe.protocLDRef, pe.grpcCppLDRef},
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: grpcCCParsed},
		})

		reg.register(&GeneratedFileInfo{
			OutputPath:     grpcPbH,
			ProducerRef:    pbRef,
			GeneratorRefs:  []NodeRef{pe.grpcCppLDRef},
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: grpcHParsed},
		})
	}

	orderedCC := make([]VFS, 0, 2+len(extraOutputPaths))

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

func pbHCompanionDirectives(pbhImports []IncludeDirective, ext string) []IncludeDirective {
	var out []IncludeDirective

	for _, dir := range pbhImports {
		base, ok := strings.CutSuffix(dir.target.string(), ".pb.h")

		if !ok {
			continue
		}

		out = append(out, IncludeDirective{kind: dir.kind, target: includeTarget(internV(base, ext).any())})
	}

	return out
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

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: cwdVFS(protocCwd),
			Env: env}),
		Env: env,

		Inputs:         na.inputList(inputs, append([][]VFS{producerSourceInputs}, transitiveProtoImports.buckets...)...),
		Outputs:        outputs,
		KV:             spec.kv,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        deps,
		ForeignDepRefs: foreignDepRefs,
		Resources:      usesPython3,
	}

	return emit.emitNode(node)
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
	protoInclude []VFS) *PbArgBlocks {
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

	return &PbArgBlocks{head: head, mid: mid, tail: tail[:len(tail):len(tail)]}
}
