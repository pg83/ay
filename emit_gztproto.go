package main

import (
	"path/filepath"
	"strings"
)

func emitLibraryGztProtoSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, protoInclude []VFS, moduleTag STR) (NodeRef, string) {
	gztSource := resolveModuleSourceVFS(ctx, instance, d, srcRel, d.srcDirs)
	moddir := instance.Path.rel()

	base := strings.TrimSuffix(filepath.Base(gztSource.rel()), filepath.Ext(gztSource.rel()))
	genProtoName := base + ".proto"
	genProto := build(moddir + "/" + genProtoName)

	converterRef, converterBin := ctx.tool(argDictGazetteerConverter)

	imports := walkClosureTail(ctx.scannerFor(instance), gztSource, protoWalkInputs(ctx.parsers, protoInclude, moddir).ScanCfg)
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
		KV:             KV{P: pkGZ, PC: pcYellow},
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(converterRef),
	}
	gzRef := ctx.emit.emit(node)

	sourceInputs := make([]VFS, 0, 1+len(imports))
	sourceInputs = append(sourceInputs, gztSource)

	for _, v := range imports {
		if v.isSource() && strings.HasSuffix(v.rel(), ".gztproto") {
			sourceInputs = append(sourceInputs, v)
		}
	}

	reg := codegenRegForInstance(ctx, instance)
	reg.register(&GeneratedFileInfo{
		ProducerKvP:  pkGZ,
		OutputPath:   genProto,
		ProducerRef:  gzRef,
		SourceInputs: sourceInputs,
	})

	generatedParse := gztGeneratedProtoParse(ctx, gztSource, inducedProtos)
	ctx.parsers.injectSourceParse(source(moddir+"/"+genProtoName), generatedParse)
	ctx.parsers.registerBuildParsedIncludes(genProto, generatedParse.bucket(parsedIncludesLocal))

	for _, s := range sourceInputs {
		reg.addClosureLeaf(genProto, s)
	}

	return gzRef, genProtoName
}

func emitLibraryGztProtoCompile(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	_, genProtoSrc := emitLibraryGztProtoSource(ctx, instance, d, srcRel, in.ProtoInclude, in.ModuleTag)

	emitProtoProducer(ctx, instance, d, genProtoSrc, in)

	return emitLibraryProtoSource(ctx, instance, d, genProtoSrc, in)
}

func gztCmdArgs(converterBin VFS, protoInclude []VFS, gztSource, genProto VFS) []STR {
	args := make([]STR, 0, 6+len(protoInclude))
	args = append(args, converterBin.str())
	args = append(args, internStr("-I"+pbRuntimeBaseVFS.string()))

	seen := make(map[string]struct{}, 2+len(protoInclude))
	emitI := func(path string) {
		if _, ok := seen[path]; ok {
			return
		}

		seen[path] = struct{}{}
		args = append(args, internStr("-I"+path))
	}

	emitI(strB.string())
	emitI(strS.string())

	for _, p := range protoInclude {
		emitI(p.string())
	}

	args = append(args, internStr("-I"+strS.string()))
	args = append(args, gztSource.str(), genProto.str())

	return args
}

func gztConverterInducedProtos(ctx *GenCtx) []VFS {
	res := ctx.toolResult(argDictGazetteerConverter)

	var out []VFS
	seen := make(map[VFS]struct{}, 2)

	for _, dir := range res.InducedDeps.bucket(parsedIncludesCpp) {
		t := dir.target.string()

		if !strings.HasSuffix(t, ".proto") {
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

		if strings.HasSuffix(t, ".gztproto") {
			t = strings.TrimSuffix(t, ".gztproto") + ".proto"
		}

		local = append(local, IncludeDirective{kind: dir.kind, target: internStr(t)})
	}

	hcpp := make([]IncludeDirective, 0, len(local))

	for _, dir := range local {
		t := dir.target.string()

		if strings.HasSuffix(t, ".proto") {
			hcpp = append(hcpp, IncludeDirective{kind: dir.kind, target: internStr(strings.TrimSuffix(t, ".proto") + ".pb.h")})
		}
	}

	var set ParsedIncludeSet
	set[parsedIncludesLocal] = local
	set[parsedIncludesHeader] = hcpp
	set[parsedIncludesCpp] = hcpp

	return set
}
