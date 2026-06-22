package main

import (
	_ "embed"
	"fmt"
	"strings"
	"time"
)

// perfDartsData holds the benchmark fixtures: the AUTOINCLUDE_PATHS roots and a
// fixed random sample of 2000 source-tree directories, as two newline-separated
// sections split by a single blank line.
//
//go:embed perf_darts_data.txt
var perfDartsData string

// perfDarts benchmarks the autoinclude longest-prefix matcher two ways over a
// fixed sample of source-tree directories: the byte double-array trie (Darts) vs
// the former ancestor-walk (IntValueMap keyed by interned root VFS, probed with
// internedPrefixed per ancestor). Prints ns/op for each.
func perfDarts() int {
	sections := strings.SplitN(strings.TrimRight(perfDartsData, "\n"), "\n\n", 2)

	if len(sections) != 2 {
		fmt.Println("perf darts: malformed perf_darts_data.txt (want roots, blank line, dirs)")

		return 2
	}

	roots := strings.Split(sections[0], "\n")
	dirs := strings.Split(sections[1], "\n")

	// Darts over "<root>/" keys.
	keys := make([]string, len(roots))

	for i, r := range roots {
		keys[i] = r + "/"
	}

	darts := NewDarts(keys)

	// Old matcher: IntValueMap keyed by the interned root VFS; lookup walks the
	// dir's ancestors deepest-first, probing the intern table (a full-path hash
	// per ancestor) until a root hits.
	old := newIntValueMap[int32](len(roots) * 2)

	for i, r := range roots {
		old.put(uint64(source(r)), int32(i))
	}

	oldLookup := func(dir string) (int, bool) {
		for d := dir; ; {
			if st := internedPrefixed("$(S)/", d); st != 0 {
				if v := old.get(uint64(st.vfs())); v != nil {
					return int(*v), true
				}
			}

			i := strings.LastIndexByte(d, '/')

			if i < 0 {
				return 0, false
			}

			d = d[:i]
		}
	}

	// Sanity: both matchers must agree before timing means anything.
	mismatch := 0

	for _, d := range dirs {
		di, dok := darts.longestMatch(d, "/")
		oi, ook := oldLookup(d)

		if dok != ook || (dok && di != oi) {
			mismatch++
		}
	}

	fmt.Printf("roots=%d queries=%d darts-vs-old mismatches=%d\n", len(roots), len(dirs), mismatch)

	const iters = 2000
	ops := int64(iters) * int64(len(dirs))
	sink := 0

	t0 := time.Now()

	for it := 0; it < iters; it++ {
		for _, d := range dirs {
			if i, ok := darts.longestMatch(d, "/"); ok {
				sink += i
			}
		}
	}

	dartsDur := time.Since(t0)

	t1 := time.Now()

	for it := 0; it < iters; it++ {
		for _, d := range dirs {
			if i, ok := oldLookup(d); ok {
				sink += i
			}
		}
	}

	oldDur := time.Since(t1)

	fmt.Printf("darts: %.1f ns/op (%v total)\n", float64(dartsDur.Nanoseconds())/float64(ops), dartsDur)
	fmt.Printf("old:   %.1f ns/op (%v total)\n", float64(oldDur.Nanoseconds())/float64(ops), oldDur)
	fmt.Printf("sink=%d\n", sink)

	return 0
}
