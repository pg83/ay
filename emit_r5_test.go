package main

import "testing"

func TestEmitR5_RlgenModeFollowsOptimized(t *testing.T) {
	releaseFlags := make(map[string]string, len(testToolchainFlags)+2)

	for k, v := range testToolchainFlags {
		releaseFlags[k] = v
	}

	releaseFlags["PIC"] = "yes"
	releaseFlags["GG_BUILD_TYPE"] = "release"
	releaseHost := newPlatform(newMemFS(nil), OSLinux, ISAX8664, releaseFlags, "", "")

	if !releaseHost.RagelOptimized {
		t.Fatalf("release host platform: RagelOptimized = false, want true")
	}

	if testTargetP.RagelOptimized {
		t.Fatalf("default target platform: RagelOptimized = true, want false (debug)")
	}

	rlgenMode := func(p *Platform) string {
		e := newStreamingEmitter(nil, nil)
		inst := ModuleInstance{Path: source("kernel/urlnorm"), Kind: KindLib, Language: LangCPP, Platform: p}
		ref := e.reserve()
		tmpOut, cppOut := ragel5OutPaths(inst, "urlhashval.rl")
		emitR5(inst, "urlhashval.rl",
			0, 0, intern("$(B)/contrib/tools/ragel5/ragel/ragel5"),
			intern("$(B)/contrib/tools/ragel5/rlgen-cd/rlgen-cd"), nil, ref, e)

		if tmpOut.string() != "$(B)/kernel/urlnorm/urlhashval.rl.tmp" {
			t.Errorf("tmpOut = %q, want $(B)/kernel/urlnorm/urlhashval.rl.tmp", tmpOut.string())
		}

		if cppOut.string() != "$(B)/kernel/urlnorm/urlhashval.rl5.cpp" {
			t.Errorf("cppOut = %q, want $(B)/kernel/urlnorm/urlhashval.rl5.cpp", cppOut.string())
		}

		flat := e.nodes[ref].Cmds[1].CmdArgs.flat()

		return flat[1].string()
	}

	if got := rlgenMode(releaseHost); got != "-G2" {
		t.Errorf("optimized contour rlgen mode = %q, want -G2", got)
	}

	if got := rlgenMode(testTargetP); got != "-T0" {
		t.Errorf("debug contour rlgen mode = %q, want -T0", got)
	}
}

func TestGen_Ragel5NodeIncludesGeneratedOutputClosure(t *testing.T) {
	files := map[string]string{}
	writeToolProgram(files, "contrib/tools/ragel5/ragel", "ragel")
	writeToolProgram(files, "contrib/tools/ragel5/rlgen-cd", "rlgen-cd")

	writeTestModuleFile(files, "mod/ya.make", `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(lexer.rl)
END()
`)
	writeTestModuleFile(files, "mod/lexer.rl", `#include "helper.h"
%%{
    machine x;
    main := 'a';
}%%
`)
	writeTestModuleFile(files, "mod/helper.h", "#pragma once\n")

	g := testGen(newMemFS(files), "mod")

	r5 := mustNodeByOutput(t, g, "$(B)/mod/lexer.rl.tmp")

	if !nodeHasInput(r5, "$(S)/mod/helper.h") {
		t.Fatalf("ragel5 node inputs missing generated-output header %q: %#v", "$(S)/mod/helper.h", r5.flatInputs())
	}
}
