package main

import "testing"

// TestGen_ProtoLibrary_YmapsSprotoEmitsHeadersAndFeedsGeneratedCCInputs models a
// maps/doc/proto PROTO_LIBRARY with EXPORT_YMAPS_PROTO + YMAPS_SPROTO: it must
// emit a .sproto.h PB/yellow producer per listed proto (run through
// maps/libs/sproto/sprotoc), make the maps/libs/sproto/sprotoc tool + library
// reachable, and thread the generated .sproto.h plus the sprotoc-induced sproto
// runtime header into the generated C++ compile inputs of an importing proto.
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

	// sprotoc's PROGRAM peer; maps/libs/sproto is its runtime library.
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

	// 3. The importing proto's generated C++ unit sees the generated .sproto.h
	// (PROTO_HEADER_EXTS .pb.h .sproto.h) and — through the sprotoc GeneratorRef
	// induced deps — the sproto runtime header. attribution directly imports
	// image, so attribution.pb.cc.o must see image.sproto.h. response imports
	// attribution (which imports image), so response.pb.cc.o must see the
	// transitive image.sproto.h plus the direct attribution.sproto.h.
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
