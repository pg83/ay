package main

import (
	"path/filepath"
	"strings"
)

var gztprotoKV = KV{P: pkGZ, PC: pcYellow}

type GztProtoProducer struct {
	Instance     ModuleInstance
	ProtoInclude []VFS
}

func (e *EmitContext) emitLibraryGztProtoSource(srcRel string, protoInclude []VFS) (NodeRef, string) {
	gztSource := resolveSourceOrGeneratedVFS(e.ctx, e.instance, srcRel, e.d.srcDirs)
	genProto := e.ctx.gztProtoSource(e.instance, gztSource, e.d.scanCtx, protoInclude)

	e.collectScriptOutput(genProto)

	return e.codegen.mustInfo(genProto).ProducerRef, filepath.Base(genProto.relString())
}

func (ctx *GenCtx) gztProtoSource(instance ModuleInstance, gztSource VFS, scanCtx *ScanContext, protoInclude []VFS) VFS {
	genProto := gztProtoOutput(gztSource)
	scanner := ctx.scannerFor(instance)
	reg := scanner.codegen

	if info := reg.lookup(genProto); info != nil {
		if info.gztProto() == nil || info.SourcePath != gztSource || info.OwnerModule != instance.Path || info.gztProto().Instance == instance {
			throwFmt("CodegenRegistry: duplicate producer for %q (existing source=%q, new source=%q)",
				genProto.string(), info.SourcePath.string(), gztSource.string())
		}

		return genProto
	}

	converterRef, converterBin := ctx.tool(argDictGazetteerConverter)
	inducedProtos := gztConverterInducedProtos(ctx)
	na := ctx.emit.nodeArenas()
	env := envVarsVCS
	gzRef := ctx.emit.reserve()
	protoIncludeSnap := na.vfsList(protoInclude...)
	producer := &GztProtoProducer{Instance: instance, ProtoInclude: protoIncludeSnap}

	pe := func() {
		imports := scanner.walkClosure(gztSource, scanCtx, scanDomainProto)
		importBuckets := imports.bucketList()
		inputChunks := na.inputs.alloc(2 + len(importBuckets))[:0]

		inputChunks = append(inputChunks, na.vfsList(converterBin), na.vfsList(gztSource))
		inputChunks = append(inputChunks, importBuckets...)
		na.inputs.commit(len(inputChunks))

		node := Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{
				CmdArgs: na.chunkList(gztCmdArgs(na, converterBin, producer.ProtoInclude, gztSource, genProto)),
				Env:     env,
			}),
			Env:    env,
			Inputs: NodeInputs{files: InputChunks(inputChunks[:len(inputChunks):len(inputChunks)]), roots: InputChunks(inputChunks[1:len(inputChunks):len(inputChunks)])},

			Outputs:        na.vfsList(genProto),
			KV:             &gztprotoKV,
			ForeignDepRefs: na.refList(converterRef),
		}

		resolver := EmitContext{ctx: ctx, instance: instance, scanner: scanner, codegen: reg, d: &ModuleData{scanCtx: scanCtx}}

		resolver.emitReservedNode(node, gzRef)
	}

	pending := na.pending.one()

	*pending = PendingEmit{fn: pe, publication: ctx.emit.pendingPublication()}

	reg.register(GeneratedFileSpec{
		OutputPath:     genProto,
		GztProto:       producer,
		SourcePath:     gztSource,
		OwnerModule:    instance.Path,
		ProducerRef:    gzRef,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: gztGeneratedProtoParse(ctx, scanner, gztSource, inducedProtos)},
		OnUse:          pending,
	})

	return genProto
}

func gztProtoOutput(input VFS) VFS {
	return build(strings.TrimSuffix(input.relString(), filepath.Ext(input.relString())), ".proto")
}

func (e *EmitContext) gztGenProto(srcRel string) VFS {
	gztSource := resolveSourceOrGeneratedVFS(e.ctx, e.instance, srcRel, e.d.srcDirs)

	return gztProtoOutput(gztSource)
}

func (e *EmitContext) emitLibraryGztProtoCompile(src ANY) {
	_, _, d := e.ctx, e.instance, e.d

	if isPythonProtoModule(d) {
		return
	}

	e.emitLibraryGztProtoSource(src.string(), d.cc.ProtoInclude)
}

func gztCmdArgs(na *NodeArenas, converterBin VFS, protoInclude []VFS, gztSource, genProto VFS) []ANY {
	args := na.anys.alloc(8 + len(protoInclude))[:0]

	args = append(args, converterBin.any())
	args = append(args, internV("-I", pbRuntimeBaseVFS.prefix(), pbRuntimeBaseVFS.relString()).any())

	seen := make(map[string]struct{}, 2+len(protoInclude))

	emitI := func(path string) {
		if _, ok := seen[path]; ok {
			return
		}

		seen[path] = struct{}{}
		args = append(args, internV("-I", path).any())
	}

	emitI(strB.string())
	emitI(strS.string())

	for _, p := range protoInclude {
		emitI(p.string())
	}

	args = append(args, internV("-I", strS.string()).any())
	args = append(args, gztSource.any(), genProto.any())
	na.anys.commit(len(args))

	return args[:len(args):len(args)]
}

func gztConverterInducedProtos(ctx *GenCtx) []VFS {
	res := ctx.toolResult(argDictGazetteerConverter)

	var out []VFS

	seen := make(map[VFS]struct{}, 2)

	for _, dir := range res.InducedDeps.bucket(parsedIncludesCpp) {
		var v VFS

		if tv := dir.target.vfs(); tv != 0 {
			if !extIsProto(tv.relString()) {
				continue
			}

			v = tv.rel().source()
		} else {
			t := dir.target.string()

			if !extIsProto(t) {
				continue
			}

			v = source(strings.TrimPrefix(strings.TrimPrefix(t, "$(S)/"), "$(B)/"))
		}

		if _, ok := seen[v]; ok {
			continue
		}

		seen[v] = struct{}{}
		out = append(out, v)
	}

	return out
}

func gztGeneratedProtoParse(ctx *GenCtx, scanner *IncludeScanner, gztSource VFS, inducedProtos []VFS) []IncludeDirective {
	gztLocal := scanner.parsedBucketForInput(gztSource, parsedIncludesLocal, nil)
	local := ctx.na.dirs.alloc(len(inducedProtos) + len(gztLocal))[:0]

	for _, v := range inducedProtos {
		local = append(local, IncludeDirective{kind: includeQuoted, target: includeTarget(v.rel().any())})
	}

	for _, dir := range gztLocal {
		t := dir.target.string()

		if extIsGztproto(t) {
			t = strings.TrimSuffix(t, ".gztproto") + ".proto"
		}

		local = append(local, IncludeDirective{kind: dir.kind, target: includeTarget(internStr(t).any())})
	}

	ctx.na.dirs.commit(len(local))

	return local[:len(local):len(local)]
}
