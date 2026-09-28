//go:build aycoverage

package main

import (
	"os"
	"runtime/coverage"
)

// flushCoverage writes the counters a crash would otherwise lose. A failed
// write is not reported here: it shows as missing counts in the merged profile.
func flushCoverage() {
	_ = coverage.WriteCountersDir(os.Getenv("GOCOVERDIR"))
}
