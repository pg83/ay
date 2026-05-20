package main

import (
	"os"
	"path/filepath"
	"testing"
)

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

	if err := os.WriteFile(filepath.Join(dir, "pkg/logger.pxd"), []byte("cdef extern from 'library/cpp/logger/priority.h':\n    pass\n"), 0o644); err != nil {
		t.Fatalf("write logger.pxd: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "library/cpp/logger/priority.h"), []byte("// priority\n"), 0o644); err != nil {
		t.Fatalf("write priority.h: %v", err)
	}

	scanner := NewIncludeScanner(dir, SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "pkg/logger.pxd",
		OwnAddIncl: []VFS{Source("")},
	})

	want := Source("library/cpp/logger/priority.h")
	if len(closure) != 1 || closure[0] != want {
		t.Fatalf("closure = %v, want [%v]", closure, want)
	}
}

func TestScanner_CythonExternFromQuotedAngleResolves(t *testing.T) {
	dir := t.TempDir()

	for _, rel := range []string{
		"pkg",
		"util/generic",
	} {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "pkg/string_user.pxd"), []byte("cdef extern from \"<util/generic/string.h>\":\n    pass\n"), 0o644); err != nil {
		t.Fatalf("write string_user.pxd: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "util/generic/string.h"), []byte("// string\n"), 0o644); err != nil {
		t.Fatalf("write string.h: %v", err)
	}

	scanner := NewIncludeScanner(dir, SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel:  "pkg/string_user.pxd",
		OwnAddIncl: []VFS{Source("")},
	})

	want := Source("util/generic/string.h")
	if len(closure) != 1 || closure[0] != want {
		t.Fatalf("closure = %v, want [%v]", closure, want)
	}
}
