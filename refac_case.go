package main

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// stdlibCaseMethods are method names that MUST stay upper-case: the standard
// library's machinery finds them by name through well-known interfaces
// (fmt.Stringer, error, json.Marshaler/Unmarshaler, sort.Interface,
// errors.Unwrap). Renaming them silently detaches the implementation.
var stdlibCaseMethods = map[string]bool{
	"String":        true,
	"Error":         true,
	"MarshalJSON":   true,
	"UnmarshalJSON": true,
	"Len":           true,
	"Less":          true,
	"Swap":          true,
	"Unwrap":        true,
}

// refacCase enforces the package naming convention — type names upper-case,
// method names lower-case (stdlibCaseMethods excepted). It renames the
// DECLARATIONS via AST positions, then drives every reference to the new
// name off the compiler's error positions (go build -gcflags=-e, then go
// vet for the test files), iterating to a fixpoint. A receiver-blind textual
// rename would clobber same-named methods on foreign types (sync.Pool.Get,
// strings.Builder.WriteString); the compiler resolves receivers for us and
// flags exactly the references that need the flip.
func refacCase(args []string) int {
	files := goFilesFromArgs(args)

	typeRen := map[string]string{}
	methodRen := map[string]string{}

	for _, path := range files {
		renameCaseDecls(path, typeRen, methodRen)
	}

	fmt.Fprintf(os.Stderr, "refac case: renamed decls: %d types, %d method names\n", len(typeRen), len(methodRen))

	for round := 0; ; round++ {
		if round > 500 {
			ThrowFmt("refac case: no fixpoint after %d rounds", round)
		}

		fixed, clean := fixCaseRefsOnce(typeRen, methodRen)

		if clean {
			break
		}

		if fixed == 0 {
			fmt.Fprintln(os.Stderr, "refac case: unfixable build errors remain:")
			out, _ := exec.Command("go", "build", "-gcflags=-e", "./...").CombinedOutput()
			os.Stderr.Write(out)
			return 1
		}
	}

	return 0
}

// renameCaseDecls rewrites the violating declarations of one file in place:
// lower-case package-level type names, upper-case method names (on both the
// func decls and any interface type's method list), recording old->new.
func renameCaseDecls(path string, typeRen, methodRen map[string]string) {
	fset := gotoken.NewFileSet()
	src := Throw2(os.ReadFile(path))
	f := Throw2(goparser.ParseFile(fset, path, src, goparser.SkipObjectResolution))

	type edit struct {
		off  int
		old  string
		new_ string
	}

	var edits []edit
	add := func(id *ast.Ident, new_ string) {
		edits = append(edits, edit{off: fset.Position(id.Pos()).Offset, old: id.Name, new_: new_})
	}

	capitalize := func(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
	lower := func(s string) string { return strings.ToLower(s[:1]) + s[1:] }

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != gotoken.TYPE {
				continue
			}

			for _, spec := range d.Specs {
				ts := spec.(*ast.TypeSpec)

				if name := ts.Name.Name; name[0] >= 'a' && name[0] <= 'z' {
					typeRen[name] = capitalize(name)
					add(ts.Name, typeRen[name])
				}

				// Interface method names follow the method convention.
				if it, ok := ts.Type.(*ast.InterfaceType); ok {
					for _, m := range it.Methods.List {
						for _, id := range m.Names {
							if id.Name[0] >= 'A' && id.Name[0] <= 'Z' && !stdlibCaseMethods[id.Name] {
								methodRen[id.Name] = lower(id.Name)
								add(id, methodRen[id.Name])
							}
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil {
				continue
			}

			if name := d.Name.Name; name[0] >= 'A' && name[0] <= 'Z' && !stdlibCaseMethods[name] {
				methodRen[name] = lower(name)
				add(d.Name, methodRen[name])
			}
		}
	}

	if len(edits) == 0 {
		return
	}

	sort.Slice(edits, func(i, j int) bool { return edits[i].off > edits[j].off })

	out := string(src)

	for _, e := range edits {
		if out[e.off:e.off+len(e.old)] != e.old {
			ThrowFmt("refac case: %s: offset %d holds %q, want %q", path, e.off, out[e.off:e.off+min(len(e.old), 20)], e.old)
		}

		out = out[:e.off] + e.new_ + out[e.off+len(e.old):]
	}

	Throw(os.WriteFile(path, []byte(out), 0o644))
}

var caseErrRe = regexp.MustCompile(`^([^:\s]+\.go):(\d+):(\d+): (?:undefined: (\w+)|\S+ undefined \(.*has no field or method (\w+))`)

// fixCaseRefsOnce compiles the package (and its tests) and flips the case of
// every reference the compiler reports as undefined under the recorded
// renames. Returns the number of fixes applied and whether both compiles ran
// clean.
func fixCaseRefsOnce(typeRen, methodRen map[string]string) (int, bool) {
	build, _ := exec.Command("go", "build", "-gcflags=-e", "./...").CombinedOutput()
	vet := []byte{}

	if len(build) == 0 {
		vet, _ = exec.Command("go", "vet", "./...").CombinedOutput()
	}

	type fix struct {
		line, col int
		old, new_ string
	}

	byFile := map[string][]fix{}
	total := 0

	for _, lineS := range strings.Split(string(build)+string(vet), "\n") {
		lineS = strings.TrimPrefix(strings.TrimSpace(lineS), "vet: ")
		m := caseErrRe.FindStringSubmatch(lineS)

		if m == nil {
			continue
		}

		name := m[4]
		ren := typeRen

		if name == "" {
			name = m[5]
			ren = methodRen
		}

		new_, ok := ren[name]

		if !ok {
			// An undefined TYPE reference can also surface through the method
			// table (conversions inside selector chains) and vice versa; try
			// the other map before giving up.
			if other, ok2 := methodRen[name]; ok2 {
				new_ = other
			} else if other, ok2 := typeRen[name]; ok2 {
				new_ = other
			} else {
				continue
			}
		}

		f := fix{old: name, new_: new_}
		fmt.Sscanf(m[2]+" "+m[3], "%d %d", &f.line, &f.col)
		byFile[strings.TrimPrefix(m[1], "./")] = append(byFile[strings.TrimPrefix(m[1], "./")], f)
	}

	for path, fixes := range byFile {
		src := strings.Split(string(Throw2(os.ReadFile(path))), "\n")
		sort.Slice(fixes, func(i, j int) bool {
			if fixes[i].line != fixes[j].line {
				return fixes[i].line > fixes[j].line
			}

			return fixes[i].col > fixes[j].col
		})

		for _, fx := range fixes {
			l := src[fx.line-1]
			off := fx.col - 1

			if off+len(fx.old) > len(l) || l[off:off+len(fx.old)] != fx.old {
				continue // stale position from an earlier fix on this line
			}

			src[fx.line-1] = l[:off] + fx.new_ + l[off+len(fx.old):]
			total++
		}

		Throw(os.WriteFile(path, []byte(strings.Join(src, "\n")), 0o644))
	}

	return total, len(build) == 0 && len(vet) == 0
}

// lintCaseConvention reports (without fixing) declarations that violate the
// naming convention `refac case` enforces.
func lintCaseConvention(path string) bool {
	fset := gotoken.NewFileSet()
	src := Throw2(os.ReadFile(path))
	f := Throw2(goparser.ParseFile(fset, path, src, goparser.SkipObjectResolution))

	bad := false
	report := func(pos gotoken.Pos, kind, name string) {
		p := fset.Position(pos)
		fmt.Fprintf(os.Stderr, "refac lint: case-convention: %s:%d: %s %s\n", path, p.Line, kind, name)

		bad = true
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != gotoken.TYPE {
				continue
			}

			for _, spec := range d.Specs {
				ts := spec.(*ast.TypeSpec)

				if n := ts.Name.Name; n[0] >= 'a' && n[0] <= 'z' {
					report(ts.Name.Pos(), "lower-case type", n)
				}

				if it, ok := ts.Type.(*ast.InterfaceType); ok {
					for _, m := range it.Methods.List {
						for _, id := range m.Names {
							if id.Name[0] >= 'A' && id.Name[0] <= 'Z' && !stdlibCaseMethods[id.Name] {
								report(id.Pos(), "upper-case interface method", id.Name)
							}
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil {
				continue
			}

			if n := d.Name.Name; n[0] >= 'A' && n[0] <= 'Z' && !stdlibCaseMethods[n] {
				report(d.Name.Pos(), "upper-case method", n)
			}
		}
	}

	return bad
}
