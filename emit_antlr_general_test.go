package main

import "testing"

func TestAntlrParsedIncludes_ExcludesBuildIntermediateInputs(t *testing.T) {
	const mod = "yql/essentials/parser/proto_ast/gen/v0_proto_split"
	stgBuild := build(mod + "/org/antlr/codegen/templates/protobuf/protobuf.stg")
	inputs := []VFS{
		source("yql/essentials/sql/v0/SQL.g"),
		stgBuild,
		source("yql/essentials/parser/proto_ast/org/antlr/codegen/templates/protobuf/protobuf.stg.in"),
	}

	parsed := antlrParsedIncludes(
		mod,
		AntlrRunInfo{},
		"SQLParser.proto",
		map[string]VFS{"SQLParser.proto": build(mod + "/SQLParser.proto")},
		inputs,
		antlr3JarVFS,
	)

	got := make(map[string]struct{}, len(parsed))

	for _, d := range parsed {
		got[d.target.string()] = struct{}{}
	}

	if _, leaked := got[stgBuild.relString()]; leaked {
		t.Errorf("antlrParsedIncludes leaked $(B) generator intermediate %q: %v", stgBuild.relString(), keysOf(got))
	}

	for _, want := range []string{
		"yql/essentials/sql/v0/SQL.g",
		"yql/essentials/parser/proto_ast/org/antlr/codegen/templates/protobuf/protobuf.stg.in",
		stdout2stderrVFS.relString(),
		antlr3JarVFS.relString(),
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("antlrParsedIncludes missing $(S) source %q: %v", want, keysOf(got))
		}
	}
}

func TestAntlrParsedIncludes_LexerCrossIncludesParserCpp(t *testing.T) {
	const mod = "yql/essentials/parser/proto_ast/gen/jsonpath"
	outByTok := map[string]VFS{
		"JsonPathParser.cpp": build(mod + "/JsonPathParser.cpp"),
		"JsonPathLexer.cpp":  build(mod + "/JsonPathLexer.cpp"),
		"JsonPathParser.h":   build(mod + "/JsonPathParser.h"),
		"JsonPathLexer.h":    build(mod + "/JsonPathLexer.h"),
	}
	run := AntlrRunInfo{}

	induced := func(outTok string) map[string]struct{} {
		parsed := antlrParsedIncludes(mod, run, outTok, outByTok, nil, antlr3JarVFS)
		got := make(map[string]struct{}, len(parsed))

		for _, d := range parsed {
			got[d.target.string()] = struct{}{}
		}

		return got
	}

	lexerRel := outByTok["JsonPathLexer.cpp"].relString()
	parserRel := outByTok["JsonPathParser.cpp"].relString()
	lexerHRel := outByTok["JsonPathLexer.h"].relString()
	parserHRel := outByTok["JsonPathParser.h"].relString()

	lex := induced("JsonPathLexer.cpp")

	if _, ok := lex[parserRel]; !ok {
		t.Errorf("Lexer.cpp must induce paired Parser.cpp %q: %v", parserRel, keysOf(lex))
	}

	if _, ok := lex[lexerHRel]; ok {
		t.Errorf("Lexer.cpp must not induce sibling .h %q: %v", lexerHRel, keysOf(lex))
	}

	par := induced("JsonPathParser.cpp")

	if _, ok := par[parserHRel]; ok {
		t.Errorf("Parser.cpp must not induce sibling .h %q: %v", parserHRel, keysOf(par))
	}

	if _, ok := par[lexerRel]; ok {
		t.Errorf("Parser.cpp must not induce Lexer.cpp %q: %v", lexerRel, keysOf(par))
	}
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))

	for k := range m {
		out = append(out, k)
	}

	return out
}
