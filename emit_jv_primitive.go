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

const jdkResourcePath = "$(JDK17-564746473)/bin/java"

func emitJVNode(instance ModuleInstance, cmdArgs []ANY, inputs []VFS, outputs []VFS, cwd string, depRefs []NodeRef, moduleTag string, emit Emitter) NodeRef {
	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
				Cwd:     cwd,
			},
		},
		Env:     env,
		Inputs:  inputs,
		KV:      KV{P: pkJV, PC: pcLightBlue, ShowOut: "yes"},
		Outputs: outputs,
		Tags:    []string{},
		TargetProperties: func() TargetProperties {
			tp := TargetProperties{ModuleDir: instance.Path}

			if moduleTag != "" {
				tp.ModuleTag = moduleTag
			}

			return tp
		}(),
		Platform:     string(instance.Platform.Target),
		Requirements: Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:      depRefs,
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3, resourcePatternJDK17), instance.Platform))
}

func EmitJV(
	instance ModuleInstance,
	grammar string,
	options []string,
	visitor bool,
	listener bool,
	moduleTag string,
	emit Emitter,
) NodeRef {
	grammarVFS := Source(instance.Path + "/" + grammar)
	outDirVFS := Build(instance.Path)
	outDir := outDirVFS.String()

	cmdArgs := []ANY{
		internAny(instance.Platform.Tools.Python3),
		internAny(stdout2stderrPath),
		internAny(jdkResourcePath),
		anyJar,
		internAny(antlr4JarPath),
		vfsAny(grammarVFS),
		anyDlanguageCpp,
		argDashO,
		internAny(outDir),
	}

	if visitor {
		cmdArgs = append(cmdArgs, anyVisitor)
	}

	if !listener {
		cmdArgs = append(cmdArgs, anyNoListener)
	} else {
		cmdArgs = append(cmdArgs, anyListener)
	}

	cmdArgs = appendStringAny(cmdArgs, options)

	inputs := []VFS{
		grammarVFS,
		stdout2stderrVFS,
		antlr4JarVFS,
	}

	base := strings.TrimSuffix(filepath.Base(grammar), ".g4")
	outPrefix := instance.Path + "/" + base
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
	moduleTag string,
	emit Emitter,
) NodeRef {
	lexerVFS := Source(instance.Path + "/" + lexer)
	parserVFS := Source(instance.Path + "/" + parser)
	outDirVFS := Build(instance.Path)
	outDir := outDirVFS.String()

	cmdArgs := []ANY{
		internAny(instance.Platform.Tools.Python3),
		internAny(stdout2stderrPath),
		internAny(jdkResourcePath),
		anyJar,
		internAny(antlr4JarPath),
		vfsAny(lexerVFS),
		vfsAny(parserVFS),
		anyDlanguageCpp,
		argDashO,
		internAny(outDir),
	}

	if visitor {
		cmdArgs = append(cmdArgs, anyVisitor)
	}

	if !listener {
		cmdArgs = append(cmdArgs, anyNoListener)
	} else {
		cmdArgs = append(cmdArgs, anyListener)
	}

	inputs := []VFS{
		lexerVFS,
		parserVFS,
		stdout2stderrVFS,
		antlr4JarVFS,
	}

	lexerBase := strings.TrimSuffix(filepath.Base(lexer), ".g4")
	parserBase := strings.TrimSuffix(filepath.Base(parser), ".g4")
	visitorBase := parserBase
	outPrefix := instance.Path + "/"
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
	moduleTag string,
	emit Emitter,
) NodeRef {
	cmdArgs := make([]ANY, 0, 5+len(args))
	cmdArgs = append(cmdArgs,
		internAny(instance.Platform.Tools.Python3),
		internAny(stdout2stderrPath),
		internAny(jdkResourcePath),
		anyJar,
		vfsAny(jarVFS),
	)
	cmdArgs = appendStringAny(cmdArgs, args)

	jvInputs := make([]VFS, 0, len(inputs)+2)
	jvInputs = append(jvInputs, inputs...)
	jvInputs = append(jvInputs, stdout2stderrVFS, jarVFS)

	return emitJVNode(instance, cmdArgs, jvInputs, outputs, cwd, depRefs, moduleTag, emit)
}
