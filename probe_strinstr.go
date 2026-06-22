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

func strProbeAt() {
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
