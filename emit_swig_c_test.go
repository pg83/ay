package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSwigIncludeClosure_ParsesIAndSystemRoots(t *testing.T) {
	root := t.TempDir()

	writeSwigTestFile(t, root, "mod/src.swg", `%module x
%include "local.i"
`)
	writeSwigTestFile(t, root, "mod/local.i", `%import <typemaps.i>
%include "nested.i"
`)
	writeSwigTestFile(t, root, "mod/nested.i", `%module nested
`)

	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/swig.swg", `%module swig
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/go.swg", `%module go
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/java.swg", `%module java
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/perl5.swg", `%include "perl5/reference.i"
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/python.swg", `%module python
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/go/typemaps.i", `%module gotypes
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/java/typemaps.i", `%module javatypes
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/perl5/typemaps.i", `%module perltypes
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/perl5/reference.i", `%module perlref
`)

	ctx := &genCtx{fs: NewFS(root)}
	closure := swigIncludeClosure(ctx, Source("mod/src.swg"))

	got := make([]string, 0, len(closure))
	for _, v := range closure {
		got = append(got, v.Rel)
	}

	want := []string{
		"contrib/tools/swig/Lib/go.swg",
		"contrib/tools/swig/Lib/go/typemaps.i",
		"contrib/tools/swig/Lib/java.swg",
		"contrib/tools/swig/Lib/java/typemaps.i",
		"contrib/tools/swig/Lib/perl5.swg",
		"contrib/tools/swig/Lib/perl5/reference.i",
		"contrib/tools/swig/Lib/perl5/typemaps.i",
		"contrib/tools/swig/Lib/python.swg",
		"contrib/tools/swig/Lib/swig.swg",
		"mod/local.i",
		"mod/nested.i",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("swigIncludeClosure mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestCollectSwigInducedIncludes_DedupsAcrossClosure(t *testing.T) {
	root := t.TempDir()

	writeSwigTestFile(t, root, "mod/src.swg", `%include "local.i"
%{
#include <Python.h>
#include "archive.h"
%}
`)
	writeSwigTestFile(t, root, "mod/local.i", `%include "nested.i"
%{
#include <jni.h>
#include <Python.h>
%}
`)
	writeSwigTestFile(t, root, "mod/nested.i", `%{
#include "archive_entry.h"
%}
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/swig.swg", `%module swig
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/go.swg", `%module go
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/java.swg", `%module java
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/perl5.swg", `%module perl
`)
	writeSwigTestFile(t, root, "contrib/tools/swig/Lib/python.swg", `%module python
`)

	ctx := &genCtx{fs: NewFS(root)}
	closure := swigIncludeClosure(ctx, Source("mod/src.swg"))
	got := collectSwigInducedIncludes(ctx, Source("mod/src.swg"), closure)
	want := []includeDirective{
		{kind: includeSystem, target: "Python.h"},
		{kind: includeQuoted, target: "archive.h"},
		{kind: includeSystem, target: "jni.h"},
		{kind: includeQuoted, target: "archive_entry.h"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectSwigInducedIncludes mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func writeSwigTestFile(t *testing.T, root, rel, contents string) {
	t.Helper()

	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}
