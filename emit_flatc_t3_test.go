package main

import "testing"

func TestEmitFL_NodeShape(t *testing.T) {
	target := newTestPlatform(OSLinux, ISAX8664, "no", nil)
	instance := ModuleInstance{
		Path:     "mod",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := NewBufferedEmitter()
	_, header, cpp, bfbs := EmitFL(
		instance,
		"mod/File.fbs",
		Source("mod/File.fbs"),
		NodeRef{id: 9},
		Build("contrib/libs/flatbuffers/flatc/flatc"),
		[]string{"--scoped-enums"},
		[]VFS{Source("mod/Schema.fbs")},
		e,
	)

	if header.String() != "$(B)/mod/File.fbs.h" {
		t.Fatalf("header = %q", header)
	}
	if cpp.String() != "$(B)/mod/File.fbs.cpp" {
		t.Fatalf("cpp = %q", cpp)
	}
	if bfbs.String() != "$(B)/mod/File.bfbs" {
		t.Fatalf("bfbs = %q", bfbs)
	}
	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	node := e.nodes[0]
	if node.KV["p"] != "FL" {
		t.Fatalf("kv.p = %q, want FL", node.KV["p"])
	}
	if got := node.Cmds[0].CmdArgs; !contains(got, "--scoped-enums") {
		t.Fatalf("cmd args missing --scoped-enums: %v", got)
	}
	if got := node.Cmds[0].CmdArgs; got[len(got)-3] != "-o" || got[len(got)-2] != "$(B)/mod/File.fbs.h" || got[len(got)-1] != "$(S)/mod/File.fbs" {
		t.Fatalf("unexpected cmd arg tail: %v", got[len(got)-5:])
	}
	if len(node.DepRefs) != 1 || node.DepRefs[0].id != 9 {
		t.Fatalf("DepRefs = %#v, want flatc dep", node.DepRefs)
	}
	if got := len(node.ForeignDepRefs["tool"]); got != 1 {
		t.Fatalf("foreign tool deps = %d, want 1", got)
	}
}
