package main

import (
	"slices"
	"testing"
)

func TestIncompleteSourceDirectives(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    IncludeDirectiveParser
		body string
	}{
		{"nlg-comment", NlgIncludeDirectiveParser{}, "# no newline"},
		{"nlg-brace", NlgIncludeDirectiveParser{}, "{literal text}"},
		{"nlg-tag", NlgIncludeDirectiveParser{}, "{% import 'missing.nlg'"},
		{"ts-line-comment", TSIncludeDirectiveParser{}, "// import './ignored'"},
		{"ts-block-comment", TSIncludeDirectiveParser{}, "/* import './ignored'"},
		{"ts-string", TSIncludeDirectiveParser{}, "'escaped\\' import \\\"./ignored\\\"'"},
		{"fortran-empty", FortranIncludeDirectiveParser{}, "include ''\ninclude 'unfinished\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.p.parse("input", [][]byte{[]byte(tc.body)}, newBumpAllocator[IncludeDirective]())
			for _, bucket := range got {
				testEqual(t, len(bucket), 0)
			}
		})
	}
	got := (RosIncludeDirectiveParser{}).parse("msg", [][]byte{[]byte("Header header\n")}, newBumpAllocator[IncludeDirective]())
	testEqual(t, got[parsedIncludesLocal][0].target.string(), "std_msgs/Header")
	pm := newIncludeParserManagerFS(ContractFS{}, newSharedParseCache())
	testEqual(t, pm.resolveTSImport("app", "./missing.js"), VFS(0))
}

func TestConfigurationSyntaxErrors(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"}", "unmatched }"}, {"when ($X) {", "unfinished block"}, {"X = value\\", "unfinished block"},
		{"@import other.conf", "unsupported @import"},
	} {
		testThrows(t, tc.want, func() { readConfFile("test.conf", tc.body) })
	}
	testThrows(t, "unsupported condition", func() { parseConfCondition("test.conf", `(A == "x" A)`) })
	testEqual(t, expandConfScalar("${UNBOUND}", newEnvironment()), "")
}

func TestSourceGlobBoundaries(t *testing.T) {
	fs := ContractFS{files: map[string]string{"app/src/A.java": "", "app/src/AB.java": "", "app/file.java": "", "app/sub/one.json": "", "app/one.json": ""}}
	ctx := &GenCtx{fs: fs}
	testEqual(t, ctx.javaGlob("app", "/src//?.java", newJavaExcludeMatcher("app", nil)), []string{"app/src/A.java"})
	testEqual(t, ctx.javaGlob("app", "*/*.java", newJavaExcludeMatcher("app", nil)), []string{"app/src/A.java", "app/src/AB.java"})
	testEqual(t, ctx.javaGlob("app", "$UNRESOLVED/*.java", newJavaExcludeMatcher("app", nil)), []string(nil))
	ex := newJavaExcludeMatcher("app", []string{"$(S)/app//src/?*.java"})
	testEqual(t, ex.excluded("app/src/A.java", false), true)
	testEqual(t, ex.excluded("app/sub/A.java", false), false)
	testEqual(t, globExcludeRegex("/app//s?b/*.json").MatchString("app/sub/one.json"), true)
	testEqual(t, ymakeGlob(fs, "app/s?b/*.json"), []string{"app/sub/one.json"})
	testEqual(t, notsGlob("app/**").MatchString("app/sub/one.json"), true)
	testEqual(t, notsGlob("app/*").MatchString("app/sub/one.json"), false)
}

func TestNotsVersionSelectionAndCaches(t *testing.T) {
	fs := ContractFS{files: map[string]string{
		notsERMPath:   `{"nodejs":{"versions":{"22.0.0":{},"20.0.0":{},"24.0.0":{}}}}`,
		"config.yaml": "answer: 42\n",
	}}
	pm := newIncludeParserManagerFS(fs, newSharedParseCache())
	testEqual(t, pm.notsVersion("nodejs", ">=21.0.0"), "22.0.0")
	testEqual(t, pm.notsVersion("nodejs", "=24.0.0"), "24.0.0")
	testThrows(t, "no nodejs version satisfies", func() { pm.notsVersion("nodejs", "=23.0.0") })
	first := pm.notsYAML("config.yaml")
	delete(fs.files, "config.yaml")
	testEqual(t, pm.notsYAML("config.yaml"), first)
	testEqual(t, len(first), 1)
}

func TestUsrvFileSelection(t *testing.T) {
	d := &ModuleData{}
	testThrows(t, "missing FROM", func() { d.collectUsrvFiles([]string{"FROM"}) })
	testThrows(t, "FROM is required", func() { d.collectUsrvFiles(nil) })
	d.collectUsrvFiles([]string{"FROM", "generated.tar", "a.cpp", "OUT_NOAUTO", "manual.cpp"})
	testEqual(t, d.usrvFiles, []UsrvFile{{Tar: "generated.tar", File: "a.cpp", DepsFile: "NO_DEPS", Auto: true}, {Tar: "generated.tar", File: "manual.cpp", DepsFile: "NO_DEPS"}})
	testThrows(t, "missing SUFFIX", func() { usrvAllSources(nil, "app", []string{"SUFFIX"}) })
	fs := ContractFS{files: map[string]string{"app/ya.make": "", "app/a.cpp": "", "app/notes.txt": "", "app/nested/ya.make": ""}}
	testEqual(t, usrvAllSources(fs, "app", nil), []string{"a.cpp"})
	testThrows(t, "nested module", func() { usrvAllSources(fs, "app", []string{"RECURSIVE"}) })
}

func TestGoDefaultCommonApis(t *testing.T) {
	d := &ModuleData{}
	applyGoCommonGoogleApis(nil, d)
	peers := anyStrs(d.goCommonGoogleApis)
	for _, want := range []string{
		"vendor/google.golang.org/genproto/googleapis/api/annotations",
		"vendor/cloud.google.com/go/longrunning/autogen/longrunningpb",
		"vendor/google.golang.org/genproto/googleapis/rpc/status",
		"vendor/google.golang.org/genproto/googleapis/type/timeofday",
	} {
		testEqual(t, slices.Contains(peers, want), true)
	}
	testEqual(t, len(d.goCommonGoogleApis), 41)
	testEqual(t, d.protoAddInclGlobal, []VFS{contribLibsGoogleapisCommonProtos})
	applyGoCommonGoogleApis(nil, d)
	testEqual(t, len(d.protoAddInclGlobal), 1)
}
