package main

import (
	"testing"
)

func TestEmitR6_RagelHostRecursion_Synthetic(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: appendInternStrs(nil, []string{"link"}), Env: nil}},
		Env:              nil,
		Inputs:           ToVFSSlice([]string{}),
		KV:               KV{P: pkLD},
		Outputs:          ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
		Platform:         &Platform{Target: "default-linux-x86_64"},
		Requirements:     Requirements{},
		Tags:             []string{"tool"},
		TargetProperties: TargetProperties{ModuleDir: "contrib/tools/ragel6"},
	})

	r6Ref, outPath := EmitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, Intern("$(B)/contrib/tools/ragel6/ragel6"), nil, nil, e)

	wantOut := "$(B)/util/_/datetime/parser.rl6.cpp"
	if outPath.String() != wantOut {
		t.Errorf("outPath = %q, want %q", outPath.String(), wantOut)
	}

	got := e.nodes[r6Ref]

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
		if got.Cmds[0].CmdArgs[i].String() != w {
			t.Errorf("cmd_args[%d] = %q, want %q", i, got.Cmds[0].CmdArgs[i].String(), w)
		}
	}

	if got.KV.P != pkR6 {
		t.Errorf("kv.p = %q, want R6", got.KV.P)
	}

	if got.KV.PC != pcYellow {
		t.Errorf("kv.pc = %q, want yellow", got.KV.PC)
	}

	if platformTarget(got.Platform) != string(PlatformDefaultLinuxAArch64) {
		t.Errorf("platform = %q, want %q", platformTarget(got.Platform), PlatformDefaultLinuxAArch64)
	}

	if nodeHasHostTag(got.Tags) {
		t.Errorf("tags carry \"tool\" → host_platform = true, want false; tags=%v", got.Tags)
	}

	if len(got.Tags) != 0 {
		t.Errorf("tags = %v, want [] (aarch64 R6 is target-side)", got.Tags)
	}

	if len(got.DepRefs) != 1 {
		t.Fatalf("DepRefs len = %d, want 1", len(got.DepRefs))
	}

	if got.DepRefs[0] != ragel6LD {
		t.Errorf("DepRefs[0] = %v, want %v", got.DepRefs[0], ragel6LD)
	}

	if len(got.ForeignDepRefs) != 1 {
		t.Errorf("ForeignDepRefs len = %d, want 1 (PR-L4-C/07 restores foreign_deps[tool])", len(got.ForeignDepRefs))
	} else if len(got.ForeignDepRefs) != 1 || got.ForeignDepRefs[0] != ragel6LD {
		t.Errorf("ForeignDepRefs[tool] = %v, want [%v]", got.ForeignDepRefs, ragel6LD)
	}

	if got.Requirements.Network != "restricted" {
		t.Errorf("requirements.network = %v, want restricted", got.Requirements.Network)
	}
}

func TestEmitR6_CanonicalizesBinPath_PR35j(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: appendInternStrs(nil, []string{"link"}), Env: nil}},
		Env:     nil,
		Inputs:  ToVFSSlice([]string{}),
		KV:      KV{P: pkLD},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/bin/ragel6"}),
	})

	r6Ref, _ := EmitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, Intern("$(B)/contrib/tools/ragel6/bin/ragel6"), nil, nil, e)

	got := e.nodes[r6Ref]

	wantCmd0 := "$(B)/contrib/tools/ragel6/ragel6"

	if len(got.Cmds) == 0 || len(got.Cmds[0].CmdArgs) == 0 {
		t.Fatalf("R6 node has no Cmds[0].CmdArgs; got Cmds=%v", got.Cmds)
	}

	if got.Cmds[0].CmdArgs[0].String() != wantCmd0 {
		t.Errorf("R6 cmd_args[0] = %q, want %q (PR-35j: /bin/ stripped to match reference)",
			got.Cmds[0].CmdArgs[0].String(), wantCmd0)
	}
}

func TestCanonicalizeRagel6BinaryPath_PassThrough(t *testing.T) {
	cases := []struct {
		in, want string
	}{

		{
			in:   "$(B)/contrib/tools/ragel6/ragel6",
			want: "$(B)/contrib/tools/ragel6/ragel6",
		},

		{
			in:   "$(B)/contrib/tools/ragel6/bin/ragel6",
			want: "$(B)/contrib/tools/ragel6/ragel6",
		},

		{
			in:   "$(B)/contrib/tools/yasm/yasm",
			want: "$(B)/contrib/tools/yasm/yasm",
		},

		{
			in:   "$(B)/contrib/tools/ragel6/bin/other",
			want: "$(B)/contrib/tools/ragel6/other",
		},
	}

	for _, c := range cases {
		if !vfsHasPrefix(c.in) {
			t.Fatalf("Intern(%q): not a VFS token", c.in)
		}
		got := canonicalizeRagel6Binary(Intern(c.in)).String()
		if got != c.want {
			t.Errorf("canonicalizeRagel6Binary(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEmitR6_ModuleSetOverridesDefault_PR_M3_ragel_flags(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: appendInternStrs(nil, []string{"link"}), Env: nil}},
		Env:     nil,
		Inputs:  ToVFSSlice([]string{}),
		KV:      KV{P: pkLD},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	r6Ref, _ := EmitR6(
		targetInstance("devtools/ymake/lang/makelists"),
		"makefile_lang.rl6",
		ragel6LD,
		Intern("$(B)/contrib/tools/ragel6/ragel6"),
		internArgs([]string{"-lF1"}),
		nil,
		e,
	)

	got := e.nodes[r6Ref]

	if got.Cmds[0].CmdArgs[1].String() != "-lF1" {
		t.Errorf("cmd_args[1] = %q, want -lF1 (per-module SET(RAGEL6_FLAGS) override)", got.Cmds[0].CmdArgs[1].String())
	}

	for i, a := range got.Cmds[0].CmdArgs {
		if a.String() == "-CT0" || a.String() == "-CG2" {
			t.Errorf("cmd_args[%d] = %q — default flag leaked through the SET override", i, a.String())
		}
	}

	if len(got.Cmds[0].CmdArgs) != 7 {
		t.Errorf("cmd_args length = %d, want 7 (SET(RAGEL6_FLAGS -lF1) is a 1-arg replacement, not an append)", len(got.Cmds[0].CmdArgs))
	}
}

func TestEmitR6_X8664HostDefault_PR_M3_ragel_flags(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: appendInternStrs(nil, []string{"link"}), Env: nil}},
		Env:     nil,
		Inputs:  ToVFSSlice([]string{}),
		KV:      KV{P: pkLD},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	releaseHostFlags := map[string]string{}
	for k, v := range testToolchainFlags {
		releaseHostFlags[k] = v
	}
	releaseHostFlags["PIC"] = "yes"
	releaseHostFlags["GG_BUILD_TYPE"] = "release"
	releaseHost := NewPlatform(newMemFS(nil), OSLinux, ISAX8664, releaseHostFlags, []string{"tool"}, "", "", nil)

	r6Ref, _ := EmitR6(
		ModuleInstance{
			Path:     "util",
			Kind:     KindLib,
			Language: LangCPP,
			Platform: releaseHost,
		},
		"datetime/parser.rl6",
		ragel6LD,
		Intern("$(B)/contrib/tools/ragel6/ragel6"),
		nil,
		nil,
		e,
	)

	got := e.nodes[r6Ref]

	if got.Cmds[0].CmdArgs[1].String() != "-CG2" {
		t.Errorf("cmd_args[1] = %q, want -CG2 (x86_64 host = release = optimized)", got.Cmds[0].CmdArgs[1].String())
	}

	if !nodeHasHostTag(got.Tags) {
		t.Errorf("tags do not carry \"tool\"; want host_platform-equivalent. tags=%v", got.Tags)
	}

	if len(got.Tags) != 1 || got.Tags[0] != "tool" {
		t.Errorf("tags = %v, want [tool]", got.Tags)
	}
}

func TestEmitR6_InputsIncludeBinarySourceAndClosure_PR35z(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{
		Cmds:    []Cmd{{CmdArgs: appendInternStrs(nil, []string{"link"}), Env: nil}},
		Env:     nil,
		Inputs:  ToVFSSlice([]string{}),
		KV:      KV{P: pkLD},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	closure := []VFS{
		Intern("$(S)/util/datetime/parser.h"),
		Intern("$(S)/util/generic/ymath.h"),
	}

	r6Ref, _ := EmitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, Intern("$(B)/contrib/tools/ragel6/ragel6"), nil, closure, e)

	got := e.nodes[r6Ref]

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
