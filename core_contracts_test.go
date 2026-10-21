package main

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func testEqual[T any](t *testing.T, got, want T) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCityHashUpstreamVectors(t *testing.T) {
	// Generated with util/digest/city.cpp at upstream d02375f8. The input
	// contains binary bytes and crosses every CityHash64 length boundary.
	cases := []struct {
		n int
		h uint64
	}{
		{0, 0x9ae16a3b2f90404f}, {1, 0x043648e8f1e3d715},
		{3, 0xa2c53c985be56a2f}, {4, 0x4157856f6d3ce3b2},
		{8, 0x897131b7f17d0588}, {9, 0x281e9dea0fbe631e},
		{16, 0x3292ee8642e63927}, {17, 0x3040ae7626e452e2},
		{32, 0xe1eae5f14516be5b}, {33, 0xeef23c18cef14a71},
		{64, 0x40f9113eba41913b}, {65, 0xcfd9a46175f104cf},
		{127, 0x0d9c850067303140}, {128, 0xe89d932b069b7c56},
		{129, 0xad8856753e699ea1}, {256, 0x97b100160e908046},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.n), func(t *testing.T) {
			input := make([]byte, tc.n)

			for i := range input {
				input[i] = byte(i*37 + 11)
			}

			testEqual(t, cityHash64(input), tc.h)
		})
	}
}

func TestBucketCollection(t *testing.T) {
	na := newNodeArenas()
	a, b, gen := source("a.h"), source("b.h"), build("generated.h")
	chunks := [][]VFS{nil, {a, gen}, {}, {b, a}}
	testEqual(t, collectBucketVFS(na, chunks, func(v VFS) bool { return v.isBuild() }), []VFS{gen})
	testEqual(t, collectBucketVFS(na, chunks, func(v VFS) bool { return v.isSource() }), []VFS{a, b, a})
	testEqual(t, chunks[1], []VFS{a, gen})
}

func TestJavaRawExpansion(t *testing.T) {
	d := JavaData{rawVars: map[string]string{"A": "${B}", "B": "value", "CYCLE": "${CYCLE}"}}

	for _, tc := range []struct{ input, want string }{
		{"${A}/${B}", "value/value"},
		{"${MISSING}/suffix", "/suffix"},
		{"${KOTLIN_VERSION}", "2.3.10"},
		{"${ARCADIA_ROOT}/src", "${ARCADIA_ROOT}/src"},
		{"prefix${unfinished", "prefix${unfinished"},
		{"${CYCLE}", "${CYCLE}"},
	} {
		testEqual(t, d.rawExpand(tc.input), tc.want)
	}

	testEqual(t, javaDropUnresolved([]string{"${MISSING}", "pre${A}post${B}", "${broken", "${$}HOME"}), []string{"prepost", "${broken", "$HOME"})
}

func TestJavaSourceSpecAndScripts(t *testing.T) {
	spec := parseJavaSrcs([]string{"RESOURCES", "SKIP_CHECK_SRCDIR", "SRCDIR", "src", "PACKAGE_PREFIX", "a.b", "*.java", "EXCLUDE", "Skip.java", "FILES", "One.java", "SRCDIR"})

	testEqual(t, spec.srcDir, "src")
	testEqual(t, spec.packagePrefix, "a.b")
	testEqual(t, spec.resources && spec.skipCheckSrcdir, true)
	testEqual(t, spec.globs, []string{"*.java"})
	testEqual(t, spec.excludes, []string{"Skip.java"})
	testEqual(t, spec.files, []string{"One.java"})
	testEqual(t, javaSrcsSalt(spec), "src *.java Skip.java a.b yes One.java")
	testEqual(t, parseJavaGenerateScript([]string{"OUT", "run.sh"}), []javaGenScript{{out: "run.sh", template: "template.tmpl"}})

	jm := &JavaModule{dir: "app", path: source("app")}

	testEqual(t, javaArgVFS(jm, "$(B)/app/../generated"), build("generated"))
	testEqual(t, javaArgVFS(jm, "$(S)/other/file"), source("other/file"))
	testEqual(t, javaOutDirVFS(jm, "$(B)/generated"), build("generated"))
}

func TestJavaDefaultAndConditionalOptions(t *testing.T) {
	opts := javaCondVar{base: []string{"base"}}

	opts.appendStmt("explicit")
	testEqual(t, opts.cur, []string{"base", "explicit"})
	opts.recalc([]string{"conditional"})
	testEqual(t, opts.cur, []string{"base", "explicit", "conditional"})
	opts.recalc(nil)
	testEqual(t, opts.cur, []string{"base", "explicit"})
}

func TestJavaMacroScope(t *testing.T) {
	for _, kind := range []javaUnitKind{juJar, juRunnable, juTest} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			jm := &JavaModule{unit: &javaUnit{kind: kind}, path: source("app")}
			d := &JavaData{}
			env := newEnvironment()

			for _, name := range []string{"JAVA_API_PEERDIR", "JAVA_IMPL_PEERDIR", "JAVA_TEST_PEERDIR", "JAVA_RUNTIME_PEERDIR"} {
				applyJavaMacro(nil, jm, d, name, []string{name}, nil, env)
			}

			want := []JavaPeerSpec{{dir: "JAVA_API_PEERDIR"}, {dir: "JAVA_IMPL_PEERDIR"}}

			if kind == juTest {
				want = append(want, JavaPeerSpec{dir: "JAVA_TEST_PEERDIR"})
			}

			if kind == juRunnable {
				want = append(want, JavaPeerSpec{dir: "JAVA_RUNTIME_PEERDIR"})
			}

			testEqual(t, d.peerStmts, want)
			testEqual(t, d.implPeers, []string{"JAVA_IMPL_PEERDIR"})

			for _, name := range []string{"JAVA_TEST_EXCLUDE", "JAVA_RUNTIME_EXCLUDE"} {
				applyJavaMacro(nil, jm, d, name, []string{name}, nil, env)
			}

			testEqual(t, slices.Contains(d.excludes, "JAVA_TEST_EXCLUDE"), kind == juTest)
			testEqual(t, slices.Contains(d.excludes, "JAVA_RUNTIME_EXCLUDE"), kind == juRunnable)
			testEqual(t, javaMacroIsAlias(jm, "EXCLUDE"), true)
			applyJavaMacro(nil, jm, d, "JAVA_DEPENDENCIES_CONFIGURATION", []string{"REQUIRE_DM"}, nil, env)
			testEqual(t, len(d.depsConfig) == 1, kind == juJar)
		})
	}
}

func TestJavaAppendVariables(t *testing.T) {
	d := &JavaData{}
	env := newEnvironment()
	jm := &JavaModule{unit: javaUnitJar}

	for _, args := range [][]string{nil, {"_PROTOC_FLAGS", "--first"}, {"_PROTOC_FLAGS", "--second"}} {
		applyJavaMacro(nil, jm, d, "SET_APPEND", args, args, env)
	}

	value, ok := env.lookup("_PROTOC_FLAGS")

	testEqual(t, ok, true)
	testEqual(t, value, "--first --second")
	testEqual(t, d.rawVars["_PROTOC_FLAGS"], value)
	testEqual(t, d.protocFlags, []string{"--first", "--second"})
}

func TestJavaProtoPlugins(t *testing.T) {
	d := &JavaData{javaProtobufPeers: []string{javaKotlinProtoPeersMarker}}
	jm := &JavaModule{unit: &javaUnit{kind: juExternal, proto: true}}
	env := newEnvironment()

	javaProtoPluginMacro(d, []string{"incomplete"})
	testEqual(t, len(d.javaProtoArgs), 0)
	applyJavaMacro(nil, jm, d, "WITH_KOTLIN_GRPC", nil, nil, env)
	applyJavaMacro(nil, jm, d, "JAVA_PROTO_PLUGIN", []string{"custom", "tools/plugin", "DEPS", "runtime"}, nil, env)
	applyJavaMacro(nil, jm, d, "EXPORT_YMAPS_PROTO", nil, nil, env)
	applyJavaMacro(nil, jm, d, "PROTOC_FATAL_WARNINGS", nil, nil, env)
	applyJavaMacro(nil, jm, d, "USE_COMMON_GOOGLE_APIS", nil, nil, env)

	testEqual(t, d.javaProtoArgs, []JavaProtoPlugin{{name: "custom", tool: "tools/plugin"}})
	testEqual(t, d.kotlinProto && d.kotlinGrpc && env.bool(envKotlinProto), true)
	testEqual(t, slices.Contains(d.javaProtobufPeers, "runtime"), true)
	testEqual(t, slices.Contains(d.javaProtobufPeers, javaKotlinProtoPeersMarker), false)
	testEqual(t, d.protoNamespace, "maps/doc/proto")
	testEqual(t, d.protocFlags, []string{"--fatal_warnings"})
	testEqual(t, d.commonGoogleAPIs, true)
	jm.data = d
	testEqual(t, slices.Contains(jm.varPeers(), "contrib/java/com/google/api/grpc/proto-google-common-protos/2.63.2"), true)
	testEqual(t, slices.Contains(jm.varPeers(), "contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/2.3.10"), true)
}
