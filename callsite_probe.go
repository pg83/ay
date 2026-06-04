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

func recordCall(site string) { callCounts[site]++ }

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
