package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDelimitedIncludeTarget_QuotedAngleSystem(t *testing.T) {
	target, kind, ok := parseDelimitedIncludeTarget("\"<util/system/error.h>\"")
	if !ok {
		t.Fatal("parseDelimitedIncludeTarget returned ok=false")
	}

	if target != "util/system/error.h" {
		t.Fatalf("target = %q, want %q", target, "util/system/error.h")
	}

	if kind != includeSystem {
		t.Fatalf("kind = %v, want includeSystem", kind)
	}
}

func TestParseCIncludes_IncludeNextNotMisparsed(t *testing.T) {
	got := parseCIncludes([]byte("#if __has_include_next(<stdlib.h>)\n#    include_next <stdlib.h>\n#endif\n"))

	for _, d := range got {
		if d.target.String() == "_next" {
			t.Fatalf("#include_next misparsed as include %q; directives: %+v", d.target.String(), got)
		}
	}

	if len(got) != 0 {
		t.Fatalf("expected no directives from an #include_next block, got %+v", got)
	}

	norm := parseCIncludes([]byte("#include <foo/bar.h>\n#include \"baz.h\"\n"))
	if len(norm) != 2 || norm[0].target.String() != "foo/bar.h" || norm[1].target.String() != "baz.h" {
		t.Fatalf("normal #include parsing regressed: %+v", norm)
	}
}

func TestScanner_CythonExternFromQuotedAngleResolves(t *testing.T) {
	dir := t.TempDir()

	for _, rel := range []string{
		"pkg",
		"util/system",
	} {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "pkg/error.pxd"), []byte("cdef extern from \"<util/system/error.h>\"\n"), 0o644); err != nil {
		t.Fatalf("write pkg/error.pxd: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "util/system/error.h"), []byte("// error.h\n"), 0o644); err != nil {
		t.Fatalf("write util/system/error.h: %v", err)
	}

	scanner := NewIncludeScanner(dir, SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "pkg/error.pxd",
		OwnAddIncl: []VFS{Intern("$(S)/")},
	})

	if len(closure) != 1 {
		t.Fatalf("closure len = %d, want 1; got %v", len(closure), closure)
	}

	if got := closure[0].String(); got != "$(S)/util/system/error.h" {
		t.Fatalf("closure[0] = %q, want %q", got, "$(S)/util/system/error.h")
	}
}

func TestScanner_CythonExternFromSingleQuotedResolves(t *testing.T) {
	dir := t.TempDir()

	for _, rel := range []string{
		"pkg",
		"library/cpp/logger",
	} {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "pkg/logger.pxd"), []byte("cdef extern from 'library/cpp/logger/priority.h':\n"), 0o644); err != nil {
		t.Fatalf("write pkg/logger.pxd: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "library/cpp/logger/priority.h"), []byte("// priority.h\n"), 0o644); err != nil {
		t.Fatalf("write library/cpp/logger/priority.h: %v", err)
	}

	scanner := NewIncludeScanner(dir, SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "pkg/logger.pxd",
		OwnAddIncl: []VFS{Intern("$(S)/")},
	})

	if len(closure) != 1 {
		t.Fatalf("closure len = %d, want 1; got %v", len(closure), closure)
	}

	if got := closure[0].String(); got != "$(S)/library/cpp/logger/priority.h" {
		t.Fatalf("closure[0] = %q, want %q", got, "$(S)/library/cpp/logger/priority.h")
	}
}
