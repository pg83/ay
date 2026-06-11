package main

import (
	"sort"
	"strings"
)

type EnumSrcsResult struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
}

func resolveEnumHeaderInput(ctx *GenCtx, instance ModuleInstance, headerRel string, srcDirs []VFS) VFS {
	headerInput := resolveSourceVFS(ctx, instance, headerRel, srcDirs)

	if !ctx.fs.isFile(srcRootVFS, headerInput.rel()) {
		if vfs := sourceInputVFS(ctx.fs, instance.Path.rel(), headerRel); vfs != nil && vfs.isSource() {
			headerInput = *vfs
		}
	}

	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		buildHeader := Build(headerInput.rel())

		if reg.lookup(buildHeader) != nil {
			return buildHeader
		}
	}

	return headerInput
}

func emitEnumSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerAddInclGlobal []VFS, consumerInputs *ModuleCCInputs) *EnumSrcsResult {
	if len(d.enumSrcs) == 0 {
		return nil
	}

	enumParserLD, enumParserBin := ctx.tool(argToolsEnumParserEnumParser)

	scanIn := ModuleCCInputs{
		TC:                d.tc,
		InclArgs:          ctx.inclArgs,
		Flags:             d.flags,
		AddIncl:           d.addIncl,
		PeerAddInclGlobal: peerAddInclGlobal,
		SourceRoot:        ctx.sourceRoot,
		FS:                ctx.fs,
		SrcDirs:           d.srcDirs,
	}
	res := &EnumSrcsResult{}

	for _, stmt := range d.enumSrcs {
		withHeader := stmt.Variant == "with_header"
		headerInput := resolveEnumHeaderInput(ctx, instance, stmt.Header, d.srcDirs)

		// For a rooted spelling (e.g. ${ARCADIA_BUILD_ROOT}/<mod>/foo.pb.h for a
		// generated proto header), the canonical module-relative headerRel comes
		// from the resolved VFS, so the _serialized.cpp/.h outputs and the
		// downstream CC are named under the module's build dir.
		headerRel := stmt.Header

		if moduleRootedVFS(instance.Path.rel(), stmt.Header) != nil {
			headerRel = strings.TrimPrefix(headerInput.rel(), instance.Path.rel()+"/")
		}

		closure := walkClosure(ctx, instance, headerInput, scanIn)

		serializedCPPPath := Build(instance.Path.rel() + "/" + headerRel + "_serialized.cpp")
		var serializedHPath VFS

		if withHeader {
			serializedHPath = Build(instance.Path.rel() + "/" + headerRel + "_serialized.h")
		}

		if ctx.scannerTarget.codegen != nil {
			// Only the macro-level output_includes are woven by hand: the enum header
			// itself (output_include:File) and util/generic/serialized_enum.h. The
			// runtime headers come from enum_parser's INDUCED_DEPS via the GeneratorRef
			// (the h+cpp group), and enum_runtime.h's own transitive includes
			// (dispatch_methods.h, ordered_pairs.h) arrive through the closure walk.
			cppParsed := []IncludeDirective{
				{kind: includeQuoted, target: internStr(headerInput.rel())},
				{kind: includeQuoted, target: strUtilGenericSerializedEnumH},
			}
			sort.Slice(cppParsed, func(i, j int) bool { return cppParsed[i].target.String() < cppParsed[j].target.String() })
			registerGeneratedParsedOutput(ctx, instance, pkEN, serializedCPPPath, cppParsed, []NodeRef{enumParserLD})

			if withHeader {
				// serialized_enum.h reaches the header via enum_parser's INDUCED_DEPS(h …).
				hParsed := []IncludeDirective{
					{kind: includeQuoted, target: internStr(headerInput.rel())},
					{kind: includeQuoted, target: internStr(serializedCPPPath.rel())},
				}
				sort.Slice(hParsed, func(i, j int) bool { return hParsed[i].target.String() < hParsed[j].target.String() })
				registerGeneratedParsedOutput(ctx, instance, pkEN, serializedHPath, hParsed, []NodeRef{enumParserLD})
			}
		}

		var ownOutputClosure []VFS

		if !withHeader && ctx.scannerTarget.codegen != nil {
			// Tail: serializedCPPPath is the EN node's own output, never its
			// input (headerInput dedups out via dedupVFS below).
			ownOutputClosure = walkClosureTail(ctx, instance, serializedCPPPath, scanIn)
		}

		// closure is the headerInput window (header-led), so the EN input list
		// is the deduped union directly — no separate header prepend.
		enClosure := dedupVFS(closure, ownOutputClosure)

		augmentedDepENRefs := resolveCodegenDepRefs(ctx, instance, enClosure)

		var moduleTag STR

		if d.moduleStmt != nil && d.moduleStmt.Name == tokProtoLibrary {
			moduleTag = tagCppProto
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
				reg.setProducerRef(p, enRef)
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

func EmitEN(
	instance ModuleInstance,
	headerInput VFS,
	headerRel string,
	moduleTag STR,
	withHeader bool,
	enumParserLD NodeRef,
	enumParserBin VFS,
	depENRefs []NodeRef,
	headerIncludeClosure []VFS,
	emit Emitter,
) (NodeRef, []VFS) {
	serializedCPPVFS := Build(instance.Path.rel() + "/" + headerRel + "_serialized.cpp")

	cmdArgs := []STR{
		(enumParserBin).str(),
		(headerInput).str(),
		argIncludePath.str(),
		internStr(headerInput.rel()),
		argOutput.str(),
		(serializedCPPVFS).str(),
	}
	outputs := []VFS{serializedCPPVFS}

	if withHeader {
		serializedHVFS := Build(instance.Path.rel() + "/" + headerRel + "_serialized.h")
		cmdArgs = append(cmdArgs, argHeader.str(), (serializedHVFS).str())
		outputs = append(outputs, serializedHVFS)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	depRefs := make([]NodeRef, 0, len(depENRefs)+1)

	if enumParserLD != (NodeRef(0)) {
		depRefs = append(depRefs, enumParserLD)
	}

	depRefs = append(depRefs, depENRefs...)

	var foreignDepRefs []NodeRef

	if enumParserLD != (NodeRef(0)) {
		foreignDepRefs = []NodeRef{enumParserLD}
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: ArgChunks{cmdArgs},
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           InputChunks{{enumParserBin}, headerIncludeClosure},
		KV:               KV{P: pkEN, PC: pcYellow},
		Outputs:          outputs,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	if moduleTag != 0 {
		node.TargetProperties.ModuleTag = moduleTag
	}

	return emit.emit(node), outputs
}
