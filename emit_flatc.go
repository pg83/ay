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

func flatcDirectImportNames(pm *IncludeParserManager, srcRel string) []string {
	direct := pm.sourceParsedBuckets(source(srcRel), nil).bucket(parsedIncludesLocal)

	if len(direct) == 0 {
		return nil
	}

	out := make([]string, 0, len(direct))

	for _, d := range direct {
		out = append(out, d.target.string())
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

func emitFL(instance ModuleInstance, srcRel string, srcVFS VFS, flatcLDRef NodeRef, flatcBinary VFS, flatcFlags []ARG, transitiveImports []VFS, tc ModuleToolchain, emit Emitter) (NodeRef, VFS, VFS, VFS) {
	na := emit.nodeArenas()

	headerVFS := build(srcRel + ".h")
	cppVFS := build(srcRel + ".cpp")
	bfbsVFS := build(strings.TrimSuffix(srcRel, ".fbs") + ".bfbs")

	cmdArgs := na.chunkList(na.strList(tc.Python3, (flatcWrapperVFS).str(), (flatcBinary).str()), flatcConstFlags)

	if len(flatcFlags) > 0 {
		cmdArgs = append(cmdArgs, appendArgStr(nil, flatcFlags))
	}

	cmdArgs = append(cmdArgs, flatcIOLeadArgs, []STR{(headerVFS).str(), (srcVFS).str()})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: strB,
			Env: env}),
		Env:              env,
		ForeignDepRefs:   depRefs(flatcLDRef),
		Inputs:           na.inputList(na.vfsList(flatcBinary, flatcWrapperVFS, srcVFS), transitiveImports),
		KV:               KV{P: pkFL, PC: pcLightGreen},
		Outputs:          na.vfsList(headerVFS, cppVFS, bfbsVFS),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Resources:        usesPython3,
	}

	return emit.emit(node), headerVFS, cppVFS, bfbsVFS
}

// emitFlatcProducer emits the flatc node for one .fbs and registers its outputs
// (.h/.cpp/.bfbs) in the codegen registry. Run in a pre-pass over all of a
// module's .fbs before any flatc CC closure is walked, so a .fbs importing a
// sibling resolves the sibling's generated .h — the same two-phase shape proto
// uses in emitCPPProtoSrcs (register every pb.h, then compile).
func emitFlatcProducer(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string) {
	srcVFS := resolveSourceVFS(ctx, instance, srcRel, d.srcDirs)

	flatcRes := ctx.toolResult(argContribLibsFlatbuffersFlatc)
	flatcLDRef, flatcBinary := flatcRes.LDRef, *flatcRes.LDPath
	transitiveImports := walkClosureTail(ctx.scannerFor(instance), srcVFS, newScanContext(ctx.parsers, nil, nil, includeScannerBasePaths(), instance.Path.rel()))
	flRef, headerVFS, cppVFS, bfbsVFS := emitFL(instance, srcVFS.rel(), srcVFS, flatcLDRef, flatcBinary, d.flatcFlags, transitiveImports, d.tc, ctx.emit)

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
}

func emitLibraryFlatcSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	// The producer was emitted+registered in the pre-pass (emitFlatcProducer);
	// take its ref from the codegen registry and compile the generated .cpp.
	cppVFS := build(resolveSourceVFS(ctx, instance, srcRel, d.srcDirs).rel() + ".cpp")
	flRef := codegenRegForInstance(ctx, instance).lookup(cppVFS).ProducerRef

	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), cppVFS, in.ScanCfg)

	ccIn.ExtraDepRefs = append([]NodeRef{flRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, flRef)...)
	ccSrcRel := strings.TrimPrefix(cppVFS.rel(), instance.Path.rel()+"/")
	ccRef, ccOut, _ := emitCC(instance, ccSrcRel, cppVFS, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}
