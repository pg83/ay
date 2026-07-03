package main

import (
	"slices"
	"strings"
	"testing"
)

func TestScheduleProducers_NoEdgesKeepsDeclarationOrder(t *testing.T) {
	positions := []ProducerPos{
		{kind: prodRunProgram, index: 0, outs: []VFS{build("m/a.bin")}},
		{kind: prodRunProgram, index: 1, outs: []VFS{build("m/b.bin")}},
		{kind: prodRunPython, index: 0, outs: []VFS{build("m/c.txt")}},
	}

	var m IdValueMap

	order := scheduleProducers(&m, positions, "m")

	for i, pi := range order {
		if pi != i {
			t.Fatalf("no-edge order = %v, want identity", order)
		}
	}
}

func TestScheduleProducers_ReversedChainReordered(t *testing.T) {
	positions := []ProducerPos{
		{kind: prodRunProgram, index: 0, outs: []VFS{build("m/third.bin")}, ins: []VFS{build("m/second.bin")}},
		{kind: prodRunProgram, index: 1, outs: []VFS{build("m/second.bin")}, ins: []VFS{build("m/first.txt")}},
		{kind: prodRunPython, index: 0, outs: []VFS{build("m/first.txt")}},
	}

	var m IdValueMap

	order := scheduleProducers(&m, positions, "m")

	if order[0] != 2 || order[1] != 1 || order[2] != 0 {
		t.Fatalf("chain order = %v, want [2 1 0]", order)
	}
}

func TestScheduleProducers_TieBreaksByDeclarationOrder(t *testing.T) {
	positions := []ProducerPos{
		{kind: prodRunProgram, index: 0, outs: []VFS{build("m/late.bin")}, ins: []VFS{build("m/base.txt")}},
		{kind: prodRunProgram, index: 1, outs: []VFS{build("m/mid.bin")}},
		{kind: prodRunPython, index: 0, outs: []VFS{build("m/base.txt")}},
	}

	var m IdValueMap

	order := scheduleProducers(&m, positions, "m")

	if order[0] != 1 || order[1] != 2 || order[2] != 0 {
		t.Fatalf("tie order = %v, want [1 2 0]", order)
	}
}

func TestScheduleProducers_CycleThrows(t *testing.T) {
	positions := []ProducerPos{
		{kind: prodRunProgram, index: 0, outs: []VFS{build("m/a.bin")}, ins: []VFS{build("m/b.bin")}},
		{kind: prodRunProgram, index: 1, outs: []VFS{build("m/b.bin")}, ins: []VFS{build("m/a.bin")}},
	}

	var m IdValueMap

	exc := try(func() {
		scheduleProducers(&m, positions, "m")
	})

	if exc == nil {
		t.Fatal("cycle did not throw")
	}

	if !strings.Contains(exc.Error(), "dependency cycle") {
		t.Fatalf("cycle threw %q, want dependency-cycle diagnostics", exc.Error())
	}
}

func TestGen_ProtoImportOfGztGeneratedProtoDeclarationOrderIndependent(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "lib/syn/ya.make", `LIBRARY(syn)
SRCS(
    user.proto
    model.gztproto
)
END()
`)
	writeTestModuleFile(files, "lib/syn/user.proto", `syntax = "proto2";
import "lib/syn/model.proto";
package NGzt;
message TUser { optional TModel m = 1; }
`)
	writeTestModuleFile(files, "lib/syn/model.gztproto", "package NGzt;\nmessage TModel { optional uint32 X = 1; }\n")

	writeTestModuleFile(files, "dict/gazetteer/converter/ya.make", `PROGRAM(gztconverter)
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "dict/gazetteer/converter/main.cpp", "int main(){return 0;}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	g := testGen(newMemFS(files), "lib/syn")
	gz := mustNodeByOutput(t, g, "$(B)/lib/syn/model.proto")
	userPB := mustNodeByAnyOutput(t, g, "$(B)/lib/syn/user.pb.h")

	if !nodeHasInput(userPB, "$(B)/lib/syn/model.proto") {
		t.Fatalf("user.proto PB inputs missing generated import $(B)/lib/syn/model.proto: %v", vfsStringsT3(userPB.flatInputs()))
	}

	if !slices.Contains(graphDeps(g, userPB), gz.Ref) {
		t.Fatalf("user.proto PB deps missing gzt converter ref %d: %v", gz.Ref, graphDeps(g, userPB))
	}
}
