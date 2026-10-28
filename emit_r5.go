package main

import (
	"path/filepath"
	"strings"
)

var r5KV = KV{P: pkR5, PC: pcYellow}

func (e *EmitContext) emitR5Reserved(
	srcRel string,
	srcVFS VFS,
	ragel5LD NodeRef,
	rlgenCdLD NodeRef,
	ragel5BinPath VFS,
	rlgenCdBinPath VFS,
	closure Closure,
	id NodeRef,
) {
	instance := e.instance
	na := e.ctx.na
	tmpVFS := build(instance.Path.relString(), "/", srcRel, ".tmp")
	cppVFS := build(instance.Path.relString(), "/", strings.TrimSuffix(srcRel, filepath.Ext(srcRel)), ".rl5.cpp")
	env := envVarsVCS

	cmd0 := Cmd{
		CmdArgs: na.chunkList(na.anyList(ragel5BinPath.any(),
			argDashO.any(),
			tmpVFS.any(),
			srcVFS.any())),
		Env: env,
	}

	cmd1 := Cmd{
		CmdArgs: na.chunkList(na.anyList(rlgenCdBinPath.any()), instance.Platform.RlgenFlags, na.anyList(
			argDashO.any(),
			cppVFS.any(),
			tmpVFS.any())),
		Env: env,
	}

	node := Node{
		Platform: instance.Platform,
		Cmds:     na.cmdList(cmd0, cmd1),
		Env:      env,
		Inputs:   NodeInputs{files: na.inputList(na.vfsList(ragel5BinPath, rlgenCdBinPath, srcVFS), closure.bucketList()...), roots: na.inputList(na.vfsList(srcVFS), closure.bucketList()...)},

		Outputs:        na.vfsList(tmpVFS, cppVFS),
		KV:             &r5KV,
		ForeignDepRefs: na.refList(ragel5LD, rlgenCdLD),
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitLibraryRagel5Source(meta SrcMeta) {
	ctx, instance := e.ctx, e.instance
	src := meta.Source
	srcRel := e.moduleSourceRel(src)
	ragel5LDRef, ragel5BinVFS := ctx.tool(argContribToolsRagel5Ragel)
	rlgenCdLDRef, rlgenCdBinVFS := ctx.tool(argContribToolsRagel5RlgenCd)
	r5Ref := ctx.emit.reserve()
	r5TmpOut := build(instance.Path.relString(), "/", srcRel, ".tmp")
	r5CppOut := build(instance.Path.relString(), "/", strings.TrimSuffix(srcRel, filepath.Ext(srcRel)), ".rl5.cpp")
	rlSourceVFS := e.sourceForEmission(meta)

	pe := func() {
		e.emitR5Reserved(srcRel, rlSourceVFS, ragel5LDRef, rlgenCdLDRef, ragel5BinVFS, rlgenCdBinVFS, e.scanner.walkClosure(rlSourceVFS, e.d.scanCtx, scanDomainCC), r5Ref)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileInfo{
		OutputPath:    r5TmpOut,
		ProducerRef:   r5Ref,
		GeneratorRefs: e.ctx.na.refList(ragel5LDRef, rlgenCdLDRef),
		OnUse:         pending,
	})

	e.register(GeneratedFileInfo{
		OutputPath:      r5CppOut,
		ProducerRef:     r5Ref,
		ProducerMainOut: r5TmpOut,
		GeneratorRefs:   e.ctx.na.refList(ragel5LDRef, rlgenCdLDRef),
		ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: e.scanner.parsedBucketForInput(rlSourceVFS, parsedIncludesLocal, nil)},
		OnUse:           pending,
	})

	meta.Source = r5CppOut.any()
	e.enqueueSrc(meta)
}
