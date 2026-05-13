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
	"strings"
	"time"
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
    help       Show this message.

Quality / comparison checks live in normalize.py — run that against the
generated graph and a reference graph for the canonical L0..L4 verdict.
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
	out := fs.String("out", "", "path to write the generated JSON (use '-' for stdout; empty skips serialisation, useful for build-only timing)")
	timeReport := fs.Bool("time", false, "print wall-time breakdown (gen + serialise) to stderr")
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

	genStart := time.Now()
	g := GenWithMode(TargetCfg, *sourceRoot, *target, defines.toMap(), *scanCtxMode)
	genDur := time.Since(genStart)

	if *memProfile != "" {
		f, ferr := os.Create(*memProfile)
		Throw(ferr)

		runtime.GC()
		Throw(pprof.WriteHeapProfile(f))
		Throw(f.Close())
	}

	var writeDur time.Duration

	if *out != "" {
		writeStart := time.Now()
		writeGraph(*out, g)
		writeDur = time.Since(writeStart)
	}

	if *timeReport {
		if *out != "" {
			fmt.Fprintf(os.Stderr, "time: gen=%s serialise=%s total=%s\n", genDur.Round(time.Millisecond), writeDur.Round(time.Millisecond), (genDur + writeDur).Round(time.Millisecond))
		} else {
			fmt.Fprintf(os.Stderr, "time: gen=%s (no --out, serialisation skipped)\n", genDur.Round(time.Millisecond))
		}
	}

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
	fmt.Fprint(w, `Usage: yatool gen --target <module-dir> [--out <path|->] [--source-root <path>] [--define KEY=VALUE]...
Parse <source-root>/<module-dir>/ya.make and build the in-memory graph;
serialise to JSON when --out is given.

Flags:
    --target <path>        Module-relative ya.make directory (e.g. build/cow/on). Required.
    --out <path|->         Output JSON path; "-" writes to stdout. Empty (default)
                           skips serialisation — useful with --time for build-only timing.
    --source-root <path>   Absolute source-tree root. Defaults to /home/pg/monorepo/yatool_orig.
    --define KEY=VALUE     Repeatable. Mirrors ymake's -D flag. Default when omitted: MUSL=yes.
    --time                 Print wall-time breakdown (gen + serialise) to stderr.
    --cpuprofile <path>    Write CPU profile (pprof) over the run. Empty disables.
    --memprofile <path>    Write heap profile after Gen. Empty disables.
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

