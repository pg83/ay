package main

import (
	"strings"
)

// sourceEmit is the emit-product of emitOneSource: a single
// CC/AS/RD/R5/R6/CF/etc. node from one declared source. nil = silently
// skipped (e.g. `.h` headers, deferred-kind sources).
type sourceEmit struct {
	Ref     NodeRef
	OutPath VFS
}

func emitOneSource(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, in ModuleCCInputs, ancestorRebase bool) *sourceEmit {
	if isHeaderSource(srcRel) {
		return nil
	}

	rebaseAncestorSource := ancestorRebase && !strings.Contains(srcRel, "/")

	srcInstance := instance
	if rebaseAncestorSource {
		srcInstance.Path = *d.srcDir
	}

	srcIn := in
	if rebaseAncestorSource {
		srcIn.SrcDir = nil
	}

	switch {
	case strings.HasSuffix(srcRel, ".proto"):
		return emitLibraryProtoSource(ctx, srcInstance, d, srcRel, srcIn)
	case strings.HasSuffix(srcRel, ".fbs"):
		return emitLibraryFlatcSource(ctx, srcInstance, d, srcRel, srcIn)
	case strings.HasSuffix(srcRel, ".rodata"):
		if instance.Platform.ISA != ISAX8664 {
			ThrowFmt("gen: unsupported .rodata platform %s for %q", instance.Platform.ISA, srcRel)
		}

		yasmLDRef, _ := ctx.tool("contrib/tools/yasm")
		srcVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)
		ref, _, outPath := EmitRD(srcInstance, srcRel, srcVFS, yasmLDRef, ctx.emit)

		return &sourceEmit{Ref: ref, OutPath: outPath}
	case strings.HasSuffix(srcRel, ".c"),
		strings.HasSuffix(srcRel, ".cpp"),
		strings.HasSuffix(srcRel, ".cc"),
		strings.HasSuffix(srcRel, ".cxx"):
		srcVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)
		// Primary-first closure: full = [srcVFS, ...headers] (nil when the
		// module has no scanner). Headers-only view is full[1:].
		full := walkClosureRoot(ctx, srcInstance, srcVFS, srcVFS.Rel, srcIn)
		if full != nil {
			srcIn.IncludeInputs = full[1:]
		}
		flatcExtras := flatcCCExtraInputs(ctx, srcIn.IncludeInputs)
		if len(flatcExtras) > 0 {
			srcIn.IncludeInputs = appendVFSUnique(srcIn.IncludeInputs, flatcExtras)
		}
		extras := runtimePy3CCExtraInputs(srcInstance.Path, srcRel)
		if len(extras) > 0 {
			srcIn.IncludeInputs = appendVFSUnique(srcIn.IncludeInputs, extras)
		}
		// With no extras perturbing it, `full` is the exact [primary, ...headers]
		// node.Inputs — hand it to EmitCC to skip the rebuild. An append above
		// reallocated IncludeInputs away from full, so there leave NodeInputs nil
		// and let EmitCC build from inVFS+headers.
		if len(flatcExtras) == 0 && len(extras) == 0 {
			srcIn.NodeInputs = full
		}
		srcIn.ExtraDepRefs = resolveCodegenDepRefsExt(ctx, srcInstance, srcIn.IncludeInputs, []VFS{srcVFS})

		ref, outPath, _ := EmitCC(srcInstance, srcRel, srcVFS, srcIn, ctx.host, ctx.emit)

		return &sourceEmit{Ref: ref, OutPath: outPath}
	case strings.HasSuffix(srcRel, ".S"),
		strings.HasSuffix(srcRel, ".s"),
		strings.HasSuffix(srcRel, ".asm"):
		var yasmRef *NodeRef

		if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
			ldRef, _ := ctx.tool("contrib/tools/yasm")
			yasmRef = &ldRef
		}

		asIn := srcIn
		srcVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)

		// FOR asm ADDINCL paths are assembler-only includes: they feed the
		// AS source scan (so `%include "reg_sizes.asm"` resolves) but never
		// the CC/CXX -I list. Re-merge them into the scan input here; cmd_args
		// composition (yasm hardcodes -I; clang-AS unused by FOR-asm modules)
		// is left untouched.
		scanIn := srcIn
		if len(d.asmAddIncl) > 0 {
			scanIn.AddIncl = mergeDedupVFS(srcIn.AddIncl, d.asmAddIncl)
		}

		asIn.IncludeInputs = walkClosure(ctx, srcInstance, srcVFS, scanIn)
		ref, outPath := EmitAS(srcInstance, srcRel, srcVFS, asIn, yasmRef, ctx.host, ctx.emit)

		return &sourceEmit{Ref: ref, OutPath: outPath}
	case strings.HasSuffix(srcRel, ".rl6"):
		ragelLDRef, ragelBinaryVFS := ctx.tool("contrib/tools/ragel6/bin")

		rl6SourceVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)
		rl6Closure := walkClosure(ctx, srcInstance, rl6SourceVFS, srcIn)
		rl6Closure = filterEnSerializedSiblings(rl6Closure)

		r6Ref, r6Out := EmitR6(srcInstance, srcRel, ragelLDRef, ragelBinaryVFS, srcIn.Ragel6Flags, rl6Closure, ctx.emit)

		var r6Parsed []includeDirective
		if scanner := ctx.scannerFor(srcInstance); scanner != nil {
			r6Parsed = scanner.parsers.sourceParsedBuckets(rl6SourceVFS.Rel).bucket(parsedIncludesHCPP)
		}
		registerGeneratedParsedOutput(ctx, srcInstance, "R6", r6Out, r6Parsed)

		ccSrcRel := strings.TrimPrefix(r6Out.Rel, srcInstance.Path+"/")
		ccIncludeInputs := walkClosure(ctx, srcInstance, r6Out, srcIn)

		ccIn := srcIn
		ccIn.IncludeInputs = ccIncludeInputs
		ccIn.PerSourceCFlags = append(append([]string(nil), srcIn.PerSourceCFlags...), "-Wno-implicit-fallthrough")
		ccIn.ExtraDepRefs = append([]NodeRef{r6Ref}, resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r6Ref)...)

		ccRef, ccOut, _ := EmitCC(srcInstance, ccSrcRel, r6Out, ccIn, ctx.host, ctx.emit)

		return &sourceEmit{Ref: ccRef, OutPath: ccOut}
	case strings.HasSuffix(srcRel, ".y"):
		return emitBisonY(ctx, srcInstance, srcRel, srcIn, srcIn.BisonGenExt)
	case strings.HasSuffix(srcRel, ".ev"):
		evSource := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)
		evRelPath := evSource.Rel

		protocLDRef, protocBinary := ctx.tool(pbProtocModule)
		cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(pbCppStyleguideModule)
		event2cppLDRef, event2cppBinary := ctx.tool(evEvent2cppModule)

		evImports := evTransitiveImports(ctx.parsers, ctx.fs, evRelPath)
		evRef := EmitEV(
			srcInstance, evRelPath,
			cppStyleguideLDRef, protocLDRef, event2cppLDRef,
			cppStyleguideBinary, protocBinary, event2cppBinary,
			nil, evImports, ctx.emit)

		evH := Build(evRelPath + ".pb.h")
		evPbCC := Build(evRelPath + ".pb.cc")

		evKey := codegenOutputKey{platform: srcInstance.Platform}
		evKey.path = evH
		ctx.evOutputs[evKey] = evRef
		evKey.path = evPbCC
		ctx.evOutputs[evKey] = evRef
		if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
			directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, "")
			evExtras := evWitnessExtras(evRelPath, evPbCC)
			evHParsed := make([]includeDirective, 0, len(directImports)+len(protobufRuntimeHeaders)+len(evExtras))
			evHParsed = append(evHParsed, directImports...)
			for _, include := range protobufRuntimeHeaders {
				evHParsed = append(evHParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
			evHParsed = append(evHParsed, evExtras...)
			registerGeneratedParsedOutput(ctx, srcInstance, "EV", evH, evHParsed)
			evCCParsed := make([]includeDirective, 0, 1+len(protobufRuntimeHeaders))
			evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: evH.Rel})
			for _, include := range protobufRuntimeHeaders {
				evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: include.Rel})
			}
			registerGeneratedParsedOutput(ctx, srcInstance, "EV", evPbCC, evCCParsed)
		}

		evPbCCSuffix := srcRel + ".pb.cc"
		ccIn := srcIn
		ccIn.IncludeInputs = walkClosure(ctx, srcInstance, evPbCC, srcIn)
		{
			filtered := make([]VFS, 0, len(ccIn.IncludeInputs))
			for _, in := range ccIn.IncludeInputs {
				if in == evH {
					continue
				}
				filtered = append(filtered, in)
			}
			ccIn.IncludeInputs = filtered
		}
		wireFormatVFS := Source(pbRuntimeBase + "google/protobuf/wire_format.h")
		ccIn.IncludeInputs = append(ccIn.IncludeInputs, wireFormatVFS)
		ccIn.ExtraDepRefs = append([]NodeRef{evRef}, resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, evRef)...)

		ref, outPath, _ := EmitCC(srcInstance, evPbCCSuffix, evPbCC, ccIn, ctx.host, ctx.emit)

		return &sourceEmit{Ref: ref, OutPath: outPath}
	case strings.HasSuffix(srcRel, ".rl"):
		ragel5LDRef, ragel5BinVFS := ctx.tool("contrib/tools/ragel5/ragel")
		rlgenCdLDRef, rlgenCdBinVFS := ctx.tool("contrib/tools/ragel5/rlgen-cd")

		r5Ref, r5TmpOut, r5CppOut := EmitR5(srcInstance, srcRel, ragel5LDRef, rlgenCdLDRef, ragel5BinVFS, rlgenCdBinVFS, ctx.emit)
		_ = r5Ref

		rlSourceVFS := Source(srcInstance.Path + "/" + srcRel)
		registerBoundGeneratedParsedOutput(ctx, srcInstance, "R5", r5TmpOut, nil, r5Ref)
		var r5Parsed []includeDirective
		if scanner := ctx.scannerFor(srcInstance); scanner != nil {
			r5Parsed = scanner.parsers.sourceParsedBuckets(rlSourceVFS.Rel).bucket(parsedIncludesHCPP)
		}
		registerBoundGeneratedParsedOutput(ctx, srcInstance, "R5", r5CppOut, r5Parsed, r5Ref)

		ccSrcRel := strings.TrimPrefix(r5CppOut.Rel, srcInstance.Path+"/")
		ccIn := srcIn
		ccClosure := walkClosure(ctx, srcInstance, r5CppOut, srcIn)
		ccIn.IncludeInputs = append([]VFS{r5TmpOut}, ccClosure...)
		ccIn.PerSourceCFlags = append(append([]string(nil), srcIn.PerSourceCFlags...), "-Wno-implicit-fallthrough")
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r5Ref)
		ccIn.ExtraDepRefs = append([]NodeRef{r5Ref}, ccIn.ExtraDepRefs...)

		ccRef, ccOut, _ := EmitCC(srcInstance, ccSrcRel, r5CppOut, ccIn, ctx.host, ctx.emit)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut}
	case strings.HasSuffix(srcRel, ".h.in"):
		// A generated header declared in SRCS is not compiled in its declaring
		// module — ymake realizes it in the module that #includes it. Register
		// it as a deferred CF: the scanner sees the output so consumer closures
		// resolve it, but the CF node is emitted by the first consumer (with
		// that consumer's module_dir) in resolveCodegenDepRefsExt. Returning nil
		// keeps the header out of the declaring module's archive.
		inSourceVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, inSourceVFS, srcIn)
		cfgVars := buildCFGVars(ctx.fs, inSourceVFS.Rel, srcIn.SetVars, srcIn.DefaultVars)
		cfOut := Build(srcInstance.Path + "/" + strings.TrimSuffix(srcRel, ".in"))

		parsed := []includeDirective{
			{kind: includeQuoted, target: inSourceVFS.Rel},
			{kind: includeQuoted, target: configureFilePyVFS.Rel},
		}
		parsed = append(parsed, cfIncludeDirectives(ctx.parsers, inSourceVFS.Rel)...)
		registerDeferredCF(ctx, srcInstance, cfOut, parsed, &deferredCF{
			instance:      srcInstance,
			srcVFS:        inSourceVFS,
			outVFS:        cfOut,
			cfgVars:       cfgVars,
			includeInputs: srcIn.IncludeInputs,
		})

		return nil
	case strings.HasSuffix(srcRel, ".cpp.in"),
		strings.HasSuffix(srcRel, ".c.in"):
		inSourceVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, inSourceVFS, srcIn)
		cfgVars := buildCFGVars(ctx.fs, inSourceVFS.Rel, srcIn.SetVars, srcIn.DefaultVars)
		cfOut := Build(srcInstance.Path + "/" + strings.TrimSuffix(srcRel, ".in"))
		cfRef, cfOut := EmitCF(srcInstance, inSourceVFS, cfOut, cfgVars, srcIn.IncludeInputs, srcInstance.Path, ctx.emit)

		registerBoundGeneratedParsedOutput(ctx, srcInstance, "CF", cfOut, []includeDirective{
			{kind: includeQuoted, target: inSourceVFS.Rel},
			{kind: includeQuoted, target: configureFilePyVFS.Rel},
		}, cfRef)

		ccSrcRel := strings.TrimPrefix(cfOut.Rel, srcInstance.Path+"/")
		ccIn := srcIn
		ccIn.IncludeInputs = walkClosure(ctx, srcInstance, cfOut, srcIn)
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, cfRef)
		ccIn.ExtraDepRefs = append([]NodeRef{cfRef}, ccIn.ExtraDepRefs...)

		ccRef, ccOut, _ := EmitCC(srcInstance, ccSrcRel, cfOut, ccIn, ctx.host, ctx.emit)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut}
	}

	if isSkippedSource(srcRel) {
		return nil
	}

	ThrowFmt("gen: %s: unsupported source extension in %q", instance.Path, srcRel)
	return nil
}

func emitLibraryProtoSource(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, in ModuleCCInputs) *sourceEmit {
	pb := emitProtoPB(ctx, instance, d, srcRel, protoPBConfig{})

	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx, instance, pb.pbCC, in)
	ccIn.ExtraDepRefs = append([]NodeRef{pb.pbRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, pb.pbRef)...)

	ccSrcRel := strings.TrimPrefix(pb.pbCC.Rel, instance.Path+"/")
	ccRef, ccOut, _ := EmitCC(instance, ccSrcRel, pb.pbCC, ccIn, ctx.host, ctx.emit)

	return &sourceEmit{Ref: ccRef, OutPath: ccOut}
}

func emitLibraryFlatcSource(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, in ModuleCCInputs) *sourceEmit {
	fl := ensureFlatcEmission(ctx, instance, d, srcRel)

	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx, instance, fl.cpp, in)
	if extras := flatcCCExtraInputs(ctx, ccIn.IncludeInputs); len(extras) > 0 {
		ccIn.IncludeInputs = appendVFSUnique(ccIn.IncludeInputs, extras)
	}
	ccIn.ExtraDepRefs = append([]NodeRef{fl.flRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, fl.flRef)...)

	ccSrcRel := strings.TrimPrefix(fl.cpp.Rel, instance.Path+"/")
	ccRef, ccOut, _ := EmitCC(instance, ccSrcRel, fl.cpp, ccIn, ctx.host, ctx.emit)

	return &sourceEmit{Ref: ccRef, OutPath: ccOut}
}
