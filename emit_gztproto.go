package main

import (
	"path/filepath"
	"strings"
)

// emitLibraryGztProtoSource emits the GZ stage of a `.gztproto` source: the
// converter writes `$(B)/<moddir>/<base>.proto` (include order: protobuf-src,
// _PROTO__INCLUDE chain, then root, for path canonization). The ordinary protoc
// path compiles it; this only produces and registers it, returning the GZ ref
// and the generated proto's module-relative name (fed back into protoSrcs).
//
// The converter rewrites `.gztproto` imports to `.proto` and injects an
// `import base.proto` (INDUCED_DEPS(proto …)). Since the proto is not on disk at
// configure time, its parse (imports + induced .pb.h) is injected under its
// source VFS as a context-free precomputed parse.
func emitLibraryGztProtoSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, protoInclude []VFS, moduleTag STR) (NodeRef, string) {
	gztSource := resolveModuleSourceVFS(ctx, instance, d, srcRel, d.srcDirs)
	moddir := instance.Path.rel()

	// basename + .proto in the module build dir.
	base := strings.TrimSuffix(filepath.Base(gztSource.rel()), filepath.Ext(gztSource.rel()))
	genProtoName := base + ".proto"
	genProto := build(moddir + "/" + genProtoName)

	converterRef, converterBin := ctx.tool(argDictGazetteerConverter)

	imports := walkClosureTail(ctx.scannerFor(instance), gztSource, protoWalkInputs(ctx.parsers, protoInclude, moddir).ScanCfg)
	inducedProtos := gztConverterInducedProtos(ctx)

	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// tool binary + induced base.proto + the source; the import closure rides
	// as its own chunk.
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
		Env:              env,
		Inputs:           na.inputList(inputs, imports),
		Outputs:          []VFS{genProto},
		KV:               KV{P: pkGZ, PC: pcYellow},
		TargetProperties: TargetProperties{ModuleDir: moddir, ModuleTag: moduleTag},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs:   depRefs(converterRef),
	}
	gzRef := ctx.emit.emit(node)

	// Producer-source bundle for the downstream PB node: the .gztproto sources
	// (self + transitively-imported). The .proto imports ride the PB node's own
	// transitive walk, so they are not producer sources.
	sourceInputs := make([]VFS, 0, 1+len(imports))
	sourceInputs = append(sourceInputs, gztSource)

	for _, v := range imports {
		if v.isSource() && strings.HasSuffix(v.rel(), ".gztproto") {
			sourceInputs = append(sourceInputs, v)
		}
	}

	// No GeneratorRefs: the generated .proto is consumed as a proto SOURCE, not
	// via induced-deps. Recording the converter here would mis-induce its
	// INDUCED_DEPS(h+cpp …/base.pb.h) onto the .proto output, dragging base.pb.h's
	// C++ closure into the PB node.
	reg := codegenRegForInstance(ctx, instance)
	reg.register(&GeneratedFileInfo{
		ProducerKvP:  pkGZ,
		OutputPath:   genProto,
		ProducerRef:  gzRef,
		SourceInputs: sourceInputs,
	})

	// Parse shared by two readers: injectSourceParse for the node compiling THIS
	// proto (SOURCE-rooted), registerBuildParsedIncludes for a LATER consumer
	// importing it BUILD-rooted (parsedIncludes consults only buildParsed for
	// build paths, else the consumer walks no nested imports). Context-free.
	generatedParse := gztGeneratedProtoParse(ctx, gztSource, inducedProtos)
	ctx.parsers.injectSourceParse(source(moddir+"/"+genProtoName), generatedParse)
	ctx.parsers.registerBuildParsedIncludes(genProto, generatedParse.bucket(parsedIncludesLocal))

	// The raw .gztproto leaves are a generated-from edge of the $(B) .proto, not a
	// parseable import: a PB consumer rides them as non-expanded closure leaves,
	// as a .pb.h rides its .proto.
	for _, s := range sourceInputs {
		reg.addClosureLeaf(genProto, s)
	}

	return gzRef, genProtoName
}

// emitLibraryGztProtoCompile is the regular-module (LIBRARY/PROGRAM) counterpart
// of emitLibraryProtoSource for a `.gztproto` SRCS entry. _SRC("gztproto") is
// per-source, so a plain LIBRARY's `.gztproto` must compile and archive its
// generated `.pb.cc.o` like a `.proto` does. It runs the GZ producer, then
// delegates the generated proto to the ordinary protoc-compile path (picked up
// via the protoSrcOverride lookup).
func emitLibraryGztProtoCompile(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	_, genProtoSrc := emitLibraryGztProtoSource(ctx, instance, d, srcRel, in.ProtoInclude, in.ModuleTag)

	// The gzt-generated .proto is not in d.srcs, so the proto producer pre-pass
	// never sees it; emit its PB producer here before compiling.
	emitProtoProducer(ctx, instance, d, genProtoSrc, in)

	return emitLibraryProtoSource(ctx, instance, d, genProtoSrc, in)
}

// gztCmdArgs builds the converter command:
//
//	converter -I$PROTOBUF_INCLUDE_PATH ${pre=-I:_PROTO__INCLUDE} -I$ROOT
//	          <src.gztproto> <out.proto>
//
// _PROTO__INCLUDE is the $(B)/$(S) base plus the module's proto-include set,
// deduplicated as a set; the protobuf-src and $(S) roots are emitted once on
// each side regardless (distinct command positions, not a dedup set).
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

// gztConverterInducedProtos returns the proto-level files the converter injects
// into every .proto it generates — its INDUCED_DEPS(proto …) targets. The .pb.h
// induced sibling rides the import-to-.pb.h rule, so only .proto is taken here.
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

// gztGeneratedProtoParse computes the generated .proto's parse: the injected
// proto imports plus the .gztproto's own imports rewritten .gztproto→.proto, with
// the matching induced .pb.h for each — the parse the file would yield on disk.
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
