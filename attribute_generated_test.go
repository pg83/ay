package main

import "testing"

// TestGen_RunPython3OutHeaderAttributedToConsumer reproduces the T-119 PY
// residual (restrictions_json.h): a RUN_PYTHON3 OUT header produced in one
// module but first #included by a CC unit in a PEERDIR consumer must be
// attributed (target_properties.module_dir) to that consumer, mirroring
// upstream Node2Module first-claim-wins. Before the predicate widening the PY
// producer kept its own module dir.
func TestGen_RunPython3OutHeaderAttributedToConsumer(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "gen/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
RUN_PYTHON3(
    gen.py emit
    OUT gen.h
)
END()
`)
	writeTestModuleFile(files, "gen/gen.py", "print('// generated')\n")

	writeTestModuleFile(files, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(gen)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "cons/use.cpp", "#include \"gen/gen.h\"\nint use(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cons)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app")

	py := findGraphNodeByOutputs(t, g, "$(B)/gen/gen.h")
	if py.KV.P != pkPY {
		t.Fatalf("kv.p = %q, want PY", py.KV.P)
	}
	if got := py.TargetProperties.ModuleDir; got != "cons" {
		t.Fatalf("PY producer module_dir = %q, want consumer %q", got, "cons")
	}
}

// TestGen_ScHeaderAttributedToConsumer reproduces the T-119 SC residual
// (options.sc.h): a SRCS(*.sc) generated .sc.h first #included by a CC unit in a
// PEERDIR consumer is attributed to that consumer. Before the predicate widening
// the SC producer kept its own module dir.
func TestGen_ScHeaderAttributedToConsumer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/domschemec", "domschemec")
	writeTestModuleFile(files, "library/cpp/domscheme/ya.make", "LIBRARY()\nSRCS(domscheme.cpp)\nEND()\n")
	files["library/cpp/domscheme/domscheme.cpp"] = "int domscheme() { return 0; }\n"
	files["library/cpp/domscheme/runtime.h"] = "#pragma once\n"

	writeTestModuleFile(files, "scheme/ya.make", `LIBRARY()
SRCS(options.sc)
END()
`)
	files["scheme/options.sc"] = "struct TOptions { ui32 X; };\n"

	writeTestModuleFile(files, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(scheme)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "cons/use.cpp", "#include \"scheme/options.sc.h\"\nint use(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cons)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app")

	sc := findGraphNodeByOutputs(t, g, "$(B)/scheme/options.sc.h")
	if sc.KV.P != pkSC {
		t.Fatalf("kv.p = %q, want SC", sc.KV.P)
	}
	if got := sc.TargetProperties.ModuleDir; got != "cons" {
		t.Fatalf("SC producer module_dir = %q, want consumer %q", got, "cons")
	}
}

// TestGen_ArchiveHeaderAttributedToConsumer reproduces the T-119 non-library AR
// residual (static.h): an ARCHIVE(NAME header ...) generated header product
// first #included by a CC unit in a PEERDIR consumer is attributed to that
// consumer. The producer's own module compiles nothing that includes the header.
func TestGen_ArchiveHeaderAttributedToConsumer(t *testing.T) {
	files := map[string]string{}

	writeToolProgram(files, "tools/archiver", "archiver")

	writeTestModuleFile(files, "cfg/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
ARCHIVE(
    NAME static.h
    payload.lst
)
END()
`)
	writeTestModuleFile(files, "cfg/payload.lst", "row\n")

	writeTestModuleFile(files, "cons/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cfg)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "cons/use.cpp", "#include \"cfg/static.h\"\nint use(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(cons)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app")

	ar := findGraphNodeByOutputs(t, g, "$(B)/cfg/static.h")
	if ar.KV.P != pkAR {
		t.Fatalf("kv.p = %q, want AR", ar.KV.P)
	}
	if got := ar.TargetProperties.ModuleDir; got != "cons" {
		t.Fatalf("ARCHIVE header producer module_dir = %q, want consumer %q", got, "cons")
	}
}

// TestGen_LibraryArchiveKeepsProducerOwnership guards that an ordinary library
// archive (lib*.a, module_type=lib/module_lang=cpp) is NOT re-attributed: it has
// no #include-resolvable header, and the predicate must exclude it on
// module_type/module_lang/suffix. Its producer module ownership and library
// module properties stay intact even when consumed via PEERDIR.
func TestGen_LibraryArchiveKeepsProducerOwnership(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "lib/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(a.cpp)
END()
`)
	writeTestModuleFile(files, "lib/a.cpp", "int a(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", `PROGRAM()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(lib)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGenDumpGraph(newMemFS(files), "app")

	ar := findGraphNodeByOutputs(t, g, "$(B)/lib/liblib.a")
	if ar.KV.P != pkAR {
		t.Fatalf("kv.p = %q, want AR", ar.KV.P)
	}
	if got := ar.TargetProperties.ModuleDir; got != "lib" {
		t.Fatalf("library archive module_dir = %q, want producer %q", got, "lib")
	}
	if ar.TargetProperties.ModuleType != mtLib {
		t.Fatalf("library archive module_type = %v, want lib", ar.TargetProperties.ModuleType)
	}
	if ar.TargetProperties.ModuleLang != mlCPP {
		t.Fatalf("library archive module_lang = %v, want cpp", ar.TargetProperties.ModuleLang)
	}
}

// TestOverrideGeneratedModuleDir_CppProtoConsumerTagPropagation pins the T-99
// rule: a generated header produced OUTSIDE a PROTO_LIBRARY directory (here a
// RUN_PROGRAM/PR producer under a plain LIBRARY, the apphost cow well-known
// generator) but first-claimed by a consuming CPP_PROTO module is re-attributed
// to that consumer with BOTH its module_dir AND its module_tag — upstream
// Node2Module inherits dir+tag from the owning module. The pre-T-99 pass set
// module_dir only, leaving module_tag unset (the reproduced divergence on the
// well_known *.cow.pb.h nodes).
func TestOverrideGeneratedModuleDir_CppProtoConsumerTagPropagation(t *testing.T) {
	const producerDir = "apphost/gp/lib/proto/cow/generator/well_known"
	const consumerDir = "apphost/lib/proto_answers"

	out := build(producerDir + "/google/protobuf/any.cow.pb.h")

	node := &Node{
		KV:               KV{P: pkPR},
		Outputs:          []VFS{out},
		TargetProperties: TargetProperties{ModuleDir: producerDir},
	}

	e := &BufferedEmitter{
		nodes: []*Node{node},
		generatedFirstClaim: map[VFS]GenOwner{
			out: {Dir: consumerDir, Tag: tagCppProto},
		},
	}

	overrideGeneratedModuleDir(e)

	if got := node.TargetProperties.ModuleDir; got != consumerDir {
		t.Fatalf("module_dir: got %q, want consumer %q", got, consumerDir)
	}

	if got := node.TargetProperties.ModuleTag; got != tagCppProto {
		t.Fatalf("module_tag: got %v, want cpp_proto (%v)", got, tagCppProto)
	}
}

// TestOverrideGeneratedModuleDir_UntaggedConsumerLeavesTagUnset guards the
// common case: a first-claim from a consumer with no module_tag re-attributes
// the dir but must NOT invent a tag.
func TestOverrideGeneratedModuleDir_UntaggedConsumerLeavesTagUnset(t *testing.T) {
	const producerDir = "contrib/tools/gen/producer"
	const consumerDir = "lib/plain_consumer"

	out := build(producerDir + "/gen_table.inc")

	node := &Node{
		KV:               KV{P: pkPR},
		Outputs:          []VFS{out},
		TargetProperties: TargetProperties{ModuleDir: producerDir},
	}

	e := &BufferedEmitter{
		nodes: []*Node{node},
		generatedFirstClaim: map[VFS]GenOwner{
			out: {Dir: consumerDir},
		},
	}

	overrideGeneratedModuleDir(e)

	if got := node.TargetProperties.ModuleDir; got != consumerDir {
		t.Fatalf("module_dir: got %q, want consumer %q", got, consumerDir)
	}

	if got := node.TargetProperties.ModuleTag; got != 0 {
		t.Fatalf("module_tag: got %v, want unset (0)", got)
	}
}
