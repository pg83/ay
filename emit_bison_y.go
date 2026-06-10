package main

import (
	"path/filepath"
	"strings"
)

var bisonCppSkeletonInputs = []VFS{
	Intern("$(S)/contrib/tools/bison/data/m4sugar/foreach.m4"),
	Intern("$(S)/contrib/tools/bison/data/m4sugar/m4sugar.m4"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/bison.m4"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/c++-skel.m4"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/c++.m4"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/c-like.m4"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/c-skel.m4"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/c.m4"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/glr.cc"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/lalr1.cc"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/location.cc"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/stack.hh"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/variant.hh"),
	Intern("$(S)/contrib/tools/bison/data/skeletons/yacc.c"),
}

var bisonCppSkeletonDirectives = quotedDirectives(bisonCppSkeletonInputs)

func bisonCppHeaderParsed(srcVFS VFS) []includeDirective {
	parsed := make([]includeDirective, 0, 1+len(bisonCppSkeletonDirectives))
	parsed = append(parsed,
		includeDirective{kind: includeQuoted, target: internStr(bisonPreprocessPyVFS.Rel())},
	)
	parsed = append(parsed, bisonCppSkeletonDirectives...)

	return dedupDirectives(parsed)
}

func bisonGeneratedCPPParsed(ctx *genCtx, instance ModuleInstance, srcVFS, headerVFS VFS) []includeDirective {
	parsed := []includeDirective{
		{kind: includeQuoted, target: internStr(headerVFS.Rel())},
		{kind: includeQuoted, target: internStr(srcVFS.Rel())},
	}

	if scanner := ctx.scannerFor(instance); scanner != nil {
		parsed = append(parsed, scanner.parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)
	}

	return dedupDirectives(parsed)
}

func emitBisonY(ctx *genCtx, instance ModuleInstance, srcRel string, in ModuleCCInputs, genExt string) *sourceEmit {
	bisonRef, bisonBin := bisonTool(ctx, instance)
	m4Ref, m4Bin := m4Tool(ctx, instance)
	preprocessHeader := genExt != ".c"

	baseNoExt := strings.TrimSuffix(srcRel, filepath.Ext(srcRel))
	headerRel := baseNoExt + ".h"
	generatedRel := "_/" + srcRel + genExt
	headerVFS := Build(instance.Path.Rel() + "/" + headerRel)
	generatedVFS := Build(instance.Path.Rel() + "/" + generatedRel)
	srcVFS := Source(instance.Path.Rel() + "/" + srcRel)
	headerParsed := []includeDirective{{kind: includeQuoted, target: internStr(srcVFS.Rel())}}

	if preprocessHeader {
		headerParsed = bisonCppHeaderParsed(srcVFS)
	} else if scanner := ctx.scannerFor(instance); scanner != nil {
		headerParsed = append(headerParsed, scanner.parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesLocal)...)
	}

	registerGeneratedParsedOutput(ctx, instance, pkYC, headerVFS, dedupDirectives(headerParsed), []NodeRef{bisonRef, m4Ref})

	if preprocessHeader {
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			// The .y source is a real input of any unit whose include-closure reaches
			// this preprocessed header. Ride it as a non-expanded closure leaf so every
			// consumer picks it up transitively through the cached window, instead of
			// the former per-CC-source pull (bisonCCSourceInputs).
			reg.AddClosureLeaf(headerVFS, srcVFS)
		}
	}

	generatedParsed := []includeDirective{{kind: includeQuoted, target: internStr(headerVFS.Rel())}}

	if preprocessHeader {
		generatedParsed = bisonGeneratedCPPParsed(ctx, instance, srcVFS, headerVFS)
	}

	registerGeneratedParsedOutput(ctx, instance, pkYC, generatedVFS, generatedParsed, []NodeRef{bisonRef, m4Ref})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}, {Name: envBISON_PKGDATADIR, Value: strBisonPkgData}, {Name: envM4, Value: m4Bin}}
	preprocessEnv := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmds := []Cmd{
		{
			CmdArgs: []STR{
				internStr(bisonBin),
				argV.str(),
				internStr("--defines=" + headerVFS.String()),
				argDashO.str(),
				(generatedVFS).str(),
				(srcVFS).str(),
			},
			Env: env,
		},
	}
	inputs := []VFS{bldContribToolsBisonBison, bldContribToolsM4M4, srcVFS}

	if preprocessHeader {
		cmds = append(cmds, Cmd{
			CmdArgs: []STR{
				in.TC.Python3,
				(bisonPreprocessPyVFS).str(),
				(headerVFS).str(),
			},
			Env: preprocessEnv,
		})
		inputs = append(inputs, bisonPreprocessPyVFS)
		inputs = append(inputs, bisonCppSkeletonInputs...)
		inputs = dedupVFS(inputs, generatedOutputClosure(ctx, instance, headerVFS, in))
	}

	ycRef := ctx.emit.Emit(&Node{
		Platform:         instance.Platform,
		Cmds:             cmds,
		DepRefs:          []NodeRef{bisonRef, m4Ref},
		Env:              env,
		Inputs:           inputChunks{inputs},
		Outputs:          []VFS{headerVFS, generatedVFS},
		KV:               KV{P: pkYC, PC: pcLightGreen},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.Rel()},
		usesResources:    []string{resourcePatternYMakePython3},
	})
	bindGeneratedOutput(ctx, instance, headerVFS, ycRef)
	bindGeneratedOutput(ctx, instance, generatedVFS, ycRef)

	ccIn := in
	ccIn.ExtraDepRefs = []NodeRef{ycRef}
	ccIn.IncludeInputs = walkClosure(ctx, instance, generatedVFS, in)

	if preprocessHeader {
		ccIn.PerSourceCFlags = append(append([]ARG(nil), in.PerSourceCFlags...), argWnoUnusedButSetVariable, argWnoDeprecatedCopy)
	}

	ccRef, ccOut, _ := EmitCC(instance, generatedRel, generatedVFS, ccIn, ctx.host, ctx.emit)
	return &sourceEmit{Ref: ccRef, OutPath: ccOut}
}

func bisonTool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool(argContribToolsBison)
	return ref, bin.String()
}

func m4Tool(ctx *genCtx, instance ModuleInstance) (NodeRef, STR) {
	ref, bin := ctx.tool(argContribToolsM4)
	return ref, bin.str()
}
