package main

import (
	"strings"
	"testing"
)

// TestEmitProtoSrcs_YaffGeneratedHeaderClosureRidesIntoConsumer reproduces the
// sg7 YaFF include-closure gap (representative: ads/argus/libs/common/types.cpp.o).
// A unit includes a generated <proto>.yaff.h. Upstream's YaFF protoc plugin
// writes that header so it #includes the yaff runtime (yaff/struct/protobuf/
// reflect) + the proto's own .pb.h + — for an EXPERIMENTAL proto — the
// experiments builder/column/merge runtime. The generated header must therefore
// be registered with those parsed includes so the whole closure rides into every
// compile that includes it. Before the fix the .yaff.h is an unregistered
// generated output: the include resolves to nothing and none of the closure
// (foo.yaff.h, foo.pb.h, the yaff/experiments source headers) reaches the
// consumer's inputs.
func TestEmitProtoSrcs_YaffGeneratedHeaderClosureRidesIntoConsumer(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/yaff/tools/protoc_plugin", "protoc_plugin")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	// YaFF runtime source headers. yaff.h and experiments/builder.h each pull a
	// transitive sibling (base.h) so the test pins closure propagation, not just
	// the direct includes.
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

// TestEmitProtoSrcs_YaffFilesWhitelistSkipsNonWhitelistedHeaderClosure pins the
// upstream FILES-whitelist gate (proto_plugin.cpp NeedToProcessFile). With
// YAFF(FILES kept.proto) the plugin runs its generators only for kept.proto;
// skipped.yaff.h is opened but written empty, so a unit that includes
// skipped.yaff.h must NOT pull the yaff runtime / .pb.h / experiments closure,
// while a unit that includes kept.yaff.h still does. Without the whitelist gate
// the registration over-collects the runtime closure for every YaFF output.
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
	// skipped.yaff.h itself still resolves (it is a generated output), but it is
	// empty upstream: none of the runtime / .pb.h / experiments closure rides in.
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

// TestEmitProtoSrcs_YaffCppInputClosureInducesWireFormatDropsSiblingHeader pins
// the T-33 divergence. The YaFF protoc plugin writes the generated .yaff.cpp as a
// thin wrapper whose only content is `#include "<stem>.yaff.h"` (by basename),
// produced by the same protoc invocation. Upstream resolves that sibling header
// for its transitive closure but does NOT record the header itself as a compile
// input; the translation unit's protobuf runtime headers — notably wire_format.h
// — arrive via protoc's INDUCED_DEPS(cpp …) on the .yaff.cpp output. So the
// generated .yaff.cpp.o must carry wire_format.h (induced, cpp bucket) and must
// NOT carry the sibling .yaff.h. Before the fix the .yaff.cpp registration passes
// no GeneratorRefs (no induced wire_format.h) and records the sibling header.
func TestEmitProtoSrcs_YaffCppInputClosureInducesWireFormatDropsSiblingHeader(t *testing.T) {
	files := map[string]string{}
	// protoc tool declares the real induced-deps split: wire_format.h is cpp-only
	// (rides .cc/.cpp outputs), message.h is h+cpp (rides headers too).
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
	// The header's transitive closure still rides in (walked through the dropped
	// sibling): the proto's own .pb.h and the yaff runtime headers remain inputs.
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

// TestEmitProtoSrcs_NonWhitelistedYaffCppRidesProtoMainPbHeader pins the T-43
// divergence. The YaFF plugin ALWAYS writes <proto>.yaff.cpp as a thin
// `#include "<proto>.yaff.h"` wrapper, but writes <proto>.yaff.h empty for a proto
// OUTSIDE the YAFF(FILES …) allowlist (NeedToProcessFile false). So an include scan
// of a non-whitelisted .yaff.cpp reaches only the empty sibling header — no .pb.h.
//
// Upstream still records the proto's own .pb.h plus its producer-source bundle
// (.proto, cpp_proto_wrapper.py) on the non-whitelisted .yaff.cpp.o: the protoc
// command floats .pb.h to the front as the MAIN output (${main;…:.pb.h}), and every
// sibling output (incl. .yaff.cpp) rides that main output via EDT_OutTogether
// (TJSONVisitor::PrepareLeaving), transitively expanded. The whitelisted sibling
// gets .pb.h coincidentally through its non-empty .yaff.h #include; the
// non-whitelisted one only through OutTogether.
//
// Before the fix the non-whitelisted skipped.yaff.cpp.o carries neither skipped.pb.h
// nor its .proto/wrapper bundle. The whitelisted kept.yaff.cpp.o (T-33) must stay
// stable: wire_format.h induced in, sibling kept.yaff.h dropped.
func TestEmitProtoSrcs_NonWhitelistedYaffCppRidesProtoMainPbHeader(t *testing.T) {
	files := map[string]string{}
	// protoc tool declares the real induced-deps split: wire_format.h is cpp-only
	// (rides .cc/.cpp outputs), message.h is h+cpp (rides headers too).
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

	// Non-whitelisted: the empty skipped.yaff.h yields no closure, but the OutTogether
	// main output skipped.pb.h still rides — expanded — carrying its .proto and the
	// wrapper producer source. The sibling self-header is dropped (T-33).
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

	// Whitelisted (T-33) stays stable: wire_format.h induced, own .pb.h present,
	// sibling kept.yaff.h dropped.
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

// TestEmitProtoSrcs_YaffOutputOrderFollowsLiteHeaderDeclarationOrder pins the
// upstream CPP_PROTO_OUTS accumulation order. The wrapper's --outputs list (and
// the PB node's outputs) is CPP_PROTO_OUTS in statement order, main .pb.h
// floated to the front. The YAFF() plugin appends .yaff.h/.yaff.cpp; the lite
// header .deps.pb.h is appended by the `when ($PROTOC_TRANSITIVE_HEADERS=="no")`
// block triggered by SET(PROTOC_TRANSITIVE_HEADERS "no"). So the yaff group
// precedes the cpp_out group (.pb.cc + .deps.pb.h) iff YAFF() is declared before
// lite headers are turned on (sg7 representative ads/peafowl), and follows it
// otherwise (sg7 majority, e.g. yabs/server/proto/log). Before the fix both
// orderings emit the cpp_out group first, so the YAFF-before-SET case mismatches.
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

	// YAFF() before SET(PROTOC_TRANSITIVE_HEADERS "no") — yaff group precedes the
	// cpp_out group.
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

	// SET(PROTOC_TRANSITIVE_HEADERS "no") before YAFF() — cpp_out group first.
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

	// The --outputs command list must mirror the node outputs exactly.
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

// TestEmitProtoSrcs_GeneratedProtoInheritsProducerSourceInputs locks the
// transitive source closure: when SRCS(X.proto) is build-generated by a
// RUN_ANTLR (JV) step, the PB protoc node's inputs must include the JV
// producer's $(S) leaf sources (grammar, CONFIGURE_FILE source, antlr.jar,
// and the wrapper scripts), exactly as upstream tracks them — otherwise the
// PB self_uid diverges and the drift cascades to .pb.cc.o and the proto AR.
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

// TestEmitProtoSrcs_AntlrCppOutsCompileIntoProtoArchive locks the second
// jsonpath G2 leg: RUN_ANTLR(... OUT *.cpp ...) inside a PROTO_LIBRARY's
// IF(GEN_PROTO) block — upstream auto-promotes those .cpp outputs to SRCS,
// compiling them and archiving the .o into the same proto-library AR
// alongside .pb.cc.o (see jsonpath ya.make: SRCS lists only the .proto, but
// JsonPathParser.cpp and JsonPathLexer.cpp from the second RUN_ANTLR land
// in libproto_ast-gen-jsonpath.a).
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

	// Member ORDER: the ANTLR-generated .cpp objects are ordinary translation
	// units (regular archive phase) and upstream lists them BEFORE the proto
	// .pb.cc.o (proto-codegen phase). Reference jsonpath AR order is
	// [JsonPathParser.cpp.o, JsonPathLexer.cpp.o, JsonPathParser.pb.cc.o].
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

// TestEmitPyProtoSrc_GeneratedProtoWiresProducerDep is the Python protobuf
// analogue of TestEmitProtoSrcs_GeneratedProtoWiresProducerDep: a PROTO_LIBRARY
// whose SRCS(X.proto) is the OUT of a RUN_ANTLR (no on-disk X.proto), consumed
// by a PY3_LIBRARY. Upstream wires the python protoc (PB) node to the build-tree
// proto, takes the producer dependency, and carries the producer's $(S) leaf
// sources. Before the fix the py PB node listed $(S)/.../JsonPathParser.proto —
// a nonexistent source path that faults sandboxing content-hashing — with no
// producer dep and no producer source inputs.
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
		if n.KV.P == pkPB && n.TargetProperties.ModuleTag == tagPy3Proto &&
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

	// (1) build-tree proto input, not the nonexistent $(S) source.
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

	// (2) producer dependency.
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

	// (3) producer source inputs ride on the py PB node's flat inputs.
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

// TestEmitProtoSrcs_SetAppendProtoFilesNotDoubled reproduces the grut auxiliary
// duplicate-PB-producer abort (T-14). A PROTO_LIBRARY that builds its proto list
// with SET_APPEND(PROTO_FILES …) and feeds it to SRCS(${PROTO_FILES}) is
// collected twice by genModule: a probe pass to learn the module type, then a
// cpp-proto re-collect. When the re-collect env is cloned from the probe-mutated
// env, SET_APPEND re-appends PROTO_FILES (VAR = "$VAR x"), doubling the list, so
// the same proto is scheduled for PB generation twice and CodegenRegistry aborts
// on the duplicate producer. The re-collect must start from a clean module base
// env, so each proto yields exactly one PB producer.
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

// TestEmitProtoSrcs_SrcDirAscentObjectPath reproduces the sg7 market/proto/content
// path-shape gap: a PROTO_LIBRARY whose SRCDIR points at a PARENT directory and
// whose SRCS names a .proto living there. The generated .pb.cc therefore has a
// logical path OUTSIDE the module dir; upstream names the compiled object by
// rebasing that path under the module's build dir, mapping the `..` ascent into a
// `__` segment (market/proto/content/ir/common/__/BusinessCleanWebStatus.pb.cc.o),
// exactly as a SRCDIR-resolved C++ source object is named. The previous build-path
// branch instead emitted `_/<full-source-path>` (…/common/_/market/proto/content/
// ir/BusinessCleanWebStatus.pb.cc.o), an output-only divergence.
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

// TestGen_PyProtoLibrary_TransitivePROTONamespaceReachesPyProtoCmd reproduces the
// sg7 brandformance py3_proto gap: a PY-addressed PROTO_LIBRARY reaching a
// transitive PROTO_LIBRARY that declares a bare (non-GLOBAL) PROTO_NAMESPACE(yt)
// must carry -I=$(S)/yt in its gen_py_protos protoc command, exactly as the C++
// pb.h side does. The contributor chain mirrors the reference:
// ads/autobudget/protos -> grut/libs/proto/public/metadata -> yt/yt_proto/yt/core
// (PROTO_NAMESPACE(yt)). yt and protobuf-src both ride the single ordered
// _PROTO__INCLUDE set in encounter order: the transitive PROTO_NAMESPACE(yt) is
// reached before contrib/libs/protobuf, so the reference orders the namespace
// token *before* the protobuf-src include and before the NEED_GOOGLE_PROTO_PEERDIRS
// protoc-src include, inside the -I block (before --python_out) — identical to the
// C++ pb side (no standalone protobuf-src precedes the band).
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
		if n.KV.P == pkPB && n.TargetProperties.ModuleTag == tagPy3Proto &&
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
	// Encounter order: the transitive yt namespace precedes contrib/libs/protobuf
	// in _PROTO__INCLUDE, so yt's include precedes the band's protobuf-src, which in
	// turn precedes the NEED_GOOGLE_PROTO_PEERDIRS protoc-src — exactly the cpp side.
	if !(ytIdx < protobufSrcIdx && protobufSrcIdx < pyOutIdx) {
		t.Fatalf("expected yt < protobuf-src < python_out: yt=%d protobuf-src=%d python_out=%d args=%v",
			ytIdx, protobufSrcIdx, pyOutIdx, strStrs(args))
	}
	if protocSrcIdx >= 0 && !(protobufSrcIdx < protocSrcIdx) {
		t.Fatalf("expected protobuf-src before protoc-src: protobuf-src=%d protoc-src=%d args=%v", protobufSrcIdx, protocSrcIdx, strStrs(args))
	}

	// The trailing -I=$(B) -I=$(S)/contrib/libs/protobuf/src pair (the structural
	// -I=$ARCADIA_BUILD_ROOT -I=$PROTOBUF_INCLUDE_PATH suffix) must be preserved
	// immediately before --python_out, distinct from the band's protobuf-src above.
	if pyOutIdx < 2 || args[pyOutIdx-1].string() != "-I=$(S)/contrib/libs/protobuf/src" || args[pyOutIdx-2].string() != "-I=$(B)" {
		t.Fatalf("expected trailing -I=$(B) -I=$(S)/contrib/libs/protobuf/src before --python_out: %v", strStrs(args))
	}
}

// TestGen_PyProtoLibrary_ProtobufBuiltinKeepsBandProtobufSrc guards the protobuf
// runtime's own python protos: their PROTO_NAMESPACE IS contrib/libs/protobuf/src,
// so the source-root include -I=$(S)/contrib/libs/protobuf/src appears as the
// structural namespace prefix, AS a band member (the builtin's own GLOBAL FOR proto
// addincl rides its own _PROTO__INCLUDE), and again in the trailing
// -I=$PROTOBUF_INCLUDE_PATH — three copies total. Dropping the standalone
// pre-band protobuf-src (T-75) must not collapse the band copy: EmitPB's
// `if cppOutRoot != ""` arm (mirrored on the py side) re-renders the namespace
// after the structural prefix regardless of it being protobuf-src.
func TestGen_PyProtoLibrary_ProtobufBuiltinKeepsBandProtobufSrc(t *testing.T) {
	const consumer = "app/pytool"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")

	// The protobuf runtime PROTO_LIBRARY: its own PROTO_NAMESPACE is the protobuf
	// src root, and it carries the GLOBAL FOR proto addincl for that same root.
	// NO_OPTIMIZE_PY_PROTOS / NEED_GOOGLE_PROTO_PEERDIRS(no) match the builtin shape
	// (proto.conf:717,857): no protoc-src include is added.
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

	// The trailing -I=$(B) -I=$(S)/contrib/libs/protobuf/src pair sits just before
	// --python_out, unchanged by the T-75 fix.
	pyOutIdx := indexOfArg(args, "--python_out=$(B)/contrib/libs/protobuf/src")
	if pyOutIdx < 2 || args[pyOutIdx-1].string() != protobufSrc || args[pyOutIdx-2].string() != "-I=$(B)" {
		t.Fatalf("expected trailing -I=$(B) %s before --python_out: %v", protobufSrc, strStrs(args))
	}

	// The band copy (the builtin's own GLOBAL FOR proto addincl in _PROTO__INCLUDE)
	// must survive: at least one protobuf-src include sits AFTER the structural bare
	// -I=$(S) prefix and BEFORE the trailing -I=$(B) pair. Removing line 206 without
	// re-rendering the own-namespace band copy would collapse this to prefix+trailing.
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

	// No NEED_GOOGLE_PROTO_PEERDIRS protoc-src for the builtin (it DISABLEs it).
	if protocSrcIdx := indexOfArg(args, "-I=$(S)/contrib/libs/protoc/src"); protocSrcIdx >= 0 {
		t.Fatalf("protobuf builtin must not carry protoc-src include: %v", strStrs(args))
	}
}

// TestGen_ProtoLibrary_TransitiveGlobalNamespaceInterleavesInBothCmds pins T-4:
// a transitive GLOBAL PROTO_NAMESPACE peer (lib/gapis) reached *after* a bare
// PROTO_NAMESPACE peer (lib/yt) must land in the single ordered _PROTO__INCLUDE
// set — once, after the bare namespace (encounter order) — in BOTH the C++ and
// the mirrored Python protoc commands. Upstream (proto.conf) makes bare and GLOBAL
// PROTO_NAMESPACE contribute identically to _PROTO__INCLUDE; our former split
// rendered GLOBAL-before-bare and omitted GLOBAL from the py command entirely.
func TestGen_ProtoLibrary_TransitiveGlobalNamespaceInterleavesInBothCmds(t *testing.T) {
	const consumer = "app/pytool"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/py3cc", "py3cc")
	writeToolProgram(files, "tools/py3cc/slow", "py3cc_slow")
	writeToolProgram(files, "tools/rescompiler", "rescompiler")
	writeToolProgram(files, "contrib/python/mypy-protobuf/bin/protoc-gen-mypy", "protoc-gen-mypy")

	// GLOBAL PROTO_NAMESPACE peer — its namespace rides _PROTO__INCLUDE everywhere.
	writeTestModuleFile(files, "lib/gapis/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(GLOBAL lib/gapis)
SRCS(g.proto)
END()
`)
	writeTestModuleFile(files, "lib/gapis/g.proto", "syntax = \"proto3\";\npackage gapis;\nmessage G {}\n")

	// Bare PROTO_NAMESPACE peer, encountered before the GLOBAL one (it peers it).
	writeTestModuleFile(files, "lib/yt/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(yt)
PEERDIR(lib/gapis)
SRCS(y.proto)
END()
`)
	writeTestModuleFile(files, "lib/yt/y.proto", "syntax = \"proto3\";\npackage yt;\nmessage Y {}\n")

	// Consumer PROTO_LIBRARY with no own namespace: its band is purely the peers'.
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

	// C++ PB command for the consumer's c.pb.h (cpp sibling of the py proto lib).
	var cppArgs []STR
	for _, n := range g.Graph {
		if n.KV.P == pkPB && n.TargetProperties.ModuleTag == tagCppProto &&
			len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), "app/proto/c.pb.h") {
			cppArgs = n.Cmds[0].CmdArgs.flat()
			break
		}
	}
	if cppArgs == nil {
		t.Fatal("no C++ PB node for app/proto/c.pb.h emitted")
	}
	assertInterleavedBand("cpp", cppArgs)

	// Python PB command for the same source.
	assertInterleavedBand("py", pyProtoCmdArgsForOutput(t, g, "c__intpy3___pb2.py"))
}

// TestProtoPythonResourceKey_PYNamespacePreservesNestedSubdir pins the T-39B
// resource-key shape: with PY_NAMESPACE(yt_proto.yt.client) the aux resource key
// for a nested SRC must keep the module-local proto subdirectory under the
// namespace (yt_proto/yt/client/chunk_client/proto/data_statistics_pb2.py), not
// collapse it to filepath.Base (yt_proto/yt/client/data_statistics_pb2.py).
// Root-level SRCs (no subdir) are unaffected.
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

	// Root-level source: no subdirectory to preserve, unchanged.
	gotRoot := protoPythonResourceKey(instance, d, "access_control_service.proto", "_pb2.py")
	const wantRoot = "yt_proto/yt/client/access_control_service_pb2.py"
	if gotRoot != wantRoot {
		t.Errorf("root-level PY_NAMESPACE key = %q, want %q", gotRoot, wantRoot)
	}
}

// pyProtoNamespaceIncludeCounts returns, for the py PB/grpc producer whose first
// output ends with wantSuffix, the total count of -I=$(S)/yt tokens and the
// flat command args. yt modules render -I=$(S)/yt three times: the
// -I=$ARCADIA_ROOT/$PROTO_NAMESPACE output-root arg plus two copies inside
// _PROTO__INCLUDE (the own namespace + the CPP_PROTO self-sibling re-contribution).
func pyProtoCmdArgsForOutput(t *testing.T, g *Graph, wantSuffix string) []STR {
	t.Helper()
	for _, n := range g.Graph {
		if n.KV.P == pkPB && n.TargetProperties.ModuleTag == tagPy3Proto &&
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

	// Order: the two _PROTO__INCLUDE copies sit immediately after the bare
	// -I=$(S) and immediately before the protobuf-src include.
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

// TestGen_PyProtoLibrary_OwnPROTONamespaceDuplicatesNamespaceInclude reproduces
// the T-39B command gap: a PROTO_LIBRARY that declares its own PROTO_NAMESPACE(yt)
// (with PY_NAMESPACE) must render -I=$(S)/yt twice inside _PROTO__INCLUDE — the
// own namespace plus the CPP_PROTO self-sibling's GLOBAL re-contribution — exactly
// as the reference and the C++ duplicateOutputRootInclude path do. The aux
// resource key for the nested SRC must keep its module-local subdirectory.
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

	// End-to-end: the aux/rescompiler resource key preserves the nested subdir.
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

// TestGen_PyProtoLibrary_GrpcRootSourceSharesDuplicateInclude covers the
// yt/orm/api shape: a GRPC root-level source keeps its existing _pb2_grpc.py
// output and its shared protoc producer carries the same duplicated -I=$(S)/yt.
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

	// The grpc python output still exists and shares the PB producer command.
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

// TestEmitProtoSrcs_CppEvlogCarriesEvent2cppInducedDeps reproduces the T-40
// residual: a PROTO_LIBRARY() with CPP_EVLOG() builds its .proto outputs as
// eventlog (upstream _BUILD_PROTO_AS_EVLOG), so tools/event2cpp is one of the
// protoc plugins producing the .pb.h/.pb.cc. event2cpp's INDUCED_DEPS(h+cpp …)
// must therefore reach the generated foo.pb.cc.o input closure — exactly as the
// true .ev path already does. An otherwise identical PROTO_LIBRARY() WITHOUT
// CPP_EVLOG() must NOT gain that induced input.
func TestEmitProtoSrcs_CppEvlogCarriesEvent2cppInducedDeps(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "library/cpp/eventlog/ya.make", "LIBRARY()\nSRCS(eventlog.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/eventlog/eventlog.cpp", "int eventlog(){return 0;}\n")

	// Stub event2cpp tool declaring a single (h+cpp) induced header.
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

	// T-58: CPP_EVLOG must also make event2cpp an ordinary C++ proto plugin on the
	// PB producer command — upstream CPP_PROTO_PLUGIN0(event2cpp tools/event2cpp …).
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

// T-54: a PROTO_LIBRARY leaf reached only through a peers list that was included
// via a variable-bearing INCLUDE path. Before parse-time expansion the peers
// list was skipped (its ${VAR} stayed literal), FEATURE_PEERDIRS expanded to
// nothing, and the leaf PY3 proto cluster never entered the graph. This mirrors
// the sg7 fs_codegen reachability class without real yabs files.
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
		if n.KV.P == pkPB && n.TargetProperties.ModuleTag == tagPy3Proto &&
			len(n.Outputs) > 0 && strings.HasSuffix(n.Outputs[0].string(), "enum_options__intpy3___pb2.py") {
			pyPB = n
			break
		}
	}
	if pyPB == nil {
		t.Fatal("leaf PY3 proto enum_options__intpy3___pb2.py not reachable through variable-bearing include")
	}
}

// T-32: a py-addressed PROTO_LIBRARY(name) with an explicit module name carries
// that name into its py-proto global archive basename (libpy3<name>.global.a),
// exactly as the C++ archive (emit_proto.go) and the objcopy global (gen.go)
// already do from $MODULE_PREFIX$REALPRJNAME. An unnamed PROTO_LIBRARY() keeps
// the path-derived form (libpy3<dir-tail>.global.a). The former py-proto path
// hardcoded the explicit-name arg to "", always emitting the path-derived name.
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
		if n.KV.P == pkAR && n.TargetProperties.ModuleTag == tagPy3ProtoGlobal && len(n.Outputs) > 0 {
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
