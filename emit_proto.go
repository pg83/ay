package main

import (
	"path/filepath"
	"sort"
	"strings"
)

var (
	// Parsed-include directives for the constant runtime header lists, built once at
	// init instead of re-interning per generated output. append copies them out, so
	// sharing the read-only backing is safe.
	protobufRuntimeDirectives      = quotedDirectives(protobufRuntimeHeaders)
	pbDescriptorImporterDirectives = quotedDirectives(pbDescriptorImporterHeaders)
	// pbRuntimeBaseVFS is the protobuf runtime src root fed to the proto closure walk.
	pbRuntimeBaseVFS = source(strings.TrimSuffix(pbRuntimeBase, "/"))
)

// yaffBaseRuntimeHeaders are the includes the base YaFF C++ generator always
// writes into <proto>.yaff.h.
var yaffBaseRuntimeHeaders = []string{
	yaffRuntimeBase + "yaff.h",
	yaffRuntimeBase + "struct.h",
	yaffRuntimeBase + "protobuf.h",
	yaffRuntimeBase + "reflect.h",
}

// yaffExperimentsRuntimeHeaders are the extra includes the experiments YaFF C++
// generator appends for an EXPERIMENTAL proto.
var yaffExperimentsRuntimeHeaders = []string{
	yaffRuntimeBase + "experiments/builder.h",
	yaffRuntimeBase + "experiments/column.h",
	yaffRuntimeBase + "experiments/merge.h",
}

func quotedDirectives(headers []VFS) []IncludeDirective {
	out := make([]IncludeDirective, len(headers))

	for i, h := range headers {
		out[i] = IncludeDirective{kind: includeQuoted, target: internStr(h.rel())}
	}

	return out
}

const yaffRuntimeBase = "library/cpp/yaff/"

// yaffGeneratedHeaderIncludes returns the parsed #includes of a generated
// <proto>.yaff.h: the base yaff runtime, the proto's own .pb.h, and the
// experiments runtime for an EXPERIMENTAL proto.
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

// protoDirectPbHResolved maps a .proto's direct imports to the generated .pb.h they
// induce, rooting each at the IMPORTED proto's actual output location (via
// resolveProtoImportPath) rather than the importer's own PROTO_NAMESPACE — a
// cross-namespace import would otherwise mis-root. The directive is the full
// output rel, so the scanner binds it context-free.
func protoDirectPbHResolved(pm *IncludeParserManager, srcRel string, searchPaths []VFS) []IncludeDirective {
	local := pm.sourceParsedBuckets(source(srcRel), nil).bucket(parsedIncludesLocal)

	if len(local) == 0 {
		return nil
	}

	out := make([]IncludeDirective, 0, len(local))

	for _, d := range local {
		name := filepath.ToSlash(filepath.Clean(d.target.string()))

		pbH, ok := protoImportInducedHeader(name)

		if !ok {
			continue
		}

		if strings.HasPrefix(pbH, "google/protobuf/") {
			pbH = pbRuntimeBase + pbH
		} else if resolved := resolveProtoImportPath(pm.fs, name, searchPaths); resolved != "" {
			// Reroot the induced header under the same <root> the source proto was
			// found at. An unresolved import falls through to the verbatim form.
			pbH = strings.TrimSuffix(resolved, name) + pbH
		}

		out = append(out, IncludeDirective{kind: d.kind, target: internStr(pbH)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].target.string() < out[j].target.string() })

	return out
}

// protoImportRelsToPbH maps a build-generated proto's declared direct imports to
// the `.pb.h` includes a checked-in proto's parse would have produced. Only the
// DIRECT imports are seeded; the transitive closure follows from the scanner walk.
func protoImportRelsToPbH(importRels []string, outputRoot string) []IncludeDirective {
	out := make([]IncludeDirective, 0, len(importRels))

	for _, rel := range importRels {
		pbH := strings.TrimSuffix(rel, ".proto") + ".pb.h"

		if strings.HasPrefix(pbH, "google/protobuf/") {
			pbH = pbRuntimeBase + pbH
		} else {
			pbH = protoOutputRel(outputRoot, pbH)
		}

		out = append(out, IncludeDirective{kind: includeQuoted, target: internStr(pbH)})
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

// protoWalkInputs builds the scan inputs for closing over .proto imports,
// mirroring protoc's -I set: the module's _PROTO__INCLUDE chain plus the protobuf
// runtime src.
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

	// Peer contributions land in protoc's -I flags; mirror that so transitive
	// .proto inputs resolve through the same search prefix protoc does.
	for _, p := range peerProtoAddIncl {
		if p.isBuild() {
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
	pbRef    NodeRef
	pbCC     VFS
	grpcPbCC VFS
	// orderedCC is the proto's buildable codegen outputs in $CPP_PROTO_OUTS order,
	// the per-proto archive member order. See assembleProtoCmdOutputs.
	orderedCC []VFS
	relPath   string
}

// PbModuleEmission is the per-module proto emission context: resolved tool
// refs/binaries and stable protoc arg blocks, built ONCE and shared by every PB node.
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

func emitProtoPB(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, cfg ProtoPBConfig, pe *PbModuleEmission, peerProtoAddIncl []VFS, sprotoProduced map[string]struct{}) ProtoPBEmission {
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcRel)
	// Search transitive .proto imports through the same -I prefixes protoc receives:
	// the own PROTO_NAMESPACE (cppOutRoot) plus every peer-contributed namespace.
	// Without the own namespace, a same-namespace sibling import would not resolve.
	protoSearchPaths := peerProtoAddIncl

	if cfg.cppOutRoot != "" {
		protoSearchPaths = append([]VFS{source(cfg.cppOutRoot)}, peerProtoAddIncl...)
	}

	protoVFS := source(protoRelPath)
	transitiveImports := walkClosureTail(ctx.scannerFor(instance), protoVFS, protoWalkInputs(ctx.parsers, protoSearchPaths, instance.Path.rel()).ScanCfg)

	// SRCS(X.proto) may name a build-generated .proto with no source committed.
	// Look it up in the codegen registry: if present, swap srcVFS to the build path
	// and pin the producer ref as a PB dep, else the node is unreachable from the LD
	// root after finalize-DFS.
	var protoSrcOverride VFS
	var extraProtoDeps []NodeRef
	var protoProducerSourceInputs []VFS
	var genProtoImportRels []string

	buildProto := build(protoRelPath)

	if info := codegenRegForInstance(ctx, instance).lookup(buildProto); info != nil {
		protoSrcOverride = buildProto
		extraProtoDeps = []NodeRef{info.ProducerRef}

		// Fold the generated proto producer's full transitive $(S) closure onto the
		// PB node; fall back to direct-leaf SourceInputs when none was recorded.
		protoProducerSourceInputs = info.SourceInputs

		if len(info.ProducerSourceClosure) > 0 {
			protoProducerSourceInputs = info.ProducerSourceClosure
		}

		// A generated proto's source is not scannable; its direct imports ride here
		// as OUTPUT_INCLUDES `.proto` rels.
		genProtoImportRels = info.ProtoImportRels
	}

	// A build-rooted generated transitive import is produced by its own codegen
	// node; pin that producer as a PB dep so it stays reachable from the LD root.
	// deps are not a dumpContentFields member, so this is Merkle-graph parity.
	extraProtoDeps = append(extraProtoDeps,
		resolveCodegenDepRefs(ctx, instance, transitiveImports, extraProtoDeps...)...)

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

	for _, plugin := range d.cppProtoPlugins {
		for _, suffix := range plugin.OutputSuffixes {
			extraOutputPaths = append(extraOutputPaths, build(protoBase+suffix))
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

	{
		directImports := protoDirectPbHResolved(ctx.parsers, protoRelPath, protoSearchPaths)

		// A build-generated proto's direct imports come from ProtoImportRels; map
		// them to `.pb.h` as the checked-in parse would, so the generated `.pb.h`
		// re-exports its imports.
		if protoSrcOverride != 0 && len(genProtoImportRels) > 0 {
			directImports = protoImportRelsToPbH(genProtoImportRels, cfg.cppOutRoot)
		}

		pbHImports := directImports

		// In a sproto module the generated .pb.h also #includes the .sproto.h sibling
		// of every imported proto this module produces, so a pb.h consumer reaches the
		// .sproto.h and, through its sprotoc GeneratorRef, the sproto runtime headers.
		if siblings := sprotoSiblingDirectives(directImports, sprotoProduced); len(siblings) > 0 {
			pbHImports = append(append([]IncludeDirective(nil), directImports...), siblings...)
		}

		extras := pbHEmitsIncludesExtras()
		pbHParsed := make([]IncludeDirective, 0, len(pbHImports)+len(extras)+len(transitiveImports))
		pbHParsed = append(pbHParsed, pbHImports...)
		pbHParsed = append(pbHParsed, extras...)

		for _, ti := range transitiveImports {
			// A build-generated transitive import is a real protoc input on THIS PB
			// node, but a unit including this .pb.h reaches the import's pre-generation
			// SOURCE (via protoProducerSourceInputs), not the $(B) .proto. Emitting the
			// $(B) path here would drag the generated .proto into every consumer.
			if ti.isBuild() {
				continue
			}

			pbHParsed = append(pbHParsed, IncludeDirective{kind: includeQuoted, target: internStr(ti.rel())})
		}

		// protoc induces the protobuf runtime headers; a grpc service additionally
		// induces the grpcpp service headers. Both via GeneratorRefs.
		pbGenRefs := []NodeRef{pe.protocLDRef, pe.cppStyleguideLDRef}

		if cfg.grpc {
			pbGenRefs = append(pbGenRefs, pe.grpcCppLDRef)
		}

		// Each C++ proto plugin also produces these PB outputs, so its
		// INDUCED_DEPS(h+cpp) ride the .pb.h/.pb.cc closure.
		for _, p := range pe.extraPlugins {
			pbGenRefs = append(pbGenRefs, depRefs(p.LDRef)...)
		}

		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, pbH, pbHParsed, pbRef, pbGenRefs)

		// The source a generated header is produced FROM is a real input of every
		// unit that includes it. Ride it as a non-expanded closure leaf of pb.h: the
		// .proto itself for a real $(S) source, or the generator's own $(S) sources
		// (protoProducerSourceInputs) for a generated $(B) .proto.
		{
			reg := codegenRegForInstance(ctx, instance)

			if protoSrcOverride == 0 {
				reg.addClosureLeaf(pbH, source(protoRelPath))
			} else {
				for _, s := range protoProducerSourceInputs {
					reg.addClosureLeaf(pbH, s)
				}
			}

			// The YaFF plugin emits a <proto>.yaff.h / .yaff.cpp pair off the same PB
			// node. The header #includes the yaff runtime + the proto's .pb.h; register
			// both so a unit including the header rides that closure. Both are protoc
			// outputs, so protoc's INDUCED_DEPS ride them via GeneratorRefs.
			protoBaseName := filepath.Base(protoRelPath)

			for _, plugin := range d.cppProtoPlugins {
				if !plugin.isYaff() || len(plugin.OutputSuffixes) != 2 {
					continue
				}

				yaffH := build(protoBase + plugin.OutputSuffixes[0])
				yaffCC := build(protoBase + plugin.OutputSuffixes[1])

				// A YaFF output outside the FILES whitelist is written empty.
				var yaffHParsed []IncludeDirective

				if plugin.processesFile(protoBaseName) {
					yaffHParsed = yaffGeneratedHeaderIncludes(plugin.isExperimental(protoBaseName), pbH.rel())
				}

				registerBoundGeneratedParsedOutput(ctx, instance, pkPB, yaffH, yaffHParsed, pbRef, nil)

				// The .yaff.cpp wraps `#include "<stem>.yaff.h"`. The proto's .pb.h is
				// added explicitly here so the .yaff.cpp.o carries .pb.h's window even
				// when the .yaff.h is empty (non-whitelisted); for a whitelisted proto
				// the non-empty .yaff.h already #includes .pb.h, so this dedupes.
				yaffCCParsed := []IncludeDirective{
					{kind: includeQuoted, target: internStr(yaffH.rel())},
					{kind: includeQuoted, target: internStr(pbH.rel())},
				}
				registerBoundGeneratedParsedOutput(ctx, instance, pkPB, yaffCC, yaffCCParsed, pbRef, pbGenRefs)
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

func emitProtoSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs) *ProtoSrcsResult {
	var protoSrcs, evSrcs, gztSrcs []string

	for _, src := range d.srcs {
		switch {
		case strings.HasSuffix(src.string(), ".proto"):
			protoSrcs = append(protoSrcs, src.string())
		case strings.HasSuffix(src.string(), ".ev"):
			evSrcs = append(evSrcs, src.string())
		case strings.HasSuffix(src.string(), ".gztproto"):
			gztSrcs = append(gztSrcs, src.string())
		}
	}

	if len(protoSrcs) == 0 && len(evSrcs) == 0 && len(gztSrcs) == 0 {
		return nil
	}

	switch instance.Language {
	case LangPy:
		return emitPyProtoSrcs(ctx, instance, d, peerContribs, protoSrcs, evSrcs)
	default:
		return emitCPPProtoSrcs(ctx, instance, d, peerContribs, protoSrcs, evSrcs, gztSrcs)
	}
}

func emitCPPProtoSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs, protoSrcs, evSrcs, gztSrcs []string) *ProtoSrcsResult {
	// Source FIFO position of each direct SRC: a generated .pb.cc is queued back at
	// its source's declaration position, so proto/ev compiles archive interleaved by
	// SRCS textual order. Gzt-generated protos sort after all direct sources.
	srcDeclIdx := make(map[string]int, len(d.srcs))

	for i, src := range d.srcs {
		if _, seen := srcDeclIdx[src.string()]; !seen {
			srcDeclIdx[src.string()] = i
		}
	}

	// A .gztproto runs a converter to produce <base>.proto, then compiles it
	// through the ordinary protoc path below (via the protoSrcOverride lookup).
	for i, gztSrc := range gztSrcs {
		_, genProtoSrc := emitLibraryGztProtoSource(ctx, instance, d, gztSrc, peerContribs.protoInclude, tagCppProto)
		protoSrcs = append(protoSrcs, genProtoSrc)
		// A gztproto's generated .proto sorts at the tail, past every direct source.
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

	// The protos whose .sproto.h this module produces. Known before the PB loop so
	// pb.h registration can add the .sproto.h sibling; the producer nodes emit after
	// the loop (their closure reaches imported .pb.h, which must register first).
	sprotoProduced := ymapsSprotoProducedBases(ctx, instance, d)

	pe := newPBModuleEmission(ctx, d, cfg, peerContribs.protoInclude)

	for _, src := range protoSrcs {
		pb := emitProtoPB(ctx, instance, d, src, cfg, pe, peerContribs.protoInclude, sprotoProduced)

		// orderedCC carries the codegen outputs in per-proto archive member order.
		for _, cc := range pb.orderedCC {
			ccSrcRel := strings.TrimPrefix(cc.rel(), cppInstance.Path.rel()+"/")
			appendCodegenOutput(pb.pbRef, cc, ccSrcRel, srcDeclIdx[src])
		}
	}

	// Emit the .sproto.h producers now that every .pb.h is registered and before the
	// generated-C++ compile closure is walked.
	emitYmapsSprotoHeaders(ctx, instance, d, peerContribs, sprotoProduced)

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
				tagCppProto, evImports, peerContribs.protoInclude,
				!protoTransitiveHeadersEnabled(d),
				d.tc, ctx.emit)

			evH := build(evRelPath + ".pb.h")
			evPbCC := build(evRelPath + ".pb.cc")

			{
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
			evSrcRel := strings.TrimPrefix(evRelPath+".pb.cc", cppInstance.Path.rel()+"/")
			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef:  evRef,
				pbCC:    evPbCC,
				srcRel:  evSrcRel,
				declIdx: srcDeclIdx[src],
			})
		}
	}

	// Interleave proto and ev members by SRCS declaration order. Stable so each
	// proto's orderedCC members keep their relative order and gzt-generated protos
	// stay at the tail.
	sort.SliceStable(codegenOutputs, func(i, j int) bool {
		return codegenOutputs[i].declIdx < codegenOutputs[j].declIdx
	})

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
		ClangWarnings:        d.clangWarnings,
		SrcDirs:              d.srcDirs,
		FS:                   ctx.fs,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		SetVars:              d.setVars,
		ModuleTag:            tagCppProto,
	}
	// The generated .pb.cc compiles under the CPP_PROTO submodule; carry the
	// cpp_proto tag so an induced-dep header first-claimed here re-attributes the
	// producer node's module_dir AND module_tag.
	moduleInputs.ScanCfg.OwnerModuleTag = tagCppProto
	moduleInputs.CCBlocks = composeCCModuleArgBlocks(ctx.na, cppInstance.Platform, &moduleInputs)

	ccRefs := make([]NodeRef, 0, len(codegenOutputs))
	ccOutputs := make([]VFS, 0, len(codegenOutputs))

	// arDeclMeta carries each archive member's AR-ordering key so reorderARMembers
	// merges proto/ev codegen, RUN_ANTLR .cpp, and enum-serialization members into
	// one faithful order. Proto/ev .pb.cc sit in the prio-4 SRCS band keyed by SRCS
	// declaration index.
	arDeclMeta := map[VFS]SrcMeta{}

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

		// A YaFF .yaff.cpp only `#include`s its own .yaff.h sibling. Resolve that
		// header for its closure but drop the self header from the walked input set
		// (the .pb.h / yaff runtime closure already rode through it).
		if strings.HasSuffix(co.srcRel, ".yaff.cpp") {
			selfH := build(strings.TrimSuffix(co.pbCC.rel(), ".cpp") + ".h")
			filtered := make([]VFS, 0, len(ccIn.IncludeInputs))

			for _, in := range ccIn.IncludeInputs {
				if in == selfH {
					continue
				}

				filtered = append(filtered, in)
			}

			ccIn.IncludeInputs = filtered
		}

		ccIn.ExtraDepRefs = append([]NodeRef{co.genRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, co.genRef)...)
		ccRef, ccOut, _ := emitCC(cppInstance, co.srcRel, co.pbCC, ccIn, ctx.host, ctx.emit)
		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)
		arDeclMeta[ccOut] = SrcMeta{Prio: stmtPrioSrcs, Seq: co.declIdx, Generated: true}
	}

	enRes := emitEnumSrcs(ctx, instance, d, peerContribs.addIncl, &moduleInputs)

	if enRes != nil {
		// External/source-header EN (prio-2 band) archives ahead of this module's
		// proto .pb.cc.o; a same-module generated-.pb.h EN is second-level and
		// archives after every first-level member.
		for i := range enRes.CCRefs {
			ccRefs = append(ccRefs, enRes.CCRefs[i])
			ccOutputs = append(ccOutputs, enRes.CCOutputs[i])
			arDeclMeta[enRes.CCOutputs[i]] = SrcMeta{Prio: stmtPrioDefault, Seq: enRes.Seqs[i], Generated: true, SecondLevel: enRes.SecondLevel[i]}
		}
	}

	// RUN_ANTLR .cpp outputs inside a PROTO_LIBRARY auto-promote to SRCS and, as
	// ordinary translation units, order BEFORE the proto .pb.cc.o objects: compile
	// each here, collect separately, and prepend.
	var antlrRefs []NodeRef
	var antlrOutputs []VFS

	{
		reg := codegenRegForInstance(ctx, instance)

		for _, run := range d.antlrRuns {
			for _, outTok := range run.OUTFiles {
				if !isCCSourceExt(outTok.string()) {
					continue
				}

				outVFS := copyFileOutputVFS(instance.Path.rel(), outTok.string())
				info := reg.lookup(outVFS)

				if info == nil {
					continue
				}

				cppRel := antlrOutputModuleRel(instance.Path.rel(), outVFS)
				ccRef, ccOut := emitCodegenDownstreamCC(ctx, cppInstance, cppRel, []NodeRef{info.ProducerRef}, moduleInputs)
				antlrRefs = append(antlrRefs, ccRef)
				antlrOutputs = append(antlrOutputs, ccOut)
				// RUN_ANTLR .cpp sits in the prio-2 band, ahead of the proto .pb.cc.o.
				arDeclMeta[ccOut] = SrcMeta{Prio: stmtPrioDefault, Generated: true}
			}
		}
	}

	if len(antlrRefs) > 0 {
		ccRefs = append(antlrRefs, ccRefs...)
		ccOutputs = append(antlrOutputs, ccOutputs...)
	}

	ccRefs, ccOutputs = reorderARMembers(ccRefs, ccOutputs, arDeclMeta)

	var protoLibName string

	if len(d.moduleStmt.Args) > 0 {
		protoLibName = d.moduleStmt.Args[0].string()
	}

	arBaseName := archiveNameWithPrefixOrName(instance.Path.rel(), "lib", protoLibName)
	archivePath := build(instance.Path.rel() + "/" + arBaseName)
	arRef := emitARNode(instance, archivePath, tagCppProto, ccRefs, ccOutputs, nil, nil, nil, d.tc, ctx.host, ctx.emit)

	return &ProtoSrcsResult{ARRef: arRef, ARPath: &archivePath}
}

// emitProtoProducer emits the PB node for one LIBRARY-hosted .proto and registers
// its outputs. Run in a pre-pass before any proto CC closure is walked, so a
// .proto importing a LATER-declared same-module sibling resolves the sibling's
// generated .pb.h. Compiles identically to a PROTO_LIBRARY proto.
func emitProtoProducer(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) {
	cfg := ProtoPBConfig{
		cppOutRoot: protoCPPOutRoot(d),
		grpc:       d.grpc,
	}
	pe := newPBModuleEmission(ctx, d, cfg, in.ProtoIncludePeers)
	emitProtoPB(ctx, instance, d, srcRel, cfg, pe, in.ProtoInclude, nil)
}

func emitLibraryProtoSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	// The PB producer was emitted+registered by the pre-pass; take its ref from the
	// codegen registry and compile the generated .pb.cc.
	protoBase := strings.TrimSuffix(protoSourceRelPath(ctx.fs, instance, d, srcRel), ".proto")
	pbRef := codegenRegForInstance(ctx, instance).lookup(build(protoBase + ".pb.cc")).ProducerRef

	emitGenCC := func(pbCC VFS) SourceEmit {
		ccIn := in
		ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), pbCC, in.ScanCfg)
		ccIn.ExtraDepRefs = append([]NodeRef{pbRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, pbRef)...)
		ccSrcRel := strings.TrimPrefix(pbCC.rel(), instance.Path.rel()+"/")
		ccRef, ccOut, _ := emitCC(instance, ccSrcRel, pbCC, ccIn, ctx.host, ctx.emit)

		return SourceEmit{Ref: ccRef, OutPath: ccOut}
	}

	se := emitGenCC(build(protoBase + ".pb.cc"))

	if d.grpc {
		se.Extra = append(se.Extra, emitGenCC(build(protoBase+".grpc.pb.cc")))
	}

	return &se
}
