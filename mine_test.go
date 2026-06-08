package main

import (
	"reflect"
	"testing"
)

func TestPrebuiltToolchainFlags_UseHashedResourcePatterns(t *testing.T) {
	flags := prebuiltToolchainFlags()

	if got, want := flags["BUILD_PYTHON_BIN"], "$(YMAKE_PYTHON3)/bin/python3"; got != want {
		t.Fatalf("BUILD_PYTHON_BIN = %q, want %q", got, want)
	}
	if got, want := flags["CLANG_TOOL"], "$(CLANG)/bin/clang"; got != want {
		t.Fatalf("CLANG_TOOL = %q, want %q", got, want)
	}
	if got, want := flags["LLD_TOOL"], "$(LLD_ROOT)/bin/ld.lld"; got != want {
		t.Fatalf("LLD_TOOL = %q, want %q", got, want)
	}
	if got, want := flags["CLANG16_RESOURCE_GLOBAL"], "CLANG16_RESOURCE_GLOBAL::$(CLANG16)"; got != want {
		t.Fatalf("CLANG16_RESOURCE_GLOBAL = %q, want %q", got, want)
	}
	if got, want := flags["CLANG18_RESOURCE_GLOBAL"], "CLANG18_RESOURCE_GLOBAL::$(CLANG18)"; got != want {
		t.Fatalf("CLANG18_RESOURCE_GLOBAL = %q, want %q", got, want)
	}
	if got, want := flags["CLANG20_RESOURCE_GLOBAL"], "CLANG20_RESOURCE_GLOBAL::$(CLANG20)"; got != want {
		t.Fatalf("CLANG20_RESOURCE_GLOBAL = %q, want %q", got, want)
	}
	// build/platform/lld's --ld-path=${LLD_ROOT_RESOURCE_GLOBAL}/bin/ld.lld needs
	// the bare $(LLD_ROOT) dir, not the --global-resource token.
	if got, want := flags["LLD_ROOT_RESOURCE_GLOBAL"], "$(LLD_ROOT)"; got != want {
		t.Fatalf("LLD_ROOT_RESOURCE_GLOBAL = %q, want %q", got, want)
	}
}

func TestGraphConfForToolchainFlags_KeepsOnlyVCSStub(t *testing.T) {
	// Toolchain resources (CLANG*, LLD_ROOT, YMAKE_PYTHON3, …) are now declared by
	// the build/platform/* RESOURCES_LIBRARY modules and fetched via emitResourceFetch;
	// graphConfForToolchainFlags carries only the inline VCS stub no module declares.
	conf := graphConfForToolchainFlags(newMemFS(nil), prebuiltToolchainFlags())
	if conf == nil {
		t.Fatal("graphConfForToolchainFlags returned nil")
	}

	var gotPatterns []string
	for _, r := range conf.Resources {
		gotPatterns = append(gotPatterns, r.Pattern)
	}

	if !reflect.DeepEqual(gotPatterns, []string{"VCS"}) {
		t.Fatalf("resource patterns = %#v, want [VCS]", gotPatterns)
	}

	last := conf.Resources[len(conf.Resources)-1]
	if last.Name != "vcs" || last.Resource != "base64:vcs.json:e30=" {
		t.Fatalf("vcs stub = %#v, want name=vcs resource=base64:vcs.json:e30=", last)
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
