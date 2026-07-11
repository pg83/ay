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
		e := newStreamingEmitter(nil)
		inst := ModuleInstance{Path: source("kernel/urlnorm"), Kind: KindLib, Language: LangCPP, Platform: p}
		ref := e.reserve()

		emitR5Reserved(inst, "urlhashval.rl", source("kernel/urlnorm/urlhashval.rl"),
			0, 0, build("contrib/tools/ragel5/ragel/ragel5"),
			build("contrib/tools/ragel5/rlgen-cd/rlgen-cd"), nil, ref, e)

		flat := e.nodes.s[ref].Cmds[1].CmdArgs.flat()

		return flat[1].string()
	}

	if got := rlgenMode(releaseHost); got != "-G2" {
		t.Errorf("optimized contour rlgen mode = %q, want -G2", got)
	}

	if got := rlgenMode(testTargetP); got != "-T0" {
		t.Errorf("debug contour rlgen mode = %q, want -T0", got)
	}
}
