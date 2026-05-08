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

// TestEmitR6_CanonicalizesBinPath_PR35j pins PR-35j: when the caller
// passes a `/contrib/tools/ragel6/bin/ragel6` path (because our host
// walker walks the `bin/` subdirectory rather than expanding the
// upstream `INCLUDE(${ARCADIA_ROOT}/contrib/tools/ragel6/bin/ya.make)`
// at the parent level), EmitR6 rewrites cmd_args[0] to the
// reference-shaped parent path `/contrib/tools/ragel6/ragel6`.
// Reference verification source:
// /home/pg/monorepo/yatool_orig/sg.json — the util R6 node invokes
// ragel6 at the parent path, NOT under bin/.
func TestEmitR6_CanonicalizesBinPath_PR35j(t *testing.T) {
	e := NewBufferedEmitter()

	// Stub host LD whose outputs[0] mirrors what our own walker emits
	// when it walks `contrib/tools/ragel6/bin` directly.
	ragel6LD := e.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: []string{"link"}, Env: map[string]string{}}},
		Env:     map[string]string{},
		Inputs:  []string{},
		KV:      map[string]string{"p": "LD"},
		Outputs: []string{"$(BUILD_ROOT)/contrib/tools/ragel6/bin/ragel6"},
	})

	// Caller threads the (non-canonical) host LD output. EmitR6 must
	// canonicalise to the reference shape.
	r6Ref, _ := EmitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, "$(BUILD_ROOT)/contrib/tools/ragel6/bin/ragel6", e)

	got := e.nodes[r6Ref.id]

	wantCmd0 := "$(BUILD_ROOT)/contrib/tools/ragel6/ragel6"

	if len(got.Cmds) == 0 || len(got.Cmds[0].CmdArgs) == 0 {
		t.Fatalf("R6 node has no Cmds[0].CmdArgs; got Cmds=%v", got.Cmds)
	}

	if got.Cmds[0].CmdArgs[0] != wantCmd0 {
		t.Errorf("R6 cmd_args[0] = %q, want %q (PR-35j: /bin/ stripped to match reference)",
			got.Cmds[0].CmdArgs[0], wantCmd0)
	}
}

// TestCanonicalizeRagel6BinaryPath_PassThrough pins that
// canonicalizeRagel6BinaryPath leaves non-matching inputs unchanged:
// the parse-gap fallback string supplied by gen.go (already canonical)
// must not be double-rewritten, and arbitrary other strings must
// pass through untouched.
func TestCanonicalizeRagel6BinaryPath_PassThrough(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Already canonical — no rewrite.
		{
			in:   "$(BUILD_ROOT)/contrib/tools/ragel6/ragel6",
			want: "$(BUILD_ROOT)/contrib/tools/ragel6/ragel6",
		},
		// Bin-subpath — rewrite to parent.
		{
			in:   "$(BUILD_ROOT)/contrib/tools/ragel6/bin/ragel6",
			want: "$(BUILD_ROOT)/contrib/tools/ragel6/ragel6",
		},
		// Arbitrary unrelated path — unchanged.
		{
			in:   "$(BUILD_ROOT)/contrib/tools/yasm/yasm",
			want: "$(BUILD_ROOT)/contrib/tools/yasm/yasm",
		},
		// Empty — unchanged.
		{
			in:   "",
			want: "",
		},
		// Different basename under ragel6/bin/ (defensive — future-proof
		// against a hypothetical second binary).
		{
			in:   "$(BUILD_ROOT)/contrib/tools/ragel6/bin/other",
			want: "$(BUILD_ROOT)/contrib/tools/ragel6/other",
		},
	}

	for _, c := range cases {
		got := canonicalizeRagel6BinaryPath(c.in)
		if got != c.want {
			t.Errorf("canonicalizeRagel6BinaryPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
