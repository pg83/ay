package main

import (
	"reflect"
	"testing"
)

func TestSwigParser_ImplicitIncludesOnRootSwg(t *testing.T) {
	// A root .swg carries the implicit language runtimes as system directives.
	set := SwigIncludeDirectiveParser{}.parse("mod/src.swg", []byte("%include \"local.i\"\n"), newBumpAllocator[IncludeDirective](directiveBlockHint))
	local := set.bucket(parsedIncludesLocal)

	want := []string{"swig.swg", "go.swg", "java.swg", "perl5.swg", "python.swg", "local.i"}
	got := make([]string, 0, len(local))

	for _, d := range local {
		got = append(got, d.target.string())
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("root .swg directives = %v, want %v", got, want)
	}

	// Library files get no implicit prefix.
	libSet := SwigIncludeDirectiveParser{}.parse("contrib/tools/swig/Lib/python/python.swg", []byte("%include \"pyrun.swg\"\n"), newBumpAllocator[IncludeDirective](directiveBlockHint))

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

	ctx := &GenCtx{fs: fs, parsers: newIncludeParserManagerFS(fs, newSharedParseCache())}
	// collectSwigInducedIncludes runs after the walk has parsed the closure.
	closure := []VFS{intern("$(S)/mod/local.i"), intern("$(S)/mod/nested.i")}
	got := collectSwigInducedIncludes(ctx, intern("$(S)/mod/src.swg"), closure)
	// Raw union, root first; cross-file repeats stay.
	want := []IncludeDirective{
		{kind: includeSystem, target: internStr("Python.h")},
		{kind: includeQuoted, target: internStr("archive.h")},
		{kind: includeSystem, target: internStr("jni.h")},
		{kind: includeSystem, target: internStr("Python.h")},
		{kind: includeQuoted, target: internStr("archive_entry.h")},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectSwigInducedIncludes mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
