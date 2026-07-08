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
	source("contrib/tools/bison/data/m4sugar/foreach.m4"),
	source("contrib/tools/bison/data/m4sugar/m4sugar.m4"),
	source("contrib/tools/bison/data/skeletons/bison.m4"),
	source("contrib/tools/bison/data/skeletons/c++-skel.m4"),
	source("contrib/tools/bison/data/skeletons/c++.m4"),
	source("contrib/tools/bison/data/skeletons/c-like.m4"),
	source("contrib/tools/bison/data/skeletons/c-skel.m4"),
	source("contrib/tools/bison/data/skeletons/c.m4"),
	source("contrib/tools/bison/data/skeletons/glr.cc"),
	source("contrib/tools/bison/data/skeletons/lalr1.cc"),
	source("contrib/tools/bison/data/skeletons/location.cc"),
	source("contrib/tools/bison/data/skeletons/stack.hh"),
	source("contrib/tools/bison/data/skeletons/variant.hh"),
	source("contrib/tools/bison/data/skeletons/yacc.c"),
}

func bisonCppHeaderParsed(srcVFS VFS) []IncludeDirective {
	parsed := make([]IncludeDirective, 0, 1+len(bisonCppSkeletonDirectives))

	parsed = append(parsed,
		IncludeDirective{kind: includeQuoted, target: includeTarget(bisonPreprocessPyVFS.rel())},
	)

	parsed = append(parsed, bisonCppSkeletonDirectives...)

	return parsed
}

func (e *EmitContext) bisonGeneratedCPPParsed(srcVFS, headerVFS VFS) []IncludeDirective {
	parsed := []IncludeDirective{
		{kind: includeQuoted, target: includeTarget(headerVFS.rel())},
		{kind: includeQuoted, target: includeTarget(srcVFS.rel())},
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
	headerVFS := build(instance.Path.relString(), "/", headerRel)
	generatedVFS := build(instance.Path.relString(), "/", generatedRel)
	srcVFS := source(instance.Path.relString(), "/", srcRel)
	headerParsed := []IncludeDirective{{kind: includeQuoted, target: includeTarget(srcVFS.rel())}}

	if preprocessHeader {
		headerParsed = append([]IncludeDirective{{kind: includeQuoted, target: includeTarget(srcVFS.rel())}}, bisonCppHeaderParsed(srcVFS)...)
	} else {
		headerParsed = append(headerParsed, e.scanner.parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)
	}

	ycRef := ctx.emit.reserve()
	reg := e.codegen

	headerInfo := &GeneratedFileInfo{
		OutputPath:     headerVFS,
		ProducerRef:    ycRef,
		GeneratorRefs:  []NodeRef{bisonRef, m4Ref},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerParsed},
	}

	reg.register(headerInfo)

	generatedParsed := []IncludeDirective{{kind: includeQuoted, target: includeTarget(headerVFS.rel())}}

	if preprocessHeader {
		generatedParsed = e.bisonGeneratedCPPParsed(srcVFS, headerVFS)
	}

	spec := &CompileSpec{FlatOutput: d.flatSrc(src.any())}

	if extras := d.perSrcCFlagsFor(src.any()); extras != nil {
		spec.CFlags = append(spec.CFlags, *extras...)
	}

	if preprocessHeader {
		spec.CFlags = append(spec.CFlags, argWnoUnusedButSetVariable.any(), argWnoDeprecatedCopy.any())
	}

	reg.register(&GeneratedFileInfo{
		OutputPath:     generatedVFS,
		ProducerRef:    ycRef,
		GeneratorRefs:  []NodeRef{bisonRef, m4Ref},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: generatedParsed},
		Compile:        spec,
	})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}, {Name: envBISON_PKGDATADIR, Value: strBisonPkgData.any()}, {Name: envM4, Value: m4Bin.any()}}
	preprocessEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}}
	head := make([]ANY, 0, 6+len(d.cc.BisonFlags))

	head = append(head, bisonBin.any(), argV.any())
	head = appendAnyLists(head, d.cc.BisonFlags)

	head = append(head,
		internV("--defines=", headerVFS.prefix(), headerVFS.relString()).any(),
		argDashO.any(),
		(generatedVFS).any(),
		(srcVFS).any())

	cmds := na.cmdList(Cmd{CmdArgs: na.chunkList(head), Env: env})
	inputs := []VFS{bldContribToolsBisonBison, bldContribToolsM4M4, srcVFS}

	if preprocessHeader {
		cmds = append(cmds, Cmd{
			CmdArgs: na.chunkList(na.anyList(d.cc.TC.Python3.any(),
				(bisonPreprocessPyVFS).any(),
				(headerVFS).any())),
			Env: preprocessEnv,
		})

		inputs = append(inputs, bisonPreprocessPyVFS)
		inputs = append(inputs, bisonCppSkeletonInputs...)

		for _, sk := range bisonCppSkeletonInputs {
			skCV := walkClosure(e.scanner, sk, d.cc.ScanCfg)

			inputs = dedupClosure(inputs, skCV.buckets)
		}
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

func (e *EmitContext) emitBisonY(src ANY) {
	_, instance, d := e.ctx, e.instance, e.d

	e.emitBisonProducer(src.str())

	generatedRel := bisonGeneratedRel(src.string(), d.cc.BisonGenExt)
	generatedVFS := build(instance.Path.relString(), "/", generatedRel)
	meta := d.srcMetaOf(src)

	meta.Generated = true
	meta.Source = generatedVFS.any()
	e.enqueueSrc(meta)
}

func bisonTool(ctx *GenCtx, instance ModuleInstance) (NodeRef, VFS) {
	return ctx.tool(argContribToolsBison)
}

func m4Tool(ctx *GenCtx, instance ModuleInstance) (NodeRef, VFS) {
	return ctx.tool(argContribToolsM4)
}
