package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestThrow_NilErrorDoesNotPanic(t *testing.T) {
	if exc := try(func() { throw(nil) }); exc != nil {
		t.Fatalf("throw(nil) raised: %v", exc.error())
	}
}

func TestThrow_ErrorPanicsAndTryCatchesIt(t *testing.T) {
	sentinel := errors.New("boom")
	exc := try(func() { throw(sentinel) })

	if exc == nil {
		t.Fatal("try returned nil for a thrown error")
	}

	if exc.unwrap() != sentinel {
		t.Fatalf("unwrap = %v, want the sentinel", exc.unwrap())
	}
}

func TestThrow2_PassesValueThrough(t *testing.T) {
	if got := throw2(42, nil); got != 42 {
		t.Fatalf("throw2 = %d, want 42", got)
	}
}

func TestThrow2_ThrowsOnError(t *testing.T) {
	exc := try(func() {
		_ = throw2("ignored", errors.New("io failed"))
	})

	if exc == nil || exc.error() != "io failed" {
		t.Fatalf("exc = %v, want io failed", exc)
	}
}

func TestThrowFmt_FormatsTheMessage(t *testing.T) {
	exc := try(func() { throwFmt("bad %s at %d", "token", 7) })

	if exc == nil || exc.error() != "bad token at 7" {
		t.Fatalf("exc = %v, want formatted message", exc)
	}
}

func TestTry_CleanCallbackReturnsNil(t *testing.T) {
	if exc := try(func() {}); exc != nil {
		t.Fatalf("try of a clean callback = %v, want nil", exc)
	}
}

func TestTry_ForeignPanicIsNotSwallowed(t *testing.T) {
	defer func() {
		if rec := recover(); rec != "not an exception" {
			t.Fatalf("recovered %v, want the foreign panic", rec)
		}
	}()

	try(func() { panic("not an exception") })
	t.Fatal("foreign panic was swallowed")
}

func TestCatch_RunsOnlyOnException(t *testing.T) {
	ran := false

	try(func() {}).catch(func(*Exception) { ran = true })

	if ran {
		t.Fatal("catch ran for a nil exception")
	}

	try(func() { throwFmt("x") }).catch(func(*Exception) { ran = true })

	if !ran {
		t.Fatal("catch did not run for a thrown exception")
	}
}

func TestAsError_NilExceptionIsNilError(t *testing.T) {
	if err := try(func() {}).asError(); err != nil {
		t.Fatalf("asError of nil = %v, want nil", err)
	}

	sentinel := errors.New("boom")

	if err := try(func() { throw(sentinel) }).asError(); err != sentinel {
		t.Fatalf("asError = %v, want the sentinel", err)
	}
}

// Error/Unwrap wrap error()/unwrap(): errors.Is/As and %v must see through an
// *Exception.
func TestException_ErrorsChainAndFormatting(t *testing.T) {
	sentinel := errors.New("root cause")
	exc := try(func() { throw(fmt.Errorf("wrapped: %w", sentinel)) })

	if !errors.Is(exc, sentinel) {
		t.Fatal("errors.Is does not reach the sentinel through Exception")
	}

	if exc.Error() != exc.error() || exc.Error() != "wrapped: root cause" {
		t.Fatalf("Error = %q, want %q", exc.Error(), "wrapped: root cause")
	}

	if got := fmt.Sprintf("%v", exc); got != "wrapped: root cause" {
		t.Fatalf("%%v = %q, want the message", got)
	}
}
