package main

import (
	"path"
	"strings"
)

type RunAntlr4PythonStmt struct {
	Args    []ANY
	Version string
	Line    int
}

type antlrPythonArguments struct {
	Grammar      ANY
	Subdir       ANY
	Options      []ANY
	ExtraOutputs []ANY
	Visitor      bool
	Listener     bool
}

func (*RunAntlr4PythonStmt) stmtMarker() {
}

func bindRunAntlr4Python(args []ANY, version string) *antlrPythonArguments {
	if len(args) == 0 {
		throwFmt("RUN_ANTLR4_PYTHON%s expects a grammar", version)
	}

	stmt := &antlrPythonArguments{Grammar: args[0], Subdir: str4.any()}
	extra := false

	for i := 1; i < len(args); i++ {
		switch args[i].string() {
		case "LISTENER":
			stmt.Listener, extra = true, false
		case "VISITOR":
			stmt.Visitor, extra = true, false
		case "SUBDIR":
			if i+1 == len(args) {
				throwFmt("RUN_ANTLR4_PYTHON%s SUBDIR expects a value", version)
			}

			i++
			stmt.Subdir, extra = args[i], false
		case "EXTRA_OUTS":
			extra = true
		default:
			if extra {
				stmt.ExtraOutputs = append(stmt.ExtraOutputs, args[i])
			} else {
				stmt.Options = append(stmt.Options, args[i])
			}
		}
	}

	return stmt
}

func antlrPythonConfigValue(pm *IncludeParserManager, env Environment, name string) string {
	if value, bound := env.lookup(name); bound {
		return expandConfScalar(value, env)
	}

	conf := pm.confFile("build/ymake.core.conf")
	local := env.clone()
	key := internEnv(name)

	for _, stmt := range conf.stmts {
		if stmt.name == key && conf.holds(stmt.branch, local) {
			applyConfStmt(local, stmt, stmt.branch != 0)
		}
	}

	return expandConfScalar(local.string(internEnv(name)), local)
}

func collectAntlrPython(pm *IncludeParserManager, module string, source *RunAntlr4PythonStmt, env Environment, d *ModuleData) {
	stmt := bindRunAntlr4Python(expandSourceArguments(source.Args, env), source.Version)
	grammar := expandStmtTokenAny(stmt.Grammar, env)
	subdir := expandStmtTokenAny(stmt.Subdir, env).string()
	prefix := "_ANTLR4"

	if source.Version == "2" {
		prefix = "_ANTLR4_11_1"
	}

	visitor, listener := "-no-visitor", "-no-listener"

	if stmt.Visitor {
		visitor = "-visitor"
	}

	if stmt.Listener {
		listener = "-listener"
	}

	args := []ANY{grammar, internStr("-Dlanguage=Python" + source.Version).any(), argDashO.any(), internStr(build(module).string() + "/" + subdir).any(), internStr(visitor).any(), internStr(listener).any()}

	args = append(args, expandStmtTokens(stmt.Options, env)...)

	base := path.Base(stmt.Grammar.string())

	base = strings.TrimSuffix(base, path.Ext(base))

	names := []string{base + "Lexer.py", base + "Parser.py"}

	if stmt.Listener {
		names = append(names, base+"Listener.py")
	}

	if stmt.Visitor {
		names = append(names, base+"Visitor.py")
	}

	for _, out := range expandStmtTokens(stmt.ExtraOutputs, env) {
		names = append(names, out.string())
	}

	args = append(args, kwCWD.any(), build(module).any(), kwIN.any(), grammar, kwOUT_NOAUTO.any())

	for _, name := range names {
		args = append(args, internStr(path.Join(subdir, name)).any())
	}

	lowered := parseRunAntlr(args, Token{id: kwRUN_ANTLR4, line: source.Line})

	run := AntlrRunInfo{DeclSeq: d.nextDeclSeq(), Macro: lowered.Macro, Args: lowered.Args, INFiles: lowered.INFiles,
		OUTFiles: lowered.OUTFiles, OUTNoAutoFiles: lowered.OUTNoAutoFiles, CWD: lowered.CWD, OutputIncludes: lowered.OutputIncludes,
		Jar: internStr(antlrPythonConfigValue(pm, env, prefix+"_JAR")).any()}

	d.antlrRuns = append(d.antlrRuns, run)
	d.peerdirs = append(d.peerdirs, internStr(antlrPythonConfigValue(pm, env, prefix+"_PYTHON_RUNTIME")).any())
}
