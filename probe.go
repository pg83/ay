package main

// Throwaway instrumentation that rewrites the package's own source in place to
// measure runtime behaviour: `probe mapinstr` (per-site map-op counters) and
// `probe callsite` (per-function reachability). Run in a worktree, build,
// measure, revert.

// dumpProbes flushes each requested probe's tally after the handler returns,
// before exit (cmds return rather than os.Exit). Counters always tally; only the
// dump is gated, by the --probe=X flags. Names are validated at parse.
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
