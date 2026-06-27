package main

import (
	"fmt"
	"go/ast"
	"go/importer"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
)

func probeMapInstr(_ GlobalFlags, args []string) int {
	files := goFilesFromArgs(args)
	fset := gotoken.NewFileSet()
	asts := make(map[string]*ast.File, len(files))
	src := map[string][]byte{}
	order := []string{}

	for _, p := range files {
		b, err := os.ReadFile(p)

		if err != nil {
			fmt.Fprintf(os.Stderr, "mapinstr: read %s: %v\n", p, err)

			return 1
		}

		f, err := goparser.ParseFile(fset, p, b, goparser.ParseComments)

		if err != nil {
			fmt.Fprintf(os.Stderr, "mapinstr: parse %s: %v\n", p, err)

			return 1
		}

		asts[p] = f

		src[p] = b

		order = append(order, p)
	}

	collect := make([]*ast.File, 0, len(asts))

	for _, p := range order {
		collect = append(collect, asts[p])
	}

	info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)}
	conf := types.Config{Importer: importer.Default(), Error: func(error) {}}

	_, _ = conf.Check("main", fset, collect, info)

	type edit struct {
		start, end int
		fn         string
		site       string
	}

	edits := map[string][]edit{}
	reads, writes := 0, 0

	for _, p := range order {
		base := filepath.Base(p)

		if base == "probe_mapinstr.go" || base == "probe_callsite.go" || base == "probe.go" {
			continue
		}

		f := asts[p]
		writeIdx := map[*ast.IndexExpr]bool{}

		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range x.Lhs {
					if ix, ok := lhs.(*ast.IndexExpr); ok {
						writeIdx[ix] = true
					}
				}
			case *ast.IncDecStmt:
				if ix, ok := x.X.(*ast.IndexExpr); ok {
					writeIdx[ix] = true
				}
			}

			return true
		})

		add := func(keyExpr ast.Expr, write bool) {
			ps := fset.Position(keyExpr.Pos())
			pe := fset.Position(keyExpr.End())
			fn := "mapKR"

			if write {
				fn = "mapKW"
				writes++
			} else {
				reads++
			}

			edits[p] = append(edits[p], edit{ps.Offset, pe.Offset, fn, fmt.Sprintf("%s:%d", base, ps.Line)})
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.IndexExpr:
				if isMapExpr(info, x.X) {
					add(x.Index, writeIdx[x])
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "delete" && len(x.Args) == 2 && isMapExpr(info, x.Args[0]) {
					add(x.Args[1], true)
				}
			}

			return true
		})
	}

	for p, es := range edits {
		sort.Slice(es, func(i, j int) bool { return es[i].start > es[j].start })

		b := src[p]

		for _, e := range es {
			key := string(b[e.start:e.end])
			repl := fmt.Sprintf("%s(%s, %q)", e.fn, key, e.site)

			b = append(b[:e.start:e.start], append([]byte(repl), b[e.end:]...)...)
		}

		if err := os.WriteFile(p, b, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "mapinstr: write %s: %v\n", p, err)

			return 1
		}
	}

	fmt.Fprintf(os.Stderr, "mapinstr: wrapped %d reads + %d writes across %d files\n", reads, writes, len(edits))

	return 0
}

func isMapExpr(info *types.Info, e ast.Expr) bool {
	t := info.TypeOf(e)

	if t == nil {
		return false
	}

	_, ok := t.Underlying().(*types.Map)

	return ok
}

type MapProbeEntry struct {
	reads  uint64
	writes uint64
}

var mapProbeCounts = map[string]*MapProbeEntry{}

func mapProbeAt(site string, write bool) {
	e := mapProbeCounts[site]

	if e == nil {
		e = &MapProbeEntry{}

		mapProbeCounts[site] = e
	}

	if write {
		e.writes++
	} else {
		e.reads++
	}
}

func mapKR[K any](k K, site string) K {
	mapProbeAt(site, false)

	return k
}

func mapKW[K any](k K, site string) K {
	mapProbeAt(site, true)

	return k
}

func reportMapProbe() {
	type row struct {
		site   string
		reads  uint64
		writes uint64
	}

	rows := make([]row, 0, len(mapProbeCounts))

	for s, e := range mapProbeCounts {
		rows = append(rows, row{s, e.reads, e.writes})
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].reads+rows[i].writes > rows[j].reads+rows[j].writes })

	for _, r := range rows {
		fmt.Fprintf(os.Stderr, "mapop\t%d\t%d\t%s\n", r.reads, r.writes, r.site)
	}
}
