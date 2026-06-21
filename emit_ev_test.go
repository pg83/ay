package main

import (
	"testing"
)

// TestGen_ProtoLibrary_TransitivePROTONamespaceReachesEVCmd locks the EV
// counterpart of TestGen_ProtoLibrary_TransitivePROTONamespaceReachesCppProtoCmd:
// CPP_EV_CMDLINE reuses _CPP_PROTO_CMDLINE_BASE, so a PROTO_LIBRARY with a .ev
// source that PEERDIR-reaches a bare PROTO_NAMESPACE(yt) provider must carry
// -I=$(S)/yt in its protoc include block, exactly like the PB path.
func TestGen_ProtoLibrary_TransitivePROTONamespaceReachesEVCmd(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "leaf/ya.make", `PROTO_LIBRARY()
PROTO_NAMESPACE(yt)
SRCS(leaf.proto)
END()
`)
	writeTestModuleFile(files, "leaf/leaf.proto", "syntax = \"proto3\";\npackage test;\nmessage Leaf {}\n")

	writeTestModuleFile(files, "consumer/ya.make", `PROTO_LIBRARY()
PEERDIR(leaf)
SRCS(events.ev)
END()
`)
	writeTestModuleFile(files, "consumer/events.ev", "message TEvent {\n}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/event2cpp", "event2cpp")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "library/cpp/eventlog/ya.make", "LIBRARY()\nSRCS(eventlog.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/eventlog/eventlog.cpp", "int eventlog(){return 0;}\n")

	g := testGen(newMemFS(files), "consumer")

	ev := findGraphNodeByOutputs(t, g,
		"$(B)/consumer/events.ev.pb.cc",
		"$(B)/consumer/events.ev.pb.h",
	)

	args := strStrs(ev.Cmds[0].CmdArgs.flat())

	ytCount := 0
	for _, a := range args {
		if a == "-I=$(S)/yt" {
			ytCount++
		}
	}
	if ytCount == 0 {
		t.Fatalf("EV cmd missing transitive PROTO_NAMESPACE token -I=$(S)/yt: %v", args)
	}
	if ytCount > 1 {
		t.Fatalf("EV cmd duplicates -I=$(S)/yt (%d times): %v", ytCount, args)
	}

	// No source-root C++ -I leakage: the proto include uses -I=$(S)/..., never
	// the C++ form -I$(S)/yt.
	for _, a := range args {
		if a == "-I$(S)/yt" {
			t.Fatalf("EV cmd leaks C++ source-root include -I$(S)/yt: %v", args)
		}
	}

	// The peer proto include sits inside the include block, before --cpp_out.
	ytIdx := indexOfArg(ev.Cmds[0].CmdArgs.flat(), "-I=$(S)/yt")
	cppOutIdx := indexOfArg(ev.Cmds[0].CmdArgs.flat(), "--cpp_out=:$(B)/")
	if cppOutIdx < 0 {
		t.Fatalf("EV cmd missing --cpp_out=:$(B)/: %v", args)
	}
	if !(ytIdx < cppOutIdx) {
		t.Fatalf("expected -I=$(S)/yt before --cpp_out: yt=%d cpp_out=%d args=%v", ytIdx, cppOutIdx, args)
	}

	// Control: without SET(PROTOC_TRANSITIVE_HEADERS "no") the EV cpp_out keeps
	// its bare form and must NOT gain the lite-header proto_h token.
	if containsString(args, "--cpp_out=proto_h=true:$(B)/") {
		t.Fatalf("EV cmd unexpectedly emits proto_h cpp_out without PROTOC_TRANSITIVE_HEADERS=no: %v", args)
	}
}

// TestGen_EV_LiteHeaders_CppOutProtoH locks the EV counterpart of the PB
// lite-header rule: CPP_EV_CMDLINE shares _CPP_PROTO_CMDLINE_BASE with the PB
// command, so SET(PROTOC_TRANSITIVE_HEADERS "no") (ymake.core.conf:656 →
// CPP_PROTO_PLUGINS=proto_h=true) makes the EV --cpp_out carry proto_h=true,
// exactly like the .pb.cc producer. Representative: yabs/server/proto/event_log.
func TestGen_EV_LiteHeaders_CppOutProtoH(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "consumer/ya.make", `PROTO_LIBRARY()
SET(PROTOC_TRANSITIVE_HEADERS "no")
SRCS(events.ev)
END()
`)
	writeTestModuleFile(files, "consumer/events.ev", "message TEvent {\n}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeToolProgram(files, "tools/event2cpp", "event2cpp")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")
	writeTestModuleFile(files, "library/cpp/eventlog/ya.make", "LIBRARY()\nSRCS(eventlog.cpp)\nEND()\n")
	writeTestModuleFile(files, "library/cpp/eventlog/eventlog.cpp", "int eventlog(){return 0;}\n")

	g := testGen(newMemFS(files), "consumer")

	ev := findGraphNodeByOutputs(t, g,
		"$(B)/consumer/events.ev.pb.cc",
		"$(B)/consumer/events.ev.pb.h",
	)

	args := strStrs(ev.Cmds[0].CmdArgs.flat())

	if !containsString(args, "--cpp_out=proto_h=true:$(B)/") {
		t.Fatalf("EV cmd missing lite-header cpp_out --cpp_out=proto_h=true:$(B)/: %v", args)
	}
	if containsString(args, "--cpp_out=:$(B)/") {
		t.Fatalf("EV cmd retains bare --cpp_out=:$(B)/ alongside proto_h form: %v", args)
	}
}
