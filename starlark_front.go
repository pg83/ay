package main

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

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
		"library":                        sink.moduleBuiltin("LIBRARY"),
		"program":                        sink.moduleBuiltin("PROGRAM"),
		"run_program":                    starlark.NewBuiltin("run_program", runProgramBuiltin),
		"enum_serialization":             starlark.NewBuiltin("enum_serialization", enumSerBuiltin("plain")),
		"enum_serialization_with_header": starlark.NewBuiltin("enum_serialization_with_header", enumSerBuiltin("with_header")),
		"enum_serialization_noutf":       starlark.NewBuiltin("enum_serialization_noutf", enumSerBuiltin("noutf")),
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

// moduleBuiltin builds the `library`/`program` rule: it emits ModuleStmt, one stmt per
// declared attribute (mirroring buildStmt), then EndStmt.
func (s *stmtSink) moduleBuiltin(typeName string) *starlark.Builtin {
	name := typeName

	return starlark.NewBuiltin(name, func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var modName string

		var srcs, peerdir, cflags, cxxflags, conlyflags, addincl, extraOutputs *starlark.List

		var setDict *starlark.Dict

		if err := starlark.UnpackArgs(b.Name(), args, kwargs,
			"name?", &modName,
			"srcs?", &srcs,
			"peerdir?", &peerdir,
			"cflags?", &cflags,
			"cxxflags?", &cxxflags,
			"conlyflags?", &conlyflags,
			"addincl?", &addincl,
			"set?", &setDict,
			"extra_outputs?", &extraOutputs,
		); err != nil {
			return nil, err
		}

		var modArgs []STR

		if modName != "" {
			modArgs = STRS(modName)
		}

		s.add(&ModuleStmt{Name: internTok(typeName), Args: modArgs})

		if err := s.emitSrcs(srcs); err != nil {
			return nil, err
		}

		if nonEmptyList(peerdir) {
			ps, err := unpackStrList(peerdir)
			if err != nil {
				return nil, err
			}

			s.add(&PeerdirStmt{Paths: ps})
		}

		if err := s.emitFlags(cflags, func(g, o []STR) Stmt { return &CFlagsStmt{GlobalFlags: g, OwnFlags: o} }); err != nil {
			return nil, err
		}

		if err := s.emitFlags(cxxflags, func(g, o []STR) Stmt { return &CXXFlagsStmt{GlobalFlags: g, OwnFlags: o} }); err != nil {
			return nil, err
		}

		if err := s.emitFlags(conlyflags, func(g, o []STR) Stmt { return &CONLYFlagsStmt{GlobalFlags: g, OwnFlags: o} }); err != nil {
			return nil, err
		}

		if nonEmptyList(addincl) {
			as, err := unpackStrList(addincl)
			if err != nil {
				return nil, err
			}

			gp, ol, op, cp, ap, pgp, ugp, all := splitAddInclPaths(as)
			s.add(&AddInclStmt{GlobalPaths: gp, OneLevelPaths: ol, OwnPaths: op, CythonPaths: cp, AsmPaths: ap, ProtoGlobalPaths: pgp, UserGlobalPaths: ugp, AllPaths: all})
		}

		if err := s.emitSet(setDict); err != nil {
			return nil, err
		}

		if err := s.emitFrags(extraOutputs); err != nil {
			return nil, err
		}

		s.add(&EndStmt{})

		return starlark.None, nil
	})
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

// emitFlags splits a flags list into GLOBAL/own (splitFlagsByGlobal) and emits the
// kind-specific statement built by mk.
func (s *stmtSink) emitFlags(l *starlark.List, mk func(global, own []STR) Stmt) error {
	if !nonEmptyList(l) {
		return nil
	}

	fl, err := unpackStrList(l)
	if err != nil {
		return err
	}

	g, o := splitFlagsByGlobal(fl)
	s.add(mk(g, o))

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

// nonEmptyList reports whether l is a non-nil list with at least one element. An
// empty list attribute (e.g. `peerdir = [] if … else …`) contributes no statement,
// matching a ya.make that simply omits the macro.
func nonEmptyList(l *starlark.List) bool {
	return l != nil && l.Len() > 0
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
