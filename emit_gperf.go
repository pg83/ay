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

func emitGP(instance ModuleInstance, srcRel string, srcVFS, genVFS, gperfBin VFS, gperfLD NodeRef, srcInputs []VFS, ref NodeRef, emit *StreamingEmitter) {
	na := emit.nodeArenas()
	env := envVarsVCS
	head := na.anys.alloc(3 + len(gperfFlags))[:0]

	head = append(head, (gperfBin).any())
	head = append(head, gperfFlags...)
	head = append(head, internStr(gperfSymbolName(srcRel)).any(), (srcVFS).any())
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
	}

	emit.emitReservedNode(node, ref)
}

func (e *EmitContext) emitLibraryGperfSource(src ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()
	gperfLDRef, gperfBinVFS := ctx.tool(argContribToolsGperf)
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	genVFS := build(instance.Path.relString(), "/", gperfGeneratedRel(srcRel))
	gpRef := ctx.emit.reserve()

	var psc []ANY

	if p := d.perSrcCFlagsFor(src); p != nil {
		psc = *p
	}

	info := e.codegen.register(GeneratedFileInfo{
		OutputPath:     genVFS,
		ProducerRef:    gpRef,
		GeneratorRefs:  e.ctx.na.refList(gperfLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: e.ctx.na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(srcVFS.rel().any())})},
		Compile:        e.ctx.na.compileSpec(CompileSpec{FlatOutput: d.flatSrc(src), CFlags: psc}),
	})

	meta := d.srcMetaOf(src)

	meta.Generated = true
	meta.Source = genVFS.any()
	e.enqueueSrc(meta)

	scanner := e.scanner
	scanCfg := snapshotScanCfg(ctx.na, d.cc.ScanCfg)

	pe := func() {
		srcInputs := walkClosure(scanner, srcVFS, scanCfg).collect(ctx.na, func(v VFS) bool { return v.isSource() })

		emitGP(instance, srcRel, srcVFS, genVFS, gperfBinVFS, gperfLDRef, srcInputs, gpRef, ctx.emit)
	}

	info.OnUse = &pe

}
