package main

import (
	"sort"
)

type enumSrcsResult struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
}

func resolveEnumHeaderInput(ctx *genCtx, instance ModuleInstance, headerRel string, srcDir *string) VFS {
	headerInput := resolveSourceVFS(ctx, instance, headerRel, srcDir)

	if !ctx.fs.IsFile(srcRootVFS, headerInput.Rel()) {
		if vfs := sourceInputVFS(ctx.fs, instance.Path, headerRel); vfs != nil && vfs.IsSource() {
			headerInput = *vfs
		}
	}

	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		buildHeader := Build(headerInput.Rel())

		if reg.Lookup(buildHeader) != nil {
			return buildHeader
		}
	}

	return headerInput
}

func emitEnumSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerAddInclGlobal []VFS, consumerInputs *ModuleCCInputs) *enumSrcsResult {
	if len(d.enumSrcs) == 0 {
		return nil
	}

	enumParserLD, enumParserBin := ctx.tool(argToolsEnumParserEnumParser)

	scanIn := ModuleCCInputs{
		InclArgs:          ctx.inclArgs,
		Flags:             d.flags,
		AddIncl:           d.addIncl,
		PeerAddInclGlobal: peerAddInclGlobal,
		SourceRoot:        ctx.sourceRoot,
		FS:                ctx.fs,
		SrcDir:            d.srcDir,
	}
	res := &enumSrcsResult{}

	for _, stmt := range d.enumSrcs {
		headerRel := stmt.Header
		withHeader := stmt.Variant == "with_header"
		headerInput := resolveEnumHeaderInput(ctx, instance, headerRel, d.srcDir)

		closure := walkClosure(ctx, instance, headerInput, scanIn)

		serializedCPPPath := Build(instance.Path + "/" + headerRel + "_serialized.cpp")
		var serializedHPath VFS

		if withHeader {
			serializedHPath = Build(instance.Path + "/" + headerRel + "_serialized.h")
		}

		if ctx.scannerTarget.codegen != nil {
			cppParsed := []includeDirective{
				{kind: includeQuoted, target: internStr(headerInput.Rel())},
				{kind: includeQuoted, target: strToolsEnumParserEnumParserStdlibDepsH},
				{kind: includeQuoted, target: strToolsEnumParserEnumSerializationRuntimeDispatchMethodsH},
				{kind: includeQuoted, target: strToolsEnumParserEnumSerializationRuntimeEnumRuntimeH},
				{kind: includeQuoted, target: strToolsEnumParserEnumSerializationRuntimeOrderedPairsH},
				{kind: includeQuoted, target: strUtilGenericMapH},
				{kind: includeQuoted, target: strUtilGenericSerializedEnumH},
				{kind: includeQuoted, target: strUtilGenericSingletonH},
				{kind: includeQuoted, target: strUtilGenericStringH},
				{kind: includeQuoted, target: strUtilGenericTypetraitsH},
				{kind: includeQuoted, target: strUtilGenericVectorH},
				{kind: includeQuoted, target: strUtilStreamOutputH},
				{kind: includeQuoted, target: strUtilStringCastH},
			}
			sort.Slice(cppParsed, func(i, j int) bool { return cppParsed[i].target.String() < cppParsed[j].target.String() })
			registerGeneratedParsedOutput(ctx, instance, "EN", serializedCPPPath, cppParsed, nil)

			if withHeader {
				hParsed := []includeDirective{
					{kind: includeQuoted, target: internStr(headerInput.Rel())},
					{kind: includeQuoted, target: internStr(serializedCPPPath.Rel())},
					{kind: includeQuoted, target: strUtilGenericSerializedEnumH},
				}
				sort.Slice(hParsed, func(i, j int) bool { return hParsed[i].target.String() < hParsed[j].target.String() })
				registerGeneratedParsedOutput(ctx, instance, "EN", serializedHPath, hParsed, nil)
			}
		}

		filteredClosure := make([]VFS, 0, len(closure))

		for _, p := range closure {
			if p == headerInput {
				continue
			}

			filteredClosure = append(filteredClosure, p)
		}

		var ownOutputClosure []VFS

		if !withHeader && ctx.scannerTarget.codegen != nil {
			sub := walkClosure(ctx, instance, serializedCPPPath, scanIn)

			for _, p := range sub {
				if p == headerInput {
					continue
				}

				ownOutputClosure = append(ownOutputClosure, p)
			}
		}

		enClosure := dedupVFS(filteredClosure, ownOutputClosure)

		enDepScan := append([]VFS{headerInput}, enClosure...)
		augmentedDepENRefs := resolveCodegenDepRefs(ctx, instance, enDepScan)

		var moduleTag *string

		if d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary {
			moduleTag = stringPtr("cpp_proto")
		}

		enRef, enOutPaths := EmitEN(
			instance,
			headerInput,
			headerRel,
			moduleTag,
			withHeader,
			enumParserLD,
			enumParserBin,
			augmentedDepENRefs,
			enClosure,
			ctx.emit,
		)

		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			for _, p := range enOutPaths {
				reg.SetProducerRef(p, enRef)
			}
		}

		if consumerInputs != nil {
			cppRel := headerRel + "_serialized.cpp"

			allDepRefs := make([]NodeRef, 0, 1+len(augmentedDepENRefs))
			allDepRefs = append(allDepRefs, enRef)
			allDepRefs = append(allDepRefs, augmentedDepENRefs...)
			ccRef, ccOut := emitCodegenDownstreamCC(ctx, instance, cppRel, allDepRefs, *consumerInputs)
			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
		}
	}

	return res
}
