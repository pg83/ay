package main

import (
	"os"
	"testing"
)

// compare_cmd_test.go — exercises the L3 byte-exact cmds + env comparator.
//
// Test-graph helpers (mkNode, mk2NodeGraph, cloneGraph, cloneNode) live
// in compare_props_test.go and are reused here — both files compile in
// `package main`. L3-specific tests vary Cmds and Env; L0/L1/L2 are
// asserted to remain at 1.0 to confirm L3 is the only level that drops.

func TestCompareL3_IdentityIsPerfect(t *testing.T) {
	g, _, _ := mk2NodeGraph()
	r := Compare(g, g, 3)

	if r.L0 != 1.0 {
		t.Errorf("identity L0 = %v, want 1.0 (note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("identity L1 = %v, want 1.0 (note: %q)", r.L1, r.L1Note)
	}

	if r.L2 != 1.0 {
		t.Errorf("identity L2 = %v, want 1.0 (note: %q)", r.L2, r.L2Note)
	}

	if r.L3 != 1.0 {
		t.Errorf("identity L3 = %v, want 1.0 (note: %q)", r.L3, r.L3Note)
	}

	if r.L3Note == "" {
		t.Errorf("identity L3Note is empty; want a one-line summary")
	}

	if len(r.Skipped) != 0 {
		t.Errorf("Compare(g, g, 3).Skipped = %v, want empty (L3 is implemented now)", r.Skipped)
	}
}

func TestCompareL3_DifferentCmdArgsDropsBelow1(t *testing.T) {
	// Same shape, same outputs (so pairing works), same kv.p (so L0
	// stays 1.0), same target_properties / inputs / tags / requirements
	// (so L1+L2 stay 1.0). The only difference is one extra argv
	// element on one node's first cmd — only L3 should drop.
	want, _, _ := mk2NodeGraph()
	want.Graph[0].Cmds = []Cmd{{CmdArgs: []string{"clang", "-c", "a.c"}, Env: map[string]string{}}}
	want.Graph[1].Cmds = []Cmd{{CmdArgs: []string{"clang", "-c", "b.c"}, Env: map[string]string{}}}

	got := cloneGraph(want)
	got.Graph[0].Cmds[0].CmdArgs = []string{"clang", "-c", "-O2", "a.c"}

	r := Compare(want, got, 3)

	if r.L0 != 1.0 {
		t.Errorf("flipped-cmdargs L0 = %v, want 1.0 (cmds are not in the fingerprint; note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("flipped-cmdargs L1 = %v, want 1.0 (cmds are L3-only; note: %q)", r.L1, r.L1Note)
	}

	if r.L2 != 1.0 {
		t.Errorf("flipped-cmdargs L2 = %v, want 1.0 (cmds are L3-only; note: %q)", r.L2, r.L2Note)
	}

	if r.L3 >= 1.0 {
		t.Errorf("flipped-cmdargs L3 = %v, want < 1.0 (note: %q)", r.L3, r.L3Note)
	}
}

func TestCompareL3_DifferentEnvDropsBelow1(t *testing.T) {
	// Same shape, same outputs, same cmds — only the top-level node Env
	// differs in one key. L0/L1/L2 stay 1.0; L3 drops.
	want, _, _ := mk2NodeGraph()
	want.Graph[0].Cmds = []Cmd{{CmdArgs: []string{"clang", "-c", "a.c"}, Env: map[string]string{}}}
	want.Graph[1].Cmds = []Cmd{{CmdArgs: []string{"clang", "-c", "b.c"}, Env: map[string]string{}}}

	got := cloneGraph(want)
	got.Graph[0].Env["LANG"] = "C.UTF-8"

	r := Compare(want, got, 3)

	if r.L0 != 1.0 {
		t.Errorf("flipped-env L0 = %v, want 1.0 (env is not in the fingerprint; note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("flipped-env L1 = %v, want 1.0 (env is L3-only; note: %q)", r.L1, r.L1Note)
	}

	if r.L2 != 1.0 {
		t.Errorf("flipped-env L2 = %v, want 1.0 (env is L3-only; note: %q)", r.L2, r.L2Note)
	}

	if r.L3 >= 1.0 {
		t.Errorf("flipped-env L3 = %v, want < 1.0 (note: %q)", r.L3, r.L3Note)
	}
}

func TestCompareL3_DifferentCmdEnvDropsBelow1(t *testing.T) {
	// Same shape, same outputs, same top-level node Env, same CmdArgs —
	// only a per-cmd Env entry differs on one node. L0/L1/L2 stay 1.0;
	// L3 drops because per-cmd Env is a L3 field.
	//
	// This test relies on cloneNode deep-copying Cmds[i].Env so the
	// mutation of got.Graph[0].Cmds[0].Env does not alias back into want.
	want, _, _ := mk2NodeGraph()
	want.Graph[0].Cmds = []Cmd{{CmdArgs: []string{"clang", "-c", "a.c"}, Env: map[string]string{}}}
	want.Graph[1].Cmds = []Cmd{{CmdArgs: []string{"clang", "-c", "b.c"}, Env: map[string]string{}}}

	got := cloneGraph(want)
	got.Graph[0].Cmds[0].Env["X"] = "y"

	r := Compare(want, got, 3)

	if r.L0 != 1.0 {
		t.Errorf("L0 should be 1.0, got %v", r.L0)
	}

	if r.L1 != 1.0 {
		t.Errorf("L1 should be 1.0, got %v", r.L1)
	}

	if r.L2 != 1.0 {
		t.Errorf("L2 should be 1.0, got %v", r.L2)
	}

	if r.L3 >= 1.0 {
		t.Errorf("L3 should be < 1.0, got %v", r.L3)
	}
}

func TestCompareL3_DifferentCmdsLengthDropsBelow1(t *testing.T) {
	// One side has 1 cmd on the first node, the other has 2. Length
	// mismatch is the cheapest L3-fail signal — confirm it actually
	// counts as a mismatch and doesn't crash on the per-index loop.
	want, _, _ := mk2NodeGraph()
	want.Graph[0].Cmds = []Cmd{{CmdArgs: []string{"clang", "-c", "a.c"}, Env: map[string]string{}}}
	want.Graph[1].Cmds = []Cmd{{CmdArgs: []string{"clang", "-c", "b.c"}, Env: map[string]string{}}}

	got := cloneGraph(want)
	got.Graph[0].Cmds = []Cmd{
		{CmdArgs: []string{"clang", "-c", "a.c"}, Env: map[string]string{}},
		{CmdArgs: []string{"strip", "a.o"}, Env: map[string]string{}},
	}

	r := Compare(want, got, 3)

	if r.L3 >= 1.0 {
		t.Errorf("differing-cmds-length L3 = %v, want < 1.0 (note: %q)", r.L3, r.L3Note)
	}
}

func TestStringMapEqual_NilVsEmpty(t *testing.T) {
	if !stringMapEqual(nil, map[string]string{}) {
		t.Error("stringMapEqual(nil, empty) should be true")
	}

	if !stringMapEqual(map[string]string{}, nil) {
		t.Error("stringMapEqual(empty, nil) should be true")
	}

	if !stringMapEqual(nil, nil) {
		t.Error("stringMapEqual(nil, nil) should be true")
	}

	if stringMapEqual(map[string]string{"a": "1"}, map[string]string{"a": "2"}) {
		t.Error("different values should be unequal")
	}

	if stringMapEqual(map[string]string{"a": "1"}, map[string]string{"b": "1"}) {
		t.Error("different keys should be unequal")
	}

	if stringMapEqual(map[string]string{"x": ""}, map[string]string{"y": ""}) {
		t.Error("same-size maps with different keys (empty values) should be unequal")
	}
}

func TestCompareL3_RealGraphSelfMatch(t *testing.T) {
	// Load-bearing acceptance: comparing the real reference graph to
	// itself MUST report L3 == 1.0 exactly. If this drops below 1.0
	// the L3 comparator is broken for the only "trusted" input we have.
	if _, err := os.Stat(referenceGraphPath); err != nil {
		t.Skipf("reference graph %s not present: %v", referenceGraphPath, err)
	}

	g := LoadReference(referenceGraphPath)
	r := Compare(g, g, 3)

	if r.L0 != 1.0 {
		t.Errorf("real-graph self-match L0 = %v, want 1.0 (note: %q)", r.L0, r.L0Note)
	}

	if r.L1 != 1.0 {
		t.Errorf("real-graph self-match L1 = %v, want 1.0 (note: %q)", r.L1, r.L1Note)
	}

	if r.L2 != 1.0 {
		t.Errorf("real-graph self-match L2 = %v, want 1.0 (note: %q)", r.L2, r.L2Note)
	}

	if r.L3 != 1.0 {
		t.Errorf("real-graph self-match L3 = %v, want 1.0 (note: %q)", r.L3, r.L3Note)
	}
}
