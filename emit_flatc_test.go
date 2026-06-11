package main

import "testing"

func TestEmitFL_NodeShape(t *testing.T) {
	target := newTestPlatform(OSLinux, ISAX8664, "no", nil)
	instance := ModuleInstance{
		Path:     source("mod"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := newBufferedEmitter()
	_, header, cpp, bfbs := emitFL(
		instance,
		"mod/File.fbs",
		intern("$(S)/mod/File.fbs"),
		NodeRef(9),
		intern("$(B)/contrib/libs/flatbuffers/flatc/flatc"),
		internArgs([]string{"--scoped-enums"}),
		[]VFS{intern("$(S)/mod/Schema.fbs")},
		testToolchain(),
		e,
	)

	if header.string() != "$(B)/mod/File.fbs.h" {
		t.Fatalf("header = %q", header)
	}
	if cpp.string() != "$(B)/mod/File.fbs.cpp" {
		t.Fatalf("cpp = %q", cpp)
	}
	if bfbs.string() != "$(B)/mod/File.bfbs" {
		t.Fatalf("bfbs = %q", bfbs)
	}
	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	node := e.nodes[0]
	if node.KV.P != pkFL {
		t.Fatalf("kv.p = %q, want FL", node.KV.P)
	}
	if got := node.Cmds[0].CmdArgs.flat(); !contains(got, "--scoped-enums") {
		t.Fatalf("cmd args missing --scoped-enums: %v", got)
	}
	if got := node.Cmds[0].CmdArgs.flat(); got[len(got)-3].string() != "-o" || got[len(got)-2].string() != "$(B)/mod/File.fbs.h" || got[len(got)-1].string() != "$(S)/mod/File.fbs" {
		t.Fatalf("unexpected cmd arg tail: %v", got[len(got)-5:])
	}
	if len(node.DepRefs) != 1 || node.DepRefs[0] != 9 {
		t.Fatalf("DepRefs = %#v, want flatc dep", node.DepRefs)
	}
	if got := len(node.ForeignDepRefs); got != 1 {
		t.Fatalf("foreign tool deps = %d, want 1", got)
	}
}
