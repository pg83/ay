package main

import (
	"slices"
	"strings"
	"testing"
)

func cppEnumsSerializationProtoFiles(macroArgs string) map[string]string {
	files := map[string]string{}
	writeTestModuleFile(files, "pe/proto/ya.make", "PROTO_LIBRARY()\nSRCS(package.proto)\nIF (CPP_PROTO)\nCPP_ENUMS_SERIALIZATION("+macroArgs+")\nENDIF()\nEND()\n")
	writeTestModuleFile(files, "pe/proto/package.proto", "syntax = \"proto3\";\npackage test;\nenum Mode {\n  MODE_UNSPECIFIED = 0;\n}\nmessage Package { Mode mode = 1; }\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	return files
}

func TestGen_CppEnumsSerialization(t *testing.T) {
	g := testGen(newMemFS(cppEnumsSerializationProtoFiles("package.pb.h")), "pe/proto")

	en := mustNodeByOutput(t, g, "$(B)/pe/proto/package.pb.h_serialized.cpp")
	mustNodeByAnyOutput(t, g, "$(B)/pe/proto/package.pb.h_serialized.h")

	if indexOfArg(en.Cmds[0].CmdArgs.flat(), "--header") < 0 {
		t.Fatalf("EN command missing --header: %#v", en.Cmds[0].CmdArgs.flat())
	}

	mustNodeByOutputSuffix(t, g, "package.pb.h_serialized.cpp.o")

	ar := mustNodeByOutput(t, g, "$(B)/pe/proto/libpe-proto.a")

	if !containsString(strStrs(ar.Cmds[0].CmdArgs.flat()), "$(B)/pe/proto/package.pb.h_serialized.cpp.o") {
		t.Fatalf("archive missing serialized-enum object; cmd_args=%#v", strStrs(ar.Cmds[0].CmdArgs.flat()))
	}
}

func TestGen_CppEnumsSerializationNamespaceControlToken(t *testing.T) {
	g := testGen(newMemFS(cppEnumsSerializationProtoFiles("NAMESPACE something package.pb.h")), "pe/proto")

	mustNodeByOutput(t, g, "$(B)/pe/proto/package.pb.h_serialized.cpp")

	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			s := o.string()

			if strings.Contains(s, "NAMESPACE_serialized") || strings.Contains(s, "something_serialized") {
				t.Fatalf("bogus serialized output from NAMESPACE control token: %q", s)
			}
		}
	}
}

func TestGen_EnumSerializationConsumesRunProgramGeneratedHeader(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/genhdr", "genhdr")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/genhdr --header gen.h
    IN gen.in
    OUT gen.cpp gen.h
)
SRCS(real.cpp)
GENERATE_ENUM_SERIALIZATION(gen.h)
END()
`)
	writeTestModuleFile(files, "mod/gen.in", "enum class E { A = 0 };\n")
	writeTestModuleFile(files, "mod/real.cpp", "int real(){return 0;}\n")

	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "mod")

	en := mustNodeByOutput(t, g, "$(B)/mod/gen.h_serialized.cpp")

	if !nodeHasInput(en, "$(B)/mod/gen.h") {
		t.Fatalf("EN node inputs: want $(B)/mod/gen.h (generated), got: %v", en.flatInputs())
	}

	if nodeHasInput(en, "$(S)/mod/gen.h") {
		t.Fatalf("EN node inputs: got nonexistent source $(S)/mod/gen.h: %v", en.flatInputs())
	}

	if got := en.Cmds[0].CmdArgs.flat()[1].string(); got != "$(B)/mod/gen.h" {
		t.Fatalf("EN cmd_args[1] = %q, want $(B)/mod/gen.h", got)
	}

	genH := mustNodeByAnyOutput(t, g, "$(B)/mod/gen.h")

	if !slices.Contains(graphDeps(g, en), genH.Ref) {
		t.Fatalf("EN node deps missing generated-header producer ref %d: %v", genH.Ref, graphDeps(g, en))
	}
}

func TestGen_RunProgramEnumSerializationArchiveMemberOrder(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/formula_gen", "formula_gen")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PROGRAM(
    tools/formula_gen --formula formula.in --cpp formula.cpp --header formula.h
    IN formula.in
    OUT formula.cpp formula.h
)
SRCS(plain.cpp)
GENERATE_ENUM_SERIALIZATION(formula.h)
END()
`)
	writeTestModuleFile(files, "mod/formula.in", "enum class E { A = 0 };\n")
	writeTestModuleFile(files, "mod/plain.cpp", "int plain(){return 0;}\n")

	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "mod")

	ar := mustNodeByOutput(t, g, "$(B)/mod/libmod.a")

	idxOf := func(rel string) int {
		want := "$(B)/mod/" + rel

		for i, in := range ar.flatInputs() {
			if in.string() == want {
				return i
			}
		}

		t.Fatalf("libmod.a missing member %q: %v", want, vfsStrings(ar.flatInputs()))

		return -1
	}

	plain := idxOf("plain.cpp.o")
	formula := idxOf("formula.cpp.o")
	serialized := idxOf("formula.h_serialized.cpp.o")

	if !(plain < formula && formula < serialized) {
		t.Fatalf("archive order plain.cpp.o(%d) < formula.cpp.o(%d) < formula.h_serialized.cpp.o(%d) violated: %v",
			plain, formula, serialized, vfsStrings(ar.flatInputs()))
	}
}

func TestGen_EnumSerializationSiblingSerializedOutputsPropagate(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "m/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
GENERATE_ENUM_SERIALIZATION_WITH_HEADER(a.h)
GENERATE_ENUM_SERIALIZATION_WITH_HEADER(b.h)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "m/a.h", "#pragma once\n#include \"aux.h\"\nenum class A { X = 0 };\n")
	writeTestModuleFile(files, "m/aux.h", "#pragma once\n#include <m/b.h_serialized.h>\n")
	writeTestModuleFile(files, "m/b.h", "#pragma once\nenum class B { Y = 0 };\n")
	writeTestModuleFile(files, "m/use.cpp", "#include <m/a.h_serialized.h>\nint use(){return 0;}\n")

	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "m")

	enA := mustNodeByOutput(t, g, "$(B)/m/a.h_serialized.cpp")

	if !nodeHasInput(enA, "$(B)/m/b.h_serialized.h") {
		t.Fatalf("EN a.h_serialized.cpp missing sibling $(B)/m/b.h_serialized.h: %v", enA.flatInputs())
	}

	if !nodeHasInput(enA, "$(B)/m/b.h_serialized.cpp") {
		t.Fatalf("EN a.h_serialized.cpp missing sibling $(B)/m/b.h_serialized.cpp: %v", enA.flatInputs())
	}

	use := mustNodeByOutputSuffix(t, g, "use.cpp.o")

	if !nodeHasInput(use, "$(B)/m/b.h_serialized.h") {
		t.Fatalf("CC use.cpp.o missing sibling $(B)/m/b.h_serialized.h: %v", use.flatInputs())
	}

	if !nodeHasInput(use, "$(B)/m/b.h_serialized.cpp") {
		t.Fatalf("CC use.cpp.o missing sibling $(B)/m/b.h_serialized.cpp: %v", use.flatInputs())
	}
}

func TestGen_ProtoGeneratedHeaderEnumSerializationArchivesAfterProto(t *testing.T) {
	const modPath = "mod/pg"

	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	writeTestModuleFile(files, modPath+"/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(data.proto)
GENERATE_ENUM_SERIALIZATION(data.pb.h)
END()
`)
	writeTestModuleFile(files, modPath+"/data.proto", "syntax = \"proto3\";\npackage test;\nenum E { E0 = 0; }\nmessage M { E e = 1; }\n")

	g := testGen(newMemFS(files), modPath)

	ar := mustNodeByOutput(t, g, "$(B)/"+modPath+"/"+archiveNameWithPrefixOrName(modPath, "lib", ""))

	idxOf := func(rel string) int {
		want := "$(B)/" + modPath + "/" + rel

		for i, in := range ar.flatInputs() {
			if in.string() == want {
				return i
			}
		}

		t.Fatalf("archive missing member %q: %v", want, vfsStrings(ar.flatInputs()))

		return -1
	}

	proto := idxOf("data.pb.cc.o")
	serialized := idxOf("data.pb.h_serialized.cpp.o")

	if !(proto < serialized) {
		t.Fatalf("archive order data.pb.cc.o(%d) < data.pb.h_serialized.cpp.o(%d) violated: %v",
			proto, serialized, vfsStrings(ar.flatInputs()))
	}
}

func TestGen_EnumSerializationOwnerKeepsModuleDirAcrossConsumer(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "own/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
GENERATE_ENUM_SERIALIZATION_WITH_HEADER(mode.h)
SRCS(stub.cpp)
END()
`)
	writeTestModuleFile(files, "own/mode.h", "enum class Mode { A = 0, B = 1 };\n")
	writeTestModuleFile(files, "own/stub.cpp", "int stub(){return 0;}\n")

	writeTestModuleFile(files, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(own)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "cons/use.cpp", "#include <own/mode.h_serialized.h>\nint use(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cons)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app")

	mustNodeByOutput(t, g, "$(B)/own/mode.h_serialized.cpp")

	cc := mustNodeByOutputSuffix(t, g, "mode.h_serialized.cpp.o")

	if cc.KV.P != pkCC {
		t.Fatalf("expected CC node compiling mode.h_serialized.cpp, got KV.p %v", cc.KV.P)
	}
}

func TestGen_EnumSerializationWithSRCDIRResolvesHeaderViaSourceDir(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "shared/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(iface.cpp)\nGENERATE_ENUM_SERIALIZATION(iface.h)\nEND()\n")
	writeTestModuleFile(files, "shared/iface.h", "enum class Mode { A = 0, B = 1 };\n")
	writeTestModuleFile(files, "shared/iface.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(files, "shared/ya.make.inc", "SRCDIR(\n    shared\n)\nSRCS(iface.cpp)\nGENERATE_ENUM_SERIALIZATION(iface.h)\n")

	writeTestModuleFile(files, "consumer/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nINCLUDE(${ARCADIA_ROOT}/shared/ya.make.inc)\nSRCS(consumer.cpp)\nEND()\n")
	writeTestModuleFile(files, "consumer/consumer.cpp", "int g(){return 0;}\n")

	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "consumer")

	en := mustNodeByOutput(t, g, "$(B)/consumer/iface.h_serialized.cpp")

	if !nodeHasInput(en, "$(S)/shared/iface.h") {
		t.Fatalf("EN node inputs: want $(S)/shared/iface.h (via SRCDIR), got: %v", en.flatInputs())
	}

	if nodeHasInput(en, "$(S)/consumer/iface.h") {
		t.Fatalf("EN node inputs: got wrong path $(S)/consumer/iface.h (SRCDIR not applied): %v", en.flatInputs())
	}

	if got := en.Cmds[0].CmdArgs.flat()[1].string(); got != "$(S)/shared/iface.h" {
		t.Fatalf("EN cmd_args[1] = %q, want $(S)/shared/iface.h", got)
	}
}

func TestGen_EnumSerializationRootedHeaderOutsideModuleDirOutputPath(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "m/meta/ya.make", `PROTO_LIBRARY()
SRCDIR(m)
SRCS(Offer.proto)
IF (CPP_PROTO)
GENERATE_ENUM_SERIALIZATION(${ARCADIA_BUILD_ROOT}/m/Offer.pb.h)
ENDIF()
END()
`)
	writeTestModuleFile(files, "m/Offer.proto", "syntax = \"proto3\";\npackage test;\nenum Mode {\n  MODE_UNSPECIFIED = 0;\n}\nmessage Offer { Mode mode = 1; }\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "m/meta")

	mustNodeByOutput(t, g, "$(B)/m/Offer.pb.h_serialized.cpp")

	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			s := o.string()

			if strings.Contains(s, "m/meta/m/Offer.pb.h_serialized") || strings.Contains(s, "m/meta/_/m/Offer.pb.h_serialized") {
				t.Fatalf("serialized enum output doubled/localized under module dir: %q", s)
			}
		}
	}

	mustNodeByOutput(t, g, "$(B)/m/meta/__/Offer.pb.h_serialized.cpp.o")

	ar := mustNodeByOutput(t, g, "$(B)/m/meta/"+archiveNameWithPrefixOrName("m/meta", "lib", ""))
	idxOf := func(rel string) int {
		for i, in := range ar.flatInputs() {
			if in.string() == rel {
				return i
			}
		}

		t.Fatalf("archive missing member %q: %v", rel, vfsStrings(ar.flatInputs()))

		return -1
	}
	proto := idxOf("$(B)/m/meta/__/Offer.pb.cc.o")
	serialized := idxOf("$(B)/m/meta/__/Offer.pb.h_serialized.cpp.o")

	if !(proto < serialized) {
		t.Fatalf("archive order Offer.pb.cc.o(%d) < Offer.pb.h_serialized.cpp.o(%d) violated: %v",
			proto, serialized, vfsStrings(ar.flatInputs()))
	}
}

func TestGen_EnumSerializationRootQualifiedHeaderUsesCanonicalInput(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "pkg/sub/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
GENERATE_ENUM_SERIALIZATION(pkg/sub/codecs.h)
SRCS(stub.cpp)
END()
`)
	writeTestModuleFile(files, "pkg/sub/codecs.h", "enum class E { A = 0 };\n")
	writeTestModuleFile(files, "pkg/sub/stub.cpp", "int stub(){return 0;}\n")
	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "pkg/sub")

	en := mustNodeByOutput(t, g, "$(B)/pkg/sub/pkg/sub/codecs.h_serialized.cpp")

	if !nodeHasInput(en, "$(S)/pkg/sub/codecs.h") {
		t.Fatalf("enum inputs missing canonical header path: %#v", en.flatInputs())
	}

	if nodeHasInput(en, "$(S)/pkg/sub/pkg/sub/codecs.h") {
		t.Fatalf("enum inputs still carry duplicated header path: %#v", en.flatInputs())
	}

	if got := en.Cmds[0].CmdArgs.flat()[1].string(); got != "$(S)/pkg/sub/codecs.h" {
		t.Fatalf("enum parser input = %q, want $(S)/pkg/sub/codecs.h", got)
	}

	if idx := indexOfArg(en.Cmds[0].CmdArgs.flat(), "--include-path"); idx < 0 || idx+1 >= len(en.Cmds[0].CmdArgs.flat()) || en.Cmds[0].CmdArgs.flat()[idx+1].string() != "pkg/sub/codecs.h" {
		t.Fatalf("enum --include-path mismatch: %#v", en.Cmds[0].CmdArgs.flat())
	}
}
