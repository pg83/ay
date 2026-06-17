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
		case *EndStmt:
			b.WriteString("END\n")
		default:
			fmt.Fprintf(&b, "UNHANDLED %T\n", s)
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
