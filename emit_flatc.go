package main

import (
	"path/filepath"
	"strings"
)

// flatcConstFlags / flatcIOLeadArgs are the constant spans around the module's
// FLATC_FLAGS; the per-node remainder is [<header>, <src>].
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

// flatc64ConstFlags / flatc64IOLeadArgs are the FL64 counterparts: no
// --gen-object-api, suffix .fbs64, and include pair in order opposite to FL.
var flatc64ConstFlags = []STR{
	argNoWarnings.str(),
	argCpp.str(),
	argKeepPrefix.str(),
	argGenMutable.str(),
	argSchema.str(),
	argB2.str(),
	argFilenameSuffix.str(),
	argFbs64.str(),
}

var flatc64IOLeadArgs = []STR{
	argI.str(), argS.str(),
	argI.str(), argB.str(),
	argDashO.str(),
}

var flatcVariantFL = flatcVariant{
	toolArg:    argContribLibsFlatbuffersFlatc,
	constFlags: flatcConstFlags,
	ioLeadArgs: flatcIOLeadArgs,
	procKind:   pkFL,
	srcExt:     ".fbs",
	bfbsExt:    ".bfbs",
	runtimeVFS: flatcRuntimeVFS,
}

var flatcVariantFL64 = flatcVariant{
	toolArg:    argContribLibsFlatbuffers64Flatc,
	constFlags: flatc64ConstFlags,
	ioLeadArgs: flatc64IOLeadArgs,
	procKind:   pkFL64,
	srcExt:     ".fbs64",
	bfbsExt:    ".bfbs64",
	runtimeVFS: flatc64RuntimeVFS,
}

// flatcVariant captures the constant differences between FL (.fbs) and FL64
// (.fbs64). The producer pre-pass picks the variant by source extension; the rest
// is shared.
type flatcVariant struct {
	toolArg    ARG
	constFlags []STR
	ioLeadArgs []STR
	procKind   ProcKind
	srcExt     string
	bfbsExt    string
	runtimeVFS VFS
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

func emitFL(instance ModuleInstance, srcRel string, srcVFS VFS, flatcLDRef NodeRef, flatcBinary VFS, flatcFlags []ARG, transitiveImports []VFS, moduleTag STR, tc ModuleToolchain, emit Emitter, v *flatcVariant, genDeps []NodeRef) (NodeRef, VFS, VFS, VFS) {
	na := emit.nodeArenas()

	headerVFS := build(srcRel + ".h")
	cppVFS := build(srcRel + ".cpp")
	bfbsVFS := build(strings.TrimSuffix(srcRel, v.srcExt) + v.bfbsExt)

	cmdArgs := na.chunkList(na.strList(tc.Python3, (flatcWrapperVFS).str(), (flatcBinary).str()), v.constFlags)

	if len(flatcFlags) > 0 {
		cmdArgs = append(cmdArgs, appendArgStr(nil, flatcFlags))
	}

	cmdArgs = append(cmdArgs, v.ioLeadArgs, []STR{(headerVFS).str(), (srcVFS).str()})

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: strB,
			Env: env}),
		Env:              env,
		DepRefs:          genDeps,
		ForeignDepRefs:   depRefs(flatcLDRef),
		Inputs:           na.inputList(na.vfsList(flatcBinary, flatcWrapperVFS, srcVFS), transitiveImports),
		KV:               KV{P: v.procKind, PC: pcLightGreen},
		Outputs:          na.vfsList(headerVFS, cppVFS, bfbsVFS),
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel(), ModuleTag: moduleTag},
		Resources:        usesPython3,
	}

	return emit.emit(node), headerVFS, cppVFS, bfbsVFS
}

// emitFlatcProducer emits the flatc node for one .fbs and registers its
// .h/.cpp/.bfbs outputs. Runs in a pre-pass before any CC closure is walked, so a
// .fbs importing a sibling resolves the sibling's generated .h. genDeps are extra
// producer deps (nil for a checked-in source .fbs).
func emitFlatcProducer(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcVFS VFS, v *flatcVariant, genDeps []NodeRef) {
	flatcRes := ctx.toolResult(v.toolArg)
	flatcLDRef, flatcBinary := flatcRes.LDRef, *flatcRes.LDPath
	transitiveImports := walkClosureTail(ctx.scannerFor(instance), srcVFS, newScanContext(ctx.parsers, nil, nil, includeScannerBasePaths(), instance.Path.rel()))
	flRef, headerVFS, cppVFS, bfbsVFS := emitFL(instance, srcVFS.rel(), srcVFS, flatcLDRef, flatcBinary, d.flatcFlags, transitiveImports, moduleCCTag(d.moduleStmt.Name), d.tc, ctx.emit, v, genDeps)

	// flatc's induced deps ride into both closures via the flatc GeneratorRef below.
	headerIncludes := flatcDirectGeneratedHeaderIncludes(ctx.parsers, ctx.fs, srcVFS.rel())

	registerBoundGeneratedParsedOutput(ctx, instance, v.procKind, headerVFS, headerIncludes, flRef, []NodeRef{flatcLDRef})

	// The flatc tooling, .fbs source and imports, and runtime header are real inputs
	// of any unit reaching this header. Ride them as closure leaves of the .h.
	reg := codegenRegForInstance(ctx, instance)
	reg.addClosureLeaf(headerVFS, flatcWrapperVFS)

	// A checked-in source .fbs rides to consumers as a closure leaf of its .fbs.h.
	// A generated .fbs is reached via the producer dep edge, never a C++ include, so
	// it must NOT be spliced into consumers' windows.
	if srcVFS.isSource() {
		reg.addClosureLeaf(headerVFS, srcVFS)
	}

	reg.addClosureLeaf(headerVFS, v.runtimeVFS)

	for _, imp := range transitiveImports {
		reg.addClosureLeaf(headerVFS, imp)
	}

	cppIncludes := []IncludeDirective{{kind: includeQuoted, target: internStr(headerVFS.rel())}}

	registerBoundGeneratedParsedOutput(ctx, instance, v.procKind, cppVFS, cppIncludes, flRef, []NodeRef{flatcLDRef})
	registerBoundGeneratedParsedOutput(ctx, instance, v.procKind, bfbsVFS, nil, flRef, []NodeRef{flatcLDRef})
}

func emitLibraryFlatcSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	// Producer was emitted+registered in the pre-pass; take its ref from the
	// codegen registry and compile the generated .cpp.
	cppVFS := build(resolveSourceVFS(ctx, instance, srcRel, d.srcDirs).rel() + ".cpp")

	return emitFlatcCppCompile(ctx, instance, cppVFS, in)
}

// emitFlatcCppCompile compiles a flatc-generated .fbs.cpp (its FL producer was
// already emitted+registered). Shared by the source .fbs path and the
// RUN_PROGRAM-generated .fbs bridge.
func emitFlatcCppCompile(ctx *GenCtx, instance ModuleInstance, cppVFS VFS, in ModuleCCInputs) *SourceEmit {
	flRef := codegenRegForInstance(ctx, instance).lookup(cppVFS).ProducerRef

	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx.scannerFor(instance), cppVFS, in.ScanCfg)

	ccIn.ExtraDepRefs = append([]NodeRef{flRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, flRef)...)
	ccSrcRel := strings.TrimPrefix(cppVFS.rel(), instance.Path.rel()+"/")
	ccRef, ccOut, _ := emitCC(instance, ccSrcRel, cppVFS, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}
