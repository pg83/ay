package main

import "strings"

// ymapsSprotoProducedBases is the set of source-root-relative proto bases (path
// without the trailing .proto) that YMAPS_SPROTO(...) names in this module — the
// protos whose .sproto.h this module generates. Computed before the PB loop so
// pb.h-header registration can add the .sproto.h sibling only for produced
// imports (the macro's SET(PROTO_HEADER_EXTS .pb.h .sproto.h)).
func ymapsSprotoProducedBases(ctx *GenCtx, instance ModuleInstance, d *ModuleData) map[string]struct{} {
	if len(d.ymapsSprotoSrcs) == 0 {
		return nil
	}

	produced := make(map[string]struct{}, len(d.ymapsSprotoSrcs))

	for _, srcTok := range d.ymapsSprotoSrcs {
		protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcTok.string())
		produced[strings.TrimSuffix(protoRelPath, ".proto")] = struct{}{}
	}

	return produced
}

// emitYmapsSprotoHeaders models the maps-specific YMAPS_SPROTO(...) macro
// (build/internal/conf/project_specific/maps/sproto.conf). For each listed
// .proto it emits one PB/yellow .sproto.h producer that runs
// maps/libs/sproto/sprotoc with maps/doc/proto-rooted include/output arguments
// (_YMAPS_GENERATE_SPROTO_HEADER). The generated header is registered against
// the sprotoc LD ref as a GeneratorRef so the tool's
// INDUCED_DEPS(h+cpp maps/libs/sproto/include/sproto.h) ride — via the existing
// scanner.resolveInducedDeps path — into every consumer that includes the
// header AND into the producer node's own flat input closure (upstream attaches
// a ${tool}'s induced deps to the node that uses the tool; that is why the
// reference .sproto.h producer carries the full sproto.h C++ closure as inputs,
// unlike the .pb.h producer whose protoc has no such induced deps).
//
// It must run AFTER the module's .pb.h producers are registered: a .sproto.h
// #includes each imported proto's .pb.h, so its input closure (walked below)
// only reaches the imports' protobuf-descriptor C++ closures once those .pb.h
// are in the codegen registry. It must also run before the generated-C++ compile
// closure is walked, so consumers resolve the .sproto.h.
// ymapsSprotoPending carries a reserved .sproto.h producer between the two
// emission passes: every output is registered in the codegen registry before
// any producer closure is walked, so a producer whose generated header includes
// a sibling .sproto.h resolves it regardless of declaration order (include
// resolution stays context-independent — a sibling registered after the walk
// would otherwise be cached unresolved).
type ymapsSprotoPending struct {
	ref          NodeRef
	sprotoH      VFS
	protoRelPath string
}

func emitYmapsSprotoHeaders(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs, produced map[string]struct{}) {
	if len(produced) == 0 {
		return
	}

	// PROTO_NAMESPACE output root (EXPORT_YMAPS_PROTO -> maps/doc/proto). The
	// sprotoc -I/--sproto_out arguments and the .sproto.h output all root here.
	outRoot := protoCPPOutRoot(d)

	sprotocRes := ctx.toolResult(argMapsLibsSprotoSprotoc)
	sprotocLDRef, sprotocBinary := sprotocRes.LDRef, *sprotocRes.LDPath

	scanCfg := newScanContext(ctx.parsers, d.addIncl, peerContribs.addIncl, includeScannerBasePaths(), instance.Path.rel())

	// Pass 1: reserve a ref and register every .sproto.h output (and its source
	// .proto closure leaf) BEFORE any producer closure is walked in pass 2.
	pending := make([]ymapsSprotoPending, 0, len(d.ymapsSprotoSrcs))

	for _, srcTok := range d.ymapsSprotoSrcs {
		protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcTok.string())
		sprotoH := build(strings.TrimSuffix(protoRelPath, ".proto") + ".sproto.h")
		sprotoRef := ctx.emit.reserve()

		// The generated .sproto.h #includes, for every imported proto, both the
		// .pb.h and the .sproto.h sibling (PROTO_HEADER_EXTS .pb.h .sproto.h). Those
		// pull the imports' protobuf-message closures; sproto.h itself rides via the
		// sprotoc GeneratorRef below.
		pbhImports := protoDirectPbHIncludes(ctx.parsers, protoRelPath, outRoot)
		parsed := make([]IncludeDirective, 0, 2*len(pbhImports))
		parsed = append(parsed, pbhImports...)
		parsed = append(parsed, sprotoSiblingDirectives(pbhImports, produced)...)

		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, sprotoH, parsed, sprotoRef, []NodeRef{sprotocLDRef})

		// The source .proto a generated header is produced from is a real input of
		// the producer (and of every unit that includes the header): ride it as a
		// non-expanded closure leaf, mirroring the .pb.h registration.
		codegenRegForInstance(ctx, instance).addClosureLeaf(sprotoH, source(protoRelPath))

		pending = append(pending, ymapsSprotoPending{ref: sprotoRef, sprotoH: sprotoH, protoRelPath: protoRelPath})
	}

	// Pass 2: walk each producer's closure (all siblings now registered) and emit.
	for _, p := range pending {
		emitYmapsSprotoHeader(ctx, instance, p, outRoot, sprotocLDRef, sprotocBinary, scanCfg)
	}
}

func emitYmapsSprotoHeader(ctx *GenCtx, instance ModuleInstance, p ymapsSprotoPending, outRoot string, sprotocLDRef NodeRef, sprotocBinary VFS, scanCfg ScanContext) {
	na := ctx.emit.nodeArenas()

	// .CMD=${cwd;rootdir;input:File} ${tool:"maps/libs/sproto/sprotoc"}
	//   -I=./$PROTO_NAMESPACE -I=$ARCADIA_ROOT/$PROTO_NAMESPACE -I=$ARCADIA_BUILD_ROOT
	//   -I=$PROTOBUF_INCLUDE_PATH --sproto_out=$ARCADIA_BUILD_ROOT/$PROTO_NAMESPACE
	//   ${rootrel;input:File} ${hide;norel;output;suf=.sproto.h;...:File}
	cmdArgs := na.chunkList(na.strList(
		sprotocBinary.str(),
		internStr("-I=./"+outRoot),
		internStr("-I=$(S)/"+outRoot),
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		internStr("--sproto_out=$(B)/"+outRoot),
		internStr(p.protoRelPath),
	))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// Upstream's flat input model lists the full transitive closure of the
	// generated header on its producer node; walkClosureTail yields exactly that
	// (sproto.h induced closure + imported headers' closures + the source proto
	// leaf), with the .sproto.h root stripped.
	//
	// A generated proto-header producer never lists OTHER generated proto-headers
	// (.pb.h / .sproto.h) as flat inputs — those are reached through their own
	// producer nodes in the build graph; only the source-level closure they pull
	// in (protobuf runtime, cpp_proto_wrapper.py, libcxx) belongs here. The
	// ordinary .pb.h producer (emitPB) gets this for free by walking the source
	// .proto; this producer walks the generated header (to reach an imported
	// proto's .pb.h descriptor closure), so the generated sibling header nodes
	// must be dropped after the walk.
	closure := dropGeneratedProtoHeaders(walkClosureTail(ctx.scannerFor(instance), p.sprotoH, scanCfg))

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: strS,
			Env: env}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(sprotocBinary), closure),
		Outputs:          []VFS{p.sprotoH},
		KV:               KV{P: pkPB, PC: pcYellow},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleTag: tagCppProto},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs:   depRefs(sprotocLDRef),
	}

	ctx.emit.emitReserved(node, p.ref)
}

// dropGeneratedProtoHeaders removes build-tree generated proto headers
// (.pb.h / .sproto.h) from a producer-node input closure. Upstream's flat input
// model for a generated proto header lists source-level files and the generator
// tool only; sibling generated headers reach the build through their own
// producer nodes, not as flat inputs of another codegen node.
func dropGeneratedProtoHeaders(closure []VFS) []VFS {
	var out []VFS

	for _, v := range closure {
		if !v.isSource() {
			rel := v.rel()

			if strings.HasSuffix(rel, ".pb.h") || strings.HasSuffix(rel, ".sproto.h") {
				continue
			}
		}

		out = append(out, v)
	}

	return out
}

// sprotoSiblingDirectives maps each induced .pb.h import directive whose proto
// has a module-local YMAPS_SPROTO producer to its .sproto.h sibling. Guarding on
// the produced set keeps the .sproto.h induce edge faithful to upstream — only
// protos whose .sproto.h this module generates — and avoids inventing .sproto.h
// inputs for ordinary proto modules.
func sprotoSiblingDirectives(pbhImports []IncludeDirective, produced map[string]struct{}) []IncludeDirective {
	if len(produced) == 0 {
		return nil
	}

	var out []IncludeDirective

	for _, dir := range pbhImports {
		base, ok := strings.CutSuffix(dir.target.string(), ".pb.h")

		if !ok {
			continue
		}

		if _, p := produced[base]; !p {
			continue
		}

		out = append(out, IncludeDirective{kind: dir.kind, target: internStr(base + ".sproto.h")})
	}

	return out
}
