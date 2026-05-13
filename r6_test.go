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
		Inputs:           ToVFSSlice([]string{}),
		KV:               map[string]string{"p": "LD"},
		Outputs:          ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
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
	r6Ref, outPath := EmitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, "$(B)/contrib/tools/ragel6/ragel6", nil, nil, e)

	wantOut := "$(B)/util/_/datetime/parser.rl6.cpp"
	if outPath != wantOut {
		t.Errorf("outPath = %q, want %q", outPath, wantOut)
	}

	got := e.nodes[r6Ref.id]

	// Verify cmd_args shape (7 args).
	if len(got.Cmds[0].CmdArgs) != 7 {
		t.Errorf("cmd_args length = %d, want 7", len(got.Cmds[0].CmdArgs))
	}

	wantCmd := []string{
		"$(B)/contrib/tools/ragel6/ragel6",
		"-CT0",
		"-L",
		"-I$(S)",
		"-o",
		wantOut,
		"$(S)/util/datetime/parser.rl6",
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

	// PR-M3-rl6-host-platform-and-cctype: target-side (aarch64) R6 nodes
	// keep tags empty — the "tool" tag is reserved for host-side
	// (x86_64) codegen invocations.
	if len(got.Tags) != 0 {
		t.Errorf("tags = %v, want [] (aarch64 R6 is target-side)", got.Tags)
	}

	// PR-L4-C/07: ragel6 host LD edge lives in both DepRefs (for the L0
	// topology fingerprint) AND ForeignDepRefs["tool"] (matching REF's
	// foreign_deps shape for the R6 aarch64 node).
	if len(got.DepRefs) != 1 {
		t.Fatalf("DepRefs len = %d, want 1", len(got.DepRefs))
	}

	if got.DepRefs[0] != ragel6LD {
		t.Errorf("DepRefs[0] = %v, want %v", got.DepRefs[0], ragel6LD)
	}

	if len(got.ForeignDepRefs) != 1 {
		t.Errorf("ForeignDepRefs len = %d, want 1 (PR-L4-C/07 restores foreign_deps[tool])", len(got.ForeignDepRefs))
	} else if len(got.ForeignDepRefs["tool"]) != 1 || got.ForeignDepRefs["tool"][0] != ragel6LD {
		t.Errorf("ForeignDepRefs[tool] = %v, want [%v]", got.ForeignDepRefs["tool"], ragel6LD)
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
		Inputs:  ToVFSSlice([]string{}),
		KV:      map[string]string{"p": "LD"},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/bin/ragel6"}),
	})

	// Caller threads the (non-canonical) host LD output. EmitR6 must
	// canonicalise to the reference shape.
	r6Ref, _ := EmitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, "$(B)/contrib/tools/ragel6/bin/ragel6", nil, nil, e)

	got := e.nodes[r6Ref.id]

	wantCmd0 := "$(B)/contrib/tools/ragel6/ragel6"

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
			in:   "$(B)/contrib/tools/ragel6/ragel6",
			want: "$(B)/contrib/tools/ragel6/ragel6",
		},
		// Bin-subpath — rewrite to parent.
		{
			in:   "$(B)/contrib/tools/ragel6/bin/ragel6",
			want: "$(B)/contrib/tools/ragel6/ragel6",
		},
		// Arbitrary unrelated path — unchanged.
		{
			in:   "$(B)/contrib/tools/yasm/yasm",
			want: "$(B)/contrib/tools/yasm/yasm",
		},
		// Empty — unchanged.
		{
			in:   "",
			want: "",
		},
		// Different basename under ragel6/bin/ (defensive — future-proof
		// against a hypothetical second binary).
		{
			in:   "$(B)/contrib/tools/ragel6/bin/other",
			want: "$(B)/contrib/tools/ragel6/other",
		},
	}

	for _, c := range cases {
		got := canonicalizeRagel6BinaryPath(c.in)
		if got != c.want {
			t.Errorf("canonicalizeRagel6BinaryPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestEmitR6_ModuleSetOverridesDefault_PR_M3_ragel_flags pins the
// per-module `SET(RAGEL6_FLAGS <value>)` path. The override REPLACES
// the platform-default `-CT0` / `-CG2` slot at cmd_args[1] — upstream
// `_SRC("rl6", ...)` at build/ymake.core.conf:3284 interpolates
// `$RAGEL6_FLAGS` before everything else and `SET` does not
// concatenate. Empirical M3 witness: devtools/ymake/lang/makelists/
// ya.make:6 sets `-lF1`, producing the cmd_args[1] observed in the
// reference graph's `makefile_lang.rl6.cpp` node.
func TestEmitR6_ModuleSetOverridesDefault_PR_M3_ragel_flags(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: []string{"link"}, Env: map[string]string{}}},
		Env:     map[string]string{},
		Inputs:  ToVFSSlice([]string{}),
		KV:      map[string]string{"p": "LD"},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	r6Ref, _ := EmitR6(
		targetInstance("devtools/ymake/lang/makelists"),
		"makefile_lang.rl6",
		ragel6LD,
		"$(B)/contrib/tools/ragel6/ragel6",
		[]string{"-lF1"},
		nil,
		e,
	)

	got := e.nodes[r6Ref.id]

	if got.Cmds[0].CmdArgs[1] != "-lF1" {
		t.Errorf("cmd_args[1] = %q, want -lF1 (per-module SET(RAGEL6_FLAGS) override)", got.Cmds[0].CmdArgs[1])
	}

	// The default flag must NOT also appear — SET replaces, does not
	// append (cmd_args length is the same as the default-flag case).
	for i, a := range got.Cmds[0].CmdArgs {
		if a == "-CT0" || a == "-CG2" {
			t.Errorf("cmd_args[%d] = %q — default flag leaked through the SET override", i, a)
		}
	}

	if len(got.Cmds[0].CmdArgs) != 7 {
		t.Errorf("cmd_args length = %d, want 7 (SET(RAGEL6_FLAGS -lF1) is a 1-arg replacement, not an append)", len(got.Cmds[0].CmdArgs))
	}
}

// TestEmitR6_X8664HostDefault_PR_M3_ragel_flags pins the
// platform-default branch: a host (x86_64) build with no per-module
// SET picks `-CG2` (release toolchain), not `-CT0`. Mirrors upstream
// `build/ymake_conf.py:2271-2277` where `set_default_flags(optimized=
// True)` appends `-CG2`. Empirical M3 witness: the only x86_64 R6
// node in devtools/ymake/bin's closure — util/_/datetime/parser.rl6.cpp
// — has cmd_args[1]=`-CG2` in the reference graph.
func TestEmitR6_X8664HostDefault_PR_M3_ragel_flags(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: []string{"link"}, Env: map[string]string{}}},
		Env:     map[string]string{},
		Inputs:  ToVFSSlice([]string{}),
		KV:      map[string]string{"p": "LD"},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	r6Ref, _ := EmitR6(
		hostInstance("util"),
		"datetime/parser.rl6",
		ragel6LD,
		"$(B)/contrib/tools/ragel6/ragel6",
		nil,
		nil,
		e,
	)

	got := e.nodes[r6Ref.id]

	if got.Cmds[0].CmdArgs[1] != "-CG2" {
		t.Errorf("cmd_args[1] = %q, want -CG2 (x86_64 host = release = optimized)", got.Cmds[0].CmdArgs[1])
	}

	// PR-M3-rl6-host-platform-and-cctype: x86_64 R6 nodes carry
	// `host_platform=true` and `tags=["tool"]` matching the reference
	// graph's classification of host-side codegen invocations.
	if !got.HostPlatform {
		t.Errorf("host_platform = false, want true (x86_64 R6 is host-side)")
	}

	if len(got.Tags) != 1 || got.Tags[0] != "tool" {
		t.Errorf("tags = %v, want [tool]", got.Tags)
	}
}

// TestEmitR6_InputsIncludeBinarySourceAndClosure_PR35z pins the R6
// inputs shape post-PR-35z. The reference R6 node carries
// `[ragel6BinaryPath, .rl6 source, ...transitive header closure]`
// — the binary the node invokes plus the source plus its scanned
// `#include` closure (1009 inputs for util/datetime/parser.rl6 in
// the M2 closure). This test exercises the structural shape on a
// 2-element synthetic closure rather than reading util/datetime
// off-disk; integration coverage of the real M2 case lives in the
// gen → compare pipeline.
func TestEmitR6_InputsIncludeBinarySourceAndClosure_PR35z(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: []string{"link"}, Env: map[string]string{}}},
		Env:     map[string]string{},
		Inputs:  ToVFSSlice([]string{}),
		KV:      map[string]string{"p": "LD"},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	closure := []VFS{
		Source("util/datetime/parser.h"),
		Source("util/generic/ymath.h"),
	}

	r6Ref, _ := EmitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, "$(B)/contrib/tools/ragel6/ragel6", nil, closure, e)

	got := e.nodes[r6Ref.id]

	wantInputs := []string{
		"$(B)/contrib/tools/ragel6/ragel6",
		"$(S)/util/datetime/parser.rl6",
		"$(S)/util/datetime/parser.h",
		"$(S)/util/generic/ymath.h",
	}

	if len(got.Inputs) != len(wantInputs) {
		t.Fatalf("R6 inputs len = %d, want %d (got=%v)", len(got.Inputs), len(wantInputs), got.Inputs)
	}

	for i, w := range wantInputs {
		if got.Inputs[i].String() != w {
			t.Errorf("R6 inputs[%d] = %q, want %q", i, got.Inputs[i].String(), w)
		}
	}
}
