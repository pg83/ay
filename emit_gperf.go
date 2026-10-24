package main

import (
	"path/filepath"
	"strings"
)

var (
	gperfFlags = []ANY{argGpCtTLANSIC.any(), argGpDk.any(), argDashC.any()}
	gperfKV    = KV{P: pkGP, PC: pcYellow}
)

func gperfGeneratedRel(srcRel string) string {
	return filepath.Base(srcRel) + ".cpp"
}

func gperfSymbolName(srcRel string) string {
	base := filepath.Base(srcRel)

	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}

	return "-Nin_" + base + "_set"
}

func (e *EmitContext) emitGP(srcRel string, srcVFS, genVFS, gperfBin VFS, gperfLD NodeRef, srcInputs []VFS, ref NodeRef) {
	na := e.ctx.na
	env := envVarsVCS
	head := na.anys.alloc(3 + len(gperfFlags))[:0]

	head = append(head, gperfBin.any())
	head = append(head, gperfFlags...)
	head = append(head, internStr(gperfSymbolName(srcRel)).any(), srcVFS.any())
	na.anys.commit(len(head))

	head = head[:len(head):len(head)]

	node := Node{
		Platform:        e.instance.Platform,
		Cmds:            na.cmdList(Cmd{CmdArgs: na.chunkList(head), Env: env, Stdout: genVFS}),
		Env:             env,
		Inputs:          na.inputList(na.vfsList(gperfBin), srcInputs),
		BuildFromInputs: na.inputList(srcInputs),
		Outputs:         na.vfsList(genVFS),
		KV:              &gperfKV,
		ForeignDepRefs:  na.refList(gperfLD),
	}

	e.emitReservedNode(node, ref)
}

func (e *EmitContext) emitLibraryGperfSource(meta SrcMeta) {
	ctx, instance, d := e.ctx, e.instance, e.d
	src := meta.Source
	srcRel := e.moduleSourceRel(src)
	gperfLDRef, gperfBinVFS := ctx.tool(argContribToolsGperf)
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	genVFS := build(instance.Path.relString(), "/", gperfGeneratedRel(srcRel))
	gpRef := ctx.emit.reserve()

	meta.Source = genVFS.any()
	e.enqueueSrc(meta)

	pe := func() {
		e.emitGP(srcRel, srcVFS, genVFS, gperfBinVFS, gperfLDRef, ctx.na.vfsList(srcVFS), gpRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     genVFS,
		ProducerRef:    gpRef,
		GeneratorRefs:  e.ctx.na.refList(gperfLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.scanner.parsedBucketForInput(srcVFS, parsedIncludesCpp, nil)},
		OnUse:          pending,
	})
}
