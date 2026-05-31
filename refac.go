package main

import (
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

// cmdRefac dispatches in-tree refactoring helpers. They mutate source files in
// place; run them in a throwaway worktree and review the diff.
func cmdRefac(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ay refac consts [files...]")
		return 2
	}
	switch args[0] {
	case "consts":
		return refacConsts(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown refac subcommand: %s\n", args[0])
		return 2
	}
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
	files := args
	if len(files) == 0 {
		ents := Throw2(os.ReadDir("."))
		for _, e := range ents {
			n := e.Name()
			if !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
				files = append(files, n)
			}
		}
		sort.Strings(files)
	}

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

	// Phase 2: rewrite each file's call sites, allocating new vars against the
	// shared maps so later files reuse what earlier files introduced.
	for _, pf := range parsed {
		if rewriteRefacFile(pf, existing, used) {
			fmt.Fprintf(os.Stderr, "refac consts: rewrote %s\n", pf.path)
		}
	}
	return 0
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

// rewriteRefacFile rewrites pf's hoist-eligible call sites to var references,
// allocating new package-level vars (into the shared existing/used maps) for
// canonical VFS values not already declared, and appending those new vars to pf.
func rewriteRefacFile(pf *parsedFile, existing map[string]string, used map[string]bool) bool {
	var edits []constEdit
	var added []newVar

	ast.Inspect(pf.f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if pf.declared[call] {
			return true
		}
		fn, lit, ok := hoistCall(call)
		if !ok {
			return true
		}
		canon := canonVFS(fn, lit)
		name := existing[canon]
		if name == "" {
			name = uniqueName(identForVFS(canon), used)
			used[name] = true
			existing[canon] = name
			added = append(added, newVar{name: name, def: constDef(canon)})
		}
		edits = append(edits, constEdit{
			start: pf.fset.Position(call.Pos()).Offset,
			end:   pf.fset.Position(call.End()).Offset,
			name:  name,
		})
		return false // the only child is the string literal — nothing to hoist inside
	})

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

var goKeyword = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}
