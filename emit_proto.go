package main

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// protoDirectImportIncludes parses direct `import "..."` statements
// from a .proto/.ev source and converts them to protoc's $(B) outputs:
//
//	import "x/y/z.proto" → "$(B)/x/y/z.pb.h"
//	import "x/y/z.ev"    → "$(B)/x/y/z.ev.pb.h"
//
// Direct imports only (no recursion). Returns nil on read failure;
// results sorted. Upstream pattern:
// proto_processor.cpp:43-56::TProtoIncludeProcessor::PrepareIncludes.
//
// Legitimate disk read: extracts structured `import` directives at
// registration time to populate EmitsIncludes. NOT for closure walks.
func protoDirectImportIncludes(sourceRoot, srcRel, outputRoot string) []includeDirective {
	absPath := filepath.Join(sourceRoot, srcRel)
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []includeDirective
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "import ") {
			continue
		}
		start := strings.IndexByte(line, '"')
		end := strings.LastIndexByte(line, '"')
		if start < 0 || end <= start {
			continue
		}
		imp := line[start+1 : end]
		if strings.HasSuffix(imp, ".ev") {
			out = append(out, includeDirective{kind: includeQuoted, target: protoOutputRel(outputRoot, strings.TrimSuffix(imp, ".ev")+".ev.pb.h")})
		} else if strings.HasSuffix(imp, ".proto") {
			base := strings.TrimSuffix(imp, ".proto")
			if imp == "google/protobuf/descriptor.proto" {
				// descriptor.pb.h is pre-committed, not a codegen output.
				// Upstream tree: contrib/libs/protobuf/src/google/protobuf/descriptor.pb.h
				out = append(out, includeDirective{kind: includeQuoted, target: pbRuntimeBase + "google/protobuf/descriptor.pb.h"})
			} else {
				out = append(out, includeDirective{kind: includeQuoted, target: protoOutputRel(outputRoot, base+".pb.h")})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].target < out[j].target })
	return out
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
	// Walk host protoc and cpp_styleguide tool programs.
	cppStyleguideBinary := pbCppStyleguideVFS
	protocBinary := pbProtocBinaryVFS
	grpcCppBinary := pbGrpcCppVFS

	var cppStyleguideLDRef, protocLDRef, grpcCppLDRef NodeRef

	protocHostInst := NewToolInstance(ctx.host, pbProtocModule)
	protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)

	if exc := Try(func() {
		result := genModule(ctx, protocHostInst)
		protocLDRef = result.LDRef
		if result.LDPath != nil {
			protocBinary = *result.LDPath
		}
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
	}

	cppStyleguideHostInst := NewToolInstance(ctx.host, pbCppStyleguideModule)
	cppStyleguideHostInst.Flags = inferFlagsFromPath(pbCppStyleguideModule, true)

	if exc := Try(func() {
		result := genModule(ctx, cppStyleguideHostInst)
		cppStyleguideLDRef = result.LDRef
		if result.LDPath != nil {
			cppStyleguideBinary = *result.LDPath
		}
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
	}

	if d.grpc {
		grpcCppHostInst := NewToolInstance(ctx.host, pbGrpcCppModule)
		grpcCppHostInst.Flags = inferFlagsFromPath(pbGrpcCppModule, true)

		if exc := Try(func() {
			result := genModule(ctx, grpcCppHostInst)
			grpcCppLDRef = result.LDRef
			if result.LDPath != nil {
				grpcCppBinary = *result.LDPath
			}
		}); exc != nil {
			_ = exc
		}
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
		protoRelPath := protoSourceRelPath(ctx.sourceRoot, instance, d, src)

		pbRef := EmitPB(
			instance, protoRelPath, cppStyleguideLDRef, protocLDRef,
			grpcCppLDRef, cppStyleguideBinary, protocBinary,
			grpcCppBinary, d.grpc,
			stringPtr("cpp_proto"), protoCPPOutRoot(d), duplicateOutputRootInclude, ctx.sourceRoot, ctx.emit)

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
			directImports := protoDirectImportIncludes(ctx.sourceRoot, protoRelPath, protoCPPOutRoot(d))
			extras := pbDescriptorImporterExtras(ctx.sourceRoot, protoRelPath)
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
		event2cppBinary := evEvent2cppBinaryVFS
		var event2cppLDRef NodeRef

		event2cppHostInst := NewToolInstance(ctx.host, evEvent2cppModule)
		event2cppHostInst.Flags = inferFlagsFromPath(evEvent2cppModule, true)

		if exc := Try(func() {
			result := genModule(ctx, event2cppHostInst)
			event2cppLDRef = result.LDRef
			if result.LDPath != nil {
				event2cppBinary = *result.LDPath
			}
		}); exc != nil {
			_ = exc
		}

		for _, src := range evSrcs {
			evRelPath := protoSourceRelPath(ctx.sourceRoot, instance, d, src)

			evRef := EmitEV(
				instance, evRelPath, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				stringPtr("cpp_proto"), ctx.sourceRoot, ctx.emit)

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
				directImports := protoDirectImportIncludes(ctx.sourceRoot, evRelPath, protoCPPOutRoot(d))
				evExtras := evWitnessExtras(ctx.sourceRoot, evRelPath, evPbCC)
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
	// NoStdInc modules zero their own GLOBAL CFLAGS.
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	if instance.Flags.NoStdInc {
		ownCFlagsGlobalSelf = nil
		ownCXXFlagsGlobalSelf = nil
		ownCOnlyFlagsGlobalSelf = nil
	}

	dedupedAddIncl := mergeDedupVFS(d.addIncl, nil)

	moduleInputs := ModuleCCInputs{
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
		ccIn.IsGenerated = true
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

		ccRef, ccOut := EmitCC(cppInstance, co.srcRel, ccIn, ctx.host, ctx.emit)
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
