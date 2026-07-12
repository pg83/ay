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
	ref          NodeRef
	sprotoH      VFS
	protoRelPath string
}

func (e *EmitContext) emitYmapsSprotoStmt(srcTok ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	outRoot := protoCPPOutRoot(d)
	sprotocRes := ctx.toolResult(argMapsLibsSprotoSprotoc)
	sprotocLDRef, sprotocBinary := sprotocRes.LDRef, *sprotocRes.LDPath
	scanCtx := d.scanCtx
	protoRelPath := protoSourceRelPath(ctx.fs, instance, d, srcTok.string())
	sprotoH := build(strings.TrimSuffix(protoRelPath, ".proto"), ".sproto.h")
	sprotoRef := ctx.emit.reserve()
	pbhImports := protoDirectPbHIncludes(ctx.parsers, protoRelPath, outRoot, e.dirScratch[:0])

	e.dirScratch = pbhImports

	parsed := ctx.na.dirs.alloc(2 * len(pbhImports))[:0]

	parsed = append(parsed, pbhImports...)
	parsed = appendPbHCompanions(parsed, pbhImports, sprotoPbHCompanionExt)
	ctx.na.dirs.commit(len(parsed))
	parsed = parsed[:len(parsed):len(parsed)]

	pending := YmapsSprotoPending{ref: sprotoRef, sprotoH: sprotoH, protoRelPath: protoRelPath}
	scanner := e.scanner

	pe := func() {
		e.emitYmapsSprotoHeaderSnap(scanner, pending, outRoot, sprotocLDRef, sprotocBinary, scanCtx)
	}
	onUse := e.ctx.na.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     sprotoH,
		ProducerRef:    sprotoRef,
		GeneratorRefs:  e.ctx.na.refList(sprotocLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		ClosureLeaves:  e.ctx.na.vfsList(source(protoRelPath)),
		OnUse:          onUse,
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
		internStr(p.protoRelPath).any(),
	))

	env := envVarsVCS
	sprotoCV := scanner.walkClosure(p.sprotoH, scanCtx, scanDomainAux)

	closure := collectBucketVFS(na, sprotoCV.bucketList(), func(v VFS) bool {
		return v.isSource() || !extIsProtoGeneratedHeader(v.relString())
	})

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: srcRootDirVFS,
			Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(sprotocBinary), closure),
		Outputs:        na.vfsList(p.sprotoH),
		KV:             &ymapsSprotoKV,
		ForeignDepRefs: na.refList(sprotocLDRef),
	}

	e.emitReservedNode(node, p.ref)
}
