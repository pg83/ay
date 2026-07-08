package main

import (
	"slices"
	"strings"
)

var enKV = KV{P: pkEN, PC: pcYellow}

func (e *EmitContext) moduleProtoGenHeaders() map[STR]struct{} {
	ctx, instance, d := e.ctx, e.instance, e.d

	var set map[STR]struct{}

	add := func(h STR) {
		if set == nil {
			set = map[STR]struct{}{}
		}

		set[h] = struct{}{}
	}

	for _, src := range d.srcs {
		s := src.string()

		switch {
		case extIsProto(s):
			base := strings.TrimSuffix(protoSourceRelPath(ctx.fs, instance, d, s), ".proto")

			add(internV(base, ".pb.h"))
		case extIsEv(s):
			add(internV(protoSourceRelPath(ctx.fs, instance, d, s), ".pb.h"))
		}
	}

	return set
}

func (e *EmitContext) enumHeaderSourceInput(headerRel string, srcDirs []VFS) VFS {
	ctx, instance := e.ctx, e.instance
	headerInput := resolveSourceVFS(ctx, instance, headerRel, srcDirs)

	if !ctx.fs.isFile(srcRootRel, headerInput.relString()) {
		if vfs, ok := sourceInputVFS(ctx.fs, instance.Path, headerRel); ok && vfs.isSource() {
			headerInput = vfs
		}
	}

	return headerInput
}

func (e *EmitContext) resolveEnumHeaderInput(headerRel string, srcDirs []VFS) VFS {
	headerInput := e.enumHeaderSourceInput(headerRel, srcDirs)
	buildHeader := headerInput.rel().build()

	if e.codegen.lookup(buildHeader) != nil {
		return buildHeader
	}

	return headerInput
}

func (e *EmitContext) enumSerializedBaseParts(stmt *GenerateEnumSerializationStmt) (dir, sep, base string) {
	if _, ok := moduleRootedVFS(e.instance.Path.relString(), stmt.Header); ok {
		return e.enumHeaderSourceInput(stmt.Header, e.d.srcDirs).relString(), "", ""
	}

	return e.instance.Path.relString(), "/", stmt.Header
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
	baseDir, baseSep, baseName := e.enumSerializedBaseParts(stmt)
	_, secondLevel := protoGenHeaders[headerInput.rel()]
	serializedCPPPath := build(baseDir, baseSep, baseName, "_serialized.cpp")

	var serializedHPath VFS

	if withHeader {
		serializedHPath = build(baseDir, baseSep, baseName, "_serialized.h")
	}

	enRef := ctx.emit.reserve()

	cppParsed := e.ctx.na.dirList(
		IncludeDirective{kind: includeQuoted, target: includeTarget(headerInput.rel().any())},
		IncludeDirective{kind: includeQuoted, target: includeTarget(strUtilGenericSerializedEnumH.any())})

	slices.SortFunc(cppParsed, func(a, b IncludeDirective) int { return strings.Compare(a.target.string(), b.target.string()) })

	reg := e.codegen

	reg.register(GeneratedFileInfo{
		OutputPath:     serializedCPPPath,
		ProducerRef:    enRef,
		GeneratorRefs:  e.ctx.na.refList(enumParserLD),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cppParsed},
	})

	if withHeader {
		hParsed := e.ctx.na.dirList(
			IncludeDirective{kind: includeQuoted, target: includeTarget(headerInput.rel().any())},
			IncludeDirective{kind: includeQuoted, target: includeTarget(serializedCPPPath.rel().any())})

		slices.SortFunc(hParsed, func(a, b IncludeDirective) int { return strings.Compare(a.target.string(), b.target.string()) })

		reg.register(GeneratedFileInfo{
			OutputPath:     serializedHPath,
			ProducerRef:    enRef,
			GeneratorRefs:  e.ctx.na.refList(enumParserLD),
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
			enClosure = dedupClosure(ctx.na, []VFS{headerClosure.self}, headerClosure.buckets)
		} else {
			ownCV := walkClosure(e.scanner, serializedCPPPath, scanCfg)

			enClosure = dedupClosure(ctx.na, []VFS{headerClosure.self}, headerClosure.buckets, ownCV.buckets)
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

		e.enqueueSrc(SrcMeta{Source: serializedCPPPath.any(), Prio: stmtPrioDefault, Seq: declSeq, Generated: true, SecondLevel: secondLevel})
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
	cmdArgs := na.anys.alloc(8)[:0]

	cmdArgs = append(cmdArgs,
		(enumParserBin).any(),
		(headerInput).any(),
		argIncludePath.any(),
		headerInput.rel().any(),
		argOutput.any(),
		(serializedCPPVFS).any(),
	)

	if withHeader {
		cmdArgs = append(cmdArgs, argHeader.any(), (serializedHVFS).any())
	}

	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	var outputs []VFS

	if withHeader {
		outputs = na.vfsList(serializedCPPVFS, serializedHVFS)
	} else {
		outputs = na.vfsList(serializedCPPVFS)
	}

	env := envVarsVCS
	deps := na.noderefs.list(depENRefs...)
	foreignDepRefs := na.refList(enumParserLD)

	node := Node{
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

	emit.emitReservedNode(node, id)
}
