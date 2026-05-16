package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

func main() {
	exc := Try(func() {
		dispatch(os.Args)
	})

	exc.Catch(func(e *Exception) {
		fatalException(e)
	})
}

// dispatch parses the command-line and routes to the per-subcommand
// handler. It is the throw-style boundary between main()'s Try/Catch
// wrapper and the rest of the program: any panic with an *Exception
// bubbles up here and prints to stderr.
//
// Note: os.Exit from a subcommand bypasses outer-Try defers (only panics
// propagate). If success-path cleanup needs to fire, dispatch must return
// an exit code instead of calling os.Exit directly.
func dispatch(argv []string) {
	if len(argv) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	switch argv[1] {
	case "help", "-h", "--help":
		os.Exit(cmdHelp(argv[2:]))
	case "fetch":
		os.Exit(cmdFetch(argv[2:]))
	case "make":
		os.Exit(cmdMake(argv[2:]))
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
    fetch      Fetch and unpack an external resource.
    make       Generate and execute the build graph for a target.
    help       Show this message.

Use yatool make -j 0 -G <target> > graph.json for graph-generation
checks, then compare with normalize.py for the canonical L0..L4 verdict.
`)
}

func cmdHelp(_ []string) int {
	printUsage(os.Stdout)

	return 0
}

// writeGraph encodes g as JSON to path (or stdout when path == "-").
// Delegates to writeGraphIndented (gjson_write.go), a hand-rolled streaming
// serializer that matches json.Encoder with SetEscapeHTML(false) +
// SetIndent("", "  ") byte-for-byte in a single pass. Output is buffered
// through a 1 MiB bufio.Writer.
func writeGraph(out string, g *Graph) {
	var w io.Writer

	if out == "-" {
		w = os.Stdout
	} else {
		f := Throw2(os.Create(out))
		defer func() {
			Throw(f.Close())
		}()
		w = f
	}

	bw := bufio.NewWriterSize(w, 1<<20)
	writeGraphIndented(bw, g)
	Throw(bw.Flush())
}
