package main

import (
	"reflect"
	"testing"
)

func TestSwigIncludeClosure_ParsesIAndSystemRoots(t *testing.T) {
	fs := newMemFS(map[string]string{
		"mod/src.swg":                              "%module x\n%include \"local.i\"\n",
		"mod/local.i":                              "%import <typemaps.i>\n%include \"nested.i\"\n",
		"mod/nested.i":                             "%module nested\n",
		"contrib/tools/swig/Lib/swig.swg":          "%module swig\n",
		"contrib/tools/swig/Lib/go.swg":            "%module go\n",
		"contrib/tools/swig/Lib/java.swg":          "%module java\n",
		"contrib/tools/swig/Lib/perl5.swg":         "%include \"perl5/reference.i\"\n",
		"contrib/tools/swig/Lib/python.swg":        "%module python\n",
		"contrib/tools/swig/Lib/go/typemaps.i":     "%module gotypes\n",
		"contrib/tools/swig/Lib/java/typemaps.i":   "%module javatypes\n",
		"contrib/tools/swig/Lib/perl5/typemaps.i":  "%module perltypes\n",
		"contrib/tools/swig/Lib/perl5/reference.i": "%module perlref\n",
	})

	ctx := &genCtx{fs: fs}
	closure := swigIncludeClosure(ctx, Intern("$(S)/mod/src.swg"))

	got := make([]string, 0, len(closure))
	for _, v := range closure {
		got = append(got, v.Rel())
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
	fs := newMemFS(map[string]string{
		"mod/src.swg": `%include "local.i"
%{
#include <Python.h>
#include "archive.h"
%}
`,
		"mod/local.i": `%include "nested.i"
%{
#include <jni.h>
#include <Python.h>
%}
`,
		"mod/nested.i": `%{
#include "archive_entry.h"
%}
`,
		"contrib/tools/swig/Lib/swig.swg":   "%module swig\n",
		"contrib/tools/swig/Lib/go.swg":     "%module go\n",
		"contrib/tools/swig/Lib/java.swg":   "%module java\n",
		"contrib/tools/swig/Lib/perl5.swg":  "%module perl\n",
		"contrib/tools/swig/Lib/python.swg": "%module python\n",
	})

	ctx := &genCtx{fs: fs}
	closure := swigIncludeClosure(ctx, Intern("$(S)/mod/src.swg"))
	got := collectSwigInducedIncludes(ctx, Intern("$(S)/mod/src.swg"), closure)
	want := []includeDirective{
		{kind: includeSystem, target: internStr("Python.h")},
		{kind: includeQuoted, target: internStr("archive.h")},
		{kind: includeSystem, target: internStr("jni.h")},
		{kind: includeQuoted, target: internStr("archive_entry.h")},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectSwigInducedIncludes mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
