package main

import (
	"slices"
	"strings"
	"testing"
)

// cppEnumsSerializationProtoFiles builds a minimal PROTO_LIBRARY that compiles
// package.proto and runs CPP_ENUMS_SERIALIZATION over the generated header(s).
// macroArgs is the literal argument list of CPP_ENUMS_SERIALIZATION(...).
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

// TestGen_CppEnumsSerialization mirrors upstream pybuild.py CPP_ENUMS_SERIALIZATION:
// each header argument expands to GENERATE_ENUM_SERIALIZATION_WITH_HEADER, emitting
// File_serialized.cpp + File_serialized.h (with --header), compiled and archived.
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

// TestGen_CppEnumsSerializationNamespaceControlToken verifies that a NAMESPACE
// pair is consumed as a control token (pybuild.py: `if arg == 'NAMESPACE': next(args)`)
// and emits no serialized output for NAMESPACE or its value — only the real header.
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
	// Reproduces the ads/bsyeti/libs/features divergence: a RUN_PROGRAM OUT
	// emits a header (formula.h) into the build tree with no $(S) counterpart,
	// and GENERATE_ENUM_SERIALIZATION in the same module consumes that generated
	// header. The EN input must resolve to the $(B) producer output and take a
	// dependency on the producing RUN_PROGRAM node — never fall back to a
	// nonexistent $(S) source.
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

	// The header input must resolve to the $(B) producer output, not a
	// nonexistent $(S) source.
	if !nodeHasInput(en, "$(B)/mod/gen.h") {
		t.Fatalf("EN node inputs: want $(B)/mod/gen.h (generated), got: %v", en.flatInputs())
	}
	if nodeHasInput(en, "$(S)/mod/gen.h") {
		t.Fatalf("EN node inputs: got nonexistent source $(S)/mod/gen.h: %v", en.flatInputs())
	}
	if got := en.Cmds[0].CmdArgs.flat()[1].string(); got != "$(B)/mod/gen.h" {
		t.Fatalf("EN cmd_args[1] = %q, want $(B)/mod/gen.h", got)
	}

	// The EN node must depend on the RUN_PROGRAM producer of gen.h (its node
	// lists gen.cpp as Outputs[0], so match on any output).
	genH := mustNodeByAnyOutput(t, g, "$(B)/mod/gen.h")
	if !slices.Contains(graphDeps(g, en), genH.UID) {
		t.Fatalf("EN node deps missing generated-header producer uid %q: %v", genH.UID, graphDeps(g, en))
	}
}

// Archive member order for the ads/bsyeti/libs/features shape: a RUN_PROGRAM
// generates formula.cpp/formula.h, ordinary SRCS follow, then
// GENERATE_ENUM_SERIALIZATION serializes the generated header. ymake re-queues
// each generated output at its declaring statement's processing point (default
// priority 2), so among the generated auxiliary members the archive order is
// the statement declaration order: the RUN_PROGRAM object (formula.cpp.o)
// precedes the serialized-header object (formula.h_serialized.cpp.o), after the
// ordinary direct source. Before the fix every generated member tied at Seq 0
// and the EN members were appended before the PR members, so
// formula.h_serialized.cpp.o wrongly preceded formula.cpp.o.
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

// Same-module serialized-enum sibling propagation (T-135): two
// GENERATE_ENUM_SERIALIZATION_WITH_HEADER declarations in one module where the
// FIRST declared header reaches the SECOND's generated serialized header through
// an ordinary #include chain. Mirrors balancer/kernel/cookie: set_cookie.h
// (declared first) includes cookie_errors.h, which includes the generated
// set_cookie_errors.h_serialized.h (declared second). Both the EN producer of
// the first header and an ordinary CC consumer that includes the first
// serialized header must carry the SECOND header's generated serialized outputs
// (.cpp and .h) as inputs. The previous single-pass emitEnumSrcs registered and
// walked each declaration in turn, so the first declaration's closure walk saw
// the sibling unregistered and cached an empty resolution for the including
// header — poisoning both the EN producer and the later ordinary CC compile.
func TestGen_EnumSerializationSiblingSerializedOutputsPropagate(t *testing.T) {
	files := map[string]string{}

	// a.h (declared FIRST) reaches b's generated serialized header through a
	// plain header, exactly like set_cookie.h -> cookie_errors.h -> ...serialized.h.
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

	// EN producer of the FIRST header carries the SECOND header's serialized outputs.
	enA := mustNodeByOutput(t, g, "$(B)/m/a.h_serialized.cpp")
	if !nodeHasInput(enA, "$(B)/m/b.h_serialized.h") {
		t.Fatalf("EN a.h_serialized.cpp missing sibling $(B)/m/b.h_serialized.h: %v", enA.flatInputs())
	}
	if !nodeHasInput(enA, "$(B)/m/b.h_serialized.cpp") {
		t.Fatalf("EN a.h_serialized.cpp missing sibling $(B)/m/b.h_serialized.cpp: %v", enA.flatInputs())
	}

	// An ordinary CC consumer that includes the first serialized header reaches
	// the sibling generated outputs too.
	use := mustNodeByOutputSuffix(t, g, "use.cpp.o")
	if !nodeHasInput(use, "$(B)/m/b.h_serialized.h") {
		t.Fatalf("CC use.cpp.o missing sibling $(B)/m/b.h_serialized.h: %v", use.flatInputs())
	}
	if !nodeHasInput(use, "$(B)/m/b.h_serialized.cpp") {
		t.Fatalf("CC use.cpp.o missing sibling $(B)/m/b.h_serialized.cpp: %v", use.flatInputs())
	}
}

// Owner/consumer split: an enum-serialization generated C++ output (the EN
// node) and its compile (CC) carry the DECLARING module's module_dir even when
// a DIFFERENT module include-resolves the generated *_serialized.h first. This
// is the converged socdem_type shape (T-49): the serialized-enum EN/CC nodes
// follow ymake's enum-generation module attribution (the macro's owning module
// via the emit-time default), NOT a consumer-claim override like the RUN_PROGRAM
// PR/CF/CP first-claim path. Guards against re-attributing EN nodes to the
// first consumer (which regresses the yabs/server/libs/enums/* family).
func TestGen_EnumSerializationOwnerKeepsModuleDirAcrossConsumer(t *testing.T) {
	files := map[string]string{}

	// own: declares the WITH_HEADER enum serialization for own/mode.h and
	// compiles the generated mode.h_serialized.cpp into its own library.
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

	// cons: a different module that include-resolves the generated serialized
	// header own/mode.h_serialized.h — the consumer, distinct from the owner.
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

	// The EN generation node stays with the owner that declared the macro.
	en := mustNodeByOutput(t, g, "$(B)/own/mode.h_serialized.cpp")
	if got := en.TargetProperties.ModuleDir; got != "own" {
		t.Fatalf("EN serialized-enum module_dir = %q, want %q (declaring owner, not consumer)", got, "own")
	}
	// The compile of the generated source likewise stays with the owner.
	cc := mustNodeByOutputSuffix(t, g, "mode.h_serialized.cpp.o")
	if cc.KV.P != pkCC {
		t.Fatalf("expected CC node compiling mode.h_serialized.cpp, got KV.p %v", cc.KV.P)
	}
	if got := cc.TargetProperties.ModuleDir; got != "own" {
		t.Fatalf("CC serialized-enum compile module_dir = %q, want %q (declaring owner)", got, "own")
	}
}

// Nested-submodule directory-owned header drift (T-52): the parent LIBRARY
// declares the enum serialization, but a NESTED submodule (own ya.make, its dir
// strictly under the parent's) owns a header that #includes the generated
// *_serialized.h. ymake's Node2Module (FindModule on the DFS stack) attributes
// the EN producer to the nearest module on the stack; the nested peerdir
// submodule leaves the generated node before the enclosing parent completes
// (submodule-before-parent post-order), so the EN node drifts to the submodule.
// A second EN whose generated header is reached only through a NON-module subdir
// of the parent keeps the parent — the discriminator that the consumer-claim
// override (T-49) lacked.
func TestGen_EnumSerializationDriftsToNestedSubmoduleDirectoryOwnedHeader(t *testing.T) {
	files := map[string]string{}

	// parent declares both enum serializations and compiles sub/services.cpp
	// (a SRC physically located in the nested submodule's directory).
	writeTestModuleFile(files, "parent/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(parent/sub)
SRCS(sub/services.cpp main2.cpp)
GENERATE_ENUM_SERIALIZATION_WITH_HEADER(degr.h)
GENERATE_ENUM_SERIALIZATION_WITH_HEADER(plain.h)
END()
`)
	writeTestModuleFile(files, "parent/degr.h", "enum class Degr { A = 0 };\n")
	writeTestModuleFile(files, "parent/plain.h", "enum class Plain { A = 0 };\n")
	// main2.cpp pulls the non-module-subdir header that includes plain's serialized.
	writeTestModuleFile(files, "parent/main2.cpp", "#include \"util/u.h\"\nint m2(){return 0;}\n")
	// util/ has NO ya.make: owned by parent, not a nested submodule.
	writeTestModuleFile(files, "parent/util/u.h", "#include <parent/plain.h_serialized.h>\n")

	// nested submodule: its directory owns services.h, which #includes degr's
	// generated serialized header. services.cpp is compiled by the PARENT.
	writeTestModuleFile(files, "parent/sub/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(config.cpp)
END()
`)
	writeTestModuleFile(files, "parent/sub/config.cpp", "int cfg(){return 0;}\n")
	writeTestModuleFile(files, "parent/sub/services.h", "#include <parent/degr.h_serialized.h>\n")
	writeTestModuleFile(files, "parent/sub/services.cpp", "#include \"services.h\"\nint svc(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(parent)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app")

	// degr's serialized header is reached through the nested submodule's
	// directory-owned services.h → drifts to the submodule.
	degr := mustNodeByOutput(t, g, "$(B)/parent/degr.h_serialized.cpp")
	if got := degr.TargetProperties.ModuleDir; got != "parent/sub" {
		t.Fatalf("degr EN module_dir = %q, want %q (nested submodule directory-owned header)", got, "parent/sub")
	}

	// plain's serialized header is reached only through a non-module subdir of
	// parent → stays with the declaring parent.
	plain := mustNodeByOutput(t, g, "$(B)/parent/plain.h_serialized.cpp")
	if got := plain.TargetProperties.ModuleDir; got != "parent" {
		t.Fatalf("plain EN module_dir = %q, want %q (non-module subdir keeps declaring owner)", got, "parent")
	}
}

func TestGen_EnumSerializationWithSRCDIRResolvesHeaderViaSourceDir(t *testing.T) {
	// Reproduces the purecalc_no_pg_wrapper divergence: a module uses INCLUDE()
	// to pull in a .ya.make.inc that contains SRCDIR() + GENERATE_ENUM_SERIALIZATION().
	// The header must be resolved relative to the SRCDIR, not the including module's path.
	files := map[string]string{}

	// shared lib provides the header and the ya.make.inc
	writeTestModuleFile(files, "shared/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(iface.cpp)\nGENERATE_ENUM_SERIALIZATION(iface.h)\nEND()\n")
	writeTestModuleFile(files, "shared/iface.h", "enum class Mode { A = 0, B = 1 };\n")
	writeTestModuleFile(files, "shared/iface.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(files, "shared/ya.make.inc", "SRCDIR(\n    shared\n)\nSRCS(iface.cpp)\nGENERATE_ENUM_SERIALIZATION(iface.h)\n")

	// consumer module includes the ya.make.inc — SRCDIR remaps to "shared"
	writeTestModuleFile(files, "consumer/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nINCLUDE(${ARCADIA_ROOT}/shared/ya.make.inc)\nSRCS(consumer.cpp)\nEND()\n")
	writeTestModuleFile(files, "consumer/consumer.cpp", "int g(){return 0;}\n")

	writeToolProgram(files, "tools/enum_parser/enum_parser", "enum_parser")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "tools/enum_parser/enum_serialization_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "consumer")

	// The EN node output is in the consumer module's path, but the header input
	// must be from the SRCDIR (shared/iface.h), not consumer/iface.h (which doesn't exist).
	en := mustNodeByOutput(t, g, "$(B)/consumer/iface.h_serialized.cpp")

	// The header input must resolve to shared/iface.h via SRCDIR
	if !nodeHasInput(en, "$(S)/shared/iface.h") {
		t.Fatalf("EN node inputs: want $(S)/shared/iface.h (via SRCDIR), got: %v", en.flatInputs())
	}
	// Must NOT use the consumer module path for the header
	if nodeHasInput(en, "$(S)/consumer/iface.h") {
		t.Fatalf("EN node inputs: got wrong path $(S)/consumer/iface.h (SRCDIR not applied): %v", en.flatInputs())
	}
	// The enum_parser cmd arg[1] must be the correct source path
	if got := en.Cmds[0].CmdArgs.flat()[1].string(); got != "$(S)/shared/iface.h" {
		t.Fatalf("EN cmd_args[1] = %q, want $(S)/shared/iface.h", got)
	}
}

func TestGen_EnumSerializationRootedHeaderOutsideModuleDirOutputPath(t *testing.T) {
	// Reproduces market/idx/datacamp/proto/offer/meta: a PROTO_LIBRARY whose
	// SRCDIR points at a sibling source root generates the .pb.h THERE, then
	// GENERATE_ENUM_SERIALIZATION runs over the ${ARCADIA_BUILD_ROOT}-rooted
	// generated header that lies OUTSIDE the module dir. Upstream's
	// ${output;suf=_serialized.cpp:File} places the serialized.cpp at the rooted
	// File's own resolved build location (not doubled under the module BINDIR),
	// and the compile object rebases the cross-dir source under the module BINDIR
	// mapping the `..` ascent to `__` (TCommandInfo::InitDirs).
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

	// EN output lands at the rooted header's own build dir (m/), NOT doubled under
	// the module dir (m/meta/) nor localized under _/.
	mustNodeByOutput(t, g, "$(B)/m/Offer.pb.h_serialized.cpp")
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			s := o.string()
			if strings.Contains(s, "m/meta/m/Offer.pb.h_serialized") || strings.Contains(s, "m/meta/_/m/Offer.pb.h_serialized") {
				t.Fatalf("serialized enum output doubled/localized under module dir: %q", s)
			}
		}
	}

	// The compile object rebases the cross-dir source under the module BINDIR with
	// the `..` ascent mapped to `__`.
	mustNodeByOutput(t, g, "$(B)/m/meta/__/Offer.pb.h_serialized.cpp.o")
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
