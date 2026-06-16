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

	code := runCommand(rest) // empty rest falls through to the choice-point help

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
// args that follow the matched path. hide keeps internal commands (invoked by
// build-graph nodes, not users) out of the usage listing.
type command struct {
	path []string
	run  func(args []string) int
	hide bool
}

// commands is the flat subcommand table. Dispatch picks the entry whose token
// path is the longest prefix of the argv (so {"make"} and {"make","cas"} coexist;
// `make cas …` takes the latter, `make …` the former).
var commands = []command{
	{path: []string{"fetch"}, run: cmdFetch, hide: true},
	{path: []string{"fetch", "base64"}, run: cmdFetchBase64, hide: true},
	{path: []string{"make"}, run: cmdMake},
	{path: []string{"make", "cas"}, run: cmdCasAnalyze},
	{path: []string{"dump", "normalize"}, run: cmdDumpNormalize},
	{path: []string{"dump", "sort"}, run: cmdDumpSort},
	{path: []string{"dump", "diff"}, run: cmdDumpDiff},
	{path: []string{"dump", "grep"}, run: cmdDumpGrep},
	{path: []string{"perf", "parser"}, run: cmdPerfParser},
	{path: []string{"perf", "darts"}, run: cmdPerfDarts},
	{path: []string{"refac", "consts"}, run: refacConsts},
	{path: []string{"refac", "lint"}, run: refacLint},
	{path: []string{"refac", "case"}, run: refacCase},
	{path: []string{"probe", "mapinstr"}, run: probeMapInstr},
	{path: []string{"probe", "callsite"}, run: probeCallSite},
}

// isTokenPrefix reports whether p is a token-wise prefix of of.
func isTokenPrefix(p, of []string) bool {
	if len(p) > len(of) {
		return false
	}

	for i, tok := range p {
		if of[i] != tok {
			return false
		}
	}

	return true
}

// usageCommands lists the visible subcommand paths under prefix (all, for the
// empty prefix), one per line — the help shown at a choice point.
func usageCommands(prefix []string) string {
	var b strings.Builder

	if len(prefix) == 0 {
		b.WriteString("usage: ay <subcommand> [flags]")
	} else {
		b.WriteString("usage: ay " + strings.Join(prefix, " ") + " <subcommand> [flags]")
	}

	b.WriteString("\nsubcommands:")

	for _, c := range commands {
		if c.hide || !isTokenPrefix(prefix, c.path) {
			continue
		}

		b.WriteString("\n  ")
		b.WriteString(strings.Join(c.path, " "))
	}

	return b.String()
}

// runCommand dispatches argv against commands by longest matching token path. A
// full leaf match runs its handler; otherwise, if argv is a choice-point prefix
// of some visible command(s) (the empty argv included), the prefix help is shown;
// anything else is an unknown subcommand.
func runCommand(argv []string) int {
	best := -1
	bestLen := 0

	for i, c := range commands {
		if isTokenPrefix(c.path, argv) && (best < 0 || len(c.path) > bestLen) {
			best = i
			bestLen = len(c.path)
		}
	}

	if best >= 0 {
		return commands[best].run(argv[bestLen:])
	}

	for _, c := range commands {
		if !c.hide && isTokenPrefix(argv, c.path) {
			fmt.Fprintln(os.Stderr, usageCommands(argv))

			return 2
		}
	}

	fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n%s\n", strings.Join(argv, " "), usageCommands(nil))

	return 2
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
