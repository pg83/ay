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
	mf, err := Parse(testParserFS, "test.input", src)

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

// TestEvalCond_UnknownVarDefaultsFalse pins the ymake-compatible fallback
// for unset ALL_CAPS build variables: they coerce to false/empty rather
// than throwing.
func TestEvalCond_UnknownVarDefaultsFalse(t *testing.T) {
	expr := parseCondForTest(t, "UNKNOWN_VAR")
	if EvalCond(expr, DefaultIfEnv) {
		t.Fatal("EvalCond(UNKNOWN_VAR) = true, want false")
	}
}

// TestEvalCond_DefaultEnvCoversArchiverClosureCanaries pins the
// build-wide canary identifiers that DefaultIfEnv MUST bind true.
// These are independent of the instance ISA (OS_LINUX + clang);
// ARCH_* booleans are deliberately not canaries since `buildIfEnv`
// flips them per instance.Platform.ISA, and MUSL is per-platform
// rather than a build-wide default — see TestEvalCond_ARCH_ARM64_Aliased
// for the ISA-dispatch coverage.
func TestEvalCond_DefaultEnvCoversArchiverClosureCanaries(t *testing.T) {
	canaries := []string{"OS_LINUX", "CLANG"}

	for _, name := range canaries {
		expr := &ExprIdent{Name: name}
		got := EvalCond(expr, DefaultIfEnv)

		if !got {
			t.Errorf("EvalCond(%s) = false, want true (DefaultIfEnv canary)", name)
		}
	}
}

// TestEvalCond_NonMuslYdbIfBindings pins ticket-5 IF-flag bindings for the
// ydb (non-musl, OS_SDK=local) walk: OS_FREERTOS/STATIC_STL bind false in
// bool position, OS_SDK is a string (compared to SDK literals, not a bool),
// and MUSL is no longer a build-wide always-true default.
func TestEvalCond_NonMuslYdbIfBindings(t *testing.T) {
	for _, name := range []string{"OS_FREERTOS", "STATIC_STL"} {
		if EvalCond(&ExprIdent{Name: name}, DefaultIfEnv) {
			t.Errorf("EvalCond(%s) = true, want false", name)
		}
	}

	if EvalCond(parseCondForTest(t, `OS_SDK == "ubuntu-12"`), DefaultIfEnv) {
		t.Error(`EvalCond(OS_SDK == "ubuntu-12") = true, want false`)
	}

	localEnv := DefaultIfEnv.Clone()
	localEnv.SetFromString("OS_SDK", "local")
	if !EvalCond(parseCondForTest(t, `OS_SDK == "local"`), localEnv) {
		t.Error(`EvalCond(OS_SDK == "local") with OS_SDK=local = false, want true`)
	}

	if EvalCond(&ExprIdent{Name: "MUSL"}, DefaultIfEnv) {
		t.Error("EvalCond(MUSL) on DefaultIfEnv = true, want false (CLI-driven)")
	}

	muslEnv := buildIfEnv(ModuleInstance{
		Kind:     KindLib,
		Platform: NewPlatform(OSLinux, ISAX8664, map[string]string{"MUSL": "yes"}, nil, "", ""),
	})
	if !EvalCond(&ExprIdent{Name: "MUSL"}, muslEnv) {
		t.Error("EvalCond(MUSL) with platform MUSL=yes = false, want true")
	}
}

// TestEvalCond_StringEquality (PR-27) pins string-equality semantics:
// the env binds CXX_RT="libcxxrt"; an `IF (CXX_RT == "libcxxrt")`
// evaluates true, and `IF (CXX_RT == "other")` evaluates false.
func TestEvalCond_StringEquality(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"matching_value", `CXX_RT == "libcxxrt"`, true},
		{"non_matching_value", `CXX_RT == "libcxxabi"`, false},
		// `undefined` is bound as a string equal to its own name —
		// the libcxxrt sanitizer-type pattern. SANITIZER_TYPE is "" in M2.
		{"sanitizer_type_undefined_false", `SANITIZER_TYPE == undefined`, false},
		{"sanitizer_type_memory_false", `SANITIZER_TYPE == memory`, false},
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

// TestEvalCond_NumericLessThan (PR-27) pins int-comparison
// semantics: env binds ANDROID_API=0; `IF (ANDROID_API < 28)` is
// true, `IF (ANDROID_API < 0)` is false (strict less-than, not
// less-or-equal).
func TestEvalCond_NumericLessThan(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"strictly_less", "ANDROID_API < 28", true},
		{"equal_is_not_less", "ANDROID_API < 0", false},
		{"literal_less_literal", "0 < 28", true},
		{"literal_eq_literal_not_less", "5 < 5", false},
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

// TestEvalCond_TypeMismatch (PR-27) pins the strict type-check on
// comparator operands: a string compared to an int throws, as does
// `<` applied to non-int operands. Silent coercion would mask
// real ya.make errors; throwing surfaces them at evaluation time.
func TestEvalCond_TypeMismatch(t *testing.T) {
	cases := []struct {
		name    string
		expr    string
		wantSub string
	}{
		{
			name:    "string_lt_int_throws",
			expr:    `CXX_RT < 5`,
			wantSub: "< requires int operands",
		},
		{
			name:    "int_eq_string_throws",
			expr:    `ANDROID_API == "x"`,
			wantSub: "operand type mismatch",
		},
		{
			name:    "int_eq_bool_throws",
			expr:    `ANDROID_API == CLANG`,
			wantSub: "operand type mismatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr := parseCondForTest(t, tc.expr)
			exc := Try(func() {
				EvalCond(expr, DefaultIfEnv)
			})

			if exc == nil {
				t.Fatalf("EvalCond(%q) returned nil exception, want throw", tc.expr)
			}

			if !strings.Contains(exc.Error(), tc.wantSub) {
				t.Errorf("exception %q does not contain %q", exc.Error(), tc.wantSub)
			}
		})
	}
}

// TestEvalCond_BareLiteralInPredicateThrows (PR-27) pins that a
// string or int literal cannot stand alone as a boolean predicate —
// `IF ("foo")` and `IF (42)` are degenerate forms that the
// evaluator rejects.
func TestEvalCond_BareLiteralInPredicateThrows(t *testing.T) {
	cases := []string{`"foo"`, `42`}

	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			expr := parseCondForTest(t, c)
			exc := Try(func() {
				EvalCond(expr, DefaultIfEnv)
			})

			if exc == nil {
				t.Fatalf("EvalCond(%q) did not throw", c)
			}

			if !strings.Contains(exc.Error(), "cannot be evaluated as a boolean condition") {
				t.Errorf("exception %q does not mention boolean-condition rejection", exc.Error())
			}
		})
	}
}

// TestEnvironment_BoolMethodRejectsTypedBindings (PR-27) pins that
// using a string- or int-typed binding in boolean position throws
// (so a typo like `IF (CXX_RT)` instead of `IF (CXX_RT == "...")`
// fails fast).
func TestEnvironment_BoolMethodRejectsTypedBindings(t *testing.T) {
	// PR-M3-A: string bindings are now coerced to bool (empty→false,
	// non-empty→true) to match upstream ymake semantics for bare-ident
	// use of string variables (e.g. `IF (SANITIZER_TYPE OR ...)`). The
	// "string_in_bool_position" sub-case formerly expected a throw; it
	// is now replaced by a coercion check.  Int bindings still throw.
	t.Run("string_in_bool_position_coerces_not_throws", func(t *testing.T) {
		// CXX_RT == "libcxxrt" in DefaultIfEnv → non-empty → coerced true.
		expr := &ExprIdent{Name: "CXX_RT"}
		exc := Try(func() {
			got := EvalCond(expr, DefaultIfEnv)
			if !got {
				t.Errorf("EvalCond(CXX_RT) = false; want true (non-empty string coerces to true)")
			}
		})
		if exc != nil {
			t.Fatalf("EvalCond(CXX_RT) threw unexpectedly: %v", exc)
		}
	})

	t.Run("empty_string_in_bool_position_coerces_false", func(t *testing.T) {
		// SANITIZER_TYPE == "" in DefaultIfEnv → empty → coerced false.
		expr := &ExprIdent{Name: "SANITIZER_TYPE"}
		exc := Try(func() {
			got := EvalCond(expr, DefaultIfEnv)
			if got {
				t.Errorf("EvalCond(SANITIZER_TYPE) = true; want false (empty string coerces to false)")
			}
		})
		if exc != nil {
			t.Fatalf("EvalCond(SANITIZER_TYPE) threw unexpectedly: %v", exc)
		}
	})

	t.Run("int_in_bool_position", func(t *testing.T) {
		expr := &ExprIdent{Name: "ANDROID_API"}
		exc := Try(func() {
			EvalCond(expr, DefaultIfEnv)
		})

		if exc == nil {
			t.Fatalf("EvalCond(ANDROID_API) did not throw")
		}

		if !strings.Contains(exc.Error(), "int binding") {
			t.Errorf("exception %q does not contain %q", exc.Error(), "int binding")
		}
	})
}
