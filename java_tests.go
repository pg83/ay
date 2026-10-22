package main

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"path"
	"slices"
	"strconv"
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

func (ctx *GenCtx) javaTestSuites(jm *JavaModule) []*TestSuite {
	var suites []*TestSuite

	switch jm.unit.kind {
	case juJar:
		suites = append(suites, ctx.javaKtlintSuite(jm), ctx.javaStyleSuite(jm))
	case juTest:
		suites = append(suites, ctx.javaJunitSuite(jm))
	}

	if (jm.unit.kind == juJar || jm.unit.kind == juExternal) && resourceValidationEnabled(jm.data.env) {
		globals := ctx.javaResourceGlobals(jm)

		suites = append(suites, ctx.dataValidationSuite(jm.dir, jm.data.test.data, jm.data.testEnv, globals), ctx.sandboxValidationSuite(jm.dir, jm.data.fromSandboxes, jm.data.testEnv, globals))
	}

	if jm.checksClasspathClash() {
		suites = append(suites, ctx.javaClasspathClashSuite(jm))
	}

	for _, l := range jm.data.customLints {
		suites = append(suites, ctx.javaCustomLintSuite(jm, l))
	}

	active := suites[:0]

	for _, s := range suites {
		if s != nil {
			active = append(active, s)
		}
	}

	return active
}

func (ctx *GenCtx) javaCheckSourceDir(jm *JavaModule, dir string) VFS {
	if dir == "." {
		dir = jm.dir
	}

	if rooted, ok := moduleRootedVFS(jm.dir, dir); ok {
		return rooted
	}

	dirs := append(slices.Clone(jm.data.srcDirs), v, build(jm.dir), bld)

	for _, base := range dirs {
		rel := path.Join(base.relString(), dir)

		if base.isSource() {
			if present, _ := ctx.fs.exists(srcRootRel, rel); present {
				return source(rel)
			}
		} else if ctx.javaReg(jm).lookup(build(rel)) != nil {
			return build(rel)
		}
	}

	// java_srcdirs_to_data joins an unresolved empty path with MODDIR.
	return jm.path
}

func (ctx *GenCtx) javaSrcDirsTestData(jm *JavaModule) []string {
	var out []string

	for _, spec := range jm.data.javaSrcs {
		dir := ctx.javaCheckSourceDir(jm, spec.srcDir)

		if dir.isSource() {
			out = append(out, dir.string())
		}
	}

	return out
}

func sortedUnique(in []string) []string {
	out := slices.Clone(in)

	slices.Sort(out)

	return slices.Compact(out)
}

func (ctx *GenCtx) javaSuiteBase(jm *JavaModule, name, testType, ciType string) *TestSuite {
	return &TestSuite{
		p:           jm.instance.Platform,
		projectPath: jm.dir,
		name:        name,
		testType:    testType,
		ciType:      ciType,
		size:        "small",
		timeout:     60,
		globals:     ctx.javaResourceGlobals(jm),
		runInputs:   []VFS{source(jm.dir)},
	}
}

func (ctx *GenCtx) javaProducer(jm *JavaModule, out VFS) NodeRef {
	return ctx.javaReg(jm).use(out).ProducerRef
}

func (ctx *GenCtx) javaStyleSuite(jm *JavaModule) *TestSuite {
	d := jm.data
	dirs := ctx.javaSrcDirsTestData(jm)

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
	s.deps = []NodeRef{ctx.javaProducer(jm, srclst)}
	s.env = append(slices.Clone(d.testEnv), javaStyleServerEnv)

	if len(d.testEnv) != 0 {
		s.env = append(s.env, javaStyleServerEnv)
	}

	globals := ctx.javaGlobals(jm)
	jdk := "JDK" + javaConfGlobals["JDK_LATEST_VERSION"]

	s.runCmd = func(work string, _ TestChunk) []ANY {
		strs := []string{testToolHostPath, "run_javastyle", "--source-root", "$(S)", "--trace-path", work + "/ytest.report.trace",
			"--out-path", work + "/testing_out_stuff", "--java", globals[jdk].string() + "/bin/java",
			"--runner-lib-path", javaStyleRunnerLib, "--config-xml", javaStyleConfigs[d.lintLevel], "--verbose",
			"--modulo", "1", "--modulo-index", "0"}

		if ctx.tests.jstyleServer {
			strs = append(strs, "--use-jstyle-server")
		}

		strs = append(strs, "--jstyle-migrations", "$(S)/"+javaStyleMigrations, srclst.string()+"::$(S)")

		return testArgs(strs)
	}

	return s
}

func (ctx *GenCtx) javaKtlintSuite(jm *JavaModule) *TestSuite {
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
	dirs := []string{editorconfig}

	if d.env.string(internEnv("_WITH_YA_1931")) != "yes" {
		dirs = append(dirs, ctx.javaSrcDirsTestData(jm)...)
	}

	s.timeout = 120
	s.requirements = map[string]string{"cpu": "2"}
	s.relatedPaths = append([]string{"$(S)/" + jm.dir, "$(S)/.editorconfig"}, sortedUnique(dirs)...)
	s.runInputs = append(s.runInputs, srclist)
	s.projectBuildDeps = true
	s.deps = []NodeRef{ctx.javaProducer(jm, srclist)}
	s.env = slices.Clone(d.testEnv)

	s.runCmd = func(work string, _ TestChunk) []ANY {
		return testArgs([]string{testToolHostPath, "run_ktlint_test", "--srclist-path", srclist.string(), "--binary", binary,
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

func (ctx *GenCtx) javaCustomLintSuite(jm *JavaModule, l javaCustomLint) *TestSuite {
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

	s.runCmd = func(work string, _ TestChunk) []ANY {
		strs := []string{testToolHostPath, "run_custom_lint", "--source-root", "$(S)", "--build-root", "$(B)",
			"--project-path", "$(S)/" + jm.dir, "--trace-path", work + "/ytest.report.trace",
			"--out-path", work + "/testing_out_stuff", "--lint-name", l.linter.name, "--wrapper-script", l.linter.wrapper,
			"--depends", jm.dir}

		for _, c := range configs {
			strs = append(strs, "--config", c)
		}

		for _, g := range sortedResourceGlobals(s.globals) {
			strs = append(strs, "--global-resource", g.Token.string())
		}

		return testArgs(append(strs, files...))
	}

	return s
}

func (jm *JavaModule) junitTestJar() VFS {
	if jm.data.unittestDir == "" {
		return build(jm.dir, "/", jm.name, ".jar")
	}

	testJar := build(jm.data.unittestMod)

	for _, peer := range jm.managedPeers {
		if peer.dir == jm.data.unittestDir {
			testJar = peer.target
		}
	}

	return testJar
}

func (ctx *GenCtx) javaJunitSuite(jm *JavaModule) *TestSuite {
	d := jm.data
	t := d.test
	testJar := jm.junitTestJar()
	cpf := build(strings.TrimSuffix(testJar.relString(), path.Ext(testJar.relString())), ".test.cpf")
	closure := ctx.javaManagedPeersClosure(jm)
	globals := ctx.javaGlobals(jm)
	jdk := "JDK" + d.jdkReal
	jdkForTests, ok := globals[jdk+"_FOR_TESTS"]

	if !ok {
		jdkForTests = globals[jdk]
	}

	s := ctx.javaSuiteBase(jm, path.Base(path.Dir(jm.dir))+"-"+path.Base(jm.dir), "java", "test")

	s.size = cmp.Or(t.size, "small")
	s.timeout = testSizeTimeouts[s.size]

	if t.timeout != "" {
		s.timeout, _ = strconv.Atoi(t.timeout)
	}

	s.recipes = testRecipes(t.recipes)
	s.depends = t.depends
	s.data = sortedUnique(t.data)
	s.tags = sortedUnique(t.tags)
	s.requirements = testRequirementsOf(t.requirements)
	s.env = slices.Clone(d.testEnv)
	s.supportsCanonization = true
	s.canonicalData = true
	s.supportsTestParameters = true
	s.smoothShutdown = []string{"SIGUSR1"}
	s.split = 1

	if t.cwd != "" {
		s.runCwd = source(t.cwd).string()
	}

	if t.forkMode != "" {
		s.split = 10
	}

	if t.splitFactor != "" {
		s.split, _ = strconv.Atoi(t.splitFactor)
	}

	data := []string{"build/scripts/run_junit.py", "build/scripts/unpacking_jtest_runner.py", jm.dir}

	for _, e := range t.data {
		if rel, ok := strings.CutPrefix(strings.TrimRight(e, "/"), "arcadia/"); ok {
			data = append(data, path.Clean(rel))
		}
	}

	for _, rel := range sortedUnique(data) {
		s.relatedPaths = append(s.relatedPaths, "$(S)/"+rel)
	}

	var classpath []string
	var testDeps []JavaClosureEntry

	for _, e := range closure {
		if rel := e.path.relString(); strings.HasSuffix(rel, ".jar") {
			classpath = append(classpath, rel)
			testDeps = append(testDeps, e)
		}
	}

	var libpath []string

	for _, p := range ctx.javaUnmanageablePeersClosure(jm) {
		f := javaForeignFileName(p)

		switch path.Ext(f.relString()) {
		case ".so", ".dll", ".dylib":
			testDeps = append(testDeps, JavaClosureEntry{path: f, foreign: p.res})
		}

		if strings.HasSuffix(f.relString(), ".so") {
			libpath = append(libpath, "$(B)/"+path.Dir(f.relString()))
		}
	}

	deps := ctx.javaBuilder(jm)

	deps.closure(testDeps)

	s.projectBuildDeps = true
	s.deps = deps.deps

	py := internStr(testYMakePython3).any()

	s.prepare = [][]ANY{
		{py, buildScriptsMkdirPy.any(), internStr(path.Dir(cpf.string())).any()},
		append(append([]ANY{py, buildScriptsWriterPy.any(), strFile.any(), cpf.any(), strM.any(), strYaStartCommandFile.any()},
			testArgs(classpath)...), strYaEndCommandFile.any()),
	}

	for _, f := range []string{"mkdir.py", "run_junit.py", "writer.py", "process_command_files.py"} {
		s.prepareInputs = append(s.prepareInputs, source("build/scripts/", f))
	}

	jvm := []string{"-Dfile.encoding=utf8", "-Dya.make", "-DARCADIA_BUILD_ROOT=$(B)", "-DARCADIA_ROOT=$(S)",
		"-DBINDIR=$(B)/" + jm.dir, "-DCURDIR=$(S)/" + jm.dir, "-ea"}

	vars := strings.NewReplacer("${CURDIR}", "$(S)/"+jm.dir, "${ARCADIA_ROOT}", "$(S)", "${BINDIR}", "$(B)/"+jm.dir, "${ARCADIA_BUILD_ROOT}", "$(B)")

	for _, a := range d.jvmArgs {
		jvm = append(jvm, vars.Replace(a))
	}

	jvm = append(jvm, "-DJAVA="+jdkForTests.string()+"/bin/java")

	props := javaTestProperties(t.systemProperties)
	cpType := strings.ToLower(cmp.Or(t.classpathCmdType, "MANIFEST"))

	s.runCmd = func(work string, chunk TestChunk) []ANY {
		strs := []string{testYMakePython3, "$(S)/build/scripts/run_junit.py", testYMakePython3, "$(S)/build/scripts/unpacking_jtest_runner.py",
			"--classpath-option-type", cpType, "--trace-file", work + "/ytest.report.trace",
			"--jar-binary", jdkForTests.string() + "/bin/jar", "--tests-jar-path", testJar.string(), jdkForTests.string() + "/bin/java",
			"-Djava.io.tmpdir=" + work + "/tests_tmp_dir"}

		if len(libpath) != 0 {
			libs := strings.Join(dedupKeepOrder(libpath), ":")

			strs = append(strs, "-Djava.library.path="+libs, "-Djna.library.path="+libs)
		}

		strs = append(strs, jvm...)

		strs = append(strs, "-classpath", "@"+cpf.string(), "ru.yandex.devtools.test.Runner", "--tests-jar", testJar.string(),
			"--source-root", "$(S)", "--modulo", strconv.Itoa(chunk.split), "--modulo-index", strconv.Itoa(chunk.index))

		if t.experimentalFork {
			strs = append(strs, "--experimental-fork")
		}

		strs = append(strs, "--build-root", "$(B)", "--sandbox-resources-root", work, "--test-outputs-root", work+"/testing_out_stuff",
			"--output", work+"/ytest.report.trace")

		if t.forkMode == "subtests" {
			strs = append(strs, "--fork-subtests")
		}

		strs = append(strs, "--runner-log-path", work+"/testing_out_stuff/run.log", "--properties", props)

		return testArgs(strs)
	}

	return s
}

func javaTestProperties(args []string) string {
	props := []map[string]string{}

	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == "FILE" {
			props = append(props, map[string]string{"type": "file", "path": "${ARCADIA_ROOT}/" + args[i+1]})
		} else {
			props = append(props, map[string]string{"type": "inline", "key": args[i], "value": args[i+1]})
		}
	}

	return base64.StdEncoding.EncodeToString(throw2(json.Marshal(props)))
}

func (ctx *GenCtx) javaUnmanageablePeersClosure(jm *JavaModule) []javaForeignPeer {
	if jm.unmanageableDone {
		return jm.unmanageableDart
	}

	jm.unmanageableDone = true

	var out []javaForeignPeer

	seen := map[*ModuleEmitResult]bool{}

	add := func(peers []javaForeignPeer) {
		for _, p := range peers {
			if !seen[p.res] {
				seen[p.res] = true
				out = append(out, p)
			}
		}
	}

	for _, e := range ctx.javaManagedPeersClosure(jm) {
		if e.java != nil && !e.java.consumesUnmanageable() {
			add(ctx.javaUnmanageablePeersClosure(e.java))
		}
	}

	add(jm.unmanageable)

	jm.unmanageableDart = out

	return out
}

func (jm *JavaModule) checksClasspathClash() bool {
	if v := jm.data.checkJavaDeps; v != "yes" && v != "strict" {
		return false
	}

	u := jm.unit

	switch {
	case u.kind == juTest, u.sub == KindJavaFull, u.kind == juJar && jm.stmt == tokJavaLibraryNonSplit:
		return true
	case u.kind == juRunnable:
		return !jm.data.uberjar
	case u.sub == KindJavaCompilation && (jm.stmt == tokJavaProgram || jm.stmt == tokJavaAnnotationProcessor):
		return jm.data.uberjar
	}

	return false
}

func (ctx *GenCtx) javaClasspathClashSuite(jm *JavaModule) *TestSuite {
	d := jm.data
	name := strings.Trim(path.Base(path.Dir(jm.dir))+"-"+path.Base(jm.dir)+"-dependencies", "-")
	s := ctx.javaSuiteBase(jm, name, "classpath.clash", "style")

	s.setupPythonpathEnv = true
	s.supportsTestParameters = true

	paths := []string{jm.dir + "/" + jm.name + ".jar"}

	for _, e := range ctx.javaManagedPeersClosure(jm) {
		if e.java != nil {
			paths = append(paths, e.path.relString())
		}
	}

	for _, p := range paths {
		if info := ctx.javaReg(jm).use(build(p)); info != nil {
			s.deps = append(s.deps, info.ProducerRef)
		}
	}

	var ignored []string

	for _, c := range sortedUnique(d.test.ignoreClasspathClash) {
		ignored = append(ignored, "ignore_class:"+c)
	}

	s.runCmd = func(work string, _ TestChunk) []ANY {
		strs := []string{testToolHostPath, "run_check", "--source-root", "$(S)", "--checker", "run_classpath_clash", "--check-name", "classpath.clash",
			"--trace-path", work + "/ytest.report.trace", "--out-path", work + "/testing_out_stuff",
			"--log-path", work + "/testing_out_stuff/run_check.log", "--no-snippet-from-stderr", "--batch", "--batch-name", name,
			"$(B)", testCmdFileStart}

		strs = append(append(append(strs, paths...), testCmdFileEnd), ignored...)

		if ctx.tests.dist || d.checkJavaDeps == "strict" {
			strs = append(strs, "--")
		}

		if ctx.tests.dist {
			strs = append(strs, "--verbose")
		}

		if d.checkJavaDeps == "strict" {
			strs = append(strs, "--strict")
		}

		return testArgs(strs)
	}

	return s
}
