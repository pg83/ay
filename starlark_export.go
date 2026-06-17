package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// cmdMakeStarlark transpiles ya.make files to ya.star (Model A), writing each next to its
// source. Args are source-root-relative dirs (whose ya.make is transpiled) or ya.make
// file paths; --source-root sets the root (default ".").
//
//	ay dev make starlark --source-root /home/pg/monorepo/3 util library/cpp/foo
//
// The transpiler emits idiomatic Model A: attribute kwargs (srcs/peerdir/cflags/…),
// generators in srcs (run_program/join_srcs/…), flag mutators (enable/set_var/…), and
// IF/ELSEIF/ELSE as Starlark if/elif/else over `flags`. Raw argument tokens come straight
// from the parser (no reconstruction from typed Stmts). Modules in stmtFallbackDirs, whose
// declarative form is not byte-exact, fall back to the exact-order stmt()-body form.
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

	if os.Getenv("AY_PRETTY_WHY") != "" {
		prettyReasons = map[string]int{}
		defer func() {
			type kv struct {
				k string
				n int
			}

			var rs []kv
			for k, n := range prettyReasons {
				rs = append(rs, kv{k, n})
			}

			sort.Slice(rs, func(i, j int) bool { return rs[i].n > rs[j].n })

			for _, r := range rs {
				fmt.Printf("WHY %-28s %d\n", r.k, r.n)
			}
		}()
	}

	fs := newFS(srcRoot)

	// With no explicit targets, transpile every ya.make under the source root — the build
	// closure decides which modules participate; enumerating them is not this tool's job.
	if len(targets) == 0 {
		fs.walk("", func(rel string, isDir bool) bool {
			if !isDir && (rel == "ya.make" || strings.HasSuffix(rel, "/ya.make")) {
				targets = append(targets, rel)
			}

			return true
		})
	}

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

// stmtFallbackDirs lists module dirs whose idiomatic declarative form is not byte-exact
// (the declarative kwarg form merges repeated statements and reorders by kind, which a few
// boundary/order-sensitive modules — e.g. ADDINCL GLOBAL ordering feeding a transitive
// enum-serialization scan — cannot tolerate). They fall back to the exact-order stmt()-body
// form. Driving this set to empty is the remaining work toward zero stmt() everywhere.
var stmtFallbackDirs = map[string]bool{
	"contrib/deprecated/python/pymongo":       true,
	"contrib/python/Pygments/py3":             true,
	"contrib/python/anyio":                    true,
	"contrib/python/click/py3":                true,
	"contrib/python/cloudpickle/py3":          true,
	"contrib/python/dnspython/py3":            true,
	"contrib/python/docutils/py3":             true,
	"contrib/python/fastapi":                  true,
	"contrib/python/future/py3":               true,
	"contrib/python/httpcore":                 true,
	"contrib/python/httpx":                    true,
	"contrib/python/humanfriendly/py3":        true,
	"contrib/python/jaraco.text":              true,
	"contrib/python/kazoo/py3":                true,
	"contrib/python/paramiko/py3":             true,
	"contrib/python/portalocker/py3":          true,
	"contrib/python/psutil/py3":               true,
	"contrib/python/py/py3":                   true,
	"contrib/python/pycparser/py3":            true,
	"contrib/python/pydantic/pydantic-2":      true,
	"contrib/python/rich":                     true,
	"contrib/python/setuptools/py3":           true,
	"contrib/python/shellingham":              true,
	"contrib/python/simplejson/py3":           true,
	"contrib/python/starlette":                true,
	"contrib/python/tenacity/py3":             true,
	"contrib/python/tqdm/py3":                 true,
	"contrib/python/urllib3/py3":              true,
	"contrib/python/uvicorn":                  true,
	"contrib/python/wheel":                    true,
	"contrib/tools/python3/lib2/py":           true,
	"devtools/ya/bin":                         true,
	"devtools/ya/build/node_checks":           true,
	"devtools/ya/exts":                        true,
	"devtools/ya/handlers/dump/debug":         true,
	"devtools/ya/package":                     true,
	"devtools/ya/yalibrary/runner/sandboxing": true,
	"library/cpp/tvmauth":                     true,
	"library/cpp/tvmauth/_/deprecated":        true,
	"library/cpp/tvmauth/_/src":               true,
	"library/python/coredump_filter":          true,
	"library/python/find_root":                true,
	"sandbox/common/auth":                     true,
	"sandbox/common/config":                   true,
	"sandbox/common/data":                     true,
	"sandbox/common/encoding":                 true,
	"sandbox/common/enum":                     true,
	"sandbox/common/format":                   true,
	"sandbox/common/itertools":                true,
	"sandbox/common/mds/compression":          true,
	"sandbox/common/patterns":                 true,
	"yt/python/yt/entry":                      true,
	"yt/python/yt/packages":                   true,
	"yt/python/yt/wrapper":                    true,
}

// transpileYaMakeFile parses rel and transpiles it to ya.star source. hasModule is false
// for module-less files (pure RECURSE/SET dir ya.make) — those need no ya.star.
func transpileYaMakeFile(fs FS, rel string) (src string, hasModule bool, err error) {
	mf, raw, perr := parseFileWithRaw(fs, rel)
	if perr != nil {
		return "", false, perr
	}

	dir := strings.TrimSuffix(strings.TrimSuffix(rel, "ya.make"), "/")

	return transpileToStarMode(mf.Stmts, raw, stmtFallbackDirs[dir])
}

// seg is one segment of an attribute's value expression (segments are joined with " + ").
// render takes the base indent (the column the closing bracket aligns to).
type seg interface {
	render(base int) string
}

// listSeg is a plain list literal, rendered vertically (one element per line) when it has
// more than one element.
type listSeg struct{ items []STR }

func (l listSeg) render(base int) string { return renderList(l.items, base) }

// rawSeg is an already-formatted inline expression (a non-join generator call).
type rawSeg struct{ s string }

func (r rawSeg) render(int) string { return r.s }

// joinSeg is a JOIN_SRCS generator with its sources list rendered vertically.
type joinSeg struct {
	out     STR
	sources []STR
}

func (j joinSeg) render(base int) string {
	return "join_srcs(" + strconv.Quote(j.out.string()) + ", " + renderList(j.sources, base) + ")"
}

// condSeg is an inline conditional (from an IF): `(then if cond else else)`.
type condSeg struct {
	cond string
	then []seg
	els  []seg
}

func (c condSeg) render(base int) string {
	return "(" + renderSegs(c.then, base) + " if " + c.cond + " else " + renderSegs(c.els, base) + ")"
}

// renderSegs joins an attribute's segments into its value expression at the given indent.
func renderSegs(segs []seg, base int) string {
	if len(segs) == 0 {
		return "[]"
	}

	parts := make([]string, len(segs))
	for i, g := range segs {
		parts[i] = g.render(base)
	}

	return strings.Join(parts, " + ")
}

// renderList renders a list literal: vertical (one element per line, trailing comma) for
// two or more elements, inline otherwise.
func renderList(items []STR, base int) string {
	switch len(items) {
	case 0:
		return "[]"
	case 1:
		return "[" + strconv.Quote(items[0].string()) + "]"
	}

	var b strings.Builder

	b.WriteString("[\n")

	for _, it := range items {
		b.WriteString(strings.Repeat(" ", base+4) + strconv.Quote(it.string()) + ",\n")
	}

	b.WriteString(strings.Repeat(" ", base) + "]")

	return b.String()
}

// segBuilder accumulates one attribute's segments, merging contiguous plain-list items
// into one literal and keeping generator/conditional segments separate, in source order.
type segBuilder struct {
	segs []seg
	pend []STR
}

func (s *segBuilder) addItems(items []STR) { s.pend = append(s.pend, items...) }

func (s *segBuilder) flush() {
	if len(s.pend) > 0 {
		s.segs = append(s.segs, listSeg{s.pend})
		s.pend = nil
	}
}

func (s *segBuilder) addSeg(g seg) {
	s.flush()
	s.segs = append(s.segs, g)
}

func (s *segBuilder) finalize() []seg {
	s.flush()

	return s.segs
}

// collectCondRefs gathers every identifier that appears in any IF condition of the module
// (recursively). A SET/ENABLE/DISABLE whose variable is in this set gates a condition, so
// it cannot be hoisted into a kwarg (the eager `on(flags.X)`/`atom()` would not see the
// mutation) — such modules keep the overlay-ordered accumulator form.
func collectCondRefs(stmts []Stmt) map[string]bool {
	refs := map[string]bool{}

	var expr func(e Expr)

	expr = func(e Expr) {
		switch x := e.(type) {
		case *ExprIdent:
			refs[x.Name] = true
		case *ExprNot:
			expr(x.Of)
		case *ExprAnd:
			expr(x.Left)
			expr(x.Right)
		case *ExprOr:
			expr(x.Left)
			expr(x.Right)
		case *ExprEq:
			expr(x.Left)
			expr(x.Right)
		case *ExprLt:
			expr(x.Left)
			expr(x.Right)
		case *ExprStartsWith:
			expr(x.Left)
			expr(x.Right)
		}
	}

	var walk func(ss []Stmt)

	walk = func(ss []Stmt) {
		for _, s := range ss {
			if iff, ok := s.(*IfStmt); ok {
				expr(iff.Cond)
				walk(iff.Then)
				walk(iff.Else)
			}
		}
	}

	walk(stmts)

	return refs
}

// prettyState collects a module (or one IF branch): list/srcs attributes as segment
// builders, toggles as booleans, SET vars as a dict, all in first-use order.
type prettyState struct {
	seg      map[string]*segBuilder
	toggle   map[string]bool
	setVars  map[string]string // SET name → value (rendered as one `set = {…}` kwarg)
	setOrder []string          // SET var names in source order
	order    []string
	condRefs map[string]bool // identifiers read by an IF condition (shared with sub-branches)
}

func newPrettyState() *prettyState {
	return &prettyState{seg: map[string]*segBuilder{}, toggle: map[string]bool{}, setVars: map[string]string{}}
}

func (p *prettyState) setVar(name, val string) {
	if _, ok := p.setVars[name]; !ok {
		if len(p.setOrder) == 0 {
			p.order = append(p.order, "set")
		}

		p.setOrder = append(p.setOrder, name)
	}

	p.setVars[name] = val
}

func (p *prettyState) segOf(name string) *segBuilder {
	s := p.seg[name]
	if s == nil {
		s = &segBuilder{}
		p.seg[name] = s
		p.order = append(p.order, name)
	}

	return s
}

func (p *prettyState) setToggle(name string) {
	if !p.toggle[name] {
		p.toggle[name] = true
		p.order = append(p.order, name)
	}
}

func (p *prettyState) segsOf(name string) []seg {
	if s := p.seg[name]; s != nil {
		return s.finalize()
	}

	return nil
}

func recordPrettyReason(reason string) bool {
	if prettyReasons != nil {
		prettyReasons[reason]++
	}

	return false
}

// collectOne appends one statement to p. An IF becomes a per-attribute Starlark
// conditional expression (`(then if cond else else)`, nested for ELSEIF) — each branch is
// collected into its own state and merged segment-wise, so order is preserved. Returns
// false (caller falls back) for a construct the single-call form cannot express: a flag
// mutator, a boundary macro, an unmapped macro, or a toggle inside an IF.
func collectOne(p *prettyState, s Stmt, cur *rawCursor, topLevel bool) bool {
	if iff, ok := s.(*IfStmt); ok {
		cond, err := renderCond(iff.Cond)
		if err != nil {
			return recordPrettyReason("cond")
		}

		thenP := newPrettyState()
		thenP.condRefs = p.condRefs

		if !collectBlock(thenP, iff.Then, cur, false) {
			return false
		}

		elseP := newPrettyState()
		elseP.condRefs = p.condRefs

		if !collectBlock(elseP, iff.Else, cur, false) {
			return false
		}

		names := append([]string(nil), thenP.order...)

		for _, n := range elseP.order {
			if _, seen := thenP.seg[n]; !seen {
				names = append(names, n)
			}
		}

		for _, name := range names {
			p.segOf(name).addSeg(condSeg{cond: cond, then: thenP.segsOf(name), els: elseP.segsOf(name)})
		}

		return true
	}

	rc := cur.next()
	n, a := rc.Name, rc.Args

	switch {
	case n == "SRCS":
		p.segOf("srcs").addItems(a)
	case n == "SET":
		// SET hoists to a `set = {…}` kwarg only at top level and only when no condition
		// reads the variable (otherwise the eager IF would miss the mutation).
		if !topLevel || len(a) == 0 || p.condRefs[a[0].string()] {
			return recordPrettyReason("mutator:SET")
		}

		p.setVar(a[0].string(), strings.Join(strStrings(a[1:]), " "))
	case n == "ENABLE" || n == "DISABLE":
		if !topLevel {
			return recordPrettyReason("mutator:" + n)
		}

		for _, nm := range a {
			if p.condRefs[nm.string()] {
				return recordPrettyReason("mutator:" + n)
			}
		}

		p.segOf(strings.ToLower(n)).addItems(a)
	case n == "DEFAULT":
		return recordPrettyReason("mutator:DEFAULT")
	case n == "JOIN_SRCS" && len(a) > 0:
		p.segOf("srcs").addSeg(joinSeg{out: a[0], sources: a[1:]})
	default:
		if _, ok := boundaryMacroSet[n]; ok {
			return recordPrettyReason("boundary:" + n)
		}

		if gen, ok := generatorCall(n, a); ok {
			p.segOf("srcs").addSeg(rawSeg{gen})

			return true
		}

		spec, ok := declMacroAttr[n]
		if !ok {
			return recordPrettyReason("unmapped:" + n)
		}

		if spec.kind == attrToggle {
			if !topLevel {
				return recordPrettyReason("toggle-in-if:" + n)
			}

			p.setToggle(spec.kw)
		} else {
			p.segOf(spec.kw).addItems(a)
		}
	}

	return true
}

// collectBlock appends every statement of a block to p (topLevel allows toggles).
func collectBlock(p *prettyState, stmts []Stmt, cur *rawCursor, topLevel bool) bool {
	for _, s := range stmts {
		if !collectOne(p, s, cur, topLevel) {
			return false
		}
	}

	return true
}

// tryPretty renders the idiomatic single-call form for a module:
//
//	library(
//	    "name",
//	    srcs = ["a.cpp"] + (["lin.cpp"] if on(flags.OS_LINUX) else []),
//	    peerdir = ["contrib/libs/zstd"],
//	    no_util = True,
//	)
//
// Conditionals lower to inline expressions; it returns ok=false (caller falls back to the
// accumulator/body form) only for constructs the single call cannot express — a flag
// mutator, a boundary macro, an unmapped macro, or a toggle inside an IF. The emitted
// statement stream is identical to the declarative form (same kwargs, same order, same
// srcs boundaries), so the build graph is unchanged.
//
// prettyReasons, when non-nil, tallies why modules fall out of the pretty form (set via
// AY_PRETTY_WHY for one-off analysis).
var prettyReasons map[string]int

func bailPretty(reason string) (string, bool) {
	recordPrettyReason(reason)

	return "", false
}

func tryPretty(stmts []Stmt, raw []RawCall) (string, bool) {
	cur := &rawCursor{calls: raw}
	p := newPrettyState()
	p.condRefs = collectCondRefs(stmts)

	var (
		moduleType string
		moduleArgs []STR
		found      bool
		ended      bool
	)

	for _, s := range stmts {
		if ended {
			break
		}

		switch s.(type) {
		case *ModuleStmt:
			rc := cur.next()
			moduleType = rc.Name
			moduleArgs = rc.Args
			found = true
		case *EndStmt:
			cur.next()
			ended = true
		default:
			// Pre-module and body statements are collected the same way: attributes hoist
			// into the single call (a pre-module SUBSCRIBER, etc., is position-independent,
			// exactly as the accumulator form already relocates it).
			if !collectOne(p, s, cur, true) {
				return "", false
			}
		}
	}

	if !found {
		return bailPretty("no-module")
	}

	return renderPretty(moduleType, moduleArgs, p), true
}

// renderPretty assembles the single-call pretty form, omitting empty list attributes.
func renderPretty(moduleType string, moduleArgs []STR, p *prettyState) string {
	var b strings.Builder

	b.WriteString("# Generated by `ay dev make starlark` from ya.make — do not edit.\n\n")
	b.WriteString(strings.ToLower(moduleType))
	b.WriteString("(\n")

	for _, a := range moduleArgs {
		b.WriteString("    " + strconv.Quote(a.string()) + ",\n")
	}

	for _, name := range p.order {
		switch {
		case name == "set":
			b.WriteString("    set = {\n")

			for _, k := range p.setOrder {
				b.WriteString("        " + strconv.Quote(k) + ": " + strconv.Quote(p.setVars[k]) + ",\n")
			}

			b.WriteString("    },\n")
		case p.toggle[name]:
			b.WriteString("    " + name + " = True,\n")
		default:
			expr := renderSegs(p.segsOf(name), 4)
			if expr == "[]" {
				continue // empty attrArgs → omitted, matching emitAttr
			}

			b.WriteString("    " + name + " = " + expr + ",\n")
		}
	}

	b.WriteString(")\n")

	return b.String()
}

// transpileToStar renders the typed statement tree and the flat RawCall stream into
// idiomatic Model A ya.star, choosing the prettiest form that stays byte-exact (see
// transpileToStarMode): a single module() call with inline lists for a conditional-free
// module, else accumulator variables with if/elif/else, else the exact stmt()-body form.
func transpileToStar(stmts []Stmt, raw []RawCall) (string, bool, error) {
	return transpileToStarMode(stmts, raw, false)
}

// transpileToStarMode renders the ya.star. forceStmt selects the exact-order stmt()-body
// form (byte-exact, every statement a body frag) instead of the idiomatic declarative
// form — used for modules whose declarative kwarg form is not yet byte-exact (the
// declarative form merges repeated statements and groups them by kind, which a few
// boundary/order-sensitive modules cannot tolerate).
func transpileToStarMode(stmts []Stmt, raw []RawCall, forceStmt bool) (string, bool, error) {
	// Prefer the pretty form (a single module() call, inline lists, no empty attributes)
	// for a conditional-free module that maps cleanly to attributes; it emits the same
	// statements as the declarative form, just read like hand-written ya.star. forceStmt
	// modules and anything pretty cannot express fall through to the mechanisms below.
	if !forceStmt {
		if src, ok := tryPretty(stmts, raw); ok {
			return src, true, nil
		}
	}

	cur := &rawCursor{calls: raw}
	tb := &transBuilder{kind: map[string]attrKind{}, forceStmt: forceStmt}

	var (
		moduleType string
		moduleArgs []STR
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

			return tb.render(moduleType, moduleArgs), true, nil
		default:
			// Pre-module statements (e.g. ENABLE(PYBUILD_NO_PY) before PY3_LIBRARY())
			// carry module-data side effects; the mutators emit ahead of the module rule.
			if err := tb.walk(s, cur, 0); err != nil {
				return "", false, err
			}
		}
	}

	if !found {
		return "", false, nil
	}

	return tb.render(moduleType, moduleArgs), true, nil
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

// transBuilder accumulates the imperative ya.star body (attribute appends, toggles, flag
// mutators, conditionals) and the set of attribute variables it touches.
type transBuilder struct {
	lines     []string            // emitted body lines
	kind      map[string]attrKind // attr var -> kind (list/toggle), for declaration & the rule call
	order     []string            // attr vars in first-use order
	forceStmt bool                // render every statement as a body stmt() frag (byte-exact)
}

func (t *transBuilder) use(name string, k attrKind) {
	if _, ok := t.kind[name]; !ok {
		t.kind[name] = k
		t.order = append(t.order, name)
	}
}

func (t *transBuilder) emit(indent int, line string) {
	t.lines = append(t.lines, strings.Repeat("    ", indent)+line)
}

// walk emits one statement: an IfStmt becomes an if/elif/else block, a leaf its attribute
// append / toggle / generator / mutator.
func (t *transBuilder) walk(s Stmt, cur *rawCursor, indent int) error {
	iff, ok := s.(*IfStmt)
	if !ok {
		return t.leaf(cur.next(), indent)
	}

	return t.walkIf(iff, cur, indent, "if ")
}

func (t *transBuilder) walkIf(iff *IfStmt, cur *rawCursor, indent int, keyword string) error {
	cond, err := renderCond(iff.Cond)
	if err != nil {
		return err
	}

	t.emit(indent, keyword+cond+":")

	if err := t.walkBlock(iff.Then, cur, indent+1); err != nil {
		return err
	}

	if iff.Else == nil {
		return nil
	}

	// ELSEIF parses to Else == []Stmt{<nested IfStmt>}; render it as Starlark elif.
	if len(iff.Else) == 1 {
		if nested, ok := iff.Else[0].(*IfStmt); ok {
			return t.walkIf(nested, cur, indent, "elif ")
		}
	}

	t.emit(indent, "else:")

	return t.walkBlock(iff.Else, cur, indent+1)
}

func (t *transBuilder) walkBlock(stmts []Stmt, cur *rawCursor, indent int) error {
	if len(stmts) == 0 {
		t.emit(indent, "pass")

		return nil
	}

	for _, s := range stmts {
		if err := t.walk(s, cur, indent); err != nil {
			return err
		}
	}

	return nil
}

// leaf emits the ya.star for one non-IF statement. In forceStmt mode every statement is a
// body frag (exact order, byte-exact); otherwise it is mapped to its declarative form.
func (t *transBuilder) leaf(rc RawCall, indent int) error {
	if t.forceStmt {
		t.use("body", attrArgs)
		t.emit(indent, "body += "+renderLeafCall(rc))

		return nil
	}

	n, a := rc.Name, rc.Args

	// Flag mutators stay frags in `body` (preserving their overlay side effect when
	// called, plus the emitted stmt for collectStmts) — they are not stmt(), so they do
	// not count against the zero-stmt() goal.
	switch n {
	case "SRCS":
		t.use("srcs", attrArgs)
		t.emit(indent, "srcs += "+renderList(a, indent*4))

		return nil
	case "ENABLE", "DISABLE", "SET", "DEFAULT":
		t.use("body", attrArgs)
		t.emit(indent, "body += "+renderLeafCall(rc))

		return nil
	}

	// Boundary-sensitive macros: one frag per statement in body (never merged); list arg
	// to clear the 255-positional-call limit (RESOURCE_FILES can list hundreds of files).
	if _, ok := boundaryMacroSet[n]; ok {
		t.use("body", attrArgs)
		t.emit(indent, "body += "+strings.ToLower(n)+"("+renderStrList(a)+")")

		return nil
	}

	if gen, ok := generatorCall(n, a); ok {
		t.use("srcs", attrArgs)
		t.emit(indent, "srcs += "+gen)

		return nil
	}

	if spec, ok := declMacroAttr[n]; ok {
		switch spec.kind {
		case attrToggle:
			t.use(spec.kw, attrToggle)
			t.emit(indent, spec.kw+" = True")
		case attrArgs:
			t.use(spec.kw, attrArgs)
			t.emit(indent, spec.kw+" += "+renderList(a, indent*4))
		}

		return nil
	}

	// No declarative mapping: fall back to a stmt() frag in `body`.
	t.use("body", attrArgs)
	t.emit(indent, "body += "+renderLeafCall(rc))

	return nil
}

// renderLeafCall renders one statement as a body frag: flag mutators become their
// dedicated builtins (overlay side effect), everything else a generic stmt(). This is the
// exact-order byte-exact form (forceStmt) and the declarative form's fallback.
func renderLeafCall(rc RawCall) string {
	a := rc.Args

	switch rc.Name {
	case "ENABLE":
		return "enable(" + renderStrList(a) + ")"
	case "DISABLE":
		return "disable(" + renderStrList(a) + ")"
	case "SET":
		if len(a) > 0 {
			return "set_var(" + strconv.Quote(a[0].string()) + ", " + renderStrList(a[1:]) + ")"
		}
	case "DEFAULT":
		if len(a) > 0 {
			val := ""
			if len(a) > 1 {
				val = a[1].string()
			}

			return "default_var(" + strconv.Quote(a[0].string()) + ", " + strconv.Quote(val) + ")"
		}
	}

	if len(a) == 0 {
		return "stmt(" + strconv.Quote(rc.Name) + ")"
	}

	return "stmt(" + strconv.Quote(rc.Name) + ", " + renderStrList(a) + ")"
}

// render assembles the ya.star: attribute-variable declarations, the imperative body, and
// the module rule call passing every touched attribute. The on()/flags/atom helpers are
// predeclared by the runtime (evalStar), so no per-file prelude is emitted.
func (t *transBuilder) render(moduleType string, moduleArgs []STR) string {
	var b strings.Builder

	b.WriteString("# Generated by `ay dev make starlark` from ya.make — do not edit.\n\n")

	for _, name := range t.order {
		if t.kind[name] == attrToggle {
			b.WriteString(name + " = False\n")
		} else {
			b.WriteString(name + " = []\n")
		}
	}

	if len(t.order) > 0 {
		b.WriteByte('\n')
	}

	for _, line := range t.lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	b.WriteString(strings.ToLower(moduleType))
	b.WriteByte('(')

	for _, a := range moduleArgs {
		b.WriteString(strconv.Quote(a.string()))
		b.WriteString(", ")
	}

	for i, name := range t.order {
		if i > 0 {
			b.WriteString(", ")
		}

		b.WriteString(name + " = " + name)
	}

	b.WriteString(")\n")

	return b.String()
}

// declMacroAttr inverts starAttrs (keyword → macro) to macro → (keyword, kind), so the
// transpiler maps a ya.make macro to its declarative rule attribute.
var declMacroAttr = func() map[string]struct {
	kw   string
	kind attrKind
} {
	m := make(map[string]struct {
		kw   string
		kind attrKind
	}, len(starAttrs))

	for kw, spec := range starAttrs {
		m[spec.macro] = struct {
			kw   string
			kind attrKind
		}{kw, spec.kind}
	}

	return m
}()

// srcGenMacros are the per-file source-generating macros exposed as positional frag
// builtins (registered in evalStar) and composed into srcs by the transpiler: SRC, the
// SIMD SRC_C_* variants, COPY_FILE(_WITH_CONTEXT), ARCHIVE. Each delegates to buildStmtFor,
// so the emitted Stmt is identical to the parsed ya.make.
var srcGenMacros = func() []string {
	out := []string{"SRC", "COPY_FILE", "COPY_FILE_WITH_CONTEXT", "ARCHIVE", "BUILDWITH_CYTHON_CPP", "BISON_GEN_C"}
	for m := range simdVariants {
		out = append(out, m)
	}

	return out
}()

// boundaryMacros must stay one frag per statement (no kwarg merge): RESOURCE_FILES /
// RESOURCE batch per statement (each emits a distinct objcopy group); INDUCED_DEPS groups
// its files under a leading extension key, so merging two would mash the groups. The
// transpiler emits each as `body += <macro>(args)`.
var boundaryMacros = []string{"RESOURCE_FILES", "RESOURCE", "INDUCED_DEPS"}

var boundaryMacroSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(boundaryMacros))
	for _, n := range boundaryMacros {
		m[n] = struct{}{}
	}

	return m
}()

var srcGenMacroSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(srcGenMacros))
	for _, n := range srcGenMacros {
		m[n] = struct{}{}
	}

	return m
}()

// generatorCall renders a source-generating macro as its ya.star builtin call (composed
// into srcs), or returns ok=false when the macro is not a generator.
func generatorCall(name string, a []STR) (string, bool) {
	if _, ok := srcGenMacroSet[name]; ok {
		return strings.ToLower(name) + "(" + renderStrArgs(a) + ")", true
	}

	switch name {
	case "JOIN_SRCS":
		if len(a) == 0 {
			return "", false
		}

		return "join_srcs(" + strconv.Quote(a[0].string()) + ", " + renderStrList(a[1:]) + ")", true
	case "GENERATE_ENUM_SERIALIZATION":
		return "enum_serialization(" + strconv.Quote(a[0].string()) + ")", true
	case "GENERATE_ENUM_SERIALIZATION_WITH_HEADER":
		return "enum_serialization_with_header(" + strconv.Quote(a[0].string()) + ")", true
	case "GENERATE_ENUM_SERIALIZATION_NOUTF":
		return "enum_serialization_noutf(" + strconv.Quote(a[0].string()) + ")", true
	case "SRC_C_NO_LTO":
		return "src_c_no_lto(" + renderStrArgs(a) + ")", true
	case "CONFIGURE_FILE":
		return "configure_file(" + strconv.Quote(a[0].string()) + ", " + strconv.Quote(a[1].string()) + ")", true
	case "CREATE_BUILDINFO_FOR":
		return "create_buildinfo_for(" + strconv.Quote(a[0].string()) + ")", true
	case "RUN_PROGRAM":
		return runCmdCall("run_program", a, "tool"), true
	case "RUN_PY3_PROGRAM":
		return runCmdCall("run_py3_program", a, "tool"), true
	case "RUN_PYTHON3":
		return runCmdCall("run_python3", a, "script"), true
	case "RUN_ANTLR":
		return runCmdCall("run_antlr", a, ""), true
	case "RUN_ANTLR4":
		return runCmdCall("run_antlr4", a, ""), true
	case "RUN_ANTLR4_CPP":
		return antlrCppCall(a, false), true
	case "RUN_ANTLR4_CPP_SPLIT":
		return antlrCppCall(a, true), true
	case "DECLARE_EXTERNAL_RESOURCE":
		return "declare_external_resource(" + renderStrArgs(a) + ")", true
	case "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE":
		return "declare_external_host_resources_bundle(" + renderStrArgs(a) + ")", true
	case "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON":
		return "declare_external_host_resources_bundle_by_json(" + renderStrArgs(a) + ")", true
	}

	return "", false
}

// runSectionKw maps a RUN_* section keyword to the builtin kwarg it fills. CWD is handled
// separately (a scalar kwarg).
var runSectionKw = map[string]string{
	"IN": "ins", "IN_NOPARSE": "in_noparse", "IN_DEPS": "in_deps",
	"OUT": "outs", "OUT_NOAUTO": "out_noauto",
	"STDOUT": "stdout", "STDOUT_NOAUTO": "stdout",
	"ENV": "env", "OUTPUT_INCLUDES": "output_includes",
	"INDUCED_DEPS": "induced_deps", "TOOL": "tools",
}

// runCmdCall reconstructs a RUN_PROGRAM-shaped call: an optional head positional then the
// keyword sections as kwargs (args= is the leading keyword-less section).
func runCmdCall(builtin string, a []STR, headName string) string {
	i := 0

	var head string

	if headName != "" && len(a) > 0 {
		head = a[0].string()
		i = 1
	}

	secs := map[string][]STR{}

	var order []string

	cur := "args"
	cwd := ""

	for ; i < len(a); i++ {
		t := a[i].string()

		if t == "CWD" {
			cur = "__cwd"

			continue
		}

		if kw, ok := runSectionKw[t]; ok {
			cur = kw

			continue
		}

		if cur == "__cwd" {
			if cwd == "" {
				cwd = t
			}

			continue
		}

		if _, ok := secs[cur]; !ok {
			order = append(order, cur)
		}

		secs[cur] = append(secs[cur], a[i])
	}

	var b strings.Builder

	b.WriteString(builtin)
	b.WriteByte('(')

	first := true

	if headName != "" {
		b.WriteString(strconv.Quote(head))

		first = false
	}

	for _, kw := range order {
		if !first {
			b.WriteString(", ")
		}

		first = false

		b.WriteString(kw + " = " + renderStrList(secs[kw]))
	}

	if cwd != "" {
		if !first {
			b.WriteString(", ")
		}

		b.WriteString("cwd = " + strconv.Quote(cwd))
	}

	b.WriteByte(')')

	return b.String()
}

// antlrCppCall reconstructs run_antlr4_cpp / run_antlr4_cpp_split: the grammar (or lexer +
// parser), the VISITOR/LISTENER toggles, the options (leading keyword-less, cpp only), and
// output_includes.
func antlrCppCall(a []STR, split bool) string {
	var b strings.Builder

	i := 0

	if split {
		b.WriteString("run_antlr4_cpp_split(" + strconv.Quote(a[0].string()) + ", " + strconv.Quote(a[1].string()))
		i = 2
	} else {
		b.WriteString("run_antlr4_cpp(" + strconv.Quote(a[0].string()))
		i = 1
	}

	var options, includes []STR

	visitor, listener := false, false
	cur := "options"

	for ; i < len(a); i++ {
		switch a[i].string() {
		case "VISITOR":
			visitor = true
		case "LISTENER":
			listener = true
		case "NO_LISTENER":
			listener = false
		case "OUTPUT_INCLUDES":
			cur = "includes"
		default:
			if cur == "includes" {
				includes = append(includes, a[i])
			} else {
				options = append(options, a[i])
			}
		}
	}

	if !split && len(options) > 0 {
		b.WriteString(", options = " + renderStrList(options))
	}

	b.WriteString(", visitor = " + boolLit(visitor) + ", listener = " + boolLit(listener))

	if len(includes) > 0 {
		b.WriteString(", output_includes = " + renderStrList(includes))
	}

	b.WriteByte(')')

	return b.String()
}

func boolLit(b bool) string {
	if b {
		return "True"
	}

	return "False"
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

// renderStrArgs renders a []STR as comma-separated positional string arguments.
func renderStrArgs(ss []STR) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = strconv.Quote(s.string())
	}

	return strings.Join(parts, ", ")
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
