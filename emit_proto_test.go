package main

import (
	"strings"
	"testing"
)

// TestEmitProtoSrcs_GeneratedProtoWiresProducerDep reproduces the
// jsonpath G2 gap: a PROTO_LIBRARY whose SRCS(X.proto) is itself the OUT
// of a RUN_ANTLR (no X.proto in source tree). The PB protoc node must wire
// a dep to the JV producer of X.proto AND treat the input as build-rooted,
// or the JV (and its CF dep on protobuf.stg) get DFS-pruned at finalize.
func TestEmitProtoSrcs_GeneratedProtoWiresProducerDep(t *testing.T) {
	const modPath = "yql/essentials/parser/proto_ast/gen/jsonpath"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	for path, body := range map[string]string{
		modPath + "/ya.make": `PROTO_LIBRARY()

IF (GEN_PROTO)
    SET(antlr_output ${ARCADIA_BUILD_ROOT}/${MODDIR})
    SET(antlr_templates ${antlr_output}/org/antlr/codegen/templates)
    SET(jsonpath_grammar ${ARCADIA_ROOT}/yql/essentials/minikql/jsonpath/JsonPath.g)

    CONFIGURE_FILE(${ARCADIA_ROOT}/templates/protobuf.stg.in ${antlr_templates}/protobuf/protobuf.stg)

    RUN_ANTLR(
        ${jsonpath_grammar}
        -lib .
        -fo ${antlr_output}
        -language protobuf
        IN ${jsonpath_grammar} ${antlr_templates}/protobuf/protobuf.stg
        OUT_NOAUTO JsonPathParser.proto
        CWD ${antlr_output}
    )
ENDIF()

SRCS(JsonPathParser.proto)

EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)

END()
`,
		"templates/protobuf.stg.in":                  "stub stg\n",
		"yql/essentials/minikql/jsonpath/JsonPath.g": "stub grammar\n",
		"contrib/libs/protobuf/ya.make":              "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n",
	} {
		files[path] = body
	}

	g := testGen(newMemFS(files), modPath)

	byOut := make(map[string]*Node, len(g.Graph))
	for _, n := range g.Graph {
		if len(n.Outputs) > 0 {
			byOut[n.Outputs[0].String()] = n
		}
	}

	for _, key := range []string{
		"$(B)/" + modPath + "/JsonPathParser.proto",
		"$(B)/" + modPath + "/org/antlr/codegen/templates/protobuf/protobuf.stg",
	} {
		if byOut[key] == nil {
			t.Errorf("graph missing reachable node with output %q", key)
		}
	}

	var pb *Node
	for _, n := range g.Graph {
		if n.KV["p"] == "PB" && strings.HasSuffix(n.Outputs[0].String(), "JsonPathParser.pb.h") {
			pb = n
			break
		}
	}
	if pb == nil {
		t.Fatal("no PB node for JsonPathParser.pb.h emitted")
	}

	jv := byOut["$(B)/"+modPath+"/JsonPathParser.proto"]
	if jv == nil {
		t.Fatal("no JV node producing JsonPathParser.proto")
	}
	if jv.KV["p"] != "JV" {
		t.Errorf("expected JV kv.p, got %v", jv.KV["p"])
	}

	found := false
	for _, d := range pb.Deps {
		if d == jv.UID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PB.Deps %v does not include JV(.proto) uid %q", pb.Deps, jv.UID)
	}

	hasBuildProto := false
	for _, in := range pb.Inputs {
		if in.String() == "$(B)/"+modPath+"/JsonPathParser.proto" {
			hasBuildProto = true
			break
		}
	}
	if !hasBuildProto {
		t.Errorf("PB.Inputs does not include $(B)/.../JsonPathParser.proto: %v", pb.Inputs)
	}
}
