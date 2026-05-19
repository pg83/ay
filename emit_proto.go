package main

import (
	"path/filepath"
	"sort"
	"strings"
)

// protoDirectPbHIncludes returns the `.pb.h`-textual-includes set of a
// protoc-generated `.pb.h` (or `.ev.pb.h`) by transforming the
// parsedIncludesHCPP bucket of its source `.proto`/`.ev`:
//   - relative entries get the codegen output-root prefix applied;
//   - `google/protobuf/descriptor.pb.h` is rebased onto the protobuf
//     runtime tree (pre-committed header, not a codegen output).
//
// Mirrors upstream proto_processor.cpp::PrepareIncludes; the parser
// (parsers.go::protoIncludeDirectiveParser) extracts the raw mapping,
// the walker applies the prefixes here.
func protoDirectPbHIncludes(pm *includeParserManager, srcRel, outputRoot string) []includeDirective {
	hcpp := pm.sourceParsedBuckets(srcRel).bucket(parsedIncludesHCPP)
	if len(hcpp) == 0 {
		return nil
	}

	out := make([]includeDirective, 0, len(hcpp))
	for _, d := range hcpp {
		target := d.target
		if target == "google/protobuf/descriptor.pb.h" {
			target = pbRuntimeBase + target
		} else {
			target = protoOutputRel(outputRoot, target)
		}
		out = append(out, includeDirective{kind: d.kind, target: target})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].target < out[j].target })
	return out
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
	protocLDRef, protocBinary := ctx.tool(pbProtocModule)
	cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(pbCppStyleguideModule)

	var grpcCppLDRef NodeRef
	grpcCppBinary := pbGrpcCppVFS
	if d.grpc {
		grpcCppLDRef, grpcCppBinary = ctx.tool(pbGrpcCppModule)
	}

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
	duplicateOutputRootInclude := false
	if cppOutRoot := protoCPPOutRoot(d); cppOutRoot != "" {
		duplicateOutputRootInclude = containsVFS(peerContribs.addIncl, Build(cppOutRoot))
	}

	// Emit PB nodes.
	for _, src := range protoSrcs {
		protoRelPath := protoSourceRelPath(ctx.fs, instance, d, src)
		transitiveImports, hasDescriptor := protoTransitiveImports(ctx.parsers, ctx.fs, protoRelPath)

		pbRef := EmitPB(
			instance, protoRelPath, cppStyleguideLDRef, protocLDRef,
			grpcCppLDRef, cppStyleguideBinary, protocBinary,
			grpcCppBinary, d.grpc,
			stringPtr("cpp_proto"), protoCPPOutRoot(d), duplicateOutputRootInclude,
			transitiveImports, hasDescriptor, ctx.emit)

		// Register the .pb.h with EmitsIncludes: .pb.h's of every imported
		// proto plus the constant protobuf runtime header set.
		protoBase := strings.TrimSuffix(protoRelPath, ".proto")
		pbH := Build(protoBase + ".pb.h")
		pbCC := Build(protoBase + ".pb.cc")
		grpcPbH := Build(protoBase + ".grpc.pb.h")
		grpcPbCC := Build(protoBase + ".grpc.pb.cc")

		// Stash the PB NodeRef under both output paths on the emitting
		// platform so resolveCodegenDepRefs can thread it as a direct dep
		// on consumer CCs whose IncludeInputs carry the .pb.h/.pb.cc path.
		// Keyed per-platform: x86_64 consumers reach the x86_64 PB,
		// aarch64 consumers reach the aarch64 PB.
		pbKey := codegenOutputKey{platform: instance.Platform}
		pbKey.path = pbH
		ctx.pbOutputs[pbKey] = pbRef
		pbKey.path = pbCC
		ctx.pbOutputs[pbKey] = pbRef
		if d.grpc {
			pbKey.path = grpcPbH
			ctx.pbOutputs[pbKey] = pbRef
			pbKey.path = grpcPbCC
			ctx.pbOutputs[pbKey] = pbRef
		}
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			directImports := protoDirectPbHIncludes(ctx.parsers, protoRelPath, protoCPPOutRoot(d))
			extras := pbHEmitsIncludesExtras(protoRelPath, hasDescriptor)
			pbHParsed := make([]includeDirective, 0, len(directImports)+len(protobufRuntimeHeaders)+len(extras))
			pbHParsed = append(pbHParsed, directImports...)
			for _, include := range protobufRuntimeHeaders {
				pbHParsed = append(pbHParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
			pbHParsed = append(pbHParsed, extras...)
			registerGeneratedParsedOutput(ctx, instance, "PB", pbH, pbHParsed)
			// Register the .pb.cc output: protoc emits `#include
			// "<base>.pb.h"` plus the protobuf runtime headers; the .pb.cc.o
			// consumer also reaches the deep protobuf+abseil-cpp-tstring
			// transitive closure (pbCcDeepRuntimeHeaders), plus the .proto
			// source itself and cpp_proto_wrapper.py. Scope is narrow: ONLY
			// on the .pb.cc, never the .pb.h — broad .pb.h consumers must
			// NOT inherit the abseil closure.
			pbCCParsed := make([]includeDirective, 0, 3+len(protobufRuntimeHeaders)+len(pbCcDeepRuntimeHeaders))
			pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: pbH.Rel})
			pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: protoRelPath})
			pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: pbWrapperVFS.Rel})
			for _, include := range protobufRuntimeHeaders {
				pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
			for _, include := range pbCcDeepRuntimeHeaders {
				pbCCParsed = append(pbCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
			registerGeneratedParsedOutput(ctx, instance, "PB", pbCC, pbCCParsed)
			if d.grpc {
				grpcCCParsed := make([]includeDirective, 0, len(pbCCParsed))
				grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: grpcPbH.Rel})
				grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: protoRelPath})
				grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: pbWrapperVFS.Rel})
				for _, include := range protobufRuntimeHeaders {
					grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
				}
				for _, include := range pbCcDeepRuntimeHeaders {
					grpcCCParsed = append(grpcCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
				}
				registerGeneratedParsedOutput(ctx, instance, "PB", grpcPbCC, grpcCCParsed)
			}
		}

		// Stash the (PB ref, .pb.cc, src-with-suffix) for downstream-CC + AR.
		cppInstance := instance
		cppInstance.Path = protoCPPModulePath(instance, d)
		ccSrcRel := strings.TrimPrefix(protoBase+".pb.cc", cppInstance.Path+"/")
		codegenOutputs = append(codegenOutputs, protoCodegenOutput{
			genRef:  pbRef,
			pbCC:    pbCC,
			srcRel:  ccSrcRel,
			primSrc: Source(protoRelPath),
		})
		if d.grpc {
			grpcSrcRel := strings.TrimPrefix(protoBase+".grpc.pb.cc", cppInstance.Path+"/")
			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef:  pbRef,
				pbCC:    grpcPbCC,
				srcRel:  grpcSrcRel,
				primSrc: Source(protoRelPath),
			})
		}
	}

	// Emit EV nodes (PROTO_LIBRARY with .ev sources → module_tag:"cpp_proto").
	if len(evSrcs) > 0 {
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

	dedupedAddIncl := mergeDedupVFS(d.addIncl, nil)

	moduleInputs := ModuleCCInputs{
		Flags:                d.flags,
		AddIncl:              dedupedAddIncl,
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
		ModuleTag:            stringPtr("cpp_proto"),
	}

	// Per-source downstream-CC emission for the PROTO_LIBRARY context.
	cppInstance := instance
	cppInstance.Path = protoCPPModulePath(instance, d)
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
		ccIn.Generator = co.genRef
		ccIn.HasGenerator = true
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
		// Cross-codegen deps via .pb.h imports.
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, co.genRef)

		ccRef, ccOut := EmitCC(cppInstance, co.srcRel, co.pbCC, ccIn, ctx.host, ctx.emit)
		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)

		// AR memberInputs: primary source first, then the CC's include
		// closure.
		perCC := make([]VFS, 0, 1+len(ccIn.IncludeInputs))
		perCC = append(perCC, co.primSrc)
		perCC = append(perCC, ccIn.IncludeInputs...)
		addMemberInputs(perCC)
	}

	// AR emission with module_tag=cpp_proto.
	arBaseName := ArchiveName(instance.Path)
	archivePath := Build(instance.Path + "/" + arBaseName)
	arRef := emitARNode(instance, archivePath, stringPtr("cpp_proto"), ccRefs, ccOutputs, nil, memberInputs, nil, ctx.host, ctx.emit)

	return &protoSrcsResult{ARRef: arRef, ARPath: &archivePath}
}
