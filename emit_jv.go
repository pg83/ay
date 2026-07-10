package main

import (
	"path/filepath"
	"strings"
)

var (
	antlr4RuntimeHeaderPath = antlr4RuntimeHeaderVFS.string()
	antlr4JarPath           = antlr4JarVFS.string()
	antlr3JarPath           = antlr3JarVFS.string()
	stdout2stderrPath       = stdout2stderrVFS.string()
	jvKV                    = KV{P: pkJV, PC: pcLightBlue, ShowOut: true}
)

var antlrJavaConstHead = []ANY{
	internStr(stdout2stderrPath).any(),
	internStr(jdkResourcePath).any(),
	argJar.any(),
	internStr(antlr4JarPath).any(),
}

const jdkResourcePath = "$(B)/resources/JDK17/bin/java"

func (e *EmitContext) emitJVDownstreamCPCC(
	jvRef NodeRef,
	jvPrimary VFS,
	jvInputs []VFS,
	cpccPairs []struct{ cpp, h VFS },
	outputIncludes []string,
) {
	ctx, instance, d := e.ctx, e.instance, e.d

	for _, pair := range cpccPairs {
		srcCpp := pair.cpp
		srcH := pair.h
		base := strings.TrimSuffix(filepath.Base(srcCpp.relString()), ".cpp")
		g4CppPath := build(instance.Path.relString(), "/", base, ".g4.cpp")
		cpRef := ctx.emit.reserve()
		emits := ctx.na.dirs.alloc(1 + len(outputIncludes))[:0]

		emits = append(emits, IncludeDirective{kind: includeQuoted, target: includeTarget(antlr4RuntimeHeaderVFS.rel().any())})

		for _, h := range outputIncludes {
			emits = append(emits, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(h).any())})
		}

		ctx.na.dirs.commit(len(emits))

		emits = emits[:len(emits):len(emits)]

		fsTools := ctx.scripts[antlr4FsToolsVFS.rel()]
		leaves := ctx.na.vfs.alloc(2 + len(fsTools) + len(jvInputs))[:0]

		leaves = append(leaves, jvPrimary, srcH)
		leaves = append(leaves, fsTools...)
		leaves = append(leaves, jvInputs...)
		ctx.na.vfs.commit(len(leaves))
		leaves = leaves[:len(leaves):len(leaves)]

		scanner := e.scanner
		scanCfg := snapshotScanCfg(ctx.na, d.cc.ScanCfg)
		tc := d.cc.TC

		pe := func() {
			leafSet := make(map[VFS]bool, len(leaves))

			for _, l := range leaves {
				leafSet[l] = true
			}

			cpClosure := walkClosure(scanner, g4CppPath, scanCfg).collect(ctx.na, func(v VFS) bool {
				return v != g4CppPath && !leafSet[v]
			})

			emitJVCPG4(instance, srcCpp, g4CppPath, jvRef, jvPrimary, jvInputs, cpClosure, cpRef, tc, ctx.scripts, ctx.emit)
		}

		e.register(GeneratedFileInfo{
			OutputPath:     g4CppPath,
			ProducerRef:    cpRef,
			GeneratorRefs:  nil,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: emits},
			ClosureLeaves:  leaves,
			OnUse:          &pe,
		})

		e.enqueueSrc(SrcMeta{
			Source: g4CppPath.any(), Prio: stmtPrioDefault, Generated: true, Bucket: bkJV,
			Compile: CompileSpec{CFlags: e.ctx.na.anyList(argWnoUnusedVariable.any())},
		})
	}
}

func emitJVNodeReserved(instance ModuleInstance, cmdArgs []ANY, inputs InputChunks, outputs []VFS, cwd string, depRefs []NodeRef, moduleTag STR, emit *StreamingEmitter, id NodeRef) {
	na := emit.nodeArenas()
	env := envVarsVCS

	node := Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyChunkAny(cmdArgs)),
			Env: env,
			Cwd: cwdVFS(cwd)}),
		Env:          env,
		Inputs:       inputs,
		KV:           &jvKV,
		Outputs:      na.vfsList(outputs...),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      na.noderefs.list(depRefs...),
		Resources:    usesPython3JDK17,
	}

	emit.emitReservedNode(node, id)
}

func emitJVReserved(
	instance ModuleInstance,
	grammar string,
	options []string,
	visitor bool,
	listener bool,
	moduleTag STR,
	tc ModuleToolchain,
	emit *StreamingEmitter,
	id NodeRef,
) {
	na := emit.nodeArenas()
	grammarVFS := source(instance.Path.relString(), "/", grammar)
	outDirVFS := instance.Path.rel().build()
	outDir := outDirVFS.string()
	cmdArgs := make([]ANY, 0, 8+len(antlrJavaConstHead))

	cmdArgs = append(cmdArgs, tc.Python3.any())
	cmdArgs = append(cmdArgs, antlrJavaConstHead...)

	cmdArgs = append(cmdArgs,
		(grammarVFS).any(),
		argDlanguageCpp.any(),
		argDashO.any(),
		internStr(outDir).any(),
	)

	if visitor {
		cmdArgs = append(cmdArgs, argVisitor.any())
	}

	if !listener {
		cmdArgs = append(cmdArgs, argNoListener.any())
	} else {
		cmdArgs = append(cmdArgs, argListener.any())
	}

	cmdArgs = appendInternAnys(cmdArgs, options)

	inputs := na.inputList(na.vfsList(grammarVFS,
		stdout2stderrVFS,
		antlr4JarVFS))

	base := strings.TrimSuffix(filepath.Base(grammar), ".g4")
	outPrefix := instance.Path.relString() + "/" + base

	outputs := []VFS{
		build(outPrefix, "Lexer.cpp"),
		build(outPrefix, "Lexer.h"),
		build(outPrefix, "Parser.cpp"),
		build(outPrefix, "Parser.h"),
		build(outPrefix, "Visitor.h"),
		build(outPrefix, "BaseVisitor.h"),
	}

	emitJVNodeReserved(instance, cmdArgs, inputs, outputs, outDir, nil, moduleTag, emit, id)
}

func emitJVSplitReserved(
	instance ModuleInstance,
	lexer string,
	parser string,
	visitor bool,
	listener bool,
	moduleTag STR,
	tc ModuleToolchain,
	emit *StreamingEmitter,
	id NodeRef,
) {
	na := emit.nodeArenas()
	lexerVFS := source(instance.Path.relString(), "/", lexer)
	parserVFS := source(instance.Path.relString(), "/", parser)
	outDirVFS := instance.Path.rel().build()
	outDir := outDirVFS.string()

	cmdArgs := []ANY{
		tc.Python3.any(),
		internStr(stdout2stderrPath).any(),
		internStr(jdkResourcePath).any(),
		argJar.any(),
		internStr(antlr4JarPath).any(),
		(lexerVFS).any(),
		(parserVFS).any(),
		argDlanguageCpp.any(),
		argDashO.any(),
		internStr(outDir).any(),
	}

	if visitor {
		cmdArgs = append(cmdArgs, argVisitor.any())
	}

	if !listener {
		cmdArgs = append(cmdArgs, argNoListener.any())
	} else {
		cmdArgs = append(cmdArgs, argListener.any())
	}

	inputs := na.inputList(na.vfsList(lexerVFS,
		parserVFS,
		stdout2stderrVFS,
		antlr4JarVFS))

	lexerBase := strings.TrimSuffix(filepath.Base(lexer), ".g4")
	parserBase := strings.TrimSuffix(filepath.Base(parser), ".g4")
	visitorBase := parserBase
	outPrefix := instance.Path.relString() + "/"

	outputs := []VFS{
		build(outPrefix, lexerBase, ".cpp"),
		build(outPrefix, lexerBase, ".h"),
		build(outPrefix, parserBase, ".cpp"),
		build(outPrefix, parserBase, ".h"),
		build(outPrefix, visitorBase, "Visitor.h"),
		build(outPrefix, visitorBase, "BaseVisitor.h"),
	}

	emitJVNodeReserved(instance, cmdArgs, inputs, outputs, outDir, nil, moduleTag, emit, id)
}

func emitJVGeneralReserved(
	instance ModuleInstance,
	jarVFS VFS,
	args []string,
	inputs []VFS,
	outputs []VFS,
	cwd string,
	depRefs []NodeRef,
	moduleTag STR,
	tc ModuleToolchain,
	emit *StreamingEmitter,
	id NodeRef,
) {
	na := emit.nodeArenas()
	cmdArgs := make([]ANY, 0, 5+len(args))

	cmdArgs = append(cmdArgs,
		tc.Python3.any(),
		internStr(stdout2stderrPath).any(),
		internStr(jdkResourcePath).any(),
		argJar.any(),
		(jarVFS).any(),
	)

	cmdArgs = appendInternAnys(cmdArgs, args)

	jvInputs := na.inputList(na.vfsList(inputs...), na.vfsList(stdout2stderrVFS, jarVFS))

	emitJVNodeReserved(instance, cmdArgs, jvInputs, outputs, cwd, depRefs, moduleTag, emit, id)
}
