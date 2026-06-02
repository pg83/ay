package main

import (
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
	scanner := newTestScanner(newMemFS(map[string]string{
		"pkg/error.pxd":       "cdef extern from \"<util/system/error.h>\"\n",
		"util/system/error.h": "// error.h\n",
	}), SysInclSet{})
	closure := scanClosure(scanner, ScanContext{
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
	scanner := newTestScanner(newMemFS(map[string]string{
		"pkg/logger.pxd":                "cdef extern from 'library/cpp/logger/priority.h':\n",
		"library/cpp/logger/priority.h": "// priority.h\n",
	}), SysInclSet{})
	closure := scanClosure(scanner, ScanContext{
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
