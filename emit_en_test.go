package main

import (
	"slices"
	"testing"
)

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
