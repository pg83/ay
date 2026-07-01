package main

import "strings"

var r5KV = KV{P: pkR5, PC: pcYellow}

func ragel5OutPaths(instance ModuleInstance, srcRel string) (tmpVFS, cppVFS VFS) {
	tmpVFS = build(instance.Path.rel(), "/", srcRel, ".tmp")
	cppVFS = build(instance.Path.rel(), "/", strings.TrimSuffix(srcRel, ".rl"), ".rl5.cpp")

	return tmpVFS, cppVFS
}

func emitR5(
	instance ModuleInstance,
	srcRel string,
	ragel5LD NodeRef,
	rlgenCdLD NodeRef,
	ragel5BinPath VFS,
	rlgenCdBinPath VFS,
	closure []VFS,
	ref NodeRef,
	emit *StreamingEmitter,
) {
	na := emit.nodeArenas()
	srcVFS := source(instance.Path.rel(), "/", srcRel)
	tmpVFS, cppVFS := ragel5OutPaths(instance, srcRel)
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmd0 := Cmd{
		CmdArgs: na.chunkList(na.strList((ragel5BinPath).str(),
			argDashO.str(),
			(tmpVFS).str(),
			(srcVFS).str())),
		Env: env,
	}

	rlgenMode := argT0

	if instance.Platform.RagelOptimized {
		rlgenMode = argG2
	}

	cmd1 := Cmd{
		CmdArgs: na.chunkList(na.strList((rlgenCdBinPath).str(),
			rlgenMode.str(),
			argDashO.str(),
			(cppVFS).str(),
			(tmpVFS).str())),
		Env: env,
	}

	inputs := append([]VFS{ragel5BinPath, rlgenCdBinPath, srcVFS}, closure...)

	node := &Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(cmd0, cmd1),
		Env:            env,
		Inputs:         na.inputList(inputs),
		Outputs:        na.vfsList(tmpVFS, cppVFS),
		KV:             &r5KV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(ragel5LD, rlgenCdLD),
	}

	emit.emitReserved(node, ref)
}

func (e *EmitContext) emitLibraryRagel5Source(src STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()

	var psc []ARG

	if p := d.perSrcCFlagsFor(src); p != nil {
		psc = *p
	}

	ragel5LDRef, ragel5BinVFS := ctx.tool(argContribToolsRagel5Ragel)
	rlgenCdLDRef, rlgenCdBinVFS := ctx.tool(argContribToolsRagel5RlgenCd)
	r5Ref := ctx.emit.reserve()
	r5TmpOut, r5CppOut := ragel5OutPaths(instance, srcRel)
	rlSourceVFS := source(instance.Path.rel(), "/", srcRel)
	reg := e.codegen

	reg.register(&GeneratedFileInfo{
		OutputPath:     r5TmpOut,
		ProducerRef:    r5Ref,
		GeneratorRefs:  []NodeRef{ragel5LDRef, rlgenCdLDRef},
		ParsedIncludes: nil,
	})

	r5Parsed := e.scanner.parsers.sourceParsedBuckets(rlSourceVFS, nil).bucket(parsedIncludesCpp)

	reg.register(&GeneratedFileInfo{
		OutputPath:     r5CppOut,
		ProducerRef:    r5Ref,
		GeneratorRefs:  []NodeRef{ragel5LDRef, rlgenCdLDRef},
		ParsedIncludes: append([]IncludeDirective{{kind: includeQuoted, target: internStr(r5TmpOut.rel())}}, r5Parsed...),
		Compile: &CompileSpec{
			FlatOutput: d.flatSrc(src),
			CFlags:     concat(psc, []ARG{argWnoImplicitFallthrough}),
		},
	})

	meta := d.srcMetaOf(src)

	meta.Generated = true
	e.enqueueSrc(r5CppOut.str(), meta)

	e.deferPass2(func() {
		window := walkClosure(e.scanner, r5CppOut, d.cc.ScanCfg)
		closure := keepOnlySourceVFS(filterEnSerializedSiblings(window))

		emitR5(instance, srcRel, ragel5LDRef, rlgenCdLDRef, ragel5BinVFS, rlgenCdBinVFS, closure, r5Ref, ctx.emit)
	})
}
