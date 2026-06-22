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

// --- runtime probe over STR.string(): the hook lives inside the function and
// resolves the use site from the call stack. Enabled by --probe=str; disabled
// it costs one branch. Sites tally by caller PC, resolved to file:line at dump
// time. ---

func strProbeAt() {
	// Frame 2 skips strProbeAt and string() itself to the use site.
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
