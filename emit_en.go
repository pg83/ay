package main

import (
	"sort"
	"strings"
)

type EnumSrcsResult struct {
	CCRefs    []NodeRef
	CCOutputs []VFS
	// Seqs parallels CCRefs/CCOutputs: each member's declaring statement sequence,
	// ordering generated archive members by declaration order.
	Seqs []int
	// SecondLevel parallels CCRefs/CCOutputs: true when the EN's input header is a
	// .pb.h/.ev.pb.h from THIS module's own proto/ev SRCS. Such an EN defers one
	// further FIFO round, archiving after every first-level generated member.
	SecondLevel []bool
}

// moduleProtoGenHeaders returns the root-relative generated header outputs this
// module's own proto/ev SRCS produce, resolved through SRCDIR/PROTO_NAMESPACE.
// Keying on the resolved path (not the raw token) avoids misclassifying
// SRCDIR-rooted same-module headers as external.
func moduleProtoGenHeaders(ctx *GenCtx, instance ModuleInstance, d *ModuleData) map[string]struct{} {
	var set map[string]struct{}

	add := func(h string) {
		if set == nil {
			set = map[string]struct{}{}
		}

		set[h] = struct{}{}
	}

	for _, src := range d.srcs {
		s := src.string()

		switch {
		case strings.HasSuffix(s, ".proto"):
			base := strings.TrimSuffix(protoSourceRelPath(ctx.fs, instance, d, s), ".proto")
			add(base + ".pb.h")
		case strings.HasSuffix(s, ".ev"):
			add(protoSourceRelPath(ctx.fs, instance, d, s) + ".pb.h")
		}
	}

	return set
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
	protoGenHeaders := moduleProtoGenHeaders(ctx, instance, d)

	// One declaration's generated header can include another's serialized header
	// from the SAME module. The scanner caches resolved children by file id, so the
	// first closure walk must already see every sibling's serialized outputs
	// registered, else it caches an empty resolution that poisons later compiles.
	// Two passes: register all serialized outputs first, then walk closures and emit.
	type enumStmtPlan struct {
		withHeader        bool
		headerInput       VFS
		serializedCPPPath VFS
		serializedHPath   VFS
		enRef             NodeRef
		declSeq           int
		secondLevel       bool
	}

	plans := make([]enumStmtPlan, len(d.enumSrcs))

	for i, stmt := range d.enumSrcs {
		withHeader := stmt.Variant == "with_header"
		headerInput := resolveEnumHeaderInput(ctx, instance, stmt.Header, d.srcDirs)

		// The EN output is named from the File argument: a relative spelling joins
		// the raw token under the module BINDIR; a rooted spelling (build-root header)
		// lands at its own resolved build location, which the downstream compile then
		// rebases under the module BINDIR via composeCCPaths.
		serializedBase := instance.Path.rel() + "/" + stmt.Header

		if moduleRootedVFS(instance.Path.rel(), stmt.Header) != nil {
			serializedBase = headerInput.rel()
		}

		// An EN over a .pb.h/.ev.pb.h from this module's own proto/ev SRCS is
		// second-level; see EnumSrcsResult.SecondLevel.
		_, secondLevel := protoGenHeaders[headerInput.rel()]

		serializedCPPPath := build(serializedBase + "_serialized.cpp")
		var serializedHPath VFS

		if withHeader {
			serializedHPath = build(serializedBase + "_serialized.h")
		}

		// Reserve the EN producer's ref before registering its outputs: a consumer
		// resolving these outputs to a dep edge reads the producer ref.
		enRef := ctx.emit.reserve()

		// Only the macro-level output_includes are woven by hand; runtime headers
		// come from enum_parser's INDUCED_DEPS and the closure walk.
		cppParsed := []IncludeDirective{
			{kind: includeQuoted, target: internStr(headerInput.rel())},
			{kind: includeQuoted, target: strUtilGenericSerializedEnumH},
		}
		sort.Slice(cppParsed, func(i, j int) bool { return cppParsed[i].target.string() < cppParsed[j].target.string() })
		registerBoundGeneratedParsedOutput(ctx, instance, pkEN, serializedCPPPath, cppParsed, enRef, []NodeRef{enumParserLD})

		if withHeader {
			hParsed := []IncludeDirective{
				{kind: includeQuoted, target: internStr(headerInput.rel())},
				{kind: includeQuoted, target: internStr(serializedCPPPath.rel())},
			}
			sort.Slice(hParsed, func(i, j int) bool { return hParsed[i].target.string() < hParsed[j].target.string() })
			registerBoundGeneratedParsedOutput(ctx, instance, pkEN, serializedHPath, hParsed, enRef, []NodeRef{enumParserLD})
		}

		plans[i] = enumStmtPlan{
			withHeader:        withHeader,
			headerInput:       headerInput,
			serializedCPPPath: serializedCPPPath,
			serializedHPath:   serializedHPath,
			enRef:             enRef,
			declSeq:           stmt.DeclSeq,
			secondLevel:       secondLevel,
		}
	}

	var moduleTag STR

	if d.moduleStmt.Name == tokProtoLibrary {
		moduleTag = tagCppProto
	}

	for _, p := range plans {
		closure := walkClosure(ctx.scannerFor(instance), p.headerInput, scanIn.ScanCfg)

		var ownOutputClosure []VFS

		if !p.withHeader {
			// serializedCPPPath is the EN node's own output, never its input.
			ownOutputClosure = walkClosureTail(ctx.scannerFor(instance), p.serializedCPPPath, scanIn.ScanCfg)
		}

		// closure is header-led, so the deduped union is the EN input list directly.
		enClosure := dedupVFS(closure, ownOutputClosure)

		augmentedDepENRefs := resolveCodegenDepRefs(ctx, instance, enClosure)

		emitEN(
			instance,
			p.headerInput,
			p.serializedCPPPath,
			p.serializedHPath,
			moduleTag,
			p.withHeader,
			enumParserLD,
			enumParserBin,
			augmentedDepENRefs,
			enClosure,
			p.enRef,
			ctx.emit,
		)

		if consumerInputs != nil {
			cppRel := strings.TrimPrefix(p.serializedCPPPath.rel(), instance.Path.rel()+"/")

			allDepRefs := make([]NodeRef, 0, 1+len(augmentedDepENRefs))
			allDepRefs = append(allDepRefs, p.enRef)
			allDepRefs = append(allDepRefs, augmentedDepENRefs...)
			ccRef, ccOut := emitCodegenDownstreamCCFromVFS(ctx, instance, cppRel, p.serializedCPPPath, allDepRefs, *consumerInputs)
			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
			res.Seqs = append(res.Seqs, p.declSeq)
			res.SecondLevel = append(res.SecondLevel, p.secondLevel)
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
