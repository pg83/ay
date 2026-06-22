package main

import "testing"

// TestGen_ProtoLibrary_YmapsSprotoEmitsHeadersAndFeedsGeneratedCCInputs models a
// PROTO_LIBRARY with EXPORT_YMAPS_PROTO + YMAPS_SPROTO: emit a .sproto.h producer
// per proto, make the sprotoc tool reachable, and thread the .sproto.h plus the
// sproto runtime header into an importing proto's generated C++ compile inputs.
func TestGen_ProtoLibrary_YmapsSprotoEmitsHeadersAndFeedsGeneratedCCInputs(t *testing.T) {
	files := map[string]string{}

	mod := "maps/doc/proto/yandex/maps/proto/common2"

	writeTestModuleFile(files, mod+"/ya.make", `PROTO_LIBRARY()
EXPORT_YMAPS_PROTO()
PY_NAMESPACE(yandex.maps.proto.common2)
SRCS(
    attribution.proto
    image.proto
    response.proto
)
YMAPS_SPROTO(
    attribution.proto
    image.proto
    response.proto
)
EXCLUDE_TAGS(GO_PROTO)
END()
`)
	writeTestModuleFile(files, mod+"/image.proto", `syntax = "proto2";
package yandex.maps.proto.common2.image;
message Image {}
`)
	writeTestModuleFile(files, mod+"/attribution.proto", `syntax = "proto2";
package yandex.maps.proto.common2.attribution;
import "yandex/maps/proto/common2/image.proto";
message Attribution { optional Image image = 1; }
`)
	writeTestModuleFile(files, mod+"/response.proto", `syntax = "proto2";
package yandex.maps.proto.common2.response;
import "yandex/maps/proto/common2/attribution.proto";
message Response { optional Attribution a = 1; }
`)

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	// sprotoc's PROGRAM peer.
	writeTestModuleFile(files, "contrib/libs/protoc/ya.make", "LIBRARY()\nSRCS(protoc.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protoc/protoc.cpp", "int protoc(){return 0;}\n")

	writeTestModuleFile(files, "maps/libs/sproto/ya.make", "LIBRARY()\nSRCS(sproto.cpp)\nEND()\n")
	writeTestModuleFile(files, "maps/libs/sproto/sproto.cpp", "int sproto(){return 0;}\n")
	writeTestModuleFile(files, "maps/libs/sproto/include/sproto.h",
		"#pragma once\n#include <maps/libs/sproto/include/msgbase.h>\n")
	writeTestModuleFile(files, "maps/libs/sproto/include/msgbase.h", "#pragma once\n")

	writeTestModuleFile(files, "maps/libs/sproto/sprotoc/ya.make", `PROGRAM()
PEERDIR(
    contrib/libs/protoc
    maps/libs/sproto
)
INDUCED_DEPS(
    h+cpp
    ${ARCADIA_ROOT}/maps/libs/sproto/include/sproto.h
)
SRCS(main.cpp)
END()
`)
	writeTestModuleFile(files, "maps/libs/sproto/sprotoc/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), mod)

	// 1. The .sproto.h producer exists with the right node shape.
	sproto := mustNodeByOutput(t, g, "$(B)/"+mod+"/attribution.sproto.h")
	if sproto.KV.P != pkPB || sproto.KV.PC != pcYellow {
		t.Fatalf("attribution.sproto.h kv = {P:%q PC:%q}, want {PB yellow}", sproto.KV.P.string(), sproto.KV.PC.string())
	}

	cmd := strStrs(sproto.Cmds[0].CmdArgs.flat())
	for _, want := range []string{
		"$(B)/maps/libs/sproto/sprotoc/sprotoc",
		"--sproto_out=$(B)/maps/doc/proto",
		mod + "/attribution.proto",
	} {
		if !containsString(cmd, want) {
			t.Fatalf("attribution.sproto.h cmd missing %q: %v", want, cmd)
		}
	}
	if sproto.Cmds[0].Cwd != strS {
		t.Fatalf("attribution.sproto.h cwd = %q, want $(S)", sproto.Cmds[0].Cwd.string())
	}

	// 2. The sprotoc tool and its maps/libs/sproto library are reachable.
	mustNodeByOutput(t, g, "$(B)/maps/libs/sproto/sprotoc/sprotoc")
	mustNodeByOutput(t, g, "$(B)/maps/libs/sproto/libmaps-libs-sproto.a")

	// 3. The importing proto's generated C++ unit sees the .sproto.h and, via the
	// sprotoc induced deps, the sproto runtime header — transitively across imports.
	attrCC := mustNodeByOutput(t, g, "$(B)/"+mod+"/attribution.pb.cc.o")
	for _, want := range []string{
		"$(B)/" + mod + "/image.sproto.h",
		"$(S)/maps/libs/sproto/include/sproto.h",
	} {
		if !nodeHasInput(attrCC, want) {
			t.Fatalf("attribution.pb.cc.o inputs missing %q: %#v", want, attrCC.flatInputs())
		}
	}

	responseCC := mustNodeByOutput(t, g, "$(B)/"+mod+"/response.pb.cc.o")
	for _, want := range []string{
		"$(B)/" + mod + "/attribution.sproto.h",
		"$(B)/" + mod + "/image.sproto.h",
		"$(S)/maps/libs/sproto/include/sproto.h",
	} {
		if !nodeHasInput(responseCC, want) {
			t.Fatalf("response.pb.cc.o inputs missing %q: %#v", want, responseCC.flatInputs())
		}
	}
}

// TestGen_YmapsSproto_InducesTargetSprotoPeerArchive pins the .PEERDIR of the
// sproto-header macro: a YMAPS_SPROTO module peers the target-side sproto library,
// so its non-PIC archive reaches a program link separate from the PIC host path.
func TestGen_YmapsSproto_InducesTargetSprotoPeerArchive(t *testing.T) {
	mod := "maps/doc/proto/yandex/maps/proto/common2"
	files := ymapsSprotoFixtureFiles(t, mod, true)

	writeTestModuleFile(files, "app/ya.make", "PROGRAM()\nPEERDIR("+mod+")\nSRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	// Both variants of the sproto member object exist: non-PIC via the induced
	// target peer, PIC via the host tool.
	mustNodeByOutput(t, g, "$(B)/maps/libs/sproto/sproto.cpp.o")
	mustNodeByOutput(t, g, "$(B)/maps/libs/sproto/sproto.cpp.pic.o")

	// Two AR nodes share the archive output: one for the non-PIC member, one PIC.
	const archive = "$(B)/maps/libs/sproto/libmaps-libs-sproto.a"
	var hasNonPIC, hasPIC bool
	for _, n := range g.Graph {
		if len(n.Outputs) == 0 || n.Outputs[0].string() != archive {
			continue
		}
		if nodeHasInput(n, "$(B)/maps/libs/sproto/sproto.cpp.o") {
			hasNonPIC = true
		}
		if nodeHasInput(n, "$(B)/maps/libs/sproto/sproto.cpp.pic.o") {
			hasPIC = true
		}
	}
	if !hasNonPIC {
		t.Fatalf("no AR node archives the non-PIC sproto.cpp.o into %s", archive)
	}
	if !hasPIC {
		t.Fatalf("no AR node archives the PIC sproto.cpp.pic.o into %s", archive)
	}

	// The consumer link lists the induced sproto archive before the proto library's.
	var ldNode *Node
	for _, n := range g.Graph {
		if n.KV.P == pkLD {
			ldNode = n
			break
		}
	}
	if ldNode == nil {
		t.Fatal("no LD node found in graph")
	}
	linkArgs := ldNode.Cmds[2].CmdArgs.flat()
	sprotoIdx := indexOfArg(linkArgs, "maps/libs/sproto/libmaps-libs-sproto.a")
	protoIdx := indexOfArg(linkArgs, mod+"/libmaps-proto-common2.a")
	if sprotoIdx < 0 {
		t.Fatalf("program link missing maps/libs/sproto/libmaps-libs-sproto.a: %v", linkArgs)
	}
	if protoIdx < 0 {
		t.Fatalf("program link missing %s/libmaps-proto-common2.a: %v", mod, linkArgs)
	}
	if sprotoIdx > protoIdx {
		t.Fatalf("maps/libs/sproto archive [%d] appears after the proto library archive [%d]; want before", sprotoIdx, protoIdx)
	}
}

// TestGen_YmapsSproto_NegativeGuard_NoSprotoPeerWithoutMacro asserts that
// EXPORT_YMAPS_PROTO() without YMAPS_SPROTO(...) does not induce the sproto target
// peer — the peer rides only on the sproto-header macro.
func TestGen_YmapsSproto_NegativeGuard_NoSprotoPeerWithoutMacro(t *testing.T) {
	mod := "maps/doc/proto/yandex/maps/proto/common2"
	files := ymapsSprotoFixtureFiles(t, mod, false)

	writeTestModuleFile(files, "app/ya.make", "PROGRAM()\nPEERDIR("+mod+")\nSRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, "app/main.cpp", "int main(){return 0;}\n")

	g := testGen(newMemFS(files), "app")

	if n := nodeByOutput(g, "$(B)/maps/libs/sproto/sproto.cpp.o"); n != nil {
		t.Fatalf("non-PIC sproto.cpp.o emitted without YMAPS_SPROTO; maps/libs/sproto must not be peered")
	}
	if n := nodeByOutput(g, "$(B)/maps/libs/sproto/libmaps-libs-sproto.a"); n != nil {
		t.Fatalf("libmaps-libs-sproto.a emitted without YMAPS_SPROTO; maps/libs/sproto must not be peered")
	}
}

// ymapsSprotoFixtureFiles builds a proto module fixture: a PROTO_LIBRARY (with
// YMAPS_SPROTO when withSproto), a sproto library, a host sprotoc PROGRAM, and the
// protobuf/protoc support modules.
func ymapsSprotoFixtureFiles(t *testing.T, mod string, withSproto bool) map[string]string {
	t.Helper()

	files := map[string]string{}

	sprotoStmt := ""
	if withSproto {
		sprotoStmt = "YMAPS_SPROTO(\n    image.proto\n)\n"
	}
	writeTestModuleFile(files, mod+"/ya.make", "PROTO_LIBRARY()\nEXPORT_YMAPS_PROTO()\nSRCS(\n    image.proto\n)\n"+sprotoStmt+"EXCLUDE_TAGS(GO_PROTO)\nEND()\n")
	writeTestModuleFile(files, mod+"/image.proto", "syntax = \"proto2\";\npackage yandex.maps.proto.common2.image;\nmessage Image {}\n")

	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")

	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make", "LIBRARY()\nSRCS(protobuf.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/protobuf.cpp", "int protobuf(){return 0;}\n")

	writeTestModuleFile(files, "contrib/libs/protoc/ya.make", "LIBRARY()\nSRCS(protoc.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protoc/protoc.cpp", "int protoc(){return 0;}\n")

	writeTestModuleFile(files, "maps/libs/sproto/ya.make", "LIBRARY()\nSRCS(sproto.cpp)\nEND()\n")
	writeTestModuleFile(files, "maps/libs/sproto/sproto.cpp", "int sproto(){return 0;}\n")
	writeTestModuleFile(files, "maps/libs/sproto/include/sproto.h",
		"#pragma once\n#include <maps/libs/sproto/include/msgbase.h>\n")
	writeTestModuleFile(files, "maps/libs/sproto/include/msgbase.h", "#pragma once\n")

	writeTestModuleFile(files, "maps/libs/sproto/sprotoc/ya.make", "PROGRAM()\nPEERDIR(\n    contrib/libs/protoc\n    maps/libs/sproto\n)\nINDUCED_DEPS(\n    h+cpp\n    ${ARCADIA_ROOT}/maps/libs/sproto/include/sproto.h\n)\nSRCS(main.cpp)\nEND()\n")
	writeTestModuleFile(files, "maps/libs/sproto/sprotoc/main.cpp", "int main(){return 0;}\n")

	return files
}
