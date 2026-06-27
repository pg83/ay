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

var stdlibCaseMethods = map[string]bool{
	"String":        true,
	"Error":         true,
	"MarshalJSON":   true,
	"UnmarshalJSON": true,
	"Len":           true,
	"Less":          true,
	"Swap":          true,
	"Push":          true,
	"Pop":           true,
	"Unwrap":        true,
}

var predeclaredIdents = map[string]bool{
	"new": true, "make": true, "len": true, "cap": true, "copy": true,
	"append": true, "delete": true, "clear": true, "close": true,
	"panic": true, "recover": true, "print": true, "println": true,
	"min": true, "max": true, "complex": true, "real": true, "imag": true,
	"true": true, "false": true, "nil": true, "iota": true,
	"bool": true, "byte": true, "rune": true, "error": true, "string": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "float32": true, "float64": true, "complex64": true,
	"complex128": true, "any": true, "comparable": true,
}

var caseErrRe = regexp.MustCompile(`^([^:\s]+\.go):(\d+):(\d+): (?:undefined: (\w+)|\S+ undefined \(.*has no field or method (\w+))`)

func refacCase(_ GlobalFlags, args []string) int {
	files := goFilesFromArgs(args)
	typeRen := map[string]string{}
	methodRen := map[string]string{}
	forbidden := forbiddenLowerNames(files)

	for _, path := range files {
		renameCaseDecls(path, typeRen, methodRen, forbidden)
	}

	fmt.Fprintf(os.Stderr, "refac case: renamed decls: %d types, %d method names\n", len(typeRen), len(methodRen))

	for round := 0; ; round++ {
		if round > 500 {
			throwFmt("refac case: no fixpoint after %d rounds", round)
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

func forbiddenLowerNames(files []string) map[string]bool {
	out := make(map[string]bool, len(predeclaredIdents)+32)

	for n := range predeclaredIdents {
		out[n] = true
	}

	for _, path := range files {
		fset := gotoken.NewFileSet()
		f := throw2(goparser.ParseFile(fset, path, throw2(os.ReadFile(path)), goparser.ImportsOnly))

		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)

			if imp.Name != nil {
				out[imp.Name.Name] = true
			} else if i := strings.LastIndexByte(p, '/'); i >= 0 {
				out[p[i+1:]] = true
			} else {
				out[p] = true
			}
		}
	}

	return out
}

func renameCaseDecls(path string, typeRen, methodRen map[string]string, forbidden map[string]bool) {
	fset := gotoken.NewFileSet()
	src := throw2(os.ReadFile(path))
	f := throw2(goparser.ParseFile(fset, path, src, goparser.SkipObjectResolution))

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
			name := d.Name.Name

			if name[0] < 'A' || name[0] > 'Z' {
				continue
			}

			if d.Recv == nil {
				if forbidden[lower(name)] {
					fmt.Fprintf(os.Stderr, "refac case: %s: function %s lowers to reserved %q — rename manually\n", path, name, lower(name))

					continue
				}

				typeRen[name] = lower(name)
				add(d.Name, typeRen[name])

				continue
			}

			if !stdlibCaseMethods[name] {
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
			throwFmt("refac case: %s: offset %d holds %q, want %q", path, e.off, out[e.off:e.off+min(len(e.old), 20)], e.old)
		}

		out = out[:e.off] + e.new_ + out[e.off+len(e.old):]
	}

	throw(os.WriteFile(path, []byte(out), 0o644))
}

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
		src := strings.Split(string(throw2(os.ReadFile(path))), "\n")
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
				continue
			}

			src[fx.line-1] = l[:off] + fx.new_ + l[off+len(fx.old):]
			total++
		}

		throw(os.WriteFile(path, []byte(strings.Join(src, "\n")), 0o644))
	}

	return total, len(build) == 0 && len(vet) == 0
}

func lintCaseConvention(path string) bool {
	fset := gotoken.NewFileSet()
	src := throw2(os.ReadFile(path))
	f := throw2(goparser.ParseFile(fset, path, src, goparser.SkipObjectResolution))
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
			n := d.Name.Name

			if n[0] < 'A' || n[0] > 'Z' {
				continue
			}

			if d.Recv == nil {
				report(d.Name.Pos(), "upper-case function", n)

				continue
			}

			if !stdlibCaseMethods[n] {
				report(d.Name.Pos(), "upper-case method", n)

				continue
			}

			if !isInterfaceWrapper(d) {
				report(d.Name.Pos(), "non-wrapper upper-case stdlib method", n)
			}
		}
	}

	return bad
}

func isInterfaceWrapper(d *ast.FuncDecl) bool {
	if d.Body == nil || len(d.Body.List) != 1 || d.Recv == nil ||
		len(d.Recv.List) != 1 || len(d.Recv.List[0].Names) != 1 {
		return false
	}

	var call *ast.CallExpr

	switch st := d.Body.List[0].(type) {
	case *ast.ReturnStmt:
		if len(st.Results) != 1 {
			return false
		}

		c, ok := st.Results[0].(*ast.CallExpr)

		if !ok {
			return false
		}

		call = c
	case *ast.ExprStmt:
		c, ok := st.X.(*ast.CallExpr)

		if !ok {
			return false
		}

		call = c
	default:
		return false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)

	if !ok {
		return false
	}

	recv, ok := sel.X.(*ast.Ident)

	if !ok {
		return false
	}

	name := d.Name.Name

	return recv.Name == d.Recv.List[0].Names[0].Name &&
		sel.Sel.Name == strings.ToLower(name[:1])+name[1:]
}
