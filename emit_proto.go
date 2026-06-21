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

const yaffRuntimeBase = "library/cpp/yaff/"

// yaffBaseRuntimeHeaders are the includes the base YaFF C++ generator always
// writes into <proto>.yaff.h (the plugin hardcodes GenerateProtobufApi /
// GenerateReflectionApi / GenerateStructApi = true): see
// library/cpp/yaff/compilation/cpp_gen.cpp GenerateHeader.
var yaffBaseRuntimeHeaders = []string{
	yaffRuntimeBase + "yaff.h",
	yaffRuntimeBase + "struct.h",
	yaffRuntimeBase + "protobuf.h",
	yaffRuntimeBase + "reflect.h",
}

// yaffExperimentsRuntimeHeaders are the extra includes the experiments YaFF C++
// generator appends for an EXPERIMENTAL proto: see
// library/cpp/yaff/experiments/compilation/cpp_gen.cpp GenerateHeader.
var yaffExperimentsRuntimeHeaders = []string{
	yaffRuntimeBase + "experiments/builder.h",
	yaffRuntimeBase + "experiments/column.h",
	yaffRuntimeBase + "experiments/merge.h",
}

// yaffGeneratedHeaderIncludes returns the parsed #includes of a generated
// <proto>.yaff.h: the base yaff runtime, the proto's own .pb.h, and — for an
// EXPERIMENTAL proto — the experiments runtime.
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

// protoDirectPbHResolved maps a .proto's direct imports to the generated .pb.h
// they induce, rooting each at the IMPORTED proto's actual generated-output
// location (found through the same -I roots protoc/our proto scan search via
// resolveProtoImportPath) instead of blindly prefixing the IMPORTER's own
// PROTO_NAMESPACE. protoc writes `#include "<import>.pb.h"` verbatim and ymake
// resolves it against the GLOBAL PROTO_NAMESPACE addincl of the imported lib, so
// a generated header lives under the imported proto's namespace root. For a
// same-namespace import that equals the importer's cppOutRoot (so existing
// behavior is unchanged); for a cross-namespace import (e.g. a yp-namespace
// proto importing yt_proto/...) prefixing cppOutRoot mis-roots the directive to
// a non-existent output and the downstream consumer never reaches the sibling
// header. The resolved directive is the full output rel, so the scanner binds it
// context-free (build-includer lookupSTR) — the resolution-cache invariant holds.
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
			// resolved == <root>/<name>; reroot the induced header under the same
			// <root> the source proto was found at. An unresolved import (no such
			// source on disk) falls through to the verbatim, addincl-resolved form.
			pbH = strings.TrimSuffix(resolved, name) + pbH
		}

		out = append(out, IncludeDirective{kind: d.kind, target: internStr(pbH)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].target.string() < out[j].target.string() })

	return out
}

// protoImportRelsToPbH maps a build-generated proto's declared direct imports
// (OUTPUT_INCLUDES `.proto` rels) to the `.pb.h` includes a checked-in proto's
// parse would have produced — `google/protobuf/*` to the protobuf runtime src,
// every other import to its generated `.pb.h` under the output root. The scanner
// resolves each through its registered `.pb.h` (whose own direct imports it then
// walks), so only the DIRECT imports are seeded here; the transitive closure
// (incl. the canonical descriptor) follows from the per-`.pb.h` walk.
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
	// orderedCC is the proto's buildable codegen outputs (.pb.cc, .grpc.pb.cc,
	// plugin .cpp's) in $CPP_PROTO_OUTS order — the per-proto archive member
	// order. See assembleProtoCmdOutputs.
	orderedCC []VFS
	relPath   string
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
	var genProtoImportRels []string

	buildProto := build(protoRelPath)

	if info := codegenRegForInstance(ctx, instance).lookup(buildProto); info != nil {
		protoSrcOverride = buildProto
		extraProtoDeps = []NodeRef{info.ProducerRef}

		// Upstream's flat-input model folds the generated `.proto` producer's full
		// transitive $(S) closure (its OUTPUT_INCLUDES protos and their imports) onto
		// the protoc PB node, exactly as on the py PB / py3cc nodes. Prefer the full
		// closure; fall back to the direct-leaf SourceInputs when none was recorded.
		protoProducerSourceInputs = info.SourceInputs

		if len(info.ProducerSourceClosure) > 0 {
			protoProducerSourceInputs = info.ProducerSourceClosure
		}

		// A generated proto's source is not scannable for its `import` statements;
		// its declared direct imports ride here as OUTPUT_INCLUDES `.proto` rels.
		genProtoImportRels = info.ProtoImportRels
	}

	// A build-rooted generated transitive import (e.g. a GZT-converted .proto this
	// PB source imports) is produced by its own GZ/codegen node; pin that producer
	// as a PB dep so the generated .proto and its producer-source closure stay
	// reachable from the LD root. Upstream's flat graph carries these producer
	// edges (the raw blender_setup.pb.h record has 4 deps to our 2). deps are not a
	// dumpContentFields member, so this is Merkle-graph parity, not a --pair input.
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

		// A build-generated proto has no scannable source: its direct imports come
		// from the producer's OUTPUT_INCLUDES (recorded as ProtoImportRels). Map them
		// to `.pb.h` exactly as the checked-in parse would, so the generated
		// `<proto>.pb.h` re-exports its imports and the `.pb.cc` compile reaches the
		// full transitive `.pb.h` closure through the scanner's per-header walk.
		if protoSrcOverride != 0 && len(genProtoImportRels) > 0 {
			directImports = protoImportRelsToPbH(genProtoImportRels, cfg.cppOutRoot)
		}

		pbHImports := directImports

		// YMAPS_SPROTO's SET(PROTO_HEADER_EXTS .pb.h .sproto.h): in a maps sproto
		// module, the generated .pb.h additionally #includes the .sproto.h sibling
		// of every imported proto whose .sproto.h this module produces, so a
		// consumer of pb.h (e.g. response.pb.cc.o) reaches the generated .sproto.h
		// and — through its sprotoc GeneratorRef — the sproto runtime headers.
		if siblings := sprotoSiblingDirectives(directImports, sprotoProduced); len(siblings) > 0 {
			pbHImports = append(append([]IncludeDirective(nil), directImports...), siblings...)
		}

		extras := pbHEmitsIncludesExtras()
		pbHParsed := make([]IncludeDirective, 0, len(pbHImports)+len(extras)+len(transitiveImports))
		pbHParsed = append(pbHParsed, pbHImports...)
		pbHParsed = append(pbHParsed, extras...)

		for _, ti := range transitiveImports {
			// A build-generated transitive import (e.g. a peer .gztproto's generated
			// .proto) is a codegen intermediate: it is a real protoc input on THIS PB
			// node, but a unit that #includes this .pb.h reaches the import's
			// pre-generation SOURCE (the .gztproto, riding as a pb.h closure leaf via
			// protoProducerSourceInputs), not the $(B) .proto. Emitting the $(B) path
			// here would drag the generated .proto into every consumer's compile,
			// which upstream does not. Source-rooted imports ride through unchanged.
			if ti.isBuild() {
				continue
			}

			pbHParsed = append(pbHParsed, IncludeDirective{kind: includeQuoted, target: internStr(ti.rel())})
		}

		// protoc induces the protobuf runtime headers; for a grpc service the
		// grpc_cpp plugin induces the grpcpp service headers too. Both via
		// GeneratorRefs (type-split by output kind) instead of hand-woven lists.
		pbGenRefs := []NodeRef{pe.protocLDRef, pe.cppStyleguideLDRef}

		if cfg.grpc {
			pbGenRefs = append(pbGenRefs, pe.grpcCppLDRef)
		}

		// Each C++ proto plugin (incl. event2cpp under CPP_EVLOG) also produces
		// these PB outputs, so its INDUCED_DEPS(h+cpp) ride the .pb.h/.pb.cc closure.
		for _, p := range pe.extraPlugins {
			pbGenRefs = append(pbGenRefs, depRefs(p.LDRef)...)
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
		{
			reg := codegenRegForInstance(ctx, instance)

			if protoSrcOverride == 0 {
				reg.addClosureLeaf(pbH, source(protoRelPath))
			} else {
				for _, s := range protoProducerSourceInputs {
					reg.addClosureLeaf(pbH, s)
				}
			}

			// The YaFF protoc plugin emits a <proto>.yaff.h / .yaff.cpp pair off the
			// same PB node. The header #includes the yaff runtime + the proto's own
			// .pb.h (+ the experiments runtime for an EXPERIMENTAL proto); register
			// it so a unit that includes the generated yaff header rides that
			// closure, and register the .yaff.cpp which includes the header. The
			// .yaff.h / .yaff.cpp are protoc outputs, so protoc's INDUCED_DEPS ride
			// them via GeneratorRefs (header bucket for .yaff.h, cpp bucket for
			// .yaff.cpp — the latter is how wire_format.h, a cpp-only induced dep,
			// reaches the .yaff.cpp.o closure, exactly as it does for .pb.cc).
			protoBaseName := filepath.Base(protoRelPath)

			for _, plugin := range d.cppProtoPlugins {
				if !plugin.isYaff() || len(plugin.OutputSuffixes) != 2 {
					continue
				}

				yaffH := build(protoBase + plugin.OutputSuffixes[0])
				yaffCC := build(protoBase + plugin.OutputSuffixes[1])

				// Upstream NeedToProcessFile: a YaFF output outside the FILES
				// whitelist is opened but written empty, so it carries no closure.
				var yaffHParsed []IncludeDirective
				if plugin.processesFile(protoBaseName) {
					yaffHParsed = yaffGeneratedHeaderIncludes(plugin.isExperimental(protoBaseName), pbH.rel())
				}

				registerBoundGeneratedParsedOutput(ctx, instance, pkPB, yaffH, yaffHParsed, pbRef, nil)

				// The .yaff.cpp wraps `#include "<stem>.yaff.h"`, but the protoc command
				// floats the proto's own .pb.h to the front as its MAIN output
				// (${main;…:.pb.h}); every sibling output (incl. .yaff.cpp) rides that
				// main output via EDT_OutTogether (json_visitor PrepareLeaving), expanded
				// — so the .yaff.cpp.o carries .pb.h plus its producer-source bundle
				// (.proto closure-leaf + cpp_proto_wrapper.py). For a FILES-whitelisted
				// proto the non-empty .yaff.h already #includes .pb.h (this dedupes); for
				// a non-whitelisted proto the .yaff.h is empty, so OutTogether is the only
				// path. Mirrors emit_pr.go's main-output ride; modeled as a parsed include
				// because .pb.h carries its own window (wrapper, .proto) that must expand.
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
	// _SRC("gztproto"): run dict/gazetteer/converter to produce <base>.proto, then
	// compile the generated .proto through the ordinary protoc path below (it is
	// picked up via the codegen-registry protoSrcOverride lookup in emitProtoPB).
	for _, gztSrc := range gztSrcs {
		_, genProtoSrc := emitLibraryGztProtoSource(ctx, instance, d, gztSrc, peerContribs.protoInclude, tagCppProto)
		protoSrcs = append(protoSrcs, genProtoSrc)
	}

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

	cppInstance := instance

	// The set of protos whose .sproto.h this module produces (YMAPS_SPROTO). Known
	// before the PB loop so pb.h registration can add the .sproto.h sibling header;
	// the producer nodes themselves are emitted after the loop (their input closure
	// reaches imported .pb.h, which must be registered first).
	sprotoProduced := ymapsSprotoProducedBases(ctx, instance, d)

	pe := newPBModuleEmission(ctx, d, cfg, peerContribs.protoInclude)

	for _, src := range protoSrcs {
		pb := emitProtoPB(ctx, instance, d, src, cfg, pe, peerContribs.protoInclude, sprotoProduced)

		// orderedCC carries .pb.cc, .grpc.pb.cc and any plugin .cpp's in
		// $CPP_PROTO_OUTS order, which is the per-proto archive member order.
		for _, cc := range pb.orderedCC {
			ccSrcRel := strings.TrimPrefix(cc.rel(), cppInstance.Path.rel()+"/")
			appendCodegenOutput(pb.pbRef, cc, ccSrcRel)
		}
	}

	// Emit the YMAPS_SPROTO .sproto.h producers now that every .pb.h is registered
	// (a .sproto.h includes its imports' .pb.h) and before the generated-C++
	// compile closure is walked (so those units resolve the .sproto.h).
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
		ClangWarnings:        d.clangWarnings,
		SrcDirs:              d.srcDirs,
		FS:                   ctx.fs,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		SetVars:              d.setVars,
		ModuleTag:            tagCppProto,
	}
	// The generated .pb.cc compiles here under the CPP_PROTO submodule. A plugin
	// well-known header it pulls in via a rooted induced-dep (apphost cow
	// *.cow.pb.h) is first-claimed by this module; carry the cpp_proto tag so the
	// claim re-attributes the producer node's module_dir AND module_tag (upstream
	// Node2Module dir+tag inheritance).
	moduleInputs.ScanCfg.OwnerModuleTag = tagCppProto
	moduleInputs.CCBlocks = composeCCModuleArgBlocks(ctx.na, cppInstance.Platform, &moduleInputs)

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

		// A YaFF .yaff.cpp is a thin protoc-plugin wrapper whose only content is
		// `#include "<stem>.yaff.h"` (by basename). Upstream resolves that sibling
		// header for its transitive closure but does not record it as a compile
		// input; drop the self header from the walked closure (the .pb.h / yaff
		// runtime closure was already walked through it and survives). wire_format.h
		// rides in from protoc's INDUCED_DEPS(cpp …) on the .yaff.cpp output (the
		// yaffCC GeneratorRefs), not a hand-woven append.
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
	arRef := emitARNode(instance, archivePath, tagCppProto, ccRefs, ccOutputs, nil, nil, nil, d.tc, ctx.host, ctx.emit)

	return &ProtoSrcsResult{ARRef: arRef, ARPath: &archivePath}
}

func emitLibraryProtoSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	// A LIBRARY-hosted .proto compiles identically to one in a PROTO_LIBRARY:
	// PROTO_NAMESPACE roots the protoc output/import roots (cppOutRoot). A peer
	// that re-declares the namespace re-contributes its `FOR proto $(S)/<ns>`
	// addincl, which rides the peers-only _PROTO__INCLUDE band at its encounter
	// position (in.ProtoIncludePeers), exactly as the PROTO_LIBRARY path. GRPC()
	// additionally enables the grpc_cpp protoc plugin (build/conf/proto.conf): the
	// producer gains .grpc.pb.{cc,h} outputs + the plugin tool input, and the
	// generated .grpc.pb.cc compiles into the module archive — exactly as a
	// PROTO_LIBRARY() does in emitCPPProtoSrcs.
	cfg := ProtoPBConfig{
		cppOutRoot: protoCPPOutRoot(d),
		grpc:       d.grpc,
	}
	pe := newPBModuleEmission(ctx, d, cfg, in.ProtoIncludePeers)
	pb := emitProtoPB(ctx, instance, d, srcRel, cfg, pe, in.ProtoInclude, nil)

	emitGenCC := func(pbCC VFS) SourceEmit {
		ccIn := in
		ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), pbCC, in.ScanCfg)
		ccIn.ExtraDepRefs = append([]NodeRef{pb.pbRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, pb.pbRef)...)
		ccSrcRel := strings.TrimPrefix(pbCC.rel(), instance.Path.rel()+"/")
		ccRef, ccOut, _ := emitCC(instance, ccSrcRel, pbCC, ccIn, ctx.host, ctx.emit)
		return SourceEmit{Ref: ccRef, OutPath: ccOut}
	}

	se := emitGenCC(pb.pbCC)

	if d.grpc {
		se.Extra = append(se.Extra, emitGenCC(pb.grpcPbCC))
	}

	return &se
}
