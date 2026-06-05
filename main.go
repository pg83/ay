package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
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
	probes, rest := parseGlobalFlags(argv[1:])

	if len(rest) == 0 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	code := runCommand(rest[0], rest[1:])

	dumpProbes(probes) // flush enabled probe stats before exit (cmds os.Exit-free)
	os.Exit(code)
}

// parseGlobalFlags consumes the leading -flags before the subcommand (program-
// wide options) and returns the requested --probe values plus the remaining
// argv (subcommand + its args). The first non-flag arg ends the global section;
// -h/--help/help fall through to runCommand as the help subcommand.
func parseGlobalFlags(argv []string) (probes []string, rest []string) {
	i := 0

	for ; i < len(argv); i++ {
		a := argv[i]

		if a == "" || a[0] != '-' || a == "-h" || a == "--help" {
			break
		}

		k, v, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")

		switch {
		case k == "probe" && (v == "map" || v == "callsite"):
			probes = append(probes, v)
		case k == "probe":
			ThrowFmt("unknown --probe=%q (want map|callsite)", v)
		default:
			ThrowFmt("unknown global flag %q", a)
		}
	}

	return probes, argv[i:]
}

func runCommand(name string, args []string) int {
	switch name {
	case "help", "-h", "--help":
		return cmdHelp(args)
	case "fetch":
		return cmdFetch(args)
	case "make":
		return cmdMake(args)
	case "dump":
		return cmdDump(args)
	case "perf":
		return cmdPerf(args)
	case "refac":
		return cmdRefac(args)
	case "probe":
		return cmdProbe(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", name)
		printUsage(os.Stderr)

		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `ay — recreate ymake build-graph generator

Usage:
    ay [global flags] <subcommand> [flags]

Subcommands:
    make       Generate (and execute) the build graph for a target.
    dump       Graph tools: normalize | sort | diff | grep | graph.
    fetch      Fetch and unpack an external resource.
    perf       Benchmark the ya.make parser over a directory tree.
    refac      In-house source tooling: consts | lint.
    probe      Instrument the package's own source: mapinstr | callsite.
    help       Show this message.

Global flags (before the subcommand):
    --probe=map|callsite   Dump the named instrumentation tally on exit;
                           repeatable. Requires a binary built after the
                           matching 'ay probe <map|callsite>' instrumentation.

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
