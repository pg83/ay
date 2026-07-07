package main

import "strings"

var (
	flexOutputInclude = IncludeDirective{kind: includeQuoted, target: includeTarget(internStr("util/system/compiler.h"))}
	flexKV            = KV{P: pkLX, PC: pcYellow}
)

const flexDefaultGenExt = ".cpp"

func flexGeneratedVFS(instance ModuleInstance, srcRel string) VFS {
	if strings.Contains(srcRel, "/") {
		return build(instance.Path.relString(), "/_/", srcRel, flexDefaultGenExt)
	}

	return build(instance.Path.relString(), "/", srcRel, flexDefaultGenExt)
}

func (e *EmitContext) emitLibraryFlexSource(src ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()
	flexRef, flexBin := ctx.tool(argContribToolsFlexOld)
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	outVFS := flexGeneratedVFS(instance, srcRel)
	parsed := make([]IncludeDirective, 0, 2)

	parsed = append(parsed, flexOutputInclude)
	parsed = append(parsed, e.scanner.parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)

	var psc []ARG

	if p := d.perSrcCFlagsFor(src); p != nil {
		psc = *p
	}

	cflags := psc

	if extIsFlexL(srcRel) {
		parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(srcVFS.relString()))})
		cflags = concat(psc, []ARG{argWnoUnusedVariable})
	}

	lxRef := ctx.emit.reserve()

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     outVFS,
		ProducerRef:    lxRef,
		GeneratorRefs:  []NodeRef{flexRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: parsed},
		Compile:        &CompileSpec{FlatOutput: d.flatSrc(src), CFlags: cflags},
	})

	meta := d.srcMetaOf(src)

	meta.Generated = true
	meta.Source = outVFS.any()
	e.enqueueSrc(meta)

	e.deferPass2(func() {
		lxClosure := walkClosure(e.scanner, outVFS, d.cc.ScanCfg).collect(func(v VFS) bool { return v.isSource() })

		emitFlexLX(instance, flexRef, flexBin, srcVFS, outVFS, lxClosure, lxRef, ctx.emit)
	})
}

func emitFlexLX(instance ModuleInstance, flexRef NodeRef, flexBin VFS, srcVFS, outVFS VFS, closure []VFS, id NodeRef, emit *StreamingEmitter) {
	na := emit.nodeArenas()

	cmdArgs := na.chunkList(na.anyList(
		flexBin.any(),
		internV(argDashO.string(), outVFS.string()).any(),
		srcVFS.any(),
	))

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	emit.emitReservedNode(Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: cmdArgs, Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(flexBin, srcVFS), closure),
		Outputs:        na.vfsList(outVFS),
		KV:             &flexKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: []NodeRef{flexRef},
	}, id)
}
