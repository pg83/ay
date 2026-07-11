package main

import (
	"slices"
	"strings"
)

var enKV = KV{P: pkEN, PC: pcYellow}

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
		IncludeDirective{kind: includeQuoted, target: includeTarget(headerInput.rel().any())},
		IncludeDirective{kind: includeQuoted, target: includeTarget(strUtilGenericSerializedEnumH.any())})

	slices.SortFunc(cppParsed, func(a, b IncludeDirective) int { return strings.Compare(a.target.string(), b.target.string()) })

	var moduleTag STR

	if d.moduleStmt.Name == tokProtoLibrary {
		moduleTag = tagCppProto
	}

	codegen := e.codegen

	e.enqueueSrc(SrcMeta{Source: serializedCPPPath.any(), Prio: stmtPrioDefault, Seq: stmt.DeclSeq})

	pe := ctx.na.pendingEmit(func() {
		enumParserLD, enumParserBin := ctx.tool(argToolsEnumParserEnumParser)
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
			moduleTag,
			withHeader,
			enumParserLD,
			enumParserBin,
			enRef,
		)
	})

	e.register(GeneratedFileInfo{
		OutputPath:     serializedCPPPath,
		SourcePath:     headerInput,
		ProducerRef:    enRef,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cppParsed},
		OnUse:          pe,
	})

	if withHeader {
		hParsed := e.ctx.na.dirList(
			IncludeDirective{kind: includeQuoted, target: includeTarget(headerInput.rel().any())},
			IncludeDirective{kind: includeQuoted, target: includeTarget(serializedCPPPath.rel().any())})

		slices.SortFunc(hParsed, func(a, b IncludeDirective) int { return strings.Compare(a.target.string(), b.target.string()) })

		e.register(GeneratedFileInfo{
			OutputPath:     serializedHPath,
			SourcePath:     headerInput,
			ProducerRef:    enRef,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: hParsed},
			OnUse:          pe,
		})
	}
}

func (e *EmitContext) emitEN(
	headerInput VFS,
	serializedCPPVFS VFS,
	serializedHVFS VFS,
	moduleTag STR,
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

	node := Node{
		Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(enumParserBin, headerInput)),
		KV:             &enKV,
		Outputs:        outputs,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: foreignDepRefs,
	}

	e.emitReservedNode(node, id)
}
