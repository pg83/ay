package main

import (
	"path/filepath"
	"sort"
	"strings"
)

func protoPbHIncludes(pm *includeParserManager, srcRel, outputRoot string, bucket parsedIncludeBucket) []includeDirective {
	hcpp := pm.sourceParsedBuckets(Source(srcRel)).bucket(bucket)

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

		out = append(out, includeDirective{kind: d.kind, target: internString(target)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].target.String() < out[j].target.String() })
	return out
}

func protoDirectPbHIncludes(pm *includeParserManager, srcRel, outputRoot string) []includeDirective {
	return protoPbHIncludes(pm, srcRel, outputRoot, parsedIncludesHCPP)
}

func pbHEmitsIncludesExtras(protoRelPath string, hasDescriptor bool) []includeDirective {
	out := make([]includeDirective, 0, len(pbDescriptorImporterHeaders)+3)
	out = append(out, includeDirective{kind: includeQuoted, target: internString(pbWrapperVFS.Rel())})
	out = append(out, includeDirective{kind: includeQuoted, target: internString(protoRelPath)})

	for _, v := range pbDescriptorImporterHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: internString(v.Rel())})
	}

	if hasDescriptor {
		out = append(out, includeDirective{kind: includeQuoted, target: internString(pbDescriptorVFS.Rel())})
	}

	return out
}

func cloneIncludeDirectives(parsed []includeDirective) []includeDirective {
	if len(parsed) == 0 {
		return nil
	}

	return append([]includeDirective(nil), parsed...)
}

func extraProtoOutputParsedIncludes(output, pbH, grpcPbH, grpcPbCC VFS, grpcHParsed, grpcCCParsed []includeDirective) []includeDirective {
	switch output.Rel() {
	case grpcPbH.Rel():
		return cloneIncludeDirectives(grpcHParsed)
	case grpcPbCC.Rel():
		return cloneIncludeDirectives(grpcCCParsed)
	}

	switch {
	case strings.HasSuffix(output.Rel(), ".h"),
		strings.HasSuffix(output.Rel(), ".hh"),
		strings.HasSuffix(output.Rel(), ".hpp"),
		strings.HasSuffix(output.Rel(), ".hxx"),
		strings.HasSuffix(output.Rel(), ".inc"),
		strings.HasSuffix(output.Rel(), ".inl"):
		return []includeDirective{{kind: includeQuoted, target: internString(pbH.Rel())}}
	case isCCSourceExt(output.Rel()):
		return []includeDirective{{kind: includeQuoted, target: internString(pbH.Rel())}}
	default:
		return nil
	}
}

func protoTransitiveImports(pm *includeParserManager, fs FS, srcRel string, peerProtoAddIncl []VFS) ([]VFS, bool) {
	rootImports := protoDirectImportNames(pm, srcRel)

	if rootImports == nil {
		return nil, false
	}

	var imports []VFS
	hasDescriptor := false
	seen := map[string]struct{}{}
	scanned := map[string]struct{}{}
	var walk func(string)
	walk = func(rel string) {
		if _, done := scanned[rel]; done {
			return
		}

		scanned[rel] = struct{}{}
		direct := protoDirectImportNames(pm, rel)

		for _, imp := range direct {
			if imp == "google/protobuf/descriptor.proto" {
				hasDescriptor = true
				continue
			}

			resolved := resolveProtoImportPath(fs, imp, peerProtoAddIncl)

			if resolved == "" {
				continue
			}

			if _, ok := seen[resolved]; ok {
				continue
			}

			seen[resolved] = struct{}{}
			imports = append(imports, Source(resolved))
		}

		for _, imp := range direct {
			if imp == "google/protobuf/descriptor.proto" {
				continue
			}

			if resolved := resolveProtoImportPath(fs, imp, peerProtoAddIncl); resolved != "" {
				walk(resolved)
			}
		}
	}

	for _, imp := range rootImports {
		if imp == "google/protobuf/descriptor.proto" {
			hasDescriptor = true
			continue
		}

		resolved := resolveProtoImportPath(fs, imp, peerProtoAddIncl)

		if resolved == "" {
			continue
		}

		if _, ok := seen[resolved]; ok {
			continue
		}

		seen[resolved] = struct{}{}
		imports = append(imports, Source(resolved))
	}

	for _, imp := range rootImports {
		if imp == "google/protobuf/descriptor.proto" {
			continue
		}

		if resolved := resolveProtoImportPath(fs, imp, peerProtoAddIncl); resolved != "" {
			walk(resolved)
		}
	}

	return imports, hasDescriptor
}

func evTransitiveImports(pm *includeParserManager, fs FS, srcRel string) []VFS {
	visited := map[string]struct{}{}
	order := make([]VFS, 0, 8)
	descriptorAdded := false

	var walk func(rel string)
	walk = func(rel string) {
		if _, seen := visited[rel]; seen {
			return
		}

		visited[rel] = struct{}{}
		direct := protoDirectImportNames(pm, rel)

		if direct == nil {
			return
		}

		var imports []string

		for _, importedRel := range direct {
			if importedRel == "google/protobuf/descriptor.proto" {
				if !descriptorAdded {
					order = append(order, pbDescriptorVFS)
					descriptorAdded = true
				}

				continue
			}

			imports = append(imports, importedRel)
		}

		order = append(order, Source(rel))

		for _, imp := range imports {
			walk(imp)
		}
	}

	topImports := protoDirectImportNames(pm, srcRel)

	if topImports == nil {
		return nil
	}

	for _, imp := range topImports {
		walk(imp)
	}

	return order
}

func protoDirectImportNames(pm *includeParserManager, srcRel string) []string {
	direct := pm.sourceParsedBuckets(Source(srcRel)).bucket(parsedIncludesLocal)

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
	candidates := []string{clean}

	if !strings.HasPrefix(clean, "yt/") {
		candidates = append(candidates, filepath.ToSlash(filepath.Clean("yt/"+clean)))
	}

	candidates = append(candidates, filepath.ToSlash(filepath.Clean(pbRuntimeBase+clean)))

	// Peer PROTO_NAMESPACE / PROTO_LIBRARY contributions land in protoc's -I
	// flags (peerProtoAddIncl); mirror that here so transitive .proto inputs
	// resolve through the same search prefix protoc does (e.g. opentelemetry's
	// `import "opentelemetry/proto/common/v1/common.proto"` finds the file at
	// $(S)/contrib/libs/opentelemetry-proto/opentelemetry/proto/common/v1/common.proto
	// via the `contrib/libs/opentelemetry-proto` -I).
	for _, p := range peerProtoAddIncl {
		if p.Root() != VFSRootSource {
			continue
		}

		candidates = append(candidates, filepath.ToSlash(filepath.Clean(p.Rel()+"/"+clean)))
	}

	for _, cand := range candidates {
		if fs.IsFile(cand) {
			return cand
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
	moduleTag                  *string
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

func emitProtoPB(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, cfg protoPBConfig, peerProtoAddIncl []VFS) protoPBEmission {
	protocLDRef, protocBinary := ctx.tool(pbProtocModule)
	cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(pbCppStyleguideModule)
	liteHeaders := !protoTransitiveHeadersEnabled(d)

	var grpcCppLDRef NodeRef
	grpcCppBinary := pbGrpcCppVFS

	if cfg.grpc {
		grpcCppLDRef, grpcCppBinary = ctx.tool(pbGrpcCppModule)
	}

	extraPlugins := make([]resolvedCPPProtoPlugin, 0, len(d.cppProtoPlugins))

	for _, spec := range d.cppProtoPlugins {
		ldRef, binary := ctx.tool(spec.ToolPath)
		extraPlugins = append(extraPlugins, resolvedCPPProtoPlugin{
			Spec:   spec,
			LDRef:  ldRef,
			Binary: binary,
		})
	}

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

	transitiveImports, hasDescriptor := protoTransitiveImports(ctx.parsers, ctx.fs, protoRelPath, protoSearchPaths)

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
		instance, protoRelPath, protoSrcOverride, cppStyleguideLDRef, protocLDRef,
		grpcCppLDRef, cppStyleguideBinary, protocBinary, grpcCppBinary,
		cfg.grpc, cfg.moduleTag, cfg.cppOutRoot, cfg.duplicateOutputRootInclude,
		liteHeaders,
		d.protocFlags,
		extraPlugins,
		transitiveImports, hasDescriptor,
		peerProtoAddIncl,
		extraProtoDeps,
		protoProducerSourceInputs,
		ctx.emit,
	)

	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	pbH := Build(protoBase + ".pb.h")
	pbCC := Build(protoBase + ".pb.cc")
	pbDepsH := Build(protoBase + ".deps.pb.h")
	grpcPbH := Build(protoBase + ".grpc.pb.h")
	grpcPbCC := Build(protoBase + ".grpc.pb.cc")
	pbKey := codegenOutputKey{platform: instance.Platform}
	pbKey.path = pbH
	ctx.pbOutputs[pbKey] = pbRef
	pbKey.path = pbCC
	ctx.pbOutputs[pbKey] = pbRef

	if liteHeaders {
		pbKey.path = pbDepsH
		ctx.pbOutputs[pbKey] = pbRef
	}

	if cfg.grpc {
		pbKey.path = grpcPbH
		ctx.pbOutputs[pbKey] = pbRef
		pbKey.path = grpcPbCC
		ctx.pbOutputs[pbKey] = pbRef
	}

	extraOutputPaths := make([]VFS, 0, 4)
	extraSourceOutputs := make([]VFS, 0, 2)

	for _, plugin := range d.cppProtoPlugins {
		for _, suffix := range plugin.OutputSuffixes {
			out := Build(protoBase + suffix)
			extraOutputPaths = append(extraOutputPaths, out)

			if isCCSourceExt(out.Rel()) {
				extraSourceOutputs = append(extraSourceOutputs, out)
			}

			pbKey.path = out
			ctx.pbOutputs[pbKey] = pbRef
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
		extras := pbHEmitsIncludesExtras(protoRelPath, hasDescriptor)
		pbHParsed := make([]includeDirective, 0, len(pbHImports)+len(protobufRuntimeHeaders)+len(extras)+len(grpcServiceHeaderIncludes))
		pbHParsed = append(pbHParsed, pbHImports...)

		for _, include := range protobufRuntimeHeaders {
			pbHParsed = append(pbHParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
		}

		pbHParsed = append(pbHParsed, extras...)

		for _, ti := range transitiveImports {
			pbHParsed = append(pbHParsed, includeDirective{kind: includeQuoted, target: internString(ti.Rel())})
		}

		if cfg.grpc {
			for _, include := range grpcServiceHeaderIncludes {
				pbHParsed = append(pbHParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
			}
		}

		registerGeneratedParsedOutput(ctx, instance, "PB", pbH, pbHParsed)

		if liteHeaders {
			depsParsed := make([]includeDirective, 0, 1+len(directImports))
			depsParsed = append(depsParsed, includeDirective{kind: includeQuoted, target: internString(pbH.Rel())})
			depsParsed = append(depsParsed, directImports...)
			registerGeneratedParsedOutput(ctx, instance, "PB", pbDepsH, depsParsed)
		}

		pbCCParsed := make([]includeDirective, 0, 3+len(directImports)+len(protobufRuntimeHeaders)+len(pbCcDeepRuntimeHeaders))
		pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: internString(pbH.Rel())})

		if liteHeaders {
			pbCCParsed = append(pbCCParsed, directImports...)
		}

		pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: internString(protoRelPath)})
		pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: internString(pbWrapperVFS.Rel())})

		for _, include := range protobufRuntimeHeaders {
			pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
		}

		for _, include := range pbCcDeepRuntimeHeaders {
			pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
		}

		if cfg.grpc {
			for _, include := range grpcSourceExtraIncludes {
				pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
			}
		}

		registerGeneratedParsedOutput(ctx, instance, "PB", pbCC, pbCCParsed)

		var grpcCCParsed, grpcHParsed []includeDirective

		if needsGRPCParsed {
			grpcCCParsed = make([]includeDirective, 0, 3+len(protobufRuntimeHeaders)+len(pbCcDeepRuntimeHeaders)+len(grpcSourceExtraIncludes))
			grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: internString(pbH.Rel())})
			grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: internString(protoRelPath)})
			grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: internString(pbWrapperVFS.Rel())})

			for _, include := range protobufRuntimeHeaders {
				grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
			}

			for _, include := range pbCcDeepRuntimeHeaders {
				grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
			}

			for _, include := range grpcSourceExtraIncludes {
				grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
			}

			grpcHParsed = make([]includeDirective, 0, 2+len(directImports)+len(grpcServiceHeaderIncludes))
			grpcHParsed = append(grpcHParsed, includeDirective{kind: includeQuoted, target: internString(pbH.Rel())})
			grpcHParsed = append(grpcHParsed, directImports...)

			for _, include := range grpcServiceHeaderIncludes {
				grpcHParsed = append(grpcHParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
			}

			grpcHParsed = append(grpcHParsed, includeDirective{kind: includeQuoted, target: internString(pbRuntimeBase + "google/protobuf/port_def.inc")})
		}

		if cfg.grpc {
			registerGeneratedParsedOutput(ctx, instance, "PB", grpcPbCC, grpcCCParsed)
			registerGeneratedParsedOutput(ctx, instance, "PB", grpcPbH, grpcHParsed)
		}

		for _, out := range extraOutputPaths {
			registerGeneratedParsedOutput(ctx, instance, "PB", out, extraProtoOutputParsedIncludes(out, pbH, grpcPbH, grpcPbCC, grpcHParsed, grpcCCParsed))
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
		moduleTag:  stringPtr("cpp_proto"),
		cppOutRoot: protoCPPOutRoot(d),
	}

	if cfg.cppOutRoot != "" {
		cfg.duplicateOutputRootInclude = containsVFS(peerContribs.addIncl, Build(cfg.cppOutRoot))
	}

	cppInstance := instance
	cppInstance.Path = protoCPPModulePath(instance, d)

	for _, src := range protoSrcs {
		pb := emitProtoPB(ctx, instance, d, src, cfg, peerContribs.protoAddIncl)

		ccSrcRel := strings.TrimPrefix(pb.pbCC.Rel(), cppInstance.Path+"/")
		appendCodegenOutput(pb.pbRef, pb.pbCC, ccSrcRel)

		if d.grpc {
			grpcSrcRel := strings.TrimPrefix(pb.grpcPbCC.Rel(), cppInstance.Path+"/")
			appendCodegenOutput(pb.pbRef, pb.grpcPbCC, grpcSrcRel)
		}

		for _, extraSrc := range pb.extraSourceCC {
			extraSrcRel := strings.TrimPrefix(extraSrc.Rel(), cppInstance.Path+"/")
			appendCodegenOutput(pb.pbRef, extraSrc, extraSrcRel)
		}
	}

	if len(evSrcs) > 0 {
		protocLDRef, protocBinary := ctx.tool(pbProtocModule)
		cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(pbCppStyleguideModule)
		event2cppLDRef, event2cppBinary := ctx.tool(evEvent2cppModule)

		for _, src := range evSrcs {
			evRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
			evImports := evTransitiveImports(ctx.parsers, ctx.fs, evRelPath)

			evRef := EmitEV(
				instance, evRelPath, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				stringPtr("cpp_proto"), evImports, ctx.emit)

			evH := Build(evRelPath + ".pb.h")
			evPbCC := Build(evRelPath + ".pb.cc")
			evKey := codegenOutputKey{platform: instance.Platform}
			evKey.path = evH
			ctx.evOutputs[evKey] = evRef
			evKey.path = evPbCC
			ctx.evOutputs[evKey] = evRef

			if reg := codegenRegForInstance(ctx, instance); reg != nil {
				directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, protoCPPOutRoot(d))
				evExtras := evWitnessExtras(evRelPath, evPbCC)
				evHParsed := make([]includeDirective, 0, len(directImports)+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders)+len(evExtras))
				evHParsed = append(evHParsed, directImports...)

				for _, include := range protobufRuntimeHeaders {
					evHParsed = append(evHParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
				}

				for _, include := range eventRuntimeHeaders {
					evHParsed = append(evHParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
				}

				evHParsed = append(evHParsed, evExtras...)
				registerGeneratedParsedOutput(ctx, instance, "EV", evH, evHParsed)

				evCCParsed := make([]includeDirective, 0, 1+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders))
				evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: internString(evH.Rel())})

				for _, include := range protobufRuntimeHeaders {
					evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
				}

				for _, include := range eventRuntimeHeaders {
					evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
				}

				registerGeneratedParsedOutput(ctx, instance, "EV", evPbCC, evCCParsed)
			}

			cppInstance := instance
			cppInstance.Path = protoCPPModulePath(instance, d)
			evSrcRel := strings.TrimPrefix(evRelPath+".pb.cc", cppInstance.Path+"/")
			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef: evRef,
				pbCC:   evPbCC,
				srcRel: evSrcRel,
			})
		}
	}

	if d.moduleStmt.Name != "PROTO_LIBRARY" || len(codegenOutputs) == 0 {
		return nil
	}

	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	moduleInputs := ModuleCCInputs{
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
		SrcDir:               d.srcDir,
		SourceRoot:           ctx.sourceRoot,
		FS:                   ctx.fs,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		SetVars:              d.setVars,
		ModuleTag:            stringPtr("cpp_proto"),
	}

	ccRefs := make([]NodeRef, 0, len(codegenOutputs))
	ccOutputs := make([]VFS, 0, len(codegenOutputs))

	wireFormatVFS := Source(pbRuntimeBase + "google/protobuf/wire_format.h")

	for _, co := range codegenOutputs {
		ccIn := moduleInputs
		ccIn.IncludeInputs = dropTransitiveGeneratedProto(walkClosure(ctx, instance, co.pbCC, moduleInputs))

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

				outVFS := copyFileOutputVFS(instance.Path, outTok)
				info := reg.Lookup(outVFS)

				if info == nil || !info.HasProducerRef {
					continue
				}

				cppRel := antlrOutputModuleRel(instance.Path, outVFS)
				ccRef, ccOut := emitCodegenDownstreamCC(ctx, cppInstance, cppRel, nil, []NodeRef{info.ProducerRef}, moduleInputs)
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

	arBaseName := archiveNameWithPrefixOrName(instance.Path, "lib", protoLibName)
	archivePath := Build(instance.Path + "/" + arBaseName)
	arRef := emitARNode(instance, archivePath, stringPtr("cpp_proto"), ccRefs, ccOutputs, nil, nil, ctx.host, ctx.emit)
	return &protoSrcsResult{ARRef: arRef, ARPath: &archivePath}
}
