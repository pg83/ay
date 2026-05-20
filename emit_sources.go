package main

import (
	"strings"
)

// sourceEmit is the emit-product of emitOneSource: a single
// CC/AS/R5/R6/CF/etc. node from one declared source. nil = silently
// skipped (e.g. `.h` headers, deferred-kind sources). PrimaryCount is
// the leading-CcIns count naming the member's primary source(s) —
// `.c/.cpp/.cc/.cxx/.S/.s/.asm` yield 1; `.rl6` yields 1 or 2 (source
// ± companion `.h`).
type sourceEmit struct {
	Ref          NodeRef
	OutPath      VFS
	CcIns        []VFS
	PrimaryCount int
}

func emitOneSource(ctx *genCtx, instance ModuleInstance, d *moduleData, srcRel string, in ModuleCCInputs, ancestorRebase bool) *sourceEmit {
	if isHeaderSource(srcRel) {
		return nil
	}

	srcInstance := instance
	if ancestorRebase {
		srcInstance.Path = *d.srcDir
	}

	srcIn := in
	if ancestorRebase {
		srcIn.SrcDir = nil
	}

	switch {
	case strings.HasSuffix(srcRel, ".proto"):
		return emitLibraryProtoSource(ctx, srcInstance, d, srcRel, srcIn)
	case strings.HasSuffix(srcRel, ".c"),
		strings.HasSuffix(srcRel, ".cpp"),
		strings.HasSuffix(srcRel, ".cc"),
		strings.HasSuffix(srcRel, ".cxx"):
		srcVFS := resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir)
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, srcVFS, srcIn)
		extras := runtimePy3CCExtraInputs(srcInstance.Path, srcRel)
		if len(extras) > 0 {
			srcIn.IncludeInputs = appendVFSUnique(srcIn.IncludeInputs, extras)
		}
		srcIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, srcIn.IncludeInputs)

		ref, outPath := EmitCC(srcInstance, srcRel, srcVFS, srcIn, ctx.host, ctx.emit)
		ccInputs := append([]VFS{srcVFS}, srcIn.IncludeInputs...)

		return &sourceEmit{Ref: ref, OutPath: outPath, CcIns: ccInputs, PrimaryCount: 1}
	case strings.HasSuffix(srcRel, ".S"),
		strings.HasSuffix(srcRel, ".s"),
		strings.HasSuffix(srcRel, ".asm"):
		var yasmRef *NodeRef

		if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
			ldRef, _ := ctx.tool("contrib/tools/yasm")
			yasmRef = &ldRef
		}

		asIn := srcIn
		srcVFS := resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir)
		asIn.IncludeInputs = walkClosure(ctx, srcInstance, srcVFS, srcIn)
		ref, outPath := EmitAS(srcInstance, srcRel, srcVFS, asIn, yasmRef, ctx.host, ctx.emit)

		asInputs := append([]VFS{srcVFS}, asIn.IncludeInputs...)
		return &sourceEmit{Ref: ref, OutPath: outPath, CcIns: asInputs, PrimaryCount: 1}
	case strings.HasSuffix(srcRel, ".rl6"):
		ragelLDRef, ragelBinaryVFS := ctx.tool("contrib/tools/ragel6/bin")

		rl6SourceVFS := resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir)
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

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, r6Out, ccIn, ctx.host, ctx.emit)

		ccInputs := []VFS{rl6SourceVFS}
		primaryCount := 1

		companionRel := strings.TrimSuffix(srcRel, ".rl6") + ".h"
		if ctx.fs.IsFile(srcInstance.Path + "/" + companionRel) {
			ccInputs = append(ccInputs, Source(srcInstance.Path+"/"+companionRel))
			primaryCount = 2
		}
		ccInputs = append(ccInputs, ccIncludeInputs...)

		return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: ccInputs, PrimaryCount: primaryCount}
	case strings.HasSuffix(srcRel, ".y"):
		return emitBisonY(ctx, srcInstance, srcRel, srcIn, srcIn.BisonGenExt)
	case strings.HasSuffix(srcRel, ".ev"):
		evSource := resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir)
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

		ref, outPath := EmitCC(srcInstance, evPbCCSuffix, evPbCC, ccIn, ctx.host, ctx.emit)

		return &sourceEmit{
			Ref:          ref,
			OutPath:      outPath,
			CcIns:        []VFS{Source(srcInstance.Path + "/" + srcRel), wireFormatVFS},
			PrimaryCount: 1,
		}
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

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, r5CppOut, ccIn, ctx.host, ctx.emit)
		rlMemberInputs := append([]VFS{Source(srcInstance.Path + "/" + srcRel)}, ccClosure...)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: rlMemberInputs, PrimaryCount: 1}
	case strings.HasSuffix(srcRel, ".h.in"):
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)
		cfgVars := buildCFGVars(ctx.fs, srcInstance.Path+"/"+srcRel, srcIn.SetVars, srcIn.DefaultVars)
		cfRef, cfOut := EmitCF(srcInstance, srcRel, cfgVars, srcIn.IncludeInputs, ctx.emit)

		inSourceVFS := Source(srcInstance.Path + "/" + srcRel)
		registerBoundGeneratedParsedOutput(ctx, srcInstance, "CF", cfOut, []includeDirective{
			{kind: includeQuoted, target: inSourceVFS.Rel},
			{kind: includeQuoted, target: configureFilePyVFS.Rel},
		}, cfRef)

		cfMemberInputs := append([]VFS{Source(srcInstance.Path + "/" + srcRel)}, srcIn.IncludeInputs...)
		return &sourceEmit{Ref: cfRef, OutPath: cfOut, CcIns: cfMemberInputs, PrimaryCount: 1}
	case strings.HasSuffix(srcRel, ".cpp.in"),
		strings.HasSuffix(srcRel, ".c.in"):
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)
		cfgVars := buildCFGVars(ctx.fs, srcInstance.Path+"/"+srcRel, srcIn.SetVars, srcIn.DefaultVars)
		cfRef, cfOut := EmitCF(srcInstance, srcRel, cfgVars, srcIn.IncludeInputs, ctx.emit)

		inSourceVFS := Source(srcInstance.Path + "/" + srcRel)
		registerBoundGeneratedParsedOutput(ctx, srcInstance, "CF", cfOut, []includeDirective{
			{kind: includeQuoted, target: inSourceVFS.Rel},
			{kind: includeQuoted, target: configureFilePyVFS.Rel},
		}, cfRef)

		ccSrcRel := strings.TrimPrefix(cfOut.Rel, srcInstance.Path+"/")
		ccIn := srcIn
		ccIn.IncludeInputs = walkClosure(ctx, srcInstance, cfOut, srcIn)
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, cfRef)
		ccIn.ExtraDepRefs = append([]NodeRef{cfRef}, ccIn.ExtraDepRefs...)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, cfOut, ccIn, ctx.host, ctx.emit)
		cfMemberInputs := append([]VFS{Source(srcInstance.Path + "/" + srcRel)}, ccIn.IncludeInputs...)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: cfMemberInputs, PrimaryCount: 1}
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
	ccRef, ccOut := EmitCC(instance, ccSrcRel, pb.pbCC, ccIn, ctx.host, ctx.emit)
	ccInputs := make([]VFS, 0, 1+len(ccIn.IncludeInputs))
	ccInputs = append(ccInputs, Source(pb.relPath))
	ccInputs = append(ccInputs, ccIn.IncludeInputs...)

	return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: ccInputs, PrimaryCount: 1}
}
