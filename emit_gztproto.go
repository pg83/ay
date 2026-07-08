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
	imports := walkClosure(e.scanner, gztSource, protoWalkInputs(ctx.parsers, protoInclude, moddir))
	inducedProtos := gztConverterInducedProtos(ctx)
	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}}
	inputs := make([]VFS, 0, 1+len(inducedProtos)+1)

	inputs = append(inputs, converterBin)
	inputs = append(inputs, inducedProtos...)
	inputs = append(inputs, gztSource)

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(gztCmdArgs(converterBin, protoInclude, gztSource, genProto)),
			Env:     env,
		}),
		Env:            env,
		Inputs:         na.inputList(inputs, imports.buckets...),
		Outputs:        []VFS{genProto},
		KV:             &gztprotoKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(converterRef),
	}

	gzRef := ctx.emit.emitNode(node)
	sourceInputs := make([]VFS, 0, 1+imports.len())

	sourceInputs = append(sourceInputs, gztSource)

	eachBucketVFS(imports.buckets, func(v VFS) {
		if v.isSource() && extIsGztproto(v.relString()) {
			sourceInputs = append(sourceInputs, v)
		}
	})

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     genProto,
		ProducerRef:    gzRef,
		SourceInputs:   sourceInputs,
		ClosureLeaves:  sourceInputs,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: gztGeneratedProtoParse(ctx, gztSource, inducedProtos)},
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

func gztCmdArgs(converterBin VFS, protoInclude []VFS, gztSource, genProto VFS) []ANY {
	args := make([]ANY, 0, 6+len(protoInclude))

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

	return args
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

func gztGeneratedProtoParse(ctx *GenCtx, gztSource VFS, inducedProtos []VFS) []IncludeDirective {
	gztLocal := ctx.parsers.sourceParsedBuckets(gztSource, nil).bucket(parsedIncludesLocal)
	local := make([]IncludeDirective, 0, len(inducedProtos)+len(gztLocal))

	for _, v := range inducedProtos {
		local = append(local, IncludeDirective{kind: includeQuoted, target: includeTarget(v.rel())})
	}

	for _, dir := range gztLocal {
		t := dir.target.string()

		if extIsGztproto(t) {
			t = strings.TrimSuffix(t, ".gztproto") + ".proto"
		}

		local = append(local, IncludeDirective{kind: dir.kind, target: includeTarget(internStr(t))})
	}

	return local
}
