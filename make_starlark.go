package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cmdMakeStarlark transpiles ya.make files to ya.star (Model A), writing each next to its
// source. Args are source-root-relative dirs (whose ya.make is transpiled) or ya.make
// file paths; --source-root sets the root (default ".").
//
//	ay dev make starlark --source-root /home/pg/monorepo/3 util library/cpp/foo
//
// The transpiler is purely syntactic: each ya.make statement becomes a stmt(NAME, args…)
// frag appended to `body` in exact source order, and IF/ELSEIF/ELSE become Starlark
// if/elif/else over `flags`. The raw argument tokens come straight from the parser (no
// reconstruction from typed Stmts), so buildStmtFor re-derives identical Stmts.
func cmdMakeStarlark(_ GlobalFlags, args []string) int {
	srcRoot := "."

	var targets []string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--source-root" && i+1 < len(args):
			i++
			srcRoot = args[i]
		case strings.HasPrefix(args[i], "--source-root="):
			srcRoot = strings.TrimPrefix(args[i], "--source-root=")
		default:
			targets = append(targets, args[i])
		}
	}

	if len(targets) == 0 {
		throwFmt("dev make starlark: no targets supplied")
	}

	fs := newFS(srcRoot)

	written, skipped, failed := 0, 0, 0

	for _, t := range targets {
		rel := cleanRel(t)

		if !strings.HasSuffix(rel, "ya.make") {
			rel = joinRel(rel, "ya.make")
		}

		if !fs.isFile(srcRootVFS, rel) {
			fmt.Printf("SKIP %s: no such ya.make\n", rel)
			failed++

			continue
		}

		// Per-file failures are reported, not fatal, so a bulk run completes.
		src, hasModule, err := transpileYaMakeFile(fs, rel)
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", rel, err)
			failed++

			continue
		}

		if !hasModule {
			skipped++

			continue
		}

		out := filepath.Join(srcRoot, strings.TrimSuffix(rel, "ya.make")+"ya.star")

		if err := os.WriteFile(out, []byte(src), 0o644); err != nil {
			throwFmt("dev make starlark: write %s: %v", out, err)
		}

		written++
	}

	fmt.Printf("dev make starlark: wrote %d ya.star, skipped %d (no module), failed %d\n", written, skipped, failed)

	return 0
}

// transpileYaMakeFile parses rel and transpiles it to ya.star source. hasModule is false
// for module-less files (pure RECURSE/SET dir ya.make) — those need no ya.star.
func transpileYaMakeFile(fs FS, rel string) (src string, hasModule bool, err error) {
	mf, raw, perr := parseFileWithRaw(fs, rel)
	if perr != nil {
		return "", false, perr
	}

	return transpileToStar(mf.Stmts, raw)
}

// transpileToStar renders the typed statement tree (carrying IF structure) and the flat
// RawCall stream (carrying raw args, aligned 1:1 with the tree's non-IF leaves in
// pre-order) into ya.star source.
func transpileToStar(stmts []Stmt, raw []RawCall) (string, bool, error) {
	cur := &rawCursor{calls: raw}

	var (
		moduleType string
		moduleArgs []STR
		body       strings.Builder
		found      bool
	)

	for _, s := range stmts {
		switch s.(type) {
		case *ModuleStmt:
			rc := cur.next()
			moduleType = rc.Name
			moduleArgs = rc.Args
			found = true
		case *EndStmt:
			cur.next()

			// Statements after END (RECURSE, …) do not reach the module graph.
			return renderStarModule(moduleType, moduleArgs, body.String()), true, nil
		default:
			// Pre-module statements (e.g. ENABLE(PYBUILD_NO_PY) before PY3_LIBRARY())
			// carry module-data side effects and must be emitted; they land at the head
			// of `body`, which collectStmts processes before the emit phase reads them.
			if perr := renderBodyStmt(s, cur, &body, 0); perr != nil {
				return "", false, perr
			}
		}
	}

	if !found {
		return "", false, nil
	}

	// A module with no END (malformed) — emit what we have.
	return renderStarModule(moduleType, moduleArgs, body.String()), true, nil
}

// rawCursor walks the flat RawCall stream in lockstep with a pre-order traversal of the
// typed tree's non-IF leaves.
type rawCursor struct {
	calls []RawCall
	pos   int
}

func (c *rawCursor) next() RawCall {
	rc := c.calls[c.pos]
	c.pos++

	return rc
}

// renderBodyStmt emits one body statement at the given indent: a leaf becomes
// `body += stmt(NAME, args…)`; an IfStmt becomes a Starlark if/elif/else block.
func renderBodyStmt(s Stmt, cur *rawCursor, b *strings.Builder, indent int) error {
	iff, ok := s.(*IfStmt)
	if !ok {
		rc := cur.next()
		writeIndent(b, indent)
		b.WriteString("body += ")
		b.WriteString(renderLeafCall(rc))
		b.WriteByte('\n')

		return nil
	}

	return renderIf(iff, cur, b, indent, "if ")
}

// renderIf emits an IF chain. An ELSEIF lowers to a nested IfStmt in Else; we render it as
// Starlark `elif` to keep the source flat.
func renderIf(iff *IfStmt, cur *rawCursor, b *strings.Builder, indent int, keyword string) error {
	cond, err := renderCond(iff.Cond)
	if err != nil {
		return err
	}

	writeIndent(b, indent)
	b.WriteString(keyword)
	b.WriteString(cond)
	b.WriteString(":\n")

	if err := renderBlock(iff.Then, cur, b, indent+1); err != nil {
		return err
	}

	if iff.Else == nil {
		return nil
	}

	// `ELSEIF` parses to Else == []Stmt{<nested IfStmt>}; render it as `elif`.
	if len(iff.Else) == 1 {
		if nested, ok := iff.Else[0].(*IfStmt); ok {
			return renderIf(nested, cur, b, indent, "elif ")
		}
	}

	writeIndent(b, indent)
	b.WriteString("else:\n")

	return renderBlock(iff.Else, cur, b, indent+1)
}

// renderBlock emits a block body, substituting `pass` for an empty branch.
func renderBlock(stmts []Stmt, cur *rawCursor, b *strings.Builder, indent int) error {
	if len(stmts) == 0 {
		writeIndent(b, indent)
		b.WriteString("pass\n")

		return nil
	}

	for _, s := range stmts {
		if err := renderBodyStmt(s, cur, b, indent); err != nil {
			return err
		}
	}

	return nil
}

// renderLeafCall renders one body statement. Flag-mutating macros (ENABLE/DISABLE/SET/
// DEFAULT) become their dedicated builtins so they update the eval-time overlay a later
// condition reads; everything else is a generic stmt().
func renderLeafCall(rc RawCall) string {
	a := rc.Args

	switch rc.Name {
	case "ENABLE":
		return "enable(" + renderStrList(a) + ")"
	case "DISABLE":
		return "disable(" + renderStrList(a) + ")"
	case "SET":
		if len(a) == 0 {
			break
		}

		return "set_var(" + strconv.Quote(a[0].string()) + ", " + renderStrList(a[1:]) + ")"
	case "DEFAULT":
		if len(a) == 0 {
			break
		}

		val := ""
		if len(a) > 1 {
			val = a[1].string()
		}

		return "default_var(" + strconv.Quote(a[0].string()) + ", " + strconv.Quote(val) + ")"
	}

	if len(a) == 0 {
		return "stmt(" + strconv.Quote(rc.Name) + ")"
	}

	return "stmt(" + strconv.Quote(rc.Name) + ", " + renderStrList(a) + ")"
}

// renderStrList renders a []STR as a Starlark list literal.
func renderStrList(ss []STR) string {
	var b strings.Builder

	b.WriteByte('[')

	for i, s := range ss {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString(strconv.Quote(s.string()))
	}

	b.WriteByte(']')

	return b.String()
}

// renderStarModule assembles the final ya.star: the on() truthiness helper, the body
// accumulator, and the module rule call.
func renderStarModule(moduleType string, moduleArgs []STR, body string) string {
	var b strings.Builder

	b.WriteString("# Generated by `ay dev make starlark` from ya.make — do not edit.\n\n")
	b.WriteString("def on(v):\n")
	b.WriteString("    return v != \"\" and v.lower() not in [\"false\", \"f\", \"no\", \"n\", \"off\", \"0\", \"net\"]\n\n")
	b.WriteString("body = []\n")
	b.WriteString(body)
	b.WriteByte('\n')
	b.WriteString(strings.ToLower(moduleType))
	b.WriteByte('(')

	for _, a := range moduleArgs {
		b.WriteString(strconv.Quote(a.string()))
		b.WriteString(", ")
	}

	b.WriteString("body = body)\n")

	return b.String()
}

// renderCond renders an IF condition Expr as a Starlark boolean expression over `flags`,
// mirroring evalCond: a bare identifier is a truthiness test (on(flags.X)); comparisons
// compare the string forms (flags values are strings).
func renderCond(e Expr) (string, error) {
	switch x := e.(type) {
	case *ExprIdent:
		switch x.Name {
		case "yes":
			return "True", nil
		case "no":
			return "False", nil
		}

		return "on(flags." + x.Name + ")", nil
	case *ExprNot:
		of, err := renderCond(x.Of)
		if err != nil {
			return "", err
		}

		return "not (" + of + ")", nil
	case *ExprAnd:
		return renderBinaryCond(x.Left, x.Right, " and ")
	case *ExprOr:
		return renderBinaryCond(x.Left, x.Right, " or ")
	case *ExprEq:
		return renderCmp(x.Left, x.Right, " == ")
	case *ExprLt:
		return renderCmp(x.Left, x.Right, " < ")
	case *ExprStartsWith:
		l, err := renderAtom(x.Left)
		if err != nil {
			return "", err
		}

		r, err := renderAtom(x.Right)
		if err != nil {
			return "", err
		}

		return l + ".startswith(" + r + ")", nil
	}

	return "", fmt.Errorf("unhandled condition Expr %T", e)
}

func renderBinaryCond(left, right Expr, op string) (string, error) {
	l, err := renderCond(left)
	if err != nil {
		return "", err
	}

	r, err := renderCond(right)
	if err != nil {
		return "", err
	}

	return "(" + l + op + r + ")", nil
}

func renderCmp(left, right Expr, op string) (string, error) {
	l, err := renderAtom(left)
	if err != nil {
		return "", err
	}

	r, err := renderAtom(right)
	if err != nil {
		return "", err
	}

	return l + op + r, nil
}

// renderAtom renders a comparison operand: an identifier is its flags string value; a
// string/int literal is a quoted string (ya.make values are strings — an int compares by
// its decimal form, matching evalEq).
func renderAtom(e Expr) (string, error) {
	switch x := e.(type) {
	case *ExprIdent:
		// Comparison operands take evalAtom semantics: an unbound identifier is its own
		// literal name (e.g. RHS of MODULE_KIND == PROTO_LIBRARY), not a truthiness test.
		return "atom(" + strconv.Quote(x.Name) + ")", nil
	case *ExprString:
		return strconv.Quote(x.Value), nil
	case *ExprInt:
		return strconv.Quote(strconv.Itoa(x.Value)), nil
	}

	return "", fmt.Errorf("unhandled atom Expr %T", e)
}

func writeIndent(b *strings.Builder, indent int) {
	for i := 0; i < indent; i++ {
		b.WriteString("    ")
	}
}
