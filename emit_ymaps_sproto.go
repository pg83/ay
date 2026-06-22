package main

import "strings"

// ymapsSprotoProducedBases is the set of proto bases (no .proto) that
// YMAPS_SPROTO(...) names. Computed before the PB loop so pb.h registration adds
// the .sproto.h sibling only for produced imports.
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

// emitYmapsSprotoHeaders models the YMAPS_SPROTO(...) macro: per listed .proto it
// emits one PB/yellow .sproto.h producer running sprotoc, registered against the
// sprotoc LD ref as a GeneratorRef so the tool's induced deps ride into consumers
// and the producer's own flat input closure.
//
// It runs AFTER the module's .pb.h producers (a .sproto.h #includes each imported
// proto's .pb.h) and before the generated-C++ compile closure is walked.
// ymapsSprotoPending carries a reserved producer between the two passes so a
// sibling .sproto.h include resolves regardless of declaration order.
type ymapsSprotoPending struct {
	ref          NodeRef
	sprotoH      VFS
	protoRelPath string
}

func emitYmapsSprotoHeaders(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs, produced map[string]struct{}) {
	if len(produced) == 0 {
		return
	}

	// PROTO_NAMESPACE output root; the sprotoc args and .sproto.h output root here.
	outRoot := protoCPPOutRoot(d)

	sprotocRes := ctx.toolResult(argMapsLibsSprotoSprotoc)
	sprotocLDRef, sprotocBinary := sprotocRes.LDRef, *sprotocRes.LDPath

	scanCfg := newScanContext(ctx.parsers, d.addIncl, peerContribs.addIncl, includeScannerBasePaths(), instance.Path.rel())

	// Pass 1: reserve a ref and register every .sproto.h output (and its source
	// .proto leaf) BEFORE any producer closure is walked in pass 2.
	pending := make([]ymapsSprotoPending, 0, len(d.ymapsSprotoSrcs))

	for _, srcTok := range d.ymapsSprotoSrcs {
		protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcTok.string())
		sprotoH := build(strings.TrimSuffix(protoRelPath, ".proto") + ".sproto.h")
		sprotoRef := ctx.emit.reserve()

		// The .sproto.h #includes both the .pb.h and .sproto.h sibling per imported
		// proto, pulling the imports' message closures; sproto.h itself rides via
		// the sprotoc GeneratorRef.
		pbhImports := protoDirectPbHIncludes(ctx.parsers, protoRelPath, outRoot)
		parsed := make([]IncludeDirective, 0, 2*len(pbhImports))
		parsed = append(parsed, pbhImports...)
		parsed = append(parsed, sprotoSiblingDirectives(pbhImports, produced)...)

		registerBoundGeneratedParsedOutput(ctx, instance, pkPB, sprotoH, parsed, sprotoRef, []NodeRef{sprotocLDRef})

		// The source .proto is a real input: ride it as a non-expanded closure leaf,
		// mirroring the .pb.h registration.
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

	// The flat input model lists the header's full transitive closure on its
	// producer; walkClosureTail yields that, root stripped. A producer never lists
	// OTHER generated proto-headers (they reach the build via their own producers),
	// so sibling header nodes must be dropped after the walk.
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
// (.pb.h / .sproto.h) from a producer-node input closure: the flat input model
// lists source-level files and the generator tool only.
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
// has a module-local YMAPS_SPROTO producer to its .sproto.h sibling. The produced
// guard avoids inventing inputs for ordinary proto modules.
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
