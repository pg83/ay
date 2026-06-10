package main

import "testing"

func TestEmitRD_NodeShape(t *testing.T) {
	target := newTestPlatform(OSLinux, ISAX8664, "no", nil)
	instance := ModuleInstance{
		Path:     Source("contrib/libs/icu"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := NewBufferedEmitter()
	_, asmOut, objOut := EmitRD(instance, "icudt78_dat.rodata", Intern("$(S)/contrib/libs/icu/icudt78_dat.rodata"), NodeRef(7), testToolchain(), e)

	if asmOut.String() != "$(B)/contrib/libs/icu/icudt78_dat.rodata.asm" {
		t.Fatalf("asmOut = %q", asmOut)
	}
	if objOut.String() != "$(B)/contrib/libs/icu/icudt78_dat.rodata.o" {
		t.Fatalf("objOut = %q", objOut)
	}
	if len(e.nodes) != 1 {
		t.Fatalf("emitted %d nodes, want 1", len(e.nodes))
	}

	node := e.nodes[0]
	if node.KV.P != pkRD {
		t.Fatalf("kv.p = %q, want RD", node.KV.P)
	}
	if len(node.Cmds) != 2 {
		t.Fatalf("len(Cmds) = %d, want 2", len(node.Cmds))
	}
	if len(node.Outputs) != 2 {
		t.Fatalf("len(Outputs) = %d, want 2", len(node.Outputs))
	}
	if len(node.DepRefs) != 1 || node.DepRefs[0] != 7 {
		t.Fatalf("DepRefs = %#v, want yasm dep", node.DepRefs)
	}
	if got := len(node.ForeignDepRefs); got != 1 {
		t.Fatalf("foreign tool deps = %d, want 1", got)
	}
}
