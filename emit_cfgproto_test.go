package main

import (
	"testing"
)

func TestGen_CfgProto_EmitsPBProducerAndCompile(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "lib/ya.make", `LIBRARY()
SRCS(backend.cpp backend_config.cfgproto)
END()
`)
	writeTestModuleFile(files, "lib/backend.cpp", "int backend(){return 0;}\n")
	writeTestModuleFile(files, "lib/backend_config.cfgproto", `package NSrvKernelProto;

import "library/cpp/proto_config/protos/extensions.proto";

option (NProtoConfig.Include) = "util/datetime/base.h";
option (NProtoConfig.Include) = "lib/port.h";

message Cfg {}
`)
	writeTestModuleFile(files, "lib/port.h", "#pragma once\n")
	writeTestModuleFile(files, "util/datetime/base.h", "#pragma once\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/proto_config/plugin", "plugin")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")

	writeTestModuleFile(files, "library/cpp/proto_config/protos/ya.make", "LIBRARY()\nSRCS(extensions.proto)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/proto_config/protos/extensions.proto", "syntax = \"proto2\";\nimport \"google/protobuf/descriptor.proto\";\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/descriptor.proto", "syntax = \"proto2\";\n")
	writeTestModuleFile(files, "library/cpp/proto_config/codegen/ya.make", "LIBRARY()\nSRCS(parse_value.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/proto_config/codegen/parse_value.cpp", "int parse(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "lib")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/lib/backend_config.cfgproto.pb.cc",
		"$(B)/lib/backend_config.cfgproto.pb.h",
	)

	if pb.KV.P != pkPB || pb.KV.PC != pcYellow {
		t.Fatalf("cfgproto producer KV = {%v,%v}, want {PB,yellow}", pb.KV.P, pb.KV.PC)
	}

	args := anyStrs(pb.Cmds[0].CmdArgs.flat())
	wantArgs := []string{
		"--config_out=$(B)/",
		"--cpp_out=:$(B)/",
		"--cpp_styleguide_out=:$(B)/",
		"--plugin=protoc-gen-config=$(B)/library/cpp/proto_config/plugin/plugin",
		"--plugin=protoc-gen-cpp_styleguide=$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide",
		"lib/backend_config.cfgproto",
	}

	for _, want := range wantArgs {
		found := false

		for _, a := range args {
			if a == want {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("cfgproto producer command missing %q\ngot: %v", want, args)
		}
	}

	for _, a := range args {
		if a == "--plugin=protoc-gen-event2cpp="+"$(B)/tools/event2cpp/event2cpp" {
			t.Errorf("cfgproto producer must not carry the event2cpp plugin: %v", args)
		}
	}

	wantInputs := []string{
		"$(B)/contrib/tools/protoc/protoc",
		"$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide",
		"$(B)/library/cpp/proto_config/plugin/plugin",
		"$(S)/lib/backend_config.cfgproto",
		"$(S)/build/scripts/cpp_proto_wrapper.py",
		"$(S)/library/cpp/proto_config/protos/extensions.proto",
		"$(S)/contrib/libs/protobuf/src/google/protobuf/descriptor.proto",
	}

	for _, want := range wantInputs {
		if !nodeHasInput(pb, want) {
			t.Errorf("cfgproto producer inputs missing %q\ngot: %v", want, pb.flatInputs())
		}
	}

	obj := mustNodeByOutput(t, g, "$(B)/lib/backend_config.cfgproto.pb.cc.o")

	if !nodeHasInput(obj, "$(B)/lib/backend_config.cfgproto.pb.cc") {
		t.Fatalf("cfgproto object missing generated .pb.cc input: %v", obj.flatInputs())
	}

	if nodeHasInput(obj, "$(B)/lib/backend_config.cfgproto.pb.h") {
		t.Errorf("cfgproto object must not carry its own generated .pb.h: %v", obj.flatInputs())
	}

	if !nodeHasInput(obj, "$(S)/util/datetime/base.h") {
		t.Errorf("cfgproto object missing NProtoConfig.Include header util/datetime/base.h: %v", obj.flatInputs())
	}

	if !nodeHasInput(obj, "$(S)/lib/port.h") {
		t.Errorf("cfgproto object missing NProtoConfig.Include header lib/port.h: %v", obj.flatInputs())
	}

	ar := findNodeByOutputPrefix(g, "$(B)/lib/liblib.a")

	if ar == nil {
		t.Fatal("no lib archive found")
	}

	if !nodeHasInput(ar, "$(B)/lib/backend_config.cfgproto.pb.cc.o") {
		t.Fatalf("archive missing cfgproto object member: %v", ar.flatInputs())
	}
}

func TestGen_CfgProto_WireFormatDoesNotLeakToConsumer(t *testing.T) {
	const wireFormat = "$(S)/contrib/libs/protobuf/src/google/protobuf/wire_format.h"

	files := map[string]string{}

	writeTestModuleFile(files, "lib/ya.make", `LIBRARY()
SRCS(backend_config.cfgproto)
END()
`)
	writeTestModuleFile(files, "lib/backend_config.cfgproto", `package NSrvKernelProto;

import "library/cpp/proto_config/protos/extensions.proto";

message Cfg {}
`)

	files["contrib/tools/protoc/ya.make"] = "PROGRAM(protoc)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\n" +
		"INDUCED_DEPS(cpp ${ARCADIA_ROOT}/contrib/libs/protobuf/src/google/protobuf/wire_format.h)\nEND()\n"
	files["contrib/tools/protoc/main.cpp"] = "int main(){return 0;}\n"
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/wire_format.h", "#pragma once\n")

	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "library/cpp/proto_config/plugin", "plugin")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")

	writeTestModuleFile(files, "library/cpp/proto_config/protos/ya.make", "LIBRARY()\nSRCS(extensions.proto)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/proto_config/protos/extensions.proto", "syntax = \"proto2\";\nimport \"google/protobuf/descriptor.proto\";\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/descriptor.proto", "syntax = \"proto2\";\n")
	writeTestModuleFile(files, "library/cpp/proto_config/codegen/ya.make", "LIBRARY()\nSRCS(parse_value.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/proto_config/codegen/parse_value.cpp", "int parse(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "app/ya.make", "LIBRARY()\nPEERDIR(lib)\nSRCS(use.cpp)\nEND()\n")
	writeTestModuleFile(files, "app/use.cpp", "#include <lib/backend_config.cfgproto.pb.h>\nint use(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	obj := mustNodeByOutput(t, g, "$(B)/lib/backend_config.cfgproto.pb.cc.o")

	if !nodeHasInput(obj, wireFormat) {
		t.Fatalf("cfgproto .pb.cc.o must keep protoc induced wire_format.h: %v", obj.flatInputs())
	}

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")

	for _, want := range []string{
		"$(B)/lib/backend_config.cfgproto.pb.cc",
		"$(B)/lib/backend_config.cfgproto.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Errorf("consumer use.cpp.o missing OutTogether sibling %q: %v", want, useCC.flatInputs())
		}
	}

	if nodeHasInput(useCC, wireFormat) {
		t.Errorf("consumer use.cpp.o must NOT inherit protoc cpp-only wire_format.h: %v", useCC.flatInputs())
	}
}

func TestGen_OrdinaryProto_HasNoConfigPlugin(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "lib/ya.make", `LIBRARY()
SRCS(data.proto)
END()
`)
	writeTestModuleFile(files, "lib/data.proto", "syntax = \"proto3\";\npackage lib;\nmessage Data {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "lib")

	var pb *Node

	for _, n := range g.Graph {
		if n.KV.P != pkPB {
			continue
		}

		for _, o := range n.Outputs {
			if o.string() == "$(B)/lib/data.pb.h" {
				pb = n
			}
		}
	}

	if pb == nil {
		t.Fatal("no PB producer for data.proto found")
	}

	for _, a := range anyStrs(pb.Cmds[0].CmdArgs.flat()) {
		if a == "--config_out=$(B)/" ||
			a == "--plugin=protoc-gen-config=$(B)/library/cpp/proto_config/plugin/plugin" {
			t.Fatalf("ordinary .proto must not carry the proto_config plugin: %q", a)
		}
	}
}
