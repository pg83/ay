package main

import (
	"path/filepath"
	"strings"
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

var bisonCppSkeletonDirectives = quotedDirectives(bisonCppSkeletonInputs)

func bisonCppHeaderParsed(srcVFS VFS) []IncludeDirective {
	parsed := make([]IncludeDirective, 0, 1+len(bisonCppSkeletonDirectives))
	parsed = append(parsed,
		IncludeDirective{kind: includeQuoted, target: internStr(bisonPreprocessPyVFS.rel())},
	)
	parsed = append(parsed, bisonCppSkeletonDirectives...)

	return parsed
}

func bisonGeneratedCPPParsed(ctx *GenCtx, instance ModuleInstance, srcVFS, headerVFS VFS) []IncludeDirective {
	parsed := []IncludeDirective{
		{kind: includeQuoted, target: internStr(headerVFS.rel())},
		{kind: includeQuoted, target: internStr(srcVFS.rel())},
	}

	parsed = append(parsed, ctx.scannerFor(instance).parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)

	return parsed
}

// bisonGeneratedRel is the module-relative path of a bison-generated source; a subdir
// source is rebased under the _/ namespace.
func bisonGeneratedRel(srcRel, genExt string) string {
	if strings.Contains(srcRel, "/") {
		return "_/" + srcRel + genExt
	}

	return srcRel + genExt
}

// emitBisonProducer emits the YC (bison + preprocess) node and registers its generated
// header and source. It runs in the pre-pass, before any sibling's closure is walked, so a
// sibling including the generated parser.h resolves to this $(B) output rather than caching
// a stale empty closure (scanCache is shared across configs).
func emitBisonProducer(ctx *GenCtx, instance ModuleInstance, srcRel string, in ModuleCCInputs, genExt string) {
	na := ctx.na

	bisonRef, bisonBin := bisonTool(ctx, instance)
	m4Ref, m4Bin := m4Tool(ctx, instance)
	preprocessHeader := genExt != ".c"

	baseNoExt := strings.TrimSuffix(srcRel, filepath.Ext(srcRel))
	headerRel := baseNoExt + ".h"
	generatedRel := bisonGeneratedRel(srcRel, genExt)
	headerVFS := build(instance.Path.rel() + "/" + headerRel)
	generatedVFS := build(instance.Path.rel() + "/" + generatedRel)
	srcVFS := source(instance.Path.rel() + "/" + srcRel)
	headerParsed := []IncludeDirective{{kind: includeQuoted, target: internStr(srcVFS.rel())}}

	if preprocessHeader {
		headerParsed = bisonCppHeaderParsed(srcVFS)
	} else {
		headerParsed = append(headerParsed, ctx.scannerFor(instance).parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)
	}

	// Reserve before registering: the generatedOutputClosure walk below needs the registration first.
	ycRef := ctx.emit.reserve()

	registerBoundGeneratedParsedOutput(ctx, instance, pkYC, headerVFS, headerParsed, ycRef, []NodeRef{bisonRef, m4Ref})

	if preprocessHeader {
		// The .y source is a real input of any unit reaching this preprocessed header;
		// ride it as a non-expanded closure leaf so consumers pick it up transitively.
		codegenRegForInstance(ctx, instance).addClosureLeaf(headerVFS, srcVFS)
	}

	generatedParsed := []IncludeDirective{{kind: includeQuoted, target: internStr(headerVFS.rel())}}

	if preprocessHeader {
		generatedParsed = bisonGeneratedCPPParsed(ctx, instance, srcVFS, headerVFS)
	}

	registerBoundGeneratedParsedOutput(ctx, instance, pkYC, generatedVFS, generatedParsed, ycRef, []NodeRef{bisonRef, m4Ref})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envBISON_PKGDATADIR, Value: strBisonPkgData}, {Name: envM4, Value: m4Bin}}
	preprocessEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// BISON_FLAGS (default -v) expands between the bison tool and --defines.
	head := make([]STR, 0, 6+len(in.BisonFlags))
	head = append(head, internStr(bisonBin), argV.str())
	head = appendArgStr(head, in.BisonFlags)
	head = append(head,
		internStr("--defines="+headerVFS.string()),
		argDashO.str(),
		(generatedVFS).str(),
		(srcVFS).str())
	cmds := na.cmdList(Cmd{CmdArgs: na.chunkList(head), Env: env})
	inputs := []VFS{bldContribToolsBisonBison, bldContribToolsM4M4, srcVFS}

	if preprocessHeader {
		cmds = append(cmds, Cmd{
			CmdArgs: na.chunkList(na.strList(in.TC.Python3,
				(bisonPreprocessPyVFS).str(),
				(headerVFS).str())),
			Env: preprocessEnv,
		})
		inputs = append(inputs, bisonPreprocessPyVFS)
		inputs = append(inputs, bisonCppSkeletonInputs...)
		inputs = dedupVFS(inputs, generatedOutputClosure(ctx, instance, headerVFS, in))
	}

	ctx.emit.emitReserved(&Node{
		Platform:         instance.Platform,
		Cmds:             cmds,
		DepRefs:          []NodeRef{bisonRef, m4Ref},
		Env:              env,
		Inputs:           na.inputList(inputs),
		Outputs:          na.vfsList(headerVFS, generatedVFS),
		KV:               KV{P: pkYC, PC: pcLightGreen},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Resources:        usesPython3,
	}, ycRef)
}

func emitBisonY(ctx *GenCtx, instance ModuleInstance, srcRel string, in ModuleCCInputs, genExt string) *SourceEmit {
	preprocessHeader := genExt != ".c"
	generatedRel := bisonGeneratedRel(srcRel, genExt)
	generatedVFS := build(instance.Path.rel() + "/" + generatedRel)

	// The YC producer was registered in the pre-pass; take its ref.
	ycRef := codegenRegForInstance(ctx, instance).lookup(generatedVFS).ProducerRef

	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), generatedVFS, in.ScanCfg)
	// A generated header reachable through the closure makes its producer a dep; ycRef
	// is excluded to avoid a dup.
	ccIn.ExtraDepRefs = append([]NodeRef{ycRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, ycRef)...)

	if preprocessHeader {
		ccIn.PerSourceCFlags = append(append([]ARG(nil), in.PerSourceCFlags...), argWnoUnusedButSetVariable, argWnoDeprecatedCopy)
	}

	ccRef, ccOut, _ := emitCC(instance, generatedRel, generatedVFS, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}

func bisonTool(ctx *GenCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool(argContribToolsBison)

	return ref, bin.string()
}

func m4Tool(ctx *GenCtx, instance ModuleInstance) (NodeRef, STR) {
	ref, bin := ctx.tool(argContribToolsM4)

	return ref, bin.str()
}
