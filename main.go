package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "help", "-h", "--help":
		os.Exit(cmdHelp(os.Args[2:]))
	case "gen":
		os.Exit(cmdGen(os.Args[2:]))
	case "compare":
		os.Exit(cmdCompare(os.Args[2:]))
	case "inspect":
		os.Exit(cmdInspect(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
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
