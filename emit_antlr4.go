package main

import (
	"path/filepath"
	"strings"
)

func (e *EmitContext) emitAntlr4GrammarStmt(g Antlr4GrammarInfo) {
	module := e.instance.Path.relString()
	base := func(name string) string { return strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)) }
	lexerBase, parserBase := base(g.Grammar)+"Lexer", base(g.Grammar)+"Parser"
	optionalBase := base(g.Grammar)
	grammars := []string{g.Grammar}

	if g.IsSplit {
		lexerBase, parserBase = base(g.Lexer), base(g.Parser)
		optionalBase = parserBase
		grammars = []string{g.Lexer, g.Parser}
	}

	args := internAnys(grammars)

	args = append(args, argDlanguageCpp.any(), argDashO.any(), build(module).any())

	if g.Visitor {
		args = append(args, argVisitor.any())
	} else {
		args = append(args, argNoVisitor.any())
	}

	if g.Listener {
		args = append(args, argListener.any())
	} else {
		args = append(args, argNoListener.any())
	}

	args = appendInternAnys(args, g.Options)
	args = append(args, kwCWD.any(), build(module).any(), kwIN.any())
	args = appendInternAnys(args, grammars)
	args = append(args, kwOUT_NOAUTO.any())
	args = appendInternAnys(args, []string{lexerBase + ".cpp", lexerBase + ".h", parserBase + ".cpp", parserBase + ".h"})
	args = appendInternAnys(args, antlr4OptionalHeaders(optionalBase, g.Visitor, g.Listener))
	args = append(args, kwOUTPUT_INCLUDES.any(), antlr4RuntimeHeaderVFS.any())
	args = appendInternAnys(args, g.OutputIncludes)

	call := parseRunAntlr(args, Token{id: kwRUN_ANTLR4})

	e.emitAntlrRunStmt(AntlrRunInfo{
		Macro: call.Macro, Args: call.Args, INFiles: call.INFiles,
		OUTFiles: call.OUTFiles, OUTNoAutoFiles: call.OUTNoAutoFiles,
		CWD: call.CWD, OutputIncludes: call.OutputIncludes,
	})

	e.emitJVDownstreamCPCC([]struct{ cpp, h VFS }{
		{build(module, "/", lexerBase, ".cpp"), build(module, "/", lexerBase, ".h")},
		{build(module, "/", parserBase, ".cpp"), build(module, "/", parserBase, ".h")},
	}, g.OutputIncludes)
}

func antlr4OptionalHeaders(base string, visitor, listener bool) []string {
	var headers []string

	if listener {
		headers = append(headers, base+"Listener.h", base+"BaseListener.h")
	}

	if visitor {
		headers = append(headers, base+"Visitor.h", base+"BaseVisitor.h")
	}

	return headers
}
