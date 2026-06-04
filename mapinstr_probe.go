package main

import (
	"fmt"
	"os"
	"sort"
)

// mapinstr probe — populated by `ay refac mapinstr` wrapping each map key in
// mapKR/mapKW. This file is excluded from instrumentation (its own counter map
// must not recurse). Throwaway.

type mapProbeEntry struct {
	reads  uint64
	writes uint64
}

var mapProbeCounts = map[string]*mapProbeEntry{}

func mapProbeAt(site string, write bool) {
	e := mapProbeCounts[site]

	if e == nil {
		e = &mapProbeEntry{}
		mapProbeCounts[site] = e
	}

	if write {
		e.writes++
	} else {
		e.reads++
	}
}

func mapKR[K any](k K, site string) K {
	if perfStatsEnabled {
		mapProbeAt(site, false)
	}

	return k
}

func mapKW[K any](k K, site string) K {
	if perfStatsEnabled {
		mapProbeAt(site, true)
	}

	return k
}

func reportMapProbe() {
	type row struct {
		site   string
		reads  uint64
		writes uint64
	}
	rows := make([]row, 0, len(mapProbeCounts))

	for s, e := range mapProbeCounts {
		rows = append(rows, row{s, e.reads, e.writes})
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].reads+rows[i].writes > rows[j].reads+rows[j].writes })

	for _, r := range rows {
		fmt.Fprintf(os.Stderr, "mapop\t%d\t%d\t%s\n", r.reads, r.writes, r.site)
	}
}
