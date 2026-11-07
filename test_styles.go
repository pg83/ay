package main

import (
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
)

var (
	flake8MigrationParams  = []string{"DISABLE_FLAKE8_MIGRATIONS=yes"}
	flake8MigrationConfigs = []string{"build/rules/flake8/migrations.yaml"}
)

const yamlfmtResourcePeer = "build/external_resources/yamlfmt"

type StyleCheck struct {
	Name               string
	ConfigMap          STR
	Config             string
	ConfigType         string
	ConfigDir          string
	Wrapper            string
	Resource           STR
	Files              []VFS
	ExtraConfigs       []string
	ExtraParams        []string
	FileProcessingTime float64
	Python             bool
	CPP                bool
	Depends            []STR
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
	case tokPy3Program, tokPy3ProgramBin, tokPy3Library, tokPy23Library, tokPy3Test, tokPy3TestBin, tokYqlUdfMinitest:
		name = "flake8"
	}

	if name != "" {
		d.styleChecks = append(d.styleChecks, pythonStyleCheck(name, env))
	}

	checks := d.styleChecks[:0]

	for _, check := range d.styleChecks {
		if noLint := env.string(envNoLintValue); check.CPP && (noLint == "none" || noLint == "none_internal" || env.bool(envCppAnalysisMode)) {
			continue
		}

		if check.Python {
			if d.pythonLint.analysis {
				continue
			}

			raw := d.pythonLint.value()

			if raw == "none" || raw == "none_internal" {
				continue
			}

			d.peerdirs = append(d.peerdirs, internStr(check.Peer).any())
		}

		checks = append(checks, check)
	}

	d.styleChecks = checks
}

func pythonStyleCheck(name string, env Environment) StyleCheck {
	check := StyleCheck{Name: name, Python: true, ConfigMap: strBuildConfigTestsPyStyleDefaultConfigsJson, FileProcessingTime: 1.5}
	timeVar := envFlake8FileProcessingTime

	switch name {
	case "flake8", "py2_flake8":
		check.Peer = "build/external_resources/flake8_py3"
		check.Resource = strFlake8Py3ResourceGlobal

		if name == "py2_flake8" {
			check.Peer = "build/external_resources/flake8_py2"
			check.Resource = strFlake8Py2ResourceGlobal
		}

		check.Wrapper = "tools/flake8_linter/__main__.py"

		if env.bool(envDisableFlake8Migrations) {
			check.ExtraParams = flake8MigrationParams
		} else {
			check.ExtraConfigs = flake8MigrationConfigs
		}
	case "black":
		check.Peer, check.Resource, check.Wrapper = "build/external_resources/black", strBlackResourceGlobal, "tools/black_linter/wrapper.py"
		timeVar = envBlackFileProcessingTime
	case "ruff":
		check.Peer, check.Resource, check.Wrapper = "build/external_resources/ruff", strRuffResourceGlobal, "tools/ruff_linter/wrapper.py"
		check.FileProcessingTime = 0.5

		timeVar = envRuffFileProcessingTime
		check.ExtraParams = []string{"check_format=no", "run_in_source_root=no"}
	}

	if value, ok := env.lookupSTR(envPythonLintersDefaultConfigs); ok && value != strEmpty {
		check.ConfigMap = value
	}

	if value, ok := env.lookupENV(timeVar); ok && value != "" {
		check.FileProcessingTime = throw2(strconv.ParseFloat(value, 64))
	}

	return check
}

func (d *ModuleData) collectExplicitPythonStyle(stmt UnknownStmt, env Environment) {
	if stmt.Name == tokStylePython && d.moduleStmt != nil {
		internalTaskBinary := d.moduleStmt.Name == tokPy2Program && isSandboxPython2Task(d.multimodule)

		if !internalTaskBinary {
			switch d.moduleStmt.Name {
			case tokPy2Program, tokPy2Test, tokPy2Library:
				return
			}
		}
	}

	name := "black"

	if stmt.Name == tokStyleRuff {
		name = "ruff"
	}

	check := pythonStyleCheck(name, env)

	for i := 0; i < len(stmt.Args); i++ {
		switch stmt.Args[i] {
		case kwCONFIG_TYPE.any():
			i++

			if i >= len(stmt.Args) {
				throwFmt("STYLE: CONFIG_TYPE requires a config type")
			}

			check.ConfigType = stmt.Args[i].string()
			check.ConfigDir, _ = env.lookupENV(envModuleCommonConfigsDir)
		case strCheckFormat.any():
			if name != "ruff" {
				throwFmt("STYLE: CHECK_FORMAT requires ruff")
			}

			check.ExtraParams[0] = "check_format=yes"
		case strRunInSourceRoot.any():
			if name != "ruff" {
				throwFmt("STYLE: RUN_IN_SOURCE_ROOT requires ruff")
			}

			check.ExtraParams[1] = "run_in_source_root=yes"
		}
	}

	d.styleChecks = append(d.styleChecks, check)
}

func styleChecksDisabled(env Environment) bool {
	value := env.string(envNoLintValue)

	return value == "none" || value == "none_internal" || env.is(envTidy, strYes)
}

func yamlStyleCheck(env Environment) StyleCheck {
	check := StyleCheck{Name: "yamlfmt", ConfigMap: strBuildConfigTestsYamlStyleDefaultConfigsJson, Wrapper: "tools/yamlfmt_linter/wrapper.py", Resource: strYamlfmtResourceGlobal, Peer: yamlfmtResourcePeer}

	if value, ok := env.lookupSTR(envYamlLinterDefaultConfigs); ok && value != strEmpty {
		check.ConfigMap = value
	}

	return check
}

func (d *ModuleData) collectYAMLStyleWithConfig(module string, stmt UnknownStmt, env Environment) {
	if styleChecksDisabled(env) {
		return
	}

	check := yamlStyleCheck(env)
	files, _ := env.lookupENV(envYamlLintSrcs)

	for _, file := range strings.Fields(files) {
		check.Files = append(check.Files, source(styleSourcePath(module, file)))
	}

	args := stmt.Args

	if len(args) > 0 && args[0] == kwCONFIG_TYPE.any() {
		check.ConfigType = strings.Join(anyStrs(args[1:]), " ")
		check.ConfigDir, _ = env.lookupENV(envModuleCommonConfigsDir)
	}

	d.addFileStyleCheck(check)
}

func (d *ModuleData) collectStyleCheck(fs FS, module string, stmt UnknownStmt, env Environment) {
	if styleChecksDisabled(env) {
		return
	}

	check := explicitStyleCheck(stmt.Name, env)

	check.Files = collectStyleFiles(fs, module, stmt)
	d.addFileStyleCheck(check)
}

func explicitStyleCheck(name TOK, env Environment) StyleCheck {
	switch name {
	case tokStyleYAML, tokStyleYAMLConf:
		return yamlStyleCheck(env)
	case tokStyleYQL:
		return StyleCheck{Name: "yqlfmt", Wrapper: "tools/yqlfmt_linter/wrapper.py"}
	}

	check := StyleCheck{Name: "clang_format_json", ConfigMap: strBuildConfigTestsJsonStyleDefaultConfigsJson, Wrapper: "tools/clang_format_json_linter/wrapper.py", Resource: strClangFormatResourceGlobal, Peer: "build/platform/clang/clang-format"}

	if value, ok := env.lookupSTR(envJsonLinterDefaultConfigs); ok && value != strEmpty {
		check.ConfigMap = value
	}

	return check
}

func collectStyleFiles(fs FS, module string, stmt UnknownStmt) []VFS {
	var dirs, recursive []string
	var files []VFS
	var excludes []*globMatcher

	ext := ".json"
	yaml := stmt.Name == tokStyleYAML || stmt.Name == tokCollectYAML

	if stmt.Name == tokStyleYQL {
		ext = ".yql"
	}

	dst := &dirs

	for _, arg := range stmt.Args {
		switch arg {
		case kwDIRS.any():
			dst = &dirs
		case kwDIRS_RECURSE.any():
			dst = &recursive
		case kwEXCLUDE.any():
			dst = nil
		default:
			if dst == nil {
				excludes = append(excludes, globExcludeMatcher(arg.string()))
			} else {
				*dst = append(*dst, arg.string())
			}
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

	modulePrefix := module + "/"

	for _, pattern := range patterns {
		globVisit(fs, pattern, func(file string, view bool) {
			rel := strings.TrimPrefix(file, modulePrefix)

			if rel == "ya.make" || strings.HasPrefix(rel, "canondata/") || globExcluded(excludes, rel, false) {
				return
			}

			if path.Base(file) == "ya.make" {
			} else if yaml {
				if stmt.Name == tokStyleYAML && (path.Base(file) == "a.yaml" || path.Base(file) == "t.yaml") || (path.Ext(file) != ".yaml" && path.Ext(file) != ".yml") {
					return
				}
			} else if path.Ext(file) != ext {
				return
			}

			files = append(files, source(file))
		})
	}

	return sortedUniqueVFS(files)
}

func (d *ModuleData) addFileStyleCheck(check StyleCheck) {
	for _, file := range check.Files {
		if path.Base(file.relString()) == "ya.make" {
			throwFmt("STYLE: nested module %s", file.relString())
		}
	}

	if check.Peer != "" {
		d.peerdirs = append(d.peerdirs, internStr(check.Peer).any())
	}

	if len(check.Files) == 0 {
		return
	}

	for i := range d.styleChecks {
		if previous := &d.styleChecks[i]; previous.Name == check.Name {
			previous.Files = sortedUniqueVFS(append(previous.Files, check.Files...))

			return
		}
	}

	d.styleChecks = append(d.styleChecks, check)
}

func (e *EmitContext) styleSuites(globals []ResourceDecl) []*TestSuite {
	pythonFiles := e.pythonLinterFiles()

	if !e.ctx.testMode {
		return nil
	}

	var suites []*TestSuite

	project := e.instance.Path.relString()

	for _, check := range e.d.styleChecks {
		if check.CPP {
			prepareCPPStyle(&check, project)
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

		e.ctx.resolveStyleConfig(&check, project)

		if check.Python && (check.Name == "flake8" || check.Name == "py2_flake8") && e.d.pythonLint.disableF8 {
			continue
		}

		if check.CPP && len(check.Files) == 0 && e.d.moduleStmt.Name == tokGenLibrary {
			continue
		}

		if check.CPP && slices.ContainsFunc(suites, func(suite *TestSuite) bool { return suite.testType.string() == check.Name }) {
			continue
		}

		suites = append(suites, e.ctx.styleSuite(check, e.instance.Path.rel(), e.d.test.env, styleCheckResources(check, globals)))
	}

	return suites
}

func (ctx *GenCtx) resolveStyleConfig(check *StyleCheck, project string) {
	if check.ConfigType != "" {
		validType := check.ConfigType == ".clang-format" && check.CPP ||
			check.ConfigType == "pyproject.toml" && !check.CPP ||
			check.ConfigType == "ruff.toml" && check.Name == "ruff" ||
			check.ConfigType == ".yamlfmt.yml" && check.Name == "yamlfmt"

		if !validType {
			throwFmt("STYLE: unknown %s config type %s", check.Name, check.ConfigType)
		}

		if check.ConfigDir == "" {
			if inc, ok := ctx.autoincludeIdx.lintersMakeIncFor(project); ok {
				check.ConfigDir = path.Dir(inc.relString())
			}
		}

		if check.ConfigDir == "" {
			throwFmt("STYLE: config type requires MODULE_COMMON_CONFIGS_DIR")
		}

		check.Config = path.Join(check.ConfigDir, check.ConfigType)

		if !ctx.fs.isFilePathClean(check.Config) {
			throwFmt("STYLE: missing config %s", check.Config)
		}
	}

	if check.Config == "" && !check.ConfigMap.isEmpty() {
		config, ok := ctx.parsers.notsJSONID(check.ConfigMap)[check.Name].(string)

		if !ok {
			throwFmt("STYLE: missing %s config in %s", check.Name, check.ConfigMap.string())
		}

		check.Config = config
	}
}

func styleCheckResources(check StyleCheck, globals []ResourceDecl) []ResourceDecl {
	if check.Resource == 0 {
		return globals
	}

	resource := check.Resource

	for i, r := range globals {
		if r.GlobalVar == resource {
			return globals[i : i+1 : i+1]
		}
	}

	return nil
}

func (ctx *GenCtx) styleSuite(check StyleCheck, projectID STR, env []string, resources []ResourceDecl) *TestSuite {
	nameID := internStr(check.Name)
	s := &TestSuite{p: ctx.target, depends: slices.Clone(check.Depends), projectPath: projectID, name: nameID, testType: nameID, ciType: strStyle, size: strSmall, timeout: 60, supportsTestParameters: true, globals: resources, env: env, runInputs: []VFS{projectID.source()}, styleCheck: &check}

	s.mergeStyleFiles(nil)

	projectArg, nameArg, wrapperArg := projectID.source().any(), testArg(check.Name), testArg(check.Wrapper)

	s.runCmd = func(work string, chunk TestChunk) []ANY {
		args := []ANY{testToolHostPathArg, strRunCustomLint.any(), strSourceRoot2.any(), testSourceRootArg, strBuildRoot2.any(), testBuildRootArg, strProjectPath.any(), projectArg, strTracePath.any(), testArg(work + "/ytest.report.trace"), strOutPath.any(), testArg(work + "/testing_out_stuff"), strLintName.any(), nameArg, strWrapperScript.any(), wrapperArg}

		for _, dependency := range sortedUniqueSTR(append(slices.Clone(s.depends), projectID)) {
			arg := projectID.any()

			if dependency != projectID {
				arg = canonicalANY(dependency.any())
			}

			args = append(args, strDepends.any(), arg)
		}

		if check.Config != "" {
			args = append(args, strConfig.any(), source(check.Config).any())
		}

		for _, config := range check.ExtraConfigs {
			args = append(args, strConfig.any(), source(config).any())
		}

		for _, r := range sortedResourceGlobals(resources) {
			args = append(args, strGlobalResource.any(), r.Token.any())
		}

		for _, param := range check.ExtraParams {
			args = append(args, strExtraParam.any(), testArg(param))
		}

		for i := chunk.index; i < len(check.Files); i += max(chunk.split, 1) {
			args = append(args, check.Files[i].any())
		}

		return args
	}

	return s
}

func (s *TestSuite) mergeStyleFiles(files []VFS) {
	check := s.styleCheck

	check.Files = sortedUniqueVFS(append(check.Files, files...))
	s.relatedPaths = nil

	for _, file := range check.Files {
		s.relatedPaths = append(s.relatedPaths, file.any())
	}

	if check.Config != "" {
		s.relatedPaths = append(s.relatedPaths, source(check.Config).any())
	}

	for _, config := range check.ExtraConfigs {
		s.relatedPaths = append(s.relatedPaths, source(config).any())
	}

	s.relatedPaths = append(s.relatedPaths, source(check.Wrapper).any())

	if check.FileProcessingTime > 0 {
		s.split = max(1, int(math.Ceil(check.FileProcessingTime*float64(len(check.Files))/float64(s.timeout))))
	}
}
