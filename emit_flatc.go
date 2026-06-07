package main

import (
	"path/filepath"
	"strings"
)

const flatcModule = "contrib/libs/flatbuffers/flatc"

type flatcEmission struct {
	flRef   NodeRef
	header  VFS
	cpp     VFS
	relPath string
}

func flatcDirectImportNames(pm *includeParserManager, srcRel string) []string {
	direct := pm.sourceParsedBuckets(Source(srcRel)).bucket(parsedIncludesLocal)

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
		if fs.IsFile(srcRootVFS, cand) {
			return cand
		}
	}

	return ""
}

func flatcTransitiveImports(pm *includeParserManager, fs FS, srcRel string) []VFS {
	rootImports := flatcDirectImportNames(pm, srcRel)

	if len(rootImports) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	scanned := map[string]struct{}{}
	imports := make([]VFS, 0, len(rootImports))

	var walk func(string)
	walk = func(rel string) {
		if _, done := scanned[rel]; done {
			return
		}

		scanned[rel] = struct{}{}

		for _, imp := range flatcDirectImportNames(pm, rel) {
			resolved := resolveFlatcImportPath(fs, rel, imp)

			if resolved == "" {
				continue
			}

			if _, ok := seen[resolved]; !ok {
				seen[resolved] = struct{}{}
				imports = append(imports, Source(resolved))
			}

			walk(resolved)
		}
	}

	for _, imp := range rootImports {
		resolved := resolveFlatcImportPath(fs, srcRel, imp)

		if resolved == "" {
			continue
		}

		if _, ok := seen[resolved]; !ok {
			seen[resolved] = struct{}{}
			imports = append(imports, Source(resolved))
		}

		walk(resolved)
	}

	return imports
}

func flatcDirectGeneratedHeaderIncludes(pm *includeParserManager, fs FS, srcRel string) []includeDirective {
	direct := flatcDirectImportNames(pm, srcRel)

	if len(direct) == 0 {
		return nil
	}

	out := make([]includeDirective, 0, len(direct))

	for _, imp := range direct {
		resolved := resolveFlatcImportPath(fs, srcRel, imp)

		if resolved == "" {
			continue
		}

		out = append(out, includeDirective{
			kind:   includeQuoted,
			target: internStr(resolved + ".h"),
		})
	}

	return out
}

func flatcResolvedModuleSourceRel(ctx *genCtx, instance ModuleInstance, d *moduleData, resolvedRel string) (string, bool) {
	candidates := append([]string(nil), d.srcs...)
	candidates = append(candidates, d.globalSrcs...)

	for _, srcRel := range candidates {
		if !strings.HasSuffix(srcRel, ".fbs") {
			continue
		}

		if resolveSourceVFS(ctx, instance, srcRel, d.srcDir).Rel() == resolvedRel {
			return srcRel, true
		}
	}

	return "", false
}

func EmitFL(instance ModuleInstance, srcRel string, srcVFS VFS, flatcLDRef NodeRef, flatcBinary VFS, flatcFlags []ARG, transitiveImports []VFS, emit Emitter) (NodeRef, VFS, VFS, VFS) {
	headerVFS := Build(srcRel + ".h")
	cppVFS := Build(srcRel + ".cpp")
	bfbsVFS := Build(strings.TrimSuffix(srcRel, ".fbs") + ".bfbs")

	cmdArgs := []ANY{
		internAny(instance.Platform.Tools.Python3),
		vfsAny(flatcWrapperVFS),
		vfsAny(flatcBinary),
		anyNoWarnings,
		anyCpp,
		anyKeepPrefix,
		anyGenMutable,
		anySchema,
		anyB2,
		anyGenObjectApi,
		anyFilenameSuffix,
		anyFbs,
	}
	cmdArgs = appendArgAny(cmdArgs, flatcFlags)
	cmdArgs = append(cmdArgs,
		anyI, anyB,
		anyI, anyS,
		anyDashO, vfsAny(headerVFS),
		vfsAny(srcVFS),
	)

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}
	inputs := []VFS{flatcBinary, flatcWrapperVFS, srcVFS}
	inputs = append(inputs, transitiveImports...)

	var depRefs []NodeRef
	var foreignDepRefs []NodeRef

	if flatcLDRef != (NodeRef(0)) {
		depRefs = []NodeRef{flatcLDRef}
		foreignDepRefs = []NodeRef{flatcLDRef}
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(B)",
				Env:     env,
			},
		},
		DepRefs:          depRefs,
		Env:              env,
		ForeignDepRefs:   foreignDepRefs,
		Inputs:           inputs,
		KV:               KV{P: pkFL, PC: pcLightGreen},
		Outputs:          []VFS{headerVFS, cppVFS, bfbsVFS},
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		Tags:             instance.Platform.Tags,
		TargetProperties: TargetProperties{ModuleDir: instance.Path},
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform)), headerVFS, cppVFS, bfbsVFS
}

func ensureFlatcEmission(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string) flatcEmission {
	srcVFS := resolveSourceVFS(ctx, instance, srcRel, d.srcDir)
	key := codegenOutputKey{
		platform: instance.Platform,
		path:     Build(srcVFS.Rel() + ".h"),
	}

	if got, ok := ctx.flatcEmissions[key]; ok {
		return got
	}

	for _, imp := range flatcDirectImportNames(ctx.parsers, srcVFS.Rel()) {
		resolved := resolveFlatcImportPath(ctx.fs, srcVFS.Rel(), imp)

		if resolved == "" {
			continue
		}

		if moduleSrcRel, ok := flatcResolvedModuleSourceRel(ctx, instance, d, resolved); ok {
			ensureFlatcEmission(ctx, instance, d, moduleSrcRel)
		}
	}

	flatcRes := ctx.toolResult(flatcModule)
	flatcLDRef, flatcBinary := flatcRes.LDRef, *flatcRes.LDPath
	transitiveImports := flatcTransitiveImports(ctx.parsers, ctx.fs, srcVFS.Rel())
	flRef, headerVFS, cppVFS, bfbsVFS := EmitFL(instance, srcVFS.Rel(), srcVFS, flatcLDRef, flatcBinary, d.flatcFlags, transitiveImports, ctx.emit)

	// Upstream's flatc PROGRAM declares INDUCED_DEPS(h+cpp …) covering both
	// the .h and the .cpp it emits — see contrib/libs/flatbuffers/flatc/ya.make
	// listing flatbuffers.h + flatbuffers_iter.h. Both generated outputs must
	// carry the induced deps in their parsed-include map so CC compiles
	// transitively pulling the .h pick them up too (without this,
	// flatbuffers_iter.h is missing from arrow IPC CC inputs and similar).
	headerIncludes := flatcDirectGeneratedHeaderIncludes(ctx.parsers, ctx.fs, srcVFS.Rel())

	for _, dep := range flatcRes.InducedDeps {
		headerIncludes = append(headerIncludes, includeDirective{kind: includeQuoted, target: internStr(dep)})
	}

	registerBoundGeneratedParsedOutput(ctx, instance, "FL", headerVFS, headerIncludes, flRef)

	// The flatc tooling, the .fbs source and its transitive imports, plus the
	// flatbuffers runtime header are real inputs of any unit whose include-closure
	// reaches this generated header. Ride them as non-expanded closure leaves of
	// the .h so every consumer picks them up transitively through the cached
	// window, instead of the former per-CC-source rewalk (flatcCCExtraInputs).
	reg := codegenRegForInstance(ctx, instance)
	reg.AddClosureLeaf(headerVFS, flatcWrapperVFS)
	reg.AddClosureLeaf(headerVFS, srcVFS)
	reg.AddClosureLeaf(headerVFS, flatcRuntimeVFS)

	for _, imp := range transitiveImports {
		reg.AddClosureLeaf(headerVFS, imp)
	}

	cppIncludes := make([]includeDirective, 0, 1+len(flatcRes.InducedDeps))
	cppIncludes = append(cppIncludes, includeDirective{kind: includeQuoted, target: internStr(headerVFS.Rel())})

	for _, dep := range flatcRes.InducedDeps {
		cppIncludes = append(cppIncludes, includeDirective{kind: includeQuoted, target: internStr(dep)})
	}

	registerBoundGeneratedParsedOutput(ctx, instance, "FL", cppVFS, cppIncludes, flRef)
	registerBoundGeneratedParsedOutput(ctx, instance, "FL", bfbsVFS, nil, flRef)

	out := flatcEmission{
		flRef:   flRef,
		header:  headerVFS,
		cpp:     cppVFS,
		relPath: srcVFS.Rel(),
	}
	ctx.flatcEmissions[key] = out

	return out
}
