package main

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var notsScriptNameSeparator = regexp.MustCompile(`[^\p{L}\p{N}]+`)

func (e *EmitContext) notsTestGlobals(globals []ResourceDecl) []ResourceDecl {
	for _, check := range e.d.styleChecks {
		if check.Peer != "" {
			peer := e.genPeerModule(e.derivePeerInstance(check.Peer))

			globals = append(globals, peer.ResourceGlobalClosure...)
		}
	}

	if peerYaMakeExists(e.ctx.fs, buildPlatformPythonYmakePython3) {
		res := genModule(e.ctx, e.derivePeerInstance("build/platform/python/ymake_python3"))

		globals = append(globals, res.ResourceGlobalClosure...)
	}

	seen := map[STR]bool{}
	out := make([]ResourceDecl, 0, len(globals))

	for _, decl := range globals {
		if !seen[decl.GlobalVar] {
			seen[decl.GlobalVar] = true
			out = append(out, decl)
		}
	}

	return out
}

func (e *EmitContext) notsAllFiles(dir string) []VFS {
	info := e.codegen.lookup(build(dir, "/package.json"))
	state := info.nots()

	if !state.SourcesLoaded {
		state.SourceFiles = notsAllSourceFiles(e.ctx.fs, dir)
		state.SourcesLoaded = true
	}

	return state.SourceFiles
}

func (e *EmitContext) notsFiles(dir string, outputs, excludes []string) []VFS {
	patterns := append(slices.Clone(e.d.nots.BaseExcludes), excludes...)

	for _, output := range outputs {
		patterns = append(patterns, strings.Trim(output, "/")+"/**/*")
	}

	return notsFilterFiles(e.notsAllFiles(dir), dir, patterns)
}

func notsFilterFiles(files []VFS, dir string, excludes []string) []VFS {
	out := slices.Clone(files)

	for _, exclude := range excludes {
		pattern := notsGlob(exclude)

		out = slices.DeleteFunc(out, func(file VFS) bool { return pattern.MatchString(strings.TrimPrefix(file.relString(), dir+"/")) })
	}

	return out
}

func (e *EmitContext) notsSuite(name string, globals []ResourceDecl, addSource bool) *TestSuite {
	s := &TestSuite{p: e.instance.Platform, projectPath: e.instance.Path.relString(), name: name, testType: name, ciType: "test", globals: globals, noCleanEnvironment: true}

	s.applyTestMeta(e.d.test, addSource)
	s.split = 1

	return s
}

func (e *EmitContext) notsSuiteDependencies(target string) []NodeRef {
	prepare := e.codegen.use(build(target, "/package.json"))
	refs := []NodeRef{prepare.ProducerRef, prepare.nots().ToolRef}
	pj := e.ctx.parsers.notsJSON(target + "/package.json")

	for _, peer := range e.ctx.parsers.notsPeers(target, pj) {
		info := e.codegen.use(build(peer, "/output.tar"))

		refs = append(refs, info.ProducerRef)

		for _, input := range info.notsInputs() {
			if dep := e.codegen.lookup(input); dep != nil && dep.nots() != nil {
				refs = append(refs, dep.ProducerRef)
			}
		}

		if info.nots().Schema {
			instance := e.notsPeerInstance(peer)

			instance.Language = LangProtoDescriptions

			result := genModule(e.ctx, instance)

			if result.ARPath != nil {
				refs = append(refs, result.ARRef)
			}
		}
	}

	return dedupRefs(refs)
}

func notsRecipeArgs(args []string, globals []ResourceDecl) string {
	out := slices.Clone(args)

	for i, arg := range out {
		for _, decl := range globals {
			arg = strings.ReplaceAll(arg, decl.Value.string(), "$"+decl.GlobalVar.string())
		}

		arg = strings.ReplaceAll(strings.ReplaceAll(arg, "$(S)", "$ARCADIA_ROOT"), "$(B)", "$ARCADIA_BUILD_ROOT")
		out[i] = arg
	}

	return strings.Join(out, " ")
}

func (e *EmitContext) notsRecipes(target string, head []string, globals []ResourceDecl, extract, output, epilogue bool) string {
	var recipes []string

	if epilogue {
		recipes = append(recipes, e.d.test.recipes...)
	}

	if extract {
		layer := "workspace_node_modules.tar"
		extra := ""

		if e.d.nots.Hermetic {
			layer = "node_modules.layer"

			for _, decl := range globals {
				if decl.Name.string() == "SQUASHFS_TOOLS" {
					extra = " --squashfs-tools-path $" + decl.GlobalVar.string()
				}
			}
		}

		recipes = append(recipes, notsBuilderDir+"/builder extract-node-modules "+target+" "+layer+extra)
	} else {
		recipes = append(recipes, notsBuilderDir+"/builder install-node-modules "+notsRecipeArgs(head, globals)+" --nm-bundle no --for-path "+target)
	}

	if output {
		recipes = append(recipes, notsBuilderDir+"/builder extract-output-tars "+target)
	}

	if !epilogue {
		recipes = append(recipes, e.d.test.recipes...)
	}

	return testRecipes(recipes)
}

func (e *EmitContext) notsCheckSuites(target string, globals []ResourceDecl, files []VFS, head []string, node ResourceDecl) []*TestSuite {
	if !e.ctx.testMode || len(files) == 0 || len(e.d.nots.Checks) == 0 {
		return nil
	}

	var suites []*TestSuite

	var names []string
	var related []string

	for _, file := range files {
		names = append(names, strings.TrimPrefix(file.relString(), target+"/"))
		related = append(related, file.string())
	}

	related = sortedUnique(append(related, source(target, "/package.json").string()))

	deps := e.notsSuiteDependencies(target)

	var ldEnv []string

	if i := slices.Index(head, "--ld-library-path"); i >= 0 {
		ldEnv = []string{"LD_LIBRARY_PATH=" + head[i+1]}
	}

	for _, check := range e.d.nots.Checks {
		kind, ciType := "test", "test"

		if check.Lint {
			kind, ciType = "lint", "style"
		}

		name := "ts_" + kind + "_" + notsScriptNameSeparator.ReplaceAllString(check.Script, "_")
		s := e.notsSuite(name, globals, false)

		s.deps = slices.Clone(deps)

		if e.d.nots.Kind == tokTsTestFor && e.d.nots.DependsOnBuild {
			s.depends = append(slices.Clone(s.depends), target)
		}

		s.ciType = ciType
		s.noRetries = true

		if check.Medium {
			s.size, s.timeout = "medium", 600
		} else if check.Lint {
			s.size, s.timeout = "small", 60
		}

		if check.Lint {
			s.env, s.tags, s.data, s.relatedPaths = nil, nil, nil, nil
		}

		if ldEnv != nil {
			s.env = ldEnv
		}

		for key, value := range map[string]string{"cpu": "4", "ram": "16", "ram_disk": "4"} {
			if _, exists := s.requirements[key]; !exists {
				s.requirements[key] = value
			}
		}

		s.relatedPaths = append(slices.Clone(related), sortedUnique(s.relatedPaths)...)
		s.recipes = e.notsRecipes(target, head, globals, e.d.nots.Hermetic, true, true)
		s.runCmd = func(work string, _ TestChunk) []ANY {
			args := []string{testToolHostPath, "run_ts_check", "--source-root", testSourceRoot, "--build-root", testBuildRoot, "--output-dir", work + "/testing_out_stuff", "--tracefile", work + "/ytest.report.trace", "--log-path", work + "/testing_out_stuff/ts-check.log", "--target_path", target, "--nodejs", node.Value.string(), "--script-name", check.Script}

			if check.Command != "" {
				args = append(args, "--command", check.Command)
			}

			args = append(args, "--test-type", s.testType)
			args = append(args, names...)

			return testArgs(args)
		}
		suites = append(suites, s)
	}

	return suites
}

func (e *EmitContext) notsBuildSuites(dir string, state *NotsPrepared, files []VFS) []*TestSuite {
	if !e.ctx.testMode || e.d.nots.Kind == tokProtoLibrary {
		return nil
	}

	head := state.Head[1:]
	suites := e.notsCheckSuites(dir, state.Globals, files, head, state.Nodejs)

	if !isNotsLegacyModule(e.d.nots.Kind) {
		return suites
	}

	return append(suites, e.notsLegacyStyles(dir, state, head, files)...)
}

func (e *EmitContext) notsForSuites(target string, ref NodeRef, globals []ResourceDecl) []*TestSuite {
	if !e.ctx.testMode {
		return nil
	}

	c := e.d.nots
	pj := e.ctx.parsers.notsJSON(target + "/package.json")
	config := *c

	if c.Kind != tokTsTestFor {
		config.Hermetic = false
	}

	head, ownGlobals, node := e.notsBaseArgsFor(pj, &config)

	globals = e.notsTestGlobals(append(slices.Clone(ownGlobals), globals...))

	if c.Kind == tokTsTestFor {
		return e.notsCheckSuites(target, globals, e.notsFiles(target, nil, c.Excludes), head, node)
	}

	name := "jest"

	if c.Kind == tokTsTestVitestFor {
		name = "vitest"
	}

	s := e.notsSuite(name, globals, false)

	s.deps = append(e.notsSuiteDependencies(target), ref)
	s.runInputs = []VFS{e.instance.Path}
	s.supportsCanonization = true
	s.smoothShutdown = []string{"SIGUSR2"}
	s.inputNames = []string{target + "/package.json"}
	s.recipes = e.notsRecipes(target, head, globals, false, true, false)

	testConfig := name + ".config.js"

	if name == "vitest" {
		testConfig = "vitest.config.ts"
	}

	if c.TestConfig != "" {
		testConfig = strings.TrimPrefix(path.Join(e.instance.Path.relString(), c.TestConfig), target+"/")
	}

	tsconfig := c.Configs[0]

	if c.ConfigChanged {
		tsconfig = strings.TrimPrefix(path.Join(e.instance.Path.relString(), tsconfig), target+"/")
	}

	var dataDirs []string

	for _, glob := range c.TestData {
		for _, file := range globMatch(e.ctx.fs, path.Join(e.instance.Path.relString(), glob)) {
			dataDirs = append(dataDirs, path.Dir(file))
		}
	}

	dataDirs = sortedUnique(dataDirs)
	s.runCmd = func(work string, _ TestChunk) []ANY {
		args := []string{testToolHostPath, "run_" + name, "--source-root", testSourceRoot, "--build-root", testBuildRoot, "--project-path", s.projectPath, "--test-work-dir", work, "--output-dir", work + "/testing_out_stuff", "--tracefile", work + "/ytest.report.trace", "--test-for-path", build(target).string(), "--node-path", build(target, "/node_modules").string(), "--nodejs", node.Value.string()}

		if len(dataDirs) > 0 {
			args = append(args, "--test-data-dirs")
			args = append(args, dataDirs...)
		}

		args = append(args, "--config", testConfig, "--ts-config-path", tsconfig, "--timeout", strconv.Itoa(s.timeout), "--verbose")

		return testArgs(args)
	}

	return []*TestSuite{s}
}

func (e *EmitContext) notsLegacyStyles(dir string, state *NotsPrepared, head []string, sourceFiles []VFS) []*TestSuite {
	c := e.d.nots
	excludes := append([]string{"(build|dist|bundle|.next)/**/*"}, c.Excludes...)
	all := notsFilterFiles(e.notsAllFiles(dir), dir, excludes)

	var checks []struct{ name, config, pattern string }

	if e.d.noLint != "none" {
		config := c.ESLintConfig

		if config == "" {
			config = ".eslintrc.js"
		}

		checks = append(checks, struct{ name, config, pattern string }{"eslint", config, "**/*.(ts|tsx|js|jsx)"})
	}

	if c.Typecheck {
		config := c.TypecheckConfig

		if config == "" {
			config = c.Configs[0]
		}

		checks = append(checks, struct{ name, config, pattern string }{"tsc_typecheck", config, "**/*"})
	}

	if c.StylelintConfig != "" {
		checks = append(checks, struct{ name, config, pattern string }{"ts_stylelint", c.StylelintConfig, "**/*.(css|scss|less)"})
	}

	if c.BiomeConfig != "" {
		checks = append(checks, struct{ name, config, pattern string }{"ts_biome", c.BiomeConfig, "**/*.(ts|tsx|js|jsx|css|json|gql|graphql)"})
	}

	var suites []*TestSuite

	node := state.Nodejs

	for _, check := range checks {
		pattern := notsGlob(check.pattern)

		var files, related []string

		candidates := all

		if check.name == "tsc_typecheck" {
			candidates = sourceFiles
		}

		for _, file := range candidates {
			rel := strings.TrimPrefix(file.relString(), dir+"/")

			if pattern.MatchString(rel) {
				files = append(files, rel)
				related = append(related, file.string())
			}
		}

		if len(files) == 0 {
			continue
		}

		s := e.notsSuite(check.name, state.Globals, false)

		s.ciType = "style"
		s.relatedPaths = sortedUnique(append(related, source(path.Join(dir, check.config)).string(), source(dir, "/package.json").string()))
		s.runInputs = []VFS{build(dir, "/package.json")}
		s.deps = e.notsSuiteDependencies(dir)
		s.runCwd = build(dir).string()
		s.recipes = e.notsRecipes(dir, head, state.Globals, false, check.name == "ts_biome", true)
		s.runCmd = func(work string, _ TestChunk) []ANY {
			args := []string{testToolHostPath, "run_" + check.name, "--source-root", testSourceRoot, "--build-root", testBuildRoot}

			switch check.name {
			case "eslint":
				args = append(args, "--source-folder-path", dir, "--nodejs", node.Value.string(), "--eslint-config-path", check.config, "--ts-config-path", c.Configs[0], "--tracefile", work+"/ytest.report.trace")
			case "tsc_typecheck":
				args = append(args, "--source-folder-path", dir, "--nodejs", node.Value.string(), "--ts-config-path", check.config, "--tracefile", work+"/ytest.report.trace", "--log-path", work+"/testing_out_stuff/run_tsc_typecheck.log")
			default:
				args[1] = "run_biome"

				if check.name == "ts_stylelint" {
					args[1] = "run_stylelint"
				}

				args = append(args, "--project-path", dir, "--nodejs-dir", node.Value.string(), "--test-config", check.config, "--trace", work+"/ytest.report.trace")
			}

			for _, file := range files {
				if check.name == "tsc_typecheck" && !slices.Contains([]string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"}, path.Ext(file)) {
					continue
				}

				args = append(args, file)
			}

			return testArgs(args)
		}
		suites = append(suites, s)
	}

	return suites
}
