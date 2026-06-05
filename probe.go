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
