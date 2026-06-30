package main

import (
	"sort"
	"strings"
)

var enKV = KV{P: pkEN, PC: pcYellow}

type EnumSrcsResult struct {
	CCRefs      []NodeRef
	CCOutputs   []VFS
	Seqs        []int
	SecondLevel []bool
}

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
		case extIsProto(s):
			base := strings.TrimSuffix(protoSourceRelPath(ctx.fs, instance, d, s), ".proto")

			add(base + ".pb.h")
		case extIsEv(s):
			add(protoSourceRelPath(ctx.fs, instance, d, s) + ".pb.h")
		}
	}

	return set
}

func resolveEnumHeaderInput(ctx *GenCtx, instance ModuleInstance, headerRel string, srcDirs []VFS) VFS {
	headerInput := resolveSourceVFS(ctx, instance, headerRel, srcDirs)

	if !ctx.fs.isFile(srcRootVFS, headerInput.rel()) {
		if vfs := sourceInputVFS(ctx.fs, instance.Path, headerRel); vfs != nil && vfs.isSource() {
			headerInput = *vfs
		}
	}

	buildHeader := build(headerInput.rel())

	if ctx.codegenFor(instance).lookup(buildHeader) != nil {
		return buildHeader
	}

	return headerInput
}

func emitEnumSrcs(ctx *GenCtx, instance ModuleInstance, d *ModuleData, peerAddInclGlobal []VFS) *EnumSrcsResult {
	if len(d.enumSrcs) == 0 {
		return nil
	}

	enumParserLD, enumParserBin := ctx.tool(argToolsEnumParserEnumParser)

	scanCfg := newScanContext(ctx.parsers, d.addIncl, peerAddInclGlobal, includeScannerBasePaths(), instance.Path.rel())

	res := &EnumSrcsResult{}
	protoGenHeaders := moduleProtoGenHeaders(ctx, instance, d)

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
		serializedBase := instance.Path.rel() + "/" + stmt.Header

		if moduleRootedVFS(instance.Path.rel(), stmt.Header) != nil {
			serializedBase = headerInput.rel()
		}

		_, secondLevel := protoGenHeaders[headerInput.rel()]
		serializedCPPPath := build(serializedBase, "_serialized.cpp")

		var serializedHPath VFS

		if withHeader {
			serializedHPath = build(serializedBase, "_serialized.h")
		}

		enRef := ctx.emit.reserve()

		cppParsed := []IncludeDirective{
			{kind: includeQuoted, target: internStr(headerInput.rel())},
			{kind: includeQuoted, target: strUtilGenericSerializedEnumH},
		}

		sort.Slice(cppParsed, func(i, j int) bool { return cppParsed[i].target.string() < cppParsed[j].target.string() })

		reg := ctx.codegenFor(instance)

		reg.register(&GeneratedFileInfo{
			OutputPath:     serializedCPPPath,
			ProducerRef:    enRef,
			GeneratorRefs:  []NodeRef{enumParserLD},
			ParsedIncludes: cppParsed,
		})

		if withHeader {
			hParsed := []IncludeDirective{
				{kind: includeQuoted, target: internStr(headerInput.rel())},
				{kind: includeQuoted, target: internStr(serializedCPPPath.rel())},
			}

			sort.Slice(hParsed, func(i, j int) bool { return hParsed[i].target.string() < hParsed[j].target.string() })

			reg.register(&GeneratedFileInfo{
				OutputPath:     serializedHPath,
				ProducerRef:    enRef,
				GeneratorRefs:  []NodeRef{enumParserLD},
				ParsedIncludes: hParsed,
			})
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
		closure := walkClosure(ctx.scannerFor(instance), p.headerInput, scanCfg)

		var ownOutputClosure []VFS

		if !p.withHeader {
			ownOutputClosure = walkClosureTail(ctx.scannerFor(instance), p.serializedCPPPath, scanCfg)
		}

		enClosure := dedup(closure, ownOutputClosure)
		augmentedDepENRefs := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, enClosure)

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

		se := emitOneSource(ctx, instance, d, p.serializedCPPPath.str())
		ccRef, ccOut := se.Ref, se.OutPath

		res.CCRefs = append(res.CCRefs, ccRef)
		res.CCOutputs = append(res.CCOutputs, ccOut)
		res.Seqs = append(res.Seqs, p.declSeq)
		res.SecondLevel = append(res.SecondLevel, p.secondLevel)
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
	emit *StreamingEmitter,
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
		Env:            env,
		Inputs:         na.inputList(na.vfsList(enumParserBin), headerIncludeClosure),
		KV:             &enKV,
		Outputs:        outputs,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        deps,
		ForeignDepRefs: foreignDepRefs,
	}

	emit.emitReserved(node, id)
}
