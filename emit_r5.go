package main

import (
	"path/filepath"
	"strings"
)

var r5KV = KV{P: pkR5, PC: pcYellow}

func emitR5Reserved(
	instance ModuleInstance,
	srcRel string,
	ragel5LD NodeRef,
	rlgenCdLD NodeRef,
	ragel5BinPath VFS,
	rlgenCdBinPath VFS,
	id NodeRef,
	emit *StreamingEmitter,
) {
	na := emit.nodeArenas()
	srcVFS := source(instance.Path.relString(), "/", srcRel)
	tmpVFS := build(instance.Path.relString(), "/", srcRel, ".tmp")
	cppVFS := build(instance.Path.relString(), "/", strings.TrimSuffix(srcRel, filepath.Ext(srcRel)), ".rl5.cpp")
	env := envVarsVCS

	cmd0 := Cmd{
		CmdArgs: na.chunkList(na.anyList((ragel5BinPath).any(),
			argDashO.any(),
			(tmpVFS).any(),
			(srcVFS).any())),
		Env: env,
	}

	rlgenMode := argT0

	if instance.Platform.RagelOptimized {
		rlgenMode = argG2
	}

	cmd1 := Cmd{
		CmdArgs: na.chunkList(na.anyList((rlgenCdBinPath).any(),
			rlgenMode.any(),
			argDashO.any(),
			(cppVFS).any(),
			(tmpVFS).any())),
		Env: env,
	}

	inputs := na.vfsList(ragel5BinPath, rlgenCdBinPath, srcVFS)

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(cmd0, cmd1),
		Env:            env,
		Inputs:         na.inputList(inputs),
		Outputs:        na.vfsList(tmpVFS, cppVFS),
		KV:             &r5KV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: na.refList(ragel5LD, rlgenCdLD),
	}

	emit.emitReservedNode(node, id)
}

func (e *EmitContext) emitLibraryRagel5Source(src ANY) {
	ctx, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()

	var psc []ANY

	if p := d.perSrcCFlagsFor(src); p != nil {
		psc = *p
	}

	ragel5LDRef, ragel5BinVFS := ctx.tool(argContribToolsRagel5Ragel)
	rlgenCdLDRef, rlgenCdBinVFS := ctx.tool(argContribToolsRagel5RlgenCd)
	r5Ref := ctx.emit.reserve()
	r5TmpOut := build(instance.Path.relString(), "/", srcRel, ".tmp")
	r5CppOut := build(instance.Path.relString(), "/", strings.TrimSuffix(srcRel, filepath.Ext(srcRel)), ".rl5.cpp")
	rlSourceVFS := source(instance.Path.relString(), "/", srcRel)
	reg := e.codegen

	tmpInfo := reg.register(GeneratedFileInfo{
		OutputPath:    r5TmpOut,
		ProducerRef:   r5Ref,
		GeneratorRefs: e.ctx.na.refList(ragel5LDRef, rlgenCdLDRef),
	})

	r5Parsed := e.scanner.parsers.sourceParsedBuckets(rlSourceVFS, nil).bucket(parsedIncludesCpp)

	cppInfo := reg.register(GeneratedFileInfo{
		OutputPath:     r5CppOut,
		ProducerRef:    r5Ref,
		GeneratorRefs:  e.ctx.na.refList(ragel5LDRef, rlgenCdLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: r5CppParsed(e.ctx.na, r5TmpOut, r5Parsed)},
		Compile: e.ctx.na.compileSpec(CompileSpec{
			FlatOutput: d.flatSrc(src),
			CFlags:     cflagsWnoImplicitFallthrough(e.ctx.na, psc),
		}),
	})

	pe := &PendingEmit{fn: func() {
		emitR5Reserved(instance, srcRel, ragel5LDRef, rlgenCdLDRef, ragel5BinVFS, rlgenCdBinVFS, r5Ref, ctx.emit)
	}}

	tmpInfo.pending = pe
	cppInfo.pending = pe

	e.noteOwn(pe)

	meta := d.srcMetaOf(src)

	meta.Generated = true
	meta.Source = r5CppOut.any()
	e.enqueueSrc(meta)
}

func r5CppParsed(na *NodeArenas, r5TmpOut VFS, r5Parsed []IncludeDirective) []IncludeDirective {
	out := na.dirs.alloc(1 + len(r5Parsed))[:0]

	out = append(out, IncludeDirective{kind: includeQuoted, target: includeTarget(r5TmpOut.rel().any())})
	out = append(out, r5Parsed...)
	na.dirs.commit(len(out))

	return out[:len(out):len(out)]
}

func cflagsWnoImplicitFallthrough(na *NodeArenas, psc []ANY) []ANY {
	out := na.anys.alloc(len(psc) + 1)
	n := copy(out, psc)

	out[n] = argWnoImplicitFallthrough.any()
	na.anys.commit(n + 1)

	return out[: n+1 : n+1]
}
