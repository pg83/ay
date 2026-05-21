package main

import (
	"reflect"
	"testing"
)

func newAllocatorDefaultTestPlatform(os OS, isa ISA, musl string) *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+2)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["PIC"] = "no"
	flags["MUSL"] = musl

	return NewPlatform(os, isa, flags, nil, "", "")
}

func TestDefaultProgramPeerdirsForWithState_NonMuslX8664GetsTcmallocDefault(t *testing.T) {
	instance := ModuleInstance{
		Path:     "prog",
		Kind:     KindBin,
		Language: LangCPP,
		Platform: newAllocatorDefaultTestPlatform(OSLinux, ISAX8664, "no"),
	}

	got := defaultProgramPeerdirsForWithState(nil, instance, FlagSet{}, false, "", false, false, false)
	want := []string{
		"build/cow/on",
		"library/cpp/malloc/tcmalloc",
		"contrib/libs/tcmalloc/no_percpu_cache",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultProgramPeerdirsForWithState() = %v, want %v", got, want)
	}
}

func TestDefaultProgramPeerdirsForWithState_NonMuslAArch64SkipsTcmallocDefault(t *testing.T) {
	instance := ModuleInstance{
		Path:     "prog",
		Kind:     KindBin,
		Language: LangCPP,
		Platform: newAllocatorDefaultTestPlatform(OSLinux, ISAAArch64, "no"),
	}

	got := defaultProgramPeerdirsForWithState(nil, instance, FlagSet{}, false, "", false, false, false)
	want := []string{"build/cow/on"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultProgramPeerdirsForWithState() = %v, want %v", got, want)
	}
}
