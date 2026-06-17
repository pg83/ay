package main

import (
	"os"
	"strings"
	"testing"
)

// assertTranspileRoundTrip parses a ya.make, transpiles it to ya.star, and asserts that
// under env both resolve to the same module statement stream (Line/empty-insensitive) —
// the property that makes a transpiled ya.star produce an identical build graph.
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

	want := moduleSpan(flattenIfStmts(mf.Stmts, env))

	got, err := evalStar(newMemFS(map[string]string{"m/ya.star": star}), "m/ya.star", env)
	if err != nil {
		t.Fatalf("evalStar:\n--- src ---\n%s\nerr: %v", star, err)
	}

	gs, ws := dumpStmts(got), dumpStmts(want)
	if gs != ws {
		t.Fatalf("round-trip mismatch:\n--- ya.star ---\n%s\n--- got ---\n%s--- want ---\n%s", star, gs, ws)
	}
}

// moduleSpan returns the statements a ya.star reproduces, in ya.star emission order:
// ModuleStmt first, then any pre-module statements (the transpiler relocates them to the
// head of `body`), then the module body, then EndStmt. Post-END statements (RECURSE) are
// dropped.
func moduleSpan(stmts []Stmt) []Stmt {
	start, end := -1, -1

	for i, s := range stmts {
		if _, ok := s.(*ModuleStmt); ok && start < 0 {
			start = i
		}

		if _, ok := s.(*EndStmt); ok && start >= 0 {
			end = i

			break
		}
	}

	if start < 0 {
		return nil
	}

	if end < 0 {
		end = len(stmts) - 1
	}

	out := []Stmt{stmts[start]}         // ModuleStmt
	out = append(out, stmts[:start]...) // pre-module → head of body
	out = append(out, stmts[start+1:end]...)
	out = append(out, stmts[end]) // EndStmt

	return out
}

// TestTranspile_Sg6Corpus round-trips every sg6 module ya.make under one representative
// linux-x86_64 env and reports the mismatches (transpiler defects), capped. Skipped when
// the corpus / dir list is absent. Diagnostic harness, not a parity gate.
func TestTranspile_Sg6Corpus(t *testing.T) {
	const root = "/home/pg/monorepo/3"

	list, err := os.ReadFile("debug/sg6_moduledirs.txt")
	if err != nil {
		t.Skipf("dir list absent: %v", err)
	}

	fs := newFS(root)
	dirs := strings.Fields(string(list))
	mismatch, evalErr, ok := 0, 0, 0
	shown := 0

	for _, dir := range dirs {
		rel := joinRel(dir, "ya.make")
		if !fs.isFile(srcRootVFS, rel) {
			continue
		}

		mf, raw, perr := parseFileWithRaw(fs, rel)
		if perr != nil {
			continue
		}

		star, hasModule, terr := transpileToStar(mf.Stmts, raw)
		if terr != nil || !hasModule {
			continue
		}

		env := buildIfEnv(ModuleInstance{Platform: newTestPlatform(OSLinux, ISAX8664, "no"), Kind: KindLib, Path: source(dir)})
		want := moduleSpan(flattenIfStmts(mf.Stmts, env))

		got, eerr := evalStar(newMemFS(map[string]string{"x/ya.star": star}), "x/ya.star", env)
		if eerr != nil {
			evalErr++

			if shown < 8 {
				t.Logf("EVALERR %s: %v", dir, eerr)
				shown++
			}

			continue
		}

		if dumpStmts(got) != dumpStmts(want) {
			mismatch++

			if shown < 8 {
				t.Logf("MISMATCH %s:\n%s", dir, firstDiffLine(dumpStmts(want), dumpStmts(got)))
				shown++
			}

			continue
		}

		ok++
	}

	t.Logf("sg6 corpus round-trip: ok=%d mismatch=%d evalErr=%d", ok, mismatch, evalErr)
}

// firstDiffLine returns the first differing line pair between two dumps.
func firstDiffLine(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")

	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}

		if i < len(gl) {
			g = gl[i]
		}

		if w != g {
			return "  want: " + w + "\n  got:  " + g
		}
	}

	return "(identical?)"
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

// TestTranspile_UtilModule round-trips the real util/ya.make across two platforms, when
// available.
func TestTranspile_UtilModule(t *testing.T) {
	makeSrc, err := os.ReadFile("/home/pg/monorepo/3/util/ya.make")
	if err != nil {
		t.Skipf("util/ya.make unavailable: %v", err)
	}

	fs := newMemFS(map[string]string{"util/ya.make": string(makeSrc)})

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

		want := moduleSpan(flattenIfStmts(mf.Stmts, env))

		got, err := evalStar(starFS, "util/ya.star", env)
		if err != nil {
			t.Fatalf("evalStar:\n%s\nerr: %v", star, err)
		}

		if gs, ws := dumpStmts(got), dumpStmts(want); gs != ws {
			t.Fatalf("util round-trip mismatch (isa=%v):\n--- got ---\n%s--- want ---\n%s", isa, gs, ws)
		}
	}
}
