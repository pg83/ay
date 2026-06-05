package main

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// probeCallSite instruments every top-level function: it splices a
// recordCall("file:line") call as the first statement of each func body. Run the
// whole gate with the instrumented binary (CALLSITE_OUT=<file>) and union the
// recorded sites; any site in callsites_all.txt NOT in the union is reachable
// code the gate never exercises — refactor garbage. Throwaway: apply, measure,
// revert. The recordCall/dumpCalls helpers below provide the runtime; dumpCalls
// must be wired into the cmd exit path (see the make/dump cmds).
func probeCallSite(args []string) int {
	files := goFilesFromArgs(args)
	fset := gotoken.NewFileSet()

	var allSites []string

	for _, p := range files {
		base := filepath.Base(p)

		if base == "probe_callsite.go" || base == "probe_mapinstr.go" || base == "probe.go" {
			continue
		}

		src, err := os.ReadFile(p)

		if err != nil {
			fmt.Fprintf(os.Stderr, "callsite: read %s: %v\n", p, err)
			return 1
		}

		f, err := goparser.ParseFile(fset, p, src, 0)

		if err != nil {
			fmt.Fprintf(os.Stderr, "callsite: parse %s: %v\n", p, err)
			return 1
		}

		type ins struct {
			off  int
			text string
		}
		var inserts []ins

		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)

			if !ok || fd.Body == nil {
				continue
			}

			line := fset.Position(fd.Pos()).Line
			site := fmt.Sprintf("%s:%d", base, line)
			allSites = append(allSites, site)

			lbrace := fset.Position(fd.Body.Lbrace).Offset
			// Terminate with ';' so the call is a complete statement even when
			// the body is a one-liner (func f() T { return x }) where the
			// original code stays on the same line as the splice point.
			inserts = append(inserts, ins{lbrace + 1, fmt.Sprintf("\n\trecordCall(%q);", site)})
		}

		// Apply in reverse offset order so earlier offsets stay valid.
		sort.Slice(inserts, func(i, j int) bool { return inserts[i].off > inserts[j].off })
		b := src

		for _, in := range inserts {
			b = append(b[:in.off:in.off], append([]byte(in.text), b[in.off:]...)...)
		}

		if err := os.WriteFile(p, b, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "callsite: write %s: %v\n", p, err)
			return 1
		}
	}

	sort.Strings(allSites)

	if err := os.WriteFile("dev/.out/callsites_all.txt", []byte(strings.Join(allSites, "\n")+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "callsite: write all-sites: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "callsite: injected %d sites across %d files\n", len(allSites), len(files))

	return 0
}

// --- runtime probe: populated by `ay probe callsite` above wrapping each
// top-level func with recordCall. dumpCalls flushes the called set (append) to
// $CALLSITE_OUT on cmd exit; union across the gate's runs, diff vs
// callsites_all.txt to find reachable-but-never-exercised (gate-garbage)
// functions. Throwaway. ---

var (
	callCounts = map[string]int{}
	// callRecording gates recordCall so only the single-threaded `make -G` gen path
	// records. Other handlers (notably `dump`, whose streamGraphFanout runs many
	// goroutines) would otherwise concurrently write callCounts and crash the
	// runtime with "concurrent map writes". cmdMake sets it true.
	callRecording = false
)

func recordCall(site string) {
	if !callRecording {
		return
	}

	callCounts[site]++
}

func dumpCalls() {
	path := os.Getenv("CALLSITE_OUT")

	if path == "" {
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)

	if err != nil {
		return
	}

	for s := range callCounts {
		fmt.Fprintln(f, s)
	}

	f.Close()
}
