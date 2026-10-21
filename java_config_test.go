package main

import (
	"slices"
	"strings"
	"testing"
)

func TestJavaModuleArguments(t *testing.T) {
	jm := &JavaModule{stmt: tokJtestFor}
	d := &JavaData{}

	javaModuleArgs(jm, d, nil)
	testEqual(t, d.unittestDir, "")
	javaModuleArgs(jm, d, internAnys([]string{"library/../target"}))
	testEqual(t, d.unittestDir, "target")
	jm.stmt = tokJavaLibrary
	javaModuleArgs(jm, d, internAnys([]string{"PREFIX", "org.example", "app", "ignored", "PREFIX"}))
	testEqual(t, d.prefix, "org.example")
	testEqual(t, d.name, "app")
}

func TestJavaDefaultLayouts(t *testing.T) {
	for _, name := range []string{"DEFAULT_JAVA_SRCS_LAYOUT", "DEFAULT_JUNIT_JAVA_SRCS_LAYOUT"} {
		for _, kotlin := range []bool{false, true} {
			d := &JavaData{withKotlin: kotlin}
			jm := &JavaModule{unit: javaUnitJar}

			applyJavaMacro(nil, jm, d, name, nil, nil, newEnvironment())
			testEqual(t, len(d.javaSrcs), 2)
			testEqual(t, d.javaSrcs[1].resources, true)
			testEqual(t, d.javaSrcs[0].skipCheckSrcdir, true)
			testEqual(t, slices.Contains(d.javaSrcs[0].globs, "**/*.kt"), kotlin)

			if name == "DEFAULT_JUNIT_JAVA_SRCS_LAYOUT" {
				testEqual(t, d.javaSrcs[0].srcDir, "java")
				testEqual(t, d.javaSrcs[1].srcDir, "resources")
			} else {
				testEqual(t, d.javaSrcs[0].srcDir, "src/main/java")
				testEqual(t, d.javaSrcs[1].srcDir, "src/main/resources")
			}
		}
	}

	d := &JavaData{}
	jm := &JavaModule{unit: &javaUnit{kind: juExternal, proto: true}}

	applyJavaMacro(nil, jm, d, "DEFAULT_JAVA_SRCS_LAYOUT", nil, nil, newEnvironment())
	testEqual(t, d.javaSrcsCalled, true)
	testEqual(t, len(d.javaSrcs), 0)
}

func TestJavaTestAndAnnotationSettings(t *testing.T) {
	jm := &JavaModule{unit: &javaUnit{kind: juTest}, path: source("app")}
	d := &JavaData{}
	env := newEnvironment()

	for _, call := range []struct {
		name string
		args []string
	}{
		{"JUNIT_TESTS_JAR", []string{"tests"}},
		{"TIMEOUT", []string{"120"}},
		{"TAG", []string{"ya:manual", "ya:external"}},
		{"FORK_SUBTESTS", nil},
		{"TEST_JAVA_CLASSPATH_CMD_TYPE", []string{"MANIFEST"}},
		{"DEPENDS", []string{"tools/helper"}},
		{"DATA_FILES", []string{"fixtures/a", "fixtures/b"}},
		{"NO_LINT", []string{"ktlint"}},
		{"USE_KTLINT_OLD", nil},
		{"NO_YMAKE_PYTHON3", nil},
		{"DISABLE_DATA_VALIDATION", nil},
		{"ENABLE", []string{"KOTLIN_PROTO", "SOURCES_JAR", "DISABLE_SCRIPTGEN"}},
		{"DISABLE", []string{"KOTLIN_PROTO", "SOURCES_JAR", "DISABLE_SCRIPTGEN"}},
	} {
		applyJavaMacro(nil, jm, d, call.name, call.args, call.args, env)
	}

	testEqual(t, d.unittestDir, "tests")
	testEqual(t, d.peerStmts, []JavaPeerSpec{{dir: "tests"}})
	testEqual(t, d.test.timeout, "120")
	testEqual(t, d.test.tags, []string{"ya:manual", "ya:external"})
	testEqual(t, d.test.forkMode, "subtests")
	testEqual(t, d.test.classpathCmdType, "MANIFEST")
	testEqual(t, d.test.depends, []string{"tools/helper"})
	testEqual(t, d.test.data, []string{"arcadia/fixtures/a", "arcadia/fixtures/b"})
	testEqual(t, d.noLint, "ktlint")
	testEqual(t, d.ktlintOld && d.noYmakePython3, true)
	testEqual(t, env.bool(envValidateData), false)
	testEqual(t, d.kotlinProto || d.sourcesJar || d.disableScriptgen, false)

	applyJavaMacro(nil, jm, d, "AUTO_SERVICE", []string{"1.2"}, nil, env)
	testEqual(t, d.useAnnotationProc, "contrib/java/com/google/auto/service/auto-service/1.2")
	testEqual(t, d.peerStmts[1].dir, "contrib/java/com/google/auto/service/auto-service-annotations/1.2")
	applyJavaMacro(nil, jm, d, "USE_ANNOTATION_PROCESSOR", []string{"processor"}, nil, env)
	testEqual(t, d.useAnnotationProc, "processor")
}

func TestJavaDependencyRulesAndClosure(t *testing.T) {
	a, b, c := &JavaModule{dir: "a"}, &JavaModule{dir: "b"}, &JavaModule{dir: "c"}
	b.closure = newJavaPeersClosure()
	b.closure.merge(c, emptyJavaPeersClosure, 1)
	a.closure = newJavaPeersClosure()
	a.closure.merge(b, b.closure, 1)
	closure := newJavaPeersClosure()

	closure.merge(a, a.closure, 1)
	closure.merge(c, emptyJavaPeersClosure, 1)

	// Removing a chain must retain a leaf which also has a direct path.
	filtered := closure.exclude(func(m *JavaModule) bool { return m == a || m == b }, javaClosureOf, nil)

	testEqual(t, filtered.contains(a), false)
	testEqual(t, filtered.contains(b), false)
	testEqual(t, filtered.contains(c), true)
	testEqual(t, closure.contains(a), true)

	// Transparent modules stop exclusion propagation at their boundary.
	filtered = closure.exclude(func(m *JavaModule) bool { return m == a }, javaClosureOf, func(*JavaModule) bool { return true })
	testEqual(t, filtered.contains(b), true)
	testEqual(t, javaClosureOf(&JavaModule{}), emptyJavaPeersClosure)

	rules := &javaDMRules{lib2Ver: map[string]string{"lib": "lib/2"}}

	rules.addDependencyManagement("lib")
	testEqual(t, rules.lib2Ver, map[string]string{"lib": "lib/2"})
}

func TestJavaCompileClosureHidesTransitiveImplementation(t *testing.T) {
	unit := &javaUnit{}
	a, api, impl := &JavaModule{unit: unit}, &JavaModule{unit: unit}, &JavaModule{unit: unit}
	a.direct = []javaResolvedPeer{{m: api}, {m: impl, impl: true}}
	direct := []javaResolvedPeer{{m: a}}
	resolutions := map[*JavaModule]javaResolutionInfo{}

	for _, m := range []*JavaModule{a, api, impl} {
		resolutions[m] = javaResolutionInfo{resolution: javaResolvedPeer{m: m}}
	}

	testEqual(t, javaPreorderSort(direct, resolutions, true), []*JavaModule{a, api})
	testEqual(t, javaPreorderSort(direct, resolutions, false), []*JavaModule{a, api, impl})
	testEqual(t, a.isDirectPeer(impl), true)
	testEqual(t, a.isDirectPeer(a), false)
}

func TestJavaStatementDefaultsAndSourceFlags(t *testing.T) {
	env := newEnvironment()
	jm := &JavaModule{unit: javaUnitJar}
	d := &JavaData{}
	name := internEnv("CUSTOM")
	collectJavaStmts(nil, jm, d, []Stmt{
		&DefaultVarStmt{VarName: "CUSTOM", NameEnv: name, Value: "first"},
		&DefaultVarStmt{VarName: "CUSTOM", NameEnv: name, Value: "second"},
		&PeerdirStmt{Paths: internAnys([]string{"ADDINCL", "GLOBAL", "dep"})},
		&SrcsStmt{Sources: internAnys([]string{"GLOBAL", "src.java"})},
		&AddInclStmt{ProtoGlobalPaths: internAnys([]string{"includes"})},
	}, env)
	testEqual(t, d.rawVars["CUSTOM"], "first")
	testEqual(t, env.string(name), "first")
	testEqual(t, d.peerStmts, []JavaPeerSpec{{dir: "dep", explicit: true}})
	testEqual(t, d.srcs, []string{"src.java"})
	testEqual(t, d.protoAddIncl, []javaAddIncl{{dir: source("includes"), global: true}})
	jm.unit = &javaUnit{ignorePeerdir: true}
	collectJavaStmts(nil, jm, d, []Stmt{&PeerdirStmt{Paths: internAnys([]string{"ignored"})}}, env)
	testEqual(t, len(d.peerStmts), 1)
}

func TestJavaShortNamesAndMissingVariants(t *testing.T) {
	single := strings.Repeat("a", 60)
	testEqual(t, shortenFullPath(single), single)
	testEqual(t, shortenFullPath("prefix/"+single), single)
	testEqual(t, javaUnitFor(tokJavaLibrary, ModuleKind(255)), (*javaUnit)(nil))
	jm := &JavaModule{dir: "app", data: &JavaData{javaSrcs: []JavaSrcsSpec{{srcDir: "$(B)/gen"}, {srcDir: "."}}}}
	testEqual(t, jm.srcDirsTestData(), []string{"$(S)/app"})
}
