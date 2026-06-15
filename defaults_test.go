package main

import (
	"reflect"
	"testing"
)

func newAllocatorDefaultTestPlatform(os OS, isa ISA) *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+1)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["PIC"] = "no"

	return newPlatform(newMemFS(nil), os, isa, flags, "", "")
}

func TestDefaultProgramPeerdirsForWithState_X8664GetsTcmallocDefault(t *testing.T) {
	instance := ModuleInstance{
		Path:     source("prog"),
		Kind:     KindBin,
		Language: LangCPP,
		Platform: newAllocatorDefaultTestPlatform(OSLinux, ISAX8664),
	}

	got := defaultProgramPeerdirsForWithState(nil, instance, &ModuleData{}, false)
	want := []string{
		"build/cow/on",
		"library/cpp/malloc/tcmalloc",
		"contrib/libs/tcmalloc/no_percpu_cache",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultProgramPeerdirsForWithState() = %v, want %v", got, want)
	}
}

func TestDefaultProgramPeerdirsForWithState_AArch64SkipsTcmallocDefault(t *testing.T) {
	instance := ModuleInstance{
		Path:     source("prog"),
		Kind:     KindBin,
		Language: LangCPP,
		Platform: newAllocatorDefaultTestPlatform(OSLinux, ISAAArch64),
	}

	got := defaultProgramPeerdirsForWithState(nil, instance, &ModuleData{}, false)
	want := []string{"build/cow/on"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultProgramPeerdirsForWithState() = %v, want %v", got, want)
	}
}
