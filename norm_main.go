//go:build aynorm

package main

import (
	"fmt"
	"os"
)

const normUsage = `usage:
  ay_norm normalize --in <graph.json|resource.indexed> [--out <file>] [--streaming|--auto-streaming] [--ref-graph]
      the closure of the graph's result roots as canonical JSON lines (unsorted)
  ay_norm sort [--in <file>] [--out <file>] [--chunk-bytes N]
      external sort of lines, so two normalized graphs merge-compare
`

func main() {
	os.Exit(normMain(os.Args[1:]))
}

func normMain(args []string) (code int) {
	defer func() {
		if rec := recover(); rec != nil {
			failure, ok := rec.(normFailure)

			if !ok {
				panic(rec)
			}

			fmt.Fprintf(os.Stderr, "ay_norm: %v\n", failure.err)

			code = 1
		}
	}()

	if len(args) > 0 {
		switch args[0] {
		case "normalize":
			return normCmdNormalize(args[1:])
		case "sort":
			return normCmdSort(args[1:])
		}
	}

	fmt.Fprint(os.Stderr, normUsage)

	return 2
}
