package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
)

func main() {
	exc := Try(func() {
		dispatch(os.Args)
	})

	exc.Catch(func(e *Exception) {
		fatalException(e)
	})
}

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
	case "dump":
		os.Exit(cmdDump(argv[2:]))
	case "perf":
		os.Exit(cmdPerf(argv[2:]))
	case "refac":
		os.Exit(cmdRefac(argv[2:]))
	case "probe":
		os.Exit(cmdProbe(argv[2:]))
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", argv[1])
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `ay — recreate ymake build-graph generator

Usage:
    ay <subcommand> [flags]

Subcommands:
    fetch      Fetch and unpack an external resource.
    make       Generate and execute the build graph for a target.
    dump       Graph tools: dump normalize | sort | diff | grep.
    help       Show this message.

Use ay make -j 0 -G <target> > graph.json for graph-generation
checks, then 'ay dump normalize | ay dump sort' for the canonical L0..L4
verdict.
`)
}

func cmdHelp(_ []string) int {
	printUsage(os.Stdout)

	return 0
}

func startProfilesFromEnv() func() {
	var cpuFile *os.File

	if path := os.Getenv("YATOOL_CPUPROFILE"); path != "" {
		cpuFile = Throw2(os.Create(path))
		Throw(pprof.StartCPUProfile(cpuFile))
	}

	return func() {
		if cpuFile != nil {
			pprof.StopCPUProfile()
			Throw(cpuFile.Close())
		}

		if path := os.Getenv("YATOOL_MEMPROFILE"); path != "" {
			f := Throw2(os.Create(path))
			runtime.GC()
			Throw(pprof.WriteHeapProfile(f))
			Throw(f.Close())
		}
	}
}

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
	writeGraphCompact(bw, g)
	Throw(bw.Flush())
}
