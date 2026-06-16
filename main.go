package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
)

func main() {
	exc := try(func() {
		dispatch(os.Args)
	})

	exc.catch(func(e *Exception) {
		fatalException(e)
	})
}

func dispatch(argv []string) {
	probes, rest := parseGlobalFlags(argv[1:])

	for _, p := range probes {
		if p == "str" {
			strProbeEnabled = true
		}
	}

	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ay <subcommand> [flags]")
		os.Exit(2)
	}

	code := runCommand(rest)

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
		case k == "probe" && (v == "map" || v == "callsite" || v == "str"):
			probes = append(probes, v)
		case k == "probe":
			throwFmt("unknown --probe=%q (want map|callsite|str)", v)
		default:
			throwFmt("unknown global flag %q", a)
		}
	}

	return probes, argv[i:]
}

// command binds a subcommand token path to its handler. The handler receives the
// args that follow the matched path.
type command struct {
	path []string
	run  func(args []string) int
}

// commands is the flat subcommand table. Dispatch picks the entry whose token
// path is the longest prefix of the argv (so {"make"} and {"make","cas"} coexist;
// `make cas …` takes the latter, `make …` the former).
var commands = []command{
	{[]string{"fetch"}, cmdFetch},
	{[]string{"fetch", "base64"}, cmdFetchBase64},
	{[]string{"make"}, cmdMake},
	{[]string{"make", "cas"}, cmdCasAnalyze},
	{[]string{"dump", "normalize"}, cmdDumpNormalize},
	{[]string{"dump", "sort"}, cmdDumpSort},
	{[]string{"dump", "diff"}, cmdDumpDiff},
	{[]string{"dump", "grep"}, cmdDumpGrep},
	{[]string{"perf", "parser"}, cmdPerfParser},
	{[]string{"perf", "darts"}, cmdPerfDarts},
	{[]string{"refac", "consts"}, refacConsts},
	{[]string{"refac", "lint"}, refacLint},
	{[]string{"refac", "case"}, refacCase},
	{[]string{"probe", "mapinstr"}, probeMapInstr},
	{[]string{"probe", "callsite"}, probeCallSite},
}

// runCommand dispatches argv against commands by longest matching token path.
func runCommand(argv []string) int {
	best := -1
	bestLen := 0

	for i, c := range commands {
		if len(c.path) > len(argv) || len(c.path) < bestLen {
			continue
		}

		match := true

		for j, tok := range c.path {
			if argv[j] != tok {
				match = false

				break
			}
		}

		if match && (best < 0 || len(c.path) > bestLen) {
			best = i
			bestLen = len(c.path)
		}
	}

	if best < 0 {
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", strings.Join(argv, " "))

		return 2
	}

	return commands[best].run(argv[bestLen:])
}

func startProfilesFromEnv() func() {
	var cpuFile *os.File

	if path := os.Getenv("YATOOL_CPUPROFILE"); path != "" {
		cpuFile = throw2(os.Create(path))

		if hz := os.Getenv("YATOOL_CPUHZ"); hz != "" {
			runtime.SetCPUProfileRate(throw2(strconv.Atoi(hz)))
		}

		throw(pprof.StartCPUProfile(cpuFile))
	}

	return func() {
		if cpuFile != nil {
			pprof.StopCPUProfile()
			throw(cpuFile.Close())
		}

		if path := os.Getenv("YATOOL_MEMPROFILE"); path != "" {
			f := throw2(os.Create(path))
			runtime.GC()
			throw(pprof.WriteHeapProfile(f))
			throw(f.Close())
		}
	}
}

func writeGraph(out string, g *Graph, dropSrcInputs bool) {
	var w io.Writer

	if out == "-" {
		w = os.Stdout
	} else {
		f := throw2(os.Create(out))

		defer func() {
			throw(f.Close())
		}()

		w = f
	}

	bw := bufio.NewWriterSize(w, 1<<20)
	writeGraphCompact(bw, g, dropSrcInputs)
	throw(bw.Flush())
}
