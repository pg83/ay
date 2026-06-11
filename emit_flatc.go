package main

import (
	"path/filepath"
	"strings"
)

// flatcConstFlags / flatcIOLeadArgs are the constant spans of every flatc
// command around the module's FLATC_FLAGS; the per-node remainder is
// [<header>, <src>].
var flatcConstFlags = []STR{
	argNoWarnings.str(),
	argCpp.str(),
	argKeepPrefix.str(),
	argGenMutable.str(),
	argSchema.str(),
	argB2.str(),
	argGenObjectApi.str(),
	argFilenameSuffix.str(),
	argFbs.str(),
}

var flatcIOLeadArgs = []STR{
	argI.str(), argB.str(),
	argI.str(), argS.str(),
	argDashO.str(),
}

type FlatcEmission struct {
	flRef   NodeRef
	header  VFS
	cpp     VFS
	relPath string
}

func flatcDirectImportNames(pm *IncludeParserManager, srcRel string) []string {
	direct := pm.sourceParsedBuckets(Source(srcRel), nil).bucket(parsedIncludesLocal)

	if len(direct) == 0 {
		return nil
	}

	out := make([]string, 0, len(direct))

	for _, d := range direct {
		out = append(out, d.target.String())
	}

	return out
}

func resolveFlatcImportPath(fs FS, includerRel, importedRel string) string {
	candidates := []string{
		filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(includerRel), importedRel))),
		filepath.ToSlash(filepath.Clean(importedRel)),
	}

	for _, cand := range candidates {
		if fs.isFile(srcRootVFS, cand) {
			return cand
		}
	}

	return ""
}

func flatcDirectGeneratedHeaderIncludes(pm *IncludeParserManager, fs FS, srcRel string) []IncludeDirective {
	direct := flatcDirectImportNames(pm, srcRel)

	if len(direct) == 0 {
		return nil
	}

	out := make([]IncludeDirective, 0, len(direct))

	for _, imp := range direct {
		resolved := resolveFlatcImportPath(fs, srcRel, imp)

		if resolved == "" {
			continue
		}

		out = append(out, IncludeDirective{
			kind:   includeQuoted,
			target: internStr(resolved + ".h"),
		})
	}

	return out
}

func flatcResolvedModuleSourceRel(ctx *GenCtx, instance ModuleInstance, d *ModuleData, resolvedRel string) (string, bool) {
	candidates := append([]string(nil), d.srcs...)
	candidates = append(candidates, d.globalSrcs...)

	for _, srcRel := range candidates {
		if !strings.HasSuffix(srcRel, ".fbs") {
			continue
		}

		if resolveSourceVFS(ctx, instance, srcRel, d.srcDirs).rel() == resolvedRel {
			return srcRel, true
		}
	}

	return "", false
}

func EmitFL(instance ModuleInstance, srcRel string, srcVFS VFS, flatcLDRef NodeRef, flatcBinary VFS, flatcFlags []ARG, transitiveImports []VFS, tc ModuleToolchain, emit Emitter) (NodeRef, VFS, VFS, VFS) {
	headerVFS := Build(srcRel + ".h")
	cppVFS := Build(srcRel + ".cpp")
	bfbsVFS := Build(strings.TrimSuffix(srcRel, ".fbs") + ".bfbs")

	cmdArgs := ArgChunks{
		{tc.Python3, (flatcWrapperVFS).str(), (flatcBinary).str()},
		flatcConstFlags,
	}

	if len(flatcFlags) > 0 {
		cmdArgs = append(cmdArgs, appendArgStr(nil, flatcFlags))
	}

	cmdArgs = append(cmdArgs, flatcIOLeadArgs, []STR{(headerVFS).str(), (srcVFS).str()})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	var depRefs []NodeRef
	var foreignDepRefs []NodeRef

	if flatcLDRef != (NodeRef(0)) {
		depRefs = []NodeRef{flatcLDRef}
		foreignDepRefs = []NodeRef{flatcLDRef}
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     strB,
				Env:     env,
			},
		},
		DepRefs:          depRefs,
		Env:              env,
		ForeignDepRefs:   foreignDepRefs,
		Inputs:           InputChunks{{flatcBinary, flatcWrapperVFS, srcVFS}, transitiveImports},
		KV:               KV{P: pkFL, PC: pcLightGreen},
		Outputs:          []VFS{headerVFS, cppVFS, bfbsVFS},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		usesResources:    []string{resourcePatternYMakePython3},
	}

	return emit.emit(node), headerVFS, cppVFS, bfbsVFS
}

func ensureFlatcEmission(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string) FlatcEmission {
	srcVFS := resolveSourceVFS(ctx, instance, srcRel, d.srcDirs)
	key := CodegenOutputKey{
		platform: instance.Platform,
		path:     Build(srcVFS.rel() + ".h"),
	}

	if got, ok := ctx.flatcEmissions[key]; ok {
		return got
	}

	for _, imp := range flatcDirectImportNames(ctx.parsers, srcVFS.rel()) {
		resolved := resolveFlatcImportPath(ctx.fs, srcVFS.rel(), imp)

		if resolved == "" {
			continue
		}

		if moduleSrcRel, ok := flatcResolvedModuleSourceRel(ctx, instance, d, resolved); ok {
			ensureFlatcEmission(ctx, instance, d, moduleSrcRel)
		}
	}

	flatcRes := ctx.toolResult(argContribLibsFlatbuffersFlatc)
	flatcLDRef, flatcBinary := flatcRes.LDRef, *flatcRes.LDPath
	transitiveImports := walkClosureTail(ctx, instance, srcVFS, ModuleCCInputs{})
	flRef, headerVFS, cppVFS, bfbsVFS := EmitFL(instance, srcVFS.rel(), srcVFS, flatcLDRef, flatcBinary, d.flatcFlags, transitiveImports, d.tc, ctx.emit)

	// flatc's INDUCED_DEPS(h+cpp …) — flatbuffers.h + flatbuffers_iter.h, declared
	// in contrib/libs/flatbuffers/flatc/ya.make — ride into both the .h and .cpp
	// closures generically via the flatc GeneratorRef below, so a CC transitively
	// reaching the generated header picks them up (flatbuffers_iter.h was missing
	// from arrow IPC CC inputs without this).
	headerIncludes := flatcDirectGeneratedHeaderIncludes(ctx.parsers, ctx.fs, srcVFS.rel())

	registerBoundGeneratedParsedOutput(ctx, instance, pkFL, headerVFS, headerIncludes, flRef, []NodeRef{flatcLDRef})

	// The flatc tooling, the .fbs source and its transitive imports, plus the
	// flatbuffers runtime header are real inputs of any unit whose include-closure
	// reaches this generated header. Ride them as non-expanded closure leaves of
	// the .h so every consumer picks them up transitively through the cached
	// window, instead of the former per-CC-source rewalk (flatcCCExtraInputs).
	reg := codegenRegForInstance(ctx, instance)
	reg.addClosureLeaf(headerVFS, flatcWrapperVFS)
	reg.addClosureLeaf(headerVFS, srcVFS)
	reg.addClosureLeaf(headerVFS, flatcRuntimeVFS)

	for _, imp := range transitiveImports {
		reg.addClosureLeaf(headerVFS, imp)
	}

	cppIncludes := []IncludeDirective{{kind: includeQuoted, target: internStr(headerVFS.rel())}}

	registerBoundGeneratedParsedOutput(ctx, instance, pkFL, cppVFS, cppIncludes, flRef, []NodeRef{flatcLDRef})
	registerBoundGeneratedParsedOutput(ctx, instance, pkFL, bfbsVFS, nil, flRef, []NodeRef{flatcLDRef})

	out := FlatcEmission{
		flRef:   flRef,
		header:  headerVFS,
		cpp:     cppVFS,
		relPath: srcVFS.rel(),
	}
	ctx.flatcEmissions[key] = out

	return out
}
