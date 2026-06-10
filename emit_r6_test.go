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
		Tags:             []STR{internStr("tool")},
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

	if string(got.Platform.Target) != string(PlatformDefaultLinuxAArch64) {
		t.Errorf("platform = %q, want %q", string(got.Platform.Target), PlatformDefaultLinuxAArch64)
	}

	if nodeHasHostTag(nodeTags(got)) {
		t.Errorf("tags carry \"tool\" → host_platform = true, want false; tags=%v", nodeTags(got))
	}

	if len(nodeTags(got)) != 0 {
		t.Errorf("tags = %v, want [] (aarch64 R6 is target-side)", nodeTags(got))
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

	if got.Requirements.Network != nwRestricted {
		t.Errorf("requirements.network = %v, want restricted", got.Requirements.Network.String())
	}
}



func TestEmitR6_ModuleSetOverridesDefault_PR_M3_ragel_flags(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{Platform: &Platform{},
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

	ragel6LD := e.Emit(&Node{Platform: &Platform{},
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
	releaseHost := NewPlatform(newMemFS(nil), OSLinux, ISAX8664, releaseHostFlags, []string{"tool"}, "", "")

	r6Ref, _ := EmitR6(
		ModuleInstance{
			Path:     Source("util"),
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

	if !nodeHasHostTag(nodeTags(got)) {
		t.Errorf("tags do not carry \"tool\"; want host_platform-equivalent. tags=%v", nodeTags(got))
	}

	if tg := nodeTags(got); len(tg) != 1 || tg[0] != internStr("tool") {
		t.Errorf("tags = %v, want [tool]", nodeTags(got))
	}
}

func TestEmitR6_InputsIncludeBinarySourceAndClosure_PR35z(t *testing.T) {
	e := NewBufferedEmitter()

	ragel6LD := e.Emit(&Node{Platform: &Platform{},
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
