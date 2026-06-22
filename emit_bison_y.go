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

// bisonGeneratedRel is the module-relative path of a bison-generated source.
// Upstream's nopath output placement: a flat (in-module) source lands its
// generated .cpp directly in the module build dir; only a subdir source is
// rebased under the _/ namespace (same rule as ragel6OutVFS / composeCCPaths).
func bisonGeneratedRel(srcRel, genExt string) string {
	if strings.Contains(srcRel, "/") {
		return "_/" + srcRel + genExt
	}

	return srcRel + genExt
}

// emitBisonProducer emits the YC (bison + preprocess) node and registers its
// generated header and source in the codegen registry. It runs in Pass 1's
// pre-pass — BEFORE any sibling source's include closure is walked — so a
// sibling that includes the generated <…/parser.h> resolves it to this $(B)
// output instead of caching an empty closure (the scanCache is keyed by file
// id, shared across configs; a stale miss would poison every later consumer).
// emitBisonY then takes the producer ref from the registry and compiles the
// generated source. This mirrors the flatc/proto register-then-compile split.
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

	// Reserve the YC producer's ref before registering its outputs: the
	// generatedOutputClosure walk of headerVFS below needs the registration first.
	ycRef := ctx.emit.reserve()

	registerBoundGeneratedParsedOutput(ctx, instance, pkYC, headerVFS, headerParsed, ycRef, []NodeRef{bisonRef, m4Ref})

	if preprocessHeader {
		// The .y source is a real input of any unit whose include-closure reaches
		// this preprocessed header. Ride it as a non-expanded closure leaf so every
		// consumer picks it up transitively through the cached window, instead of
		// the former per-CC-source pull (bisonCCSourceInputs).
		codegenRegForInstance(ctx, instance).addClosureLeaf(headerVFS, srcVFS)
	}

	generatedParsed := []IncludeDirective{{kind: includeQuoted, target: internStr(headerVFS.rel())}}

	if preprocessHeader {
		generatedParsed = bisonGeneratedCPPParsed(ctx, instance, srcVFS, headerVFS)
	}

	registerBoundGeneratedParsedOutput(ctx, instance, pkYC, generatedVFS, generatedParsed, ycRef, []NodeRef{bisonRef, m4Ref})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envBISON_PKGDATADIR, Value: strBisonPkgData}, {Name: envM4, Value: m4Bin}}
	preprocessEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	// Upstream `$BISON_FLAGS` (default -v, SET_APPEND'd by the BISON_FLAGS macro)
	// expands between the bison tool and $_BISON_HEADER (--defines): the module's
	// flags follow the default -v and precede --defines.
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

	// The YC producer was emitted+registered in the pre-pass (emitBisonProducer);
	// take its ref from the codegen registry and compile the generated source.
	ycRef := codegenRegForInstance(ctx, instance).lookup(generatedVFS).ProducerRef

	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), generatedVFS, in.ScanCfg)
	// A generated proto/codegen header reachable through the parser source's
	// include closure makes its producer a dep of this compile node — upstream's
	// flat dep model, the same pass every other generated-source CC emitter runs.
	// ycRef (the bison producer) is excluded so it is not duplicated.
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
