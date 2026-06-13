package main

import (
	"testing"
)

func TestEmitR6_RagelHostRecursion_Synthetic(t *testing.T) {
	e := newBufferedEmitter()

	ragel6LD := e.emit(&Node{
		Cmds:             []Cmd{{CmdArgs: ArgChunks{appendInternStrs(nil, []string{"link"})}, Env: nil}},
		Env:              nil,
		Inputs:           InputChunks{ToVFSSlice([]string{})},
		KV:               KV{P: pkLD},
		Outputs:          ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
		Platform:         &Platform{Target: "default-linux-x86_64"},
		Requirements:     Requirements{},
		Tags:             []STR{internStr("tool")},
		TargetProperties: TargetProperties{ModuleDir: "contrib/tools/ragel6"},
	})

	r6Ref, outPath := emitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, intern("$(B)/contrib/tools/ragel6/ragel6"), nil, nil, e)

	wantOut := "$(B)/util/_/datetime/parser.rl6.cpp"
	if outPath.string() != wantOut {
		t.Errorf("outPath = %q, want %q", outPath.string(), wantOut)
	}

	got := e.nodes[r6Ref]

	if len(got.Cmds[0].CmdArgs.flat()) != 7 {
		t.Errorf("cmd_args length = %d, want 7", len(got.Cmds[0].CmdArgs.flat()))
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
		if got.Cmds[0].CmdArgs.flat()[i].string() != w {
			t.Errorf("cmd_args[%d] = %q, want %q", i, got.Cmds[0].CmdArgs.flat()[i].string(), w)
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

	if got.ForeignDepRefs[0] != ragel6LD {
		t.Errorf("ForeignDepRefs[0] = %v, want %v", got.ForeignDepRefs[0], ragel6LD)
	}

	if len(got.ForeignDepRefs) != 1 {
		t.Errorf("ForeignDepRefs len = %d, want 1 (PR-L4-C/07 restores foreign_deps[tool])", len(got.ForeignDepRefs))
	} else if len(got.ForeignDepRefs) != 1 || got.ForeignDepRefs[0] != ragel6LD {
		t.Errorf("ForeignDepRefs[tool] = %v, want [%v]", got.ForeignDepRefs, ragel6LD)
	}

	if got.Requirements.Network != nwRestricted {
		t.Errorf("requirements.network = %v, want restricted", got.Requirements.Network.string())
	}
}

func TestEmitR6_ModuleSetOverridesDefault_PR_M3_ragel_flags(t *testing.T) {
	e := newBufferedEmitter()

	ragel6LD := e.emit(&Node{Platform: &Platform{},
		Cmds:    []Cmd{{CmdArgs: ArgChunks{appendInternStrs(nil, []string{"link"})}, Env: nil}},
		Env:     nil,
		Inputs:  InputChunks{ToVFSSlice([]string{})},
		KV:      KV{P: pkLD},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	r6Ref, _ := emitR6(
		targetInstance("devtools/ymake/lang/makelists"),
		"makefile_lang.rl6",
		ragel6LD,
		intern("$(B)/contrib/tools/ragel6/ragel6"),
		internArgs([]string{"-lF1"}),
		nil,
		e,
	)

	got := e.nodes[r6Ref]

	if got.Cmds[0].CmdArgs.flat()[1].string() != "-lF1" {
		t.Errorf("cmd_args[1] = %q, want -lF1 (per-module SET(RAGEL6_FLAGS) override)", got.Cmds[0].CmdArgs.flat()[1].string())
	}

	for i, a := range got.Cmds[0].CmdArgs.flat() {
		if a.string() == "-CT0" || a.string() == "-CG2" {
			t.Errorf("cmd_args[%d] = %q — default flag leaked through the SET override", i, a.string())
		}
	}

	if len(got.Cmds[0].CmdArgs.flat()) != 7 {
		t.Errorf("cmd_args length = %d, want 7 (SET(RAGEL6_FLAGS -lF1) is a 1-arg replacement, not an append)", len(got.Cmds[0].CmdArgs.flat()))
	}
}

func TestEmitR6_X8664HostDefault_PR_M3_ragel_flags(t *testing.T) {
	e := newBufferedEmitter()

	ragel6LD := e.emit(&Node{Platform: &Platform{},
		Cmds:    []Cmd{{CmdArgs: ArgChunks{appendInternStrs(nil, []string{"link"})}, Env: nil}},
		Env:     nil,
		Inputs:  InputChunks{ToVFSSlice([]string{})},
		KV:      KV{P: pkLD},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	releaseHostFlags := map[string]string{}
	for k, v := range testToolchainFlags {
		releaseHostFlags[k] = v
	}
	releaseHostFlags["PIC"] = "yes"
	releaseHostFlags["GG_BUILD_TYPE"] = "release"
	releaseHost := newPlatform(newMemFS(nil), OSLinux, ISAX8664, releaseHostFlags, []string{"tool"}, "", "")

	r6Ref, _ := emitR6(
		ModuleInstance{
			Path:     source("util"),
			Kind:     KindLib,
			Language: LangCPP,
			Platform: releaseHost,
		},
		"datetime/parser.rl6",
		ragel6LD,
		intern("$(B)/contrib/tools/ragel6/ragel6"),
		nil,
		nil,
		e,
	)

	got := e.nodes[r6Ref]

	if got.Cmds[0].CmdArgs.flat()[1].string() != "-CG2" {
		t.Errorf("cmd_args[1] = %q, want -CG2 (x86_64 host = release = optimized)", got.Cmds[0].CmdArgs.flat()[1].string())
	}

	if !nodeHasHostTag(nodeTags(got)) {
		t.Errorf("tags do not carry \"tool\"; want host_platform-equivalent. tags=%v", nodeTags(got))
	}

	if tg := nodeTags(got); len(tg) != 1 || tg[0] != internStr("tool") {
		t.Errorf("tags = %v, want [tool]", nodeTags(got))
	}
}

func TestEmitR6_InputsIncludeBinarySourceAndClosure_PR35z(t *testing.T) {
	e := newBufferedEmitter()

	ragel6LD := e.emit(&Node{Platform: &Platform{},
		Cmds:    []Cmd{{CmdArgs: ArgChunks{appendInternStrs(nil, []string{"link"})}, Env: nil}},
		Env:     nil,
		Inputs:  InputChunks{ToVFSSlice([]string{})},
		KV:      KV{P: pkLD},
		Outputs: ToVFSSlice([]string{"$(B)/contrib/tools/ragel6/ragel6"}),
	})

	// The closure is the rl6 source's window — the source itself leads it.
	closure := []VFS{
		intern("$(S)/util/datetime/parser.rl6"),
		intern("$(S)/util/datetime/parser.h"),
		intern("$(S)/util/generic/ymath.h"),
	}

	r6Ref, _ := emitR6(targetInstance("util"), "datetime/parser.rl6", ragel6LD, intern("$(B)/contrib/tools/ragel6/ragel6"), nil, closure, e)

	got := e.nodes[r6Ref]

	wantInputs := []string{
		"$(B)/contrib/tools/ragel6/ragel6",
		"$(S)/util/datetime/parser.rl6",
		"$(S)/util/datetime/parser.h",
		"$(S)/util/generic/ymath.h",
	}

	if len(got.flatInputs()) != len(wantInputs) {
		t.Fatalf("R6 inputs len = %d, want %d (got=%v)", len(got.flatInputs()), len(wantInputs), got.flatInputs())
	}

	for i, w := range wantInputs {
		if got.flatInputs()[i].string() != w {
			t.Errorf("R6 inputs[%d] = %q, want %q", i, got.flatInputs()[i].string(), w)
		}
	}
}
