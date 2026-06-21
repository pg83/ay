package main

import (
	"testing"
)

// protoNsOrderFixture reproduces the YT/proto-namespace peer ADDINCL ordering
// shape (yt/yt/client/formats): a consumer PEERDIRs a plain LIBRARY `mid` whose
// only namespace declaration is a bare `PROTO_NAMESPACE(mid)`. `mid` PEERDIRs a
// sub-module that exports a build-root subdir include `$(B)/mid/sub` GLOBAL, and
// (later in declaration order) a deeper module that also exports `$(B)/mid`
// GLOBAL.
//
// Upstream `PROTO_NAMESPACE` always expands to `ADDINCL(GLOBAL $(B)/mid)` (the
// literal GLOBAL in proto.conf's PROTO_ADDINCL call), so `mid` itself contributes
// `$(B)/mid` as a UserGlobal dir and it renders first — before the rpc_proxy-like
// `$(B)/mid/sub` carried in the peers' GlobalPropagated. The deeper `$(B)/mid`
// exporter is deduped to that earlier position.
func protoNsOrderFixture() FS {
	files := map[string]string{}

	// mid: plain LIBRARY, bare PROTO_NAMESPACE(mid). Peers the sub exporter first,
	// then the deep exporter (so without the fix `$(B)/mid` only arrives last, via
	// `deep`).
	writeTestModuleFile(files, "mid/ya.make",
		"LIBRARY()\nPROTO_NAMESPACE(mid)\nPEERDIR(mid/sub deep)\nSRCS(m.cpp)\nEND()\n")
	writeTestModuleFile(files, "mid/m.cpp", "int m(){return 0;}\n")

	// sub exporter: the rpc_proxy analog — a GLOBAL build-root subdir include.
	writeTestModuleFile(files, "mid/sub/ya.make",
		"LIBRARY()\nADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/mid/sub)\nSRCS(s.cpp)\nEND()\n")
	writeTestModuleFile(files, "mid/sub/s.cpp", "int s(){return 0;}\n")

	// deep exporter: also provides $(B)/mid GLOBAL, but is reached after mid/sub.
	writeTestModuleFile(files, "deep/ya.make",
		"LIBRARY()\nADDINCL(GLOBAL ${ARCADIA_BUILD_ROOT}/mid)\nSRCS(d.cpp)\nEND()\n")
	writeTestModuleFile(files, "deep/d.cpp", "int d(){return 0;}\n")

	// consumer: ordinary C++ unit that peers mid.
	writeTestModuleFile(files, "consumer/ya.make",
		"LIBRARY()\nPEERDIR(mid)\nSRCS(c.cpp)\nEND()\n")
	writeTestModuleFile(files, "consumer/c.cpp", "int c(){return 0;}\n")

	return newMemFS(files)
}

// TestGen_BareProtoNamespace_BuildRootIncludeIsGlobalAndOrderedFirst pins the
// T-143 divergence: a bare PROTO_NAMESPACE's `$(B)/<ns>` C++ include must be
// GLOBAL (so it reaches consumers via the declaring peer's own propagation) and
// must render before a peer-propagated build-root subdir include. Before the fix
// the build-root arm is gated on GLOBAL/PROTO_LIBRARY, so `$(B)/mid` arrives only
// via the late `deep` exporter and renders after `$(B)/mid/sub`.
func TestGen_BareProtoNamespace_BuildRootIncludeIsGlobalAndOrderedFirst(t *testing.T) {
	g := testGen(protoNsOrderFixture(), "consumer")

	n := mustNodeByOutput(t, g, "$(B)/consumer/c.cpp.o")
	args := n.Cmds[0].CmdArgs.flat()

	iNs := indexOfArg(args, "-I$(B)/mid")
	iSub := indexOfArg(args, "-I$(B)/mid/sub")

	if iNs < 0 {
		t.Fatalf("consumer compile missing -I$(B)/mid\nargs=%v", strStrs(args))
	}
	if iSub < 0 {
		t.Fatalf("consumer compile missing -I$(B)/mid/sub\nargs=%v", strStrs(args))
	}
	if iNs > iSub {
		t.Fatalf("-I$(B)/mid (idx %d) must precede -I$(B)/mid/sub (idx %d)\nargs=%v",
			iNs, iSub, strStrs(args))
	}
}
