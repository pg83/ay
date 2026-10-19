//go:build aycoverage

package main

import (
	"os"
	"runtime/coverage"
)

func flushCoverage() {
	_ = coverage.WriteCountersDir(os.Getenv("GOCOVERDIR"))
}
