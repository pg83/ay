package main

import (
	"testing"
)

// r6_test.go — synthetic test for EmitR6's host-tool wiring (D31).
// PR-23 does not yet drive a real host ragel6 LD recursion; the test
// fabricates a stub LD ref and verifies that EmitR6 wires it via
// `ForeignDepRefs["tool"]` exactly once.

// TestEmitR6_RagelHostRecursion_Synthetic emits a fake host ragel6
// LD node, then calls EmitR6 with the resulting NodeRef. Asserts
// the R6 node's `DepRefs` contains exactly that ref (PR-28 D04 moved
// the edge from `ForeignDepRefs["tool"]` to `DepRefs` to match the
// reference shape: `deps=[ragel6 host LD UID]`, no foreign_deps),
// and that cmd_args/kv/tags/requirements match the reference shape
// observed in /home/pg/monorepo/yatool_orig/sg.json.
func TestEmitR6_RagelHostRecursion_Synthetic(t *testing.T) {
	e := NewBufferedEmitter()

	// Fabricate a stub host ragel6 LD node. PR-25's walker will
	// build this via real recursion into `contrib/tools/ragel6`;
	// PR-23 only proves the wiring works.
	ragel6LD := e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"link"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           []string{},
		KV:               map[string]string{"p": "LD"},
		Outputs:          []string{"$(BUILD_ROOT)/contrib/tools/ragel6/ragel6"},
		Platform:         "default-linux-x86_64",
		HostPlatform:     true,
		Requirements:     map[string]interface{}{},
		Tags:             []string{"tool"},
		TargetProperties: map[string]string{"module_dir": "contrib/tools/ragel6"},
	})

	// Emit the R6 node against the util module's
	// `datetime/parser.rl6` source (matches the only R6 node in
	// the reference graph). The hardcoded ragel6 binary path matches
	// the stub LD's outputs[0] above; PR-28-D01 makes this the
	// caller's responsibility (the gen.go walker derives it from the
	// host LD's own emission).
	r6Ref, outPath := EmitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, "$(BUILD_ROOT)/contrib/tools/ragel6/ragel6", e)

	wantOut := "$(BUILD_ROOT)/util/_/datetime/parser.rl6.cpp"
	if outPath != wantOut {
		t.Errorf("outPath = %q, want %q", outPath, wantOut)
	}

	got := e.nodes[r6Ref.id]

	// Verify cmd_args shape (7 args).
	if len(got.Cmds[0].CmdArgs) != 7 {
		t.Errorf("cmd_args length = %d, want 7", len(got.Cmds[0].CmdArgs))
	}

	wantCmd := []string{
		"$(BUILD_ROOT)/contrib/tools/ragel6/ragel6",
		"-CT0",
		"-L",
		"-I$(SOURCE_ROOT)",
		"-o",
		wantOut,
		"$(SOURCE_ROOT)/util/datetime/parser.rl6",
	}

	for i, w := range wantCmd {
		if got.Cmds[0].CmdArgs[i] != w {
			t.Errorf("cmd_args[%d] = %q, want %q", i, got.Cmds[0].CmdArgs[i], w)
		}
	}

	// kv = {"p": "R6", "pc": "yellow"}.
	if got.KV["p"] != "R6" {
		t.Errorf("kv.p = %q, want R6", got.KV["p"])
	}

	if got.KV["pc"] != "yellow" {
		t.Errorf("kv.pc = %q, want yellow", got.KV["pc"])
	}

	// platform should be the target's (R6 runs on target side; the
	// host dep is just the ragel6 binary used to generate output).
	if got.Platform != string(PlatformDefaultLinuxAArch64) {
		t.Errorf("platform = %q, want %q", got.Platform, PlatformDefaultLinuxAArch64)
	}

	// host_platform is false (R6 is target-side; host dep is via
	// foreign_deps, not host_platform).
	if got.HostPlatform {
		t.Errorf("host_platform = true, want false")
	}

	// PR-28 D04: ragel6 host LD edge lives in DepRefs (not
	// ForeignDepRefs["tool"]) to match the empirical reference shape.
	if len(got.DepRefs) != 1 {
		t.Fatalf("DepRefs len = %d, want 1", len(got.DepRefs))
	}

	if got.DepRefs[0] != ragel6LD {
		t.Errorf("DepRefs[0] = %v, want %v", got.DepRefs[0], ragel6LD)
	}

	if len(got.ForeignDepRefs) != 0 {
		t.Errorf("ForeignDepRefs = %v, want empty (PR-28 D04 dropped placeholder)", got.ForeignDepRefs)
	}

	// requirements must include cpu/network/ram (matching reference).
	if got.Requirements["network"] != "restricted" {
		t.Errorf("requirements.network = %v, want restricted", got.Requirements["network"])
	}
}
