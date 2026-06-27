package main

import "strings"

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

type ymapsSprotoPending struct {
	ref          NodeRef
	sprotoH      VFS
	protoRelPath string
}

func emitYmapsSprotoHeaders(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerContribs PeerGlobalContribs, produced map[string]struct{}) {
	if len(produced) == 0 {
		return
	}

	outRoot := protoCPPOutRoot(d)
	sprotocRes := ctx.toolResult(argMapsLibsSprotoSprotoc)
	sprotocLDRef, sprotocBinary := sprotocRes.LDRef, *sprotocRes.LDPath
	scanCfg := newScanContext(ctx.parsers, d.addIncl, peerContribs.addIncl, includeScannerBasePaths(), instance.Path.rel())
	pending := make([]ymapsSprotoPending, 0, len(d.ymapsSprotoSrcs))

	for _, srcTok := range d.ymapsSprotoSrcs {
		protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcTok.string())
		sprotoH := build(strings.TrimSuffix(protoRelPath, ".proto"), ".sproto.h")
		sprotoRef := ctx.emit.reserve()
		pbhImports := protoDirectPbHIncludes(ctx.parsers, protoRelPath, outRoot)
		parsed := make([]IncludeDirective, 0, 2*len(pbhImports))

		parsed = append(parsed, pbhImports...)
		parsed = append(parsed, sprotoInducedHeaders(pbhImports)...)

		ctx.codegenFor(instance).register(&GeneratedFileInfo{
			ProducerKvP:    pkPB,
			OutputPath:     sprotoH,
			ProducerRef:    sprotoRef,
			GeneratorRefs:  []NodeRef{sprotocLDRef},
			ParsedIncludes: parsed,
			ClosureLeaves:  []VFS{source(protoRelPath)},
		})

		pending = append(pending, ymapsSprotoPending{ref: sprotoRef, sprotoH: sprotoH, protoRelPath: protoRelPath})
	}

	for _, p := range pending {
		emitYmapsSprotoHeader(ctx, instance, p, outRoot, sprotocLDRef, sprotocBinary, scanCfg)
	}
}

func emitYmapsSprotoHeader(ctx *GenCtx, instance ModuleInstance, p ymapsSprotoPending, outRoot string, sprotocLDRef NodeRef, sprotocBinary VFS, scanCfg ScanContext) {
	na := ctx.emit.nodeArenas()

	cmdArgs := na.chunkList(na.strList(
		sprotocBinary.str(),
		internV("-I=./", outRoot),
		internV("-I=$(S)/", outRoot),
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		internV("--sproto_out=$(B)/", outRoot),
		internStr(p.protoRelPath),
	))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	closure := dropGeneratedProtoHeaders(walkClosureTail(ctx.scannerFor(instance), p.sprotoH, scanCfg))

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: strS,
			Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(sprotocBinary), closure),
		Outputs:        []VFS{p.sprotoH},
		KV:             &ymapsSprotoKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(sprotocLDRef),
	}

	ctx.emit.emitReserved(node, p.ref)
}

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

func sprotoInducedHeaders(pbhImports []IncludeDirective) []IncludeDirective {
	var out []IncludeDirective

	for _, dir := range pbhImports {
		base, ok := strings.CutSuffix(dir.target.string(), ".pb.h")

		if !ok {
			continue
		}

		out = append(out, IncludeDirective{kind: dir.kind, target: internV(base, ".sproto.h")})
	}

	return out
}

var (
	ymapsSprotoKV = KV{P: pkPB, PC: pcYellow}
)
