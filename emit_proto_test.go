package main

import (
	"strings"
	"testing"
)

func TestEmitProtoSrcs_YaffGeneratedHeaderClosureRidesIntoConsumer(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "library/cpp/yaff/yaff.h", "#pragma once\n#include <library/cpp/yaff/base.h>\n")
	writeTestModuleFile(files, "library/cpp/yaff/base.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/struct.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/protobuf.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/reflect.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/experiments/builder.h", "#pragma once\n#include <library/cpp/yaff/experiments/base.h>\n")
	writeTestModuleFile(files, "library/cpp/yaff/experiments/base.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/experiments/column.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/experiments/merge.h", "#pragma once\n")

	writeTestModuleFile(files, "proto/ya.make",
		"PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nYAFF(EXPERIMENTAL foo.proto)\nSRCS(foo.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "proto/foo.proto", "syntax = \"proto3\";\npackage test;\nmessage Foo { string v = 1; }\n")

	writeTestModuleFile(files, "app/ya.make",
		"LIBRARY()\nPEERDIR(proto)\nSRCS(use.cpp)\nEND()\n")
	writeTestModuleFile(files, "app/use.cpp", "#include <proto/foo.yaff.h>\nint use(){return 0;}\n")

	g := testGen(newMemFS(files), "app")
	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")

	for _, want := range []string{
		"$(B)/proto/foo.yaff.h",
		"$(B)/proto/foo.pb.h",
		"$(S)/library/cpp/yaff/yaff.h",
		"$(S)/library/cpp/yaff/base.h",
		"$(S)/library/cpp/yaff/struct.h",
		"$(S)/library/cpp/yaff/protobuf.h",
		"$(S)/library/cpp/yaff/reflect.h",
		"$(S)/library/cpp/yaff/experiments/builder.h",
		"$(S)/library/cpp/yaff/experiments/base.h",
		"$(S)/library/cpp/yaff/experiments/column.h",
		"$(S)/library/cpp/yaff/experiments/merge.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Errorf("use.cpp.o missing YaFF closure input %q", want)
		}
	}
}

func TestEmitProtoSrcs_YaffFilesWhitelistSkipsNonWhitelistedHeaderClosure(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "library/cpp/yaff/yaff.h", "#pragma once\n#include <library/cpp/yaff/base.h>\n")
	writeTestModuleFile(files, "library/cpp/yaff/base.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/struct.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/protobuf.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/reflect.h", "#pragma once\n")

	writeTestModuleFile(files, "proto/ya.make",
		"PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nYAFF(FILES kept.proto)\nSRCS(kept.proto skipped.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "proto/kept.proto", "syntax = \"proto3\";\npackage test;\nmessage Kept { string v = 1; }\n")
	writeTestModuleFile(files, "proto/skipped.proto", "syntax = \"proto3\";\npackage test;\nmessage Skipped { string v = 1; }\n")

	writeTestModuleFile(files, "app/ya.make",
		"LIBRARY()\nPEERDIR(proto)\nSRCS(usekept.cpp useskip.cpp)\nEND()\n")
	writeTestModuleFile(files, "app/usekept.cpp", "#include <proto/kept.yaff.h>\nint usekept(){return 0;}\n")
	writeTestModuleFile(files, "app/useskip.cpp", "#include <proto/skipped.yaff.h>\nint useskip(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	keptCC := mustNodeByOutput(t, g, "$(B)/app/usekept.cpp.o")

	for _, want := range []string{
		"$(B)/proto/kept.yaff.h",
		"$(B)/proto/kept.pb.h",
		"$(S)/library/cpp/yaff/yaff.h",
		"$(S)/library/cpp/yaff/struct.h",
	} {
		if !nodeHasInput(keptCC, want) {
			t.Errorf("usekept.cpp.o missing whitelisted YaFF closure input %q", want)
		}
	}

	skipCC := mustNodeByOutput(t, g, "$(B)/app/useskip.cpp.o")

	for _, notWant := range []string{
		"$(B)/proto/skipped.pb.h",
		"$(S)/library/cpp/yaff/yaff.h",
		"$(S)/library/cpp/yaff/base.h",
		"$(S)/library/cpp/yaff/struct.h",
		"$(S)/library/cpp/yaff/protobuf.h",
		"$(S)/library/cpp/yaff/reflect.h",
	} {
		if nodeHasInput(skipCC, notWant) {
			t.Errorf("useskip.cpp.o over-collected non-whitelisted YaFF closure input %q", notWant)
		}
	}
}

func TestEmitProtoSrcs_YaffCppInputClosureInducesWireFormatDropsSiblingHeader(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "contrib/tools/protoc/ya.make",
		"PROGRAM(protoc)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"+
			"INDUCED_DEPS(cpp ${ARCADIA_ROOT}/contrib/libs/protobuf/src/google/protobuf/wire_format.h)\n"+
			"INDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/contrib/libs/protobuf/src/google/protobuf/message.h)\n"+
			"SRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/tools/protoc/main.cpp", "int main(){return 0;}\n")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/wire_format.h", "#pragma once\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/message.h", "#pragma once\n")

	writeTestModuleFile(files, "library/cpp/yaff/yaff.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/struct.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/protobuf.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/reflect.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/experiments/builder.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/experiments/column.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/experiments/merge.h", "#pragma once\n")

	writeTestModuleFile(files, "proto/ya.make",
		"PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nYAFF(EXPERIMENTAL foo.proto)\nSRCS(foo.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "proto/foo.proto", "syntax = \"proto3\";\npackage test;\nmessage Foo { string v = 1; }\n")

	g := testGen(newMemFS(files), "proto")
	yaffCC := mustNodeByOutput(t, g, "$(B)/proto/foo.yaff.cpp.o")

	const wireFormat = "$(S)/contrib/libs/protobuf/src/google/protobuf/wire_format.h"
	const siblingHeader = "$(B)/proto/foo.yaff.h"

	if !nodeHasInput(yaffCC, wireFormat) {
		t.Errorf("foo.yaff.cpp.o missing induced cpp input %q: %v", wireFormat, yaffCC.flatInputs())
	}

	if nodeHasInput(yaffCC, siblingHeader) {
		t.Errorf("foo.yaff.cpp.o must not record the sibling generated header %q: %v", siblingHeader, yaffCC.flatInputs())
	}

	for _, want := range []string{
		"$(B)/proto/foo.pb.h",
		"$(S)/library/cpp/yaff/yaff.h",
		"$(S)/library/cpp/yaff/protobuf.h",
	} {
		if !nodeHasInput(yaffCC, want) {
			t.Errorf("foo.yaff.cpp.o missing surviving YaFF closure input %q", want)
		}
	}
}

func TestEmitProtoSrcs_NonWhitelistedYaffCppRidesProtoMainPbHeader(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "contrib/tools/protoc/ya.make",
		"PROGRAM(protoc)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"+
			"INDUCED_DEPS(cpp ${ARCADIA_ROOT}/contrib/libs/protobuf/src/google/protobuf/wire_format.h)\n"+
			"INDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/contrib/libs/protobuf/src/google/protobuf/message.h)\n"+
			"SRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/tools/protoc/main.cpp", "int main(){return 0;}\n")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/wire_format.h", "#pragma once\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/message.h", "#pragma once\n")

	writeTestModuleFile(files, "library/cpp/yaff/yaff.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/struct.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/protobuf.h", "#pragma once\n")
	writeTestModuleFile(files, "library/cpp/yaff/reflect.h", "#pragma once\n")

	writeTestModuleFile(files, "proto/ya.make",
		"PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nYAFF(FILES kept.proto)\nSRCS(kept.proto skipped.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "proto/kept.proto", "syntax = \"proto3\";\npackage test;\nmessage Kept { string v = 1; }\n")
	writeTestModuleFile(files, "proto/skipped.proto", "syntax = \"proto3\";\npackage test;\nmessage Skipped { string v = 1; }\n")

	g := testGen(newMemFS(files), "proto")

	const wireFormat = "$(S)/contrib/libs/protobuf/src/google/protobuf/wire_format.h"
	const wrapper = "$(S)/build/scripts/cpp_proto_wrapper.py"

	skipCC := mustNodeByOutput(t, g, "$(B)/proto/skipped.yaff.cpp.o")

	for _, want := range []string{
		"$(B)/proto/skipped.pb.h",
		"$(S)/proto/skipped.proto",
		wrapper,
	} {
		if !nodeHasInput(skipCC, want) {
			t.Errorf("skipped.yaff.cpp.o missing producer-source input %q: %v", want, skipCC.flatInputs())
		}
	}

	if nodeHasInput(skipCC, "$(B)/proto/skipped.yaff.h") {
		t.Errorf("skipped.yaff.cpp.o must not record the sibling self header %q", "$(B)/proto/skipped.yaff.h")
	}

	keptCC := mustNodeByOutput(t, g, "$(B)/proto/kept.yaff.cpp.o")

	for _, want := range []string{
		wireFormat,
		"$(B)/proto/kept.pb.h",
		"$(S)/proto/kept.proto",
		wrapper,
	} {
		if !nodeHasInput(keptCC, want) {
			t.Errorf("kept.yaff.cpp.o missing input %q: %v", want, keptCC.flatInputs())
		}
	}

	if nodeHasInput(keptCC, "$(B)/proto/kept.yaff.h") {
		t.Errorf("kept.yaff.cpp.o must not record the sibling self header %q", "$(B)/proto/kept.yaff.h")
	}
}

func TestEmitProtoSrcs_YaffOutputOrderFollowsLiteHeaderDeclarationOrder(t *testing.T) {
	mkFiles := func() map[string]string {
		files := map[string]string{}
		writeToolProgram(files, "contrib/tools/protoc", "protoc")
		writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
		writeToolProgram(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
		writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
		writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
		writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

		return files
	}

	beforeFiles := mkFiles()
	writeTestModuleFile(beforeFiles, "before/ya.make",
		"PROTO_LIBRARY()\nYAFF()\nSRCS(foo.proto)\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(beforeFiles, "before/foo.proto", "syntax = \"proto3\";\npackage test;\nmessage Foo { string v = 1; }\n")

	gBefore := testGen(newMemFS(beforeFiles), "before")
	pbBefore := mustNodeByOutput(t, gBefore, "$(B)/before/foo.pb.h")
	wantBefore := []string{
		"$(B)/before/foo.pb.h",
		"$(B)/before/foo.yaff.h",
		"$(B)/before/foo.yaff.cpp",
		"$(B)/before/foo.pb.cc",
		"$(B)/before/foo.deps.pb.h",
	}
	assertOutputOrder(t, "YAFF-before-SET", pbBefore, wantBefore)

	afterFiles := mkFiles()
	writeTestModuleFile(afterFiles, "after/ya.make",
		"PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nYAFF()\nSRCS(foo.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(afterFiles, "after/foo.proto", "syntax = \"proto3\";\npackage test;\nmessage Foo { string v = 1; }\n")

	gAfter := testGen(newMemFS(afterFiles), "after")
	pbAfter := mustNodeByOutput(t, gAfter, "$(B)/after/foo.pb.h")
	wantAfter := []string{
		"$(B)/after/foo.pb.h",
		"$(B)/after/foo.pb.cc",
		"$(B)/after/foo.deps.pb.h",
		"$(B)/after/foo.yaff.h",
		"$(B)/after/foo.yaff.cpp",
	}
	assertOutputOrder(t, "SET-before-YAFF", pbAfter, wantAfter)
}

func assertOutputOrder(t *testing.T, label string, n *Node, want []string) {
	t.Helper()

	got := make([]string, len(n.Outputs))

	for i, o := range n.Outputs {
		got[i] = o.string()
	}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s: PB outputs order =\n  %v\nwant\n  %v", label, got, want)
	}

	args := strStrs(n.Cmds[0].CmdArgs.flat())
	start := -1

	for i, a := range args {
		if a == "--outputs" {
			start = i + 1

			break
		}
	}

	if start < 0 {
		t.Fatalf("%s: --outputs not found in cmd args: %v", label, args)
	}

	for i, w := range want {
		if start+i >= len(args) || args[start+i] != w {
			t.Fatalf("%s: --outputs[%d] = %q, want %q (args=%v)", label, i, args[min(start+i, len(args)-1)], w, args)
		}
	}
}

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
			byOut[n.Outputs[0].string()] = n
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
		if n.KV.P == pkPB && strings.HasSuffix(n.Outputs[0].string(), "JsonPathParser.pb.h") {
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

	if jv.KV.P != pkJV {
		t.Errorf("expected JV kv.p, got %v", jv.KV.P)
	}

	found := false

	for _, d := range graphDeps(g, pb) {
		if d == jv.UID {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("graphDeps(g, PB) %v does not include JV(.proto) uid %q", graphDeps(g, pb), jv.UID)
	}

	hasBuildProto := false

	for _, in := range pb.flatInputs() {
		if in.string() == "$(B)/"+modPath+"/JsonPathParser.proto" {
			hasBuildProto = true

			break
		}
	}

	if !hasBuildProto {
		t.Errorf("PB.flatInputs() does not include $(B)/.../JsonPathParser.proto: %v", pb.flatInputs())
	}
}

func TestEmitProtoSrcs_GeneratedProtoInheritsProducerSourceInputs(t *testing.T) {
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

	var pb *Node

	for _, n := range g.Graph {
		if n.KV.P == pkPB && strings.HasSuffix(n.Outputs[0].string(), "JsonPathParser.pb.h") {
			pb = n

			break
		}
	}

	if pb == nil {
		t.Fatal("no PB node for JsonPathParser.pb.h emitted")
	}

	have := make(map[string]struct{}, len(pb.flatInputs()))

	for _, in := range pb.flatInputs() {
		have[in.string()] = struct{}{}
	}

	for _, want := range []string{
		"$(S)/yql/essentials/minikql/jsonpath/JsonPath.g",
		"$(S)/templates/protobuf.stg.in",
		"$(S)/contrib/java/antlr/antlr3/antlr.jar",
		"$(S)/build/scripts/configure_file.py",
		"$(S)/build/scripts/stdout2stderr.py",
	} {
		if _, ok := have[want]; !ok {
			t.Errorf("PB.flatInputs() missing producer source input %q: %v", want, vfsStringsT3(pb.flatInputs()))
		}
	}
}

func TestEmitProtoSrcs_GeneratedProtoCompileCarriesOutputIncludesPbHClosure(t *testing.T) {
	const modPath = "irt/test/banner_flags"
	const consumer = "irt/test/app"

	files := map[string]string{
		consumer + "/ya.make": `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(` + modPath + `)
END()
`,
		modPath + "/ya.make": `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
NO_MYPY()
IF (GEN_PROTO)
RUN_PROGRAM(
    ` + modPath + `/gen
    STDOUT_NOAUTO gen.proto
    OUTPUT_INCLUDES dep/markup.proto
)
ENDIF()
SRCS(gen.proto)
PEERDIR(dep)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`,
		"dep/ya.make": `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
NO_MYPY()
SRCS(markup.proto)
PEERDIR(leaf)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`,
		"dep/markup.proto": "syntax = \"proto3\";\nimport \"leaf/leaf.proto\";\nmessage Markup { Leaf l = 1; }\n",
		"leaf/ya.make": `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
NO_MYPY()
SRCS(leaf.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`,
		"leaf/leaf.proto":                 "syntax = \"proto3\";\nmessage Leaf { int32 x = 1; }\n",
		"contrib/libs/protobuf/ya.make":   "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n",
		"contrib/python/protobuf/ya.make": "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n",
		"contrib/libs/python/ya.make":     "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
	}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")
	writeToolProgram(files, modPath+"/gen", "gen")

	g := testGen(newMemFS(files), consumer)

	const markupPbH = "$(B)/dep/markup.pb.h"
	const leafPbH = "$(B)/leaf/leaf.pb.h"

	cc := mustNodeByOutput(t, g, "$(B)/"+modPath+"/gen.pb.cc.o")

	if !nodeHasInput(cc, markupPbH) {
		t.Errorf("gen.pb.cc.o missing OUTPUT_INCLUDES import header %q: %v", markupPbH, vfsStringsT3(cc.flatInputs()))
	}

	if !nodeHasInput(cc, leafPbH) {
		t.Errorf("gen.pb.cc.o missing transitive import header %q: %v", leafPbH, vfsStringsT3(cc.flatInputs()))
	}

	depCC := mustNodeByOutput(t, g, "$(B)/dep/markup.pb.cc.o")

	if !nodeHasInput(depCC, leafPbH) {
		t.Errorf("checked-in dep markup.pb.cc.o missing real import %q: %v", leafPbH, vfsStringsT3(depCC.flatInputs()))
	}

	if nodeHasInput(depCC, "$(B)/"+modPath+"/gen.pb.h") {
		t.Errorf("checked-in dep markup.pb.cc.o unexpectedly carries generated gen.pb.h: %v", vfsStringsT3(depCC.flatInputs()))
	}
}

func TestEmitProtoSrcs_ForwardSameModuleImportCarriesGeneratedPbH(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")

	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"ADDINCL(GLOBAL contrib/libs/protobuf/src)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/message.h", "#pragma once\n")

	writeTestModuleFile(files, "m/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"PEERDIR(contrib/libs/protobuf)\nSRCS(main.proto dep.proto)\nEND()\n")
	writeTestModuleFile(files, "m/main.proto",
		"syntax = \"proto3\";\nimport \"m/dep.proto\";\nmessage Main { Dep d = 1; }\n")
	writeTestModuleFile(files, "m/dep.proto",
		"syntax = \"proto3\";\nmessage Dep { int32 x = 1; }\n")

	g := testGen(newMemFS(files), "m")

	const depPbH = "$(B)/m/dep.pb.h"
	cc := mustNodeByOutput(t, g, "$(B)/m/main.pb.cc.o")

	if !nodeHasInput(cc, depPbH) {
		t.Errorf("main.pb.cc.o missing forward same-module import header %q: %v", depPbH, vfsStringsT3(cc.flatInputs()))
	}
}

func TestEmitProtoSrcs_AntlrCppOutsCompileIntoProtoArchive(t *testing.T) {
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

    CONFIGURE_FILE(${ARCADIA_ROOT}/templates/Cpp.stg.in ${antlr_templates}/Cpp/Cpp.stg)
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

    RUN_ANTLR(
        ${jsonpath_grammar}
        -lib .
        -fo ${antlr_output}
        IN ${jsonpath_grammar} ${antlr_templates}/Cpp/Cpp.stg
        OUT JsonPathParser.cpp JsonPathLexer.cpp JsonPathParser.h JsonPathLexer.h
        CWD ${antlr_output}
    )
ENDIF()

SRCS(JsonPathParser.proto)

EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)

END()
`,
		"templates/Cpp.stg.in":                       "stub cpp stg\n",
		"templates/protobuf.stg.in":                  "stub protobuf stg\n",
		"yql/essentials/minikql/jsonpath/JsonPath.g": "stub grammar\n",
		"contrib/libs/protobuf/ya.make":              "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n",
	} {
		files[path] = body
	}

	g := testGen(newMemFS(files), modPath)

	byOut := make(map[string]*Node, len(g.Graph))

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 {
			byOut[n.Outputs[0].string()] = n
		}
	}

	for _, key := range []string{
		"$(B)/" + modPath + "/JsonPathLexer.cpp.o",
		"$(B)/" + modPath + "/JsonPathParser.cpp.o",
	} {
		if byOut[key] == nil {
			t.Errorf("graph missing CC node with output %q", key)
		}
	}

	ar := byOut["$(B)/"+modPath+"/libproto_ast-gen-jsonpath.a"]

	if ar == nil {
		t.Fatal("no proto AR node emitted")
	}

	for _, want := range []string{
		"$(B)/" + modPath + "/JsonPathLexer.cpp.o",
		"$(B)/" + modPath + "/JsonPathParser.cpp.o",
		"$(B)/" + modPath + "/JsonPathParser.pb.cc.o",
	} {
		found := false

		for _, in := range ar.flatInputs() {
			if in.string() == want {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("proto AR inputs missing %q: %v", want, ar.flatInputs())
		}
	}

	idxOf := func(rel string) int {
		want := "$(B)/" + modPath + "/" + rel

		for i, in := range ar.flatInputs() {
			if in.string() == want {
				return i
			}
		}

		return -1
	}
	parserCpp := idxOf("JsonPathParser.cpp.o")
	lexerCpp := idxOf("JsonPathLexer.cpp.o")
	pbCC := idxOf("JsonPathParser.pb.cc.o")

	if parserCpp < 0 || lexerCpp < 0 || pbCC < 0 {
		t.Fatalf("missing AR member (parser=%d lexer=%d pb=%d): %v", parserCpp, lexerCpp, pbCC, ar.flatInputs())
	}

	if !(parserCpp < pbCC && lexerCpp < pbCC) {
		t.Errorf("ANTLR .cpp.o must precede .pb.cc.o in proto AR: parser=%d lexer=%d pb.cc=%d (%v)",
			parserCpp, lexerCpp, pbCC, ar.flatInputs())
	}
}

func TestEmitProtoSrcs_YaffArchiveMemberOrderFollowsCppOutsOrder(t *testing.T) {
	mkFiles := func() map[string]string {
		files := map[string]string{}
		writeToolProgram(files, "contrib/tools/protoc", "protoc")
		writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
		writeToolProgram(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
		writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
		writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
		writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

		return files
	}

	arMemberIdx := func(t *testing.T, g *Graph, modPath, rel string) int {
		t.Helper()
		ar := mustNodeByOutput(t, g, "$(B)/"+modPath+"/"+archiveNameWithPrefixOrName(modPath, "lib", ""))
		want := "$(B)/" + modPath + "/" + rel

		for i, in := range ar.flatInputs() {
			if in.string() == want {
				return i
			}
		}

		t.Fatalf("AR for %s missing member %q: %v", modPath, want, ar.flatInputs())

		return -1
	}

	beforeFiles := mkFiles()
	writeTestModuleFile(beforeFiles, "before/ya.make",
		"PROTO_LIBRARY()\nYAFF()\nSRCS(foo.proto)\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(beforeFiles, "before/foo.proto", "syntax = \"proto3\";\npackage test;\nmessage Foo { string v = 1; }\n")

	gBefore := testGen(newMemFS(beforeFiles), "before")
	yaffBefore := arMemberIdx(t, gBefore, "before", "foo.yaff.cpp.o")
	pbBefore := arMemberIdx(t, gBefore, "before", "foo.pb.cc.o")

	if !(yaffBefore < pbBefore) {
		t.Errorf("YAFF-before-SET: .yaff.cpp.o (%d) must precede .pb.cc.o (%d)", yaffBefore, pbBefore)
	}

	afterFiles := mkFiles()
	writeTestModuleFile(afterFiles, "after/ya.make",
		"PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nYAFF()\nSRCS(foo.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(afterFiles, "after/foo.proto", "syntax = \"proto3\";\npackage test;\nmessage Foo { string v = 1; }\n")

	gAfter := testGen(newMemFS(afterFiles), "after")
	pbAfter := arMemberIdx(t, gAfter, "after", "foo.pb.cc.o")
	yaffAfter := arMemberIdx(t, gAfter, "after", "foo.yaff.cpp.o")

	if !(pbAfter < yaffAfter) {
		t.Errorf("SET-before-YAFF: .pb.cc.o (%d) must precede .yaff.cpp.o (%d)", pbAfter, yaffAfter)
	}
}

func TestEmitProtoSrcs_EvArchiveMemberOrderFollowsSrcsOrder(t *testing.T) {
	const modPath = "search/idlmix"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/event2cpp", "event2cpp")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "library/cpp/eventlog/ya.make", "LIBRARY()\nSRCS(eventlog.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/eventlog/eventlog.cpp", "int eventlog(){return 0;}\n")

	writeTestModuleFile(files, modPath+"/ya.make",
		"PROTO_LIBRARY()\nSRCS(a.proto e.ev b.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, modPath+"/a.proto", "syntax = \"proto3\";\npackage test;\nmessage A { string v = 1; }\n")
	writeTestModuleFile(files, modPath+"/b.proto", "syntax = \"proto3\";\npackage test;\nmessage B { string v = 1; }\n")
	writeTestModuleFile(files, modPath+"/e.ev", "message TEvent {\n}\n")

	g := testGen(newMemFS(files), modPath)

	ar := mustNodeByOutput(t, g, "$(B)/"+modPath+"/"+archiveNameWithPrefixOrName(modPath, "lib", ""))
	idxOf := func(rel string) int {
		want := "$(B)/" + modPath + "/" + rel

		for i, in := range ar.flatInputs() {
			if in.string() == want {
				return i
			}
		}

		return -1
	}

	aIdx := idxOf("a.pb.cc.o")
	eIdx := idxOf("e.ev.pb.cc.o")
	bIdx := idxOf("b.pb.cc.o")

	if aIdx < 0 || eIdx < 0 || bIdx < 0 {
		t.Fatalf("missing AR member (a=%d e=%d b=%d): %v", aIdx, eIdx, bIdx, ar.flatInputs())
	}

	if !(aIdx < eIdx && eIdx < bIdx) {
		t.Errorf(".ev.pb.cc.o must archive between surrounding proto objects in SRCS order: a=%d e=%d b=%d (%v)",
			aIdx, eIdx, bIdx, ar.flatInputs())
	}
}

func TestEmitProtoSrcs_EnumSerializationOrderByHeaderProvenance(t *testing.T) {
	const modPath = "mod/api"
	const extPath = "ext/protos"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	writeTestModuleFile(files, extPath+"/ya.make", "PROTO_LIBRARY()\nSRCS(external.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, extPath+"/external.proto", "syntax = \"proto3\";\npackage ext;\nenum X { X0 = 0; }\nmessage E { X x = 1; }\n")

	writeTestModuleFile(files, modPath+"/ya.make", `PROTO_LIBRARY()
PEERDIR(`+extPath+`)
SRCS(own.proto tail.proto)
GENERATE_ENUM_SERIALIZATION(${ARCADIA_BUILD_ROOT}/`+extPath+`/external.pb.h)
GENERATE_ENUM_SERIALIZATION(${ARCADIA_BUILD_ROOT}/`+modPath+`/own.pb.h)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, modPath+"/own.proto", "syntax = \"proto3\";\npackage api;\nenum O { O0 = 0; }\nmessage Own { O o = 1; }\n")
	writeTestModuleFile(files, modPath+"/tail.proto", "syntax = \"proto3\";\npackage api;\nmessage Tail { string v = 1; }\n")

	g := testGen(newMemFS(files), modPath)

	ar := mustNodeByOutput(t, g, "$(B)/"+modPath+"/"+archiveNameWithPrefixOrName(modPath, "lib", ""))
	idxOfSuffix := func(suffix string) int {
		for i, in := range ar.flatInputs() {
			if strings.HasSuffix(in.string(), "/"+suffix) {
				return i
			}
		}

		t.Fatalf("archive missing member with suffix %q: %v", suffix, vfsStrings(ar.flatInputs()))

		return -1
	}

	extEN := idxOfSuffix("external.pb.h_serialized.cpp.o")
	own := idxOfSuffix("own.pb.cc.o")
	tail := idxOfSuffix("tail.pb.cc.o")
	ownEN := idxOfSuffix("own.pb.h_serialized.cpp.o")

	if !(extEN < own && own < tail && tail < ownEN) {
		t.Fatalf("archive order external_EN(%d) < own.pb.cc.o(%d) < tail.pb.cc.o(%d) < own_EN(%d) violated: %v",
			extEN, own, tail, ownEN, vfsStrings(ar.flatInputs()))
	}
}

func TestEmitPyProtoSrc_GeneratedProtoWiresProducerDep(t *testing.T) {
	const modPath = "yql/essentials/parser/proto_ast/gen/jsonpath"
	const consumer = "app/pytool"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "tools/rescompressor", "rescompressor")
	writeToolProgram(files, "tools/archiver", "archiver")

	for path, body := range map[string]string{
		consumer + "/ya.make": `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(` + modPath + `)
END()
`,
		modPath + "/ya.make": `PROTO_LIBRARY()
NO_MYPY()

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
		"contrib/python/protobuf/ya.make":            "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n",
		"contrib/libs/python/ya.make":                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n",
	} {
		files[path] = body
	}

	g := testGen(newMemFS(files), consumer)

	var pyPB *Node

	for _, n := range g.Graph {
		if n.KV.P == pkPB &&
			strings.HasSuffix(n.Outputs[0].string(), "JsonPathParser__intpy3___pb2.py") {
			pyPB = n

			break
		}
	}

	if pyPB == nil {
		t.Fatal("no python PB node for JsonPathParser__intpy3___pb2.py emitted")
	}

	byOut := make(map[string]*Node, len(g.Graph))

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 {
			byOut[n.Outputs[0].string()] = n
		}
	}

	jv := byOut["$(B)/"+modPath+"/JsonPathParser.proto"]

	if jv == nil {
		t.Fatal("no JV node producing JsonPathParser.proto")
	}

	hasBuildProto := false
	hasSourceProto := false

	for _, in := range pyPB.flatInputs() {
		switch in.string() {
		case "$(B)/" + modPath + "/JsonPathParser.proto":
			hasBuildProto = true
		case "$(S)/" + modPath + "/JsonPathParser.proto":
			hasSourceProto = true
		}
	}

	if !hasBuildProto {
		t.Errorf("py PB.flatInputs() does not include $(B)/.../JsonPathParser.proto: %v", vfsStringsT3(pyPB.flatInputs()))
	}

	if hasSourceProto {
		t.Errorf("py PB.flatInputs() still lists the nonexistent $(S)/.../JsonPathParser.proto: %v", vfsStringsT3(pyPB.flatInputs()))
	}

	found := false

	for _, d := range graphDeps(g, pyPB) {
		if d == jv.UID {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("graphDeps(g, pyPB) %v does not include JV(.proto) uid %q", graphDeps(g, pyPB), jv.UID)
	}

	have := make(map[string]struct{}, len(pyPB.flatInputs()))

	for _, in := range pyPB.flatInputs() {
		have[in.string()] = struct{}{}
	}

	for _, want := range []string{
		"$(S)/yql/essentials/minikql/jsonpath/JsonPath.g",
		"$(S)/templates/protobuf.stg.in",
		"$(S)/contrib/java/antlr/antlr3/antlr.jar",
		"$(S)/build/scripts/configure_file.py",
		"$(S)/build/scripts/stdout2stderr.py",
	} {
		if _, ok := have[want]; !ok {
			t.Errorf("py PB.flatInputs() missing producer source input %q: %v", want, vfsStringsT3(pyPB.flatInputs()))
		}
	}
}

func TestEmitProtoSrcs_SetAppendProtoFilesNotDoubled(t *testing.T) {
	const modPath = "grut/libs/proto/public/auxiliary"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	for path, body := range map[string]string{
		modPath + "/ya.make": `PROTO_LIBRARY()

SET_APPEND(PROTO_FILES
    foo.proto
)

SRCS(${PROTO_FILES})

LIST_PROTO(${PROTO_FILES})

EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)

END()
`,
		modPath + "/foo.proto":          "syntax = \"proto3\";\npackage grut.auxiliary;\n",
		"contrib/libs/protobuf/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n",
	} {
		files[path] = body
	}

	g := testGen(newMemFS(files), modPath)

	var pbH, pbCC int

	for _, n := range g.Graph {
		if n.KV.P != pkPB {
			continue
		}

		for _, out := range n.Outputs {
			switch out.string() {
			case "$(B)/" + modPath + "/foo.pb.h":
				pbH++
			case "$(B)/" + modPath + "/foo.pb.cc":
				pbCC++
			}
		}
	}

	if pbH != 1 {
		t.Errorf("expected exactly one PB producer for foo.pb.h, got %d", pbH)
	}

	if pbCC != 1 {
		t.Errorf("expected exactly one PB producer for foo.pb.cc, got %d", pbCC)
	}
}

func TestEmitProtoSrcs_SrcDirAscentObjectPath(t *testing.T) {
	const modPath = "market/proto/content/ir/common"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	for path, body := range map[string]string{
		modPath + "/ya.make": `PROTO_LIBRARY()

SRCDIR(market/proto/content/ir)

SRCS(BusinessCleanWebStatus.proto)

EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)

END()
`,
		"market/proto/content/ir/BusinessCleanWebStatus.proto": "syntax = \"proto3\";\npackage market.proto.content.ir;\n",
		"contrib/libs/protobuf/ya.make":                        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n",
	} {
		files[path] = body
	}

	g := testGen(newMemFS(files), modPath)

	want := "$(B)/" + modPath + "/__/BusinessCleanWebStatus.pb.cc.o"
	bad := "$(B)/" + modPath + "/_/market/proto/content/ir/BusinessCleanWebStatus.pb.cc.o"

	var gotObj bool

	for _, n := range g.Graph {
		if n.KV.P != pkCC {
			continue
		}

		for _, out := range n.Outputs {
			if out.string() == bad {
				t.Errorf("CC object uses _/<full-path> shape, want __ ascent: %q", bad)
			}

			if out.string() == want {
				gotObj = true
			}
		}
	}

	if !gotObj {
		var ccOuts []string

		for _, n := range g.Graph {
			if n.KV.P == pkCC {
				ccOuts = append(ccOuts, n.Outputs[0].string())
			}
		}

		t.Errorf("missing SRCDIR-ascent proto object %q; CC outputs: %v", want, ccOuts)
	}
}

func TestGen_PyProtoLibrary_TransitivePROTONamespaceReachesPyProtoCmd(t *testing.T) {
	const consumer = "app/pytool"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")

	writeTestModuleFile(files, "yt/yt_proto/yt/core/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(yt)
SRCS(core.proto)
END()
`)
	writeTestModuleFile(files, "yt/yt_proto/yt/core/core.proto", "syntax = \"proto3\";\npackage yt;\nmessage Core {}\n")

	writeTestModuleFile(files, "grut/libs/proto/public/metadata/ya.make", `PROTO_LIBRARY()
PEERDIR(yt/yt_proto/yt/core)
SRCS(meta.proto)
END()
`)
	writeTestModuleFile(files, "grut/libs/proto/public/metadata/meta.proto", "syntax = \"proto3\";\npackage test;\nmessage Meta {}\n")

	writeTestModuleFile(files, "ads/autobudget/protos/ya.make", `PROTO_LIBRARY()
PEERDIR(grut/libs/proto/public/metadata)
SRCS(brand.proto)
END()
`)
	writeTestModuleFile(files, "ads/autobudget/protos/brand.proto", "syntax = \"proto3\";\npackage test;\nmessage Brand {}\n")

	writeTestModuleFile(files, consumer+"/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(ads/autobudget/protos)
END()
`)
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nADDINCL(GLOBAL FOR proto contrib/libs/protobuf/src)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/python/protobuf/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/python/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")

	g := testGen(newMemFS(files), consumer)

	var pyPB *Node

	for _, n := range g.Graph {
		if n.KV.P == pkPB &&
			strings.HasSuffix(n.Outputs[0].string(), "brand__intpy3___pb2.py") {
			pyPB = n

			break
		}
	}

	if pyPB == nil {
		t.Fatal("no python PB node for brand__intpy3___pb2.py emitted")
	}

	args := pyPB.Cmds[0].CmdArgs.flat()

	ytCount := 0

	for _, a := range args {
		if a.string() == "-I=$(S)/yt" {
			ytCount++
		}
	}

	if ytCount == 0 {
		t.Fatalf("py PB cmd missing transitive PROTO_NAMESPACE token -I=$(S)/yt: %v", strStrs(args))
	}

	if ytCount > 1 {
		t.Fatalf("py PB cmd duplicates -I=$(S)/yt (%d times): %v", ytCount, strStrs(args))
	}

	protobufSrcIdx := indexOfArg(args, "-I=$(S)/contrib/libs/protobuf/src")
	ytIdx := indexOfArg(args, "-I=$(S)/yt")
	protocSrcIdx := indexOfArg(args, "-I=$(S)/contrib/libs/protoc/src")
	pyOutIdx := indexOfArg(args, "--python_out=$(B)/")

	if protobufSrcIdx < 0 || pyOutIdx < 0 {
		t.Fatalf("py PB cmd missing protobuf-src / python_out anchors: %v", strStrs(args))
	}

	if !(ytIdx < protobufSrcIdx && protobufSrcIdx < pyOutIdx) {
		t.Fatalf("expected yt < protobuf-src < python_out: yt=%d protobuf-src=%d python_out=%d args=%v",
			ytIdx, protobufSrcIdx, pyOutIdx, strStrs(args))
	}

	if protocSrcIdx >= 0 && !(protobufSrcIdx < protocSrcIdx) {
		t.Fatalf("expected protobuf-src before protoc-src: protobuf-src=%d protoc-src=%d args=%v", protobufSrcIdx, protocSrcIdx, strStrs(args))
	}

	if pyOutIdx < 2 || args[pyOutIdx-1].string() != "-I=$(S)/contrib/libs/protobuf/src" || args[pyOutIdx-2].string() != "-I=$(B)" {
		t.Fatalf("expected trailing -I=$(B) -I=$(S)/contrib/libs/protobuf/src before --python_out: %v", strStrs(args))
	}
}

func TestGen_PyProtoLibrary_ProtobufBuiltinKeepsBandProtobufSrc(t *testing.T) {
	const consumer = "app/pytool"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")

	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(contrib/libs/protobuf/src)
NO_MYPY()
DISABLE(NEED_GOOGLE_PROTO_PEERDIRS)
ADDINCL(GLOBAL FOR proto contrib/libs/protobuf/src)
SRCS(google/protobuf/any.proto)
EXCLUDE_TAGS(GO_PROTO)
END()
`)
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/any.proto", "syntax = \"proto3\";\npackage google.protobuf;\nmessage Any {}\n")

	writeTestModuleFile(files, consumer+"/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(contrib/libs/protobuf)
END()
`)
	writeTestModuleFile(files, "contrib/python/protobuf/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/python/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")

	g := testGen(newMemFS(files), consumer)

	args := pyProtoCmdArgsForOutput(t, g, "any__intpy3___pb2.py")

	const protobufSrc = "-I=$(S)/contrib/libs/protobuf/src"

	pyOutIdx := indexOfArg(args, "--python_out=$(B)/contrib/libs/protobuf/src")

	if pyOutIdx < 2 || args[pyOutIdx-1].string() != protobufSrc || args[pyOutIdx-2].string() != "-I=$(B)" {
		t.Fatalf("expected trailing -I=$(B) %s before --python_out: %v", protobufSrc, strStrs(args))
	}

	bareIdx := indexOfArg(args, "-I=$(S)")
	trailingBIdx := pyOutIdx - 2

	if bareIdx < 0 {
		t.Fatalf("missing structural bare -I=$(S): %v", strStrs(args))
	}

	bandCopy := false

	for i := bareIdx + 1; i < trailingBIdx; i++ {
		if args[i].string() == protobufSrc {
			bandCopy = true

			break
		}
	}

	if !bandCopy {
		t.Fatalf("band protobuf-src include collapsed for the protobuf builtin (only prefix+trailing remain): %v", strStrs(args))
	}

	if protocSrcIdx := indexOfArg(args, "-I=$(S)/contrib/libs/protoc/src"); protocSrcIdx >= 0 {
		t.Fatalf("protobuf builtin must not carry protoc-src include: %v", strStrs(args))
	}
}

func TestGen_ProtoLibrary_TransitiveGlobalNamespaceInterleavesInBothCmds(t *testing.T) {
	const consumer = "app/pytool"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")

	writeTestModuleFile(files, "lib/gapis/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(GLOBAL lib/gapis)
SRCS(g.proto)
END()
`)
	writeTestModuleFile(files, "lib/gapis/g.proto", "syntax = \"proto3\";\npackage gapis;\nmessage G {}\n")

	writeTestModuleFile(files, "lib/yt/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(yt)
PEERDIR(lib/gapis)
SRCS(y.proto)
END()
`)
	writeTestModuleFile(files, "lib/yt/y.proto", "syntax = \"proto3\";\npackage yt;\nmessage Y {}\n")

	writeTestModuleFile(files, "app/proto/ya.make", `PROTO_LIBRARY()
PEERDIR(lib/yt)
SRCS(c.proto)
END()
`)
	writeTestModuleFile(files, "app/proto/c.proto", "syntax = \"proto3\";\npackage app;\nmessage C {}\n")

	writeTestModuleFile(files, consumer+"/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(app/proto)
END()
`)
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nADDINCL(GLOBAL FOR proto contrib/libs/protobuf/src)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/python/protobuf/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/python/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")

	g := testGen(newMemFS(files), consumer)

	const ytTok = "-I=$(S)/yt"
	const gapisTok = "-I=$(S)/lib/gapis"

	assertInterleavedBand := func(label string, args []STR) {
		t.Helper()
		ytIdx := indexOfArg(args, ytTok)
		gapisIdx := indexOfArg(args, gapisTok)
		gapisCount := 0

		for _, a := range args {
			if a.string() == gapisTok {
				gapisCount++
			}
		}

		if ytIdx < 0 {
			t.Fatalf("%s: missing bare namespace %s: %v", label, ytTok, strStrs(args))
		}

		if gapisCount == 0 {
			t.Fatalf("%s: missing transitive GLOBAL namespace %s: %v", label, gapisTok, strStrs(args))
		}

		if gapisCount > 1 {
			t.Fatalf("%s: GLOBAL namespace %s duplicated (%d): %v", label, gapisTok, gapisCount, strStrs(args))
		}

		if !(ytIdx < gapisIdx) {
			t.Fatalf("%s: expected bare yt (%d) before GLOBAL gapis (%d): %v", label, ytIdx, gapisIdx, strStrs(args))
		}
	}

	var cppArgs []STR

	for _, n := range g.Graph {
		if n.KV.P == pkPB &&
			len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), "app/proto/c.pb.h") {
			cppArgs = n.Cmds[0].CmdArgs.flat()

			break
		}
	}

	if cppArgs == nil {
		t.Fatal("no C++ PB node for app/proto/c.pb.h emitted")
	}

	assertInterleavedBand("cpp", cppArgs)

	assertInterleavedBand("py", pyProtoCmdArgsForOutput(t, g, "c__intpy3___pb2.py"))
}

func TestProtoPythonResourceKey_PYNamespacePreservesNestedSubdir(t *testing.T) {
	instance := ModuleInstance{Path: source("yt/yt_proto/yt/client")}
	d := &ModuleData{pyNamespace: strPtr(internStr("yt_proto.yt.client"))}

	got := protoPythonResourceKey(instance, d, "chunk_client/proto/data_statistics.proto", "_pb2.py")
	const want = "yt_proto/yt/client/chunk_client/proto/data_statistics_pb2.py"

	if got != want {
		t.Errorf("nested PY_NAMESPACE key = %q, want %q", got, want)
	}

	const collapsed = "yt_proto/yt/client/data_statistics_pb2.py"

	if got == collapsed {
		t.Errorf("key collapsed nested subdir to %q", collapsed)
	}

	gotRoot := protoPythonResourceKey(instance, d, "access_control_service.proto", "_pb2.py")
	const wantRoot = "yt_proto/yt/client/access_control_service_pb2.py"

	if gotRoot != wantRoot {
		t.Errorf("root-level PY_NAMESPACE key = %q, want %q", gotRoot, wantRoot)
	}
}

func pyProtoCmdArgsForOutput(t *testing.T, g *Graph, wantSuffix string) []STR {
	t.Helper()

	for _, n := range g.Graph {
		if n.KV.P == pkPB &&
			len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), wantSuffix) {
			return n.Cmds[0].CmdArgs.flat()
		}
	}

	t.Fatalf("no python PB node for %q", wantSuffix)

	return nil
}

func assertYtNamespaceDuplicated(t *testing.T, args []STR) {
	t.Helper()
	ytCount := 0

	for _, a := range args {
		if a.string() == "-I=$(S)/yt" {
			ytCount++
		}
	}

	if ytCount != 3 {
		t.Fatalf("expected 3 -I=$(S)/yt (output-root + duplicated _PROTO__INCLUDE), got %d: %v", ytCount, strStrs(args))
	}

	bare := indexOfArg(args, "-I=$(S)")

	if bare < 0 || bare+3 >= len(args) {
		t.Fatalf("missing bare -I=$(S) anchor: %v", strStrs(args))
	}

	if args[bare+1].string() != "-I=$(S)/yt" || args[bare+2].string() != "-I=$(S)/yt" {
		t.Fatalf("expected two consecutive -I=$(S)/yt after -I=$(S): %v", strStrs(args))
	}

	if args[bare+3].string() != "-I=$(S)/contrib/libs/protobuf/src" {
		t.Fatalf("expected protobuf-src after the duplicated namespace: %v", strStrs(args))
	}
}

func TestGen_PyProtoLibrary_OwnPROTONamespaceDuplicatesNamespaceInclude(t *testing.T) {
	const consumer = "app/pytool"
	const mod = "yt/yt_proto/yt/client"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")

	writeTestModuleFile(files, mod+"/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(yt)
PY_NAMESPACE(yt_proto.yt.client)
SRCS(chunk_client/proto/data_statistics.proto)
EXCLUDE_TAGS(GO_PROTO)
END()
`)
	writeTestModuleFile(files, mod+"/chunk_client/proto/data_statistics.proto", "syntax = \"proto3\";\npackage yt;\nmessage DataStatistics {}\n")

	writeTestModuleFile(files, consumer+"/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(`+mod+`)
END()
`)
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nADDINCL(GLOBAL FOR proto contrib/libs/protobuf/src)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/python/protobuf/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/python/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")

	g := testGen(newMemFS(files), consumer)

	args := pyProtoCmdArgsForOutput(t, g, "data_statistics__intpy3___pb2.py")
	assertYtNamespaceDuplicated(t, args)

	const wantKey = "resfs/file/py/yt_proto/yt/client/chunk_client/proto/data_statistics_pb2.py"
	const collapsedKey = "resfs/file/py/yt_proto/yt/client/data_statistics_pb2.py"
	foundKey, foundCollapsed := false, false

	for _, n := range g.Graph {
		if n.KV.P != pkPR {
			continue
		}

		for _, a := range n.Cmds[0].CmdArgs.flat() {
			s := a.string()

			if strings.Contains(s, wantKey) {
				foundKey = true
			}

			if strings.Contains(s, collapsedKey) {
				foundCollapsed = true
			}
		}
	}

	if !foundKey {
		t.Errorf("no aux resource key %q found", wantKey)
	}

	if foundCollapsed {
		t.Errorf("aux resource key still collapsed to %q", collapsedKey)
	}
}

func TestGen_PyProtoLibrary_GrpcRootSourceSharesDuplicateInclude(t *testing.T) {
	const consumer = "app/pytool"
	const mod = "yt/yt_proto/yt/orm/api"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")
	writeToolProgram(files, "contrib/tools/protoc/plugins/grpc_python", "grpc_python")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")

	writeTestModuleFile(files, mod+"/ya.make", `PROTO_LIBRARY()
GRPC()
PROTO_NAMESPACE(yt)
PY_NAMESPACE(yt_proto.yt.orm.api)
SRCS(access_control_service.proto)
END()
`)
	writeTestModuleFile(files, mod+"/access_control_service.proto", "syntax = \"proto3\";\npackage yt;\nmessage Access {}\n")

	writeTestModuleFile(files, consumer+"/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(`+mod+`)
END()
`)
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nADDINCL(GLOBAL FOR proto contrib/libs/protobuf/src)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/python/protobuf/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n")
	writeTestModuleFile(files, "contrib/python/grpcio/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/grpc/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/python/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")

	g := testGen(newMemFS(files), consumer)

	args := pyProtoCmdArgsForOutput(t, g, "access_control_service__intpy3___pb2.py")
	assertYtNamespaceDuplicated(t, args)

	hasGrpcOut := false

	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.string(), "access_control_service__intpy3___pb2_grpc.py") {
				hasGrpcOut = true
			}
		}
	}

	if !hasGrpcOut {
		t.Fatal("grpc python output access_control_service__intpy3___pb2_grpc.py missing")
	}
}

func TestEmitProtoSrcs_CppEvlogCarriesEvent2cppInducedDeps(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "library/cpp/eventlog/ya.make", "LIBRARY()\nSRCS(eventlog.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/eventlog/eventlog.cpp", "int eventlog(){return 0;}\n")

	writeTestModuleFile(files, "tools/event2cpp/ya.make",
		"PROGRAM(event2cpp)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nINDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/runtime/eventlog_runtime.h)\nSRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/event2cpp/main.cpp", "int main(){return 0;}\n")
	writeTestModuleFile(files, "runtime/eventlog_runtime.h", "#pragma once\n")

	writeTestModuleFile(files, "evlog/ya.make",
		"PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nCPP_EVLOG()\nSRCS(foo.proto)\nEND()\n")
	writeTestModuleFile(files, "evlog/foo.proto", "syntax = \"proto3\";\npackage test;\nmessage Foo { string v = 1; }\n")

	writeTestModuleFile(files, "plain/ya.make",
		"PROTO_LIBRARY()\nSET(PROTOC_TRANSITIVE_HEADERS \"no\")\nSRCS(foo.proto)\nEND()\n")
	writeTestModuleFile(files, "plain/foo.proto", "syntax = \"proto3\";\npackage test;\nmessage Foo { string v = 1; }\n")

	const induced = "$(S)/runtime/eventlog_runtime.h"

	gEv := testGen(newMemFS(files), "evlog")
	evCC := mustNodeByOutput(t, gEv, "$(B)/evlog/foo.pb.cc.o")

	if !nodeHasInput(evCC, induced) {
		t.Fatalf("CPP_EVLOG foo.pb.cc.o missing event2cpp induced input %q: %v", induced, evCC.flatInputs())
	}

	pb := mustNodeByOutput(t, gEv, "$(B)/evlog/foo.pb.h")
	const event2cppBinary = "$(B)/tools/event2cpp/event2cpp"
	pbArgs := strStrs(pb.Cmds[0].CmdArgs.flat())
	const wantPlugin = "--plugin=protoc-gen-event2cpp=" + event2cppBinary
	const wantOut = "--event2cpp_out=$(B)/"

	if !containsString(pbArgs, wantPlugin) {
		t.Fatalf("CPP_EVLOG pb cmd missing event2cpp plugin token %q: %v", wantPlugin, pbArgs)
	}

	if !containsString(pbArgs, wantOut) {
		t.Fatalf("CPP_EVLOG pb cmd missing event2cpp out token %q: %v", wantOut, pbArgs)
	}

	srcIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "evlog/foo.proto")
	pluginIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), wantPlugin)
	outIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), wantOut)

	if srcIdx < 0 || pluginIdx < 0 || outIdx < 0 {
		t.Fatalf("CPP_EVLOG pb cmd missing src/plugin/out args: src=%d plugin=%d out=%d (%v)", srcIdx, pluginIdx, outIdx, pbArgs)
	}

	if !(srcIdx < pluginIdx && srcIdx < outIdx) {
		t.Fatalf("CPP_EVLOG pb plugin tokens must follow source: src=%d plugin=%d out=%d", srcIdx, pluginIdx, outIdx)
	}

	if !nodeHasInput(pb, event2cppBinary) {
		t.Fatalf("CPP_EVLOG pb producer missing event2cpp tool input %q: %v", event2cppBinary, pb.flatInputs())
	}

	event2cppNode := mustNodeByOutput(t, gEv, event2cppBinary)
	refs := 0

	for _, dep := range graphDeps(gEv, pb) {
		if dep == event2cppNode.UID {
			refs++
		}
	}

	if refs != 1 {
		t.Fatalf("CPP_EVLOG pb event2cpp generator ref count = %d, want 1 (no duplicate)", refs)
	}

	gPlain := testGen(newMemFS(files), "plain")
	plainCC := mustNodeByOutput(t, gPlain, "$(B)/plain/foo.pb.cc.o")

	if nodeHasInput(plainCC, induced) {
		t.Fatalf("non-CPP_EVLOG foo.pb.cc.o unexpectedly carries event2cpp induced input %q: %v", induced, plainCC.flatInputs())
	}
}

func TestParseInclude_VarBearingPeersListReachesLeafPyProto(t *testing.T) {
	const consumer = "app"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")

	writeTestModuleFile(files, consumer+"/ya.make", `PY3_LIBRARY(app)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
INCLUDE(cfg/name.inc)
INCLUDE(${ARCADIA_ROOT}/gen/artefacts_${CONFIG_NAME}_/peers.lst)
PEERDIR(${FEATURE_PEERDIRS})
END()
`)
	writeTestModuleFile(files, consumer+"/cfg/name.inc", "SET(CONFIG_NAME caesar)\n")
	writeTestModuleFile(files, "gen/artefacts_caesar_/peers.lst", "SET(FEATURE_PEERDIRS feature/model)\n")
	writeTestModuleFile(files, "feature/model/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(leaf/proto)
END()
`)
	writeTestModuleFile(files, "leaf/proto/ya.make", `PROTO_LIBRARY()
NO_MYPY()
SRCS(enum_options.proto)
EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)
END()
`)
	writeTestModuleFile(files, "leaf/proto/enum_options.proto", "syntax = \"proto3\";\n")
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"
	files["contrib/python/protobuf/ya.make"] = "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n"
	files["contrib/libs/python/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n"

	g := testGen(newMemFS(files), consumer)

	var pyPB *Node

	for _, n := range g.Graph {
		if n.KV.P == pkPB &&
			len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), "enum_options__intpy3___pb2.py") {
			pyPB = n

			break
		}
	}

	if pyPB == nil {
		t.Fatal("leaf PY3 proto enum_options__intpy3___pb2.py not reachable through variable-bearing include")
	}
}

func TestEmitPyProtoSrcs_ExplicitProtoLibraryNameNamesGlobalArchive(t *testing.T) {
	const consumer = "app/pytool"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")

	writeTestModuleFile(files, "ads/caesar/libs/events/proto/ya.make", `PROTO_LIBRARY(ads-caesar-events-proto)
SRCS(ev.proto)
END()
`)
	writeTestModuleFile(files, "ads/caesar/libs/events/proto/ev.proto", "syntax = \"proto3\";\npackage test;\nmessage Ev {}\n")

	writeTestModuleFile(files, "libs/unnamed/proto/ya.make", `PROTO_LIBRARY()
SRCS(plain.proto)
END()
`)
	writeTestModuleFile(files, "libs/unnamed/proto/plain.proto", "syntax = \"proto3\";\npackage test;\nmessage Plain {}\n")

	writeTestModuleFile(files, consumer+"/ya.make", `PY3_LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
NO_PYTHON_INCLUDES()
PEERDIR(ads/caesar/libs/events/proto)
PEERDIR(libs/unnamed/proto)
END()
`)
	files["contrib/libs/protobuf/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n"
	files["contrib/python/protobuf/ya.make"] = "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nEND()\n"
	files["contrib/libs/python/ya.make"] = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n"

	g := testGen(newMemFS(files), consumer)

	globals := map[string]bool{}

	for _, n := range g.Graph {
		if n.KV.P == pkAR && len(n.Outputs) > 0 &&
			strings.HasSuffix(n.Outputs[0].string(), ".global.a") &&
			strings.Contains(n.Outputs[0].string(), "libpy3") {
			globals[n.Outputs[0].string()] = true
		}
	}

	wantNamed := "$(B)/ads/caesar/libs/events/proto/libpy3ads-caesar-events-proto.global.a"

	if !globals[wantNamed] {
		t.Fatalf("named PROTO_LIBRARY did not produce %s; py3_proto_global archives: %v", wantNamed, globals)
	}

	pathDerivedNamed := "$(B)/ads/caesar/libs/events/proto/libpy3libs-events-proto.global.a"

	if globals[pathDerivedNamed] {
		t.Fatalf("named PROTO_LIBRARY still emits path-derived %s", pathDerivedNamed)
	}

	wantUnnamed := "$(B)/libs/unnamed/proto/libpy3libs-unnamed-proto.global.a"

	if !globals[wantUnnamed] {
		t.Fatalf("unnamed PROTO_LIBRARY did not keep path-derived %s; py3_proto_global archives: %v", wantUnnamed, globals)
	}
}

func protoNsOrderFixture() FS {
	files := map[string]string{}

	writeTestModuleFile(files, "mid/ya.make",
		"LIBRARY()\nPROTO_NAMESPACE(mid)\nPEERDIR(mid/sub deep)\nSRCS(m.cpp)\nEND()\n")
	writeTestModuleFile(files, "mid/m.cpp", "int m(){return 0;}\n")

	writeTestModuleFile(files, "mid/sub/ya.make",
		"LIBRARY()\nADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/mid/sub)\nSRCS(s.cpp)\nEND()\n")
	writeTestModuleFile(files, "mid/sub/s.cpp", "int s(){return 0;}\n")

	writeTestModuleFile(files, "deep/ya.make",
		"LIBRARY()\nADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/mid)\nSRCS(d.cpp)\nEND()\n")
	writeTestModuleFile(files, "deep/d.cpp", "int d(){return 0;}\n")

	writeTestModuleFile(files, "consumer/ya.make",
		"LIBRARY()\nPEERDIR(mid)\nSRCS(c.cpp)\nEND()\n")
	writeTestModuleFile(files, "consumer/c.cpp", "int c(){return 0;}\n")

	return newMemFS(files)
}

func TestGen_BareProtoNamespace_BuildRootIncludeIsGlobalAndOrderedFirst(t *testing.T) {
	g := testGen(protoNsOrderFixture(), "consumer")

	n := mustNodeByOutput(t, g, "$(B)/consumer/c.cpp.o")
	args := n.Cmds[0].CmdArgs.flat()

	iNs := indexOfArg(args, "-I$(B)/mid")
	iSub := indexOfArg(args, "-I$(B)/mid/sub")

	if iNs < 0 {
		t.Fatalf("consumer compile missing -I$(B)/mid\nargs=%v", strStrs(args))
	}

	if iSub < 0 {
		t.Fatalf("consumer compile missing -I$(B)/mid/sub\nargs=%v", strStrs(args))
	}

	if iNs > iSub {
		t.Fatalf("-I$(B)/mid (idx %d) must precede -I$(B)/mid/sub (idx %d)\nargs=%v",
			iNs, iSub, strStrs(args))
	}
}

func protoAddInclFixture() FS {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")

	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"ADDINCL(GLOBAL contrib/libs/protobuf/src)\n"+
			"PEERDIR(contrib/restricted/abseil-cpp-tstring)\n"+
			"SRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/message.h", "#pragma once\n")

	writeTestModuleFile(files, "contrib/restricted/abseil-cpp-tstring/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"ADDINCL(GLOBAL contrib/restricted/abseil-cpp-tstring)\n"+
			"PEERDIR(contrib/restricted/abseil-cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/restricted/abseil-cpp/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"ADDINCL(GLOBAL contrib/restricted/abseil-cpp)\nEND()\n")

	writeTestModuleFile(files, "m/lib/ya.make",
		"LIBRARY()\nSRCS(svc.proto use.cpp)\nEND()\n")
	writeTestModuleFile(files, "m/lib/svc.proto", "syntax = \"proto3\";\npackage m;\nmessage Svc {}\n")
	writeTestModuleFile(files, "m/lib/use.cpp", "int use(){return 0;}\n")

	writeTestModuleFile(files, "consumer/ya.make",
		"LIBRARY()\nPEERDIR(m/lib plain/lib)\nSRCS(c.cpp)\nEND()\n")
	writeTestModuleFile(files, "consumer/c.cpp", "int c(){return 0;}\n")

	writeTestModuleFile(files, "plain/lib/ya.make",
		"LIBRARY()\nSRCS(plain.cpp)\nEND()\n")
	writeTestModuleFile(files, "plain/lib/plain.cpp", "int plain(){return 0;}\n")

	return newMemFS(files)
}

func TestGen_InlineProtoLibrary_ProtobufGlobalAddInclReachesOrdinaryAndConsumer(t *testing.T) {
	fs := protoAddInclFixture()
	g := testGen(fs, "consumer")

	band := []string{
		"-I$(S)/contrib/libs/protobuf/src",
		"-I$(S)/contrib/restricted/abseil-cpp-tstring",
		"-I$(S)/contrib/restricted/abseil-cpp",
	}

	assertBand := func(label, output string, want bool) {
		t.Helper()
		n := mustNodeByOutput(t, g, output)
		args := strStrs(n.Cmds[0].CmdArgs.flat())

		for _, inc := range band {
			has := flagsContain(args, inc)

			if has != want {
				t.Fatalf("%s (%s): include %q present=%v, want %v\nargs=%v", label, output, inc, has, want, args)
			}
		}
	}

	assertBand("inline-proto ordinary src", "$(B)/m/lib/use.cpp.o", true)

	assertBand("inline-proto generated pb.cc", "$(B)/m/lib/svc.pb.cc.o", true)

	assertBand("consumer ordinary src", "$(B)/consumer/c.cpp.o", true)

	assertBand("unrelated module", "$(B)/plain/lib/plain.cpp.o", false)
}

func crossNamespaceProtoFixture() FS {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nADDINCL(GLOBAL contrib/libs/protobuf/src)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")

	writeTestModuleFile(files, "lns/leaf/ya.make",
		"PROTO_LIBRARY()\nPROTO_NAMESPACE(lns)\nSRCS(leaf_a.proto leaf_b.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "lns/leaf/leaf_a.proto",
		"syntax = \"proto3\";\npackage lns;\nimport \"leaf/leaf_b.proto\";\nmessage LeafA { LeafB b = 1; }\n")
	writeTestModuleFile(files, "lns/leaf/leaf_b.proto",
		"syntax = \"proto3\";\npackage lns;\nmessage LeafB { string v = 1; }\n")

	writeTestModuleFile(files, "tns/top/ya.make",
		"PROTO_LIBRARY()\nPROTO_NAMESPACE(tns)\nPEERDIR(lns/leaf)\nSRCS(top.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "tns/top/top.proto",
		"syntax = \"proto3\";\npackage tns;\nimport \"leaf/leaf_a.proto\";\nmessage Top { lns.LeafA a = 1; }\n")

	writeTestModuleFile(files, "app/ya.make",
		"LIBRARY()\nPEERDIR(tns/top)\nSRCS(app.cpp)\nEND()\n")
	writeTestModuleFile(files, "app/app.cpp", "#include \"app.h\"\nint app(){return 0;}\n")
	writeTestModuleFile(files, "app/app.h", "#pragma once\n#include <top/top.pb.h>\n")

	return newMemFS(files)
}

func TestEmitProtoSrcs_CrossNamespaceDirectImportPbHRidesIntoConsumer(t *testing.T) {
	g := testGen(crossNamespaceProtoFixture(), "app")
	appCC := mustNodeByOutput(t, g, "$(B)/app/app.cpp.o")

	for _, want := range []string{
		"$(B)/tns/top/top.pb.h",
		"$(B)/lns/leaf/leaf_a.pb.h",
		"$(B)/lns/leaf/leaf_b.pb.h",
	} {
		if !nodeHasInput(appCC, want) {
			t.Errorf("app.cpp.o missing cross-namespace generated header %q\ninputs=%v", want, vfsStrings(appCC.flatInputs()))
		}
	}
}

func grpcLibraryFixture(t *testing.T, withGrpc bool) *Graph {
	t.Helper()
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/grpc/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")

	grpc := ""

	if withGrpc {
		grpc = "GRPC()\n"
	}

	writeTestModuleFile(files, "m/lib/ya.make", "LIBRARY()\nSRCDIR(m)\nSRCS(svc.proto use.cpp)\n"+grpc+"END()\n")
	writeTestModuleFile(files, "m/svc.proto", "syntax = \"proto3\";\npackage m;\nmessage Svc {}\n")
	writeTestModuleFile(files, "m/use.cpp", "int use(){return 0;}\n")

	return testGen(newMemFS(files), "m/lib")
}

func TestEmitLibraryProtoSource_GrpcEmitsProducerOutputsAndCompile(t *testing.T) {
	g := grpcLibraryFixture(t, true)

	pb := mustNodeByOutput(t, g, "$(B)/m/svc.pb.h")

	hasOut := func(n *Node, want string) bool {
		for _, o := range n.Outputs {
			if o.string() == want {
				return true
			}
		}

		return false
	}

	for _, want := range []string{"$(B)/m/svc.grpc.pb.cc", "$(B)/m/svc.grpc.pb.h"} {
		if !hasOut(pb, want) {
			t.Errorf("pb producer missing grpc output %q; outputs=%v", want, pb.Outputs)
		}
	}

	if !nodeHasInput(pb, "$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp") {
		t.Errorf("pb producer missing grpc_cpp plugin input; inputs=%v", pb.flatInputs())
	}

	args := pb.Cmds[0].CmdArgs.flat()

	if !contains(args, "--grpc_cpp_out=$(B)/") {
		t.Errorf("pb cmd missing --grpc_cpp_out; args=%v", args)
	}

	if !contains(args, "--plugin=protoc-gen-grpc_cpp=$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp") {
		t.Errorf("pb cmd missing grpc_cpp plugin flag; args=%v", args)
	}

	if !graphHasOutputSuffix(g, "svc.grpc.pb.cc.o") {
		t.Errorf("generated .grpc.pb.cc.o compile node missing")
	}
}

func TestEmitLibraryProtoSource_GrpcPluginDepAddInclLeadsDeclaredPeer(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/grpc/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nADDINCL(GLOBAL contrib/libs/grpc/inc)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/grpc/inc/h.h", "#pragma once\n")
	writeTestModuleFile(files, "declared/peer/ya.make", "LIBRARY()\nADDINCL(GLOBAL declared/peer/inc)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "declared/peer/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "declared/peer/inc/h.h", "#pragma once\n")

	writeTestModuleFile(files, "m/lib/ya.make", "LIBRARY()\nPEERDIR(declared/peer)\nSRCDIR(m)\nSRCS(svc.proto use.cpp)\nGRPC()\nEND()\n")
	writeTestModuleFile(files, "m/svc.proto", "syntax = \"proto3\";\npackage m;\nmessage Svc {}\n")
	writeTestModuleFile(files, "m/use.cpp", "int use(){return 0;}\n")

	g := testGen(newMemFS(files), "m/lib")

	cc := mustNodeByOutputSuffix(t, g, "svc.grpc.pb.cc.o")
	args := cc.Cmds[0].CmdArgs.flat()

	grpcInc := indexOfArg(args, "-I$(S)/contrib/libs/grpc/inc")
	declaredInc := indexOfArg(args, "-I$(S)/declared/peer/inc")

	if grpcInc < 0 || declaredInc < 0 {
		t.Fatalf("missing -I dirs in grpc.pb.cc.o compile cmd: grpc=%d declared=%d args=%v", grpcInc, declaredInc, args)
	}

	if grpcInc > declaredInc {
		t.Fatalf("grpc plugin-runtime include must precede declared peer include: contrib/libs/grpc/inc=%d declared/peer/inc=%d", grpcInc, declaredInc)
	}
}

func TestEmitLibraryProtoSource_NonGrpcKeepsDeclaredAddInclOrder(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "peera/ya.make", "LIBRARY()\nADDINCL(GLOBAL peera/inc)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "peera/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "peera/inc/h.h", "#pragma once\n")
	writeTestModuleFile(files, "peerb/ya.make", "LIBRARY()\nADDINCL(GLOBAL peerb/inc)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "peerb/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "peerb/inc/h.h", "#pragma once\n")

	writeTestModuleFile(files, "m/lib/ya.make", "LIBRARY()\nPEERDIR(peera)\nPEERDIR(peerb)\nSRCDIR(m)\nSRCS(svc.proto use.cpp)\nEND()\n")
	writeTestModuleFile(files, "m/svc.proto", "syntax = \"proto3\";\npackage m;\nmessage Svc {}\n")
	writeTestModuleFile(files, "m/use.cpp", "int use(){return 0;}\n")

	g := testGen(newMemFS(files), "m/lib")

	cc := mustNodeByOutputSuffix(t, g, "svc.pb.cc.o")
	args := cc.Cmds[0].CmdArgs.flat()

	aInc := indexOfArg(args, "-I$(S)/peera/inc")
	bInc := indexOfArg(args, "-I$(S)/peerb/inc")

	if aInc < 0 || bInc < 0 {
		t.Fatalf("missing -I dirs in svc.pb.cc.o compile cmd: peera=%d peerb=%d args=%v", aInc, bInc, args)
	}

	if aInc > bInc {
		t.Fatalf("non-grpc inline proto must keep declared peer order: peera/inc=%d peerb/inc=%d", aInc, bInc)
	}
}

func graphHasOutputSuffix(g *Graph, suffix string) bool {
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.string(), suffix) {
				return true
			}
		}
	}

	return false
}

func TestEmitLibraryProtoSource_NoGrpcUnchanged(t *testing.T) {
	g := grpcLibraryFixture(t, false)

	pb := mustNodeByOutput(t, g, "$(B)/m/svc.pb.h")

	for _, o := range pb.Outputs {
		if o.string() == "$(B)/m/svc.grpc.pb.cc" || o.string() == "$(B)/m/svc.grpc.pb.h" {
			t.Errorf("non-GRPC producer unexpectedly declares grpc output %q", o.string())
		}
	}

	if nodeHasInput(pb, "$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp") {
		t.Errorf("non-GRPC producer unexpectedly has grpc_cpp plugin input")
	}

	args := pb.Cmds[0].CmdArgs.flat()

	if contains(args, "--grpc_cpp_out=$(B)/") {
		t.Errorf("non-GRPC producer unexpectedly passes --grpc_cpp_out")
	}

	if graphHasOutputSuffix(g, "svc.grpc.pb.cc.o") {
		t.Errorf("non-GRPC module unexpectedly compiles a .grpc.pb.cc.o")
	}
}

func protoImportRootFixture() FS {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")

	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"ADDINCL(GLOBAL contrib/libs/protobuf/src)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/message.h", "#pragma once\n")

	writeTestModuleFile(files, "dep/ya.make", "PROTO_LIBRARY()\nSRCS(foo.proto)\nEND()\n")
	writeTestModuleFile(files, "dep/foo.proto", "syntax = \"proto3\";\npackage dep;\nmessage Foo {}\n")

	writeTestModuleFile(files, "mirror/peer/ya.make",
		"PROTO_LIBRARY()\nPROTO_NAMESPACE(mirror)\nSRCS(bar.proto)\nEND()\n")
	writeTestModuleFile(files, "mirror/peer/bar.proto", "syntax = \"proto3\";\npackage mirror;\nmessage Bar {}\n")
	writeTestModuleFile(files, "mirror/dep/foo.proto", "syntax = \"proto3\";\npackage dep;\nmessage Foo {}\n")

	writeTestModuleFile(files, "main/ya.make",
		"PROTO_LIBRARY()\nPEERDIR(dep mirror/peer)\nSRCS(main.proto)\nEND()\n")
	writeTestModuleFile(files, "main/main.proto",
		"syntax = \"proto3\";\npackage main;\nimport \"dep/foo.proto\";\nmessage Main {}\n")

	return newMemFS(files)
}

func TestGen_ProtoImport_SourceRootWinsOverPeerNamespaceMirror(t *testing.T) {
	fs := protoImportRootFixture()
	g := testGen(fs, "main")

	pb := mustNodeByOutput(t, g, "$(B)/main/main.pb.h")
	inputs := vfsStrings(pb.flatInputs())

	const (
		want   = "$(S)/dep/foo.proto"
		mirror = "$(S)/mirror/dep/foo.proto"
	)

	hasWant, hasMirror := false, false

	for _, in := range inputs {
		if in == want {
			hasWant = true
		}

		if in == mirror {
			hasMirror = true
		}
	}

	if hasMirror {
		t.Errorf("main.pb.cc carries the peer-namespace mirror import %q; protoc/upstream bind the import to the source root, not the ADDINCL mirror\ninputs=%v", mirror, inputs)
	}

	if !hasWant {
		t.Errorf("main.pb.cc is missing the source-root import %q\ninputs=%v", want, inputs)
	}
}
