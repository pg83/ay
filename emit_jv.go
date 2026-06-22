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
)

var antlrJavaConstHead = []STR{
	internStr(stdout2stderrPath),
	internStr(jdkResourcePath),
	argJar.str(),
	internStr(antlr4JarPath),
}

func emitJVDownstreamCPCC(
	ctx *GenCtx,
	instance ModuleInstance,
	jvRef NodeRef,
	jvPrimary VFS,
	jvInputs []VFS,
	cpccPairs []struct{ cpp, h VFS },
	outputIncludes []string,
	in ModuleCCInputs,
) (ccRefs []NodeRef, ccOutputs []VFS) {
	for _, pair := range cpccPairs {
		srcCpp := pair.cpp
		srcH := pair.h

		base := strings.TrimSuffix(filepath.Base(srcCpp.rel()), ".cpp")
		g4CppPath := build(instance.Path.rel() + "/" + base + ".g4.cpp")
		g4CppRel := base + ".g4.cpp"

		cpRef := ctx.emit.reserve()

		{
			emits := make([]IncludeDirective, 0, 1+len(outputIncludes))
			emits = append(emits, IncludeDirective{kind: includeQuoted, target: internStr(antlr4RuntimeHeaderVFS.rel())})

			for _, h := range outputIncludes {
				emits = append(emits, IncludeDirective{kind: includeQuoted, target: internStr(h)})
			}

			registerBoundGeneratedParsedOutput(ctx, instance, pkCP, g4CppPath, emits, cpRef, nil)
		}

		ccIn := in
		ccIn.ExtraDepRefs = nil
		closure := walkClosure(ctx.scannerFor(instance), g4CppPath, ccIn.ScanCfg)

		cpClosure := closure

		if len(cpClosure) > 0 {
			cpClosure = cpClosure[1:]
		}

		emitJVCPG4(instance, srcCpp, g4CppPath, jvRef, jvPrimary, jvInputs, cpClosure, cpRef, in.TC, ctx.scripts, ctx.emit)

		ccIncludeInputs := make([]VFS, 0, 3+len(jvInputs)+len(closure)+2)
		ccIncludeInputs = append(ccIncludeInputs, jvPrimary)
		ccIncludeInputs = append(ccIncludeInputs, srcH)
		ccIncludeInputs = append(ccIncludeInputs, ctx.scripts[antlr4FsToolsVFS]...)
		ccIncludeInputs = append(ccIncludeInputs, jvInputs...)
		ccIncludeInputs = append(ccIncludeInputs, closure...)

		ccIn.IncludeInputs = ccIncludeInputs
		ccIn.ExtraDepRefs = []NodeRef{jvRef, cpRef}
		ccIn.PerSourceCFlags = []ARG{argWnoUnusedVariable}
		ccRef, ccOut, _ := emitCC(instance, g4CppRel, g4CppPath, ccIn, ctx.host, ctx.emit)

		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)
	}

	return
}

const jdkResourcePath = "$(JDK17)/bin/java"

func emitJVNode(instance ModuleInstance, cmdArgs []STR, inputs InputChunks, outputs []VFS, cwd string, depRefs []NodeRef, moduleTag STR, emit Emitter) NodeRef {
	na := emit.nodeArenas()

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env,
			Cwd: internStr(cwd)}),
		Env:     env,
		Inputs:  inputs,
		KV:      KV{P: pkJV, PC: pcLightBlue, ShowOut: true},
		Outputs: outputs,
		TargetProperties: func() TargetProperties {
			tp := TargetProperties{ModuleDir: instance.Path.rel()}

			if moduleTag != 0 {
				tp.ModuleTag = moduleTag
			}

			return tp
		}(),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      depRefs,
		Resources:    usesPython3JDK17,
	}

	return emit.emit(node)
}

func emitJV(
	instance ModuleInstance,
	grammar string,
	options []string,
	visitor bool,
	listener bool,
	moduleTag STR,
	tc ModuleToolchain,
	emit Emitter,
) NodeRef {
	na := emit.nodeArenas()

	grammarVFS := source(instance.Path.rel() + "/" + grammar)
	outDirVFS := build(instance.Path.rel())
	outDir := outDirVFS.string()

	cmdArgs := make([]STR, 0, 8+len(antlrJavaConstHead))
	cmdArgs = append(cmdArgs, tc.Python3)
	cmdArgs = append(cmdArgs, antlrJavaConstHead...)
	cmdArgs = append(cmdArgs,
		(grammarVFS).str(),
		argDlanguageCpp.str(),
		argDashO.str(),
		internStr(outDir),
	)

	if visitor {
		cmdArgs = append(cmdArgs, argVisitor.str())
	}

	if !listener {
		cmdArgs = append(cmdArgs, argNoListener.str())
	} else {
		cmdArgs = append(cmdArgs, argListener.str())
	}

	cmdArgs = appendInternStrs(cmdArgs, options)

	inputs := na.inputList(na.vfsList(grammarVFS,
		stdout2stderrVFS,
		antlr4JarVFS))

	base := strings.TrimSuffix(filepath.Base(grammar), ".g4")
	outPrefix := instance.Path.rel() + "/" + base
	outputs := []VFS{
		build(outPrefix + "Lexer.cpp"),
		build(outPrefix + "Lexer.h"),
		build(outPrefix + "Parser.cpp"),
		build(outPrefix + "Parser.h"),
		build(outPrefix + "Visitor.h"),
		build(outPrefix + "BaseVisitor.h"),
	}

	return emitJVNode(instance, cmdArgs, inputs, outputs, outDir, nil, moduleTag, emit)
}

func emitJVSplit(
	instance ModuleInstance,
	lexer string,
	parser string,
	visitor bool,
	listener bool,
	moduleTag STR,
	tc ModuleToolchain,
	emit Emitter,
) NodeRef {
	na := emit.nodeArenas()

	lexerVFS := source(instance.Path.rel() + "/" + lexer)
	parserVFS := source(instance.Path.rel() + "/" + parser)
	outDirVFS := build(instance.Path.rel())
	outDir := outDirVFS.string()

	cmdArgs := []STR{
		tc.Python3,
		internStr(stdout2stderrPath),
		internStr(jdkResourcePath),
		argJar.str(),
		internStr(antlr4JarPath),
		(lexerVFS).str(),
		(parserVFS).str(),
		argDlanguageCpp.str(),
		argDashO.str(),
		internStr(outDir),
	}

	if visitor {
		cmdArgs = append(cmdArgs, argVisitor.str())
	}

	if !listener {
		cmdArgs = append(cmdArgs, argNoListener.str())
	} else {
		cmdArgs = append(cmdArgs, argListener.str())
	}

	inputs := na.inputList(na.vfsList(lexerVFS,
		parserVFS,
		stdout2stderrVFS,
		antlr4JarVFS))

	lexerBase := strings.TrimSuffix(filepath.Base(lexer), ".g4")
	parserBase := strings.TrimSuffix(filepath.Base(parser), ".g4")
	visitorBase := parserBase
	outPrefix := instance.Path.rel() + "/"
	outputs := []VFS{
		build(outPrefix + lexerBase + ".cpp"),
		build(outPrefix + lexerBase + ".h"),
		build(outPrefix + parserBase + ".cpp"),
		build(outPrefix + parserBase + ".h"),
		build(outPrefix + visitorBase + "Visitor.h"),
		build(outPrefix + visitorBase + "BaseVisitor.h"),
	}

	return emitJVNode(instance, cmdArgs, inputs, outputs, outDir, nil, moduleTag, emit)
}

func emitJVGeneral(
	instance ModuleInstance,
	jarVFS VFS,
	args []string,
	inputs []VFS,
	outputs []VFS,
	cwd string,
	depRefs []NodeRef,
	moduleTag STR,
	tc ModuleToolchain,
	emit Emitter,
) NodeRef {
	na := emit.nodeArenas()

	cmdArgs := make([]STR, 0, 5+len(args))
	cmdArgs = append(cmdArgs,
		tc.Python3,
		internStr(stdout2stderrPath),
		internStr(jdkResourcePath),
		argJar.str(),
		(jarVFS).str(),
	)
	cmdArgs = appendInternStrs(cmdArgs, args)

	jvInputs := na.inputList(inputs, na.vfsList(stdout2stderrVFS, jarVFS))

	return emitJVNode(instance, cmdArgs, jvInputs, outputs, cwd, depRefs, moduleTag, emit)
}
