package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
)

var (
	strProbeEnabled bool
	strProbeCounts  = map[uintptr]uint64{}
)

// --- runtime probe over STR.string(): unlike the map probe (which needs an
// AST rewrite — a map index has no hook point), .string() is a function, so
// the hook lives inside it and resolves the use site from the runtime call
// stack. Enabled by the --probe=str global flag; a disabled probe costs one
// predictable branch on a package bool. Sites tally by caller PC (cheap);
// PC -> file:line resolution happens once at dump time. ---

func strProbeAt() {
	// Skip strProbeAt and string() itself; runtime.Caller is inline-aware,
	// so the logical frame is the .string() use site.
	pc, _, _, ok := runtime.Caller(2)

	if ok {
		strProbeCounts[pc]++
	}
}

func reportStrProbe() {
	type row struct {
		pc    uintptr
		count uint64
	}

	rows := make([]row, 0, len(strProbeCounts))

	for pc, c := range strProbeCounts {
		rows = append(rows, row{pc, c})
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].count > rows[j].count })

	for _, r := range rows {
		frames := runtime.CallersFrames([]uintptr{r.pc})
		f, _ := frames.Next()
		fmt.Fprintf(os.Stderr, "strop\t%d\t%s:%d\t%s\n", r.count, f.File, f.Line, f.Function)
	}
}
