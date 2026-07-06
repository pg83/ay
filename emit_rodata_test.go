package main

import "testing"

func TestEmitRD_NodeShape(t *testing.T) {
	target := newTestPlatform(OSLinux, ISAX8664, "no")
	instance := ModuleInstance{
		Path:     source("contrib/libs/icu"),
		Kind:     KindLib,
		Language: LangCPP,
		Platform: target,
	}

	e := newStreamingEmitter(nil)
	_, asmOut, objOut := emitRD(instance, "icudt78_dat.rodata", intern("$(S)/contrib/libs/icu/icudt78_dat.rodata"), NodeRef(7), Closure{}, nil, testToolchain(), e)

	if asmOut.string() != "$(B)/contrib/libs/icu/icudt78_dat.rodata.asm" {
		t.Fatalf("asmOut = %q", asmOut)
	}

	if objOut.string() != "$(B)/contrib/libs/icu/icudt78_dat.rodata.o" {
		t.Fatalf("objOut = %q", objOut)
	}

	if e.nodes.len() != 1 {
		t.Fatalf("emitted %d nodes, want 1", e.nodes.len())
	}

	node := e.nodes.s[0]

	if node.KV.P != pkRD {
		t.Fatalf("kv.p = %q, want RD", node.KV.P)
	}

	if len(node.Cmds) != 2 {
		t.Fatalf("len(Cmds) = %d, want 2", len(node.Cmds))
	}

	if len(node.Outputs) != 2 {
		t.Fatalf("len(Outputs) = %d, want 2", len(node.Outputs))
	}

	if len(node.ForeignDepRefs) != 1 || node.ForeignDepRefs[0] != 7 {
		t.Fatalf("ForeignDepRefs = %#v, want yasm dep", node.ForeignDepRefs)
	}
}
