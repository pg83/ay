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
	probes, g, rest := parseGlobalFlags(argv[1:])

	for _, p := range probes {
		if p == "str" {
			strProbeEnabled = true
		}
	}

	code := runCommand(rest, g) // empty rest falls through to choice-point help

	dumpProbes(probes)
	dumpCalls() // flush callsite reachability when CALLSITE_OUT is set
	os.Exit(code)
}

// GlobalFlags are the options parsed before the subcommand and threaded to
// every handler.
type GlobalFlags struct {
	Verbose bool
}

// parseGlobalFlags consumes the leading -flags, returning the --probe values,
// GlobalFlags, and remaining argv. The first non-flag arg or -h/--help ends the
// global section.
func parseGlobalFlags(argv []string) (probes []string, g GlobalFlags, rest []string) {
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
		case k == "verbose" || k == "v":
			g.Verbose = true
		default:
			throwFmt("unknown global flag %q", a)
		}
	}

	return probes, g, argv[i:]
}

// command binds a subcommand token path to its handler. hide keeps internal
// commands (invoked by build-graph nodes) out of the listing.
type command struct {
	path []string
	run  func(g GlobalFlags, args []string) int
	help string
	hide bool
}

// commands is the flat subcommand table; dispatch picks the entry whose token
// path is the longest prefix of the argv.
var commands = []command{
	{
		path: []string{"fetch"}, run: cmdFetch, hide: true,
		help: "📥 Fetch a Sandbox/MDS resource into the build root (FETCH node).",
	},
	{
		path: []string{"fetch", "base64"}, run: cmdFetchBase64, hide: true,
		help: "🔣 Write base64-decoded data to a file (inline vcs.json node).",
	},
	{
		path: []string{"fetch", "sandbox"}, run: cmdFetchSandbox, hide: true,
		help: "📦 Authenticated FROM_SANDBOX fetch (SB node): replaces the\n" +
			"unauthenticated fetch_from_sandbox.py; untar/copy outputs into the build dir.",
	},
	{
		path: []string{"make"}, run: cmdMake,
		help: "🔨 Generate the build graph for the given targets and write it as JSON.\n" +
			"Mirrors ymake: --source-root, --sandboxing, -G, -j.",
	},
	{
		path: []string{"dev", "cas"}, run: cmdCasAnalyze,
		help: "🗜️ Read-only analysis: estimate extra CAS savings from content-defined\n" +
			"chunking (rolling-hash chunk dedup) on top of whole-file dedup.",
	},
	{
		path: []string{"dev", "dump", "normalize"}, run: cmdDumpNormalize,
		help: "🧼 Normalize a raw graph dump (fold producers, canonicalize paths, prune\n" +
			"ref-only artifacts) to one JSON node per line for byte-exact comparison.",
	},
	{
		path: []string{"dev", "dump", "sort"}, run: cmdDumpSort,
		help: "🔢 Stable-sort normalized graph lines so two graphs can be merge-compared.",
	},
	{
		path: []string{"dev", "dump", "diff"}, run: cmdDumpDiff,
		help: "🆚 Diff two normalized graphs — by kind/field/token, paired nodes, or roots.",
	},
	{
		path: []string{"dev", "dump", "grep"}, run: cmdDumpGrep,
		help: "🔎 Search a graph dump for nodes by output/cmd/input substring.",
	},
	{
		path: []string{"dev", "perf", "parser"}, run: cmdPerfParser,
		help: "⏱️ Benchmark the C/ya.make parser over every source file under <dir>.",
	},
	{
		path: []string{"dev", "perf", "darts"}, run: cmdPerfDarts,
		help: "🎯 Benchmark the autoinclude longest-prefix matcher: double-array trie vs\n" +
			"the former ancestor-walk. Prints ns/op for each.",
	},
	{
		path: []string{"dev", "perf", "link"}, run: cmdPerfLink,
		help: "🔗 Benchmark input materialization from CAS: hard link vs symlink vs copy.\n" +
			"Args: [count] [size-bytes]. Prints ns/op for each.",
	},
	{
		path: []string{"dev", "refac", "consts"}, run: refacConsts,
		help: "♻️ Regenerate the interned-constant files (str/arg/vfs/env) from the\n" +
			"literals used across the package. Mutates source in place.",
	},
	{
		path: []string{"dev", "refac", "lint"}, run: refacLint,
		help: "🧹 Apply the in-tree linters to the given .go files (default: all non-test\n" +
			".go here). Mutates source in place.",
	},
	{
		path: []string{"dev", "refac", "case"}, run: refacCase,
		help: "🔠 Flip identifier case via the compiler's error positions to a fixpoint.\n" +
			"Mutates source in place; run in a worktree.",
	},
	{
		path: []string{"dev", "probe", "mapinstr"}, run: probeMapInstr,
		help: "🗺️ Throwaway: instrument real map ops with per-site counters; build under\n" +
			"--probe=map to dump the tally. Run in a worktree, revert after.",
	},
	{
		path: []string{"dev", "probe", "callsite"}, run: probeCallSite,
		help: "📞 Throwaway: instrument per-function call sites for reachability; build\n" +
			"under --probe=callsite with CALLSITE_OUT to find never-run code.",
	},
}

// Help palette.
func clHeader(s string) string {
	return color("light-green", s)
}

func clName(s string) string {
	return color("light-red", s)
}

func clFlag(s string) string {
	return color("light-yellow", s)
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

// usageCommands lists the subcommands under prefix with their help. Hidden
// commands and the collapsed `dev` group expand only under verbose.
func usageCommands(prefix []string, verbose bool) string {
	var b strings.Builder

	if len(prefix) == 0 {
		b.WriteString(clHeader("usage:") + " ay [global-flags] <subcommand> [args]")
	} else {
		b.WriteString(clHeader("usage:") + " ay [global-flags] " + strings.Join(prefix, " ") + " <subcommand> [args]")
	}

	// Only --verbose shows by default; it also reveals the remaining flags.
	b.WriteString("\n  " + clFlag("-v, --verbose") + "             expand collapsed groups (dev), hidden commands, and the flags below")

	if verbose {
		b.WriteString("\n  " + clFlag("--probe=map|callsite|str") + "  dump the named runtime probe (map/callsite/str) on exit")
	}

	b.WriteString("\n\n" + clHeader("subcommands:"))

	devCollapsed := false
	first := true

	// entry opens a subcommand block, blank-line-separated from the previous.
	entry := func(name string) {
		if first {
			b.WriteString("\n  ")
		} else {
			b.WriteString("\n\n  ")
		}

		first = false
		b.WriteString(clName(name))
	}

	for _, c := range commands {
		if (c.hide && !verbose) || !isTokenPrefix(prefix, c.path) {
			continue
		}

		// Collapse the dev group to one line unless --verbose; drilling in
		// lists it regardless.
		if len(prefix) == 0 && !verbose && c.path[0] == "dev" {
			if !devCollapsed {
				entry("dev:")
				b.WriteString("\n    🛠️ Developer tooling (dump, perf, refac, probe). Pass --verbose to list.")
				devCollapsed = true
			}

			continue
		}

		entry(strings.Join(c.path, " ") + ":")

		for _, line := range strings.Split(c.help, "\n") {
			b.WriteString("\n    ")
			b.WriteString(line)
		}
	}

	return b.String()
}

// runCommand dispatches argv by longest matching token path: a leaf match runs
// its handler, a choice-point prefix shows prefix help, else unknown.
func runCommand(argv []string, g GlobalFlags) int {
	best := -1
	bestLen := 0

	for i, c := range commands {
		if isTokenPrefix(c.path, argv) && (best < 0 || len(c.path) > bestLen) {
			best = i
			bestLen = len(c.path)
		}
	}

	if best >= 0 {
		return commands[best].run(g, argv[bestLen:])
	}

	for _, c := range commands {
		if !c.hide && isTokenPrefix(argv, c.path) {
			fmt.Fprintln(os.Stderr, usageCommands(argv, g.Verbose))

			return 2
		}
	}

	fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n%s\n", strings.Join(argv, " "), usageCommands(nil, g.Verbose))

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
