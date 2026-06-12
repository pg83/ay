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
	block := make([]IncludeDirective, 64)
	got := block[:parseCIncludes([]byte("#if __has_include_next(<stdlib.h>)\n#    include_next <stdlib.h>\n#endif\n"), block, 0)]

	for _, d := range got {
		if d.target.string() == "_next" {
			t.Fatalf("#include_next misparsed as include %q; directives: %+v", d.target.string(), got)
		}
	}

	if len(got) != 0 {
		t.Fatalf("expected no directives from an #include_next block, got %+v", got)
	}

	nblock := make([]IncludeDirective, 64)
	norm := nblock[:parseCIncludes([]byte("#include <foo/bar.h>\n#include \"baz.h\"\n"), nblock, 0)]
	if len(norm) != 2 || norm[0].target.string() != "foo/bar.h" || norm[1].target.string() != "baz.h" {
		t.Fatalf("normal #include parsing regressed: %+v", norm)
	}
}

func TestScanner_CythonExternFromQuotedAngleResolves(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"pkg/error.pxd":       "cdef extern from \"<util/system/error.h>\"\n",
		"util/system/error.h": "// error.h\n",
	}), SysInclSet{})
	closure := scanClosure(scanner, "pkg/error.pxd", newScanContext(scanner.parsers, []VFS{intern("$(S)/")}, nil, nil, ""))

	if len(closure) != 1 {
		t.Fatalf("closure len = %d, want 1; got %v", len(closure), closure)
	}

	if got := closure[0].string(); got != "$(S)/util/system/error.h" {
		t.Fatalf("closure[0] = %q, want %q", got, "$(S)/util/system/error.h")
	}
}

func TestScanner_CythonExternFromSingleQuotedResolves(t *testing.T) {
	scanner := newTestScanner(newMemFS(map[string]string{
		"pkg/logger.pxd":                "cdef extern from 'library/cpp/logger/priority.h':\n",
		"library/cpp/logger/priority.h": "// priority.h\n",
	}), SysInclSet{})
	closure := scanClosure(scanner, "pkg/logger.pxd", newScanContext(scanner.parsers, []VFS{intern("$(S)/")}, nil, nil, ""))

	if len(closure) != 1 {
		t.Fatalf("closure len = %d, want 1; got %v", len(closure), closure)
	}

	if got := closure[0].string(); got != "$(S)/library/cpp/logger/priority.h" {
		t.Fatalf("closure[0] = %q, want %q", got, "$(S)/library/cpp/logger/priority.h")
	}
}
