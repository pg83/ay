package main

import (
	"path/filepath"
	"strings"
)

var gztprotoKV = KV{P: pkGZ, PC: pcYellow}

func (e *EmitContext) emitLibraryGztProtoSource(srcRel string, protoInclude []VFS, moduleTag STR) (NodeRef, string) {
	ctx, instance, d := e.ctx, e.instance, e.d
	gztSource := e.resolveModuleSourceVFS(internStr(srcRel), d.srcDirs)
	moddir := instance.Path.rel()
	base := strings.TrimSuffix(filepath.Base(gztSource.rel()), filepath.Ext(gztSource.rel()))
	genProtoName := base + ".proto"
	genProto := build(moddir, "/", genProtoName)
	converterRef, converterBin := ctx.tool(argDictGazetteerConverter)
	imports := walkClosureTail(e.scanner, gztSource, protoWalkInputs(ctx.parsers, protoInclude, moddir))
	inducedProtos := gztConverterInducedProtos(ctx)
	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	inputs := make([]VFS, 0, 1+len(inducedProtos)+1)

	inputs = append(inputs, converterBin)
	inputs = append(inputs, inducedProtos...)
	inputs = append(inputs, gztSource)

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{
			CmdArgs: na.chunkList(gztCmdArgs(converterBin, protoInclude, gztSource, genProto)),
			Env:     env,
		}),
		Env:            env,
		Inputs:         na.inputList(inputs, imports),
		Outputs:        []VFS{genProto},
		KV:             &gztprotoKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(converterRef),
	}

	gzRef := ctx.emit.emit(node)
	sourceInputs := make([]VFS, 0, 1+len(imports))

	sourceInputs = append(sourceInputs, gztSource)

	for _, v := range imports {
		if v.isSource() && extIsGztproto(v.rel()) {
			sourceInputs = append(sourceInputs, v)
		}
	}

	generatedParse := gztGeneratedProtoParse(ctx, gztSource, inducedProtos)

	ctx.parsers.injectSourceParse(source(moddir, "/", genProtoName), generatedParse)

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     genProto,
		ProducerRef:    gzRef,
		SourceInputs:   sourceInputs,
		ClosureLeaves:  sourceInputs,
		ParsedIncludes: generatedParse.bucket(parsedIncludesLocal),
	})

	return gzRef, genProtoName
}

func (e *EmitContext) emitLibraryGztProtoCompile(src STR) *SourceEmit {
	_, _, d := e.ctx, e.instance, e.d
	srcRel := src.string()
	_, genProtoSrc := e.emitLibraryGztProtoSource(srcRel, d.cc.ProtoInclude, d.cc.ModuleTag)

	e.emitProtoProducer(genProtoSrc)

	return e.emitLibraryProtoSource(internStr(genProtoSrc))
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

func gztGeneratedProtoParse(ctx *GenCtx, gztSource VFS, inducedProtos []VFS) ParsedIncludeSet {
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

	hcpp := make([]IncludeDirective, 0, len(local))

	for _, dir := range local {
		t := dir.target.string()

		if extIsProto(t) {
			hcpp = append(hcpp, IncludeDirective{kind: dir.kind, target: internV(strings.TrimSuffix(t, ".proto"), ".pb.h")})
		}
	}

	var set ParsedIncludeSet
	set[parsedIncludesLocal] = local
	set[parsedIncludesHeader] = hcpp
	set[parsedIncludesCpp] = hcpp

	return set
}
