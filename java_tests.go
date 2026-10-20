package main

import (
	"path"
	"slices"
	"strings"
)

var javaDetekt = &javaLinter{name: "detekt", wrapper: "tools/detekt_linter/wrapper.py", resourcePeer: "build/platform/java/detekt", resource: "DETEKT_RESOURCE_GLOBAL"}

var javaStyleConfigs = map[string]string{
	"base":     "/yandex_checks.xml",
	"strict":   "/yandex_checks_strict.xml",
	"extended": "/yandex_checks_extended.xml",
	"library":  "/yandex_checks_library.xml",
}

const (
	javaStyleMigrations = "build/rules/jstyle/migrations.yaml"
	javaStyleRunnerLib  = "$(JSTYLE_LIB)"
	javaStyleServerEnv  = "YA_CORE_TMP_PATH=$(RESOURCE_ROOT)/tests/jstyle_server"
)

type javaLinter struct {
	name         string
	wrapper      string
	resourcePeer string
	resource     string
}

type javaCustomLint struct {
	linter      *javaLinter
	configTypes []string
}

func (ctx *GenCtx) javaEmitTests(jm *JavaModule) []NodeRef {
	var suites []*yaTestSuite

	if jm.unit.kind == juJar {
		suites = append(suites, ctx.javaKtlintSuite(jm), ctx.javaStyleSuite(jm))
	}

	for _, l := range jm.data.customLints {
		suites = append(suites, ctx.javaCustomLintSuite(jm, l))
	}

	var refs []NodeRef

	for _, s := range suites {
		if s != nil {
			refs = append(refs, ctx.emitYaTestSuite(s)...)
		}
	}

	return refs
}

func (jm *JavaModule) srcDirsTestData() []string {
	var out []string

	for _, spec := range jm.data.javaSrcs {
		dir := spec.srcDir

		switch {
		case dir == ".":
			dir = "$(S)/" + jm.dir
		case strings.HasPrefix(dir, "$(B)/"):
			continue
		case !strings.HasPrefix(dir, "$(S)/"):
			dir = "$(S)/" + path.Clean(jm.dir+"/"+dir)
		}

		out = append(out, dir)
	}

	return out
}

func sortedUnique(in []string) []string {
	out := slices.Clone(in)

	slices.Sort(out)

	return slices.Compact(out)
}

func (ctx *GenCtx) javaSuiteBase(jm *JavaModule, name, testType, ciType string) *yaTestSuite {
	return &yaTestSuite{
		p:           jm.instance.Platform,
		projectPath: jm.dir,
		name:        name,
		testType:    testType,
		ciType:      ciType,
		size:        "small",
		timeout:     60,
		moduleLang:  "java",
		globals:     ctx.javaResourceGlobals(jm),
		runInputs:   []VFS{source(jm.dir)},
	}
}

func (ctx *GenCtx) javaProducer(jm *JavaModule, out VFS) []NodeRef {
	if ref := ctx.javaBuilder(jm).use(out); ref != 0 {
		return []NodeRef{ref}
	}

	return nil
}

func (ctx *GenCtx) javaStyleSuite(jm *JavaModule) *yaTestSuite {
	d := jm.data
	dirs := jm.srcDirsTestData()

	if d.lintLevel == "none" || d.noLint == "none" || len(d.javaSrcs) == 0 {
		return nil
	}

	srclst := build(jm.dir, "/lint-java.srclst")
	s := ctx.javaSuiteBase(jm, "java_style", "java.style", "style")

	s.timeout = 240
	s.requirements = map[string]string{"ram": "16"}
	s.supportsTestParameters = true
	s.relatedPaths = append([]string{"$(S)/" + javaStyleMigrations}, sortedUnique(dirs)...)
	s.runInputs = append(s.runInputs, srclst)
	s.deps = ctx.javaProducer(jm, srclst)
	s.env = append(slices.Clone(d.testEnv), javaStyleServerEnv)

	if len(d.testEnv) != 0 {
		s.env = append(s.env, javaStyleServerEnv)
	}

	globals := ctx.javaGlobals(jm)
	jdk := "JDK" + javaConfGlobals["JDK_LATEST_VERSION"]

	s.runCmd = func(work string, _, _ int) []ANY {
		strs := []string{yaTestTool, "run_javastyle", "--source-root", "$(S)", "--trace-path", work + "/ytest.report.trace",
			"--out-path", work + "/testing_out_stuff", "--java", globals[jdk].string() + "/bin/java",
			"--runner-lib-path", javaStyleRunnerLib, "--config-xml", javaStyleConfigs[d.lintLevel], "--verbose",
			"--modulo", "1", "--modulo-index", "0"}

		if ctx.tests.jstyleServer {
			strs = append(strs, "--use-jstyle-server")
		}

		strs = append(strs, "--jstyle-migrations", "$(S)/"+javaStyleMigrations, srclst.string()+"::$(S)")

		return yaTestArgs(strs)
	}

	return s
}

func (ctx *GenCtx) javaKtlintSuite(jm *JavaModule) *yaTestSuite {
	d := jm.data

	if !d.withKotlin || d.noLint != "" {
		return nil
	}

	folder := "ktlint"

	if d.ktlintOld {
		folder = "ktlint_old"
	}

	editorconfig := "$(S)/build/platform/java/" + folder + "/.editorconfig"
	binary := "$(" + strings.ToUpper(folder) + ")/run.bat"
	srclist := build(jm.dir, "/all-kt-sources.txt")
	s := ctx.javaSuiteBase(jm, "ktlint", "ktlint", "style")

	s.timeout = 120
	s.requirements = map[string]string{"cpu": "2"}
	s.relatedPaths = append([]string{"$(S)/" + jm.dir, "$(S)/.editorconfig"}, sortedUnique(append([]string{editorconfig}, jm.srcDirsTestData()...))...)
	s.runInputs = append(s.runInputs, srclist)
	s.deps = ctx.javaProducer(jm, srclist)
	s.env = slices.Clone(d.testEnv)

	s.runCmd = func(work string, _, _ int) []ANY {
		return yaTestArgs([]string{yaTestTool, "run_ktlint_test", "--srclist-path", srclist.string(), "--binary", binary,
			"--trace-path", work + "/ytest.report.trace", "--source-root", "$(S)", "--build-root", "$(B)",
			"--project-path", jm.dir, "--output-dir", work + "/testing_out_stuff", "--editorconfig", editorconfig})
	}

	return s
}

func javaMacroKeyword(args []string, keyword string, others ...string) []string {
	var out []string

	in := false

	for _, a := range args {
		switch {
		case a == keyword:
			in = true
		case slices.Contains(others, a):
			in = false
		case in:
			out = append(out, a)
		}
	}

	return out
}

func (ctx *GenCtx) javaKotlinLintFiles(jm *JavaModule) []string {
	args := jm.data.collectKotlinLint

	if args == nil {
		return nil
	}

	var patterns []string

	dirs := javaMacroKeyword(args, "DIRS", "DIRS_RECURSE")

	if len(dirs) == 0 {
		patterns = append(patterns, "*.kt")
	}

	for _, d := range dirs {
		patterns = append(patterns, "$(S)/"+path.Join(jm.dir, d)+"/*.kt")
	}

	for _, d := range javaMacroKeyword(args, "DIRS_RECURSE", "DIRS") {
		patterns = append(patterns, "$(S)/"+path.Join(jm.dir, d)+"/**/*.kt")
	}

	ex := newJavaExcludeMatcher(jm.dir, []string{"ya.make", "canondata/**"})

	var files []string

	for _, p := range patterns {
		for _, f := range ctx.javaGlob(jm.dir, p, ex) {
			files = append(files, "$(S)/"+f)
		}
	}

	return sortedUnique(files)
}

func (ctx *GenCtx) javaCustomLintSuite(jm *JavaModule, l javaCustomLint) *yaTestSuite {
	files := ctx.javaKotlinLintFiles(jm)

	if len(files) == 0 {
		return nil
	}

	inc, _ := ctx.autoincludeIdx.lintersMakeIncFor(jm.dir)
	configs := make([]string, 0, len(l.configTypes))

	for _, c := range l.configTypes {
		configs = append(configs, "$(S)/"+path.Join(path.Dir(inc.relString()), c))
	}

	s := ctx.javaSuiteBase(jm, l.linter.name, l.linter.name, "style")

	s.globals = slices.DeleteFunc(s.globals, func(g ResourceDecl) bool { return g.GlobalVar.string() != l.linter.resource })
	s.supportsTestParameters = true
	s.relatedPaths = append(append(slices.Clone(files), configs...), "$(S)/"+l.linter.wrapper)
	s.env = slices.Clone(jm.data.testEnv)

	s.runCmd = func(work string, _, _ int) []ANY {
		strs := []string{yaTestTool, "run_custom_lint", "--source-root", "$(S)", "--build-root", "$(B)",
			"--project-path", "$(S)/" + jm.dir, "--trace-path", work + "/ytest.report.trace",
			"--out-path", work + "/testing_out_stuff", "--lint-name", l.linter.name, "--wrapper-script", l.linter.wrapper,
			"--depends", jm.dir}

		for _, c := range configs {
			strs = append(strs, "--config", c)
		}

		for _, g := range sortedResourceGlobals(s.globals) {
			strs = append(strs, "--global-resource", g.Token.string())
		}

		return yaTestArgs(append(strs, files...))
	}

	return s
}
