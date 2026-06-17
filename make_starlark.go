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

// transpileToStar renders the typed statement tree (carrying IF structure) and the flat
// RawCall stream (raw args, aligned 1:1 with the tree's non-IF leaves in pre-order) into
// idiomatic Model A ya.star: each statement becomes an attribute-list append (srcs +=,
// peerdir +=, …), a generator call in srcs (run_program/join_srcs/…), a toggle flag, or a
// flag mutator (enable/set_var/…); IF/ELSEIF/ELSE become Starlark if/elif/else. No stmt().
func transpileToStar(stmts []Stmt, raw []RawCall) (string, bool, error) {
	return transpileToStarMode(stmts, raw, false)
}

// transpileToStarMode renders the ya.star. forceStmt selects the exact-order stmt()-body
// form (byte-exact, every statement a body frag) instead of the idiomatic declarative
// form — used for modules whose declarative kwarg form is not yet byte-exact (the
// declarative form merges repeated statements and groups them by kind, which a few
// boundary/order-sensitive modules cannot tolerate).
func transpileToStarMode(stmts []Stmt, raw []RawCall, forceStmt bool) (string, bool, error) {
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
		t.emit(indent, "srcs += "+renderStrList(a))

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
			t.emit(indent, spec.kw+" += "+renderStrList(a))
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

// render assembles the ya.star: the on() helper, attribute-variable declarations, the
// imperative body, and the module rule call passing every touched attribute.
func (t *transBuilder) render(moduleType string, moduleArgs []STR) string {
	var b strings.Builder

	b.WriteString("# Generated by `ay dev make starlark` from ya.make — do not edit.\n\n")
	b.WriteString("def on(v):\n")
	b.WriteString("    return v != \"\" and v.lower() not in [\"false\", \"f\", \"no\", \"n\", \"off\", \"0\", \"net\"]\n\n")

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
