package main

import (
	"path/filepath"
	"sort"
	"strings"
)

// protoPbHIncludes returns a protoc-generated header include set by
// transforming one of the proto parser's h+cpp buckets:
//   - relative entries get the codegen output-root prefix applied;
//   - well-known-type `google/protobuf/*.pb.h` headers are rebased onto the
//     protobuf runtime tree (pre-committed headers, not codegen outputs).
//
// Mirrors upstream proto_processor.cpp::PrepareIncludes; the parser
// (parsers.go::protoIncludeDirectiveParser) extracts the raw mapping,
// the walker applies the prefixes here.
func protoPbHIncludes(pm *includeParserManager, srcRel, outputRoot string, bucket parsedIncludeBucket) []includeDirective {
	hcpp := pm.sourceParsedBuckets(srcRel).bucket(bucket)
	if len(hcpp) == 0 {
		return nil
	}

	out := make([]includeDirective, 0, len(hcpp))
	for _, d := range hcpp {
		target := d.target
		if strings.HasPrefix(target, "google/protobuf/") && strings.HasSuffix(target, ".pb.h") {
			target = pbRuntimeBase + target
		} else {
			target = protoOutputRel(outputRoot, target)
		}
		out = append(out, includeDirective{kind: d.kind, target: target})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].target < out[j].target })
	return out
}

func protoDirectPbHIncludes(pm *includeParserManager, srcRel, outputRoot string) []includeDirective {
	return protoPbHIncludes(pm, srcRel, outputRoot, parsedIncludesHCPP)
}

// pbHEmitsIncludesExtras returns the constant witness inputs propagated
// through a protoc-generated .pb.h to its CC consumers:
// pbDescriptorImporterHeaders (7 reflection-cluster headers),
// cpp_proto_wrapper.py, the proto source itself, and descriptor.pb.h
// when hasDescriptor (caller computes this once via protoTransitiveImports).
func pbHEmitsIncludesExtras(protoRelPath string, hasDescriptor bool) []includeDirective {
	out := make([]includeDirective, 0, len(pbDescriptorImporterHeaders)+3)
	out = append(out, includeDirective{kind: includeQuoted, target: pbWrapperVFS.Rel})
	out = append(out, includeDirective{kind: includeQuoted, target: protoRelPath})
	for _, v := range pbDescriptorImporterHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: v.Rel})
	}
	if hasDescriptor {
		out = append(out, includeDirective{kind: includeQuoted, target: pbDescriptorVFS.Rel})
	}
	return out
}

func cloneIncludeDirectives(parsed []includeDirective) []includeDirective {
	if len(parsed) == 0 {
		return nil
	}

	return append([]includeDirective(nil), parsed...)
}

// extraProtoOutputParsedIncludes gives output-bearing CPP_PROTO_PLUGIN*
// outputs enough structure for downstream closure: generic plugin
// headers/sources at least reach the sibling .pb.h, while grpc_cpp-style
// outputs reuse the exact grpc parsed witness set so they carry the same
// service/source closure as the built-in GRPC() path.
func extraProtoOutputParsedIncludes(output, pbH, grpcPbH, grpcPbCC VFS, grpcHParsed, grpcCCParsed []includeDirective) []includeDirective {
	switch output.Rel {
	case grpcPbH.Rel:
		return cloneIncludeDirectives(grpcHParsed)
	case grpcPbCC.Rel:
		return cloneIncludeDirectives(grpcCCParsed)
	}

	switch {
	case strings.HasSuffix(output.Rel, ".h"),
		strings.HasSuffix(output.Rel, ".hh"),
		strings.HasSuffix(output.Rel, ".hpp"),
		strings.HasSuffix(output.Rel, ".hxx"),
		strings.HasSuffix(output.Rel, ".inc"),
		strings.HasSuffix(output.Rel, ".inl"):
		return []includeDirective{{kind: includeQuoted, target: pbH.Rel}}
	case isCCSourceExt(output.Rel):
		return []includeDirective{{kind: includeQuoted, target: pbH.Rel}}
	default:
		return nil
	}
}

// protoTransitiveImports walks the .proto/.ev import graph starting at
// srcRel and returns the deduplicated set of imported sources plus a
// HasDescriptor flag (true iff `google/protobuf/descriptor.proto`
// appears anywhere in the closure). Each per-file import list comes
// from the parser_manager cache (parsedIncludesLocal); file-on-disk
// resolution uses the proto-specific search list (yt/ prefix and the
// protobuf runtime tree).
func protoTransitiveImports(pm *includeParserManager, fs *FS, srcRel string) ([]VFS, bool) {
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
			resolved := resolveProtoImportPath(fs, imp)
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
			if resolved := resolveProtoImportPath(fs, imp); resolved != "" {
				walk(resolved)
			}
		}
	}

	for _, imp := range rootImports {
		if imp == "google/protobuf/descriptor.proto" {
			hasDescriptor = true
			continue
		}
		resolved := resolveProtoImportPath(fs, imp)
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
		if resolved := resolveProtoImportPath(fs, imp); resolved != "" {
			walk(resolved)
		}
	}

	return imports, hasDescriptor
}

// evTransitiveImports returns the deduplicated transitive import set
// for a .ev/.proto file at srcRel: every imported file resolved against
// the proto search list plus descriptor.pb.h when any chain reaches
// `google/protobuf/descriptor.proto`. Per-file imports come from the
// parser_manager cache.
func evTransitiveImports(pm *includeParserManager, fs *FS, srcRel string) []VFS {
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

// protoDirectImportNames returns the raw `import "..."` strings the
// proto parser cached for srcRel.
func protoDirectImportNames(pm *includeParserManager, srcRel string) []string {
	direct := pm.sourceParsedBuckets(srcRel).bucket(parsedIncludesLocal)
	if len(direct) == 0 {
		return nil
	}
	out := make([]string, 0, len(direct))
	for _, d := range direct {
		out = append(out, d.target)
	}
	return out
}

// resolveProtoImportPath returns the SOURCE_ROOT-relative path of an
// imported .proto/.ev file, searching the proto-specific candidate
// list: bare, "yt/" prefix (Yandex namespacing), protobuf runtime base.
func resolveProtoImportPath(fs *FS, importedRel string) string {
	clean := filepath.ToSlash(filepath.Clean(importedRel))
	candidates := []string{clean}
	if !strings.HasPrefix(clean, "yt/") {
		candidates = append(candidates, filepath.ToSlash(filepath.Clean("yt/"+clean)))
	}
	candidates = append(candidates, filepath.ToSlash(filepath.Clean(pbRuntimeBase+clean)))

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

// protoPBConfig parametrizes per-source PB-node emission for the two
// dispatch points (regular SRCS-loop's .proto handling and the
// PROTO_LIBRARY orchestrator). LIBRARY callers leave every field zero;
// PROTO_LIBRARY callers fill in grpc / module_tag=cpp_proto / cppOutRoot
// from PROTO_NAMESPACE / duplicateOutputRootInclude.
type protoPBConfig struct {
	grpc                       bool
	moduleTag                  *string
	cppOutRoot                 string
	duplicateOutputRootInclude bool
}

// protoPBEmission is what emitProtoPB returns to the caller: the PB
// NodeRef + the .pb.cc output (and the .grpc.pb.cc when cfg.grpc) and
// the resolved .proto source-relative path.
type protoPBEmission struct {
	pbRef         NodeRef
	pbCC          VFS
	grpcPbCC      VFS
	extraSourceCC []VFS
	relPath       string
}

// emitProtoPB emits the PB codegen node for `srcRel`, registers the
// output paths in ctx.pbOutputs, and registers the .pb.h / .pb.cc parsed
// includes in the codegen registry. Shared between the regular
// SRCS-loop's .proto handling (cfg{}) and the PROTO_LIBRARY orchestrator
// (cfg with grpc/moduleTag/cppOutRoot).
func emitProtoPB(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, cfg protoPBConfig) protoPBEmission {
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
	transitiveImports, hasDescriptor := protoTransitiveImports(ctx.parsers, ctx.fs, protoRelPath)

	pbRef := EmitPB(
		instance, protoRelPath, cppStyleguideLDRef, protocLDRef,
		grpcCppLDRef, cppStyleguideBinary, protocBinary, grpcCppBinary,
		cfg.grpc, cfg.moduleTag, cfg.cppOutRoot, cfg.duplicateOutputRootInclude,
		liteHeaders,
		d.protocFlags,
		extraPlugins,
		transitiveImports, hasDescriptor, ctx.emit,
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
			if isCCSourceExt(out.Rel) {
				extraSourceOutputs = append(extraSourceOutputs, out)
			}
			pbKey.path = out
			ctx.pbOutputs[pbKey] = pbRef
		}
	}
	needsGRPCParsed := cfg.grpc
	if !needsGRPCParsed {
		for _, out := range extraOutputPaths {
			if out.Rel == grpcPbH.Rel || out.Rel == grpcPbCC.Rel {
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
			pbHParsed = append(pbHParsed, includeDirective{kind: includeQuoted, target: include.Rel})
		}
		pbHParsed = append(pbHParsed, extras...)
		if cfg.grpc {
			// A grpc PROTO_LIBRARY's message .pb.h carries the grpcpp service
			// preamble (sg3.dep: runner.pb.h directly includes the grpcpp
			// service headers alongside protobuf runtime), so every consumer
			// of the .pb.h reaches the grpc closure.
			for _, include := range grpcServiceHeaderIncludes {
				pbHParsed = append(pbHParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
		}
		registerGeneratedParsedOutput(ctx, instance, "PB", pbH, pbHParsed)
		if liteHeaders {
			depsParsed := make([]includeDirective, 0, 1+len(directImports))
			depsParsed = append(depsParsed, includeDirective{kind: includeQuoted, target: pbH.Rel})
			depsParsed = append(depsParsed, directImports...)
			registerGeneratedParsedOutput(ctx, instance, "PB", pbDepsH, depsParsed)
		}

		pbCCParsed := make([]includeDirective, 0, 3+len(directImports)+len(protobufRuntimeHeaders)+len(pbCcDeepRuntimeHeaders))
		pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: pbH.Rel})
		if liteHeaders {
			pbCCParsed = append(pbCCParsed, directImports...)
		}
		pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: protoRelPath})
		pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: pbWrapperVFS.Rel})
		for _, include := range protobufRuntimeHeaders {
			pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
		}
		for _, include := range pbCcDeepRuntimeHeaders {
			pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
		}
		if cfg.grpc {
			// OutTogether-shared grpcpp source headers from the sibling
			// .grpc.pb.cc reach this message .pb.cc.o too.
			for _, include := range grpcSourceExtraIncludes {
				pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
		}
		registerGeneratedParsedOutput(ctx, instance, "PB", pbCC, pbCCParsed)

		var grpcCCParsed, grpcHParsed []includeDirective
		if needsGRPCParsed {
			grpcCCParsed = make([]includeDirective, 0, 3+len(protobufRuntimeHeaders)+len(pbCcDeepRuntimeHeaders)+len(grpcSourceExtraIncludes))
			// The .grpc.pb.cc includes its message .pb.h (cross-sibling, not
			// its own .grpc.pb.h, which is the OutTogether self) plus the
			// grpcpp source preamble.
			grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: pbH.Rel})
			grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: protoRelPath})
			grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: pbWrapperVFS.Rel})
			for _, include := range protobufRuntimeHeaders {
				grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
			for _, include := range pbCcDeepRuntimeHeaders {
				grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
			for _, include := range grpcSourceExtraIncludes {
				grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}

			// The .grpc.pb.h directly includes its message .pb.h plus the
			// fixed grpcpp service preamble (+ port_def.inc); the scanner
			// recurses those into the full grpc/protobuf/abseil/libcxx
			// closure for every CC that includes the .grpc.pb.h.
			grpcHParsed = make([]includeDirective, 0, 2+len(directImports)+len(grpcServiceHeaderIncludes))
			grpcHParsed = append(grpcHParsed, includeDirective{kind: includeQuoted, target: pbH.Rel})
			grpcHParsed = append(grpcHParsed, directImports...)
			for _, include := range grpcServiceHeaderIncludes {
				grpcHParsed = append(grpcHParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
			grpcHParsed = append(grpcHParsed, includeDirective{kind: includeQuoted, target: pbRuntimeBase + "google/protobuf/port_def.inc"})
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
	// Collect per-codegen-source (genRef, .pb.cc path) pairs so the AR
	// step can fold them into ccRefs/ccOutputs/memberInputs in
	// declaration order.
	type protoCodegenOutput struct {
		genRef  NodeRef // PB or EV node ref (used as Generator dep for the downstream CC)
		pbCC    VFS     // generated .pb.cc / .ev.pb.cc BUILD_ROOT path
		srcRel  string  // module-relative source-with-codegen-suffix (".pb.cc" appended)
		primSrc VFS     // primary source path ($(S)/<module>/<src>) for AR memberInputs
	}

	var codegenOutputs []protoCodegenOutput
	codegenOutputSeen := make(map[string]struct{})
	appendCodegenOutput := func(genRef NodeRef, pbCC VFS, srcRel string, primSrc VFS) {
		if _, dup := codegenOutputSeen[pbCC.Rel]; dup {
			return
		}
		codegenOutputSeen[pbCC.Rel] = struct{}{}
		codegenOutputs = append(codegenOutputs, protoCodegenOutput{
			genRef:  genRef,
			pbCC:    pbCC,
			srcRel:  srcRel,
			primSrc: primSrc,
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
		pb := emitProtoPB(ctx, instance, d, src, cfg)

		ccSrcRel := strings.TrimPrefix(pb.pbCC.Rel, cppInstance.Path+"/")
		appendCodegenOutput(pb.pbRef, pb.pbCC, ccSrcRel, Source(pb.relPath))
		if d.grpc {
			grpcSrcRel := strings.TrimPrefix(pb.grpcPbCC.Rel, cppInstance.Path+"/")
			appendCodegenOutput(pb.pbRef, pb.grpcPbCC, grpcSrcRel, Source(pb.relPath))
		}
		for _, extraSrc := range pb.extraSourceCC {
			extraSrcRel := strings.TrimPrefix(extraSrc.Rel, cppInstance.Path+"/")
			appendCodegenOutput(pb.pbRef, extraSrc, extraSrcRel, Source(pb.relPath))
		}
	}

	// Emit EV nodes (PROTO_LIBRARY with .ev sources → module_tag:"cpp_proto").
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

			// Register .ev.pb.h with EmitsIncludes: .ev source's direct
			// imports + protobuf runtime headers + EV-specific runtime
			// headers (util/* + eventlog).
			evH := Build(evRelPath + ".pb.h")
			evPbCC := Build(evRelPath + ".pb.cc")

			// Stash the EV NodeRef under both outputs on the emitting
			// platform. See PB branch above for keying rationale.
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
					evHParsed = append(evHParsed, includeDirective{kind: includeQuoted, target: include.Rel})
				}
				for _, include := range eventRuntimeHeaders {
					evHParsed = append(evHParsed, includeDirective{kind: includeQuoted, target: include.Rel})
				}
				evHParsed = append(evHParsed, evExtras...)
				registerGeneratedParsedOutput(ctx, instance, "EV", evH, evHParsed)
				// Register .ev.pb.cc: event2cpp emits `#include
				// "<base>.ev.pb.h"` plus protobuf + event runtime headers.
				evCCParsed := make([]includeDirective, 0, 1+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders))
				evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: evH.Rel})
				for _, include := range protobufRuntimeHeaders {
					evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
				}
				for _, include := range eventRuntimeHeaders {
					evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
				}
				registerGeneratedParsedOutput(ctx, instance, "EV", evPbCC, evCCParsed)
			}

			cppInstance := instance
			cppInstance.Path = protoCPPModulePath(instance, d)
			evSrcRel := strings.TrimPrefix(evRelPath+".pb.cc", cppInstance.Path+"/")
			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef:  evRef,
				pbCC:    evPbCC,
				srcRel:  evSrcRel,
				primSrc: Source(evRelPath),
			})
		}
	}

	// For true PROTO_LIBRARY modules, emit the downstream CC per generated
	// .pb.cc/.ev.pb.cc and the AR archiving them. LIBRARY callers handle
	// their own downstream-CC + AR aggregation in emitOneSource.
	if d.moduleStmt.Name != "PROTO_LIBRARY" || len(codegenOutputs) == 0 {
		return nil
	}

	// Compose ModuleCCInputs for the downstream CCs. Per-axis peer-GLOBAL
	// slices come from the header-only walker's peerContribs.
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	moduleInputs := ModuleCCInputs{
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
		AutoPeerCFlags:       defaultPeerCFlags(ctx, instance, d),
		SrcDir:               d.srcDir,
		SourceRoot:           ctx.sourceRoot,
		FS:                   ctx.fs,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		SetVars:              d.setVars,
		ModuleTag:            stringPtr("cpp_proto"),
	}

	// Per-source downstream-CC emission for the PROTO_LIBRARY context.
	ccRefs := make([]NodeRef, 0, len(codegenOutputs))
	ccOutputs := make([]VFS, 0, len(codegenOutputs))
	memberInputs := make([]VFS, 0, 64)
	memberInputsSeen := make(map[VFS]struct{})

	addMemberInputs := func(paths []VFS) {
		for _, p := range paths {
			if _, dup := memberInputsSeen[p]; dup {
				continue
			}
			memberInputsSeen[p] = struct{}{}
			memberInputs = append(memberInputs, p)
		}
	}

	wireFormatVFS := Source(pbRuntimeBase + "google/protobuf/wire_format.h")
	for _, co := range codegenOutputs {
		ccIn := moduleInputs
		ccIn.IncludeInputs = walkClosure(ctx, instance, co.pbCC, moduleInputs)
		// .ev.pb.cc.o consumer must not carry its own .ev.pb.h in inputs[]
		// (REF omits the self-include). Drop just the sibling header.
		if strings.HasSuffix(co.srcRel, ".ev.pb.cc") {
			selfH := Build(strings.TrimSuffix(co.pbCC.Rel, ".cc") + ".h")
			filtered := make([]VFS, 0, len(ccIn.IncludeInputs))
			for _, in := range ccIn.IncludeInputs {
				if in == selfH {
					continue
				}
				filtered = append(filtered, in)
			}
			ccIn.IncludeInputs = filtered
		}
		// .ev.pb.cc gets wire_format.h post-closure (registry-side leaks through
		// .ev.pb.h to over-emit; .pb.cc gets it via pbCcDeepRuntimeHeaders).
		if strings.HasSuffix(co.srcRel, ".ev.pb.cc") {
			ccIn.IncludeInputs = append(ccIn.IncludeInputs, wireFormatVFS)
		}
		// Generator + cross-codegen deps via .pb.h imports.
		ccIn.ExtraDepRefs = append([]NodeRef{co.genRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, co.genRef)...)

		ccRef, ccOut, _ := EmitCC(cppInstance, co.srcRel, co.pbCC, ccIn, ctx.host, ctx.emit)
		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)

		// AR memberInputs: primary source first, then the CC's include
		// closure.
		perCC := make([]VFS, 0, 1+len(ccIn.IncludeInputs))
		perCC = append(perCC, co.primSrc)
		perCC = append(perCC, ccIn.IncludeInputs...)
		addMemberInputs(perCC)
	}
	// Wire EN-sourced CC into the archive when the module declares
	// GENERATE_ENUM_SERIALIZATION. The EN source + CC are orphaned
	// without this; normalization would exclude them, inflating ref_only.
	enRes := emitEnumSrcs(ctx, instance, d, peerContribs.addIncl, &moduleInputs)
	if enRes != nil {
		ccRefs = append(ccRefs, enRes.CCRefs...)
		ccOutputs = append(ccOutputs, enRes.CCOutputs...)
		for _, mIn := range enRes.MemberInputsList {
			addMemberInputs(mIn)
		}
	}

	// AR emission with module_tag=cpp_proto.
	var protoLibName string
	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0]
	}
	arBaseName := archiveNameWithPrefixOrName(instance.Path, "lib", protoLibName)
	archivePath := Build(instance.Path + "/" + arBaseName)
	arRef := emitARNode(instance, archivePath, stringPtr("cpp_proto"), ccRefs, ccOutputs, nil, memberInputs, nil, ctx.host, ctx.emit)

	return &protoSrcsResult{ARRef: arRef, ARPath: &archivePath}
}
