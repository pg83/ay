package main

import (
	"path/filepath"
	"strings"
)

var (
	bisonCppSkeletonDirectives = quotedDirectives(bisonCppSkeletonInputs)
	bisonYKV                   = KV{P: pkYC, PC: pcLightGreen}
)

var bisonCppSkeletonInputs = []VFS{
	intern("$(S)/contrib/tools/bison/data/m4sugar/foreach.m4"),
	intern("$(S)/contrib/tools/bison/data/m4sugar/m4sugar.m4"),
	intern("$(S)/contrib/tools/bison/data/skeletons/bison.m4"),
	intern("$(S)/contrib/tools/bison/data/skeletons/c++-skel.m4"),
	intern("$(S)/contrib/tools/bison/data/skeletons/c++.m4"),
	intern("$(S)/contrib/tools/bison/data/skeletons/c-like.m4"),
	intern("$(S)/contrib/tools/bison/data/skeletons/c-skel.m4"),
	intern("$(S)/contrib/tools/bison/data/skeletons/c.m4"),
	intern("$(S)/contrib/tools/bison/data/skeletons/glr.cc"),
	intern("$(S)/contrib/tools/bison/data/skeletons/lalr1.cc"),
	intern("$(S)/contrib/tools/bison/data/skeletons/location.cc"),
	intern("$(S)/contrib/tools/bison/data/skeletons/stack.hh"),
	intern("$(S)/contrib/tools/bison/data/skeletons/variant.hh"),
	intern("$(S)/contrib/tools/bison/data/skeletons/yacc.c"),
}

func bisonCppHeaderParsed(srcVFS VFS) []IncludeDirective {
	parsed := make([]IncludeDirective, 0, 1+len(bisonCppSkeletonDirectives))

	parsed = append(parsed,
		IncludeDirective{kind: includeQuoted, target: internStr(bisonPreprocessPyVFS.rel())},
	)

	parsed = append(parsed, bisonCppSkeletonDirectives...)

	return parsed
}

func (e *EmitContext) bisonGeneratedCPPParsed(srcVFS, headerVFS VFS) []IncludeDirective {
	parsed := []IncludeDirective{
		{kind: includeQuoted, target: internStr(headerVFS.rel())},
		{kind: includeQuoted, target: internStr(srcVFS.rel())},
	}

	parsed = append(parsed, e.scanner.parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)

	return parsed
}

func bisonGeneratedRel(srcRel, genExt string) string {
	if strings.Contains(srcRel, "/") {
		return "_/" + srcRel + genExt
	}

	return srcRel + genExt
}

func (e *EmitContext) emitBisonProducer(src STR) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	srcRel := src.string()
	genExt := d.cc.BisonGenExt
	bisonRef, bisonBin := bisonTool(ctx, instance)
	m4Ref, m4Bin := m4Tool(ctx, instance)
	preprocessHeader := genExt != ".c"
	baseNoExt := strings.TrimSuffix(srcRel, filepath.Ext(srcRel))
	headerRel := baseNoExt + ".h"
	generatedRel := bisonGeneratedRel(srcRel, genExt)
	headerVFS := build(instance.Path.rel(), "/", headerRel)
	generatedVFS := build(instance.Path.rel(), "/", generatedRel)
	srcVFS := source(instance.Path.rel(), "/", srcRel)
	headerParsed := []IncludeDirective{{kind: includeQuoted, target: internStr(srcVFS.rel())}}

	if preprocessHeader {
		headerParsed = bisonCppHeaderParsed(srcVFS)
	} else {
		headerParsed = append(headerParsed, e.scanner.parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)
	}

	ycRef := ctx.emit.reserve()
	reg := e.codegen

	headerInfo := &GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    ycRef,
		GeneratorRefs:  []NodeRef{bisonRef, m4Ref},
		ParsedIncludes: headerParsed,
	}

	if preprocessHeader {
		headerInfo.ClosureLeaves = []VFS{srcVFS}
	}

	reg.register(headerInfo)

	generatedParsed := []IncludeDirective{{kind: includeQuoted, target: internStr(headerVFS.rel())}}

	if preprocessHeader {
		generatedParsed = e.bisonGeneratedCPPParsed(srcVFS, headerVFS)
	}

	spec := &CompileSpec{FlatOutput: d.flatSrc(src)}

	if extras := d.perSrcCFlagsFor(src); extras != nil {
		spec.CFlags = append(spec.CFlags, *extras...)
	}

	if preprocessHeader {
		spec.CFlags = append(spec.CFlags, argWnoUnusedButSetVariable, argWnoDeprecatedCopy)
	}

	reg.register(&GeneratedFileInfo{
		OutputPath:     generatedVFS,
		ProducerRef:    ycRef,
		GeneratorRefs:  []NodeRef{bisonRef, m4Ref},
		ParsedIncludes: generatedParsed,
		Compile:        spec,
	})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envBISON_PKGDATADIR, Value: strBisonPkgData}, {Name: envM4, Value: m4Bin}}
	preprocessEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	head := make([]STR, 0, 6+len(d.cc.BisonFlags))

	head = append(head, internStr(bisonBin), argV.str())
	head = appendArgStr(head, d.cc.BisonFlags)

	head = append(head,
		internV("--defines=", headerVFS.string()),
		argDashO.str(),
		(generatedVFS).str(),
		(srcVFS).str())

	cmds := na.cmdList(Cmd{CmdArgs: na.chunkList(head), Env: env})
	inputs := []VFS{bldContribToolsBisonBison, bldContribToolsM4M4, srcVFS}

	if preprocessHeader {
		cmds = append(cmds, Cmd{
			CmdArgs: na.chunkList(na.strList(d.cc.TC.Python3,
				(bisonPreprocessPyVFS).str(),
				(headerVFS).str())),
			Env: preprocessEnv,
		})

		inputs = append(inputs, bisonPreprocessPyVFS)
		inputs = append(inputs, bisonCppSkeletonInputs...)
		headerCV := walkClosure(e.scanner, headerVFS, d.cc.ScanCfg)
		inputs = dedupClosure(inputs, headerCV.buckets[:])
	}

	ctx.emit.emitReservedNode(Node{
		Platform:     instance.Platform,
		Cmds:         cmds,
		DepRefs:      []NodeRef{bisonRef, m4Ref},
		Env:          env,
		Inputs:       na.inputList(inputs),
		Outputs:      na.vfsList(headerVFS, generatedVFS),
		KV:           &bisonYKV,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	}, ycRef)
}

func (e *EmitContext) emitBisonY(src STR) {
	_, instance, d := e.ctx, e.instance, e.d

	e.emitBisonProducer(src)

	generatedRel := bisonGeneratedRel(src.string(), d.cc.BisonGenExt)
	generatedVFS := build(instance.Path.rel(), "/", generatedRel)
	meta := d.srcMetaOf(src)

	meta.Generated = true
	meta.Source = generatedVFS.str()
	e.enqueueSrc(meta)
}

func bisonTool(ctx *GenCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool(argContribToolsBison)

	return ref, bin.string()
}

func m4Tool(ctx *GenCtx, instance ModuleInstance) (NodeRef, STR) {
	ref, bin := ctx.tool(argContribToolsM4)

	return ref, bin.str()
}
