package main

import (
	"path/filepath"
	"strings"
)

var (
	bisonPreprocessPyVFS = Intern("$(S)/build/scripts/preprocess.py")
	// Path constants hoisted by `ay refac consts`.
	bldContribToolsBisonBison = Build("contrib/tools/bison/bison")
	bldContribToolsM4M4       = Build("contrib/tools/m4/m4")
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
		includeDirective{kind: includeQuoted, target: internString(bisonPreprocessPyVFS.Rel())},
	)

	for _, input := range bisonCppSkeletonInputs {
		parsed = append(parsed, includeDirective{kind: includeQuoted, target: internString(input.Rel())})
	}

	return dedupIncludeDirectives(parsed)
}

func bisonGeneratedCPPParsed(ctx *genCtx, instance ModuleInstance, srcVFS, headerVFS VFS) []includeDirective {
	parsed := []includeDirective{
		{kind: includeQuoted, target: internString(headerVFS.Rel())},
		{kind: includeQuoted, target: internString(srcVFS.Rel())},
	}

	if scanner := ctx.scannerFor(instance); scanner != nil {
		parsed = append(parsed, scanner.parsers.sourceParsedBuckets(srcVFS).bucket(parsedIncludesLocal)...)
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
	headerParsed := []includeDirective{{kind: includeQuoted, target: internString(srcVFS.Rel())}}

	if preprocessHeader {
		headerParsed = bisonCppHeaderParsed(srcVFS)
	} else if scanner := ctx.scannerFor(instance); scanner != nil {
		headerParsed = append(headerParsed, scanner.parsers.sourceParsedBuckets(srcVFS).bucket(parsedIncludesLocal)...)
	}

	registerGeneratedParsedOutput(ctx, instance, "YC", headerVFS, dedupIncludeDirectives(headerParsed))

	if preprocessHeader {
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			reg.SetSourceInputs(headerVFS, []VFS{srcVFS})
		}
	}

	generatedParsed := []includeDirective{{kind: includeQuoted, target: internString(headerVFS.Rel())}}

	if preprocessHeader {
		generatedParsed = bisonGeneratedCPPParsed(ctx, instance, srcVFS, headerVFS)
	}

	registerGeneratedParsedOutput(ctx, instance, "YC", generatedVFS, generatedParsed)

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}, {Name: "BISON_PKGDATADIR", Value: "$(S)/contrib/tools/bison/data"}, {Name: "M4", Value: m4Bin}}
	preprocessEnv := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

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
	inputs := []VFS{bldContribToolsBisonBison, bldContribToolsM4M4, srcVFS}

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
		inputs = dedupVFS(inputs, generatedOutputClosure(ctx, instance, headerVFS, in))
	}

	ycRef := ctx.emit.Emit(bindNodePlatform(withResources(&Node{
		Cmds:             cmds,
		DepRefs:          []NodeRef{bisonRef, m4Ref},
		Env:              env,
		Inputs:           inputs,
		Outputs:          []VFS{headerVFS, generatedVFS},
		KV:               KV{P: "YC", PC: "light-green"},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		Tags:             instance.Platform.Tags,
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
	}, resourcePatternYMakePython3), instance.Platform))
	bindGeneratedOutput(ctx, instance, headerVFS, ycRef)
	bindGeneratedOutput(ctx, instance, generatedVFS, ycRef)

	ccIn := in
	ccIn.ExtraDepRefs = []NodeRef{ycRef}
	ccIn.IncludeInputs = walkClosure(ctx, instance, generatedVFS, in)

	if preprocessHeader {
		ccIn.PerSourceCFlags = append(append([]string(nil), in.PerSourceCFlags...), "-Wno-unused-but-set-variable", "-Wno-deprecated-copy")
	}

	ccRef, ccOut, _ := EmitCC(instance, generatedRel, generatedVFS, ccIn, ctx.host, ctx.emit)
	return &sourceEmit{Ref: ccRef, OutPath: ccOut}
}

// bisonCCSourceInputs returns the bison source (.y) files that should be
// added as inputs to CC nodes whose include closure reaches bison-generated
// C++ headers. Upstream propagates the source .y file to any CC node that
// includes the generated header (the header's SourceInputs records the .y).
func bisonCCSourceInputs(ctx *genCtx, instance ModuleInstance, closure []VFS) []VFS {
	reg := codegenRegForInstance(ctx, instance)

	if reg == nil {
		return nil
	}

	var extras []VFS

	for _, v := range closure {
		info := reg.Lookup(v)

		if info == nil || len(info.SourceInputs) == 0 {
			continue
		}

		extras = dedupVFS(extras, info.SourceInputs)
	}

	return extras
}

func bisonTool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool("contrib/tools/bison")
	return ref, bin.String()
}

func m4Tool(ctx *genCtx, instance ModuleInstance) (NodeRef, string) {
	ref, bin := ctx.tool("contrib/tools/m4")
	return ref, bin.String()
}
