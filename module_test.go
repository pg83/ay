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

	// A distinct ModuleInstance keyed on the host platform must miss
	// in the map even when its path/language match `a`. Construct one
	// inline to mimic what the production tool-spawn sites do via
	// NewToolInstance.
	host := ModuleInstance{
		Path:     a.Path,
		Language: a.Language,
		Platform: testHostP,
		Flags:    inferFlagsFromPath(a.Path, true),
	}
	if host == a {
		t.Errorf("host-axis copy compared equal to target-axis original: %v == %v", host, a)
	}

	if _, ok := memo[host]; ok {
		t.Errorf("memo[host] unexpectedly hit; expected miss for distinct instance")
	}
}

// TestNewToolInstance verifies that NewToolInstance builds an
// instance from scratch — fresh Flags inferred from the tool's own
// path, not copied from a surrounding module.
func TestNewToolInstance(t *testing.T) {
	tool := NewToolInstance(testHostP, "contrib/tools/ragel6", LangCPP)

	if tool.Path != "contrib/tools/ragel6" {
		t.Errorf("Path = %q, want contrib/tools/ragel6", tool.Path)
	}

	if tool.Language != LangCPP {
		t.Errorf("Language = %q, want LangCPP", tool.Language)
	}

	if tool.Platform != testHostP {
		t.Errorf("Platform != testHostP")
	}

	if !tool.Flags.PIC {
		t.Errorf("Flags.PIC = false, want true (host axis)")
	}

	// Tool path is not in inferFlagsFromPath's special-case set, so
	// no extra flags should be set.
	if tool.Flags.NoLibc || tool.Flags.LibcMusl {
		t.Errorf("tool Flags carry unrelated module flags: %+v", tool.Flags)
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
