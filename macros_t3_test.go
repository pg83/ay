package main

import "testing"

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
