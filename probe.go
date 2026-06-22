package main

// Throwaway instrumentation that rewrites the package's own source in place to
// measure runtime behaviour. Run in a worktree, build, measure, revert.

// dumpProbes flushes each requested probe's tally after the handler returns.
// Counters always tally; only the dump is gated, by the --probe=X flags.
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
