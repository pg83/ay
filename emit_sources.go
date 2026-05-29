package main

import (
	"strings"
)

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

		full := walkClosureRoot(ctx, srcInstance, srcVFS, srcVFS.Rel(), srcIn)
		if full != nil {
			srcIn.IncludeInputs = full[1:]
		}
		// AUTO COPY entries leave both the $(S) source and the $(B) destination
		// on disk; upstream tracks both as CC inputs (the source is what the
		// scanner reads — the copy is byte-identical content — but the $(B)
		// destination is still cache-keyed via the include path because it
		// physically exists at $(B)/<modpath>/<file>). Closure walks resolve
		// only the source; pair it with the dst here for AUTO entries of this
		// module so e.g. mkql_builtins.h's $(B)-copy enters mkql_builtins.cpp.o
		// inputs alongside the $(S) original.
		autoExtras := autoCopyDstExtras(srcInstance.Path, d, srcIn.IncludeInputs, srcVFS)
		if len(autoExtras) > 0 {
			srcIn.IncludeInputs = appendVFSUnique(srcIn.IncludeInputs, autoExtras)
		}
		wcExtras := withContextSourceExtras(codegenRegForInstance(ctx, srcInstance), srcInstance.Path, d, srcIn.IncludeInputs, srcVFS)
		if len(wcExtras) > 0 {
			srcIn.IncludeInputs = appendVFSUnique(srcIn.IncludeInputs, wcExtras)
		}
		flatcExtras := flatcCCExtraInputs(ctx, srcIn.IncludeInputs)
		if len(flatcExtras) > 0 {
			srcIn.IncludeInputs = appendVFSUnique(srcIn.IncludeInputs, flatcExtras)
		}
		extras := runtimePy3CCExtraInputs(srcInstance.Path, srcRel)
		if len(extras) > 0 {
			srcIn.IncludeInputs = appendVFSUnique(srcIn.IncludeInputs, extras)
		}

		// Fast-path: when no extras were appended, IncludeInputs == full[1:]
		// (shares the same backing slice), so NodeInputs = full lets EmitCC
		// emit the full input array directly with no extra allocation. When
		// any extras are present, IncludeInputs has been reallocated past
		// full[1:] and we leave NodeInputs nil so EmitCC rebuilds the full
		// list from inVFS + IncludeInputs.
		if len(autoExtras) == 0 && len(wcExtras) == 0 && len(flatcExtras) == 0 && len(extras) == 0 {
			srcIn.NodeInputs = full
		}
		srcIn.ExtraDepRefs = resolveCodegenDepRefsExt(ctx, srcInstance, srcIn.IncludeInputs, []VFS{srcVFS})

		ref, outPath, _ := EmitCC(srcInstance, srcRel, srcVFS, srcIn, ctx.host, ctx.emit)

		return &sourceEmit{Ref: ref, OutPath: outPath}
	case strings.HasSuffix(srcRel, ".S"),
		strings.HasSuffix(srcRel, ".s"),
		strings.HasSuffix(srcRel, ".asm"):
		asIn := srcIn
		srcVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)

		scanIn := srcIn
		if len(d.asmAddIncl) > 0 {
			// `ADDINCL(FOR asm X)` (yatool/build/conf/proto.conf:104-106
			// _ORDER_ADDINCL routes the FOR asm bucket via ADDINCL) feeds
			// the assembler's -I list AND the include scanner's search
			// path. Without it the .asm's `%include "X/..."` resolves
			// against nothing — and yasm's command misses `-I X` entirely,
			// diverging from REF (e.g. yt/yt/core/misc/isa_crc64 needs
			// -I=$(S)/yt/yt/core/misc/isa_crc64/include for reg_sizes.asm).
			scanIn.AddIncl = mergeDedupVFS(srcIn.AddIncl, d.asmAddIncl)
			asIn.AddIncl = scanIn.AddIncl
		}

		asIn.IncludeInputs = walkClosure(ctx, srcInstance, srcVFS, scanIn)

		if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
			yasmLD, _ := ctx.tool("contrib/tools/yasm")
			ref, outPath := emitASYasm(srcInstance, srcRel, srcVFS, asIn, yasmLD, ctx.emit)

			return &sourceEmit{Ref: ref, OutPath: outPath}
		}

		ref, outPath := EmitAS(srcInstance, srcRel, srcVFS, asIn, ctx.host, ctx.emit)

		return &sourceEmit{Ref: ref, OutPath: outPath}
	case strings.HasSuffix(srcRel, ".rl6"):
		ragelLDRef, ragelBinaryVFS := ctx.tool("contrib/tools/ragel6/bin")

		rl6SourceVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)
		rl6Closure := walkClosure(ctx, srcInstance, rl6SourceVFS, srcIn)
		rl6Closure = filterEnSerializedSiblings(rl6Closure)

		r6Ref, r6Out := EmitR6(srcInstance, srcRel, ragelLDRef, ragelBinaryVFS, srcIn.Ragel6Flags, rl6Closure, ctx.emit)

		var r6Parsed []includeDirective
		if scanner := ctx.scannerFor(srcInstance); scanner != nil {
			r6Parsed = scanner.parsers.sourceParsedBuckets(rl6SourceVFS).bucket(parsedIncludesHCPP)
		}
		registerGeneratedParsedOutput(ctx, srcInstance, "R6", r6Out, r6Parsed)

		ccSrcRel := strings.TrimPrefix(r6Out.Rel(), srcInstance.Path+"/")
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
		evRelPath := evSource.Rel()

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
				evHParsed = append(evHParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
			}
			evHParsed = append(evHParsed, evExtras...)
			registerGeneratedParsedOutput(ctx, srcInstance, "EV", evH, evHParsed)
			evCCParsed := make([]includeDirective, 0, 1+len(protobufRuntimeHeaders))
			evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: internString(evH.Rel())})
			for _, include := range protobufRuntimeHeaders {
				evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: internString(include.Rel())})
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
			r5Parsed = scanner.parsers.sourceParsedBuckets(rlSourceVFS).bucket(parsedIncludesHCPP)
		}
		registerBoundGeneratedParsedOutput(ctx, srcInstance, "R5", r5CppOut, r5Parsed, r5Ref)

		ccSrcRel := strings.TrimPrefix(r5CppOut.Rel(), srcInstance.Path+"/")
		ccIn := srcIn
		ccClosure := walkClosure(ctx, srcInstance, r5CppOut, srcIn)
		ccIn.IncludeInputs = append([]VFS{r5TmpOut}, ccClosure...)
		ccIn.PerSourceCFlags = append(append([]string(nil), srcIn.PerSourceCFlags...), "-Wno-implicit-fallthrough")
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r5Ref)
		ccIn.ExtraDepRefs = append([]NodeRef{r5Ref}, ccIn.ExtraDepRefs...)

		ccRef, ccOut, _ := EmitCC(srcInstance, ccSrcRel, r5CppOut, ccIn, ctx.host, ctx.emit)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut}
	case strings.HasSuffix(srcRel, ".h.in"):

		inSourceVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDir)
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, inSourceVFS, srcIn)
		cfgVars := buildCFGVars(ctx.fs, inSourceVFS.Rel(), srcIn.SetVars, srcIn.DefaultVars)
		cfOut := Build(srcInstance.Path + "/" + strings.TrimSuffix(srcRel, ".in"))

		parsed := []includeDirective{
			{kind: includeQuoted, target: internString(inSourceVFS.Rel())},
			{kind: includeQuoted, target: internString(configureFilePyVFS.Rel())},
		}
		parsed = append(parsed, cfIncludeDirectives(ctx.parsers, inSourceVFS.Rel())...)
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
		cfgVars := buildCFGVars(ctx.fs, inSourceVFS.Rel(), srcIn.SetVars, srcIn.DefaultVars)
		cfOut := Build(srcInstance.Path + "/" + strings.TrimSuffix(srcRel, ".in"))
		cfRef, cfOut := EmitCF(srcInstance, inSourceVFS, cfOut, cfgVars, srcIn.IncludeInputs, srcInstance.Path, cfModuleTag(d, srcInstance), ctx.emit)

		registerBoundGeneratedParsedOutput(ctx, srcInstance, "CF", cfOut, []includeDirective{
			{kind: includeQuoted, target: internString(inSourceVFS.Rel())},
			{kind: includeQuoted, target: internString(configureFilePyVFS.Rel())},
		}, cfRef)

		ccSrcRel := strings.TrimPrefix(cfOut.Rel(), srcInstance.Path+"/")
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
	pb := emitProtoPB(ctx, instance, d, srcRel, protoPBConfig{}, in.PeerProtoAddInclGlobal)

	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx, instance, pb.pbCC, in)
	ccIn.ExtraDepRefs = append([]NodeRef{pb.pbRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, pb.pbRef)...)

	ccSrcRel := strings.TrimPrefix(pb.pbCC.Rel(), instance.Path+"/")
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

	ccSrcRel := strings.TrimPrefix(fl.cpp.Rel(), instance.Path+"/")
	ccRef, ccOut, _ := EmitCC(instance, ccSrcRel, fl.cpp, ccIn, ctx.host, ctx.emit)

	return &sourceEmit{Ref: ccRef, OutPath: ccOut}
}
