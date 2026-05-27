package main

import (
	"strings"
	"testing"
)

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

func TestEvalCond_UnknownVarDefaultsFalse(t *testing.T) {
	expr := parseCondForTest(t, "UNKNOWN_VAR")
	if EvalCond(expr, DefaultIfEnv) {
		t.Fatal("EvalCond(UNKNOWN_VAR) = true, want false")
	}
}

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

func TestEvalCond_StringEquality(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"matching_value", `CXX_RT == "libcxxrt"`, true},
		{"non_matching_value", `CXX_RT == "libcxxabi"`, false},

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

func TestEnvironment_BoolMethodRejectsTypedBindings(t *testing.T) {

	t.Run("string_in_bool_position_coerces_not_throws", func(t *testing.T) {

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

func TestDefaultIfEnv_DLL_FORDefaultsFalse(t *testing.T) {
	if EvalCond(&ExprIdent{Name: "DLL_FOR"}, DefaultIfEnv) {
		t.Fatalf("EvalCond(DLL_FOR) = true, want false")
	}
}

func TestDefaultIfEnv_DYNAMIC_BOOSTDefaultsFalse(t *testing.T) {
	if EvalCond(&ExprIdent{Name: "DYNAMIC_BOOST"}, DefaultIfEnv) {
		t.Fatalf("EvalCond(DYNAMIC_BOOST) = true, want false")
	}
}

func TestDefaultIfEnv_USE_SSE4DefaultsTrue(t *testing.T) {
	if !EvalCond(&ExprIdent{Name: "USE_SSE4"}, DefaultIfEnv) {
		t.Fatalf("EvalCond(USE_SSE4) = false, want true")
	}
}

func TestDefaultIfEnv_PROFILE_MEMORY_ALLOCATIONSDefaultsFalse(t *testing.T) {
	if EvalCond(&ExprIdent{Name: "PROFILE_MEMORY_ALLOCATIONS"}, DefaultIfEnv) {
		t.Fatalf("EvalCond(PROFILE_MEMORY_ALLOCATIONS) = true, want false")
	}
}

func TestDefaultIfEnv_ALLOCATORDefaultsEmpty(t *testing.T) {
	if got := DefaultIfEnv.String("ALLOCATOR"); got != "" {
		t.Fatalf("DefaultIfEnv.String(ALLOCATOR) = %q, want empty", got)
	}
}

func TestEvalCond_BoolStringEqualityCoercion(t *testing.T) {
	if !EvalCond(parseCondForTest(t, `USE_SSE4 == "yes"`), DefaultIfEnv) {
		t.Fatalf(`EvalCond(USE_SSE4 == "yes") = false, want true`)
	}
	if !EvalCond(parseCondForTest(t, `PROFILE_MEMORY_ALLOCATIONS == "no"`), DefaultIfEnv) {
		t.Fatalf(`EvalCond(PROFILE_MEMORY_ALLOCATIONS == "no") = false, want true`)
	}
}
