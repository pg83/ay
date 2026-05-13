package main

import (
	"testing"
)

// module_test.go — invariants for ModuleInstance / FlagSet (D30..D34).

// TestModuleInstance_Equality_Hashing verifies that ModuleInstance
// compares by value (so it can be used as a map key per D34) and
// that two semantically-equal instances hash to the same map slot.
func TestModuleInstance_Equality_Hashing(t *testing.T) {
	a := ModuleInstance{
		Path:     "build/cow/on",
		Language: LangCPP,
		Platform: testTargetP,
		Flags:    inferFlagsFromPath("build/cow/on", false),
	}

	b := ModuleInstance{
		Path:     "build/cow/on",
		Language: LangCPP,
		Platform: testTargetP,
		Flags:    inferFlagsFromPath("build/cow/on", false),
	}

	if a != b {
		t.Errorf("two semantically-equal ModuleInstances compared unequal: %v vs %v", a, b)
	}

	// Map-key usage: storing under `a`, looking up via `b` must
	// return the same value.
	memo := map[ModuleInstance]string{a: "hit"}

	if got := memo[b]; got != "hit" {
		t.Errorf("memo[b] = %q, want %q (map-key dispatch broken)", got, "hit")
	}

	// A different platform → a different instance → a different
	// map slot.
	host := a.WithHost(testHostP)
	if host == a {
		t.Errorf("WithHost did not produce a distinct instance: %v == %v", host, a)
	}

	if _, ok := memo[host]; ok {
		t.Errorf("memo[host] unexpectedly hit; expected miss for distinct instance")
	}
}

// TestModuleInstance_WithHost_FlipsTargetAndPIC verifies the
// host-flip discipline: same Path/Language, host platform, PIC=true.
func TestModuleInstance_WithHost_FlipsTargetAndPIC(t *testing.T) {
	mi := ModuleInstance{
		Path:     "build/cow/on",
		Language: LangCPP,
		Platform: testTargetP,
		Flags:    inferFlagsFromPath("build/cow/on", false),
	}

	if mi.Flags.PIC {
		t.Fatalf("seed instance has PIC=true; want false")
	}

	host := mi.WithHost(testHostP)

	if host.Path != mi.Path {
		t.Errorf("host.Path = %q, want %q", host.Path, mi.Path)
	}

	if host.Language != mi.Language {
		t.Errorf("host.Language = %q, want %q", host.Language, mi.Language)
	}

	if host.Platform != testHostP {
		t.Errorf("host.Platform != testHostP")
	}

	if !host.Flags.PIC {
		t.Errorf("host.Flags.PIC = false, want true")
	}

	// The original instance must be untouched (value semantics).
	if mi.Flags.PIC {
		t.Errorf("WithHost mutated the receiver; mi.Flags.PIC = true")
	}

	if mi.Platform != testTargetP {
		t.Errorf("WithHost mutated the receiver; mi.Platform != testTargetP")
	}
}

// TestModuleInstance_String_Diagnostic verifies that String()
// produces a stable diagnostic representation.
func TestModuleInstance_String_Diagnostic(t *testing.T) {
	mi := ModuleInstance{
		Path:     "build/cow/on",
		Language: LangCPP,
		Platform: testTargetP,
	}

	got := mi.String()
	want := "build/cow/on:cpp@default-linux-aarch64"

	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestNewFlagSet_SortsExtra verifies that NewFlagSet's Extra is
// sorted (so two FlagSets with the same flag set in different
// declaration orders compare equal).
func TestNewFlagSet_SortsExtra(t *testing.T) {
	a := NewFlagSet("z", "a", "m")
	b := NewFlagSet("a", "m", "z")

	if a != b {
		t.Errorf("FlagSets with same tokens in different order compared unequal: %q vs %q", a.Extra, b.Extra)
	}
}
