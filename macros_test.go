package main

import (
	"strings"
	"testing"
)

// parseCondForTest is a tiny helper that wraps the cond expression
// from `IF (...)` parsing — the production parser builds Expr only as
// part of an IF Stmt, so the test path goes through Parse and pulls
// the cond out of the resulting *IfStmt.
func parseCondForTest(t *testing.T, condSrc string) Expr {
	t.Helper()

	src := []byte("IF (" + condSrc + ")\nENDIF()\n")
	mf, err := Parse("test.input", src)

	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", condSrc, err)
	}

	if len(mf.Stmts) != 1 {
		t.Fatalf("len(Stmts) = %d, want 1", len(mf.Stmts))
	}

	ifStmt, ok := mf.Stmts[0].(*IfStmt)

	if !ok {
		t.Fatalf("Stmts[0] = %T, want *IfStmt", mf.Stmts[0])
	}

	return ifStmt.Cond
}

// TestEvalCond_AndOrNot pins the boolean combinators against the
// canonical DefaultIfEnv values: CLANG=true, MSVC=false, GCC=false,
// WITH_VALGRIND=false. The three cases exercise AND, OR, and the
// NOT-AND combination.
func TestEvalCond_AndOrNot(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"clang_and_not_msvc", "CLANG AND NOT MSVC", true},
		{"msvc_or_gcc", "MSVC OR GCC", false},
		{"not_valgrind_and_clang", "NOT WITH_VALGRIND AND CLANG", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr := parseCondForTest(t, tc.expr)
			got := EvalCond(expr, DefaultIfEnv)

			if got != tc.want {
				t.Errorf("EvalCond(%q) = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEvalCond_UnknownVarThrows pins D27: an unknown identifier in an
// IF predicate throws rather than silently defaulting to false. The
// error message must mention "unknown IF identifier" so the operator
// has a fast-track to the fix (extend DefaultIfEnv).
func TestEvalCond_UnknownVarThrows(t *testing.T) {
	expr := parseCondForTest(t, "UNKNOWN_VAR")
	exc := Try(func() {
		EvalCond(expr, DefaultIfEnv)
	})

	if exc == nil {
		t.Fatal("EvalCond returned nil exception, want throw")
	}

	if !strings.Contains(exc.Error(), "unknown IF identifier") {
		t.Errorf("exception %q does not contain %q", exc.Error(), "unknown IF identifier")
	}
}

// TestEvalCond_DefaultEnvCoversArchiverClosureCanaries pins the four
// canary identifiers that PR-13's DefaultIfEnv MUST bind true for the
// reference graph (`default-linux-aarch64` + clang + musl). If any of
// these flips false, downstream gen.go (PR-20) will emit the wrong
// branch of an IF and the comparator will report missing nodes; if
// any becomes UNBOUND, EvalCond throws upstream of that.
func TestEvalCond_DefaultEnvCoversArchiverClosureCanaries(t *testing.T) {
	canaries := []string{"OS_LINUX", "ARCH_AARCH64", "CLANG", "MUSL"}

	for _, name := range canaries {
		expr := &ExprIdent{Name: name}
		got := EvalCond(expr, DefaultIfEnv)

		if !got {
			t.Errorf("EvalCond(%s) = false, want true (DefaultIfEnv canary)", name)
		}
	}
}
