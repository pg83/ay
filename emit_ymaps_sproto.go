package main

import "strings"

var ymapsSprotoKV = KV{P: pkPB, PC: pcYellow}

func (e *EmitContext) ymapsSprotoProducedBases() map[string]struct{} {
	ctx, instance, d := e.ctx, e.instance, e.d

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

type YmapsSprotoPending struct {
	ref          NodeRef
	sprotoH      VFS
	protoRelPath string
}

func (e *EmitContext) emitYmapsSprotoStmt(srcTok STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	outRoot := protoCPPOutRoot(d)
	sprotocRes := ctx.toolResult(argMapsLibsSprotoSprotoc)
	sprotocLDRef, sprotocBinary := sprotocRes.LDRef, *sprotocRes.LDPath
	scanCfg := newScanContext(ctx.parsers, d.addIncl, e.peers.PeerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcTok.string())
	sprotoH := build(strings.TrimSuffix(protoRelPath, ".proto"), ".sproto.h")
	sprotoRef := ctx.emit.reserve()
	pbhImports := protoDirectPbHIncludes(ctx.parsers, protoRelPath, outRoot)
	parsed := make([]IncludeDirective, 0, 2*len(pbhImports))

	parsed = append(parsed, pbhImports...)
	parsed = append(parsed, sprotoInducedHeaders(pbhImports)...)

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     sprotoH,
		ProducerRef:    sprotoRef,
		GeneratorRefs:  []NodeRef{sprotocLDRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		ClosureLeaves:  []VFS{source(protoRelPath)},
	})

	pending := YmapsSprotoPending{ref: sprotoRef, sprotoH: sprotoH, protoRelPath: protoRelPath}

	e.deferPass2(func() {
		e.emitYmapsSprotoHeader(pending, outRoot, sprotocLDRef, sprotocBinary, scanCfg)
	})
}

func (e *EmitContext) emitYmapsSprotoHeader(p YmapsSprotoPending, outRoot string, sprotocLDRef NodeRef, sprotocBinary VFS, scanCfg ScanContext) {
	ctx, instance := e.ctx, e.instance
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
	sprotoCV := walkClosure(e.scanner, p.sprotoH, scanCfg)

	closure := collectBucketVFS(sprotoCV.buckets, func(v VFS) bool {
		return v.isSource() || !extIsProtoGeneratedHeader(v.rel())
	})

	node := Node{
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

	ctx.emit.emitReservedNode(node, p.ref)
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
