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

	exc.catch(func(e *Exception) {
		fatalException(e)
	})
}

func dispatch(argv []string) {
	probes, rest := parseGlobalFlags(argv[1:])

	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ay <subcommand> [flags]")
		os.Exit(2)
	}

	code := runCommand(rest[0], rest[1:])

	dumpProbes(probes) // flush enabled probe stats before exit (cmds os.Exit-free)
	dumpCalls()        // flush callsite reachability when CALLSITE_OUT is set (env-driven)
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

		return 2
	}
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

func writeGraph(out string, g *Graph, dropSrcInputs bool) {
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
	writeGraphCompact(bw, g, dropSrcInputs)
	Throw(bw.Flush())
}
