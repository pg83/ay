package main

import (
	"path/filepath"
	"strings"
)

var bisonPreprocessPyVFS = Source("build/scripts/preprocess.py")

var bisonCppSkeletonInputs = []VFS{
	Source("contrib/tools/bison/data/m4sugar/foreach.m4"),
	Source("contrib/tools/bison/data/m4sugar/m4sugar.m4"),
	Source("contrib/tools/bison/data/skeletons/bison.m4"),
	Source("contrib/tools/bison/data/skeletons/c++-skel.m4"),
	Source("contrib/tools/bison/data/skeletons/c++.m4"),
	Source("contrib/tools/bison/data/skeletons/c-like.m4"),
	Source("contrib/tools/bison/data/skeletons/c-skel.m4"),
	Source("contrib/tools/bison/data/skeletons/c.m4"),
	Source("contrib/tools/bison/data/skeletons/glr.cc"),
	Source("contrib/tools/bison/data/skeletons/lalr1.cc"),
	Source("contrib/tools/bison/data/skeletons/location.cc"),
	Source("contrib/tools/bison/data/skeletons/stack.hh"),
	Source("contrib/tools/bison/data/skeletons/variant.hh"),
	Source("contrib/tools/bison/data/skeletons/yacc.c"),
}

func dedupIncludeDirectives(directives []includeDirective) []includeDirective {
	if len(directives) == 0 {
		return nil
	}

	seen := make(map[includeDirective]struct{}, len(directives))
	out := make([]includeDirective, 0, len(directives))
	for _, d := range directives {
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}

	return out
}

func bisonCppHeaderParsed(srcVFS VFS) []includeDirective {
	parsed := make([]includeDirective, 0, 1+len(bisonCppSkeletonInputs))
	parsed = append(parsed,
		includeDirective{kind: includeQuoted, target: bisonPreprocessPyVFS.Rel()},
	)
	for _, input := range bisonCppSkeletonInputs {
		parsed = append(parsed, includeDirective{kind: includeQuoted, target: input.Rel()})
	}

	return dedupIncludeDirectives(parsed)
}

func bisonGeneratedCPPParsed(ctx *genCtx, instance ModuleInstance, srcVFS, headerVFS VFS) []includeDirective {
	parsed := []includeDirective{
		{kind: includeQuoted, target: headerVFS.Rel()},
		{kind: includeQuoted, target: srcVFS.Rel()},
	}
	if scanner := ctx.scannerFor(instance); scanner != nil {
		parsed = append(parsed, scanner.parsers.sourceParsedBuckets(srcVFS.Rel()).bucket(parsedIncludesLocal)...)
	}

	return dedupIncludeDirectives(parsed)
}

func emitBisonY(ctx *genCtx, instance ModuleInstance, srcRel string, in ModuleCCInputs, genExt string) *sourceEmit {
	bisonRef, bisonBin := bisonTool(ctx, instance)
	m4Ref, m4Bin := m4Tool(ctx, instance)
	preprocessHeader := genExt != ".c"

	baseNoExt := strings.TrimSuffix(srcRel, filepath.Ext(srcRel))
	headerRel := baseNoExt + ".h"
	generatedRel := "_/" + srcRel + genExt
	headerVFS := Build(instance.Path + "/" + headerRel)
	generatedVFS := Build(instance.Path + "/" + generatedRel)
	srcVFS := Source(instance.Path + "/" + srcRel)
	headerParsed := []includeDirective{{kind: includeQuoted, target: srcVFS.Rel()}}
	if preprocessHeader {
		headerParsed = bisonCppHeaderParsed(srcVFS)
	} else if scanner := ctx.scannerFor(instance); scanner != nil {
		headerParsed = append(headerParsed, scanner.parsers.sourceParsedBuckets(srcVFS.Rel()).bucket(parsedIncludesLocal)...)
	}
	registerGeneratedParsedOutput(ctx, instance, "YC", headerVFS, dedupIncludeDirectives(headerParsed))

	generatedParsed := []includeDirective{{kind: includeQuoted, target: headerVFS.Rel()}}
	if preprocessHeader {
		generatedParsed = bisonGeneratedCPPParsed(ctx, instance, srcVFS, headerVFS)
	}
	registerGeneratedParsedOutput(ctx, instance, "YC", generatedVFS, generatedParsed)

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"BISON_PKGDATADIR":       "$(S)/contrib/tools/bison/data",
		"M4":                     m4Bin,
	}
	preprocessEnv := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	cmds := []Cmd{
		{
			CmdArgs: []string{
				bisonBin,
				"-v",
				"--defines=" + headerVFS.String(),
				"-o",
				generatedVFS.String(),
				srcVFS.String(),
			},
			Env: env,
		},
	}
	inputs := []VFS{Build("contrib/tools/bison/bison"), Build("contrib/tools/m4/m4"), srcVFS}
	if preprocessHeader {
		cmds = append(cmds, Cmd{
			CmdArgs: []string{
				instance.Platform.Tools.Python3,
				bisonPreprocessPyVFS.String(),
				headerVFS.String(),
			},
			Env: preprocessEnv,
		})
		inputs = append(inputs, bisonPreprocessPyVFS)
		inputs = append(inputs, bisonCppSkeletonInputs...)
		inputs = mergeDedupVFS(inputs, generatedOutputClosure(ctx, instance, headerVFS, in))
	}

	ycRef := ctx.emit.Emit(bindNodePlatform(&Node{
		Cmds:    cmds,
		DepRefs: []NodeRef{bisonRef, m4Ref},
		Env:     env,
		Inputs:  inputs,
		Outputs: []VFS{headerVFS, generatedVFS},
		KV: map[string]interface{}{
			"p":  "YC",
			"pc": "light-green",
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: instance.Platform.Tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
	}, instance.Platform))
	bindGeneratedOutput(ctx, instance, headerVFS, ycRef)
	bindGeneratedOutput(ctx, instance, generatedVFS, ycRef)

	ccIn := in
	ccIn.ExtraDepRefs = []NodeRef{ycRef}
	ccIn.IncludeInputs = walkClosure(ctx, instance, generatedVFS, in)

	ccRef, ccOut, _ := EmitCC(instance, generatedRel, generatedVFS, ccIn, ctx.host, ctx.emit)

	return &sourceEmit{Ref: ccRef, OutPath: ccOut}
}

func bisonTool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool("contrib/tools/bison")
	return ref, bin.String()
}

func m4Tool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool("contrib/tools/m4")
	return ref, bin.String()
}
