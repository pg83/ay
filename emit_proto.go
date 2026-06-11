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
	pbRuntimeBaseVFS = Source(strings.TrimSuffix(pbRuntimeBase, "/"))
)

func quotedDirectives(headers []VFS) []includeDirective {
	out := make([]includeDirective, len(headers))

	for i, h := range headers {
		out[i] = includeDirective{kind: includeQuoted, target: internStr(h.Rel())}
	}

	return out
}

func protoPbHIncludes(pm *includeParserManager, srcRel, outputRoot string, bucket parsedIncludeBucket) []includeDirective {
	hcpp := pm.sourceParsedBuckets(Source(srcRel), nil).bucket(bucket)

	if len(hcpp) == 0 {
		return nil
	}

	out := make([]includeDirective, 0, len(hcpp))

	for _, d := range hcpp {
		target := d.target.String()

		if strings.HasPrefix(target, "google/protobuf/") && strings.HasSuffix(target, ".pb.h") {
			target = pbRuntimeBase + target
		} else {
			target = protoOutputRel(outputRoot, target)
		}

		out = append(out, includeDirective{kind: d.kind, target: internStr(target)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].target.String() < out[j].target.String() })
	return out
}

func protoDirectPbHIncludes(pm *includeParserManager, srcRel, outputRoot string) []includeDirective {
	return protoPbHIncludes(pm, srcRel, outputRoot, parsedIncludesHeader)
}

func pbHEmitsIncludesExtras() []includeDirective {
	out := make([]includeDirective, 0, len(pbDescriptorImporterDirectives)+1)
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.Rel())})
	out = append(out, pbDescriptorImporterDirectives...)

	return out
}

// protoWalkInputs builds the scan inputs for closing over .proto imports —
// the FOR-proto addincl data fed to the scanner's STANDARD resolution,
// mirroring protoc's -I set: the module's _PROTO__INCLUDE chain (own
// PROTO_NAMESPACE included — upstream's PROTO_ADDINCL is always GLOBAL) plus
// the protobuf runtime src ($PROTOBUF_INCLUDE_PATH, a contour config
// constant).
func protoWalkInputs(peerProtoAddIncl []VFS) ModuleCCInputs {
	own := make([]VFS, 0, 1+len(peerProtoAddIncl))
	own = append(own, pbRuntimeBaseVFS)
	own = append(own, peerProtoAddIncl...)

	return ModuleCCInputs{AddIncl: own}
}

func protoDirectImportNames(pm *includeParserManager, srcRel string) []string {
	direct := pm.sourceParsedBuckets(Source(srcRel), nil).bucket(parsedIncludesLocal)

	if len(direct) == 0 {
		return nil
	}

	out := make([]string, 0, len(direct))

	for _, d := range direct {
		out = append(out, d.target.String())
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
		if fs.IsFile(srcRootVFS, cand) {
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
		if p.Root() != VFSRootSource {
			continue
		}

		if fs.IsFile(p, clean) {
			return filepath.ToSlash(filepath.Clean(p.Rel() + "/" + clean))
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

func protoTransitiveHeadersEnabled(d *moduleData) bool {
	if d != nil {
		if d.setVars != nil {
			if v, ok := d.setVars["PROTOC_TRANSITIVE_HEADERS"]; ok {
				return v != "no"
			}
		}

		if d.defaultVars != nil {
			if v, ok := d.defaultVars["PROTOC_TRANSITIVE_HEADERS"]; ok {
				return v != "no"
			}
		}
	}

	return true
}

type protoPBConfig struct {
	grpc                       bool
	moduleTag                  STR
	cppOutRoot                 string
	duplicateOutputRootInclude bool
}

type protoPBEmission struct {
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
type pbModuleEmission struct {
	protocLDRef        NodeRef
	cppStyleguideLDRef NodeRef
	grpcCppLDRef       NodeRef

	protocBinary        VFS
	cppStyleguideBinary VFS
	grpcCppBinary       VFS

	liteHeaders  bool
	extraPlugins []resolvedCPPProtoPlugin

	blocks *pbArgBlocks
}

func newPBModuleEmission(ctx *genCtx, d *moduleData, cfg protoPBConfig, peerProtoAddIncl []VFS, protoNamespaceTail []VFS) *pbModuleEmission {
	pe := &pbModuleEmission{
		liteHeaders:   !protoTransitiveHeadersEnabled(d),
		grpcCppBinary: pbGrpcCppVFS,
	}
	pe.protocLDRef, pe.protocBinary = ctx.tool(argContribToolsProtoc)
	pe.cppStyleguideLDRef, pe.cppStyleguideBinary = ctx.tool(argContribToolsProtocPluginsCppStyleguide)

	if cfg.grpc {
		pe.grpcCppLDRef, pe.grpcCppBinary = ctx.tool(argContribToolsProtocPluginsGrpcCpp)
	}

	pe.extraPlugins = make([]resolvedCPPProtoPlugin, 0, len(d.cppProtoPlugins))

	for _, spec := range d.cppProtoPlugins {
		ldRef, binary := ctx.tool(internArg(spec.ToolPath))
		pe.extraPlugins = append(pe.extraPlugins, resolvedCPPProtoPlugin{
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

func emitProtoPB(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, cfg protoPBConfig, pe *pbModuleEmission, peerProtoAddIncl []VFS) protoPBEmission {
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcRel)
	// Search transitive .proto imports through the same -I prefixes protoc
	// receives: the own PROTO_NAMESPACE (cppOutRoot) plus every peer-contributed
	// proto namespace. Without the own namespace, opentelemetry-proto's
	// `import "opentelemetry/proto/common/v1/common.proto"` from resource.proto
	// would not resolve, even though protoc handles it via -I=$(S)/cppOutRoot.
	protoSearchPaths := peerProtoAddIncl

	if cfg.cppOutRoot != "" {
		protoSearchPaths = append([]VFS{Source(cfg.cppOutRoot)}, peerProtoAddIncl...)
	}

	protoVFS := Source(protoRelPath)
	transitiveImports := walkClosureTail(ctx, instance, protoVFS, protoWalkInputs(protoSearchPaths))

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
		buildProto := Build(protoRelPath)

		if info := reg.Lookup(buildProto); info != nil && info.HasProducerRef {
			protoSrcOverride = buildProto
			extraProtoDeps = []NodeRef{info.ProducerRef}
			protoProducerSourceInputs = info.SourceInputs
		}
	}

	pbRef := EmitPB(
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
	pbH := Build(protoBase + ".pb.h")
	pbCC := Build(protoBase + ".pb.cc")
	pbDepsH := Build(protoBase + ".deps.pb.h")
	grpcPbH := Build(protoBase + ".grpc.pb.h")
	grpcPbCC := Build(protoBase + ".grpc.pb.cc")
	extraOutputPaths := make([]VFS, 0, 4)
	extraSourceOutputs := make([]VFS, 0, 2)

	for _, plugin := range d.cppProtoPlugins {
		for _, suffix := range plugin.OutputSuffixes {
			out := Build(protoBase + suffix)
			extraOutputPaths = append(extraOutputPaths, out)

			if isCCSourceExt(out.Rel()) {
				extraSourceOutputs = append(extraSourceOutputs, out)
			}
		}
	}

	needsGRPCParsed := cfg.grpc

	if !needsGRPCParsed {
		for _, out := range extraOutputPaths {
			if out.Rel() == grpcPbH.Rel() || out.Rel() == grpcPbCC.Rel() {
				needsGRPCParsed = true
				break
			}
		}
	}

	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		directImports := protoDirectPbHIncludes(ctx.parsers, protoRelPath, cfg.cppOutRoot)
		pbHImports := directImports
		extras := pbHEmitsIncludesExtras()
		pbHParsed := make([]includeDirective, 0, len(pbHImports)+len(extras)+len(transitiveImports))
		pbHParsed = append(pbHParsed, pbHImports...)
		pbHParsed = append(pbHParsed, extras...)

		for _, ti := range transitiveImports {
			pbHParsed = append(pbHParsed, includeDirective{kind: includeQuoted, target: internStr(ti.Rel())})
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
				reg.AddClosureLeaf(pbH, Source(protoRelPath))
			} else {
				for _, s := range protoProducerSourceInputs {
					reg.AddClosureLeaf(pbH, s)
				}
			}
		}

		if pe.liteHeaders {
			depsParsed := make([]includeDirective, 0, 1+len(directImports))
			depsParsed = append(depsParsed, includeDirective{kind: includeQuoted, target: internStr(pbH.Rel())})
			depsParsed = append(depsParsed, directImports...)
			registerBoundGeneratedParsedOutput(ctx, instance, pkPB, pbDepsH, depsParsed, pbRef, pbGenRefs)
		}

		pbCCParsed := make([]includeDirective, 0, 3+len(directImports))
		pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: internStr(pbH.Rel())})

		if pe.liteHeaders {
			pbCCParsed = append(pbCCParsed, directImports...)
		}

		pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.Rel())})

		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, pbCC, pbCCParsed, pbRef, pbGenRefs)

		var grpcCCParsed, grpcHParsed []includeDirective

		if needsGRPCParsed {
			grpcCCParsed = make([]includeDirective, 0, 2)
			grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: internStr(pbH.Rel())})
			grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.Rel())})

			grpcHParsed = make([]includeDirective, 0, 3+len(directImports))
			grpcHParsed = append(grpcHParsed, includeDirective{kind: includeQuoted, target: internStr(pbH.Rel())})
			grpcHParsed = append(grpcHParsed, directImports...)
			grpcHParsed = append(grpcHParsed, includeDirective{kind: includeQuoted, target: internStr(pbRuntimeBase + "google/protobuf/port_def.inc")})
		}

		if cfg.grpc {
			registerBoundGeneratedParsedOutput(ctx, instance, pkPB, grpcPbCC, grpcCCParsed, pbRef, []NodeRef{pe.protocLDRef, pe.grpcCppLDRef})
			registerBoundGeneratedParsedOutput(ctx, instance, pkPB, grpcPbH, grpcHParsed, pbRef, []NodeRef{pe.grpcCppLDRef})
		}
	}

	return protoPBEmission{
		pbRef:         pbRef,
		pbCC:          pbCC,
		grpcPbCC:      grpcPbCC,
		extraSourceCC: extraSourceOutputs,
		relPath:       protoRelPath,
	}
}

func emitProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs) *protoSrcsResult {
	var protoSrcs, evSrcs []string

	for _, src := range d.srcs {
		switch {
		case strings.HasSuffix(src, ".proto"):
			protoSrcs = append(protoSrcs, src)
		case strings.HasSuffix(src, ".ev"):
			evSrcs = append(evSrcs, src)
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

func emitCPPProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs, protoSrcs, evSrcs []string) *protoSrcsResult {
	type protoCodegenOutput struct {
		genRef NodeRef
		pbCC   VFS
		srcRel string
	}

	var codegenOutputs []protoCodegenOutput
	codegenOutputSeen := make(map[string]struct{})
	appendCodegenOutput := func(genRef NodeRef, pbCC VFS, srcRel string) {
		if _, dup := codegenOutputSeen[pbCC.Rel()]; dup {
			return
		}

		codegenOutputSeen[pbCC.Rel()] = struct{}{}
		codegenOutputs = append(codegenOutputs, protoCodegenOutput{
			genRef: genRef,
			pbCC:   pbCC,
			srcRel: srcRel,
		})
	}
	cfg := protoPBConfig{
		grpc:       d.grpc,
		moduleTag:  tagCppProto,
		cppOutRoot: protoCPPOutRoot(d),
	}

	if cfg.cppOutRoot != "" {
		cfg.duplicateOutputRootInclude = containsVFS(peerContribs.addIncl, Build(cfg.cppOutRoot))
	}

	cppInstance := instance
	cppInstance.Path = protoCPPModulePath(instance, d)

	pe := newPBModuleEmission(ctx, d, cfg, peerContribs.protoAddIncl, peerContribs.protoNamespaceTail)

	for _, src := range protoSrcs {
		pb := emitProtoPB(ctx, instance, d, src, cfg, pe, peerContribs.protoAddIncl)

		ccSrcRel := strings.TrimPrefix(pb.pbCC.Rel(), cppInstance.Path.Rel()+"/")
		appendCodegenOutput(pb.pbRef, pb.pbCC, ccSrcRel)

		if d.grpc {
			grpcSrcRel := strings.TrimPrefix(pb.grpcPbCC.Rel(), cppInstance.Path.Rel()+"/")
			appendCodegenOutput(pb.pbRef, pb.grpcPbCC, grpcSrcRel)
		}

		for _, extraSrc := range pb.extraSourceCC {
			extraSrcRel := strings.TrimPrefix(extraSrc.Rel(), cppInstance.Path.Rel()+"/")
			appendCodegenOutput(pb.pbRef, extraSrc, extraSrcRel)
		}
	}

	if len(evSrcs) > 0 {
		protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
		cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
		event2cppLDRef, event2cppBinary := ctx.tool(argToolsEvent2cpp)

		for _, src := range evSrcs {
			evRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
			evVFS := Source(evRelPath)
			evImports := walkClosureTail(ctx, instance, evVFS, protoWalkInputs(nil))

			evRef := EmitEV(
				instance, evRelPath, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				tagCppProto, evImports, d.tc, ctx.emit)

			evH := Build(evRelPath + ".pb.h")
			evPbCC := Build(evRelPath + ".pb.cc")

			if reg := codegenRegForInstance(ctx, instance); reg != nil {
				directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, protoCPPOutRoot(d))
				evExtras := evWitnessExtras(evRelPath, evPbCC)
				evHParsed := make([]includeDirective, 0, len(directImports)+len(protobufRuntimeHeaders)+len(evExtras))
				evHParsed = append(evHParsed, directImports...)
				evHParsed = append(evHParsed, protobufRuntimeDirectives...)
				evHParsed = append(evHParsed, evExtras...)
				registerBoundGeneratedParsedOutput(ctx, instance, pkEV, evH, evHParsed, evRef, []NodeRef{event2cppLDRef})

				evCCParsed := make([]includeDirective, 0, 1+len(protobufRuntimeHeaders))
				evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: internStr(evH.Rel())})
				evCCParsed = append(evCCParsed, protobufRuntimeDirectives...)

				registerBoundGeneratedParsedOutput(ctx, instance, pkEV, evPbCC, evCCParsed, evRef, []NodeRef{event2cppLDRef})
			}

			cppInstance := instance
			cppInstance.Path = protoCPPModulePath(instance, d)
			evSrcRel := strings.TrimPrefix(evRelPath+".pb.cc", cppInstance.Path.Rel()+"/")
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

	wireFormatVFS := Source(pbRuntimeBase + "google/protobuf/wire_format.h")

	for _, co := range codegenOutputs {
		ccIn := moduleInputs
		ccIn.IncludeInputs = walkClosure(ctx, instance, co.pbCC, moduleInputs)

		if strings.HasSuffix(co.srcRel, ".ev.pb.cc") {
			selfH := Build(strings.TrimSuffix(co.pbCC.Rel(), ".cc") + ".h")
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
		ccRef, ccOut, _ := EmitCC(cppInstance, co.srcRel, co.pbCC, ccIn, ctx.host, ctx.emit)
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
				if !isCCSourceExt(outTok) {
					continue
				}

				outVFS := copyFileOutputVFS(instance.Path.Rel(), outTok)
				info := reg.Lookup(outVFS)

				if info == nil || !info.HasProducerRef {
					continue
				}

				cppRel := antlrOutputModuleRel(instance.Path.Rel(), outVFS)
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
		protoLibName = d.moduleStmt.Args[0]
	}

	arBaseName := archiveNameWithPrefixOrName(instance.Path.Rel(), "lib", protoLibName)
	archivePath := Build(instance.Path.Rel() + "/" + arBaseName)
	arRef := emitARNode(instance, archivePath, tagCppProto, ccRefs, ccOutputs, nil, nil, d.tc, ctx.host, ctx.emit)
	return &protoSrcsResult{ARRef: arRef, ARPath: &archivePath}
}
