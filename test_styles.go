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
	Wrapper            string
	Resource           string
	Files              []string
	ExtraConfigs       []string
	ExtraParams        []string
	FileProcessingTime float64
}

func (d *ModuleData) collectPy2StyleCheck(env Environment) {
	if !d.usePython2 {
		return
	}

	switch d.moduleStmt.Name {
	case tokPy2Program, tokPy2Library, tokPy2Test:
	default:
		return
	}

	if d.noLint == "none" || d.noLint == "none_internal" || env.bool(envCppAnalysisMode) || env.bool(envDisableFlake8) {
		return
	}

	check := StyleCheck{Name: "py2_flake8", ConfigMap: "build/config/tests/py_style/default_configs.json", Wrapper: "tools/flake8_linter/__main__.py", Resource: "FLAKE8_PY2_RESOURCE_GLOBAL", FileProcessingTime: 1.5}

	if value, ok := env.lookup("PYTHON_LINTERS_DEFAULT_CONFIGS"); ok && value != "" {
		check.ConfigMap = value
	}

	if value, ok := env.lookup("FLAKE8_FILE_PROCESSING_TIME"); ok && value != "" {
		check.FileProcessingTime = throw2(strconv.ParseFloat(value, 64))
	}

	if env.bool(envDisableFlake8Migrations) {
		check.ExtraParams = []string{"DISABLE_FLAKE8_MIGRATIONS=yes"}
	} else {
		check.ExtraConfigs = []string{"build/rules/flake8/migrations.yaml"}
	}

	d.styleChecks = append(d.styleChecks, check)
	d.peerdirs = append(d.peerdirs, strBuildExternalResourcesFlake8Py2.any())
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

	d.styleChecks = append(d.styleChecks, check)
	d.peerdirs = append(d.peerdirs, internStr(peer).any())
}

func (e *EmitContext) styleSuites(globals []ResourceDecl) []*TestSuite {
	if !e.ctx.testMode {
		return nil
	}

	var suites []*TestSuite

	for _, check := range e.d.styleChecks {
		if check.Name == "py2_flake8" {
			for _, ps := range e.pySrcsReg {
				if ps.Kind == pySourcePlain && ps.Path.isSource() {
					check.Files = append(check.Files, ps.Path.relString())
				}
			}

			check.Files = sortedUnique(check.Files)

			if len(check.Files) == 0 {
				continue
			}
		}

		config, ok := e.ctx.parsers.notsJSON(check.ConfigMap)[check.Name].(string)

		if !ok {
			throwFmt("STYLE: missing %s config in %s", check.Name, check.ConfigMap)
		}

		check.Config = config
		var resources []ResourceDecl

		for _, r := range globals {
			if r.GlobalVar.string() == check.Resource {
				resources = append(resources, r)

				break
			}
		}

		suites = append(suites, e.styleSuite(check, resources))
	}

	return suites
}

func (e *EmitContext) styleSuite(check StyleCheck, resources []ResourceDecl) *TestSuite {
	project := e.instance.Path.relString()
	s := &TestSuite{p: e.ctx.target, projectPath: project, name: check.Name, testType: check.Name, ciType: "style", size: "small", timeout: 60, supportsTestParameters: true, globals: resources, runInputs: []VFS{source(project)}}

	for _, file := range check.Files {
		s.relatedPaths = append(s.relatedPaths, source(file).string())
	}

	s.relatedPaths = append(s.relatedPaths, source(check.Config).string())

	for _, config := range check.ExtraConfigs {
		s.relatedPaths = append(s.relatedPaths, source(config).string())
	}

	s.relatedPaths = append(s.relatedPaths, source(check.Wrapper).string())

	if check.FileProcessingTime > 0 {
		s.split = max(1, int(math.Ceil(check.FileProcessingTime*float64(len(check.Files))/float64(s.timeout))))
	}

	s.runCmd = func(work string, chunk TestChunk) []ANY {
		args := []string{testToolHostPath, "run_custom_lint", "--source-root", testSourceRoot, "--build-root", testBuildRoot, "--project-path", source(project).string(), "--trace-path", work + "/ytest.report.trace", "--out-path", work + "/testing_out_stuff", "--lint-name", check.Name, "--wrapper-script", check.Wrapper, "--depends", project, "--config", source(check.Config).string()}

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
