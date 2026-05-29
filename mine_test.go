package main

import (
	"reflect"
	"testing"
)

func TestPrebuiltToolchainFlags_UseHashedResourcePatterns(t *testing.T) {
	flags := prebuiltToolchainFlags()

	if got, want := flags["BUILD_PYTHON_BIN"], "$(YMAKE_PYTHON3-1002064631)/bin/python3"; got != want {
		t.Fatalf("BUILD_PYTHON_BIN = %q, want %q", got, want)
	}
	if got, want := flags["CLANG_TOOL"], "$(CLANG-1274503668)/bin/clang"; got != want {
		t.Fatalf("CLANG_TOOL = %q, want %q", got, want)
	}
	if got, want := flags["LLD_TOOL"], "$(LLD_ROOT-3107549726)/bin/ld.lld"; got != want {
		t.Fatalf("LLD_TOOL = %q, want %q", got, want)
	}
	if got, want := flags["CLANG16_RESOURCE_GLOBAL"], "CLANG16_RESOURCE_GLOBAL::$(CLANG16-1380963495)"; got != want {
		t.Fatalf("CLANG16_RESOURCE_GLOBAL = %q, want %q", got, want)
	}
	if got, want := flags["CLANG18_RESOURCE_GLOBAL"], "CLANG18_RESOURCE_GLOBAL::$(CLANG18-1866954364)"; got != want {
		t.Fatalf("CLANG18_RESOURCE_GLOBAL = %q, want %q", got, want)
	}
	if got, want := flags["CLANG20_RESOURCE_GLOBAL"], "CLANG20_RESOURCE_GLOBAL::$(CLANG20-178457234)"; got != want {
		t.Fatalf("CLANG20_RESOURCE_GLOBAL = %q, want %q", got, want)
	}
	if got, want := flags["LLD_ROOT_RESOURCE_GLOBAL"], "LLD_ROOT_RESOURCE_GLOBAL::$(LLD_ROOT-3107549726)"; got != want {
		t.Fatalf("LLD_ROOT_RESOURCE_GLOBAL = %q, want %q", got, want)
	}
}

func TestGraphConfForToolchainFlags_HashesResourcePatternsAndKeepsVCSStub(t *testing.T) {
	const bundleBody = `{"by_platform":{"linux-x86_64":{"uri":"sbr:linux"},"linux-aarch64":{"uri":"sbr:aarch64"},"darwin-x86_64":{"uri":"sbr:darwin"},"darwin-arm64":{"uri":"sbr:darwin-arm64"},"win32-x86_64":{"uri":"sbr:win32"}}}`

	files := map[string]string{}
	for _, rel := range []string{
		"build/platform/python/ymake_python3/resources.json",
		"build/platform/clang/clang16.json",
		"build/platform/clang/clang18.json",
		"build/platform/lld/lld20.json",
		"build/platform/clang/clang20.json",
		"build/platform/java/jdk/jdk17/jdk.json",
	} {
		files[rel] = bundleBody
	}

	fs := newMemFS(files)
	conf := graphConfForToolchainFlags(fs, prebuiltToolchainFlags())
	if conf == nil {
		t.Fatal("graphConfForToolchainFlags returned nil")
	}

	var gotPatterns []string
	for _, r := range conf.Resources {
		gotPatterns = append(gotPatterns, r.Pattern)
	}
	wantPatterns := []string{
		resourcePatternYMakePython3,
		resourcePatternClang16,
		resourcePatternClang18,
		resourcePatternLLDRoot,
		resourcePatternClangTool,
		resourcePatternClang20,
		resourcePatternJDK17,
		"VCS",
	}
	if !reflect.DeepEqual(gotPatterns, wantPatterns) {
		t.Fatalf("resource patterns = %#v, want %#v", gotPatterns, wantPatterns)
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
