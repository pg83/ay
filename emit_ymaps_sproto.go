package main

import "strings"

var ymapsSprotoKV = KV{P: pkPB, PC: pcYellow}

const sprotoPbHCompanionExt = ".sproto.h"

func (e *EmitContext) sprotoAdjustProtoEnv() {
	if len(e.d.ymapsSprotoSrcs) == 0 {
		return
	}

	e.d.cc.PbHCompanionExt = sprotoPbHCompanionExt
}

type YmapsSprotoPending struct {
	ref      NodeRef
	sprotoH  VFS
	protoRel STR
}

func (e *EmitContext) emitYmapsSprotoStmt(srcTok ANY) {
	ctx, d := e.ctx, e.d
	outRoot := protoCPPOutRoot(d)
	sprotocRes := ctx.toolResult(argMapsLibsSprotoSprotoc)
	sprotocLDRef, sprotocBinary := sprotocRes.LDRef, *sprotocRes.LDPath
	scanCtx := d.scanCtx
	protoRel := e.protoSourceRel(srcTok.string())
	protoRelPath := protoRel.string()
	sprotoH := build(strings.TrimSuffix(protoRelPath, ".proto"), ".sproto.h")
	sprotoRef := ctx.emit.reserve()
	pbhImports := protoDirectPbHIncludes(ctx.parsers, protoRelPath, outRoot, e.dirScratch[:0])

	e.dirScratch = pbhImports

	parsed := ctx.na.dirs.alloc(2 * len(pbhImports))[:0]

	parsed = append(parsed, pbhImports...)
	parsed = appendPbHCompanions(parsed, pbhImports, sprotoPbHCompanionExt)
	ctx.na.dirs.commit(len(parsed))
	parsed = parsed[:len(parsed):len(parsed)]

	pending := YmapsSprotoPending{ref: sprotoRef, sprotoH: sprotoH, protoRel: protoRel}
	scanner := e.scanner

	pe := func() {
		e.emitYmapsSprotoHeaderSnap(scanner, pending, outRoot, sprotocLDRef, sprotocBinary, scanCtx)
	}

	onUse := e.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     sprotoH,
		ProducerRef:    sprotoRef,
		GeneratorRefs:  e.ctx.na.refList(sprotocLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},

		OnUse: onUse,
	})
}

func (e *EmitContext) emitYmapsSprotoHeaderSnap(scanner *IncludeScanner, p YmapsSprotoPending, outRoot string, sprotocLDRef NodeRef, sprotocBinary VFS, scanCtx *ScanContext) {
	na := e.ctx.na

	cmdArgs := na.chunkList(na.anyList(
		sprotocBinary.any(),
		internV("-I=./", outRoot).any(),
		internV("-I=$(S)/", outRoot).any(),
		argIB2.any(),
		argISContribLibsProtobufSrc.any(),
		internV("--sproto_out=$(B)/", outRoot).any(),
		p.protoRel.any(),
	))

	env := envVarsVCS
	view := scanner.walkClosure(p.protoRel.source(), scanCtx, scanDomainProto)
	closure := view.collect(na, func(VFS) bool { return true })

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: srcRootDirVFS,
			Env: env}),
		Env:             env,
		Inputs:          na.inputList(na.vfsList(sprotocBinary), closure),
		BuildFromInputs: na.inputList(closure),
		InputClosures:   []Closure{view},
		Outputs:         na.vfsList(p.sprotoH),
		KV:              &ymapsSprotoKV,
		ForeignDepRefs:  na.refList(sprotocLDRef),
	}

	e.emitReservedNode(node, p.ref)
}
