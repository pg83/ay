package main

import (
	"path/filepath"
	"strings"
)

var (
	antlr4RuntimeHeaderPath = antlr4RuntimeHeaderVFS.String()
	antlr4JarPath           = antlr4JarVFS.String()
	antlr3JarPath           = antlr3JarVFS.String()
	stdout2stderrPath       = stdout2stderrVFS.String()
)

// antlrJavaConstHead is the constant [stdout2stderr.py, <jdk>, -jar,
// <antlr4 jar>] lead of every antlr (JV) command, after the python3 token.
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
	reg := codegenRegForInstance(ctx, instance)

	for _, pair := range cpccPairs {
		srcCpp := pair.cpp
		srcH := pair.h

		base := strings.TrimSuffix(filepath.Base(srcCpp.rel()), ".cpp")
		g4CppPath := Build(instance.Path.rel() + "/" + base + ".g4.cpp")
		g4CppRel := base + ".g4.cpp"

		if reg != nil {
			emits := make([]IncludeDirective, 0, 1+len(outputIncludes))
			emits = append(emits, IncludeDirective{kind: includeQuoted, target: internStr(antlr4RuntimeHeaderVFS.rel())})

			for _, h := range outputIncludes {
				emits = append(emits, IncludeDirective{kind: includeQuoted, target: internStr(h)})
			}

			registerGeneratedParsedOutput(ctx, instance, pkCP, g4CppPath, emits, nil)
		}

		ccIn := in
		ccIn.ExtraDepRefs = nil
		closure := walkClosure(ctx, instance, g4CppPath, ccIn)

		// The CP node's inputs take the tail: g4CppPath is its own output (a
		// build output — never an SCC member, so the window leads with it).
		cpClosure := closure

		if len(cpClosure) > 0 {
			cpClosure = cpClosure[1:]
		}

		cpRef := EmitJVCPG4(instance, srcCpp, g4CppPath, jvRef, jvPrimary, jvInputs, cpClosure, in.TC, ctx.scripts, ctx.emit)

		ccIncludeInputs := make([]VFS, 0, 3+len(jvInputs)+len(closure)+2)
		ccIncludeInputs = append(ccIncludeInputs, jvPrimary)
		ccIncludeInputs = append(ccIncludeInputs, srcH)
		ccIncludeInputs = append(ccIncludeInputs, ctx.scripts[antlr4FsToolsVFS]...)
		ccIncludeInputs = append(ccIncludeInputs, jvInputs...)
		ccIncludeInputs = append(ccIncludeInputs, closure...)

		ccIn.IncludeInputs = ccIncludeInputs
		ccIn.ExtraDepRefs = []NodeRef{jvRef, cpRef}
		ccIn.PerSourceCFlags = []ARG{argWnoUnusedVariable}
		ccRef, ccOut, _ := EmitCC(instance, g4CppRel, g4CppPath, ccIn, ctx.host, ctx.emit)

		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)
	}

	return
}

const jdkResourcePath = "$(JDK17)/bin/java"

func emitJVNode(instance ModuleInstance, cmdArgs []STR, inputs InputChunks, outputs []VFS, cwd string, depRefs []NodeRef, moduleTag STR, emit Emitter) NodeRef {
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: ArgChunks{cmdArgs},
				Env:     env,
				Cwd:     internStr(cwd),
			},
		},
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
		Requirements:  Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:       depRefs,
		usesResources: []string{resourcePatternYMakePython3, resourcePatternJDK17},
	}

	return emit.emit(node)
}

func EmitJV(
	instance ModuleInstance,
	grammar string,
	options []string,
	visitor bool,
	listener bool,
	moduleTag STR,
	tc ModuleToolchain,
	emit Emitter,
) NodeRef {
	grammarVFS := Source(instance.Path.rel() + "/" + grammar)
	outDirVFS := Build(instance.Path.rel())
	outDir := outDirVFS.String()

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

	inputs := InputChunks{{
		grammarVFS,
		stdout2stderrVFS,
		antlr4JarVFS,
	}}

	base := strings.TrimSuffix(filepath.Base(grammar), ".g4")
	outPrefix := instance.Path.rel() + "/" + base
	outputs := []VFS{
		Build(outPrefix + "Lexer.cpp"),
		Build(outPrefix + "Lexer.h"),
		Build(outPrefix + "Parser.cpp"),
		Build(outPrefix + "Parser.h"),
		Build(outPrefix + "Visitor.h"),
		Build(outPrefix + "BaseVisitor.h"),
	}

	return emitJVNode(instance, cmdArgs, inputs, outputs, outDir, nil, moduleTag, emit)
}

func EmitJVSplit(
	instance ModuleInstance,
	lexer string,
	parser string,
	visitor bool,
	listener bool,
	moduleTag STR,
	tc ModuleToolchain,
	emit Emitter,
) NodeRef {
	lexerVFS := Source(instance.Path.rel() + "/" + lexer)
	parserVFS := Source(instance.Path.rel() + "/" + parser)
	outDirVFS := Build(instance.Path.rel())
	outDir := outDirVFS.String()

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

	inputs := InputChunks{{
		lexerVFS,
		parserVFS,
		stdout2stderrVFS,
		antlr4JarVFS,
	}}

	lexerBase := strings.TrimSuffix(filepath.Base(lexer), ".g4")
	parserBase := strings.TrimSuffix(filepath.Base(parser), ".g4")
	visitorBase := parserBase
	outPrefix := instance.Path.rel() + "/"
	outputs := []VFS{
		Build(outPrefix + lexerBase + ".cpp"),
		Build(outPrefix + lexerBase + ".h"),
		Build(outPrefix + parserBase + ".cpp"),
		Build(outPrefix + parserBase + ".h"),
		Build(outPrefix + visitorBase + "Visitor.h"),
		Build(outPrefix + visitorBase + "BaseVisitor.h"),
	}

	return emitJVNode(instance, cmdArgs, inputs, outputs, outDir, nil, moduleTag, emit)
}

func EmitJVGeneral(
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
	cmdArgs := make([]STR, 0, 5+len(args))
	cmdArgs = append(cmdArgs,
		tc.Python3,
		internStr(stdout2stderrPath),
		internStr(jdkResourcePath),
		argJar.str(),
		(jarVFS).str(),
	)
	cmdArgs = appendInternStrs(cmdArgs, args)

	// inputs is the caller's slice — referenced as its own chunk, never copied.
	jvInputs := InputChunks{inputs, {stdout2stderrVFS, jarVFS}}

	return emitJVNode(instance, cmdArgs, jvInputs, outputs, cwd, depRefs, moduleTag, emit)
}
