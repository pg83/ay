package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	exc := Try(func() {
		dispatch(os.Args)
	})

	exc.Catch(func(e *Exception) {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	})
}

// dispatch parses the command-line and routes to the per-subcommand
// handler. It is the throw-style boundary between main()'s
// Try(...).Catch(...) wrapper and the rest of the program: per
// STYLE.md ("Catches belong at boundaries: main.go: the top-level
// Try(...).Catch(...) prints the error and os.Exit(1)") any panic with
// an *Exception bubbles up here and gets printed to stderr.
//
// Subcommands return an int exit code today (PR-11 stubs). PR-03+
// will replace those bodies with real implementations that throw on
// failure; the contract above keeps working unchanged.
//
// Note: os.Exit from a subcommand bypasses any defers placed by the outer Try;
// only panics propagate. If success-path cleanup needs to fire from Try,
// dispatch must return an exit code instead of calling os.Exit directly.
func dispatch(argv []string) {
	if len(argv) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	switch argv[1] {
	case "help", "-h", "--help":
		os.Exit(cmdHelp(argv[2:]))
	case "gen":
		os.Exit(cmdGen(argv[2:]))
	case "compare":
		os.Exit(cmdCompare(argv[2:]))
	case "inspect":
		os.Exit(cmdInspect(argv[2:]))
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", argv[1])
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `yatool — recreate ymake build-graph generator

Usage:
    yatool <subcommand> [flags]

Subcommands:
    gen        Generate a build graph for a target.
    compare    Compare two graphs (fuzzy, with L0..L3 levels).
    inspect    Print stats about a graph file.
    help       Show this message.
`)
}

func cmdHelp(_ []string) int {
	printUsage(os.Stdout)

	return 0
}

func cmdGen(_ []string) int {
	_ = flag.NewFlagSet("gen", flag.ExitOnError)
	fmt.Fprintln(os.Stderr, "gen: not implemented yet")

	return 1
}

func cmdCompare(_ []string) int {
	_ = flag.NewFlagSet("compare", flag.ExitOnError)
	fmt.Fprintln(os.Stderr, "compare: not implemented yet")

	return 1
}

func cmdInspect(_ []string) int {
	_ = flag.NewFlagSet("inspect", flag.ExitOnError)
	fmt.Fprintln(os.Stderr, "inspect: not implemented yet")

	return 1
}
