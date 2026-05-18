package main

import (
	"path/filepath"
	"strings"
)

// jdkResourcePath is the literal JDK17 resource path. The hash suffix
// (564746473) is the resource bundle ID and is pinned byte-exact.
const jdkResourcePath = "$(JDK17-564746473)/bin/java"

// antlr4JarVFS is the source-relative VFS path to the ANTLR4 jar.
var antlr4JarVFS = Source("contrib/java/antlr/antlr4/antlr.jar")

// antlr4JarPath is the legacy string form (used in cmd_args). Equal
// to antlr4JarVFS.String().
var antlr4JarPath = antlr4JarVFS.String()

// stdout2stderr is the wrapper script that redirects antlr4's stdout
// to stderr (required so the build system captures diagnostic output
// correctly).
var stdout2stderrVFS = Source("build/scripts/stdout2stderr.py")
var stdout2stderrPath = stdout2stderrVFS.String()

// EmitJV emits a JV node for a single RUN_ANTLR4_CPP grammar (.g4
// relative to module dir). Options are extra cmd_args tokens.
// visitor=true → -visitor; listener=false (default split) → -no-listener.
//
// cmd_args: [python3, stdout2stderr.py, jdk/bin/java, -jar, antlr4.jar,
// <grammar>, -Dlanguage=Cpp, -o, $(B)/<modulePath>, ...options].
// outputs: <grammar>{Lexer,Parser,Visitor,BaseVisitor}.{cpp,h}.
// inputs: [grammar.g4, stdout2stderr.py, antlr4.jar]; cwd: $(B)/<modulePath>.
func EmitJV(
	instance ModuleInstance,
	grammar string,
	options []string,
	visitor bool,
	listener bool,
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

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

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

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
				Cwd:     outDir,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]string{
			"p":        "JV",
			"pc":       "light-blue",
			"show_out": "yes",
		},
		Outputs: outputs,
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node)
}

// EmitJVSplit emits a JV node for RUN_ANTLR4_CPP_SPLIT (separate lexer
// + parser .g4 files).
//
// cmd_args: [python3, stdout2stderr.py, jdk/bin/java, -jar, antlr4.jar,
// <lexer>, <parser>, -Dlanguage=Cpp, -o, $(B)/<modulePath>, ...flags].
// Outputs: {Lexer,Parser}.{cpp,h} + ParserVisitor.h, ParserBaseVisitor.h.
func EmitJVSplit(
	instance ModuleInstance,
	lexer string,
	parser string,
	visitor bool,
	listener bool,
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

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
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

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
				Cwd:     outDir,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]string{
			"p":        "JV",
			"pc":       "light-blue",
			"show_out": "yes",
		},
		Outputs: outputs,
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node)
}
