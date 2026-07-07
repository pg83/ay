package main

import "testing"

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

	gz := mustNodeByOutput(t, g, "$(B)/lib/syn/model.proto")

	if gz.KV.P != pkGZ || gz.KV.PC != pcYellow {
		t.Fatalf("GZ producer KV = {%v,%v}, want {GZ,yellow}", gz.KV.P, gz.KV.PC)
	}

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

	obj := mustNodeByOutput(t, g, "$(B)/lib/syn/model.pb.cc.o")

	if !nodeHasInput(obj, "$(B)/lib/syn/model.pb.cc") {
		t.Fatalf("object missing generated .pb.cc input: %v", obj.flatInputs())
	}

	ar := mustNodeByOutput(t, g, "$(B)/lib/syn/libsyn.a")

	if !nodeHasInput(ar, "$(B)/lib/syn/model.pb.cc.o") {
		t.Fatalf("library archive missing model.pb.cc.o member: %v", ar.flatInputs())
	}

	if !nodeHasInput(ar, "$(B)/lib/syn/syn.cpp.o") {
		t.Fatalf("library archive missing syn.cpp.o member: %v", ar.flatInputs())
	}
}

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
		"$(B)/gzt/model/model.proto",
		"$(B)/gzt/peer/peer.proto",
		"$(S)/gzt/model/model.gztproto",
		"$(S)/gzt/peer/peer.gztproto",
		"$(S)/gzt/data/data.proto",
		"$(S)/kernel/gazetteer/proto/base.proto",
	} {
		if !nodeHasInput(pb, want) {
			t.Errorf("consumer PB node missing transitive input %q\ngot: %v", want, pb.flatInputs())
		}
	}

	if nodeHasInput(pb, "$(B)/gzt/peer/peer.gztproto.pb.h") {
		t.Errorf("must NOT invent peer.gztproto.pb.h: %v", pb.flatInputs())
	}
}

func TestGen_GztProto_ArchivedAfterDirectSourcesRegardlessOfSRCSOrder(t *testing.T) {
	files := map[string]string{}

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

	gz := mustNodeByOutput(t, g, "$(B)/gzt/model/model.proto")

	if gz.KV.P != pkGZ || gz.KV.PC != pcYellow {
		t.Fatalf("GZ producer KV = {%v,%v}, want {GZ,yellow}", gz.KV.P, gz.KV.PC)
	}

	gzArgs := anyStrs(gz.Cmds[0].CmdArgs.flat())

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
		"$(B)/dict/gazetteer/converter/gztconverter",
		"$(S)/gzt/model/model.gztproto",
		"$(S)/gzt/peer/peer.gztproto",
		"$(S)/gzt/data/data.proto",
		"$(S)/kernel/gazetteer/proto/base.proto",
	} {
		if !nodeHasInput(gz, want) {
			t.Errorf("GZ producer inputs missing %q\ngot: %v", want, gz.flatInputs())
		}
	}

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

	if !nodeHasInput(pb, "$(B)/gzt/model/model.proto") {
		t.Errorf("PB node missing generated $(B) model.proto input: %v", pb.flatInputs())
	}

	if !nodeHasInput(pb, "$(S)/gzt/model/model.gztproto") {
		t.Errorf("PB node missing producer-source model.gztproto: %v", pb.flatInputs())
	}

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

	if !nodeHasInput(obj, "$(B)/gzt/peer/peer.pb.h") {
		t.Errorf("object must reach the imported .gztproto's generated peer.pb.h: %v", obj.flatInputs())
	}

	if nodeHasInput(obj, "$(B)/gzt/peer/peer.gztproto.pb.h") {
		t.Errorf("a .gztproto import must NOT induce peer.gztproto.pb.h (the .cfgproto rule): %v", obj.flatInputs())
	}

	if nodeHasInput(obj, "$(B)/gzt/peer/peer.proto") {
		t.Errorf("generated peer.proto must not be a compile input: %v", obj.flatInputs())
	}
}
