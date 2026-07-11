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

func emitGP(instance ModuleInstance, srcRel string, srcVFS, genVFS, gperfBin VFS, gperfLD NodeRef, srcInputs []VFS, depRefs []NodeRef, ref NodeRef, emit *StreamingEmitter) {
	na := emit.nodeArenas()
	env := envVarsVCS
	head := na.anys.alloc(3 + len(gperfFlags))[:0]

	head = append(head, gperfBin.any())
	head = append(head, gperfFlags...)
	head = append(head, internStr(gperfSymbolName(srcRel)).any(), srcVFS.any())
	na.anys.commit(len(head))

	head = head[:len(head):len(head)]

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(head), Env: env, Stdout: genVFS}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(gperfBin), srcInputs),
		Outputs:        na.vfsList(genVFS),
		KV:             &gperfKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: na.refList(gperfLD),
		DepRefs:        depRefs,
	}

	emit.emitReservedNode(node, ref)
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

	scanner := e.scanner
	scanCtx := d.scanCtx

	pe := func() {
		cv := scanner.walkClosure(srcVFS, scanCtx, scanDomainCC)
		sourceClosure := cv.collect(ctx.na, func(v VFS) bool { return v.isSource() })
		srcInputs := ctx.na.vfsList(srcVFS)

		if srcVFS.isSource() {
			srcInputs = nil
		}

		srcInputs = ctx.na.dedupClosure(srcInputs, [][]VFS{sourceClosure})
		depRefs := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, []VFS{srcVFS})

		emitGP(instance, srcRel, srcVFS, genVFS, gperfBinVFS, gperfLDRef, srcInputs, depRefs, gpRef, ctx.emit)
	}
	pending := e.ctx.na.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:     genVFS,
		ProducerRef:    gpRef,
		GeneratorRefs:  e.ctx.na.refList(gperfLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(srcVFS.rel().any())})},
		OnUse:          pending,
	})
}
