package main

import (
	"path/filepath"
	"strings"
)

var (
	antlr4JarPath     = antlr4JarVFS.String()
	antlr3JarPath     = antlr3JarVFS.String()
	stdout2stderrPath = stdout2stderrVFS.String()
)

const jdkResourcePath = "$(JDK17)/bin/java"

func emitJVNode(instance ModuleInstance, cmdArgs []STR, inputs inputChunks, outputs []VFS, cwd string, depRefs []NodeRef, moduleTag STR, emit Emitter) NodeRef {
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := &Node{
		Platform: instance.Platform,
		Cmds: []Cmd{
			{
				CmdArgs: argChunks{cmdArgs},
				Env:     env,
				Cwd:     internStr(cwd),
			},
		},
		Env:     env,
		Inputs:  inputs,
		KV:      KV{P: pkJV, PC: pcLightBlue, ShowOut: true},
		Outputs: outputs,
		TargetProperties: func() TargetProperties {
			tp := TargetProperties{ModuleDir: instance.Path.Rel()}

			if moduleTag != 0 {
				tp.ModuleTag = moduleTag
			}

			return tp
		}(),
		Requirements:  Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:       depRefs,
		usesResources: []string{resourcePatternYMakePython3, resourcePatternJDK17},
	}

	return emit.Emit(node)
}

func EmitJV(
	instance ModuleInstance,
	grammar string,
	options []string,
	visitor bool,
	listener bool,
	moduleTag STR,
	tc moduleToolchain,
	emit Emitter,
) NodeRef {
	grammarVFS := Source(instance.Path.Rel() + "/" + grammar)
	outDirVFS := Build(instance.Path.Rel())
	outDir := outDirVFS.String()

	cmdArgs := []STR{
		tc.Python3,
		internStr(stdout2stderrPath),
		internStr(jdkResourcePath),
		argJar.str(),
		internStr(antlr4JarPath),
		(grammarVFS).str(),
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

	cmdArgs = appendInternStrs(cmdArgs, options)

	inputs := inputChunks{{
		grammarVFS,
		stdout2stderrVFS,
		antlr4JarVFS,
	}}

	base := strings.TrimSuffix(filepath.Base(grammar), ".g4")
	outPrefix := instance.Path.Rel() + "/" + base
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
	tc moduleToolchain,
	emit Emitter,
) NodeRef {
	lexerVFS := Source(instance.Path.Rel() + "/" + lexer)
	parserVFS := Source(instance.Path.Rel() + "/" + parser)
	outDirVFS := Build(instance.Path.Rel())
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

	inputs := inputChunks{{
		lexerVFS,
		parserVFS,
		stdout2stderrVFS,
		antlr4JarVFS,
	}}

	lexerBase := strings.TrimSuffix(filepath.Base(lexer), ".g4")
	parserBase := strings.TrimSuffix(filepath.Base(parser), ".g4")
	visitorBase := parserBase
	outPrefix := instance.Path.Rel() + "/"
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
	tc moduleToolchain,
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
	jvInputs := inputChunks{inputs, {stdout2stderrVFS, jarVFS}}

	return emitJVNode(instance, cmdArgs, jvInputs, outputs, cwd, depRefs, moduleTag, emit)
}
