package main

import (
	"reflect"
	"testing"
)

func TestPrebuiltToolchainFlags_CarryConfigNotToolPaths(t *testing.T) {
	flags := prebuiltToolchainFlags()

	// CLANG_VER (a scalar version) stays a config flag; the tool *paths* and the
	// *_RESOURCE_GLOBAL vars do not — they come from the build/platform/* PEERDIR
	// closure (resolveModuleToolchain / DECLARE_*), never from ambient flags.
	if got, want := flags["CLANG_VER"], "20"; got != want {
		t.Fatalf("CLANG_VER = %q, want %q", got, want)
	}

	for _, k := range []string{
		"CLANG_TOOL", "CLANG_pl_pl_TOOL", "AR_TOOL", "OBJCOPY_TOOL", "STRIP_TOOL",
		"LLD_TOOL", "BUILD_PYTHON_BIN", "BUILD_PYTHON3_BIN",
		"CLANG16_RESOURCE_GLOBAL", "LLD_ROOT_RESOURCE_GLOBAL",
	} {
		if got, ok := flags[k]; ok {
			t.Fatalf("%s unexpectedly present in prebuiltToolchainFlags = %q (must come from peerdir)", k, got)
		}
	}
}

func TestGraphConfForToolchainFlags_NoResources(t *testing.T) {
	// Every resource is now a real graph node: toolchain bundles (CLANG*, LLD_ROOT,
	// YMAKE_PYTHON3) via emitResourceFetch, vcs.json via emitVCSNode. Nothing is
	// resolved out-of-band, so graphConfForToolchainFlags carries no resources and the
	// emitted graph has no conf section.
	conf := graphConfForToolchainFlags()
	if conf == nil {
		t.Fatal("graphConfForToolchainFlags returned nil")
	}

	if len(conf.Resources) != 0 {
		t.Fatalf("resources = %#v, want none", conf.Resources)
	}
}

func TestReadYaConfSections_MergesLaterFilesAndSkipsMissing(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.conf": `[flags]
ROOT_ONLY = "root"
SHARED = "root"
`,
		"build/internal/ya.conf": `[flags]
INTERNAL_ONLY = "internal"
SHARED = "internal"
`,
	})

	got := readYaConfSections(fs, "flags", "ya.conf", "missing/ya.conf", "build/internal/ya.conf")
	want := map[string]string{
		"ROOT_ONLY":     "root",
		"INTERNAL_ONLY": "internal",
		"SHARED":        "internal",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readYaConfSections() = %#v, want %#v", got, want)
	}
}
