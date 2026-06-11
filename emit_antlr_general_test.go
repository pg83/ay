package main

import "testing"

// TestAntlrParsedIncludes_ExcludesBuildIntermediateInputs locks the induced
// include set of a RUN_ANTLR output: a consumer that walks the generated
// output's closure (e.g. the proto-split RUN_PROGRAM protoc node walking
// SQLParser.proto) must see the generator's $(S) leaf sources (grammar,
// CONFIGURE_FILE source, jar, scripts) but NOT the $(B) intermediate the
// generator itself consumed (the CONFIGURE_FILE'd protobuf.stg). Upstream
// reaches that $(B) intermediate via the producer dep edge, not as a
// transitive source input; listing it diverges the PR self_uid.
func TestAntlrParsedIncludes_ExcludesBuildIntermediateInputs(t *testing.T) {
	const mod = "yql/essentials/parser/proto_ast/gen/v0_proto_split"
	stgBuild := Build(mod + "/org/antlr/codegen/templates/protobuf/protobuf.stg")
	inputs := []VFS{
		Source("yql/essentials/sql/v0/SQL.g"),
		stgBuild, // $(B) CONFIGURE_FILE output — generator intermediate
		Source("yql/essentials/parser/proto_ast/org/antlr/codegen/templates/protobuf/protobuf.stg.in"),
	}

	parsed := antlrParsedIncludes(
		mod,
		AntlrRunInfo{},
		"SQLParser.proto",
		map[string]VFS{"SQLParser.proto": Build(mod + "/SQLParser.proto")},
		inputs,
		antlr3JarVFS,
	)

	got := make(map[string]struct{}, len(parsed))
	for _, d := range parsed {
		got[d.target.String()] = struct{}{}
	}

	if _, leaked := got[stgBuild.rel()]; leaked {
		t.Errorf("antlrParsedIncludes leaked $(B) generator intermediate %q: %v", stgBuild.rel(), keysOf(got))
	}
	for _, want := range []string{
		"yql/essentials/sql/v0/SQL.g",
		"yql/essentials/parser/proto_ast/org/antlr/codegen/templates/protobuf/protobuf.stg.in",
		stdout2stderrVFS.rel(),
		antlr3JarVFS.rel(),
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("antlrParsedIncludes missing $(S) source %q: %v", want, keysOf(got))
		}
	}
}

// TestAntlrParsedIncludes_LexerCrossIncludesParserCpp locks the ANTLR3
// combined-grammar include convention observed in the reference graph: a
// generated *Lexer.cpp's compile reaches the paired *Parser.cpp (which in turn
// holds the protobuf header), and NEITHER the lexer nor the parser .cpp lists
// the sibling generated .h files as inputs. Empirically, for jsonpath:
//
//	JsonPathLexer.cpp.o inputs = {JsonPathLexer.cpp, JsonPathParser.cpp, .pb.h}
//	JsonPathParser.cpp.o inputs = {JsonPathParser.cpp, .pb.h}
//
// i.e. Lexer.cpp -> Parser.cpp (one direction only), no *.h, Parser.cpp does
// not pull Lexer.cpp.
func TestAntlrParsedIncludes_LexerCrossIncludesParserCpp(t *testing.T) {
	const mod = "yql/essentials/parser/proto_ast/gen/jsonpath"
	outByTok := map[string]VFS{
		"JsonPathParser.cpp": Build(mod + "/JsonPathParser.cpp"),
		"JsonPathLexer.cpp":  Build(mod + "/JsonPathLexer.cpp"),
		"JsonPathParser.h":   Build(mod + "/JsonPathParser.h"),
		"JsonPathLexer.h":    Build(mod + "/JsonPathLexer.h"),
	}
	run := AntlrRunInfo{}

	induced := func(outTok string) map[string]struct{} {
		parsed := antlrParsedIncludes(mod, run, outTok, outByTok, nil, antlr3JarVFS)
		got := make(map[string]struct{}, len(parsed))
		for _, d := range parsed {
			got[d.target.String()] = struct{}{}
		}
		return got
	}

	lexerRel := outByTok["JsonPathLexer.cpp"].rel()
	parserRel := outByTok["JsonPathParser.cpp"].rel()
	lexerHRel := outByTok["JsonPathLexer.h"].rel()
	parserHRel := outByTok["JsonPathParser.h"].rel()

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
