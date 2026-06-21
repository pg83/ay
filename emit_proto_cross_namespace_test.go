package main

import (
	"testing"
)

// crossNamespaceProtoFixture reproduces the T-120 (sg7) divergence
// represented by $(B)/ads/bsyeti/libs/scatter/client.cpp.o: a generated
// <proto>.pb.h that DIRECTLY imports a proto from ANOTHER PROTO_NAMESPACE does
// not re-export that import's generated .pb.h to downstream CC consumers.
//
// Shape (mirrors yp data_model -> yt_proto orm object/controls/finalizers):
//   - leaf PROTO_LIBRARY in PROTO_NAMESPACE(lns): leaf_a.proto, leaf_b.proto;
//     leaf_a.proto imports its same-namespace sibling leaf_b.proto.
//   - top PROTO_LIBRARY in PROTO_NAMESPACE(tns): top.proto imports the
//     CROSS-namespace leaf_a.proto ("leaf/leaf_a.proto", rooted at lns).
//   - app LIBRARY: app.cpp -> app.h -> <top/top.pb.h>.
//
// Upstream: top.pb.h #includes "leaf/leaf_a.pb.h" verbatim and ymake resolves it
// against leaf's GLOBAL PROTO_NAMESPACE(lns) addincl to
// $(B)/lns/leaf/leaf_a.pb.h; leaf_a.pb.h re-includes leaf_b.pb.h. So a unit that
// includes top.pb.h reaches BOTH generated leaf headers.
//
// Before the fix protoDirectPbHIncludes roots the import header by prefixing the
// IMPORTER's PROTO_NAMESPACE (tns), producing the non-existent
// tns/leaf/leaf_a.pb.h: the directive never binds, so neither leaf_a.pb.h nor
// (through it) leaf_b.pb.h reaches app.cpp.o.
func crossNamespaceProtoFixture() FS {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nADDINCL(GLOBAL contrib/libs/protobuf/src)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")

	// leaf PROTO_LIBRARY, namespace lns. leaf_a imports same-namespace leaf_b.
	writeTestModuleFile(files, "lns/leaf/ya.make",
		"PROTO_LIBRARY()\nPROTO_NAMESPACE(lns)\nSRCS(leaf_a.proto leaf_b.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "lns/leaf/leaf_a.proto",
		"syntax = \"proto3\";\npackage lns;\nimport \"leaf/leaf_b.proto\";\nmessage LeafA { LeafB b = 1; }\n")
	writeTestModuleFile(files, "lns/leaf/leaf_b.proto",
		"syntax = \"proto3\";\npackage lns;\nmessage LeafB { string v = 1; }\n")

	// top PROTO_LIBRARY, namespace tns. top imports the cross-namespace leaf_a.
	writeTestModuleFile(files, "tns/top/ya.make",
		"PROTO_LIBRARY()\nPROTO_NAMESPACE(tns)\nPEERDIR(lns/leaf)\nSRCS(top.proto)\nEXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\nEND()\n")
	writeTestModuleFile(files, "tns/top/top.proto",
		"syntax = \"proto3\";\npackage tns;\nimport \"leaf/leaf_a.proto\";\nmessage Top { lns.LeafA a = 1; }\n")

	// app LIBRARY: reaches top.pb.h through a source-header chain.
	writeTestModuleFile(files, "app/ya.make",
		"LIBRARY()\nPEERDIR(tns/top)\nSRCS(app.cpp)\nEND()\n")
	writeTestModuleFile(files, "app/app.cpp", "#include \"app.h\"\nint app(){return 0;}\n")
	writeTestModuleFile(files, "app/app.h", "#pragma once\n#include <top/top.pb.h>\n")

	return newMemFS(files)
}

// TestEmitProtoSrcs_CrossNamespaceDirectImportPbHRidesIntoConsumer pins that the
// cross-namespace direct-import generated headers reach an ordinary CC consumer.
func TestEmitProtoSrcs_CrossNamespaceDirectImportPbHRidesIntoConsumer(t *testing.T) {
	g := testGen(crossNamespaceProtoFixture(), "app")
	appCC := mustNodeByOutput(t, g, "$(B)/app/app.cpp.o")

	for _, want := range []string{
		"$(B)/tns/top/top.pb.h",
		"$(B)/lns/leaf/leaf_a.pb.h",
		"$(B)/lns/leaf/leaf_b.pb.h",
	} {
		if !nodeHasInput(appCC, want) {
			t.Errorf("app.cpp.o missing cross-namespace generated header %q\ninputs=%v", want, vfsStrings(appCC.flatInputs()))
		}
	}
}
