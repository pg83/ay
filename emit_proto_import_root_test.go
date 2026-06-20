package main

import (
	"testing"
)

// protoImportRootFixture reproduces the T-91 divergence: a proto import that
// names a fully-qualified arcadia path (`dep/foo.proto`) which exists BOTH at
// the source root ($(S)/dep/foo.proto, the real PROTO_LIBRARY) and under a peer
// PROTO_NAMESPACE addincl that happens to mirror the same subtree
// ($(S)/mirror/dep/foo.proto). protoc resolves the import against its -I list in
// order — `-I=./ -I=$(S)/ -I=$(B) -I=$(S)` (the arcadia roots) precede the peer
// PROTO_NAMESPACE -I — so the source-root copy wins and the mirror copy is never
// consulted. Upstream's TModuleResolver does the same: for a `proto` (a lang in
// LANGS_REQUIRE_BUILD_AND_SRC_ROOTS) Local include it resolves
// MakeResolvePlan(srcDir, BldDir, SrcDir) FIRST and only falls to the module's
// IncDirs (ADDINCL) if that misses (module_resolver.cpp:238-245, 322-352).
func protoImportRootFixture() FS {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/protoc", "protoc")
	writeToolProgram(files, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
	writeTestModuleFile(files, "build/scripts/cpp_proto_wrapper.py", "print('stub')\n")

	writeTestModuleFile(files, "contrib/libs/protobuf/ya.make",
		"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PLATFORM()\n"+
			"ADDINCL(GLOBAL contrib/libs/protobuf/src)\nSRCS(p.cpp)\nEND()\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/p.cpp", "int p(){return 0;}\n")
	writeTestModuleFile(files, "contrib/libs/protobuf/src/google/protobuf/message.h", "#pragma once\n")

	// The real dependency at the source root.
	writeTestModuleFile(files, "dep/ya.make", "PROTO_LIBRARY()\nSRCS(foo.proto)\nEND()\n")
	writeTestModuleFile(files, "dep/foo.proto", "syntax = \"proto3\";\npackage dep;\nmessage Foo {}\n")

	// A peer PROTO_LIBRARY that publishes the `mirror` PROTO_NAMESPACE GLOBAL
	// addincl. Its subtree mirrors dep/ — the mirror copy of foo.proto exists on
	// disk so an addincl-first resolver would wrongly bind the import there.
	writeTestModuleFile(files, "mirror/peer/ya.make",
		"PROTO_LIBRARY()\nPROTO_NAMESPACE(mirror)\nSRCS(bar.proto)\nEND()\n")
	writeTestModuleFile(files, "mirror/peer/bar.proto", "syntax = \"proto3\";\npackage mirror;\nmessage Bar {}\n")
	writeTestModuleFile(files, "mirror/dep/foo.proto", "syntax = \"proto3\";\npackage dep;\nmessage Foo {}\n")

	// The module under test: imports the fully-qualified dep/foo.proto and peers
	// both the real dep and the mirror-namespace peer.
	writeTestModuleFile(files, "main/ya.make",
		"PROTO_LIBRARY()\nPEERDIR(dep mirror/peer)\nSRCS(main.proto)\nEND()\n")
	writeTestModuleFile(files, "main/main.proto",
		"syntax = \"proto3\";\npackage main;\nimport \"dep/foo.proto\";\nmessage Main {}\n")

	return newMemFS(files)
}

// TestGen_ProtoImport_SourceRootWinsOverPeerNamespaceMirror pins that a
// fully-qualified proto import binds to the arcadia source-root copy, not to a
// peer PROTO_NAMESPACE addincl mirror of the same path. Before the fix the
// scanner consults ADDINCL before the arcadia roots, so main.pb.cc carries the
// spurious $(S)/mirror/dep/foo.proto input.
func TestGen_ProtoImport_SourceRootWinsOverPeerNamespaceMirror(t *testing.T) {
	fs := protoImportRootFixture()
	g := testGen(fs, "main")

	pb := mustNodeByOutput(t, g, "$(B)/main/main.pb.h")
	inputs := vfsStrings(pb.flatInputs())

	const (
		want    = "$(S)/dep/foo.proto"
		mirror  = "$(S)/mirror/dep/foo.proto"
	)

	hasWant, hasMirror := false, false
	for _, in := range inputs {
		if in == want {
			hasWant = true
		}
		if in == mirror {
			hasMirror = true
		}
	}

	if hasMirror {
		t.Errorf("main.pb.cc carries the peer-namespace mirror import %q; protoc/upstream bind the import to the source root, not the ADDINCL mirror\ninputs=%v", mirror, inputs)
	}

	if !hasWant {
		t.Errorf("main.pb.cc is missing the source-root import %q\ninputs=%v", want, inputs)
	}
}
