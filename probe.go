package main

import (
	"fmt"
	"os"
)

// cmdProbe dispatches the throwaway instrumentation tools that rewrite the
// package's own source in place to measure runtime behaviour: `probe mapinstr`
// (per-site map-op counters) and `probe callsite` (per-function reachability).
// Run in a worktree, build, measure, revert. The transforms and their runtime
// helpers live in probe_mapinstr.go and probe_callsite.go.
func cmdProbe(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ay probe <mapinstr|callsite> [files...]")

		return 2
	}

	switch args[0] {
	case "mapinstr":
		return probeMapInstr(args[1:])
	case "callsite":
		return probeCallSite(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "probe: unknown subcommand %q\n", args[0])

		return 2
	}
}

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
