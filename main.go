package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
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

// cmdGen parses a ya.make and writes the resulting build graph as JSON.
// Per the PR-03 pattern (D17), we use ContinueOnError + SetOutput(io.Discard)
// so all diagnostics are owned by this function and the outer Catch.
// flag.ErrHelp is discriminated explicitly so that -h / --help exits 0
// with usage on stdout (PR-03-D01).
//
// Retires the PR-01-D05 ceremony: `flag.NewFlagSet` is now load-bearing
// (real flag registration), no `_ =` discard.
//
// --target is the module-relative ya.make directory (e.g. build/cow/on).
// --out is the output JSON path; "-" writes to stdout.
// --source-root defaults to the upstream snapshot used by PR-03's
// LoadReference test; override for a different checkout.
//
// Exit code: 0 on success. Argument errors and IO/parse failures throw,
// propagating to main()'s top-level Catch which prints to stderr and
// exits 1.
func cmdGen(args []string) int {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	target := fs.String("target", "", "module-relative path to the ya.make directory (e.g. build/cow/on)")
	out := fs.String("out", "", "path to write the generated JSON (use '-' for stdout)")
	sourceRoot := fs.String("source-root", "/home/pg/monorepo/yatool_orig", "absolute path to the source tree (defaults to the upstream snapshot)")

	// PR-32 D01: --define KEY=VALUE is the user-facing CLI flag that
	// drives flag-conditional auto-PEERDIRs and peer-CFLAGs (e.g.
	// `--define MUSL=yes` mirrors `build/ymake.core.conf:781`'s
	// `when ($MUSL == "yes") { PEERDIR+=contrib/libs/musl/include }`).
	// Repeatable; bare KEY (without "=") is rejected.
	var defines stringMapValue

	fs.Var(&defines, "define", "ymake-style -D KEY=VALUE; repeatable; default: -DMUSL=yes")

	// PR-M3-perf-E: scanCtx lifecycle policy. "interned" (default;
	// winner of the bake-off) interns one scanCtx per (scanner, ctxHash)
	// for the whole Gen call. "local" allocates a fresh scanCtx per
	// genModule frame (no cross-module reuse).
	scanCtxMode := fs.String("scan-ctx-mode", defaultScanCtxMode, "scanCtx lifecycle: \"local\" or \"interned\"")

	// PR-M3-perf-profile: write a Go pprof CPU profile to PATH for
	// the duration of the Gen call. Empty (default) disables
	// profiling. Inspect with `go tool pprof <PATH>`.
	cpuProfile := fs.String("cpuprofile", "", "write CPU profile to PATH (Go pprof format); empty disables")
	memProfile := fs.String("memprofile", "", "write heap profile to PATH after Gen completes; empty disables")
	profileRate := fs.Int("profile-rate", 1000, "CPU profile sampling rate in Hz (default 1000); only effective when -cpuprofile is set")

	err := fs.Parse(args)

	if errors.Is(err, flag.ErrHelp) {
		printGenUsage(os.Stdout)

		return 0
	}

	Throw(err)

	if *target == "" {
		ThrowFmt("gen: --target is required")
	}

	if *out == "" {
		ThrowFmt("gen: --out is required (use '-' for stdout)")
	}

	if *cpuProfile != "" {
		f, ferr := os.Create(*cpuProfile)
		Throw(ferr)

		runtime.SetCPUProfileRate(*profileRate)
		Throw(pprof.StartCPUProfile(f))

		defer func() {
			pprof.StopCPUProfile()
			Throw(f.Close())
		}()
	}

	g := GenWithMode(TargetCfg, *sourceRoot, *target, defines.toMap(), *scanCtxMode)

	if *memProfile != "" {
		f, ferr := os.Create(*memProfile)
		Throw(ferr)

		runtime.GC()
		Throw(pprof.WriteHeapProfile(f))
		Throw(f.Close())
	}

	writeGraph(*out, g)

	return 0
}

// stringMapValue implements flag.Value for repeatable
// `--define KEY=VALUE` arguments. The Set method splits on the first
// `=`; bare KEY (no `=`) returns an error rather than silently binding
// the key to an empty string. Used by cmdGen's `--define` plumbing
// (PR-32 D01).
type stringMapValue struct {
	pairs []string
}

func (s *stringMapValue) String() string {
	return strings.Join(s.pairs, ",")
}

func (s *stringMapValue) Set(v string) error {
	idx := strings.IndexByte(v, '=')

	if idx < 0 {
		return fmt.Errorf("--define expects KEY=VALUE, got %q", v)
	}

	if idx == 0 {
		return fmt.Errorf("--define expects KEY=VALUE with non-empty KEY, got %q", v)
	}

	s.pairs = append(s.pairs, v)

	return nil
}

// toMap returns the accumulated KEY=VALUE pairs as a freshly-allocated
// map. Returns nil when no `--define` was supplied so callers can
// discriminate "no flag" (apply defaults) from "explicit empty".
func (s *stringMapValue) toMap() map[string]string {
	if len(s.pairs) == 0 {
		return nil
	}

	out := make(map[string]string, len(s.pairs))

	for _, p := range s.pairs {
		idx := strings.IndexByte(p, '=')
		out[p[:idx]] = p[idx+1:]
	}

	return out
}

func printGenUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: yatool gen --target <module-dir> --out <path|-> [--source-root <path>] [--define KEY=VALUE]...
Parse <source-root>/<module-dir>/ya.make and write the resulting build graph as JSON.

Flags:
    --target <path>        Module-relative ya.make directory (e.g. build/cow/on). Required.
    --out <path|->         Output JSON path; "-" writes to stdout. Required.
    --source-root <path>   Absolute source-tree root. Defaults to /home/pg/monorepo/yatool_orig.
    --define KEY=VALUE     Repeatable. Mirrors ymake's -D flag. Default when omitted: MUSL=yes.
`)
}

// writeGraph encodes g as JSON to the given path (or stdout when path
// is "-"). Delegates to writeGraphIndented (gjson_write.go), a hand-rolled
// streaming serializer that matches json.Encoder with SetEscapeHTML(false)
// + SetIndent("", "  ") byte-for-byte but emits in a single pass — avoiding
// the compact-marshal + Indent two-pass cost that dominated profile time
// (PR-34l). Output via a 1 MiB bufio.Writer to keep file syscalls to a
// minimum.
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

// cmdCompare loads two reference g.json files and prints the
// comparator's report. Per the PR-03 pattern (D17), we use
// ContinueOnError + SetOutput(io.Discard) so all diagnostics are owned
// by this function and the outer Catch — the duplicate-output bug
// (PR-03-D02) cannot recur. flag.ErrHelp is discriminated explicitly
// so that -h / --help exits 0 with usage on stdout (PR-03-D01).
//
// --level controls the highest level computed. PR-04 implemented L0;
// PR-05 added L1 and L2; PR-06 added L3 (byte-exact cmds + env).
// Levels above the highest implemented are recorded in the report's
// Skipped slice (printed as a tail line) and currently have no
// functional effect. The default of 3 covers the full L0..L3 ladder.
//
// Exit code: always 0 on a successful comparison. The comparator is
// observational by default; a future --strict flag (out of scope for
// PR-04) may exit non-zero on L0 < 1.0.
func cmdCompare(args []string) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	level := fs.Int("level", 3, "highest comparator level to run (0=topology, 1=props/outputs, 2=inputs/tags/reqs, 3=byte-exact cmd_args + env)")

	err := fs.Parse(args)

	if errors.Is(err, flag.ErrHelp) {
		printCompareUsage(os.Stdout)

		return 0
	}

	Throw(err)

	if fs.NArg() != 2 {
		ThrowFmt("compare: expected exactly 2 positional args (path-to-want.json path-to-got.json), got %d", fs.NArg())
	}

	want := LoadReference(fs.Arg(0))
	got := LoadReference(fs.Arg(1))

	report := Compare(want, got, *level)

	fmt.Printf("L0: %.2f%%  (%s)\n", report.L0*100, report.L0Note)

	if *level >= 1 {
		fmt.Printf("L1: %.2f%%  (%s)\n", report.L1*100, report.L1Note)
	}

	if *level >= 2 {
		fmt.Printf("L2: %.2f%%  (%s)\n", report.L2*100, report.L2Note)
	}

	if *level >= 3 {
		fmt.Printf("L3: %.2f%%  (%s)\n", report.L3*100, report.L3Note)
	}

	if len(report.Skipped) > 0 {
		parts := make([]string, 0, len(report.Skipped))

		for _, lvl := range report.Skipped {
			parts = append(parts, fmt.Sprintf("L%d", lvl))
		}

		fmt.Printf("skipped (not yet implemented): %s\n", strings.Join(parts, ", "))
	}

	return 0
}

func printCompareUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: yatool compare [--level N] <path-to-want.json> <path-to-got.json>
Compare two graph files and print a per-level match report.

Levels:
    0 — topology (DAG shape modulo UID renumbering, plus per-node kv.p kind)
    1 — per-pair match on kv.p, target_properties, outputs
    2 — per-pair match on inputs, tags, requirements
    3 — per-pair byte-exact match on cmd_args + env (per-cmd and top-level)

Higher levels (4+) are reserved for later PRs and are listed as
"skipped" in the report when --level requests them.
`)
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
