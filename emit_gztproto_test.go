package main

import "testing"

// TestGen_GztProto_InLibraryArchivesPbObject covers a plain LIBRARY() mixing a
// hand-written .cpp and a .gztproto: the .gztproto must emit its GZ producer, run
// protoc to <base>.pb.{cc,h}, compile, and archive alongside the .cpp.o. Pre-fix
// the regular-module SRCS loop skips .gztproto entirely.
func TestGen_GztProto_InLibraryArchivesPbObject(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "lib/syn/ya.make", `LIBRARY(syn)
SRCS(
    syn.cpp
    model.gztproto
)
END()
`)
	writeTestModuleFile(files, "lib/syn/syn.cpp", "int syn(){return 0;}\n")
	writeTestModuleFile(files, "lib/syn/syn.h", "#pragma once\n")
	writeTestModuleFile(files, "lib/syn/model.gztproto", "package NGzt;\nmessage TModel { optional uint32 X = 1; }\n")

	writeTestModuleFile(files, "dict/gazetteer/converter/ya.make", `PROGRAM(gztconverter)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
INDUCED_DEPS(proto ${ARCADIA_ROOT}/kernel/gazetteer/proto/base.proto)
INDUCED_DEPS(h+cpp ${ARCADIA_BUILD_ROOT}/kernel/gazetteer/proto/base.pb.h)
END()
`)
	writeTestModuleFile(files, "dict/gazetteer/converter/main.cpp", "int main(){return 0;}\n")
	writeTestModuleFile(files, "kernel/gazetteer/proto/ya.make", "PROTO_LIBRARY()\nSRCS(base.proto)\nEND()\n")
	writeTestModuleFile(files, "kernel/gazetteer/proto/base.proto", "syntax = \"proto2\";\npackage NGztBase;\nmessage TBase {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "lib/syn")

	// 1. GZ producer writes the generated .proto.
	gz := mustNodeByOutput(t, g, "$(B)/lib/syn/model.proto")
	if gz.KV.P != pkGZ || gz.KV.PC != pcYellow {
		t.Fatalf("GZ producer KV = {%v,%v}, want {GZ,yellow}", gz.KV.P, gz.KV.PC)
	}

	// 2. The generated .proto runs through the protoc path.
	pb := mustNodeByOutput(t, g, "$(B)/lib/syn/model.pb.h")
	pbHasCC := false
	for _, o := range pb.Outputs {
		if o.string() == "$(B)/lib/syn/model.pb.cc" {
			pbHasCC = true
		}
	}
	if !pbHasCC {
		t.Fatalf("PB node missing model.pb.cc output: %v", pb.Outputs)
	}

	// 3. The generated .pb.cc compiles.
	obj := mustNodeByOutput(t, g, "$(B)/lib/syn/model.pb.cc.o")
	if !nodeHasInput(obj, "$(B)/lib/syn/model.pb.cc") {
		t.Fatalf("object missing generated .pb.cc input: %v", obj.flatInputs())
	}

	// 4. The .pb.cc.o is archived next to syn.cpp.o.
	ar := mustNodeByOutput(t, g, "$(B)/lib/syn/libsyn.a")
	if !nodeHasInput(ar, "$(B)/lib/syn/model.pb.cc.o") {
		t.Fatalf("library archive missing model.pb.cc.o member: %v", ar.flatInputs())
	}
	if !nodeHasInput(ar, "$(B)/lib/syn/syn.cpp.o") {
		t.Fatalf("library archive missing syn.cpp.o member: %v", ar.flatInputs())
	}
}

// TestGen_GztProto_ConsumerSeesGeneratedProtoNestedClosure covers a source
// `.proto` PB node that imports a GZT-generated `.proto`: it must see that
// proto's parsed nested imports plus the raw `.gztproto` producer-source leaves.
// Pre-fix the parse is registered only SOURCE-rooted, so a consumer resolving the
// BUILD-rooted path walks no children and the .gztproto leaves never ride.
func TestGen_GztProto_ConsumerSeesGeneratedProtoNestedClosure(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "gzt/consumer/ya.make", `PROTO_LIBRARY()
PEERDIR(gzt/model)
SRCS(consumer.proto)
END()
`)
	writeTestModuleFile(files, "gzt/consumer/consumer.proto", `syntax = "proto2";
import "gzt/model/model.proto";

package NGzt;

message TConsumer {
    optional NGzt.TModel Model = 1;
}
`)

	writeTestModuleFile(files, "gzt/model/ya.make", `PROTO_LIBRARY()
PEERDIR(
    gzt/peer
    gzt/data
)
SRCS(model.gztproto)
END()
`)
	writeTestModuleFile(files, "gzt/model/model.gztproto", `import "gzt/peer/peer.gztproto";
import "gzt/data/data.proto";

package NGzt;

message TModel {
    optional NGzt.TPeer Peer = 1;
    optional NGzt.TData Data = 2;
}
`)
	writeTestModuleFile(files, "gzt/peer/ya.make", "PROTO_LIBRARY()\nSRCS(peer.gztproto)\nEND()\n")
	writeTestModuleFile(files, "gzt/peer/peer.gztproto", "package NGzt;\nmessage TPeer { optional uint32 X = 1; }\n")
	writeTestModuleFile(files, "gzt/data/ya.make", "PROTO_LIBRARY()\nSRCS(data.proto)\nEND()\n")
	writeTestModuleFile(files, "gzt/data/data.proto", "syntax = \"proto2\";\npackage NGzt;\nmessage TData {}\n")

	writeTestModuleFile(files, "dict/gazetteer/converter/ya.make", `PROGRAM(gztconverter)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
INDUCED_DEPS(proto ${ARCADIA_ROOT}/kernel/gazetteer/proto/base.proto)
INDUCED_DEPS(h+cpp ${ARCADIA_BUILD_ROOT}/kernel/gazetteer/proto/base.pb.h)
END()
`)
	writeTestModuleFile(files, "dict/gazetteer/converter/main.cpp", "int main(){return 0;}\n")
	writeTestModuleFile(files, "kernel/gazetteer/proto/ya.make", "PROTO_LIBRARY()\nSRCS(base.proto)\nEND()\n")
	writeTestModuleFile(files, "kernel/gazetteer/proto/base.proto", "syntax = \"proto2\";\npackage NGztBase;\nmessage TBase {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "gzt/consumer")

	pb := mustNodeByOutput(t, g, "$(B)/gzt/consumer/consumer.pb.h")

	for _, want := range []string{
		"$(B)/gzt/model/model.proto",             // direct generated import
		"$(B)/gzt/peer/peer.proto",               // nested rewritten
		"$(S)/gzt/model/model.gztproto",          // producer-source leaf
		"$(S)/gzt/peer/peer.gztproto",            // transitive leaf
		"$(S)/gzt/data/data.proto",               // nested .proto
		"$(S)/kernel/gazetteer/proto/base.proto", // INDUCED_DEPS(proto …)
	} {
		if !nodeHasInput(pb, want) {
			t.Errorf("consumer PB node missing transitive input %q\ngot: %v", want, pb.flatInputs())
		}
	}

	// The generated peer .proto is reached as a .proto import; its .gztproto must
	// not be invented as a .gztproto.pb.h.
	if nodeHasInput(pb, "$(B)/gzt/peer/peer.gztproto.pb.h") {
		t.Errorf("must NOT invent peer.gztproto.pb.h: %v", pb.flatInputs())
	}
}

// TestGen_GztProto_ArchivedAfterDirectSourcesRegardlessOfSRCSOrder pins the
// codegen-ordering invariant: a `.gztproto`'s generated `.pb.cc.o` archives AFTER
// the module's direct compiles regardless of SRCS position. Pre-fix, with
// `.gztproto` declared FIRST, it rode the non-codegen pass-2 loop and archived in
// declaration order — BEFORE syn.cpp.o. The fix marks the object Generated, which
// sortKey orders last.
func TestGen_GztProto_ArchivedAfterDirectSourcesRegardlessOfSRCSOrder(t *testing.T) {
	files := map[string]string{}

	// .gztproto declared FIRST, hand-written .cpp SECOND.
	writeTestModuleFile(files, "lib/syn/ya.make", `LIBRARY(syn)
SRCS(
    model.gztproto
    syn.cpp
)
END()
`)
	writeTestModuleFile(files, "lib/syn/syn.cpp", "int syn(){return 0;}\n")
	writeTestModuleFile(files, "lib/syn/syn.h", "#pragma once\n")
	writeTestModuleFile(files, "lib/syn/model.gztproto", "package NGzt;\nmessage TModel { optional uint32 X = 1; }\n")

	writeTestModuleFile(files, "dict/gazetteer/converter/ya.make", `PROGRAM(gztconverter)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
INDUCED_DEPS(proto ${ARCADIA_ROOT}/kernel/gazetteer/proto/base.proto)
INDUCED_DEPS(h+cpp ${ARCADIA_BUILD_ROOT}/kernel/gazetteer/proto/base.pb.h)
END()
`)
	writeTestModuleFile(files, "dict/gazetteer/converter/main.cpp", "int main(){return 0;}\n")
	writeTestModuleFile(files, "kernel/gazetteer/proto/ya.make", "PROTO_LIBRARY()\nSRCS(base.proto)\nEND()\n")
	writeTestModuleFile(files, "kernel/gazetteer/proto/base.proto", "syntax = \"proto2\";\npackage NGztBase;\nmessage TBase {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "lib/syn")

	ar := mustNodeByOutput(t, g, "$(B)/lib/syn/libsyn.a")
	members := ar.flatInputs()

	idx := func(out string) int {
		for i, m := range members {
			if m.string() == out {
				return i
			}
		}
		return -1
	}

	cppIdx := idx("$(B)/lib/syn/syn.cpp.o")
	pbIdx := idx("$(B)/lib/syn/model.pb.cc.o")

	if cppIdx < 0 || pbIdx < 0 {
		t.Fatalf("archive missing members: syn.cpp.o=%d model.pb.cc.o=%d in %v", cppIdx, pbIdx, members)
	}
	if cppIdx > pbIdx {
		t.Fatalf("generated model.pb.cc.o (idx %d) must archive AFTER direct syn.cpp.o (idx %d) regardless of SRCS order; got %v", pbIdx, cppIdx, members)
	}
}

// TestGen_GztProto_EmitsGZProducerPBAndArchive covers a `.gztproto` source: emit
// a GZ/yellow producer writing <base>.proto, run protoc to <base>.pb.{cc,h},
// compile, and archive. A `.gztproto` import must resolve through the generated
// <base>.pb.h — NOT the .cfgproto.pb.h naming rule.
func TestGen_GztProto_EmitsGZProducerPBAndArchive(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "gzt/model/ya.make", `PROTO_LIBRARY()
PEERDIR(
    gzt/peer
    gzt/data
)
SRCS(model.gztproto)
END()
`)
	writeTestModuleFile(files, "gzt/model/model.gztproto", `import "gzt/peer/peer.gztproto";
import "gzt/data/data.proto";

package NGzt;

message TModel {
    optional NGzt.TPeer Peer = 1;
    optional NGzt.TData Data = 2;
}
`)
	writeTestModuleFile(files, "gzt/peer/ya.make", "PROTO_LIBRARY()\nSRCS(peer.gztproto)\nEND()\n")
	writeTestModuleFile(files, "gzt/peer/peer.gztproto", "package NGzt;\nmessage TPeer { optional uint32 X = 1; }\n")
	writeTestModuleFile(files, "gzt/data/ya.make", "PROTO_LIBRARY()\nSRCS(data.proto)\nEND()\n")
	writeTestModuleFile(files, "gzt/data/data.proto", "syntax = \"proto2\";\npackage NGzt;\nmessage TData {}\n")

	writeTestModuleFile(files, "dict/gazetteer/converter/ya.make", `PROGRAM(gztconverter)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
INDUCED_DEPS(proto ${ARCADIA_ROOT}/kernel/gazetteer/proto/base.proto)
INDUCED_DEPS(h+cpp ${ARCADIA_BUILD_ROOT}/kernel/gazetteer/proto/base.pb.h)
END()
`)
	writeTestModuleFile(files, "dict/gazetteer/converter/main.cpp", "int main(){return 0;}\n")
	writeTestModuleFile(files, "kernel/gazetteer/proto/ya.make", "PROTO_LIBRARY()\nSRCS(base.proto)\nEND()\n")
	writeTestModuleFile(files, "kernel/gazetteer/proto/base.proto", "syntax = \"proto2\";\npackage NGztBase;\nmessage TBase {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "gzt/model")

	// 1. GZ producer writes the generated .proto.
	gz := mustNodeByOutput(t, g, "$(B)/gzt/model/model.proto")
	if gz.KV.P != pkGZ || gz.KV.PC != pcYellow {
		t.Fatalf("GZ producer KV = {%v,%v}, want {GZ,yellow}", gz.KV.P, gz.KV.PC)
	}

	gzArgs := strStrs(gz.Cmds[0].CmdArgs.flat())
	for _, want := range []string{
		"$(B)/dict/gazetteer/converter/gztconverter",
		"-I$(S)/contrib/libs/protobuf/src",
		"-I$(B)",
		"-I$(S)",
		"$(S)/gzt/model/model.gztproto",
		"$(B)/gzt/model/model.proto",
	} {
		if indexOfArg(gz.Cmds[0].CmdArgs.flat(), want) < 0 {
			t.Errorf("GZ command missing %q\ngot: %v", want, gzArgs)
		}
	}

	for _, want := range []string{
		"$(B)/dict/gazetteer/converter/gztconverter", // tool binary
		"$(S)/gzt/model/model.gztproto",              // source
		"$(S)/gzt/peer/peer.gztproto",                // imported .gztproto
		"$(S)/gzt/data/data.proto",                   // imported .proto
		"$(S)/kernel/gazetteer/proto/base.proto",     // INDUCED
	} {
		if !nodeHasInput(gz, want) {
			t.Errorf("GZ producer inputs missing %q\ngot: %v", want, gz.flatInputs())
		}
	}

	// 2. The generated .proto runs through the protoc path.
	pb := mustNodeByOutput(t, g, "$(B)/gzt/model/model.pb.h")
	pbHasCC := false
	for _, o := range pb.Outputs {
		if o.string() == "$(B)/gzt/model/model.pb.cc" {
			pbHasCC = true
		}
	}
	if !pbHasCC {
		t.Fatalf("PB node missing model.pb.cc output: %v", pb.Outputs)
	}
	if pb.KV.P != pkPB || pb.KV.PC != pcYellow {
		t.Fatalf("PB producer KV = {%v,%v}, want {PB,yellow}", pb.KV.P, pb.KV.PC)
	}
	// The PB node is fed the generated $(B) .proto and the GZ producer's sources.
	if !nodeHasInput(pb, "$(B)/gzt/model/model.proto") {
		t.Errorf("PB node missing generated $(B) model.proto input: %v", pb.flatInputs())
	}
	if !nodeHasInput(pb, "$(S)/gzt/model/model.gztproto") {
		t.Errorf("PB node missing producer-source model.gztproto: %v", pb.flatInputs())
	}

	// 3. The generated .pb.cc compiles and archives.
	obj := mustNodeByOutput(t, g, "$(B)/gzt/model/model.pb.cc.o")
	if !nodeHasInput(obj, "$(B)/gzt/model/model.pb.cc") {
		t.Fatalf("object missing generated .pb.cc input: %v", obj.flatInputs())
	}

	ar := findNodeByOutputPrefix(g, "$(B)/gzt/model/libgzt-model")
	if ar == nil {
		t.Fatal("no gzt/model proto archive found")
	}
	if !nodeHasInput(ar, "$(B)/gzt/model/model.pb.cc.o") {
		t.Fatalf("archive missing model.pb.cc.o member: %v", ar.flatInputs())
	}

	// 4. A .gztproto import resolves through the generated <base>.pb.h, NOT the
	// .cfgproto.pb.h rule.
	if !nodeHasInput(obj, "$(B)/gzt/peer/peer.pb.h") {
		t.Errorf("object must reach the imported .gztproto's generated peer.pb.h: %v", obj.flatInputs())
	}
	if nodeHasInput(obj, "$(B)/gzt/peer/peer.gztproto.pb.h") {
		t.Errorf("a .gztproto import must NOT induce peer.gztproto.pb.h (the .cfgproto rule): %v", obj.flatInputs())
	}
	// The generated peer .proto rides as a producer source, not a compile input.
	if nodeHasInput(obj, "$(B)/gzt/peer/peer.proto") {
		t.Errorf("generated peer.proto must not be a compile input: %v", obj.flatInputs())
	}
}
