package main

import (
	"sort"
	"strings"
)

var enKV = KV{P: pkEN, PC: pcYellow}

func (e *EmitContext) moduleProtoGenHeaders() map[string]struct{} {
	ctx, instance, d := e.ctx, e.instance, e.d

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

func (e *EmitContext) enumHeaderSourceInput(headerRel string, srcDirs []VFS) VFS {
	ctx, instance := e.ctx, e.instance
	headerInput := resolveSourceVFS(ctx, instance, headerRel, srcDirs)

	if !ctx.fs.isFile(srcRootVFS, headerInput.relString()) {
		if vfs := sourceInputVFS(ctx.fs, instance.Path, headerRel); vfs != nil && vfs.isSource() {
			headerInput = *vfs
		}
	}

	return headerInput
}

func (e *EmitContext) resolveEnumHeaderInput(headerRel string, srcDirs []VFS) VFS {
	headerInput := e.enumHeaderSourceInput(headerRel, srcDirs)
	buildHeader := build(headerInput.relString())

	if e.codegen.lookup(buildHeader) != nil {
		return buildHeader
	}

	return headerInput
}

func (e *EmitContext) enumSerializedBase(stmt *GenerateEnumSerializationStmt) string {
	if moduleRootedVFS(e.instance.Path.relString(), stmt.Header) != nil {
		return e.enumHeaderSourceInput(stmt.Header, e.d.srcDirs).relString()
	}

	return e.instance.Path.relString() + "/" + stmt.Header
}

func (e *EmitContext) emitEnumSrcStmt(stmt *GenerateEnumSerializationStmt) {
	if e.d.unit.Tag == unitTagPy3Proto {
		return
	}

	ctx, instance, d := e.ctx, e.instance, e.d
	enumParserLD, enumParserBin := ctx.tool(argToolsEnumParserEnumParser)
	scanCfg := newScanContext(ctx.parsers, d.addIncl, e.peers.SelfAddInclGlobal, includeScannerBasePaths(), instance.Path.relString())
	protoGenHeaders := e.moduleProtoGenHeaders()
	withHeader := stmt.Variant == "with_header"
	headerInput := e.resolveEnumHeaderInput(stmt.Header, d.srcDirs)
	serializedBase := e.enumSerializedBase(stmt)
	_, secondLevel := protoGenHeaders[headerInput.relString()]
	serializedCPPPath := build(serializedBase, "_serialized.cpp")

	var serializedHPath VFS

	if withHeader {
		serializedHPath = build(serializedBase, "_serialized.h")
	}

	enRef := ctx.emit.reserve()

	cppParsed := []IncludeDirective{
		{kind: includeQuoted, target: internStr(headerInput.relString())},
		{kind: includeQuoted, target: strUtilGenericSerializedEnumH},
	}

	sort.Slice(cppParsed, func(i, j int) bool { return cppParsed[i].target.string() < cppParsed[j].target.string() })

	reg := e.codegen

	reg.register(&GeneratedFileInfo{
		OutputPath:     serializedCPPPath,
		ProducerRef:    enRef,
		GeneratorRefs:  []NodeRef{enumParserLD},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cppParsed},
	})

	if withHeader {
		hParsed := []IncludeDirective{
			{kind: includeQuoted, target: internStr(headerInput.relString())},
			{kind: includeQuoted, target: internStr(serializedCPPPath.relString())},
		}

		sort.Slice(hParsed, func(i, j int) bool { return hParsed[i].target.string() < hParsed[j].target.string() })

		reg.register(&GeneratedFileInfo{
			OutputPath:     serializedHPath,
			ProducerRef:    enRef,
			GeneratorRefs:  []NodeRef{enumParserLD},
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: hParsed},
		})
	}

	var moduleTag STR

	if d.moduleStmt.Name == tokProtoLibrary {
		moduleTag = tagCppProto
	}

	declSeq := stmt.DeclSeq

	e.deferPass2(func() {
		headerClosure := walkClosure(e.scanner, headerInput, scanCfg)

		var enClosure []VFS

		if withHeader {
			enClosure = dedupClosure([]VFS{headerClosure.self}, headerClosure.buckets)
		} else {
			ownCV := walkClosure(e.scanner, serializedCPPPath, scanCfg)

			enClosure = dedupClosure([]VFS{headerClosure.self}, headerClosure.buckets, ownCV.buckets)
		}

		augmentedDepENRefs := resolveCodegenDepRefsIncl(ctx, instance, ctx.na, enClosure)

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

		e.enqueueSrc(SrcMeta{Source: serializedCPPPath.fullSTR(), Prio: stmtPrioDefault, Seq: declSeq, Generated: true, SecondLevel: secondLevel})
	})
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
		(enumParserBin).fullSTR(),
		(headerInput).fullSTR(),
		argIncludePath.str(),
		internStr(headerInput.relString()),
		argOutput.str(),
		(serializedCPPVFS).fullSTR(),
	}

	outputs := []VFS{serializedCPPVFS}

	if withHeader {
		cmdArgs = append(cmdArgs, argHeader.str(), (serializedHVFS).fullSTR())
		outputs = append(outputs, serializedHVFS)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	deps := append([]NodeRef(nil), depENRefs...)
	foreignDepRefs := depRefs(enumParserLD)

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkListSTR(cmdArgs),
			Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(enumParserBin), headerIncludeClosure),
		KV:             &enKV,
		Outputs:        outputs,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:        deps,
		ForeignDepRefs: foreignDepRefs,
	}

	emit.emitReservedNode(node, id)
}
