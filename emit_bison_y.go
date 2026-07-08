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
		IncludeDirective{kind: includeQuoted, target: includeTarget(bisonPreprocessPyVFS.rel().any())},
	)

	parsed = append(parsed, bisonCppSkeletonDirectives...)

	return parsed
}

func (e *EmitContext) bisonGeneratedCPPParsed(srcVFS, headerVFS VFS) []IncludeDirective {
	parsed := []IncludeDirective{
		{kind: includeQuoted, target: includeTarget(headerVFS.rel().any())},
		{kind: includeQuoted, target: includeTarget(srcVFS.rel().any())},
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
	headerParsed := []IncludeDirective{{kind: includeQuoted, target: includeTarget(srcVFS.rel().any())}}

	if preprocessHeader {
		headerParsed = append([]IncludeDirective{{kind: includeQuoted, target: includeTarget(srcVFS.rel().any())}}, bisonCppHeaderParsed(srcVFS)...)
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

	generatedParsed := []IncludeDirective{{kind: includeQuoted, target: includeTarget(headerVFS.rel().any())}}

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

	env := na.envList(EnvVar{Name: envARCADIA_ROOT_DISTBUILD, Value: strS.any()}, EnvVar{Name: envBISON_PKGDATADIR, Value: strBisonPkgData.any()}, EnvVar{Name: envM4, Value: m4Bin.any()})
	preprocessEnv := envVarsVCS
	head := na.anys.alloc(6 + len(d.cc.BisonFlags))[:0]

	head = append(head, bisonBin.any(), argV.any())
	head = appendAnyLists(head, d.cc.BisonFlags)

	head = append(head,
		internV("--defines=", headerVFS.prefix(), headerVFS.relString()).any(),
		argDashO.any(),
		(generatedVFS).any(),
		(srcVFS).any())

	na.anys.commit(len(head))

	head = head[:len(head):len(head)]

	cmds := na.cmds.alloc(2)[:0]

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(head), Env: env})

	inputs := na.vfsList(bldContribToolsBisonBison, bldContribToolsM4M4, srcVFS)

	if preprocessHeader {
		cmds = append(cmds, Cmd{
			CmdArgs: na.chunkList(na.anyList(d.cc.TC.Python3.any(),
				(bisonPreprocessPyVFS).any(),
				(headerVFS).any())),
			Env: preprocessEnv,
		})

		ext := na.vfs.alloc(len(inputs) + 1 + len(bisonCppSkeletonInputs))[:0]

		ext = append(ext, inputs...)
		ext = append(ext, bisonPreprocessPyVFS)
		ext = append(ext, bisonCppSkeletonInputs...)
		na.vfs.commit(len(ext))

		inputs = ext[:len(ext):len(ext)]

		for _, sk := range bisonCppSkeletonInputs {
			skCV := walkClosure(e.scanner, sk, d.cc.ScanCfg)

			inputs = dedupClosure(na, inputs, skCV.buckets)
		}
	}

	na.cmds.commit(len(cmds))

	cmds = cmds[:len(cmds):len(cmds)]

	ctx.emit.emitReservedNode(Node{
		Platform:     instance.Platform,
		Cmds:         cmds,
		DepRefs:      na.refList(bisonRef, m4Ref),
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
