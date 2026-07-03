package main

import (
	"path/filepath"
	"strings"
)

var gztprotoKV = KV{P: pkGZ, PC: pcYellow}

func (e *EmitContext) emitLibraryGztProtoSource(srcRel string, protoInclude []VFS) (NodeRef, string) {
	ctx, instance, d := e.ctx, e.instance, e.d
	gztSource := e.resolveModuleSourceVFS(internStr(srcRel), d.srcDirs)
	moddir := instance.Path.rel()
	base := strings.TrimSuffix(filepath.Base(gztSource.rel()), filepath.Ext(gztSource.rel()))
	genProtoName := base + ".proto"
	genProto := build(moddir, "/", genProtoName)
	converterRef, converterBin := ctx.tool(argDictGazetteerConverter)
	imports := walkClosure(e.scanner, gztSource, protoWalkInputs(ctx.parsers, protoInclude, moddir))
	inducedProtos := gztConverterInducedProtos(ctx)
	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
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
		if v.isSource() && extIsGztproto(v.rel()) {
			sourceInputs = append(sourceInputs, v)
		}
	})

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     genProto,
		ProducerRef:    gzRef,
		SourceInputs:   sourceInputs,
		ClosureLeaves:  sourceInputs,
		ParsedIncludes: gztGeneratedProtoParse(ctx, gztSource, inducedProtos),
	})

	return gzRef, genProtoName
}

func (e *EmitContext) gztGenProtoName(srcRel string) string {
	gztSource := e.resolveModuleSourceVFS(internStr(srcRel), e.d.srcDirs)

	return strings.TrimSuffix(filepath.Base(gztSource.rel()), filepath.Ext(gztSource.rel())) + ".proto"
}

func (e *EmitContext) emitLibraryGztProtoCompile(src STR) {
	_, _, d := e.ctx, e.instance, e.d

	if d.unit.Tag == unitTagPy3Proto {
		return
	}

	e.emitLibraryGztProtoSource(src.string(), d.cc.ProtoInclude)
}

func gztCmdArgs(converterBin VFS, protoInclude []VFS, gztSource, genProto VFS) []STR {
	args := make([]STR, 0, 6+len(protoInclude))

	args = append(args, converterBin.str())
	args = append(args, internV("-I", pbRuntimeBaseVFS.string()))

	seen := make(map[string]struct{}, 2+len(protoInclude))

	emitI := func(path string) {
		if _, ok := seen[path]; ok {
			return
		}

		seen[path] = struct{}{}
		args = append(args, internV("-I", path))
	}

	emitI(strB.string())
	emitI(strS.string())

	for _, p := range protoInclude {
		emitI(p.string())
	}

	args = append(args, internV("-I", strS.string()))
	args = append(args, gztSource.str(), genProto.str())

	return args
}

func gztConverterInducedProtos(ctx *GenCtx) []VFS {
	res := ctx.toolResult(argDictGazetteerConverter)

	var out []VFS

	seen := make(map[VFS]struct{}, 2)

	for _, dir := range res.InducedDeps.bucket(parsedIncludesCpp) {
		t := dir.target.string()

		if !extIsProto(t) {
			continue
		}

		rel := strings.TrimPrefix(strings.TrimPrefix(t, "$(S)/"), "$(B)/")
		v := source(rel)

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
		local = append(local, IncludeDirective{kind: includeQuoted, target: internStr(v.rel())})
	}

	for _, dir := range gztLocal {
		t := dir.target.string()

		if extIsGztproto(t) {
			t = strings.TrimSuffix(t, ".gztproto") + ".proto"
		}

		local = append(local, IncludeDirective{kind: dir.kind, target: internStr(t)})
	}

	return local
}
