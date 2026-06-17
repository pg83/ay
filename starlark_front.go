package main

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// moduleTypeNames are the ya.make module declarations exposed as rule builtins. They
// must match the ModuleStmt cases in buildStmtFor.
var moduleTypeNames = []string{
	"LIBRARY", "PROGRAM",
	"PY23_NATIVE_LIBRARY", "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY",
	"PY3_PROGRAM_BIN", "PY2_PROGRAM", "PY3_PROGRAM",
	"YQL_UDF_YDB", "YQL_UDF_CONTRIB",
	"PROTO_LIBRARY",
	"DLL", "SO_PROGRAM", "DYNAMIC_LIBRARY",
	"PACKAGE", "UNION", "RESOURCES_LIBRARY",
	"PREBUILT_PROGRAM",
	"UNITTEST_FOR",
}

// attrKind classifies how a rule attribute's value becomes macro arguments.
type attrKind int

const (
	attrArgs   attrKind = iota // string | [string] -> MACRO(args…); empty list omits it
	attrToggle                 // bool -> MACRO() when True
)

// starAttrs maps a rule keyword argument to the ya.make macro it emits. Args-attrs pass
// their value through as the macro's arguments (buildStmtFor handles GLOBAL splitting,
// section keywords, etc.); toggle-attrs emit the zero-argument macro when True. The few
// structural attributes (srcs, set, enable, disable, extra_outputs) are handled directly
// in emitAttr. This table is the entire declarative vocabulary — one row per macro.
var starAttrs = map[string]struct {
	macro string
	kind  attrKind
}{
	// structured list attributes (typed Stmts built by buildStmtFor)
	"peerdir":        {"PEERDIR", attrArgs},
	"srcdir":         {"SRCDIR", attrArgs},
	"global_srcs":    {"GLOBAL_SRCS", attrArgs},
	"join_srcs":      {"JOIN_SRCS", attrArgs},
	"cflags":         {"CFLAGS", attrArgs},
	"cxxflags":       {"CXXFLAGS", attrArgs},
	"conlyflags":     {"CONLYFLAGS", attrArgs},
	"ldflags":        {"LDFLAGS", attrArgs},
	"addincl":        {"ADDINCL", attrArgs},
	"resource_files": {"RESOURCE_FILES", attrArgs},
	"resource":       {"RESOURCE", attrArgs},
	"default":        {"DEFAULT", attrArgs},

	// value macros (UnknownStmt; the collect handler reads Args and validates arity)
	"version":         {"VERSION", attrArgs},
	"license":         {"LICENSE", attrArgs},
	"allocator":       {"ALLOCATOR", attrArgs},
	"maven_group_id":  {"MAVEN_GROUP_ID", attrArgs},
	"py_namespace":    {"PY_NAMESPACE", attrArgs},
	"proto_namespace": {"PROTO_NAMESPACE", attrArgs},
	"subscriber":      {"SUBSCRIBER", attrArgs},
	"data":            {"DATA", attrArgs},
	"primary_output":  {"PRIMARY_OUTPUT", attrArgs},
	"exports_script":  {"EXPORTS_SCRIPT", attrArgs},
	"toolchain":       {"TOOLCHAIN", attrArgs},
	"py_srcs":         {"PY_SRCS", attrArgs},
	"py_main":         {"PY_MAIN", attrArgs},
	"py_register":     {"PY_REGISTER", attrArgs},
	"py_constructor":  {"PY_CONSTRUCTOR", attrArgs},
	"exclude_tags":    {"EXCLUDE_TAGS", attrArgs},
	"extralibs":       {"EXTRALIBS", attrArgs},
	"yql_abi_version": {"YQL_ABI_VERSION", attrArgs},
	"split_factor":    {"SPLIT_FACTOR", attrArgs},

	// zero-argument toggle macros (bool kwargs)
	"no_optimize":               {"NO_OPTIMIZE", attrToggle},
	"no_runtime":                {"NO_RUNTIME", attrToggle},
	"no_libc":                   {"NO_LIBC", attrToggle},
	"no_platform":               {"NO_PLATFORM", attrToggle},
	"no_util":                   {"NO_UTIL", attrToggle},
	"no_compiler_warnings":      {"NO_COMPILER_WARNINGS", attrToggle},
	"no_wshadow":                {"NO_WSHADOW", attrToggle},
	"no_lto":                    {"NO_LTO", attrToggle},
	"no_mypy":                   {"NO_MYPY", attrToggle},
	"no_lint":                   {"NO_LINT", attrToggle},
	"no_check_imports":          {"NO_CHECK_IMPORTS", attrToggle},
	"no_import_tracing":         {"NO_IMPORT_TRACING", attrToggle},
	"no_python_includes":        {"NO_PYTHON_INCLUDES", attrToggle},
	"no_extended_source_search": {"NO_EXTENDED_SOURCE_SEARCH", attrToggle},
	"no_split_dwarf":            {"NO_SPLIT_DWARF", attrToggle},
	"no_join_src":               {"NO_JOIN_SRC", attrToggle},
	"no_clang_tidy":             {"NO_CLANG_TIDY", attrToggle},
	"no_clang_coverage":         {"NO_CLANG_COVERAGE", attrToggle},
	"no_sanitize":               {"NO_SANITIZE", attrToggle},
	"no_sanitize_coverage":      {"NO_SANITIZE_COVERAGE", attrToggle},
	"no_profile_runtime":        {"NO_PROFILE_RUNTIME", attrToggle},
	"no_export_dynamic_symbols": {"NO_EXPORT_DYNAMIC_SYMBOLS", attrToggle},
	"no_optimize_py_protos":     {"NO_OPTIMIZE_PY_PROTOS", attrToggle},
	"optimize_py_protos":        {"OPTIMIZE_PY_PROTOS", attrToggle},
	"protoc_fatal_warnings":     {"PROTOC_FATAL_WARNINGS", attrToggle},
	"split_dwarf":               {"SPLIT_DWARF", attrToggle},
	"style_ruff":                {"STYLE_RUFF", attrToggle},
	"style_cpp":                 {"STYLE_CPP", attrToggle},
	"style_python":              {"STYLE_PYTHON", attrToggle},
	"use_python3":               {"USE_PYTHON3", attrToggle},
	"use_cxx":                   {"USE_CXX", attrToggle},
	"use_common_google_apis":    {"USE_COMMON_GOOGLE_APIS", attrToggle},
	"use_llvm_bc16":             {"USE_LLVM_BC16", attrToggle},
	"use_llvm_bc18":             {"USE_LLVM_BC18", attrToggle},
	"use_llvm_bc20":             {"USE_LLVM_BC20", attrToggle},
}

// Starlark front-end (Model A) for ya.make. A `ya.star` evaluates — per module
// build-variant, with that variant's flags — to the same []Stmt the ya.make parser
// produces, so collectModule/genModule and the whole graph are unchanged. Codegen is
// declarative: a generator (run_program, enum_serialization, …) is a value that lands
// in `srcs`, not an imperative step. See docs/plans for the design.

// moduleSource is a module's statement source: either a parsed ya.make (env-independent,
// computed once) or a ya.star re-executed per env. The auto-included linters.make.inc
// statements are appended after the module body in both cases.
type moduleSource struct {
	starFS  FS     // non-nil => ya.star path
	starRel string // ya.star path (when starFS != nil)
	autoinc []Stmt // linters.make.inc statements, appended after the body
	stmts   []Stmt // ya.make path: body+autoinc, precomputed (starFS == nil)
}

// stmtsFor yields the module's statements for one build-variant. ya.make returns the
// single parse; ya.star re-executes with env's flags (Model A resolves `if flags.X`
// eagerly, so the statement stream depends on the flags).
func (m *moduleSource) stmtsFor(env Environment) []Stmt {
	if m.starFS == nil {
		return m.stmts
	}

	body := throw2(evalStar(m.starFS, m.starRel, env))

	if len(m.autoinc) == 0 {
		return body
	}

	out := make([]Stmt, 0, len(body)+len(m.autoinc))
	out = append(out, body...)
	out = append(out, m.autoinc...)

	return out
}

// loadModuleSource picks ya.star over ya.make when present, and pre-parses the
// auto-included linters.make.inc once (it is env-independent).
func loadModuleSource(ctx *GenCtx, dir string) *moduleSource {
	var autoinc []Stmt

	if inc, ok := ctx.autoincludeIdx.lintersMakeIncFor(dir); ok && ctx.fs.isFile(srcRootVFS, inc.rel()) {
		autoinc = throw2(parseFile(ctx.fs, inc.rel())).Stmts
	}

	if ctx.fs.isFile(dirKey(dir), "ya.star") {
		return &moduleSource{starFS: ctx.fs, starRel: joinRel(dir, "ya.star"), autoinc: autoinc}
	}

	stmts := throw2(parseFile(ctx.fs, joinRel(dir, "ya.make"))).Stmts

	if len(autoinc) > 0 {
		stmts = append(stmts, autoinc...)
	}

	return &moduleSource{stmts: stmts}
}

// evalStar reads, parses and executes a ya.star with env's flags, returning the
// accumulated []Stmt. No caching — one parse+execute per call (a ya.star runs ~once
// per module; only host-tool re-instantiation repeats).
func evalStar(fs FS, rel string, env Environment) ([]Stmt, error) {
	src := readOwnedForParse(fs, rel)
	sink := &stmtSink{}

	predeclared := starlark.StringDict{
		"flags":                          &starFlags{env: env},
		"run_program":                    starlark.NewBuiltin("run_program", runProgramBuiltin),
		"enum_serialization":             starlark.NewBuiltin("enum_serialization", enumSerBuiltin("plain")),
		"enum_serialization_with_header": starlark.NewBuiltin("enum_serialization_with_header", enumSerBuiltin("with_header")),
		"enum_serialization_noutf":       starlark.NewBuiltin("enum_serialization_noutf", enumSerBuiltin("noutf")),
	}

	// Every module type ya.make recognizes is a rule builtin (lower-cased): library(),
	// program(), py3_library(), proto_library(), dll(), … The multimodule split for
	// py3_program etc. happens downstream in collectModule, exactly as for ya.make.
	for _, typeName := range moduleTypeNames {
		predeclared[strings.ToLower(typeName)] = sink.moduleBuiltin(typeName)
	}

	thread := &starlark.Thread{Name: rel}

	if _, err := starlark.ExecFileOptions(syntax.LegacyFileOptions(), thread, rel, src, predeclared); err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}

	return sink.stmts, nil
}

// stmtSink accumulates the statements a ya.star execution emits.
type stmtSink struct {
	stmts []Stmt
}

func (s *stmtSink) add(st Stmt) {
	s.stmts = append(s.stmts, st)
}

// starFlags exposes the build-variant's env to Starlark as `flags`: `flags.MUSL`
// returns the raw string value, faithful to ya.make's `when ($MUSL == "yes")`.
type starFlags struct {
	env Environment
}

func (f *starFlags) String() string        { return "<flags>" }
func (f *starFlags) Type() string          { return "flags" }
func (f *starFlags) Freeze()               {}
func (f *starFlags) Truth() starlark.Bool  { return starlark.True }
func (f *starFlags) Hash() (uint32, error) { return 0, fmt.Errorf("flags is unhashable") }

func (f *starFlags) Attr(name string) (starlark.Value, error) {
	return starlark.String(f.env.string(internEnv(name))), nil
}

func (f *starFlags) AttrNames() []string { return nil }

// genFrag is a Model A generator value: it wraps the Stmt(s) the generator emits.
// A genFrag placed in `srcs` contributes those statements to the module in order.
type genFrag struct {
	stmts []Stmt
}

func (g *genFrag) String() string        { return "<genfrag>" }
func (g *genFrag) Type() string          { return "genfrag" }
func (g *genFrag) Freeze()               {}
func (g *genFrag) Truth() starlark.Bool  { return starlark.True }
func (g *genFrag) Hash() (uint32, error) { return 0, fmt.Errorf("genfrag is unhashable") }

// moduleBuiltin builds a module rule (library, program, py3_library, …): it emits the
// ModuleStmt, one statement per declared attribute (in source order, via buildStmtFor so
// the result is byte-identical to the equivalent ya.make), then EndStmt. The module name
// is an optional first positional or a `name=` keyword.
func (s *stmtSink) moduleBuiltin(typeName string) *starlark.Builtin {
	return starlark.NewBuiltin(strings.ToLower(typeName), func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) > 1 {
			return nil, fmt.Errorf("%s: at most one positional argument (name), got %d", b.Name(), len(args))
		}

		modName := ""

		if len(args) == 1 {
			n, ok := starlark.AsString(args[0])
			if !ok {
				return nil, fmt.Errorf("%s: name must be a string, got %s", b.Name(), args[0].Type())
			}

			modName = n
		}

		// `name` may also arrive as a keyword; pull it out, keep the rest in source order.
		body := make([]starlark.Tuple, 0, len(kwargs))

		for _, kv := range kwargs {
			key, _ := starlark.AsString(kv[0])

			if key == "name" {
				n, ok := starlark.AsString(kv[1])
				if !ok {
					return nil, fmt.Errorf("%s: name must be a string, got %s", b.Name(), kv[1].Type())
				}

				modName = n

				continue
			}

			body = append(body, kv)
		}

		var modArgs []STR

		if modName != "" {
			modArgs = STRS(modName)
		}

		s.add(buildStmtFor(typeName, modArgs, 0, throwFmt))

		for _, kv := range body {
			key, _ := starlark.AsString(kv[0])

			if err := s.emitAttr(b.Name(), key, kv[1]); err != nil {
				return nil, err
			}
		}

		s.add(buildStmtFor("END", nil, 0, throwFmt))

		return starlark.None, nil
	})
}

// emitAttr emits the statement(s) for one rule attribute. Structural attributes (srcs,
// set, enable, disable, extra_outputs) have bespoke handling; everything else is a row
// in starAttrs (args-attr or toggle-attr).
func (s *stmtSink) emitAttr(rule, key string, v starlark.Value) error {
	switch key {
	case "srcs":
		return s.emitSrcs(asList(v))
	case "set":
		d, _ := v.(*starlark.Dict)
		return s.emitSet(d)
	case "enable":
		return s.emitFlagFlips("ENABLE", v)
	case "disable":
		return s.emitFlagFlips("DISABLE", v)
	case "extra_outputs":
		return s.emitFrags(asList(v))
	}

	// An UPPER_CASE keyword is a generic flag flip: FLAG=True → ENABLE(FLAG),
	// FLAG=False → DISABLE(FLAG). Casing disambiguates it from the lower-case typed
	// attributes, so an arbitrary flag needs no table entry.
	if isFlagName(key) {
		on, ok := v.(starlark.Bool)
		if !ok {
			return fmt.Errorf("%s: flag %s expects a bool, got %s", rule, key, v.Type())
		}

		macro := "DISABLE"
		if on {
			macro = "ENABLE"
		}

		s.add(buildStmtFor(macro, STRS(key), 0, throwFmt))

		return nil
	}

	spec, ok := starAttrs[key]
	if !ok {
		return fmt.Errorf("%s: unknown attribute %q", rule, key)
	}

	switch spec.kind {
	case attrToggle:
		on, ok := v.(starlark.Bool)
		if !ok {
			return fmt.Errorf("%s: %s expects a bool, got %s", rule, key, v.Type())
		}

		if on {
			s.add(buildStmtFor(spec.macro, nil, 0, throwFmt))
		}
	case attrArgs:
		args, err := toArgs(v)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", rule, key, err)
		}

		if len(args) > 0 {
			s.add(buildStmtFor(spec.macro, args, 0, throwFmt))
		}
	}

	return nil
}

// emitFlagFlips emits one ENABLE/DISABLE per flag name: enable=["FOO","BAR"] →
// ENABLE(FOO) ENABLE(BAR) (each sets the flag yes/no, like ya.make's ENABLE/DISABLE).
func (s *stmtSink) emitFlagFlips(macro string, v starlark.Value) error {
	args, err := toArgs(v)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.ToLower(macro), err)
	}

	for _, flag := range args {
		s.add(buildStmtFor(macro, STRS(flag.string()), 0, throwFmt))
	}

	return nil
}

// isFlagName reports whether key is an UPPER_CASE flag name ([A-Z_][A-Z0-9_]* with at
// least one letter) — the form used for generic ENABLE/DISABLE flips.
func isFlagName(key string) bool {
	hasLetter := false

	for _, r := range key {
		switch {
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case (r >= '0' && r <= '9') || r == '_':
		default:
			return false
		}
	}

	return hasLetter
}

// toArgs coerces an attribute value into macro arguments: a string becomes a single
// argument; a list becomes its (string) elements. Other types are rejected.
func toArgs(v starlark.Value) ([]STR, error) {
	switch x := v.(type) {
	case starlark.String:
		return STRS(string(x)), nil
	case *starlark.List:
		return unpackStrList(x)
	case starlark.NoneType:
		return nil, nil
	default:
		return nil, fmt.Errorf("expected string or list, got %s", v.Type())
	}
}

// asList returns v as a *starlark.List, or nil if it is not one (None / absent).
func asList(v starlark.Value) *starlark.List {
	l, _ := v.(*starlark.List)

	return l
}

// emitSrcs walks a srcs list (flattening nested lists, so `+`-composed generators
// work): contiguous strings become a SrcsStmt; a genFrag flushes the pending strings
// and emits its statements, preserving declaration order.
func (s *stmtSink) emitSrcs(l *starlark.List) error {
	if l == nil {
		return nil
	}

	var pending []STR

	flush := func() {
		if len(pending) > 0 {
			s.add(&SrcsStmt{Sources: pending})
			pending = nil
		}
	}

	var walk func(v starlark.Value) error

	walk = func(v starlark.Value) error {
		switch x := v.(type) {
		case starlark.String:
			pending = append(pending, internStr(string(x)))
		case *genFrag:
			flush()

			for _, st := range x.stmts {
				s.add(st)
			}
		case *starlark.List:
			return iterValues(x, walk)
		default:
			return fmt.Errorf("srcs: expected string or generator, got %s", v.Type())
		}

		return nil
	}

	if err := iterValues(l, walk); err != nil {
		return err
	}

	flush()

	return nil
}

// emitSet emits a SetStmt per key of a `set = {name: value}` dict.
func (s *stmtSink) emitSet(d *starlark.Dict) error {
	if d == nil {
		return nil
	}

	for _, item := range d.Items() {
		name, ok := starlark.AsString(item[0])
		if !ok {
			return fmt.Errorf("set: key must be a string, got %s", item[0].Type())
		}

		value, ok := starlark.AsString(item[1])
		if !ok {
			return fmt.Errorf("set[%q]: value must be a string, got %s", name, item[1].Type())
		}

		s.add(&SetStmt{Name: name, NameEnv: internEnv(name), Value: value})
	}

	return nil
}

// emitFrags emits the statements of a (possibly nested) list of genFrags — e.g.
// extra_outputs = run_program(…) + run_program(…), runs whose outputs are not compiled.
func (s *stmtSink) emitFrags(l *starlark.List) error {
	if l == nil {
		return nil
	}

	var walk func(v starlark.Value) error

	walk = func(v starlark.Value) error {
		switch x := v.(type) {
		case *genFrag:
			for _, st := range x.stmts {
				s.add(st)
			}
		case *starlark.List:
			return iterValues(x, walk)
		default:
			return fmt.Errorf("extra_outputs: expected generator, got %s", v.Type())
		}

		return nil
	}

	return iterValues(l, walk)
}

// iterValues calls fn on each element of l (stopping on the first error).
func iterValues(l *starlark.List, fn func(starlark.Value) error) error {
	iter := l.Iterate()
	defer iter.Done()

	var v starlark.Value

	for iter.Next(&v) {
		if err := fn(v); err != nil {
			return err
		}
	}

	return nil
}

// runProgramBuiltin implements `run_program(tool, args=, ins=, outs=, out_noauto=,
// output_includes=, cwd=)` → a RunProgramStmt genFrag (mirrors parseRunProgram).
func runProgramBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var tool, cwd string

	var progArgs, ins, outs, outNoauto, outputIncludes *starlark.List

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"tool", &tool,
		"args?", &progArgs,
		"ins?", &ins,
		"outs?", &outs,
		"out_noauto?", &outNoauto,
		"output_includes?", &outputIncludes,
		"cwd?", &cwd,
	); err != nil {
		return nil, err
	}

	st := &RunProgramStmt{ToolPath: internStr(tool)}

	for _, field := range []struct {
		l   *starlark.List
		dst *[]STR
	}{
		{progArgs, &st.Args},
		{ins, &st.INFiles},
		{outs, &st.OUTFiles},
		{outNoauto, &st.OUTNoAutoFiles},
		{outputIncludes, &st.OutputIncludes},
	} {
		v, err := unpackStrList(field.l)
		if err != nil {
			return nil, err
		}

		*field.dst = v
	}

	if cwd != "" {
		c := internStr(cwd)
		st.CWD = &c
	}

	return fragList(st), nil
}

// enumSerBuiltin implements `enum_serialization[_with_header|_noutf](header)` → a
// GenerateEnumSerializationStmt genFrag.
func enumSerBuiltin(variant string) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var header string

		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "header", &header); err != nil {
			return nil, err
		}

		return fragList(&GenerateEnumSerializationStmt{Header: header, Variant: variant}), nil
	}
}

// fragList wraps a generator's statements in a single-element Starlark list, so
// generators compose into `srcs` with `+`: srcs = ["a.cpp"] + enum_serialization(…)
// + run_program(…). The srcs walker flattens the concatenated list.
func fragList(stmts ...Stmt) *starlark.List {
	return starlark.NewList([]starlark.Value{&genFrag{stmts: stmts}})
}

// unpackStrList interns a Starlark list of strings into []STR.
func unpackStrList(l *starlark.List) ([]STR, error) {
	if l == nil {
		return nil, nil
	}

	out := make([]STR, 0, l.Len())

	iter := l.Iterate()
	defer iter.Done()

	var v starlark.Value

	for iter.Next(&v) {
		str, ok := starlark.AsString(v)
		if !ok {
			return nil, fmt.Errorf("expected string, got %s", v.Type())
		}

		out = append(out, internStr(str))
	}

	return out, nil
}
