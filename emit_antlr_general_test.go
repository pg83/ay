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
		antlrRunInfo{},
		"SQLParser.proto",
		map[string]VFS{"SQLParser.proto": Build(mod + "/SQLParser.proto")},
		inputs,
		antlr3JarVFS,
	)

	got := make(map[string]struct{}, len(parsed))
	for _, d := range parsed {
		got[d.target.String()] = struct{}{}
	}

	if _, leaked := got[stgBuild.Rel()]; leaked {
		t.Errorf("antlrParsedIncludes leaked $(B) generator intermediate %q: %v", stgBuild.Rel(), keysOf(got))
	}
	for _, want := range []string{
		"yql/essentials/sql/v0/SQL.g",
		"yql/essentials/parser/proto_ast/org/antlr/codegen/templates/protobuf/protobuf.stg.in",
		stdout2stderrVFS.Rel(),
		antlr3JarVFS.Rel(),
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("antlrParsedIncludes missing $(S) source %q: %v", want, keysOf(got))
		}
	}
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
