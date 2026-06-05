package main

import (
	"path/filepath"
	"strings"
)

var (
	antlr4JarVFS      = Intern("$(S)/contrib/java/antlr/antlr4/antlr.jar")
	antlr3JarVFS      = Intern("$(S)/contrib/java/antlr/antlr3/antlr.jar")
	antlr4JarPath     = antlr4JarVFS.String()
	antlr3JarPath     = antlr3JarVFS.String()
	stdout2stderrVFS  = Intern("$(S)/build/scripts/stdout2stderr.py")
	stdout2stderrPath = stdout2stderrVFS.String()
)

const jdkResourcePath = "$(JDK17-564746473)/bin/java"

func emitJVNode(instance ModuleInstance, cmdArgs []string, inputs []VFS, outputs []VFS, cwd string, depRefs []NodeRef, moduleTag string, emit Emitter) NodeRef {
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
		KV:      KV{P: "JV", PC: "light-blue", ShowOut: "yes"},
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

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		stdout2stderrPath,
		jdkResourcePath,
		"-jar",
		antlr4JarPath,
		grammarVFS.String(),
		"-Dlanguage=Cpp",
		"-o",
		outDir,
	}

	if visitor {
		cmdArgs = append(cmdArgs, "-visitor")
	}

	if !listener {
		cmdArgs = append(cmdArgs, "-no-listener")
	} else {
		cmdArgs = append(cmdArgs, "-listener")
	}

	cmdArgs = append(cmdArgs, options...)

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

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		stdout2stderrPath,
		jdkResourcePath,
		"-jar",
		antlr4JarPath,
		lexerVFS.String(),
		parserVFS.String(),
		"-Dlanguage=Cpp",
		"-o",
		outDir,
	}

	if visitor {
		cmdArgs = append(cmdArgs, "-visitor")
	}

	if !listener {
		cmdArgs = append(cmdArgs, "-no-listener")
	} else {
		cmdArgs = append(cmdArgs, "-listener")
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
	cmdArgs := make([]string, 0, 5+len(args))
	cmdArgs = append(cmdArgs,
		instance.Platform.Tools.Python3,
		stdout2stderrPath,
		jdkResourcePath,
		"-jar",
		jarVFS.String(),
	)
	cmdArgs = append(cmdArgs, args...)

	jvInputs := make([]VFS, 0, len(inputs)+2)
	jvInputs = append(jvInputs, inputs...)
	jvInputs = append(jvInputs, stdout2stderrVFS, jarVFS)

	return emitJVNode(instance, cmdArgs, jvInputs, outputs, cwd, depRefs, moduleTag, emit)
}
