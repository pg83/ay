package main

import (
	"reflect"
	"testing"
)

func TestSwigParser_ImplicitIncludesOnRootSwg(t *testing.T) {
	set := SwigIncludeDirectiveParser{}.parse("mod/src.swg", []byte("%include \"local.i\"\n"), newBumpAllocator[IncludeDirective]())
	local := set.bucket(parsedIncludesLocal)

	want := []string{"swig.swg", "go.swg", "java.swg", "perl5.swg", "python.swg", "local.i"}
	got := make([]string, 0, len(local))

	for _, d := range local {
		got = append(got, d.target.string())
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root .swg directives = %v, want %v", got, want)
	}

	libSet := SwigIncludeDirectiveParser{}.parse("contrib/tools/swig/Lib/python/python.swg", []byte("%include \"pyrun.swg\"\n"), newBumpAllocator[IncludeDirective]())

	if got := len(libSet.bucket(parsedIncludesLocal)); got != 1 {
		t.Fatalf("Lib .swg directives = %d, want 1 (no implicit prefix)", got)
	}
}

func TestCollectSwigInducedIncludes_UnionAcrossClosure(t *testing.T) {
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

	scanner := newTestScanner(fs, SysInclSet{})

	closure := []VFS{source("mod/local.i"), source("mod/nested.i")}
	got := collectSwigInducedIncludes(scanner, source("mod/src.swg"), closure)

	want := []IncludeDirective{
		{kind: includeSystem, target: includeTarget(internStr("Python.h").any())},
		{kind: includeQuoted, target: includeTarget(internStr("archive.h").any())},
		{kind: includeSystem, target: includeTarget(internStr("jni.h").any())},
		{kind: includeSystem, target: includeTarget(internStr("Python.h").any())},
		{kind: includeQuoted, target: includeTarget(internStr("archive_entry.h").any())},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectSwigInducedIncludes mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
