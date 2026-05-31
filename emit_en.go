package main

import (
	"sort"
	"strings"
)

var (
	// Path constants hoisted by `ay refac consts`.
	strToolsEnumParserEnumParserStdlibDepsH                    = internString("tools/enum_parser/enum_parser/stdlib_deps.h")
	strToolsEnumParserEnumSerializationRuntimeDispatchMethodsH = internString("tools/enum_parser/enum_serialization_runtime/dispatch_methods.h")
	strToolsEnumParserEnumSerializationRuntimeEnumRuntimeH     = internString("tools/enum_parser/enum_serialization_runtime/enum_runtime.h")
	strToolsEnumParserEnumSerializationRuntimeOrderedPairsH    = internString("tools/enum_parser/enum_serialization_runtime/ordered_pairs.h")
	strUtilGenericMapH                                         = internString("util/generic/map.h")
	strUtilGenericSerializedEnumH                              = internString("util/generic/serialized_enum.h")
	strUtilGenericSingletonH                                   = internString("util/generic/singleton.h")
	strUtilGenericStringH                                      = internString("util/generic/string.h")
	strUtilGenericTypetraitsH                                  = internString("util/generic/typetraits.h")
	strUtilGenericVectorH                                      = internString("util/generic/vector.h")
	strUtilStreamOutputH                                       = internString("util/stream/output.h")
	strUtilStringCastH                                         = internString("util/string/cast.h")
)

type enumSrcsResult struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
}

func resolveEnumHeaderInput(ctx *genCtx, instance ModuleInstance, headerRel string, srcDir *string) VFS {
	headerInput := resolveSourceVFS(ctx, instance, headerRel, srcDir)

	if !ctx.fs.IsFile(headerInput.Rel()) {
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

	const enumParserPath = "tools/enum_parser/enum_parser"

	enumParserLD, enumParserBin := ctx.tool(enumParserPath)

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

		var depENRefs []NodeRef
		var depENOutputs []VFS

		if len(ctx.enOutputs) > 0 {
			serializedHByRel := make(map[string]VFS, len(ctx.enOutputs))

			for buildRootPath := range ctx.enOutputs {
				if !buildRootPath.IsBuild() || !strings.HasSuffix(buildRootPath.Rel(), "_serialized.h") {
					continue
				}

				serializedHByRel[buildRootPath.Rel()] = buildRootPath
			}

			depSeen := map[NodeRef]struct{}{}

			if len(serializedHByRel) > 0 {
				enScanner := ctx.scannerTarget

				for _, srcAbsPath := range closure {
					targets := enScanner.IncludeDirectiveTargets(srcAbsPath)

					for _, includePath := range targets {
						if !strings.HasSuffix(includePath, "_serialized.h") {
							continue
						}

						buildRootPath, ok := serializedHByRel[includePath]

						if !ok {
							continue
						}

						ref := ctx.enOutputs[buildRootPath]

						if _, dup := depSeen[ref]; dup {
							continue
						}

						depSeen[ref] = struct{}{}
						depENRefs = append(depENRefs, ref)
						depENOutputs = append(depENOutputs, buildRootPath)

						cppPath := Build(strings.TrimSuffix(buildRootPath.Rel(), "_serialized.h") + "_serialized.cpp")

						if cppRef, ok2 := ctx.enOutputs[cppPath]; ok2 && cppRef == ref {
							depENOutputs = append(depENOutputs, cppPath)
						}
					}
				}
			}
		}

		serializedCPPPath := Build(instance.Path + "/" + headerRel + "_serialized.cpp")
		var serializedHPath VFS

		if withHeader {
			serializedHPath = Build(instance.Path + "/" + headerRel + "_serialized.h")
		}

		if ctx.scannerTarget.codegen != nil {
			cppParsed := []includeDirective{
				{kind: includeQuoted, target: internString(headerInput.Rel())},
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
			registerGeneratedParsedOutput(ctx, instance, "EN", serializedCPPPath, cppParsed)

			if withHeader {
				hParsed := []includeDirective{
					{kind: includeQuoted, target: internString(headerInput.Rel())},
					{kind: includeQuoted, target: internString(serializedCPPPath.Rel())},
					{kind: includeQuoted, target: strUtilGenericSerializedEnumH},
				}
				sort.Slice(hParsed, func(i, j int) bool { return hParsed[i].target.String() < hParsed[j].target.String() })
				registerGeneratedParsedOutput(ctx, instance, "EN", serializedHPath, hParsed)
			}
		}

		enClosureExcl := map[VFS]struct{}{
			headerInput: {},
		}

		for _, p := range depENOutputs {
			enClosureExcl[p] = struct{}{}
		}

		filteredClosure := make([]VFS, 0, len(closure))

		for _, p := range closure {
			if _, drop := enClosureExcl[p]; drop {
				continue
			}

			filteredClosure = append(filteredClosure, p)
		}

		var crossCppClosure []VFS

		for _, depOut := range depENOutputs {
			if !strings.HasSuffix(depOut.Rel(), "_serialized.cpp") {
				continue
			}

			sub := walkClosure(ctx, instance, depOut, scanIn)

			for _, p := range sub {
				if _, drop := enClosureExcl[p]; drop {
					continue
				}

				crossCppClosure = append(crossCppClosure, p)
			}
		}

		var ownOutputClosure []VFS

		if !withHeader && ctx.scannerTarget.codegen != nil {
			sub := walkClosure(ctx, instance, serializedCPPPath, scanIn)

			for _, p := range sub {
				if _, drop := enClosureExcl[p]; drop {
					continue
				}

				ownOutputClosure = append(ownOutputClosure, p)
			}
		}

		enClosure := mergeDedupVFS(filteredClosure, crossCppClosure)
		enClosure = mergeDedupVFS(enClosure, ownOutputClosure)

		augmentedDepENRefs := depENRefs
		enDepScan := append([]VFS{headerInput}, enClosure...)

		if extra := resolveCodegenDepRefs(ctx, instance, enDepScan, depENRefs...); len(extra) > 0 {
			augmentedDepENRefs = append(append([]NodeRef(nil), depENRefs...), extra...)
		}

		var moduleTag *string

		if d.moduleStmt != nil && d.moduleStmt.Name == "PROTO_LIBRARY" {
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
			depENOutputs,
			enClosure,
			ctx.emit,
		)

		for _, p := range enOutPaths {
			ctx.enOutputs[p] = enRef
		}

		if consumerInputs != nil {
			cppRel := headerRel + "_serialized.cpp"

			allDepRefs := make([]NodeRef, 0, 1+len(augmentedDepENRefs))
			allDepRefs = append(allDepRefs, enRef)
			allDepRefs = append(allDepRefs, augmentedDepENRefs...)
			ccRef, ccOut := emitCodegenDownstreamCC(ctx, instance, cppRel, depENOutputs, allDepRefs, *consumerInputs)
			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
		}
	}

	return res
}
