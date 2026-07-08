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
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	head := make([]ANY, 0, 3+len(gperfFlags))

	head = append(head, (gperfBin).any())
	head = append(head, gperfFlags...)
	head = append(head, internStr(gperfSymbolName(srcRel)).any(), (srcVFS).any())

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(head), Env: env, Stdout: genVFS}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(gperfBin), srcInputs),
		Outputs:        na.vfsList(genVFS),
		KV:             &gperfKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(gperfLD),
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

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     genVFS,
		ProducerRef:    gpRef,
		GeneratorRefs:  []NodeRef{gperfLDRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: []IncludeDirective{{kind: includeQuoted, target: includeTarget(srcVFS.rel())}}},
		Compile:        &CompileSpec{FlatOutput: d.flatSrc(src), CFlags: psc},
	})

	meta := d.srcMetaOf(src)

	meta.Generated = true
	meta.Source = genVFS.any()
	e.enqueueSrc(meta)

	e.deferPass2(func() {
		srcInputs := walkClosure(e.scanner, srcVFS, d.cc.ScanCfg).collect(func(v VFS) bool { return v.isSource() })

		emitGP(instance, srcRel, srcVFS, genVFS, gperfBinVFS, gperfLDRef, srcInputs, gpRef, ctx.emit)
	})
}
