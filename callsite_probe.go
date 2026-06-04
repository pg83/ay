package main

import (
	"fmt"
	"os"
)

// callsite probe — populated by `ay refac callsite` wrapping each top-level func
// with recordCall. dumpCalls flushes the called set (append) to $CALLSITE_OUT on
// cmd exit; union across the gate's runs, diff vs callsites_all.txt to find
// reachable-but-never-exercised (gate-garbage) functions. Throwaway.

var callCounts = map[string]int{}

// callRecording gates recordCall so only the single-threaded `make -G` gen path
// records. Other handlers (notably `dump`, whose streamGraphFanout runs many
// goroutines) would otherwise concurrently write callCounts and crash the
// runtime with "concurrent map writes". cmdMake sets it true.
var callRecording = false

func recordCall(site string) {
	if !callRecording {
		return
	}

	callCounts[site]++
}

func dumpCalls() {
	path := os.Getenv("CALLSITE_OUT")
	if path == "" {
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}

	for s := range callCounts {
		fmt.Fprintln(f, s)
	}

	f.Close()
}
