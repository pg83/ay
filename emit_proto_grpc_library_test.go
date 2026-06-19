package main

import (
	"strings"
	"testing"
)

// grpcLibraryFixture builds a plain LIBRARY() that lists an inline .proto in
// SRCS (the ads/dssm/inference shape: a LIBRARY, not a PROTO_LIBRARY). withGrpc
// toggles the GRPC() macro. Returns the generated graph for module "m/lib".
func grpcLibraryFixture(t *testing.T, withGrpc bool) *Graph {
	t.Helper()
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "contrib/tools/protoc/plugins/grpc_cpp", "grpc_cpp")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/grpc/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\nEND()\n")

	grpc := ""
	if withGrpc {
		grpc = "GRPC()\n"
	}
	writeTestModuleFile(files, "m/lib/ya.make", "LIBRARY()\nSRCDIR(m)\nSRCS(svc.proto use.cpp)\n"+grpc+"END()\n")
	writeTestModuleFile(files, "m/svc.proto", "syntax = \"proto3\";\npackage m;\nmessage Svc {}\n")
	writeTestModuleFile(files, "m/use.cpp", "int use(){return 0;}\n")

	return testGen(newMemFS(files), "m/lib")
}

// TestEmitLibraryProtoSource_GrpcEmitsProducerOutputsAndCompile pins the upstream
// GRPC() behavior for a plain LIBRARY() with an inline .proto: protoc gains the
// grpc_cpp plugin, so the .pb producer declares .grpc.pb.{cc,h} outputs, takes
// the grpc_cpp plugin tool as a command input, passes --grpc_cpp_out, and the
// generated .grpc.pb.cc is compiled into a .grpc.pb.cc.o object.
func TestEmitLibraryProtoSource_GrpcEmitsProducerOutputsAndCompile(t *testing.T) {
	g := grpcLibraryFixture(t, true)

	pb := mustNodeByOutput(t, g, "$(B)/m/svc.pb.h")

	hasOut := func(n *Node, want string) bool {
		for _, o := range n.Outputs {
			if o.string() == want {
				return true
			}
		}
		return false
	}

	for _, want := range []string{"$(B)/m/svc.grpc.pb.cc", "$(B)/m/svc.grpc.pb.h"} {
		if !hasOut(pb, want) {
			t.Errorf("pb producer missing grpc output %q; outputs=%v", want, pb.Outputs)
		}
	}

	if !nodeHasInput(pb, "$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp") {
		t.Errorf("pb producer missing grpc_cpp plugin input; inputs=%v", pb.flatInputs())
	}

	args := pb.Cmds[0].CmdArgs.flat()
	if !contains(args, "--grpc_cpp_out=$(B)/") {
		t.Errorf("pb cmd missing --grpc_cpp_out; args=%v", args)
	}
	if !contains(args, "--plugin=protoc-gen-grpc_cpp=$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp") {
		t.Errorf("pb cmd missing grpc_cpp plugin flag; args=%v", args)
	}

	if !graphHasOutputSuffix(g, "svc.grpc.pb.cc.o") {
		t.Errorf("generated .grpc.pb.cc.o compile node missing")
	}
}

func graphHasOutputSuffix(g *Graph, suffix string) bool {
	for _, n := range g.Graph {
		for _, o := range n.Outputs {
			if strings.HasSuffix(o.string(), suffix) {
				return true
			}
		}
	}
	return false
}

// TestEmitLibraryProtoSource_NoGrpcUnchanged is the negative control: the same
// inline-proto LIBRARY WITHOUT GRPC() must declare none of the grpc outputs,
// plugin input, flags, or compile node.
func TestEmitLibraryProtoSource_NoGrpcUnchanged(t *testing.T) {
	g := grpcLibraryFixture(t, false)

	pb := mustNodeByOutput(t, g, "$(B)/m/svc.pb.h")

	for _, o := range pb.Outputs {
		if o.string() == "$(B)/m/svc.grpc.pb.cc" || o.string() == "$(B)/m/svc.grpc.pb.h" {
			t.Errorf("non-GRPC producer unexpectedly declares grpc output %q", o.string())
		}
	}

	if nodeHasInput(pb, "$(B)/contrib/tools/protoc/plugins/grpc_cpp/grpc_cpp") {
		t.Errorf("non-GRPC producer unexpectedly has grpc_cpp plugin input")
	}

	args := pb.Cmds[0].CmdArgs.flat()
	if contains(args, "--grpc_cpp_out=$(B)/") {
		t.Errorf("non-GRPC producer unexpectedly passes --grpc_cpp_out")
	}

	if graphHasOutputSuffix(g, "svc.grpc.pb.cc.o") {
		t.Errorf("non-GRPC module unexpectedly compiles a .grpc.pb.cc.o")
	}
}
