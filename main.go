package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
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

// cmdInspect loads a reference g.json and prints a one-line summary
// (node count, result count, sorted platform list). Per the PR-01-D05
// deferred constraint, we use ContinueOnError so subcommand parse
// failures route through this function's throw-style error path rather
// than calling os.Exit from inside flag itself. Argument errors and IO
// errors throw; the panic propagates to main()'s top-level Catch which
// prints to stderr and exits 1. The 0 return is the success-only path.
//
// fs.SetOutput(io.Discard) suppresses flag's built-in error/usage output
// so that all diagnostics are owned exclusively by this function and the
// outer Catch — preventing the duplicate-output bug (PR-03-D02). The
// flag.ErrHelp sentinel is handled explicitly before Throw so that -h /
// --help exits 0 with usage on stdout (PR-03-D01).
func cmdInspect(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	err := fs.Parse(args)

	if errors.Is(err, flag.ErrHelp) {
		printInspectUsage(os.Stdout)

		return 0
	}

	Throw(err)

	if fs.NArg() != 1 {
		ThrowFmt("inspect: expected exactly 1 positional arg (path to g.json), got %d", fs.NArg())
	}

	g := LoadReference(fs.Arg(0))

	// Collect distinct platforms via a set, then sort the keys (D14:
	// never range a map for output).
	platSet := make(map[string]struct{})
	for _, n := range g.Graph {
		platSet[n.Platform] = struct{}{}
	}

	platforms := make([]string, 0, len(platSet))
	for p := range platSet {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)

	fmt.Printf("nodes: %d  results: %d  platforms: %s\n", len(g.Graph), len(g.Result), strings.Join(platforms, ","))

	return 0
}

func printInspectUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: yatool inspect <path-to-g.json>
Print stats about a graph file: nodes, results, platforms.
`)
}
