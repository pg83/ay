package main

import "testing"

// TestGen_GztProto_EmitsGZProducerPBAndArchive reproduces the T-64 sg7 gap: a
// `.gztproto` source (_SRC("gztproto"), ymake.core.conf:3324) must emit a
// GZ/yellow producer (dict/gazetteer/converter) writing <base>.proto, then run
// that generated .proto through the ordinary protoc path to <base>.pb.{cc,h},
// compile <base>.pb.cc.o, and archive it. A `.gztproto` import must resolve
// through the generated <base>.pb.h — NOT the .cfgproto.pb.h naming rule.
// Representative upstream nodes:
// $(B)/search/begemot/rules/init/xml_auth/proto/xml_auth.proto (GZ) and its
// xml_auth.pb.{h,cc}/.pb.cc.o/libinit-xml_auth-proto.a chain.
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

	// dict/gazetteer/converter: the GZ tool. INDUCED_DEPS(proto …) injects
	// base.proto into every generated .proto; INDUCED_DEPS(h+cpp …) the .pb.h.
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

	// 1. GZ producer: dict/gazetteer/converter writes the generated .proto.
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
		"$(B)/dict/gazetteer/converter/gztconverter", // the tool binary
		"$(S)/gzt/model/model.gztproto",              // the source
		"$(S)/gzt/peer/peer.gztproto",                // imported .gztproto
		"$(S)/gzt/data/data.proto",                   // imported ordinary .proto
		"$(S)/kernel/gazetteer/proto/base.proto",     // INDUCED_DEPS(proto …)
	} {
		if !nodeHasInput(gz, want) {
			t.Errorf("GZ producer inputs missing %q\ngot: %v", want, gz.flatInputs())
		}
	}

	// 2. The generated .proto runs through the ordinary protoc path.
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
	// protoSrcOverride: the PB node is fed the generated $(B) .proto and rides the
	// GZ producer's .gztproto sources.
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

	// 4. Import-shaped: a .gztproto import resolves through the generated <base>.pb.h
	// (model.pb.h #includes peer.pb.h), NOT the .cfgproto.pb.h rule.
	if !nodeHasInput(obj, "$(B)/gzt/peer/peer.pb.h") {
		t.Errorf("object must reach the imported .gztproto's generated peer.pb.h: %v", obj.flatInputs())
	}
	if nodeHasInput(obj, "$(B)/gzt/peer/peer.gztproto.pb.h") {
		t.Errorf("a .gztproto import must NOT induce peer.gztproto.pb.h (the .cfgproto rule): %v", obj.flatInputs())
	}
	// The generated peer .proto is a codegen intermediate; it must not drag into
	// the compile closure (it rides as the .gztproto producer source instead).
	if nodeHasInput(obj, "$(B)/gzt/peer/peer.proto") {
		t.Errorf("generated peer.proto must not be a compile input: %v", obj.flatInputs())
	}
}
