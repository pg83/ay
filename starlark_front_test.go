package main

import (
	"fmt"
	"strings"
	"testing"
)

// The Starlark front-end must produce the same statement stream the ya.make parser
// produces for the equivalent module, so collectModule/genModule see identical input.
// We compare a Line/empty-insensitive dump of the two []Stmt.

func evalStarStr(t *testing.T, src string, env Environment) []Stmt {
	t.Helper()

	fs := newMemFS(map[string]string{"m/ya.star": src})

	stmts, err := evalStar(fs, "m/ya.star", env)
	if err != nil {
		t.Fatalf("evalStar: %v", err)
	}

	return stmts
}

func parseMakeStr(t *testing.T, src string) []Stmt {
	t.Helper()

	fs := newMemFS(map[string]string{"m/ya.make": src})

	mf, err := parseFile(fs, "m/ya.make")
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}

	return mf.Stmts
}

func assertSameStmts(t *testing.T, star, make []Stmt) {
	t.Helper()

	got, want := dumpStmts(star), dumpStmts(make)
	if got != want {
		t.Fatalf("ya.star stmts != ya.make stmts:\n--- ya.star ---\n%s--- ya.make ---\n%s", got, want)
	}
}

func TestStarlark_LibraryMatchesYaMake(t *testing.T) {
	env := DefaultIfEnv.clone()

	star := evalStarStr(t, `library(
    srcs = ["a.cpp", "b.cpp"],
    peerdir = ["contrib/libs/protobuf", "contrib/libs/zstd"],
    cflags = ["-DFOO"],
    addincl = ["contrib/libs/protobuf/src"],
)
`, env)

	make := parseMakeStr(t, `LIBRARY()
SRCS(a.cpp b.cpp)
PEERDIR(contrib/libs/protobuf contrib/libs/zstd)
CFLAGS(-DFOO)
ADDINCL(contrib/libs/protobuf/src)
END()
`)

	assertSameStmts(t, star, make)
}

func TestStarlark_ProgramName(t *testing.T) {
	env := DefaultIfEnv.clone()

	star := evalStarStr(t, `program(name = "mytool", srcs = ["main.cpp"])`, env)
	make := parseMakeStr(t, "PROGRAM(mytool)\nSRCS(main.cpp)\nEND()\n")

	assertSameStmts(t, star, make)
}

func TestStarlark_ConditionalFlags(t *testing.T) {
	src := `library(
    srcs = ["a.cpp"],
    peerdir = ["contrib/libs/musl"] if flags.MUSL == "yes" else [],
)
`

	// MUSL=yes -> the peerdir is present.
	muslEnv := DefaultIfEnv.clone()
	muslEnv.setString(internEnv("MUSL"), "yes")

	assertSameStmts(t, evalStarStr(t, src, muslEnv), parseMakeStr(t, `LIBRARY()
SRCS(a.cpp)
PEERDIR(contrib/libs/musl)
END()
`))

	// MUSL unset -> the empty peerdir contributes no statement.
	assertSameStmts(t, evalStarStr(t, src, DefaultIfEnv.clone()), parseMakeStr(t, "LIBRARY()\nSRCS(a.cpp)\nEND()\n"))
}

func TestStarlark_GeneratorsInSrcs(t *testing.T) {
	env := DefaultIfEnv.clone()

	// Model A: run_program / enum_serialization return lists, composed into srcs with
	// `+`; they emit GENERATE_ENUM_SERIALIZATION / RUN_PROGRAM in declaration order.
	star := evalStarStr(t, `library(
    srcs = ["a.cpp"]
         + enum_serialization("color.h")
         + run_program("//tools/foogen", outs = ["gen.cpp"]),
)
`, env)

	make := parseMakeStr(t, `LIBRARY()
SRCS(a.cpp)
GENERATE_ENUM_SERIALIZATION(color.h)
RUN_PROGRAM(//tools/foogen OUT gen.cpp)
END()
`)

	assertSameStmts(t, star, make)
}

func TestStarlark_Toggles(t *testing.T) {
	env := DefaultIfEnv.clone()

	// Boolean kwargs map to zero-argument macros; a False toggle emits nothing.
	star := evalStarStr(t, `library(
    srcs = ["a.cpp"],
    no_optimize = True,
    no_runtime = True,
    use_python3 = True,
    no_libc = False,
)
`, env)

	make := parseMakeStr(t, `LIBRARY()
SRCS(a.cpp)
NO_OPTIMIZE()
NO_RUNTIME()
USE_PYTHON3()
END()
`)

	assertSameStmts(t, star, make)
}

func TestStarlark_ValueMacros(t *testing.T) {
	env := DefaultIfEnv.clone()

	// Scalar/list value macros: a string is one argument, a list is many.
	star := evalStarStr(t, `library(
    srcs = ["a.cpp"],
    version = "1.0.0",
    license = ["MIT", "AND", "BSD-3-Clause"],
    py_namespace = "foo.bar",
    ldflags = ["-lm"],
    srcdir = ["contrib/libs/foo"],
)
`, env)

	make := parseMakeStr(t, `LIBRARY()
SRCS(a.cpp)
VERSION(1.0.0)
LICENSE(MIT AND BSD-3-Clause)
PY_NAMESPACE(foo.bar)
LDFLAGS(-lm)
SRCDIR(contrib/libs/foo)
END()
`)

	assertSameStmts(t, star, make)
}

func TestStarlark_EnableDisable(t *testing.T) {
	env := DefaultIfEnv.clone()

	// enable=/disable= emit one ENABLE/DISABLE per flag name, in order.
	star := evalStarStr(t, `library(
    srcs = ["a.cpp"],
    enable = ["FOO", "BAR"],
    disable = ["BAZ"],
)
`, env)

	make := parseMakeStr(t, `LIBRARY()
SRCS(a.cpp)
ENABLE(FOO)
ENABLE(BAR)
DISABLE(BAZ)
END()
`)

	assertSameStmts(t, star, make)
}

func TestStarlark_UpperCaseFlagFlip(t *testing.T) {
	env := DefaultIfEnv.clone()

	// An UPPER_CASE kwarg is a generic flag flip: True → ENABLE, False → DISABLE.
	star := evalStarStr(t, `library(
    srcs = ["a.cpp"],
    FOO = True,
    BAR_BAZ = False,
)
`, env)

	make := parseMakeStr(t, `LIBRARY()
SRCS(a.cpp)
ENABLE(FOO)
DISABLE(BAR_BAZ)
END()
`)

	assertSameStmts(t, star, make)
}

func TestStarlark_ModuleTypes(t *testing.T) {
	env := DefaultIfEnv.clone()

	// A non-trivial module type with GLOBAL cflags and a unittest_for target name.
	assertSameStmts(t,
		evalStarStr(t, `py3_library(srcs = ["m.py"], cxxflags = ["GLOBAL", "-DX"])`, env),
		parseMakeStr(t, "PY3_LIBRARY()\nSRCS(m.py)\nCXXFLAGS(GLOBAL -DX)\nEND()\n"))

	assertSameStmts(t,
		evalStarStr(t, `unittest_for("contrib/libs/foo", srcs = ["t.cpp"])`, env),
		parseMakeStr(t, "UNITTEST_FOR(contrib/libs/foo)\nSRCS(t.cpp)\nEND()\n"))
}

// dumpStmts renders a statement stream into a Line/empty-insensitive form (nil and
// empty slices both render as "[]"), so two streams that differ only in source
// positions or nil-vs-empty compare equal.
func dumpStmts(stmts []Stmt) string {
	var b strings.Builder

	for _, s := range stmts {
		switch x := s.(type) {
		case *ModuleStmt:
			fmt.Fprintf(&b, "MODULE %s %s\n", x.Name.string(), strDump(x.Args))
		case *SrcsStmt:
			fmt.Fprintf(&b, "SRCS %s\n", strDump(x.Sources))
		case *PeerdirStmt:
			fmt.Fprintf(&b, "PEERDIR %s\n", strDump(x.Paths))
		case *CFlagsStmt:
			fmt.Fprintf(&b, "CFLAGS g=%s o=%s\n", strDump(x.GlobalFlags), strDump(x.OwnFlags))
		case *CXXFlagsStmt:
			fmt.Fprintf(&b, "CXXFLAGS g=%s o=%s\n", strDump(x.GlobalFlags), strDump(x.OwnFlags))
		case *CONLYFlagsStmt:
			fmt.Fprintf(&b, "CONLYFLAGS g=%s o=%s\n", strDump(x.GlobalFlags), strDump(x.OwnFlags))
		case *AddInclStmt:
			fmt.Fprintf(&b, "ADDINCL global=%s onelevel=%s own=%s cython=%s asm=%s proto=%s user=%s all=%s\n",
				strDump(x.GlobalPaths), strDump(x.OneLevelPaths), strDump(x.OwnPaths), strDump(x.CythonPaths),
				strDump(x.AsmPaths), strDump(x.ProtoGlobalPaths), strDump(x.UserGlobalPaths), strDump(x.AllPaths))
		case *SetStmt:
			fmt.Fprintf(&b, "SET %s=%s\n", x.Name, x.Value)
		case *RunProgramStmt:
			fmt.Fprintf(&b, "RUN_PROGRAM tool=%s args=%s in=%s out=%s outnoauto=%s incl=%s\n",
				x.ToolPath.string(), strDump(x.Args), strDump(x.INFiles), strDump(x.OUTFiles),
				strDump(x.OUTNoAutoFiles), strDump(x.OutputIncludes))
		case *GenerateEnumSerializationStmt:
			fmt.Fprintf(&b, "ENUMSER %s %s\n", x.Header, x.Variant)
		case *LDFlagsStmt:
			fmt.Fprintf(&b, "LDFLAGS %s\n", strDump(x.Flags))
		case *SrcDirStmt:
			fmt.Fprintf(&b, "SRCDIR %s\n", strDump(x.Dirs))
		case *GlobalSrcsStmt:
			fmt.Fprintf(&b, "GLOBAL_SRCS %s\n", strDump(x.Sources))
		case *DefaultVarStmt:
			fmt.Fprintf(&b, "DEFAULT %s=%s\n", x.VarName, x.Value)
		case *UnknownStmt:
			fmt.Fprintf(&b, "MACRO %s %s\n", x.Name.string(), strDump(x.Args))
		case *EndStmt:
			b.WriteString("END\n")
		default:
			fmt.Fprintf(&b, "UNHANDLED %T %+v\n", s, s)
		}
	}

	return b.String()
}

func strDump(ss []STR) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = s.string()
	}

	return "[" + strings.Join(parts, " ") + "]"
}
