package main

import (
	"path/filepath"
	"sort"
	"strings"
)

var (
	// Parsed-include directives for the constant protobuf/grpc/event runtime header
	// lists, built once at init instead of re-interning each header's Rel() per
	// generated output (was ~260k internStr/run on sg5: the 187-entry deep list
	// times every pb.cc/grpc.pb.cc). append copies these into the per-output slice,
	// so sharing the read-only backing is safe.
	protobufRuntimeDirectives      = quotedDirectives(protobufRuntimeHeaders)
	pbDescriptorImporterDirectives = quotedDirectives(pbDescriptorImporterHeaders)
	// pbRuntimeBaseVFS is the protobuf runtime src root (upstream's
	// $PROTOBUF_INCLUDE_PATH config constant) fed to the proto closure walk.
	pbRuntimeBaseVFS = source(strings.TrimSuffix(pbRuntimeBase, "/"))
)

func quotedDirectives(headers []VFS) []IncludeDirective {
	out := make([]IncludeDirective, len(headers))

	for i, h := range headers {
		out[i] = IncludeDirective{kind: includeQuoted, target: internStr(h.rel())}
	}

	return out
}

func protoPbHIncludes(pm *IncludeParserManager, srcRel, outputRoot string, bucket ParsedIncludeBucket) []IncludeDirective {
	hcpp := pm.sourceParsedBuckets(source(srcRel), nil).bucket(bucket)

	if len(hcpp) == 0 {
		return nil
	}

	out := make([]IncludeDirective, 0, len(hcpp))

	for _, d := range hcpp {
		target := d.target.string()

		if strings.HasPrefix(target, "google/protobuf/") && strings.HasSuffix(target, ".pb.h") {
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

func pbHEmitsIncludesExtras() []IncludeDirective {
	out := make([]IncludeDirective, 0, len(pbDescriptorImporterDirectives)+1)
	out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})
	out = append(out, pbDescriptorImporterDirectives...)

	return out
}

// protoWalkInputs builds the scan inputs for closing over .proto imports —
// the FOR-proto addincl data fed to the scanner's STANDARD resolution,
// mirroring protoc's -I set: the module's _PROTO__INCLUDE chain (own
// PROTO_NAMESPACE included — upstream's PROTO_ADDINCL is always GLOBAL) plus
// the protobuf runtime src ($PROTOBUF_INCLUDE_PATH, a contour config
// constant).
func protoWalkInputs(pm *IncludeParserManager, peerProtoAddIncl []VFS, ownerModuleDir string) ModuleCCInputs {
	own := make([]VFS, 0, 1+len(peerProtoAddIncl))
	own = append(own, pbRuntimeBaseVFS)
	own = append(own, peerProtoAddIncl...)

	return ModuleCCInputs{AddIncl: own, ScanCfg: newScanContext(pm, own, nil, includeScannerBasePaths(), ownerModuleDir)}
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

func resolveProtoImportPath(fs FS, importedRel string, peerProtoAddIncl []VFS) string {
	clean := filepath.ToSlash(filepath.Clean(importedRel))
	rootCands := []string{clean}

	if !strings.HasPrefix(clean, "yt/") {
		rootCands = append(rootCands, filepath.ToSlash(filepath.Clean("yt/"+clean)))
	}

	rootCands = append(rootCands, filepath.ToSlash(filepath.Clean(pbRuntimeBase+clean)))

	for _, cand := range rootCands {
		if fs.isFile(srcRootVFS, cand) {
			return cand
		}
	}

	// Peer PROTO_NAMESPACE / PROTO_LIBRARY contributions land in protoc's -I
	// flags (peerProtoAddIncl); mirror that here so transitive .proto inputs
	// resolve through the same search prefix protoc does (e.g. opentelemetry's
	// `import "opentelemetry/proto/common/v1/common.proto"` finds the file at
	// $(S)/contrib/libs/opentelemetry-proto/opentelemetry/proto/common/v1/common.proto
	// via the `contrib/libs/opentelemetry-proto` -I). p is already a VFS, so it
	// keys Listdir directly — no per-candidate concat or re-intern.
	for _, p := range peerProtoAddIncl {
		if p.root() != VFSRootSource {
			continue
		}

		if fs.isFile(p, clean) {
			return filepath.ToSlash(filepath.Clean(p.rel() + "/" + clean))
		}
	}

	return ""
}

func protoOutputRel(outputRoot, rel string) string {
	if outputRoot == "" {
		return rel
	}

	return filepath.ToSlash(filepath.Clean(outputRoot + "/" + rel))
}

func protoTransitiveHeadersEnabled(d *ModuleData) bool {
	if d != nil {
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
	}

	return true
}

type ProtoPBConfig struct {
	grpc                       bool
	moduleTag                  STR
	cppOutRoot                 string
	duplicateOutputRootInclude bool
}

type ProtoPBEmission struct {
	pbRef         NodeRef
	pbCC          VFS
	grpcPbCC      VFS
	extraSourceCC []VFS
	relPath       string
}

// pbModuleEmission is the per-module proto emission context: the resolved
// tool refs/binaries and the stable protoc arg blocks. Built ONCE per module
// proto context — emitCPPProtoSrcs before its source loop,
// emitLibraryProtoSource for its single source — and shared by every PB node
// it emits.
type PbModuleEmission struct {
	protocLDRef        NodeRef
	cppStyleguideLDRef NodeRef
	grpcCppLDRef       NodeRef

	protocBinary        VFS
	cppStyleguideBinary VFS
	grpcCppBinary       VFS

	liteHeaders  bool
	extraPlugins []ResolvedCPPProtoPlugin

	blocks *PbArgBlocks
}

func newPBModuleEmission(ctx *GenCtx, d *ModuleData, cfg ProtoPBConfig, peerProtoAddIncl []VFS, protoNamespaceTail []VFS) *PbModuleEmission {
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
		cfg.grpc, cfg.moduleTag, cfg.cppOutRoot, cfg.duplicateOutputRootInclude, pe.liteHeaders,
		d.protocFlags, pe.extraPlugins, peerProtoAddIncl, protoNamespaceTail)

	return pe
}

func emitProtoPB(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, cfg ProtoPBConfig, pe *PbModuleEmission, peerProtoAddIncl []VFS) ProtoPBEmission {
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcRel)
	// Search transitive .proto imports through the same -I prefixes protoc
	// receives: the own PROTO_NAMESPACE (cppOutRoot) plus every peer-contributed
	// proto namespace. Without the own namespace, opentelemetry-proto's
	// `import "opentelemetry/proto/common/v1/common.proto"` from resource.proto
	// would not resolve, even though protoc handles it via -I=$(S)/cppOutRoot.
	protoSearchPaths := peerProtoAddIncl

	if cfg.cppOutRoot != "" {
		protoSearchPaths = append([]VFS{source(cfg.cppOutRoot)}, peerProtoAddIncl...)
	}

	protoVFS := source(protoRelPath)
	transitiveImports := walkClosureTail(ctx.scannerFor(instance), protoVFS, protoWalkInputs(ctx.parsers, protoSearchPaths, instance.Path.rel()).ScanCfg)

	// SRCS(X.proto) may name a build-generated .proto (e.g. jsonpath's
	// RUN_ANTLR -language protobuf emits JsonPathParser.proto with no source
	// committed). Without rewiring, EmitPB would feed protoc the source-rooted
	// path and miss the producer dep, leaving the JV(.proto) unreachable from
	// the LD root after finalize-DFS. Look the proto up in the codegen
	// registry: if present, swap srcVFS to the build path and pin the
	// producer ref as a PB dep.
	var protoSrcOverride VFS
	var extraProtoDeps []NodeRef
	var protoProducerSourceInputs []VFS

	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		buildProto := build(protoRelPath)

		if info := reg.lookup(buildProto); info != nil && info.HasProducerRef {
			protoSrcOverride = buildProto
			extraProtoDeps = []NodeRef{info.ProducerRef}
			protoProducerSourceInputs = info.SourceInputs
		}
	}

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
	pbH := build(protoBase + ".pb.h")
	pbCC := build(protoBase + ".pb.cc")
	pbDepsH := build(protoBase + ".deps.pb.h")
	grpcPbH := build(protoBase + ".grpc.pb.h")
	grpcPbCC := build(protoBase + ".grpc.pb.cc")
	extraOutputPaths := make([]VFS, 0, 4)
	extraSourceOutputs := make([]VFS, 0, 2)

	for _, plugin := range d.cppProtoPlugins {
		for _, suffix := range plugin.OutputSuffixes {
			out := build(protoBase + suffix)
			extraOutputPaths = append(extraOutputPaths, out)

			if isCCSourceExt(out.rel()) {
				extraSourceOutputs = append(extraSourceOutputs, out)
			}
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

	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		directImports := protoDirectPbHIncludes(ctx.parsers, protoRelPath, cfg.cppOutRoot)
		pbHImports := directImports
		extras := pbHEmitsIncludesExtras()
		pbHParsed := make([]IncludeDirective, 0, len(pbHImports)+len(extras)+len(transitiveImports))
		pbHParsed = append(pbHParsed, pbHImports...)
		pbHParsed = append(pbHParsed, extras...)

		for _, ti := range transitiveImports {
			pbHParsed = append(pbHParsed, IncludeDirective{kind: includeQuoted, target: internStr(ti.rel())})
		}

		// protoc induces the protobuf runtime headers; for a grpc service the
		// grpc_cpp plugin induces the grpcpp service headers too. Both via
		// GeneratorRefs (type-split by output kind) instead of hand-woven lists.
		pbGenRefs := []NodeRef{pe.protocLDRef, pe.cppStyleguideLDRef}

		if cfg.grpc {
			pbGenRefs = append(pbGenRefs, pe.grpcCppLDRef)
		}

		for _, p := range pe.extraPlugins {
			if p.LDRef != NodeRef(0) {
				pbGenRefs = append(pbGenRefs, p.LDRef)
			}
		}

		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, pbH, pbHParsed, pbRef, pbGenRefs)

		// The source a generated header is produced FROM is a real input of every
		// unit that includes that header — a generated-from edge, not a C++ include.
		// Ride it as a non-expanded closure leaf of pb.h (everything reaches pb.h:
		// pb.cc/grpc include it, consumers include it), instead of the old fake
		// `#include "X.proto"` that also dragged the $(B) generated .proto into the
		// closure. For a real $(S) .proto that source is the .proto itself; for a
		// generated $(B) .proto (protoSrcOverride != 0) the .proto is a codegen
		// intermediate (reached via the producer dep edge), so ride the generator's
		// own $(S) sources instead — the grammar/template/tool/scripts that
		// produced it (protoProducerSourceInputs = the $(B) .proto's SourceInputs).
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			if protoSrcOverride == 0 {
				reg.addClosureLeaf(pbH, source(protoRelPath))
			} else {
				for _, s := range protoProducerSourceInputs {
					reg.addClosureLeaf(pbH, s)
				}
			}
		}

		if pe.liteHeaders {
			depsParsed := make([]IncludeDirective, 0, 1+len(directImports))
			depsParsed = append(depsParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbH.rel())})
			depsParsed = append(depsParsed, directImports...)
			registerBoundGeneratedParsedOutput(ctx, instance, pkPB, pbDepsH, depsParsed, pbRef, pbGenRefs)
		}

		pbCCParsed := make([]IncludeDirective, 0, 3+len(directImports))
		pbCCParsed = append(pbCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbH.rel())})

		if pe.liteHeaders {
			pbCCParsed = append(pbCCParsed, directImports...)
		}

		pbCCParsed = append(pbCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})

		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, pbCC, pbCCParsed, pbRef, pbGenRefs)

		var grpcCCParsed, grpcHParsed []IncludeDirective

		if needsGRPCParsed {
			grpcCCParsed = make([]IncludeDirective, 0, 2)
			grpcCCParsed = append(grpcCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbH.rel())})
			grpcCCParsed = append(grpcCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.rel())})

			grpcHParsed = make([]IncludeDirective, 0, 3+len(directImports))
			grpcHParsed = append(grpcHParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbH.rel())})
			grpcHParsed = append(grpcHParsed, directImports...)
			grpcHParsed = append(grpcHParsed, IncludeDirective{kind: includeQuoted, target: internStr(pbRuntimeBase + "google/protobuf/port_def.inc")})
		}

		if cfg.grpc {
			registerBoundGeneratedParsedOutput(ctx, instance, pkPB, grpcPbCC, grpcCCParsed, pbRef, []NodeRef{pe.protocLDRef, pe.grpcCppLDRef})
			registerBoundGeneratedParsedOutput(ctx, instance, pkPB, grpcPbH, grpcHParsed, pbRef, []NodeRef{pe.grpcCppLDRef})
		}
	}

	return ProtoPBEmission{
		pbRef:         pbRef,
		pbCC:          pbCC,
		grpcPbCC:      grpcPbCC,
		extraSourceCC: extraSourceOutputs,
		relPath:       protoRelPath,
	}
}

func emitProtoSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs) *ProtoSrcsResult {
	var protoSrcs, evSrcs []string

	for _, src := range d.srcs {
		switch {
		case strings.HasSuffix(src.string(), ".proto"):
			protoSrcs = append(protoSrcs, src.string())
		case strings.HasSuffix(src.string(), ".ev"):
			evSrcs = append(evSrcs, src.string())
		}
	}

	if len(protoSrcs) == 0 && len(evSrcs) == 0 {
		return nil
	}

	switch instance.Language {
	case LangPy:
		return emitPyProtoSrcs(ctx, instance, d, peerContribs, protoSrcs, evSrcs)
	default:
		return emitCPPProtoSrcs(ctx, instance, d, peerContribs, protoSrcs, evSrcs)
	}
}

func emitCPPProtoSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs, protoSrcs, evSrcs []string) *ProtoSrcsResult {
	type protoCodegenOutput struct {
		genRef NodeRef
		pbCC   VFS
		srcRel string
	}

	var codegenOutputs []protoCodegenOutput
	codegenOutputSeen := make(map[STR]struct{})
	appendCodegenOutput := func(genRef NodeRef, pbCC VFS, srcRel string) {
		if _, dup := codegenOutputSeen[internStr(pbCC.rel())]; dup {
			return
		}

		codegenOutputSeen[internStr(pbCC.rel())] = struct{}{}
		codegenOutputs = append(codegenOutputs, protoCodegenOutput{
			genRef: genRef,
			pbCC:   pbCC,
			srcRel: srcRel,
		})
	}
	cfg := ProtoPBConfig{
		grpc:       d.grpc,
		moduleTag:  tagCppProto,
		cppOutRoot: protoCPPOutRoot(d),
	}

	if cfg.cppOutRoot != "" {
		cfg.duplicateOutputRootInclude = containsVFS(peerContribs.addIncl, build(cfg.cppOutRoot))
	}

	cppInstance := instance
	cppInstance.Path = protoCPPModulePath(instance, d)

	pe := newPBModuleEmission(ctx, d, cfg, peerContribs.protoAddIncl, peerContribs.protoNamespaceTail)

	for _, src := range protoSrcs {
		pb := emitProtoPB(ctx, instance, d, src, cfg, pe, peerContribs.protoAddIncl)

		ccSrcRel := strings.TrimPrefix(pb.pbCC.rel(), cppInstance.Path.rel()+"/")
		appendCodegenOutput(pb.pbRef, pb.pbCC, ccSrcRel)

		if d.grpc {
			grpcSrcRel := strings.TrimPrefix(pb.grpcPbCC.rel(), cppInstance.Path.rel()+"/")
			appendCodegenOutput(pb.pbRef, pb.grpcPbCC, grpcSrcRel)
		}

		for _, extraSrc := range pb.extraSourceCC {
			extraSrcRel := strings.TrimPrefix(extraSrc.rel(), cppInstance.Path.rel()+"/")
			appendCodegenOutput(pb.pbRef, extraSrc, extraSrcRel)
		}
	}

	if len(evSrcs) > 0 {
		protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
		cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
		event2cppLDRef, event2cppBinary := ctx.tool(argToolsEvent2cpp)

		for _, src := range evSrcs {
			evRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
			evVFS := source(evRelPath)
			evImports := walkClosureTail(ctx.scannerFor(instance), evVFS, protoWalkInputs(ctx.parsers, nil, instance.Path.rel()).ScanCfg)

			evRef := emitEV(
				instance, evRelPath, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				tagCppProto, evImports, d.tc, ctx.emit)

			evH := build(evRelPath + ".pb.h")
			evPbCC := build(evRelPath + ".pb.cc")

			if reg := codegenRegForInstance(ctx, instance); reg != nil {
				directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, protoCPPOutRoot(d))
				evExtras := evWitnessExtras(evRelPath, evPbCC)
				evHParsed := make([]IncludeDirective, 0, len(directImports)+len(protobufRuntimeHeaders)+len(evExtras))
				evHParsed = append(evHParsed, directImports...)
				evHParsed = append(evHParsed, protobufRuntimeDirectives...)
				evHParsed = append(evHParsed, evExtras...)
				registerBoundGeneratedParsedOutput(ctx, instance, pkEV, evH, evHParsed, evRef, []NodeRef{event2cppLDRef})

				evCCParsed := make([]IncludeDirective, 0, 1+len(protobufRuntimeHeaders))
				evCCParsed = append(evCCParsed, IncludeDirective{kind: includeQuoted, target: internStr(evH.rel())})
				evCCParsed = append(evCCParsed, protobufRuntimeDirectives...)

				registerBoundGeneratedParsedOutput(ctx, instance, pkEV, evPbCC, evCCParsed, evRef, []NodeRef{event2cppLDRef})
			}

			cppInstance := instance
			cppInstance.Path = protoCPPModulePath(instance, d)
			evSrcRel := strings.TrimPrefix(evRelPath+".pb.cc", cppInstance.Path.rel()+"/")
			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef: evRef,
				pbCC:   evPbCC,
				srcRel: evSrcRel,
			})
		}
	}

	if d.moduleStmt.Name != tokProtoLibrary || len(codegenOutputs) == 0 {
		return nil
	}

	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	moduleInputs := ModuleCCInputs{
		TC:                   d.tc,
		InclArgs:             ctx.inclArgs,
		Flags:                d.flags,
		AddIncl:              d.addIncl,
		PeerAddInclGlobal:    peerContribs.addIncl,
		ScanCfg:              newScanContext(ctx.parsers, d.addIncl, peerContribs.addIncl, includeScannerBasePaths(), instance.Path.rel()),
		CFlags:               d.cFlags,
		CXXFlags:             d.cxxFlags,
		COnlyFlags:           d.cOnlyFlags,
		OwnCFlagsGlobal:      ownCFlagsGlobalSelf,
		OwnCXXFlagsGlobal:    ownCXXFlagsGlobalSelf,
		OwnCOnlyFlagsGlobal:  ownCOnlyFlagsGlobalSelf,
		PeerCFlagsGlobal:     peerContribs.cFlags,
		PeerCXXFlagsGlobal:   peerContribs.cxxFlags,
		PeerCOnlyFlagsGlobal: peerContribs.cOnlyFlags,
		ModuleScopeCFlags:    d.moduleScopeCFlags,
		SrcDirs:              d.srcDirs,
		SourceRoot:           ctx.sourceRoot,
		FS:                   ctx.fs,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		SetVars:              d.setVars,
		ModuleTag:            tagCppProto,
	}
	moduleInputs.CCBlocks = composeCCModuleArgBlocks(cppInstance.Platform, &moduleInputs)

	ccRefs := make([]NodeRef, 0, len(codegenOutputs))
	ccOutputs := make([]VFS, 0, len(codegenOutputs))

	wireFormatVFS := source(pbRuntimeBase + "google/protobuf/wire_format.h")

	for _, co := range codegenOutputs {
		ccIn := moduleInputs
		ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), co.pbCC, moduleInputs.ScanCfg)

		if strings.HasSuffix(co.srcRel, ".ev.pb.cc") {
			selfH := build(strings.TrimSuffix(co.pbCC.rel(), ".cc") + ".h")
			filtered := make([]VFS, 0, len(ccIn.IncludeInputs))

			for _, in := range ccIn.IncludeInputs {
				if in == selfH {
					continue
				}

				filtered = append(filtered, in)
			}

			ccIn.IncludeInputs = filtered
		}

		if strings.HasSuffix(co.srcRel, ".ev.pb.cc") {
			ccIn.IncludeInputs = append(ccIn.IncludeInputs, wireFormatVFS)
		}

		ccIn.ExtraDepRefs = append([]NodeRef{co.genRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, co.genRef)...)
		ccRef, ccOut, _ := emitCC(cppInstance, co.srcRel, co.pbCC, ccIn, ctx.host, ctx.emit)
		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)
	}

	enRes := emitEnumSrcs(ctx, instance, d, peerContribs.addIncl, &moduleInputs)

	if enRes != nil {
		ccRefs = append(ccRefs, enRes.CCRefs...)
		ccOutputs = append(ccOutputs, enRes.CCOutputs...)
	}

	// RUN_ANTLR(... OUT *.cpp ...) inside a PROTO_LIBRARY's IF(GEN_PROTO)
	// block: upstream auto-promotes those .cpp outputs to SRCS. Compile each
	// here and archive the .o alongside .pb.cc.o (jsonpath:
	// JsonPathParser.cpp / JsonPathLexer.cpp from the second RUN_ANTLR land
	// in libproto_ast-gen-jsonpath.a).
	//
	// These ANTLR .cpp objects are ordinary translation units (the "regular"
	// archive phase) and upstream orders them BEFORE the proto .pb.cc.o objects
	// (the proto-codegen phase): the reference jsonpath AR is
	// [JsonPathParser.cpp.o, JsonPathLexer.cpp.o, JsonPathParser.pb.cc.o].
	// Collect them separately and prepend, leaving the proto/enum objects built
	// above in their existing relative order.
	var antlrRefs []NodeRef
	var antlrOutputs []VFS

	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		for _, run := range d.antlrRuns {
			for _, outTok := range run.OUTFiles {
				if !isCCSourceExt(outTok.string()) {
					continue
				}

				outVFS := copyFileOutputVFS(instance.Path.rel(), outTok.string())
				info := reg.lookup(outVFS)

				if info == nil || !info.HasProducerRef {
					continue
				}

				cppRel := antlrOutputModuleRel(instance.Path.rel(), outVFS)
				ccRef, ccOut := emitCodegenDownstreamCC(ctx, cppInstance, cppRel, []NodeRef{info.ProducerRef}, moduleInputs)
				antlrRefs = append(antlrRefs, ccRef)
				antlrOutputs = append(antlrOutputs, ccOut)
			}
		}
	}

	if len(antlrRefs) > 0 {
		ccRefs = append(antlrRefs, ccRefs...)
		ccOutputs = append(antlrOutputs, ccOutputs...)
	}

	var protoLibName string

	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0].string()
	}

	arBaseName := archiveNameWithPrefixOrName(instance.Path.rel(), "lib", protoLibName)
	archivePath := build(instance.Path.rel() + "/" + arBaseName)
	arRef := emitARNode(instance, archivePath, tagCppProto, ccRefs, ccOutputs, nil, nil, d.tc, ctx.host, ctx.emit)

	return &ProtoSrcsResult{ARRef: arRef, ARPath: &archivePath}
}
