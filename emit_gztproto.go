package main

import (
	"path/filepath"
	"strings"
)

var gztprotoKV = KV{P: pkGZ, PC: pcYellow}

func (e *EmitContext) emitLibraryGztProtoSource(srcRel string, protoInclude []VFS) (NodeRef, string) {
	ctx, instance, d := e.ctx, e.instance, e.d
	gztSource := e.resolveModuleSourceVFS(internStr(srcRel).any(), d.srcDirs)
	moddir := instance.Path.relString()
	base := strings.TrimSuffix(filepath.Base(gztSource.relString()), filepath.Ext(gztSource.relString()))
	genProtoName := base + ".proto"
	genProto := build(moddir, "/", genProtoName)
	converterRef, converterBin := ctx.tool(argDictGazetteerConverter)
	imports := e.scanner.walkClosure(gztSource, d.scanCtx, scanDomainProto)
	inducedProtos := gztConverterInducedProtos(ctx)
	na := ctx.emit.nodeArenas()
	env := envVarsVCS

	gzRef := ctx.emit.reserve()
	protoIncludeSnap := na.vfsList(protoInclude...)

	pe := func() {
		nInputs := 2 + len(imports.buckets)

		if len(inducedProtos) > 0 {
			nInputs++
		}

		inputChunks := na.inputs.alloc(nInputs)[:0]

		inputChunks = append(inputChunks, na.vfsList(converterBin))

		if len(inducedProtos) > 0 {
			inputChunks = append(inputChunks, inducedProtos)
		}

		inputChunks = append(inputChunks, na.vfsList(gztSource))
		inputChunks = append(inputChunks, imports.buckets...)
		na.inputs.commit(len(inputChunks))

		node := Node{
			Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{
				CmdArgs: na.chunkList(gztCmdArgs(na, converterBin, protoIncludeSnap, gztSource, genProto)),
				Env:     env,
			}),
			Env:            env,
			Inputs:         InputChunks(inputChunks[:len(inputChunks):len(inputChunks)]),
			Outputs:        na.vfsList(genProto),
			KV:             &gztprotoKV,
			ForeignDepRefs: na.refList(converterRef),
		}

		e.emitReservedNode(node, gzRef)
	}
	pending := e.ctx.na.pendingEmit(pe)

	sourceInputs := na.vfs.alloc(1 + imports.len())[:0]

	sourceInputs = append(sourceInputs, gztSource)

	eachBucketVFS(imports.buckets, func(v VFS) {
		if v.isSource() && extIsGztproto(v.relString()) {
			sourceInputs = append(sourceInputs, v)
		}
	})

	na.vfs.commit(len(sourceInputs))

	sourceInputs = sourceInputs[:len(sourceInputs):len(sourceInputs)]

	e.register(GeneratedFileInfo{
		OutputPath:     genProto,
		ProducerRef:    gzRef,
		SourceInputs:   sourceInputs,
		ClosureLeaves:  sourceInputs,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: gztGeneratedProtoParse(ctx, e.scanner, gztSource, inducedProtos)},
		OnUse:          pending,
	})

	return gzRef, genProtoName
}

func (e *EmitContext) gztGenProtoName(srcRel string) string {
	gztSource := e.resolveModuleSourceVFS(internStr(srcRel).any(), e.d.srcDirs)

	return strings.TrimSuffix(filepath.Base(gztSource.relString()), filepath.Ext(gztSource.relString())) + ".proto"
}

func (e *EmitContext) emitLibraryGztProtoCompile(src ANY) {
	_, _, d := e.ctx, e.instance, e.d

	if d.unit.Tag == unitTagPy3Proto {
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
