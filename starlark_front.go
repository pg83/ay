package main

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// starFileOptions enables the imperative constructs a ya.star uses to build attribute
// lists: top-level if/for (TopLevelControl) and accumulation into globals like
// `srcs += …` (GlobalReassign).
var starFileOptions = &syntax.FileOptions{
	TopLevelControl: true,
	GlobalReassign:  true,
	Set:             true,
}

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
	"headers":        {"HEADERS", attrArgs},

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
	"need_check":                {"NEED_CHECK", attrToggle},
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
	fl := &starFlags{env: env, local: map[string]string{}}

	predeclared := starlark.StringDict{
		"flags":                                  fl,
		"atom":                                   atomBuiltin(fl),
		"enable":                                 flagSetBuiltin("ENABLE", fl),
		"disable":                                flagSetBuiltin("DISABLE", fl),
		"set_var":                                setVarBuiltin(fl),
		"default_var":                            defaultVarBuiltin(fl),
		"run_program":                            runCmdBuiltin("RUN_PROGRAM", "tool"),
		"run_py3_program":                        runCmdBuiltin("RUN_PY3_PROGRAM", "tool"),
		"run_python3":                            runCmdBuiltin("RUN_PYTHON3", "script"),
		"run_antlr":                              runAntlrBuiltin("RUN_ANTLR"),
		"run_antlr4":                             runAntlrBuiltin("RUN_ANTLR4"),
		"run_antlr4_cpp":                         starlark.NewBuiltin("run_antlr4_cpp", runAntlr4CppBuiltin),
		"run_antlr4_cpp_split":                   starlark.NewBuiltin("run_antlr4_cpp_split", runAntlr4CppSplitBuiltin),
		"configure_file":                         starlark.NewBuiltin("configure_file", configureFileBuiltin),
		"create_buildinfo_for":                   starlark.NewBuiltin("create_buildinfo_for", createBuildInfoBuiltin),
		"declare_external_resource":              declareResourceBuiltin("DECLARE_EXTERNAL_RESOURCE"),
		"declare_external_host_resources_bundle": declareResourceBuiltin("DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE"),
		"declare_external_host_resources_bundle_by_json": declareResourceBuiltin("DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON"),
		"stmt":                           starlark.NewBuiltin("stmt", stmtBuiltin),
		"enum_serialization":             starlark.NewBuiltin("enum_serialization", enumSerBuiltin("plain")),
		"enum_serialization_with_header": starlark.NewBuiltin("enum_serialization_with_header", enumSerBuiltin("with_header")),
		"enum_serialization_noutf":       starlark.NewBuiltin("enum_serialization_noutf", enumSerBuiltin("noutf")),
		"join_srcs":                      starlark.NewBuiltin("join_srcs", joinSrcsBuiltin),
		"src_c_no_lto":                   starlark.NewBuiltin("src_c_no_lto", macroFragBuiltin("SRC_C_NO_LTO")),
	}

	// Every module type ya.make recognizes is a rule builtin (lower-cased): library(),
	// program(), py3_library(), proto_library(), dll(), … The multimodule split for
	// py3_program etc. happens downstream in collectModule, exactly as for ya.make.
	for _, typeName := range moduleTypeNames {
		predeclared[strings.ToLower(typeName)] = sink.moduleBuiltin(typeName)
	}

	thread := &starlark.Thread{Name: rel}

	if _, err := starlark.ExecFileOptions(starFileOptions, thread, rel, src, predeclared); err != nil {
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
// returns the raw string value, faithful to ya.make's `when ($MUSL == "yes")`. The
// `local` overlay records SET/ENABLE/DISABLE/DEFAULT performed during evaluation so a
// later condition sees them — collectStmts mutates the env in statement order, and the
// eager ya.star must reproduce that (e.g. ENABLE(PROVIDE_REALLOCARRAY) gating a later
// IF (PROVIDE_REALLOCARRAY)).
type starFlags struct {
	env   Environment
	local map[string]string
}

func (f *starFlags) String() string        { return "<flags>" }
func (f *starFlags) Type() string          { return "flags" }
func (f *starFlags) Freeze()               {}
func (f *starFlags) Truth() starlark.Bool  { return starlark.True }
func (f *starFlags) Hash() (uint32, error) { return 0, fmt.Errorf("flags is unhashable") }

func (f *starFlags) Attr(name string) (starlark.Value, error) {
	if v, ok := f.local[name]; ok {
		return starlark.String(v), nil
	}

	return starlark.String(f.env.string(internEnv(name))), nil
}

func (f *starFlags) AttrNames() []string { return nil }

// atom resolves an identifier in comparison position (evalAtom semantics) honouring the
// overlay: an overlaid or env-bound value, else the literal name.
func (f *starFlags) atom(name string) string {
	if v, ok := f.local[name]; ok {
		return v
	}

	return atomValue(f.env, name)
}

// bound reports whether name has a value (overlay or env binding) — DEFAULT only sets an
// unbound name.
func (f *starFlags) bound(name string) bool {
	if _, ok := f.local[name]; ok {
		return true
	}

	k, _ := f.env.s.lookup(internEnv(name))

	return k != envAbsent
}

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
	case "body":
		// body is the machine-generated form (`ay dev make starlark`): an ordered list
		// of stmt()/generator frags emitted verbatim, preserving exact ya.make
		// statement order.
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

// appendSection appends a RUN_* macro section — its keyword followed by its values —
// when the value list is non-empty, reconstructing the flat ya.make argument vector the
// parse* functions (via buildStmtFor) consume.
func appendSection(args []STR, kw STR, vals []STR) []STR {
	if len(vals) == 0 {
		return args
	}

	args = append(args, kw)

	return append(args, vals...)
}

// runCmdBuiltin builds a generator for a RUN_PROGRAM-shaped macro (RUN_PROGRAM,
// RUN_PY3_PROGRAM, RUN_PYTHON3): a head positional (tool path / script path) plus the
// keyword sections ya.make accepts. It reconstructs the flat argument vector and
// delegates to buildStmtFor, so the emitted Stmt is identical to the parsed ya.make.
// headName labels the head argument in error messages and the kwarg name.
func runCmdBuiltin(macro, headName string) *starlark.Builtin {
	return starlark.NewBuiltin(strings.ToLower(macro), func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var head, cwd string

		var progArgs, ins, inNoparse, inDeps, outs, outNoauto, stdout, env, outputIncludes, inducedDeps, tools *starlark.List

		if err := starlark.UnpackArgs(macro, args, kwargs,
			headName, &head,
			"args?", &progArgs,
			"ins?", &ins,
			"in_noparse?", &inNoparse,
			"in_deps?", &inDeps,
			"outs?", &outs,
			"out_noauto?", &outNoauto,
			"stdout?", &stdout,
			"env?", &env,
			"output_includes?", &outputIncludes,
			"induced_deps?", &inducedDeps,
			"tools?", &tools,
			"cwd?", &cwd,
		); err != nil {
			return nil, err
		}

		// ARGS is the default (keyword-less) section, so its values come first; every
		// other section is keyword-prefixed.
		flat := STRS(head)

		progVals, err := unpackStrList(progArgs)
		if err != nil {
			return nil, err
		}

		flat = append(flat, progVals...)

		for _, sec := range []struct {
			kw STR
			l  *starlark.List
		}{
			{kwIN, ins},
			{kwIN_NOPARSE, inNoparse},
			{kwIN_DEPS, inDeps},
			{kwOUT, outs},
			{kwOUT_NOAUTO, outNoauto},
			{kwSTDOUT, stdout},
			{kwENV, env},
			{kwOUTPUT_INCLUDES, outputIncludes},
			{kwINDUCED_DEPS, inducedDeps},
			{kwTOOL, tools},
		} {
			vals, err := unpackStrList(sec.l)
			if err != nil {
				return nil, err
			}

			flat = appendSection(flat, sec.kw, vals)
		}

		if cwd != "" {
			flat = appendSection(flat, kwCWD, STRS(cwd))
		}

		return fragList(buildStmtFor(macro, flat, 0, throwFmt)), nil
	})
}

// runAntlrBuiltin builds a generator for RUN_ANTLR / RUN_ANTLR4 (RunAntlrStmt): the same
// section grammar as runCmdBuiltin minus STDOUT, delegating to buildStmtFor.
func runAntlrBuiltin(macro string) *starlark.Builtin {
	return starlark.NewBuiltin(strings.ToLower(macro), func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var cwd string

		var progArgs, ins, inNoparse, inDeps, outs, outNoauto, outputIncludes, inducedDeps, tools, env *starlark.List

		if err := starlark.UnpackArgs(macro, args, kwargs,
			"args?", &progArgs,
			"ins?", &ins,
			"in_noparse?", &inNoparse,
			"in_deps?", &inDeps,
			"outs?", &outs,
			"out_noauto?", &outNoauto,
			"output_includes?", &outputIncludes,
			"induced_deps?", &inducedDeps,
			"tools?", &tools,
			"env?", &env,
			"cwd?", &cwd,
		); err != nil {
			return nil, err
		}

		progVals, err := unpackStrList(progArgs)
		if err != nil {
			return nil, err
		}

		flat := append([]STR(nil), progVals...)

		for _, sec := range []struct {
			kw STR
			l  *starlark.List
		}{
			{kwIN, ins},
			{kwIN_NOPARSE, inNoparse},
			{kwIN_DEPS, inDeps},
			{kwOUT, outs},
			{kwOUT_NOAUTO, outNoauto},
			{kwOUTPUT_INCLUDES, outputIncludes},
			{kwINDUCED_DEPS, inducedDeps},
			{kwTOOL, tools},
			{kwENV, env},
		} {
			vals, err := unpackStrList(sec.l)
			if err != nil {
				return nil, err
			}

			flat = appendSection(flat, sec.kw, vals)
		}

		if cwd != "" {
			flat = appendSection(flat, kwCWD, STRS(cwd))
		}

		return fragList(buildStmtFor(macro, flat, 0, throwFmt)), nil
	})
}

// antlrFlags appends the VISITOR / LISTENER toggle tokens that RUN_ANTLR4_CPP[_SPLIT]
// recognize. Listener is emitted as LISTENER/NO_LISTENER so the round-trip is explicit
// (the Stmt stores only the resolved bool).
func antlrFlags(flat []STR, visitor, listener bool) []STR {
	if visitor {
		flat = append(flat, kwVISITOR)
	}

	if listener {
		flat = append(flat, kwLISTENER)
	} else {
		flat = append(flat, kwNO_LISTENER)
	}

	return flat
}

// runAntlr4CppBuiltin implements `run_antlr4_cpp(grammar, options=, visitor=, listener=,
// output_includes=)` → a RunAntlr4CppStmt genFrag (mirrors parseRunAntlr4Cpp).
func runAntlr4CppBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var grammar string

	var visitor, listener bool

	var options, outputIncludes *starlark.List

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"grammar", &grammar,
		"options?", &options,
		"visitor?", &visitor,
		"listener?", &listener,
		"output_includes?", &outputIncludes,
	); err != nil {
		return nil, err
	}

	opts, err := unpackStrList(options)
	if err != nil {
		return nil, err
	}

	flat := append(STRS(grammar), opts...)
	flat = antlrFlags(flat, visitor, listener)

	incl, err := unpackStrList(outputIncludes)
	if err != nil {
		return nil, err
	}

	flat = appendSection(flat, kwOUTPUT_INCLUDES, incl)

	return fragList(buildStmtFor("RUN_ANTLR4_CPP", flat, 0, throwFmt)), nil
}

// runAntlr4CppSplitBuiltin implements `run_antlr4_cpp_split(lexer, parser, visitor=,
// listener=, output_includes=)` → a RunAntlr4CppSplitStmt genFrag.
func runAntlr4CppSplitBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var lexer, parser string

	var visitor, listener bool

	var outputIncludes *starlark.List

	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"lexer", &lexer,
		"parser", &parser,
		"visitor?", &visitor,
		"listener?", &listener,
		"output_includes?", &outputIncludes,
	); err != nil {
		return nil, err
	}

	flat := antlrFlags(STRS(lexer, parser), visitor, listener)

	incl, err := unpackStrList(outputIncludes)
	if err != nil {
		return nil, err
	}

	flat = appendSection(flat, kwOUTPUT_INCLUDES, incl)

	return fragList(buildStmtFor("RUN_ANTLR4_CPP_SPLIT", flat, 0, throwFmt)), nil
}

// configureFileBuiltin implements `configure_file(src, dst)` → a ConfigureFileStmt
// genFrag (mirrors the CONFIGURE_FILE handler).
func configureFileBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var src, dst string

	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "src", &src, "dst", &dst); err != nil {
		return nil, err
	}

	return fragList(buildStmtFor("CONFIGURE_FILE", STRS(src, dst), 0, throwFmt)), nil
}

// createBuildInfoBuiltin implements `create_buildinfo_for(header)` → a CreateBuildInfoStmt
// genFrag.
func createBuildInfoBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var header string

	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "header", &header); err != nil {
		return nil, err
	}

	return fragList(buildStmtFor("CREATE_BUILDINFO_FOR", STRS(header), 0, throwFmt)), nil
}

// declareResourceBuiltin builds a generator for a DECLARE_EXTERNAL_RESOURCE-family macro:
// it passes its positional string arguments through to buildStmtFor (→ DeclareResourceStmt).
func declareResourceBuiltin(macro string) *starlark.Builtin {
	return starlark.NewBuiltin(strings.ToLower(macro), func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(kwargs) > 0 {
			return nil, fmt.Errorf("%s: unexpected keyword argument", b.Name())
		}

		out := make([]STR, 0, len(args))

		for _, a := range args {
			str, ok := starlark.AsString(a)
			if !ok {
				return nil, fmt.Errorf("%s: arguments must be strings, got %s", b.Name(), a.Type())
			}

			out = append(out, internStr(str))
		}

		return fragList(buildStmtFor(macro, out, 0, throwFmt)), nil
	})
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

// joinSrcsBuiltin implements `join_srcs(output, sources)` → a JoinSrcsStmt genFrag.
// JOIN_SRCS(out a b c) concatenates the sources into a single generated out, which is
// then compiled; composed into `srcs`, it preserves declaration order among the others.
func joinSrcsBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var output string

	var sources *starlark.List

	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "output", &output, "sources", &sources); err != nil {
		return nil, err
	}

	srcs, err := unpackStrList(sources)
	if err != nil {
		return nil, err
	}

	all := append([]STR{internStr(output)}, srcs...)

	return fragList(buildStmtFor("JOIN_SRCS", all, 0, throwFmt)), nil
}

// macroFragBuiltin builds a generator for a per-source macro taking positional string
// arguments — e.g. src_c_no_lto("system/compiler.cpp") → SRC_C_NO_LTO(system/compiler.cpp),
// composed into `srcs`.
func macroFragBuiltin(macro string) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(kwargs) > 0 {
			return nil, fmt.Errorf("%s: unexpected keyword argument", b.Name())
		}

		out := make([]STR, 0, len(args))

		for _, a := range args {
			str, ok := starlark.AsString(a)
			if !ok {
				return nil, fmt.Errorf("%s: arguments must be strings, got %s", b.Name(), a.Type())
			}

			out = append(out, internStr(str))
		}

		return fragList(buildStmtFor(macro, out, 0, throwFmt)), nil
	}
}

// atomBuiltin implements `atom(name)` — the value of an IF-condition identifier in
// comparison position (evalAtom semantics): a bound variable's value, else the literal
// name. Distinct from `flags.X` (truthiness position), where an unbound name reads as
// false, not as its own name.
func atomBuiltin(fl *starFlags) *starlark.Builtin {
	return starlark.NewBuiltin("atom", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name string

		if err := starlark.UnpackArgs("atom", args, kwargs, "name", &name); err != nil {
			return nil, err
		}

		return starlark.String(fl.atom(name)), nil
	})
}

// flagSetBuiltin implements `enable([names])` / `disable([names])` — it records each name
// as yes/no in the eval-time overlay (so a later IF sees it) and emits the ENABLE/DISABLE
// stmt so collectStmts still applies its module-data side-effects (MUSL_LITE, …).
func flagSetBuiltin(macro string, fl *starFlags) *starlark.Builtin {
	on := macro == "ENABLE"
	val := strNo.string()

	if on {
		val = strYes.string()
	}

	return starlark.NewBuiltin(strings.ToLower(macro), func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var names *starlark.List

		if err := starlark.UnpackArgs(strings.ToLower(macro), args, kwargs, "names", &names); err != nil {
			return nil, err
		}

		out, err := unpackStrList(names)
		if err != nil {
			return nil, err
		}

		for _, n := range out {
			fl.local[n.string()] = val
		}

		return fragList(buildStmtFor(macro, out, 0, throwFmt)), nil
	})
}

// setVarBuiltin implements `set_var(name, [vals])` — SET in the overlay (space-joined
// value) plus the SetStmt for collectStmts.
func setVarBuiltin(fl *starFlags) *starlark.Builtin {
	return starlark.NewBuiltin("set_var", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name string

		var vals *starlark.List

		if err := starlark.UnpackArgs("set_var", args, kwargs, "name", &name, "vals?", &vals); err != nil {
			return nil, err
		}

		out, err := unpackStrList(vals)
		if err != nil {
			return nil, err
		}

		// The overlay value drives later eager conditions, so it must be expanded like
		// collectStmts does (e.g. DEFAULT(LLD_VERSION ${COMPILER_VERSION}) gating
		// IF (LLD_VERSION == 18)); the emitted stmt keeps the raw value (collectStmts
		// re-expands it).
		fl.local[name] = expandScalarVarRef(strings.Join(strStrings(out), " "), fl.env)

		return fragList(buildStmtFor("SET", append(STRS(name), out...), 0, throwFmt)), nil
	})
}

// defaultVarBuiltin implements `default_var(name, val)` — DEFAULT sets the overlay only
// when name is unbound (overlay or env), matching env.setDefaultString.
func defaultVarBuiltin(fl *starFlags) *starlark.Builtin {
	return starlark.NewBuiltin("default_var", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name, val string

		if err := starlark.UnpackArgs("default_var", args, kwargs, "name", &name, "val?", &val); err != nil {
			return nil, err
		}

		if !fl.bound(name) {
			fl.local[name] = expandScalarVarRef(val, fl.env)
		}

		return fragList(buildStmtFor("DEFAULT", STRS(name, val), 0, throwFmt)), nil
	})
}

// stmtBuiltin implements `stmt(name, *args)` — a generic generator emitting the macro
// `name(args…)` exactly as buildStmtFor produces it. It is the building block of the
// machine-generated ya.star (`ay dev make starlark`): one stmt() per ya.make statement,
// composed into `body` in order, so any ya.make statement round-trips through the same
// parse path with identical args.
func stmtBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string

	var argList *starlark.List

	// args arrives as a single list, not varargs: a SRCS with hundreds of files would
	// otherwise blow Starlark's 255-positional-argument call limit.
	if err := starlark.UnpackArgs("stmt", args, kwargs, "name", &name, "args?", &argList); err != nil {
		return nil, err
	}

	out, err := unpackStrList(argList)
	if err != nil {
		return nil, fmt.Errorf("stmt %s: %w", name, err)
	}

	return fragList(buildStmtFor(name, out, 0, throwFmt)), nil
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
