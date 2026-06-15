package main

import (
	"slices"
	"testing"
)

func TestEmitPB_ExtraProtocFlags(t *testing.T) {
	e := newBufferedEmitter()
	inst := targetInstance("pkg/proto")

	blocks := composePBArgBlocks(testToolchain(),
		intern("$(B)/contrib/tools/protoc/protoc"),
		intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false, 0, "", false, false,
		internArgs([]string{"--fatal_warnings"}), nil, nil, nil)
	emitPB(
		inst,
		"pkg/proto/test.proto",
		VFS(0),
		NodeRef(1),
		NodeRef(2),
		NodeRef(0),
		intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		intern("$(B)/contrib/tools/protoc/protoc"),
		intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false,
		0,
		false,
		nil,
		nil,
		nil,
		nil,
		blocks,
		e,
	)

	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	if !contains(e.nodes[0].Cmds[0].CmdArgs.flat(), "--fatal_warnings") {
		t.Fatalf("cmd_args missing --fatal_warnings: %v", e.nodes[0].Cmds[0].CmdArgs.flat())
	}
}

func TestEmitPB_LiteHeadersAddDepsOutputAndCppOutOption(t *testing.T) {
	e := newBufferedEmitter()
	inst := targetInstance("pkg/proto")

	blocks := composePBArgBlocks(testToolchain(),
		intern("$(B)/contrib/tools/protoc/protoc"),
		intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false, 0, "", false, true,
		nil, nil, nil, nil)
	emitPB(
		inst,
		"pkg/proto/test.proto",
		VFS(0),
		NodeRef(1),
		NodeRef(2),
		NodeRef(0),
		intern("$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"),
		intern("$(B)/contrib/tools/protoc/protoc"),
		intern("$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp"),
		false,
		0,
		true,
		nil,
		nil,
		nil,
		nil,
		blocks,
		e,
	)

	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	got := e.nodes[0]
	wantOutputs := []string{
		"$(B)/pkg/proto/test.pb.h",
		"$(B)/pkg/proto/test.pb.cc",
		"$(B)/pkg/proto/test.deps.pb.h",
	}
	if len(got.Outputs) != len(wantOutputs) {
		t.Fatalf("outputs len = %d, want %d (%v)", len(got.Outputs), len(wantOutputs), got.Outputs)
	}
	for i, want := range wantOutputs {
		if got.Outputs[i].string() != want {
			t.Fatalf("outputs[%d] = %q, want %q", i, got.Outputs[i].string(), want)
		}
	}

	if !contains(got.Cmds[0].CmdArgs.flat(), "--cpp_out=proto_h=true:$(B)/") {
		t.Fatalf("cmd_args missing lite-header cpp_out option: %v", got.Cmds[0].CmdArgs.flat())
	}
	if !contains(got.Cmds[0].CmdArgs.flat(), "$(B)/pkg/proto/test.deps.pb.h") {
		t.Fatalf("cmd_args missing deps header output: %v", got.Cmds[0].CmdArgs.flat())
	}
}

func TestGen_ProtoLibrary_CPPProtoPlugin0WiresToolDeps(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
PROTOC_FATAL_WARNINGS()
GRPC()
CPP_PROTO_PLUGIN0(config_proto_plugin tools/config_plugin DEPS deps/generated_runtime)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(files, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")

	writeTestModuleFile(files, "tools/config_plugin/ya.make", `PROGRAM(config_proto_plugin)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
PEERDIR(
    deps/plugin_runtime
)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tools/config_plugin/main.cpp", "int main(){return 0;}\n")

	writeTestModuleFile(files, "contrib/libs/grpc/ya.make", "LIBRARY()\nSRCS(grpc.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/grpc/grpc.cpp", "int grpc(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "deps/generated_runtime/ya.make", "LIBRARY()\nSRCS(gen.cpp)\nEND()\n")
	writeTestModuleFile(files, "deps/generated_runtime/gen.cpp", "int gen(){return 0;}\n")
	writeTestModuleFile(files, "deps/plugin_runtime/ya.make", "LIBRARY()\nSRCS(runtime.cpp)\nEND()\n")
	writeTestModuleFile(files, "deps/plugin_runtime/runtime.cpp", "int runtime(){return 0;}\n")

	g := testGen(newMemFS(files), "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.grpc.pb.cc",
		"$(B)/protos/test.grpc.pb.h",
	)
	styleguide := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide")
	grpcCpp := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	protoc := mustNodeByOutput(t, g, "$(B)/contrib/tools/protoc/protoc")
	configPlugin := mustNodeByOutput(t, g, "$(B)/tools/config_plugin/config_proto_plugin")
	pluginRuntime := mustNodeByOutput(t, g, "$(B)/deps/plugin_runtime/libdeps-plugin_runtime.a")
	_ = mustNodeByOutput(t, g, "$(B)/deps/generated_runtime/libdeps-generated_runtime.a")

	if !containsString(strStrs(pb.Cmds[0].CmdArgs.flat()), "--plugin=protoc-gen-config_proto_plugin=$(B)/tools/config_plugin/config_proto_plugin") {
		t.Fatalf("pb cmd args missing config proto plugin: %v", pb.Cmds[0].CmdArgs.flat())
	}
	if !containsString(strStrs(pb.Cmds[0].CmdArgs.flat()), "--config_proto_plugin_out=$(B)/") {
		t.Fatalf("pb cmd args missing config proto plugin out flag: %v", pb.Cmds[0].CmdArgs.flat())
	}

	sourceIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "protos/test.proto")
	grpcIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--plugin=protoc-gen-grpc_cpp=$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp")
	configIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--plugin=protoc-gen-config_proto_plugin=$(B)/tools/config_plugin/config_proto_plugin")
	if sourceIdx < 0 || grpcIdx < 0 || configIdx < 0 {
		t.Fatalf("missing source/grpc/config args in pb cmd: %v", pb.Cmds[0].CmdArgs.flat())
	}
	if !(sourceIdx < grpcIdx && grpcIdx < configIdx) {
		t.Fatalf("pb plugin arg order = source:%d grpc:%d config:%d, want source < grpc < config", sourceIdx, grpcIdx, configIdx)
	}

	inputs := make([]string, 0, len(pb.flatInputs()))
	for _, input := range pb.flatInputs() {
		inputs = append(inputs, input.string())
	}
	wantInputsPrefix := []string{
		"$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide",
		"$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp",
		"$(B)/contrib/tools/protoc/protoc",
		"$(B)/tools/config_plugin/config_proto_plugin",
		"$(S)/build/scripts/cpp_proto_wrapper.py",
		"$(S)/protos/test.proto",
	}
	if len(inputs) < len(wantInputsPrefix) || !equalStrings(inputs[:len(wantInputsPrefix)], wantInputsPrefix) {
		t.Fatalf("pb inputs prefix = %v, want %v", inputs, wantInputsPrefix)
	}

	wantDeps := []UID{styleguide.UID, grpcCpp.UID, protoc.UID, configPlugin.UID}
	if len(graphDeps(g, pb)) != len(wantDeps) {
		t.Fatalf("pb deps len = %d, want %d (%v)", len(graphDeps(g, pb)), len(wantDeps), graphDeps(g, pb))
	}
	for _, want := range wantDeps {
		if !slices.Contains(graphDeps(g, pb), want) {
			t.Fatalf("pb deps = %v, missing %q", graphDeps(g, pb), want)
		}
	}
	if got := graphForeignDeps(g, pb); len(got) != len(wantDeps) {
		t.Fatalf("pb foreign_deps[tool] len = %d, want %d (%v)", len(got), len(wantDeps), got)
	} else {
		for _, want := range wantDeps {
			if !slices.Contains(got, want) {
				t.Fatalf("pb foreign_deps[tool] = %v, missing %q", got, want)
			}
		}
	}
	if !nodeHasHostTag(nodeTags(configPlugin)) {
		t.Fatalf("config proto plugin tags = %v, want host tool tag", nodeTags(configPlugin))
	}
	if !slices.Contains(graphDeps(g, configPlugin), pluginRuntime.UID) {
		t.Fatalf("config proto plugin deps = %v, want runtime peer uid %q", graphDeps(g, configPlugin), pluginRuntime.UID)
	}
}

func TestGen_ProtoLibrary_CPPProtoPluginOutputsReachWrapper(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
CPP_PROTO_PLUGIN(tasklet_cpp tools/tasklet_plugin .tasklet.h)
SRCS(test.proto)
END()
`)
	writeTestModuleFile(files, "protos/test.proto", `syntax = "proto3";
package test;
message Row {}
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "tools/tasklet_plugin/ya.make", `PROGRAM(tasklet_cpp)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "tools/tasklet_plugin/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "protos")

	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.tasklet.h",
	)

	outputsIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--outputs")
	separatorIdx := indexOfArg(pb.Cmds[0].CmdArgs.flat(), "--")
	if outputsIdx < 0 || separatorIdx < 0 || separatorIdx <= outputsIdx {
		t.Fatalf("pb wrapper output section malformed: %v", pb.Cmds[0].CmdArgs.flat())
	}

	wantWrapperOutputs := []string{
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.tasklet.h",
	}
	gotWrapperOutputs := pb.Cmds[0].CmdArgs.flat()[outputsIdx+1 : separatorIdx]
	if !equalStrings(strStrs(gotWrapperOutputs), wantWrapperOutputs) {
		t.Fatalf("pb wrapper outputs = %v, want %v", gotWrapperOutputs, wantWrapperOutputs)
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNoKeepsPublicImportsOnLitePBHeader(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
SRCS(
    leaf.proto
    public.proto
    main.proto
)
END()
`)
	writeTestModuleFile(files, "protos/leaf.proto", `syntax = "proto3";
package test;
message Leaf {
  string value = 1;
}
`)
	writeTestModuleFile(files, "protos/public.proto", `syntax = "proto3";
package test;
import public "leaf.proto";
message PublicMessage {
  Leaf leaf = 1;
}
`)
	writeTestModuleFile(files, "protos/main.proto", `syntax = "proto3";
package test;
import public "public.proto";
message Main {
  PublicMessage message = 1;
}
`)
	writeTestModuleFile(files, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "app/use.cpp", `#include <protos/main.pb.h>
int use() { return 0; }
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	useCC := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")
	mainPB := mustNodeByOutput(t, g, "$(B)/protos/main.pb.h")
	publicPB := mustNodeByOutput(t, g, "$(B)/protos/public.pb.h")
	leafPB := mustNodeByOutput(t, g, "$(B)/protos/leaf.pb.h")

	for _, want := range []string{
		"$(B)/protos/main.pb.h",
		"$(B)/protos/public.pb.h",
		"$(B)/protos/leaf.pb.h",
	} {
		if !nodeHasInput(useCC, want) {
			t.Fatalf("use.cpp.o inputs missing %q: %#v", want, useCC.flatInputs())
		}
	}
	for _, want := range []UID{mainPB.UID, publicPB.UID, leafPB.UID} {
		if !slices.Contains(graphDeps(g, useCC), want) {
			t.Fatalf("use.cpp.o deps missing %q: %v", want, graphDeps(g, useCC))
		}
	}
}

func TestGen_ProtoLibrary_TransitiveHeadersNo_DepsHeaderUsesRuntimeRoot(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "protos/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
SRCS(test.proto)
END()
`)
	writeTestModuleFile(files, "protos/test.proto", `syntax = "proto3";
package test;
import "google/protobuf/any.proto";
message Row {
  google.protobuf.Any body = 1;
}
`)
	writeTestModuleFile(files, "app/ya.make", `LIBRARY()
PEERDIR(protos)
SRCS(use.cpp)
END()
`)
	writeTestModuleFile(files, "app/use.cpp", `#include <protos/test.deps.pb.h>
int use() { return 0; }
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/any.proto", `syntax = "proto3";
package google.protobuf;
message Any {}
`)
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/any.pb.h", "#pragma once\n")

	g := testGen(newMemFS(files), "app")
	pb := findGraphNodeByOutputs(t, g,
		"$(B)/protos/test.pb.h",
		"$(B)/protos/test.pb.cc",
		"$(B)/protos/test.deps.pb.h",
	)
	use := mustNodeByOutput(t, g, "$(B)/app/use.cpp.o")

	if !nodeHasInput(use, "$(B)/protos/test.deps.pb.h") {
		t.Fatalf("use.cpp.o inputs missing deps header output: %#v", use.flatInputs())
	}
	if !nodeHasInput(use, "$(S)/contrib/libs/protobuf/src/google/protobuf/any.pb.h") {
		t.Fatalf("use.cpp.o inputs missing protobuf runtime WKT header: %#v", use.flatInputs())
	}
	if nodeHasInput(use, "$(S)/google/protobuf/any.pb.h") {
		t.Fatalf("use.cpp.o inputs still contain unrebased WKT header path: %#v", use.flatInputs())
	}
	if !slices.Contains(graphDeps(g, use), pb.UID) {
		t.Fatalf("use.cpp.o deps missing PB producer uid %q: %v", pb.UID, graphDeps(g, use))
	}
}
