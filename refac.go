package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"sort"
	"strconv"
	"strings"
)

var goKeyword = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// linters is the fixed set applied by `ay refac lint`, in order.
var linters = []fileLinter{
	{name: "consolidate-vars", run: lintConsolidateVars},
	{name: "blank-around-blocks", run: lintControlBlankLines},
}

// cmdRefac dispatches in-tree refactoring helpers. They mutate source files in
// place; run them in a throwaway worktree and review the diff.
func cmdRefac(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ay refac consts|lint [files...]")
		return 2
	}

	switch args[0] {
	case "consts":
		return refacConsts(args[1:])
	case "lint":
		return refacLint(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown refac subcommand: %s\n", args[0])
		return 2
	}
}

// goFilesFromArgs returns the explicit file args, or — when none are given —
// every non-test .go file in the current directory, sorted.
func goFilesFromArgs(args []string) []string {
	if len(args) > 0 {
		return args
	}

	var files []string

	for _, e := range Throw2(os.ReadDir(".")) {
		n := e.Name()

		if !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			files = append(files, n)
		}
	}

	sort.Strings(files)
	return files
}

// refacConsts hoists every Intern/Source/Build call with a constant string literal
// out of function bodies into a package-level var, deduplicating by the resolved
// VFS path and reusing any equivalent var already declared. The call site is
// rewritten to the var name. With no file args it processes every non-test .go
// file in the current directory.
//
// Dedup and name allocation are package-wide: Go's package-level identifiers share
// one namespace across all files, so a VFS first hoisted in file A is referenced
// (not re-declared) when it recurs in file B, and generated names are unique across
// the whole package. Files are processed in sorted order, so a new var lands in the
// first file (alphabetically) that uses it.
func refacConsts(args []string) int {
	files := goFilesFromArgs(args)

	// Phase 1: parse every file and collect package-wide state — all top-level
	// identifiers (so generated names never collide) and a canon->var map of vars
	// already hoisted, with their declaring call nodes flagged so we don't re-hoist.
	var parsed []*parsedFile
	existing := map[string]string{}
	used := map[string]bool{}

	for _, path := range files {
		pf := parseRefacFile(path, existing, used)

		if pf != nil {
			parsed = append(parsed, pf)
		}
	}

	// Phase 2: classify every hoist-eligible occurrence. A "free" occurrence sits in
	// a function body; a non-free one is an element of a file-level var/const
	// composite literal (a per-file interned list). Occurrences are gathered in
	// (file, source-position) order for deterministic name allocation.
	var occs []occurrence

	for fi, pf := range parsed {
		collectOccurrences(pf, fi, &occs)
	}

	// Phase 3: decide which canons get a package-level var. A canon is hoisted only
	// if it already has a var (declared in source or seen earlier) OR it has at least
	// one free occurrence. A canon that appears ONLY as a file-level-list element is
	// left inline — hoisting it would create a var referenced exactly once, which is
	// redundant indirection. The new var's declaration is attached to the file of its
	// first free occurrence.
	var newCanons []string
	newVarFile := map[string]int{}

	for _, o := range occs {
		if !o.free {
			continue
		}

		if _, ok := existing[o.canon]; ok {
			continue
		}

		name := uniqueName(identForVFS(o.canon), used)
		used[name] = true
		existing[o.canon] = name
		newCanons = append(newCanons, o.canon)
		newVarFile[o.canon] = o.fileIdx
	}

	// Phase 4: every occurrence whose canon now has a var (free or list element) is
	// rewritten to that var; list-only canons without a var stay inline.
	editsByFile := make([][]constEdit, len(parsed))

	for _, o := range occs {
		name, ok := existing[o.canon]

		if !ok {
			continue
		}

		editsByFile[o.fileIdx] = append(editsByFile[o.fileIdx], constEdit{o.start, o.end, name})
	}

	addedByFile := make([][]newVar, len(parsed))

	for _, canon := range newCanons {
		fi := newVarFile[canon]
		addedByFile[fi] = append(addedByFile[fi], newVar{existing[canon], constDef(canon)})
	}

	for fi, pf := range parsed {
		if applyRefacEdits(pf, editsByFile[fi], addedByFile[fi]) {
			fmt.Fprintf(os.Stderr, "refac consts: rewrote %s\n", pf.path)
		}
	}

	return 0
}

// occurrence is one hoist-eligible call site, located by byte offset in its file.
type occurrence struct {
	fileIdx    int
	start, end int
	canon      string
	free       bool // in a function body (justifies a var) vs a file-level-list element
}

// collectOccurrences appends every hoist-eligible call in pf to occs, tagging each
// as free (inside a function body) or not (an element of a file-level var/const
// composite literal). The direct-value vars recorded by parseRefacFile (declared)
// are skipped — they ARE the hoisted vars, not call sites to rewrite.
func collectOccurrences(pf *parsedFile, fileIdx int, occs *[]occurrence) {
	record := func(call *ast.CallExpr, free bool) bool {
		if pf.declared[call] {
			return false // a direct-value var declaration; leave it and its subtree
		}

		fn, lit, ok := hoistCall(call)

		if !ok {
			return true
		}

		*occs = append(*occs, occurrence{
			fileIdx: fileIdx,
			start:   pf.fset.Position(call.Pos()).Offset,
			end:     pf.fset.Position(call.End()).Offset,
			canon:   canonVFS(fn, lit),
			free:    free,
		})
		return false // the only child is the string literal — nothing nested to hoist
	}

	for _, decl := range pf.f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			ast.Inspect(d, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					return record(call, true)
				}

				return true
			})
		case *ast.GenDecl:
			if d.Tok != gotoken.VAR && d.Tok != gotoken.CONST {
				continue
			}

			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)

				if !ok {
					continue
				}

				for _, val := range vs.Values {
					ast.Inspect(val, func(n ast.Node) bool {
						if call, ok := n.(*ast.CallExpr); ok {
							return record(call, false)
						}

						return true
					})
				}
			}
		}
	}
}

type parsedFile struct {
	path     string
	src      []byte
	fset     *gotoken.FileSet
	f        *ast.File
	declared map[*ast.CallExpr]bool // hoist-eligible calls that already ARE package-level var values
}

// parseRefacFile parses path and folds its top-level declarations into the shared
// existing/used maps. Returns nil (with a warning) if the file fails to parse.
func parseRefacFile(path string, existing map[string]string, used map[string]bool) *parsedFile {
	src := Throw2(os.ReadFile(path))
	fset := gotoken.NewFileSet()
	f, err := goparser.ParseFile(fset, path, src, goparser.ParseComments)

	if err != nil {
		fmt.Fprintf(os.Stderr, "refac consts: %s: parse: %v\n", path, err)
		return nil
	}

	declared := map[*ast.CallExpr]bool{}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			used[d.Name.Name] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					used[s.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range s.Names {
						used[n.Name] = true
					}

					if d.Tok == gotoken.VAR || d.Tok == gotoken.CONST {
						for i, val := range s.Values {
							call, isCall := val.(*ast.CallExpr)

							if !isCall || i >= len(s.Names) {
								continue
							}

							if fn, lit, ok := hoistCall(call); ok {
								canon := canonVFS(fn, lit)

								if _, seen := existing[canon]; !seen {
									existing[canon] = s.Names[i].Name
								}

								declared[call] = true
							}
						}
					}
				}
			}
		}
	}

	return &parsedFile{path: path, src: src, fset: fset, f: f, declared: declared}
}

// hoistCall reports whether call is `Intern|Source|Build("<literal>")` and returns
// the func name and the unquoted literal.
func hoistCall(call *ast.CallExpr) (fn, lit string, ok bool) {
	id, isID := call.Fun.(*ast.Ident)

	if !isID || (id.Name != "Intern" && id.Name != "Source" && id.Name != "Build") {
		return "", "", false
	}

	if len(call.Args) != 1 {
		return "", "", false
	}

	bl, isLit := call.Args[0].(*ast.BasicLit)

	if !isLit || bl.Kind != gotoken.STRING {
		return "", "", false
	}

	s, err := strconv.Unquote(bl.Value)

	if err != nil {
		return "", "", false
	}

	return id.Name, s, true
}

// canonVFS is the resolved VFS string for a hoistable call — the dedup key, so
// Source("x") and Intern("$(S)/x") share one var.
func canonVFS(fn, lit string) string {
	switch fn {
	case "Source":
		return "$(S)/" + lit
	case "Build":
		return "$(B)/" + lit
	default:
		return lit
	}
}

// constDef renders the var initializer for a canonical VFS, preferring the Source/
// Build short forms.
func constDef(canon string) string {
	if rel, ok := strings.CutPrefix(canon, "$(S)/"); ok {
		return fmt.Sprintf("Source(%q)", rel)
	}

	if rel, ok := strings.CutPrefix(canon, "$(B)/"); ok {
		return fmt.Sprintf("Build(%q)", rel)
	}

	return fmt.Sprintf("Intern(%q)", canon)
}

type constEdit struct {
	start, end int
	name       string
}

type newVar struct{ name, def string }

// applyRefacEdits rewrites pf's recorded call sites to var references and appends
// any new var declarations, then formats and writes the file. Returns whether the
// file changed.
func applyRefacEdits(pf *parsedFile, edits []constEdit, added []newVar) bool {
	if len(edits) == 0 {
		return false
	}

	src := pf.src

	// Apply call-site replacements back-to-front so earlier offsets stay valid.
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte(nil), src...)

	for _, e := range edits {
		out = append(out[:e.start], append([]byte(e.name), out[e.end:]...)...)
	}

	if len(added) > 0 {
		var b strings.Builder
		b.WriteString("\n// Path constants hoisted by `ay refac consts`.\nvar (\n")
		sort.Slice(added, func(i, j int) bool { return added[i].name < added[j].name })

		for _, v := range added {
			fmt.Fprintf(&b, "\t%s = %s\n", v.name, v.def)
		}

		b.WriteString(")\n")
		out = append(out, b.String()...)
	}

	formatted, err := format.Source(out)

	if err != nil {
		fmt.Fprintf(os.Stderr, "refac consts: %s: format failed (left unchanged): %v\n", pf.path, err)
		return false
	}

	Throw(os.WriteFile(pf.path, formatted, 0o644))
	return true
}

// identForVFS turns a resolved VFS path into a lowerCamel identifier: the $(S)/$(B)
// prefix is dropped ($(B) becomes a leading "bld" word so source/build siblings get
// distinct names), and every non-alphanumeric run separates words.
func identForVFS(canon string) string {
	s := canon
	var words []string

	if rel, ok := strings.CutPrefix(s, "$(S)/"); ok {
		s = rel
	} else if rel, ok := strings.CutPrefix(s, "$(B)/"); ok {
		s = rel
		words = append(words, "bld")
	}

	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}

	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}

	flush()

	if len(words) == 0 {
		words = []string{"v"}
	}

	var b strings.Builder

	for i, w := range words {
		w = strings.ToLower(w)

		if i == 0 {
			b.WriteString(w)
		} else {
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}

	id := b.String()

	if c := id[0]; c >= '0' && c <= '9' {
		id = "v" + id
	}

	return id
}

func uniqueName(base string, used map[string]bool) string {
	if !used[base] && !goKeyword[base] {
		return base
	}

	for i := 2; ; i++ {
		cand := base + strconv.Itoa(i)

		if !used[cand] {
			return cand
		}
	}
}

// fileLinter rewrites one file in place and reports whether it changed it.
type fileLinter struct {
	name string
	run  func(path string) bool
}

// refacLint applies every linter, in order, to each file. With no file args it
// processes every non-test .go file in the current directory.
func refacLint(args []string) int {
	for _, path := range goFilesFromArgs(args) {
		for _, l := range linters {
			if l.run(path) {
				fmt.Fprintf(os.Stderr, "refac lint: %s: %s\n", l.name, path)
			}
		}
	}

	return 0
}

// varItem is one package-level var spec extracted for consolidation. body holds the
// spec without the `var` keyword ("name [type] = value"); doc and blockDoc are the
// spec's own and its containing block's doc comments; multiline marks specs whose
// value spans more than one line.
type varItem struct {
	blockDoc  string
	doc       string
	body      string
	multiline bool
}

// render emits the item either as a group entry (inGroup, no `var` keyword) or as a
// standalone declaration.
func (it varItem) render(inGroup bool) string {
	var b strings.Builder

	if it.blockDoc != "" {
		b.WriteString(it.blockDoc)
		b.WriteByte('\n')
	}

	if it.doc != "" {
		b.WriteString(it.doc)
		b.WriteByte('\n')
	}

	if !inGroup {
		b.WriteString("var ")
	}

	b.WriteString(it.body)
	return b.String()
}

// lintConsolidateVars groups a file's single-line package-level vars into one
// var(...) block placed immediately after the imports, before all other code. A var
// whose value serialization spans multiple lines (composite literals such as
// map[...]{...} or []T{...}, multi-line strings — anything that introduces a new
// indentation level) is kept as its own standalone `var` declaration, emitted below
// the group, since folding it into the block would add an extra indentation level
// and hurt readability. Declaration order, doc comments, and trailing comments are
// preserved. The grouped block is formed only when at least two single-line vars
// exist; otherwise every var is emitted standalone. Acts only when the file has at
// least two package-level var specs.
func lintConsolidateVars(path string) bool {
	src := Throw2(os.ReadFile(path))
	fset := gotoken.NewFileSet()
	f, err := goparser.ParseFile(fset, path, src, goparser.ParseComments)

	if err != nil {
		fmt.Fprintf(os.Stderr, "refac lint: %s: parse: %v\n", path, err)
		return false
	}

	var varDecls []*ast.GenDecl
	totalSpecs := 0

	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == gotoken.VAR {
			varDecls = append(varDecls, gd)
			totalSpecs += len(gd.Specs)
		}
	}

	if totalSpecs < 2 {
		return false
	}

	off := func(p gotoken.Pos) int { return fset.Position(p).Offset }
	text := func(lo, hi gotoken.Pos) string { return string(src[off(lo):off(hi)]) }

	type span struct{ start, end int }
	var items []varItem
	var removals []span

	for _, gd := range varDecls {
		rmStart := off(gd.Pos())

		if gd.Doc != nil {
			rmStart = off(gd.Doc.Pos())
		}

		rmEnd := off(gd.End())

		// A grouped block's own doc comment attaches once, to the first spec it yields.
		blockDoc := ""

		if gd.Lparen.IsValid() && gd.Doc != nil {
			blockDoc = text(gd.Doc.Pos(), gd.Doc.End())
		}

		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			hi := vs.End()

			if vs.Comment != nil {
				hi = vs.Comment.End()

				if off(hi) > rmEnd {
					rmEnd = off(hi)
				}
			}

			doc := ""

			if vs.Doc != nil {
				doc = text(vs.Doc.Pos(), vs.Doc.End())
			} else if !gd.Lparen.IsValid() && gd.Doc != nil {
				doc = text(gd.Doc.Pos(), gd.Doc.End())
			}

			items = append(items, varItem{
				blockDoc:  blockDoc,
				doc:       doc,
				body:      text(vs.Pos(), hi),
				multiline: strings.IndexByte(text(vs.Pos(), vs.End()), '\n') >= 0,
			})
			blockDoc = "" // consumed by the first spec only
		}

		removals = append(removals, span{rmStart, rmEnd})
	}

	simpleCount := 0

	for _, it := range items {
		if !it.multiline {
			simpleCount++
		}
	}

	group := simpleCount >= 2

	// Assemble the replacement: the grouped block first (single-line vars, in order),
	// then standalone declarations for everything that stays ungrouped, in order.
	var parts []string

	if group {
		var b strings.Builder
		b.WriteString("var (\n")

		for _, it := range items {
			if it.multiline {
				continue
			}

			b.WriteString(it.render(true))
			b.WriteByte('\n')
		}

		b.WriteString(")")
		parts = append(parts, b.String())

		for _, it := range items {
			if it.multiline {
				parts = append(parts, it.render(false))
			}
		}
	} else {
		for _, it := range items {
			parts = append(parts, it.render(false))
		}
	}

	// Insert after the import declaration, or after the package clause if none.
	insOff := off(f.Name.End())

	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == gotoken.IMPORT {
			insOff = off(gd.End())
			break
		}
	}

	// Delete the original declarations back-to-front (every removal starts after
	// insOff, so the bytes up to insOff stay put), then splice in the rebuilt vars.
	out := append([]byte(nil), src...)
	sort.Slice(removals, func(i, j int) bool { return removals[i].start > removals[j].start })

	for _, r := range removals {
		out = append(out[:r.start], out[r.end:]...)
	}

	block := "\n\n" + strings.Join(parts, "\n\n") + "\n"
	out = append(out[:insOff], append([]byte(block), out[insOff:]...)...)

	formatted, err := format.Source(out)

	if err != nil {
		fmt.Fprintf(os.Stderr, "refac lint: %s: consolidate-vars format failed (left unchanged): %v\n", path, err)
		return false
	}

	if bytes.Equal(formatted, src) {
		return false
	}

	Throw(os.WriteFile(path, formatted, 0o644))
	return true
}

// isControlBlockStmt reports whether stmt is one of the brace-block control
// statements that STYLE.md requires blank lines around: if/for/range/switch/
// type-switch/select, and go/defer of a func literal (a `go func(){...}()` block,
// not a plain `defer f.Close()`). A labeled statement is judged by its inner stmt.
func isControlBlockStmt(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		return true
	case *ast.GoStmt:
		_, ok := s.Call.Fun.(*ast.FuncLit)
		return ok
	case *ast.DeferStmt:
		_, ok := s.Call.Fun.(*ast.FuncLit)
		return ok
	case *ast.LabeledStmt:
		return isControlBlockStmt(s.Stmt)
	}

	return false
}

// lintControlBlankLines enforces STYLE.md's "blank lines around control blocks":
// before and after every control block (if/for/switch/select/go-func/defer-func),
// except where the block is the first or last statement of its enclosing block.
//
// That before/after pair of rules, with their first/last exceptions, is exactly the
// pairwise invariant: between any two adjacent statements in one statement list, if
// either is a control block there must be a blank line. A control block that is the
// first statement has no predecessor pair (so no blank is forced after the opening
// brace), and one that is last has no successor pair (none before the closing brace).
// The linter only inserts missing blanks; gofmt already collapses extra ones.
func lintControlBlankLines(path string) bool {
	src := Throw2(os.ReadFile(path))
	fset := gotoken.NewFileSet()
	f, err := goparser.ParseFile(fset, path, src, goparser.ParseComments)

	if err != nil {
		fmt.Fprintf(os.Stderr, "refac lint: %s: parse: %v\n", path, err)
		return false
	}

	lineOf := func(p gotoken.Pos) int { return fset.Position(p).Line }

	// Lines covered by a comment, so a control block's own leading comment block can
	// be skipped over — the blank goes above the comment, keeping it attached.
	commentLine := map[int]bool{}

	for _, cg := range f.Comments {
		for _, c := range cg.List {
			for l := lineOf(c.Pos()); l <= lineOf(c.End()); l++ {
				commentLine[l] = true
			}
		}
	}

	// insertBefore holds source line numbers that need a blank line inserted above.
	insertBefore := map[int]bool{}
	process := func(list []ast.Stmt) {
		for i := 1; i < len(list); i++ {
			a, b := list[i-1], list[i]

			if !isControlBlockStmt(a) && !isControlBlockStmt(b) {
				continue
			}

			aEnd := lineOf(a.End())
			lead := lineOf(b.Pos())

			for l := lead - 1; l > aEnd && commentLine[l]; l-- {
				lead = l
			}

			if lead == aEnd+1 {
				insertBefore[lead] = true
			}
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BlockStmt:
			process(node.List)
		case *ast.CaseClause:
			process(node.Body)
		case *ast.CommClause:
			process(node.Body)
		}

		return true
	})

	if len(insertBefore) == 0 {
		return false
	}

	lines := strings.Split(string(src), "\n")
	var b strings.Builder

	for i, ln := range lines {
		if insertBefore[i+1] {
			b.WriteByte('\n')
		}

		b.WriteString(ln)

		if i < len(lines)-1 {
			b.WriteByte('\n')
		}
	}

	formatted, err := format.Source([]byte(b.String()))

	if err != nil {
		fmt.Fprintf(os.Stderr, "refac lint: %s: blank-around-blocks format failed (left unchanged): %v\n", path, err)
		return false
	}

	if bytes.Equal(formatted, src) {
		return false
	}

	Throw(os.WriteFile(path, formatted, 0o644))
	return true
}
