package main

import (
	"testing"
)

// protoAddInclFixture builds the inline-proto-LIBRARY include-propagation shape
// (library/cpp/html/face): a plain LIBRARY() that lists an inline .proto plus an
// ordinary .cpp in SRCS. Upstream _CPP_PROTO_CMD (proto.conf:461) attaches
// `.PEERDIR=contrib/libs/protobuf` to every C++ proto compile, so the module —
// PROTO_LIBRARY or not — peers contrib/libs/protobuf and inherits its GLOBAL
// ADDINCL (protobuf/src + the abseil roots protobuf itself peers). That GLOBAL
// band must reach the module's ORDINARY sources and its generated .pb.cc, and
// propagate transitively to a downstream consumer's ordinary sources.
//
// protobuf carries the same GLOBAL ADDINCL + abseil PEERDIR shape as the real
// contrib/libs/protobuf/ya.make; abseil-cpp-tstring peers abseil-cpp, so the
// closure of the three -I roots mirrors the reference parser.cpp.o band.
func protoAddInclFixture() FS {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")

	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"ADDINCL(GLOBAL contrib/libs/protobuf/src)\n"+
			"PEERDIR(contrib/restricted/abseil-cpp-tstring)\n"+
			"SRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/message.h", "#pragma once\n")

	writeTestModuleFile(files, "contrib/restricted/abseil-cpp-tstring/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"ADDINCL(GLOBAL contrib/restricted/abseil-cpp-tstring)\n"+
			"PEERDIR(contrib/restricted/abseil-cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/restricted/abseil-cpp/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"ADDINCL(GLOBAL contrib/restricted/abseil-cpp)\nEND()\n")

	// Inline-proto LIBRARY (the face shape): ordinary use.cpp + inline svc.proto.
	writeTestModuleFile(files, "m/lib/ya.make",
		"LIBRARY()\nSRCS(svc.proto use.cpp)\nEND()\n")
	writeTestModuleFile(files, "m/lib/svc.proto", "syntax = \"proto3\";\npackage m;\nmessage Svc {}\n")
	writeTestModuleFile(files, "m/lib/use.cpp", "int use(){return 0;}\n")

	// Consumer (the html5 shape): peers the inline-proto module, ordinary c.cpp.
	// Also peers an unrelated plain library to anchor the negative guard.
	writeTestModuleFile(files, "consumer/ya.make",
		"LIBRARY()\nPEERDIR(m/lib plain/lib)\nSRCS(c.cpp)\nEND()\n")
	writeTestModuleFile(files, "consumer/c.cpp", "int c(){return 0;}\n")

	writeTestModuleFile(files, "plain/lib/ya.make",
		"LIBRARY()\nSRCS(plain.cpp)\nEND()\n")
	writeTestModuleFile(files, "plain/lib/plain.cpp", "int plain(){return 0;}\n")

	return newMemFS(files)
}

// TestGen_InlineProtoLibrary_ProtobufGlobalAddInclReachesOrdinaryAndConsumer
// pins the T-60 divergence: the contrib/libs/protobuf GLOBAL ADDINCL band
// (protobuf/src + abseil-cpp-tstring + abseil-cpp) must land on an inline-proto
// LIBRARY's ordinary source AND its generated .pb.cc, and propagate to a
// downstream consumer's ordinary source — while NOT leaking to an unrelated
// module without the protobuf provider. Before the fix the base protobuf peer is
// added only for PROTO_LIBRARY, so the inline-proto LIBRARY never peers protobuf
// and none of these compiles see the band.
func TestGen_InlineProtoLibrary_ProtobufGlobalAddInclReachesOrdinaryAndConsumer(t *testing.T) {
	fs := protoAddInclFixture()
	g := testGen(fs, "consumer")

	band := []string{
		"-I$(S)/contrib/libs/protobuf/src",
		"-I$(S)/contrib/restricted/abseil-cpp-tstring",
		"-I$(S)/contrib/restricted/abseil-cpp",
	}

	assertBand := func(label, output string, want bool) {
		t.Helper()
		n := mustNodeByOutput(t, g, output)
		args := strStrs(n.Cmds[0].CmdArgs.flat())
		for _, inc := range band {
			has := flagsContain(args, inc)
			if has != want {
				t.Fatalf("%s (%s): include %q present=%v, want %v\nargs=%v", label, output, inc, has, want, args)
			}
		}
	}

	// Ordinary C++ source in the inline-proto module.
	assertBand("inline-proto ordinary src", "$(B)/m/lib/use.cpp.o", true)
	// Generated C++ source (the proto .pb.cc) in the same module.
	assertBand("inline-proto generated pb.cc", "$(B)/m/lib/svc.pb.cc.o", true)
	// Ordinary C++ source in a downstream consumer of the inline-proto module.
	assertBand("consumer ordinary src", "$(B)/consumer/c.cpp.o", true)
	// Negative guard: unrelated module without the protobuf provider.
	assertBand("unrelated module", "$(B)/plain/lib/plain.cpp.o", false)
}
