package main

import (
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
)

const yamlfmtResourcePeer = "build/external_resources/yamlfmt"

type StyleCheck struct {
	Name               string
	ConfigMap          string
	Config             string
	ConfigType         string
	ConfigDir          string
	Wrapper            string
	Resource           string
	Files              []string
	ExtraConfigs       []string
	ExtraParams        []string
	FileProcessingTime float64
	Python             bool
	CPP                bool
	Depends            []string
	Peer               string
}

func (d *ModuleData) collectPythonStyleChecks(env Environment) {
	if d.moduleStmt == nil {
		return
	}

	name := ""

	switch d.moduleStmt.Name {
	case tokPy2Program, tokPy2Library, tokPy2Test, tokPytestBin:
		name = "py2_flake8"
	case tokPy3Program, tokPy3ProgramBin, tokPy3Library, tokPy23Library, tokPy3Test:
		name = "flake8"
	}

	if name != "" {
		d.styleChecks = append(d.styleChecks, pythonStyleCheck(name, env))
	}

	checks := d.styleChecks[:0]

	for _, check := range d.styleChecks {
		if check.CPP && (d.noLint == "none" || d.noLint == "none_internal" || env.bool(envCppAnalysisMode)) {
			continue
		}

		if check.Python {
			if d.noLint == "none" || d.noLint == "none_internal" || env.bool(envCppAnalysisMode) {
				continue
			}

			if (check.Name == "flake8" || check.Name == "py2_flake8") && env.bool(envDisableFlake8) {
				continue
			}

			d.peerdirs = append(d.peerdirs, internStr(check.Peer).any())
		}

		checks = append(checks, check)
	}

	d.styleChecks = checks
}

func pythonStyleCheck(name string, env Environment) StyleCheck {
	check := StyleCheck{Name: name, Python: true, ConfigMap: "build/config/tests/py_style/default_configs.json", FileProcessingTime: 1.5}
	timeVar := "FLAKE8_FILE_PROCESSING_TIME"

	switch name {
	case "flake8", "py2_flake8":
		version := "PY3"

		if name == "py2_flake8" {
			version = "PY2"
		}

		check.Peer = "build/external_resources/flake8_" + strings.ToLower(version)
		check.Resource = "FLAKE8_" + version + "_RESOURCE_GLOBAL"
		check.Wrapper = "tools/flake8_linter/__main__.py"

		if env.bool(envDisableFlake8Migrations) {
			check.ExtraParams = []string{"DISABLE_FLAKE8_MIGRATIONS=yes"}
		} else {
			check.ExtraConfigs = []string{"build/rules/flake8/migrations.yaml"}
		}
	case "black":
		check.Peer, check.Resource, check.Wrapper = "build/external_resources/black", "BLACK_RESOURCE_GLOBAL", "tools/black_linter/wrapper.py"
		timeVar = "BLACK_FILE_PROCESSING_TIME"
	case "ruff":
		check.Peer, check.Resource, check.Wrapper = "build/external_resources/ruff", "RUFF_RESOURCE_GLOBAL", "tools/ruff_linter/wrapper.py"
		check.FileProcessingTime = 0.5

		timeVar = "RUFF_FILE_PROCESSING_TIME"
		check.ExtraParams = []string{"check_format=no", "run_in_source_root=no"}
	}

	if value, ok := env.lookup("PYTHON_LINTERS_DEFAULT_CONFIGS"); ok && value != "" {
		check.ConfigMap = value
	}

	if value, ok := env.lookup(timeVar); ok && value != "" {
		check.FileProcessingTime = throw2(strconv.ParseFloat(value, 64))
	}

	return check
}

func (d *ModuleData) collectExplicitPythonStyle(stmt UnknownStmt, env Environment) {
	if stmt.Name == tokStylePython && d.moduleStmt != nil {
		switch d.moduleStmt.Name {
		case tokPy2Program, tokPy2Test, tokPy2Library:
			return
		}
	}

	name := "black"

	if stmt.Name == tokStyleRuff {
		name = "ruff"
	}

	check := pythonStyleCheck(name, env)

	for i := 0; i < len(stmt.Args); i++ {
		switch arg := stmt.Args[i].string(); arg {
		case "CONFIG_TYPE":
			i++

			if i >= len(stmt.Args) {
				throwFmt("STYLE: CONFIG_TYPE requires a config type")
			}

			check.ConfigType = stmt.Args[i].string()
			check.ConfigDir, _ = env.lookup("MODULE_COMMON_CONFIGS_DIR")
		case "CHECK_FORMAT":
			if name != "ruff" {
				throwFmt("STYLE: CHECK_FORMAT requires ruff")
			}

			check.ExtraParams[0] = "check_format=yes"
		case "RUN_IN_SOURCE_ROOT":
			if name != "ruff" {
				throwFmt("STYLE: RUN_IN_SOURCE_ROOT requires ruff")
			}

			check.ExtraParams[1] = "run_in_source_root=yes"
		}
	}

	d.styleChecks = append(d.styleChecks, check)
}

func (d *ModuleData) collectYAMLStyleWithConfig(module string, env Environment) {
	if value, _ := env.lookup("NO_LINT"); value == "none" || value == "none_internal" {
		return
	}

	if value, _ := env.lookup("TIDY"); value == "yes" {
		return
	}

	d.peerdirs = append(d.peerdirs, internStr(yamlfmtResourcePeer).any())

	if files, _ := env.lookup("YAML_LINT_SRCS"); strings.TrimSpace(files) != "" {
		throwFmt("%s: STYLE_YAML_WITH_CONFIG over YAML_LINT_SRCS is not modelled", module)
	}
}

func (d *ModuleData) collectStyleCheck(fs FS, module string, stmt UnknownStmt, env Environment) {
	if value, _ := env.lookup("NO_LINT"); value == "none" || value == "none_internal" {
		return
	}

	if value, _ := env.lookup("TIDY"); value == "yes" {
		return
	}

	check := StyleCheck{Name: "clang_format_json", ConfigMap: "build/config/tests/json_style/default_configs.json", Wrapper: "tools/clang_format_json_linter/wrapper.py", Resource: "CLANG_FORMAT_RESOURCE_GLOBAL"}
	peer, configVar := "build/platform/clang/clang-format", "JSON_LINTER_DEFAULT_CONFIGS"
	yaml := stmt.Name == tokStyleYAML

	if yaml {
		check = StyleCheck{Name: "yamlfmt", ConfigMap: "build/config/tests/yaml_style/default_configs.json", Wrapper: "tools/yamlfmt_linter/wrapper.py", Resource: "YAMLFMT_RESOURCE_GLOBAL"}
		peer, configVar = yamlfmtResourcePeer, "YAML_LINTER_DEFAULT_CONFIGS"
	}

	if value, ok := env.lookup(configVar); ok && value != "" {
		check.ConfigMap = value
	}

	var dirs, recursive []string

	dst := &dirs

	for _, arg := range stmt.Args {
		switch arg.string() {
		case "DIRS":
			dst = &dirs
		case "DIRS_RECURSE":
			dst = &recursive
		default:
			*dst = append(*dst, arg.string())
		}
	}

	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	var patterns []string

	for _, dir := range dirs {
		patterns = append(patterns, path.Join(module, dir)+"/*")
	}

	for _, dir := range recursive {
		patterns = append(patterns, path.Join(module, dir)+"/**")
	}

	for _, pattern := range patterns {
		for _, file := range globMatch(fs, pattern) {
			rel := strings.TrimPrefix(file, module+"/")

			if rel == "ya.make" || strings.HasPrefix(rel, "canondata/") {
				continue
			}

			if path.Base(file) == "ya.make" {
				throwFmt("STYLE: nested module %s", file)
			}

			if yaml {
				if path.Base(file) == "a.yaml" || path.Base(file) == "t.yaml" || (path.Ext(file) != ".yaml" && path.Ext(file) != ".yml") {
					continue
				}
			} else if path.Ext(file) != ".json" {
				continue
			}

			check.Files = append(check.Files, file)
		}
	}

	slices.Sort(check.Files)
	check.Files = slices.Compact(check.Files)

	if len(check.Files) == 0 {
		return
	}

	for i := range d.styleChecks {
		if previous := &d.styleChecks[i]; previous.Name == check.Name {
			previous.Files = sortedUnique(append(previous.Files, check.Files...))

			return
		}
	}

	d.styleChecks = append(d.styleChecks, check)
	d.peerdirs = append(d.peerdirs, internStr(peer).any())
}

func (e *EmitContext) styleSuites(globals []ResourceDecl) []*TestSuite {
	if !e.ctx.testMode {
		return nil
	}

	var suites []*TestSuite
	var pythonFiles []string

	for _, ps := range e.pySrcsReg {
		if ps.Kind == pySourcePlain && ps.Path.isSource() {
			pythonFiles = append(pythonFiles, ps.Path.relString())
		}
	}

	for _, stmt := range e.d.cythonCpp {
		if stmt.Generated == nil && extIsPy(stmt.Src) {
			pythonFiles = append(pythonFiles, e.instance.Path.relString()+"/"+stmt.Src)
		}
	}

	pythonFiles = sortedUnique(pythonFiles)

	for _, check := range e.d.styleChecks {
		if check.CPP {
			check.Files = sortedUnique(check.Files)

			if len(check.Depends) != 0 && !customClangFormatAllowed(e.instance.Path.relString()) {
				throwFmt("STYLE: custom clang-format is not allowed in %s", e.instance.Path.relString())
			}
		}

		if check.Python {
			check.Files = pythonFiles

			if len(check.Files) == 0 {
				continue
			}

			if slices.Contains(check.ExtraParams, "run_in_source_root=yes") {
				module := e.instance.Path.relString()
				allowed := []string{"electro", "fintech/uservices", "maps/tariffs", "market/media_adv/madv-inspector", "taxi"}

				if !slices.ContainsFunc(allowed, func(prefix string) bool { return strings.HasPrefix(module, prefix) }) {
					throwFmt("STYLE: running ruff in source root is not allowed in %s", module)
				}
			}
		}

		if check.ConfigType != "" {
			if check.CPP && check.ConfigType != ".clang-format" || !check.CPP && check.ConfigType != "pyproject.toml" && (check.Name != "ruff" || check.ConfigType != "ruff.toml") {
				throwFmt("STYLE: unknown %s config type %s", check.Name, check.ConfigType)
			}

			if check.ConfigDir == "" {
				if inc, ok := e.ctx.autoincludeIdx.lintersMakeIncFor(e.instance.Path.relString()); ok {
					check.ConfigDir = path.Dir(inc.relString())
				}
			}

			if check.ConfigDir == "" {
				throwFmt("STYLE: config type requires MODULE_COMMON_CONFIGS_DIR")
			}

			check.Config = path.Join(check.ConfigDir, check.ConfigType)

			if !e.ctx.fs.isFilePathClean(check.Config) {
				throwFmt("STYLE: missing config %s", check.Config)
			}
		}

		if check.Config == "" && check.ConfigMap != "" {
			config, ok := e.ctx.parsers.notsJSON(check.ConfigMap)[check.Name].(string)

			if !ok {
				throwFmt("STYLE: missing %s config in %s", check.Name, check.ConfigMap)
			}

			check.Config = config
		}

		if check.CPP && len(check.Files) == 0 && e.d.moduleStmt.Name == tokGenLibrary {
			continue
		}

		if check.CPP && slices.ContainsFunc(suites, func(suite *TestSuite) bool { return suite.testType == check.Name }) {
			continue
		}

		var resources []ResourceDecl

		if check.Resource == "" {
			resources = globals
		} else {
			for _, r := range globals {
				if r.GlobalVar.string() == check.Resource {
					resources = append(resources, r)

					break
				}
			}
		}

		suites = append(suites, e.styleSuite(check, resources))
	}

	return suites
}

func (e *EmitContext) styleSuite(check StyleCheck, resources []ResourceDecl) *TestSuite {
	project := e.instance.Path.relString()
	s := &TestSuite{p: e.ctx.target, depends: slices.Clone(check.Depends), projectPath: project, name: check.Name, testType: check.Name, ciType: "style", size: "small", timeout: 60, supportsTestParameters: true, globals: resources, env: e.d.test.env, runInputs: []VFS{source(project)}, styleCheck: &check}

	s.mergeStyleFiles(nil)

	s.runCmd = func(work string, chunk TestChunk) []ANY {
		args := []string{testToolHostPath, "run_custom_lint", "--source-root", testSourceRoot, "--build-root", testBuildRoot, "--project-path", source(project).string(), "--trace-path", work + "/ytest.report.trace", "--out-path", work + "/testing_out_stuff", "--lint-name", check.Name, "--wrapper-script", check.Wrapper}

		for _, dependency := range sortedUnique(append(slices.Clone(s.depends), project)) {
			args = append(args, "--depends", dependency)
		}

		if check.Config != "" {
			args = append(args, "--config", source(check.Config).string())
		}

		for _, config := range check.ExtraConfigs {
			args = append(args, "--config", source(config).string())
		}

		for _, r := range sortedResourceGlobals(resources) {
			args = append(args, "--global-resource", r.Token.string())
		}

		for _, param := range check.ExtraParams {
			args = append(args, "--extra-param", param)
		}

		for i := chunk.index; i < len(check.Files); i += max(chunk.split, 1) {
			args = append(args, source(check.Files[i]).string())
		}

		return testArgs(args)
	}

	return s
}

func (s *TestSuite) mergeStyleFiles(files []string) {
	check := s.styleCheck

	check.Files = sortedUnique(append(check.Files, files...))
	s.relatedPaths = nil

	for _, file := range check.Files {
		s.relatedPaths = append(s.relatedPaths, source(file).string())
	}

	if check.Config != "" {
		s.relatedPaths = append(s.relatedPaths, source(check.Config).string())
	}

	for _, config := range check.ExtraConfigs {
		s.relatedPaths = append(s.relatedPaths, source(config).string())
	}

	s.relatedPaths = append(s.relatedPaths, source(check.Wrapper).string())

	if check.FileProcessingTime > 0 {
		s.split = max(1, int(math.Ceil(check.FileProcessingTime*float64(len(check.Files))/float64(s.timeout))))
	}
}
