package main

import (
	"strings"
)

type sourceEmit struct {
	Ref     NodeRef
	OutPath VFS
}

func emitOneSource(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, in ModuleCCInputs) *sourceEmit {
	if isHeaderSource(srcRel) {
		return nil
	}

	srcInstance := instance
	srcIn := in

	switch {
	case strings.HasSuffix(srcRel, ".proto"):
		return emitLibraryProtoSource(ctx, srcInstance, d, srcRel, srcIn)
	case strings.HasSuffix(srcRel, ".fbs"):
		return emitLibraryFlatcSource(ctx, srcInstance, d, srcRel, srcIn)
	case strings.HasSuffix(srcRel, ".rodata"):
		if instance.Platform.ISA != ISAX8664 {
			ThrowFmt("gen: unsupported .rodata platform %s for %q", instance.Platform.ISA, srcRel)
		}

		yasmLDRef, _ := ctx.tool(argContribToolsYasm)
		srcVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDirs)
		ref, _, outPath := EmitRD(srcInstance, srcRel, srcVFS, yasmLDRef, srcIn.TC, ctx.emit)
		return &sourceEmit{Ref: ref, OutPath: outPath}
	case strings.HasSuffix(srcRel, ".c"),
		strings.HasSuffix(srcRel, ".cpp"),
		strings.HasSuffix(srcRel, ".cc"),
		strings.HasSuffix(srcRel, ".cxx"):
		srcVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDirs)

		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, srcVFS, srcIn)

		srcIn.ExtraDepRefs = resolveCodegenDepRefsExt(ctx, srcInstance, srcIn.IncludeInputs, []VFS{srcVFS})
		ref, outPath, _ := EmitCC(srcInstance, srcRel, srcVFS, srcIn, ctx.host, ctx.emit)
		return &sourceEmit{Ref: ref, OutPath: outPath}
	case strings.HasSuffix(srcRel, ".S"),
		strings.HasSuffix(srcRel, ".s"),
		strings.HasSuffix(srcRel, ".asm"):
		asIn := srcIn
		srcVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDirs)

		scanIn := srcIn

		if len(d.asmAddIncl) > 0 {
			// `ADDINCL(FOR asm X)` (yatool/build/conf/proto.conf:104-106
			// _ORDER_ADDINCL routes the FOR asm bucket via ADDINCL) feeds
			// the assembler's -I list AND the include scanner's search
			// path. Without it the .asm's `%include "X/..."` resolves
			// against nothing — and yasm's command misses `-I X` entirely,
			// diverging from REF (e.g. yt/yt/core/misc/isa_crc64 needs
			// -I=$(S)/yt/yt/core/misc/isa_crc64/include for reg_sizes.asm).
			scanIn.AddIncl = dedupVFS(srcIn.AddIncl, d.asmAddIncl)
			asIn.AddIncl = scanIn.AddIncl
		}

		asIn.IncludeInputs = walkClosure(ctx, srcInstance, srcVFS, scanIn)

		if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
			yasmLD, _ := ctx.tool(argContribToolsYasm)
			ref, outPath := emitASYasm(srcInstance, srcRel, srcVFS, asIn, yasmLD, ctx.emit)
			return &sourceEmit{Ref: ref, OutPath: outPath}
		}

		ref, outPath := EmitAS(srcInstance, srcRel, srcVFS, asIn, ctx.host, ctx.emit)
		return &sourceEmit{Ref: ref, OutPath: outPath}
	case strings.HasSuffix(srcRel, ".rl6"):
		ragelLDRef, ragelBinaryVFS := ctx.tool(argContribToolsRagel6)

		rl6SourceVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDirs)
		r6Out := ragel6OutVFS(srcInstance, srcRel)

		var r6Parsed []includeDirective

		if scanner := ctx.scannerFor(srcInstance); scanner != nil {
			r6Parsed = scanner.parsers.sourceParsedBuckets(rl6SourceVFS, nil).bucket(parsedIncludesCpp)
		}

		// Register the generated cpp's induced includes (self-include + the
		// .rl6's C/C++ directives) BEFORE walking, so ONE window serves both
		// nodes: the induced directives pull the C closure, and the .rl6's own
		// walkable edges — its ragel-native %includes — pull the natively-
		// included ragel files WITHOUT their C headers (upstream
		// TRagelIncludeProcessor keeps native deps and ParsedIncls apart).
		registerGeneratedParsedOutput(ctx, srcInstance, pkR6, r6Out, r6Parsed, []NodeRef{ragelLDRef})

		window := walkClosure(ctx, srcInstance, r6Out, srcIn)

		// The ragel compiler only reads source files (the .rl6 source + any
		// natively-included .rl6 files + C/C++ headers it parses). Build-generated
		// files (the cpp itself, proto .pb.h headers pulled in via the C++ include
		// chain) must not appear as direct inputs — the ragel binary doesn't read
		// them.
		rl6Closure := keepOnlySourceVFS(filterEnSerializedSiblings(window))

		r6Ref, _ := EmitR6(srcInstance, srcRel, ragelLDRef, ragelBinaryVFS, srcIn.Ragel6Flags, rl6Closure, ctx.emit)

		ccSrcRel := strings.TrimPrefix(r6Out.Rel(), srcInstance.Path.Rel()+"/")

		ccIn := srcIn
		ccIn.IncludeInputs = window
		ccIn.PerSourceCFlags = append(append([]ARG(nil), srcIn.PerSourceCFlags...), argWnoImplicitFallthrough)
		ccIn.ExtraDepRefs = append([]NodeRef{r6Ref}, resolveCodegenDepRefs(ctx, srcInstance, window, r6Ref)...)
		ccRef, ccOut, _ := EmitCC(srcInstance, ccSrcRel, r6Out, ccIn, ctx.host, ctx.emit)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut}
	case strings.HasSuffix(srcRel, ".y"):
		return emitBisonY(ctx, srcInstance, srcRel, srcIn, srcIn.BisonGenExt)
	case strings.HasSuffix(srcRel, ".ev"):
		evSource := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDirs)
		evRelPath := evSource.Rel()

		protocLDRef, protocBinary := ctx.tool(argContribToolsProtoc)
		cppStyleguideLDRef, cppStyleguideBinary := ctx.tool(argContribToolsProtocPluginsCppStyleguide)
		event2cppLDRef, event2cppBinary := ctx.tool(argToolsEvent2cpp)

		evImports := walkClosureTail(ctx, srcInstance, evSource, protoWalkInputs(nil))
		evRef := EmitEV(
			srcInstance, evRelPath,
			cppStyleguideLDRef, protocLDRef, event2cppLDRef,
			cppStyleguideBinary, protocBinary, event2cppBinary,
			0, evImports, d.tc, ctx.emit)

		evH := Build(evRelPath + ".pb.h")
		evPbCC := Build(evRelPath + ".pb.cc")

		if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
			directImports := protoDirectPbHIncludes(ctx.parsers, evRelPath, "")
			evExtras := evWitnessExtras(evRelPath, evPbCC)
			evHParsed := make([]includeDirective, 0, len(directImports)+len(protobufRuntimeDirectives)+len(evExtras))
			evHParsed = append(evHParsed, directImports...)
			evHParsed = append(evHParsed, protobufRuntimeDirectives...)
			evHParsed = append(evHParsed, evExtras...)
			registerBoundGeneratedParsedOutput(ctx, srcInstance, pkEV, evH, evHParsed, evRef, []NodeRef{event2cppLDRef})
			evCCParsed := make([]includeDirective, 0, 1+len(protobufRuntimeDirectives))
			evCCParsed = append(evCCParsed, includeDirective{kind: includeQuoted, target: internStr(evH.Rel())})
			evCCParsed = append(evCCParsed, protobufRuntimeDirectives...)
			registerBoundGeneratedParsedOutput(ctx, srcInstance, pkEV, evPbCC, evCCParsed, evRef, []NodeRef{event2cppLDRef})
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
		ragel5LDRef, ragel5BinVFS := ctx.tool(argContribToolsRagel5Ragel)
		rlgenCdLDRef, rlgenCdBinVFS := ctx.tool(argContribToolsRagel5RlgenCd)

		r5Ref, r5TmpOut, r5CppOut := EmitR5(srcInstance, srcRel, ragel5LDRef, rlgenCdLDRef, ragel5BinVFS, rlgenCdBinVFS, ctx.emit)
		_ = r5Ref

		rlSourceVFS := Source(srcInstance.Path.Rel() + "/" + srcRel)
		registerBoundGeneratedParsedOutput(ctx, srcInstance, pkR5, r5TmpOut, nil, r5Ref, []NodeRef{ragel5LDRef, rlgenCdLDRef})
		var r5Parsed []includeDirective

		if scanner := ctx.scannerFor(srcInstance); scanner != nil {
			r5Parsed = scanner.parsers.sourceParsedBuckets(rlSourceVFS, nil).bucket(parsedIncludesCpp)
		}

		registerBoundGeneratedParsedOutput(ctx, srcInstance, pkR5, r5CppOut, r5Parsed, r5Ref, []NodeRef{ragel5LDRef, rlgenCdLDRef})

		ccSrcRel := strings.TrimPrefix(r5CppOut.Rel(), srcInstance.Path.Rel()+"/")
		ccIn := srcIn
		ccClosure := walkClosure(ctx, srcInstance, r5CppOut, srcIn)
		ccIn.IncludeInputs = append([]VFS{r5TmpOut}, ccClosure...)
		ccIn.PerSourceCFlags = append(append([]ARG(nil), srcIn.PerSourceCFlags...), argWnoImplicitFallthrough)
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r5Ref)
		ccIn.ExtraDepRefs = append([]NodeRef{r5Ref}, ccIn.ExtraDepRefs...)
		ccRef, ccOut, _ := EmitCC(srcInstance, ccSrcRel, r5CppOut, ccIn, ctx.host, ctx.emit)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut}
	case strings.HasSuffix(srcRel, ".h.in"):

		inSourceVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDirs)
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, inSourceVFS, srcIn)
		cfgVars := buildCFGVars(ctx.fs, inSourceVFS.Rel(), srcIn.SetVars, srcIn.DefaultVars)
		cfOut := Build(srcInstance.Path.Rel() + "/" + strings.TrimSuffix(srcRel, ".in"))

		parsed := []includeDirective{
			{kind: includeQuoted, target: internStr(inSourceVFS.Rel())},
			{kind: includeQuoted, target: internStr(configureFilePyVFS.Rel())},
		}
		parsed = append(parsed, cfIncludeDirectives(ctx.parsers, inSourceVFS.Rel())...)
		registerDeferredCF(ctx, srcInstance, cfOut, parsed, &deferredCF{
			instance:      srcInstance,
			srcVFS:        inSourceVFS,
			outVFS:        cfOut,
			cfgVars:       cfgVars,
			includeInputs: srcIn.IncludeInputs,
			tc:            srcIn.TC,
		})

		return nil
	case strings.HasSuffix(srcRel, ".cpp.in"),
		strings.HasSuffix(srcRel, ".c.in"):
		inSourceVFS := resolveModuleSourceVFS(ctx, srcInstance, d, srcRel, srcIn.SrcDirs)
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, inSourceVFS, srcIn)
		cfgVars := buildCFGVars(ctx.fs, inSourceVFS.Rel(), srcIn.SetVars, srcIn.DefaultVars)
		cfOut := Build(srcInstance.Path.Rel() + "/" + strings.TrimSuffix(srcRel, ".in"))
		cfRef, cfOut := EmitCF(srcInstance, inSourceVFS, cfOut, cfgVars, srcIn.IncludeInputs, srcInstance.Path.Rel(), cfModuleTag(d, srcInstance), srcIn.TC, ctx.emit)

		registerBoundGeneratedParsedOutput(ctx, srcInstance, pkCF, cfOut, []includeDirective{
			{kind: includeQuoted, target: internStr(inSourceVFS.Rel())},
			{kind: includeQuoted, target: internStr(configureFilePyVFS.Rel())},
		}, cfRef, nil)

		ccSrcRel := strings.TrimPrefix(cfOut.Rel(), srcInstance.Path.Rel()+"/")
		ccIn := srcIn
		ccIn.IncludeInputs = walkClosure(ctx, srcInstance, cfOut, srcIn)
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, cfRef)
		ccIn.ExtraDepRefs = append([]NodeRef{cfRef}, ccIn.ExtraDepRefs...)
		ccRef, ccOut, _ := EmitCC(srcInstance, ccSrcRel, cfOut, ccIn, ctx.host, ctx.emit)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut}
	}

	ThrowFmt("gen: %s: unsupported source extension in %q", instance.Path.Rel(), srcRel)
	return nil
}

func emitLibraryProtoSource(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, in ModuleCCInputs) *sourceEmit {
	pb := emitProtoPB(ctx, instance, d, srcRel, protoPBConfig{}, in.PeerProtoAddInclGlobal, in.ProtoNamespaceTail)
	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx, instance, pb.pbCC, in)
	ccIn.ExtraDepRefs = append([]NodeRef{pb.pbRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, pb.pbRef)...)
	ccSrcRel := strings.TrimPrefix(pb.pbCC.Rel(), instance.Path.Rel()+"/")
	ccRef, ccOut, _ := EmitCC(instance, ccSrcRel, pb.pbCC, ccIn, ctx.host, ctx.emit)
	return &sourceEmit{Ref: ccRef, OutPath: ccOut}
}

func emitLibraryFlatcSource(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, in ModuleCCInputs) *sourceEmit {
	fl := ensureFlatcEmission(ctx, instance, d, srcRel)

	ccIn := in
	ccIn.IncludeInputs = walkClosure(ctx, instance, fl.cpp, in)

	ccIn.ExtraDepRefs = append([]NodeRef{fl.flRef}, resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, fl.flRef)...)
	ccSrcRel := strings.TrimPrefix(fl.cpp.Rel(), instance.Path.Rel()+"/")
	ccRef, ccOut, _ := EmitCC(instance, ccSrcRel, fl.cpp, ccIn, ctx.host, ctx.emit)
	return &sourceEmit{Ref: ccRef, OutPath: ccOut}
}
