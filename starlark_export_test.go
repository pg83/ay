package main

import "testing"

// assertTranspileRoundTrip parses a ya.make, transpiles it to ya.star, and asserts that
// under env both resolve to the same module (content-equivalent: summarizeModule is
// order-/grouping-insensitive, since the declarative form legitimately merges and reorders
// — exact byte parity of the graph is the sg6 gate's job). Fully hermetic (in-memory FS).
func assertTranspileRoundTrip(t *testing.T, yamake string, env Environment) {
	t.Helper()

	fs := newMemFS(map[string]string{"m/ya.make": yamake})

	mf, raw, err := parseFileWithRaw(fs, "m/ya.make")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	star, hasModule, err := transpileToStar(mf.Stmts, raw)
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}

	if !hasModule {
		t.Fatalf("no module found in:\n%s", yamake)
	}

	want := summarizeModule(flattenIfStmts(mf.Stmts, env))

	got, err := evalStar(newMemFS(map[string]string{"m/ya.star": star}), "m/ya.star", env)
	if err != nil {
		t.Fatalf("evalStar:\n--- src ---\n%s\nerr: %v", star, err)
	}

	if gs := summarizeModule(got); gs != want {
		t.Fatalf("round-trip mismatch:\n--- ya.star ---\n%s\n--- got ---\n%s--- want ---\n%s", star, gs, want)
	}
}

func TestTranspile_Basic(t *testing.T) {
	assertTranspileRoundTrip(t, `LIBRARY(foo)
SRCS(a.cpp b.cpp)
PEERDIR(contrib/libs/zstd)
CFLAGS(GLOBAL -DX -DY)
ADDINCL(GLOBAL contrib/libs/foo FOR cython gen/inc)
END()
`, DefaultIfEnv.clone())
}

func TestTranspile_Conditionals(t *testing.T) {
	src := `LIBRARY()
SRCS(a.cpp)
IF (OS_WINDOWS)
    SRCS(win.cpp)
    PEERDIR(contrib/libs/win)
ELSEIF (OS_LINUX)
    SRCS(lin.cpp)
ELSE()
    SRCS(other.cpp)
ENDIF()
EXTRALIBS(-lrt -ldl)
END()
`

	for _, set := range []map[string]string{
		{"OS_WINDOWS": "yes"},
		{"OS_LINUX": "yes"},
		{},
	} {
		env := DefaultIfEnv.clone()

		for k, v := range set {
			env.setString(internEnv(k), v)
		}

		assertTranspileRoundTrip(t, src, env)
	}
}

func TestTranspile_EqualityCondition(t *testing.T) {
	src := `PROGRAM(p)
SRCS(main.cpp)
IF (ARCH_TYPE == "x86_64")
    SRCS(x86.cpp)
ENDIF()
RUN_PROGRAM(//tool/gen IN in.txt OUT out.cpp CWD sub)
END()
`

	envA := DefaultIfEnv.clone()
	envA.setString(internEnv("ARCH_TYPE"), "x86_64")

	assertTranspileRoundTrip(t, src, envA)
	assertTranspileRoundTrip(t, src, DefaultIfEnv.clone())
}

// TestTranspile_StatefulFlag covers the eval-time overlay: ENABLE/DEFAULT must be visible
// to a later IF (the declarative form keeps them as body frags that update `flags`).
func TestTranspile_StatefulFlag(t *testing.T) {
	src := `LIBRARY()
SRCS(a.cpp)
DEFAULT(PROVIDE_X "no")
IF (OS_LINUX)
    ENABLE(PROVIDE_X)
ENDIF()
IF (PROVIDE_X)
    ADDINCL(GLOBAL self/inc)
ENDIF()
END()
`

	env := DefaultIfEnv.clone()
	env.setString(internEnv("OS_LINUX"), "yes")

	assertTranspileRoundTrip(t, src, env)
}

// TestTranspile_BoundarySensitive covers INDUCED_DEPS, which groups files under a leading
// extension key: two statements must stay separate (not merged into one), or the grouping
// breaks.
func TestTranspile_BoundarySensitive(t *testing.T) {
	assertTranspileRoundTrip(t, `LIBRARY()
SRCS(a.cpp)
INDUCED_DEPS(h foo.h bar.h)
INDUCED_DEPS(cpp baz.cpp)
RESOURCE_FILES(PREFIX p/ x.txt y.txt)
END()
`, DefaultIfEnv.clone())
}

// TestTranspile_UtilModule round-trips the embedded util ya.make (perf_starlark.go) across
// two platforms — hermetic, no local checkout.
func TestTranspile_UtilModule(t *testing.T) {
	fs := newMemFS(map[string]string{"util/ya.make": utilYaMake})

	mf, raw, err := parseFileWithRaw(fs, "util/ya.make")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	star, hasModule, err := transpileToStar(mf.Stmts, raw)
	if err != nil || !hasModule {
		t.Fatalf("transpile: hasModule=%v err=%v", hasModule, err)
	}

	starFS := newMemFS(map[string]string{"util/ya.star": star})

	for _, isa := range []ISA{ISAX8664, ISAAArch64} {
		env := buildIfEnv(ModuleInstance{Platform: newTestPlatform(OSLinux, isa, "no"), Kind: KindLib, Path: source("util")})

		want := summarizeModule(flattenIfStmts(mf.Stmts, env))

		got, err := evalStar(starFS, "util/ya.star", env)
		if err != nil {
			t.Fatalf("evalStar:\n%s\nerr: %v", star, err)
		}

		if gs := summarizeModule(got); gs != want {
			t.Fatalf("util round-trip mismatch (isa=%v):\n--- got ---\n%s--- want ---\n%s", isa, gs, want)
		}
	}
}

// TestTranspile_ForceStmtByteExact verifies the stmt()-body fallback form reproduces the
// exact ya.make statement stream (the form stmtFallbackDirs modules use).
func TestTranspile_ForceStmtByteExact(t *testing.T) {
	src := `LIBRARY(foo)
SRCS(a.cpp b.cpp)
PEERDIR(contrib/libs/zstd)
ADDINCL(GLOBAL inc)
END()
`

	fs := newMemFS(map[string]string{"m/ya.make": src})

	mf, raw, err := parseFileWithRaw(fs, "m/ya.make")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	star, _, err := transpileToStarMode(mf.Stmts, raw, true)
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}

	env := DefaultIfEnv.clone()

	got, err := evalStar(newMemFS(map[string]string{"m/ya.star": star}), "m/ya.star", env)
	if err != nil {
		t.Fatalf("evalStar:\n%s\nerr: %v", star, err)
	}

	// forceStmt preserves exact order/boundaries, so the streams match line-for-line.
	if gs, ws := dumpStmts(got), dumpStmts(flattenIfStmts(mf.Stmts, env)); gs != ws {
		t.Fatalf("forceStmt mismatch:\n--- ya.star ---\n%s\n--- got ---\n%s--- want ---\n%s", star, gs, ws)
	}
}
