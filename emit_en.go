package main

import (
	"slices"
	"strings"
)

var enKV = KV{P: pkEN, PC: pcYellow}

func (e *EmitContext) enumHeaderSourceInput(header ANY, srcDirs []VFS) VFS {
	ctx, instance := e.ctx, e.instance
	headerInput := resolveSourceANY(ctx, instance, header, srcDirs).Path

	if !ctx.fs.isFileVFS(headerInput) {
		if vfs, ok := sourceInputVFS(ctx.fs, instance.Path, header.string()); ok && vfs.isSource() {
			headerInput = vfs
		}
	}

	return headerInput
}

func enumHeaderBuildClean(header ANY) VFS {
	if header.vfs() == 0 {
		return header.str().buildClean()
	}

	return buildClean(header.string())
}

func (e *EmitContext) resolveEnumHeaderInput(header ANY, srcDirs []VFS) VFS {
	headerInput := e.enumHeaderSourceInput(header, srcDirs)
	buildHeader := headerInput.rel().build()

	if e.codegen.lookup(buildHeader) != nil {
		return buildHeader
	}

	rootHeader := enumHeaderBuildClean(header)

	if e.codegen.lookup(rootHeader) != nil {
		return rootHeader
	}

	return headerInput
}

func (e *EmitContext) enumSerializedBaseParts(stmt *GenerateEnumSerializationStmt) (dir, sep, base string) {
	header := stmt.Header.string()

	if _, ok := moduleRootedVFS(e.instance.Path.relString(), header); ok {
		return e.enumHeaderSourceInput(stmt.Header, e.d.srcDirs).relString(), "", ""
	}

	if !pathIsClean(header) {
		return buildJoinClean(e.instance.Path.relString(), header).relString(), "", ""
	}

	return e.instance.Path.relString(), "/", header
}

func (e *EmitContext) emitEnumSrcStmt(stmt *GenerateEnumSerializationStmt) {
	if stmt.Variant == "noexcept_header" {
		e.emitAliceEnumSrcStmt(stmt)

		return
	}

	if isPythonProtoModule(e.d) {
		return
	}

	ctx, d := e.ctx, e.d
	withHeader := stmt.Variant == "with_header"
	headerInput := e.resolveEnumHeaderInput(stmt.Header, d.srcDirs)
	baseDir, baseSep, baseName := e.enumSerializedBaseParts(stmt)
	serializedCPPPath := build(baseDir, baseSep, baseName, "_serialized.cpp")

	var serializedHPath VFS

	if withHeader {
		serializedHPath = build(baseDir, baseSep, baseName, "_serialized.h")
	}

	enRef := ctx.emit.reserve()

	cppParsed := e.ctx.na.dirList(
		IncludeDirective{kind: includeQuoted, target: includeTarget(headerInput.rel().any())})

	if !withHeader {
		cppParsed = e.ctx.na.dirList(cppParsed[0],
			IncludeDirective{kind: includeQuoted, target: includeTarget(strUtilGenericSerializedEnumH.any())})

		slices.SortFunc(cppParsed, func(a, b IncludeDirective) int { return strings.Compare(a.target.string(), b.target.string()) })
	}

	codegen := e.codegen

	e.enqueueSrc(SrcMeta{Source: serializedCPPPath.any(), Prio: stmtPrioDefault})

	pe := e.pendingEmit(func() {
		enumParserLD, enumParserBin := e.tool(argToolsEnumParserEnumParser)
		generatorRefs := ctx.na.refList(enumParserLD)
		cppInfo := codegen.lookup(serializedCPPPath)

		cppInfo.GeneratorRefs = generatorRefs

		if withHeader {
			codegen.lookup(serializedHPath).GeneratorRefs = generatorRefs
		}

		e.emitEN(
			headerInput,
			serializedCPPPath,
			serializedHPath,
			withHeader,
			enumParserLD,
			enumParserBin,
			enRef,
		)
	})

	e.collectCPPStyleInputs(headerInput)

	e.register(GeneratedFileSpec{
		OutputPath:      serializedCPPPath,
		SourcePath:      headerInput,
		ProducerRef:     enRef,
		ProducerMainOut: serializedCPPPath,
		ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: cppParsed},
		OnUse:           pe,
	})

	if withHeader {
		e.register(GeneratedFileSpec{
			OutputPath:      serializedHPath,
			SourcePath:      headerInput,
			ProducerRef:     enRef,
			ProducerMainOut: serializedCPPPath,
			ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: cppParsed},
			OnUse:           pe,
		})
	}
}

func (e *EmitContext) emitAliceEnumSrcStmt(stmt *GenerateEnumSerializationStmt) {
	ctx := e.ctx
	na := ctx.na
	header := e.resolveEnumHeaderInput(stmt.Header, e.d.srcDirs)
	dir, sep, base := e.enumSerializedBaseParts(stmt)
	output := build(dir, sep, base, "_serialized.h")
	ref := ctx.emit.reserve()

	var info *GeneratedFileInfo

	pending := e.pendingEmit(func() {
		toolRef, toolBin := e.tool(argYandexIoToolsEnumSerializerGenerator)
		inputs := na.inputList(na.vfsList(toolBin), na.vfsList(header))

		args := na.anyList(toolBin.any(), strInputPath.any(), header.any(),
			strIncludePath.any(), header.rel().any(), strOutputPath.any(), output.any())

		info.GeneratorRefs = na.refList(toolRef)

		e.emitReservedNode(NodeSpec{Node: Node{Platform: e.instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(args), Env: envVarsVCS}),
			Env:  envVarsVCS, Inputs: inputs, Outputs: na.vfsList(output),
			ForeignDepRefs: na.refList(toolRef),
			KV:             &enKV}, input: InputSpec{roots: inputs[1:]},
		}, ref)
	})

	info = e.register(GeneratedFileSpec{
		OutputPath:     output,
		SourcePath:     header,
		ProducerRef:    ref,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: na.dirList(IncludeDirective{kind: includeQuoted, target: includeTarget(header.any())})},
		OnUse:          pending,
	})
}

func (e *EmitContext) emitEN(
	headerInput VFS,
	serializedCPPVFS VFS,
	serializedHVFS VFS,
	withHeader bool,
	enumParserLD NodeRef,
	enumParserBin VFS,
	id NodeRef,
) {
	na := e.ctx.na
	cmdArgs := na.anys.alloc(8)[:0]

	cmdArgs = append(cmdArgs,
		enumParserBin.any(),
		headerInput.any(),
		argIncludePath.any(),
		headerInput.rel().any(),
		argOutput.any(),
		serializedCPPVFS.any(),
	)

	if withHeader {
		cmdArgs = append(cmdArgs, argHeader.any(), serializedHVFS.any())
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
	foreignDepRefs := na.refList(enumParserLD)
	inputs := na.inputList(na.vfsList(enumParserBin), na.vfsList(headerInput))

	node := NodeSpec{Node: Node{Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env: env, Inputs: inputs, KV: &enKV,
		Outputs:        outputs,
		ForeignDepRefs: foreignDepRefs}, input: InputSpec{roots: inputs[1:]},
	}

	e.emitReservedNode(node, id)
}
