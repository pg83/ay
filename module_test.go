package main

import (
	"testing"
)

func TestModuleInstance_Equality_Hashing(t *testing.T) {
	a := ModuleInstance{
		Path:     "build/cow/on",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	b := ModuleInstance{
		Path:     "build/cow/on",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	if a != b {
		t.Errorf("two semantically-equal ModuleInstances compared unequal: %v vs %v", a, b)
	}

	memo := map[ModuleInstance]string{a: "hit"}

	if got := memo[b]; got != "hit" {
		t.Errorf("memo[b] = %q, want %q (map-key dispatch broken)", got, "hit")
	}

	host := ModuleInstance{
		Path:     a.Path,
		Kind:     KindLib,
		Language: a.Language,
		Platform: testHostP,
	}
	if host == a {
		t.Errorf("host-axis copy compared equal to target-axis original: %v == %v", host, a)
	}

	if _, ok := memo[host]; ok {
		t.Errorf("memo[host] unexpectedly hit; expected miss for distinct instance")
	}
}

func TestNewToolInstance(t *testing.T) {
	tool := NewToolInstance(testHostP, "contrib/tools/ragel6")

	if tool.Path != "contrib/tools/ragel6" {
		t.Errorf("Path = %q, want contrib/tools/ragel6", tool.Path)
	}

	if tool.Language != LangCPP {
		t.Errorf("Language = %q, want LangCPP", tool.Language)
	}

	if tool.Platform != testHostP {
		t.Errorf("Platform != testHostP")
	}

	if !tool.Platform.PIC {
		t.Errorf("Platform.PIC = false, want true (host axis)")
	}
}

func TestModuleInstance_String_Diagnostic(t *testing.T) {
	mi := ModuleInstance{
		Path:     "build/cow/on",
		Kind:     KindLib,
		Language: LangCPP,
		Platform: testTargetP,
	}

	got := mi.String()
	want := "build/cow/on[lib]:cpp@default-linux-aarch64"

	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
