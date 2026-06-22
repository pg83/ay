package main

import "testing"

// TestEmitR5_RlgenModeFollowsOptimized pins the upstream Ragel.set_default_flags
// mechanism (ymake_conf.py): the rlgen-cd mode is -G2 under an optimized
// (release, non-sanitized) toolchain and -T0 otherwise — the SAME `optimized`
// boolean R6 reads for -CG2/-CT0. A module reachable in both the host (release)
// and target (debug) contours therefore yields two R5 producers with identical
// outputs whose commands differ ONLY in the rlgen mode. Reproduces the T-153
// sg7 ref-only duplicate R5 producer for $(B)/kernel/urlnorm/urlhashval.rl{.tmp,5.cpp}.
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
		e := newBufferedEmitter()
		inst := ModuleInstance{Path: source("kernel/urlnorm"), Kind: KindLib, Language: LangCPP, Platform: p}
		ref, tmpOut, cppOut := emitR5(inst, "urlhashval.rl",
			0, 0, intern("$(B)/contrib/tools/ragel5/ragel/ragel5"),
			intern("$(B)/contrib/tools/ragel5/rlgen-cd/rlgen-cd"), e)

		if tmpOut.string() != "$(B)/kernel/urlnorm/urlhashval.rl.tmp" {
			t.Errorf("tmpOut = %q, want $(B)/kernel/urlnorm/urlhashval.rl.tmp", tmpOut.string())
		}
		if cppOut.string() != "$(B)/kernel/urlnorm/urlhashval.rl5.cpp" {
			t.Errorf("cppOut = %q, want $(B)/kernel/urlnorm/urlhashval.rl5.cpp", cppOut.string())
		}

		// The rlgen-cd command is Cmds[1]; its flag arg sits right after the binary.
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
