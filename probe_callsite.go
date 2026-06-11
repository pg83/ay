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
	"sync"
)

// probeCallSite instruments every top-level function: it splices a
// recordCall("file:line") call as the first statement of each func body. Then run
// the whole gate with the instrumented binary under --probe=callsite (which
// enables recording and dumps on exit) and CALLSITE_OUT=<file>; union the
// recorded sites across runs. Any site in the all-sites file (first arg) NOT in
// the union is reachable code the gate never exercises — refactor garbage.
// Throwaway: apply, measure, revert.
func probeCallSite(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ay probe callsite <all-sites-out> [files...]")

		return 2
	}

	outPath := args[0]
	files := goFilesFromArgs(args[1:])
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

	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "callsite: mkdir %s: %v\n", dir, err)

			return 1
		}
	}

	if err := os.WriteFile(outPath, []byte(strings.Join(allSites, "\n")+"\n"), 0o644); err != nil {
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

// callSiteSeen is the recorded reach-set. A sync.Map (not a channel) because it
// must be usable from the package's very first instruction: package-var
// initializers — e.g. includeDirectiveParsers = newIncludeDirectiveParserRegistry()
// — run before any init(), so a channel/goroutine set up in init() would miss
// those init-time calls and report init-reached funcs as dead. sync.Map's zero
// value is ready at load and safe for the concurrent stores from make's gen path
// and dump's streamGraphFanout. Store is idempotent (reachability, not counts).
var callSiteSeen sync.Map

func recordCall(site string) {
	callSiteSeen.Store(site, struct{}{})
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

	callSiteSeen.Range(func(k, _ any) bool {
		fmt.Fprintln(f, k.(string))

		return true
	})

	f.Close()
}
