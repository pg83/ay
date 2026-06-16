package main

// The throwaway instrumentation tools rewrite the package's own source in place
// to measure runtime behaviour: `probe mapinstr` (per-site map-op counters) and
// `probe callsite` (per-function reachability). Run in a worktree, build,
// measure, revert. The transforms and their runtime helpers live in
// probe_mapinstr.go and probe_callsite.go.

// dumpProbes flushes each requested probe's tally after the handler returns and
// before exit (cmds return rather than os.Exit, so this always runs on the
// success path). The instrumented counters always tally; only the dump is gated,
// by the --probe=X flags carried on main's stack. Names are validated at parse.
func dumpProbes(probes []string) {
	for _, p := range probes {
		switch p {
		case "map":
			reportMapProbe()
		case "str":
			reportStrProbe()
		}
	}
}
