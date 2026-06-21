package main

import (
	"sort"
	"strings"
)

type EnumSrcsResult struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
	// Seqs parallels CCRefs/CCOutputs: each member's declaring
	// GENERATE_ENUM_SERIALIZATION statement declaration sequence, used to order
	// generated archive members by declaration order against other
	// default-priority generated statements (RUN_PROGRAM).
	Seqs []int
}

func resolveEnumHeaderInput(ctx *GenCtx, instance ModuleInstance, headerRel string, srcDirs []VFS) VFS {
	headerInput := resolveSourceVFS(ctx, instance, headerRel, srcDirs)

	if !ctx.fs.isFile(srcRootVFS, headerInput.rel()) {
		if vfs := sourceInputVFS(ctx.fs, instance.Path.rel(), headerRel); vfs != nil && vfs.isSource() {
			headerInput = *vfs
		}
	}

	buildHeader := build(headerInput.rel())

	if codegenRegForInstance(ctx, instance).lookup(buildHeader) != nil {
		return buildHeader
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
		FS:                ctx.fs,
		SrcDirs:           d.srcDirs,
		ScanCfg:           newScanContext(ctx.parsers, d.addIncl, peerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel()),
	}
	res := &EnumSrcsResult{}

	for _, stmt := range d.enumSrcs {
		withHeader := stmt.Variant == "with_header"
		headerInput := resolveEnumHeaderInput(ctx, instance, stmt.Header, d.srcDirs)

		// ymake's ${output;suf=_serialized.cpp:File} names the EN output from the
		// File argument: a relative spelling joins the raw token under the module
		// BINDIR (a root-qualified token that repeats the module dir doubles —
		// TestGen_EnumSerializationRootQualifiedHeaderUsesCanonicalInput); a rooted
		// spelling (${ARCADIA_BUILD_ROOT}/<dir>/foo.pb.h for a generated proto
		// header) lands at the File's own resolved build location, which for a
		// header OUTSIDE the module dir is not under it. The downstream compile then
		// rebases that cross-dir source under the module BINDIR via composeCCPaths.
		serializedBase := instance.Path.rel() + "/" + stmt.Header

		if moduleRootedVFS(instance.Path.rel(), stmt.Header) != nil {
			serializedBase = headerInput.rel()
		}

		closure := walkClosure(ctx.scannerFor(instance), headerInput, scanIn.ScanCfg)

		serializedCPPPath := build(serializedBase + "_serialized.cpp")
		var serializedHPath VFS

		if withHeader {
			serializedHPath = build(serializedBase + "_serialized.h")
		}

		// Reserve the EN producer's ref before registering its outputs: the own-output
		// closure walk below (walkClosureTail of serializedCPPPath) needs the parsed
		// includes registered first, and registration records the producer ref.
		enRef := ctx.emit.reserve()

		{
			// Only the macro-level output_includes are woven by hand: the enum header
			// itself (output_include:File) and util/generic/serialized_enum.h. The
			// runtime headers come from enum_parser's INDUCED_DEPS via the GeneratorRef
			// (the h+cpp group), and enum_runtime.h's own transitive includes
			// (dispatch_methods.h, ordered_pairs.h) arrive through the closure walk.
			cppParsed := []IncludeDirective{
				{kind: includeQuoted, target: internStr(headerInput.rel())},
				{kind: includeQuoted, target: strUtilGenericSerializedEnumH},
			}
			sort.Slice(cppParsed, func(i, j int) bool { return cppParsed[i].target.string() < cppParsed[j].target.string() })
			registerBoundGeneratedParsedOutput(ctx, instance, pkEN, serializedCPPPath, cppParsed, enRef, []NodeRef{enumParserLD})

			if withHeader {
				// serialized_enum.h reaches the header via enum_parser's INDUCED_DEPS(h …).
				hParsed := []IncludeDirective{
					{kind: includeQuoted, target: internStr(headerInput.rel())},
					{kind: includeQuoted, target: internStr(serializedCPPPath.rel())},
				}
				sort.Slice(hParsed, func(i, j int) bool { return hParsed[i].target.string() < hParsed[j].target.string() })
				registerBoundGeneratedParsedOutput(ctx, instance, pkEN, serializedHPath, hParsed, enRef, []NodeRef{enumParserLD})
			}
		}

		var ownOutputClosure []VFS

		if !withHeader {
			// Tail: serializedCPPPath is the EN node's own output, never its
			// input (headerInput dedups out via dedupVFS below).
			ownOutputClosure = walkClosureTail(ctx.scannerFor(instance), serializedCPPPath, scanIn.ScanCfg)
		}

		// closure is the headerInput window (header-led), so the EN input list
		// is the deduped union directly — no separate header prepend.
		enClosure := dedupVFS(closure, ownOutputClosure)

		augmentedDepENRefs := resolveCodegenDepRefs(ctx, instance, enClosure)

		var moduleTag STR

		if d.moduleStmt.Name == tokProtoLibrary {
			moduleTag = tagCppProto
		}

		emitEN(
			instance,
			headerInput,
			serializedCPPPath,
			serializedHPath,
			moduleTag,
			withHeader,
			enumParserLD,
			enumParserBin,
			augmentedDepENRefs,
			enClosure,
			enRef,
			ctx.emit,
		)

		if consumerInputs != nil {
			cppRel := strings.TrimPrefix(serializedCPPPath.rel(), instance.Path.rel()+"/")

			allDepRefs := make([]NodeRef, 0, 1+len(augmentedDepENRefs))
			allDepRefs = append(allDepRefs, enRef)
			allDepRefs = append(allDepRefs, augmentedDepENRefs...)
			ccRef, ccOut := emitCodegenDownstreamCCFromVFS(ctx, instance, cppRel, serializedCPPPath, allDepRefs, *consumerInputs)
			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
			res.Seqs = append(res.Seqs, stmt.DeclSeq)
		}
	}

	return res
}

func emitEN(
	instance ModuleInstance,
	headerInput VFS,
	serializedCPPVFS VFS,
	serializedHVFS VFS,
	moduleTag STR,
	withHeader bool,
	enumParserLD NodeRef,
	enumParserBin VFS,
	depENRefs []NodeRef,
	headerIncludeClosure []VFS,
	id NodeRef,
	emit Emitter,
) {
	na := emit.nodeArenas()

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
		cmdArgs = append(cmdArgs, argHeader.str(), (serializedHVFS).str())
		outputs = append(outputs, serializedHVFS)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	deps := append([]NodeRef(nil), depENRefs...)
	foreignDepRefs := depRefs(enumParserLD)

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(enumParserBin), headerIncludeClosure),
		KV:               KV{P: pkEN, PC: pcYellow},
		Outputs:          outputs,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Sandboxing:       true,
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		DepRefs:          deps,
		ForeignDepRefs:   foreignDepRefs,
	}

	if moduleTag != 0 {
		node.TargetProperties.ModuleTag = moduleTag
	}

	emit.emitReserved(node, id)
}
