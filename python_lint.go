package main

import (
	"encoding/json"
	"slices"
	"strings"
)

type pythonLintState struct {
	bound     bool
	raw       string
	seed      string
	extra     []string
	analysis  bool
	disableF8 bool
	requested bool
}

func (s *pythonLintState) bind(env Environment) {
	s.bound = true
	s.raw = env.string(envNoLintValue)
	s.seed = env.string(envPyLinterFiles)
	s.extra = strings.Fields(env.string(envPyExtraLintFilesValue))
	s.analysis = env.string(envCppAnalysisMode) == "yes"
	s.disableF8 = env.string(envDisableFlake8) == "yes"
}

func (s *pythonLintState) value() string {
	if s.raw != "" && s.raw != "none" && s.raw != "none_internal" && s.raw != "ktlint" {
		throwFmt("Unsupported value for NO_LINT macro: %s", s.raw)
	}

	return s.raw
}

func (d *ModuleData) collectPythonLintFiles(module string, group int) {
	raw := d.pythonLint.value()

	if raw == "none" {
		allowed := []string{
			"contrib/", "devtools/", "junk/", "taxi/uservices/", "travel/",
			"market/report/lite/", "market/robotics/contrib/", "market/sre",
			"passport/backend/oauth/", "sdg/sdc/contrib/", "sdg/sdc/third_party/",
			"smart_devices/third_party/", "yt/yt/", "yt/python/", "yt/python_py2/",
			"cml/comrade_agent/src/contrib/",
		}

		if !slices.ContainsFunc(allowed, func(prefix string) bool { return strings.HasPrefix(module, prefix) }) {
			throwFmt("NO_LINT() is allowed only in %s", strings.Join(allowed, ", "))
		}
	}

	if raw != "none" && raw != "none_internal" {
		d.pySrcGroups[group].Lint = true
		d.pythonLint.requested = true
	}
}

func (e *EmitContext) pythonLinterFiles() []string {
	d := e.d

	var files []string

	for _, ps := range e.pySrcsReg {
		if ps.Kind == pySourcePlain && ps.Path.isSource() && d.pySrcGroups[ps.Group].Lint {
			files = append(files, ps.Path.relString())
		}
	}

	if d.pythonLint.requested {
		for _, stmt := range d.cythonCpp {
			if stmt.Lint && stmt.Generated == nil && extIsPy(stmt.Src) {
				v := resolveSourceOrGeneratedVFS(e.ctx, e.instance, stmt.Src, d.srcDirs)

				if v.isSource() {
					files = append(files, v.relString())
				}
			}
		}

		for _, extra := range d.pythonLint.extra {
			v := resolveSourceOrGeneratedVFS(e.ctx, e.instance, extra, d.srcDirs)
			_, rooted := moduleRootedVFS(e.instance.Path.relString(), extra)

			if v.isSource() && (rooted || e.ctx.fs.isFileVFS(v)) {
				files = append(files, v.relString())
			}
		}
	}

	if d.pythonLint.seed != "" && (len(files) > 0 || slices.ContainsFunc(d.styleChecks, func(check StyleCheck) bool { return check.Python })) {
		var seed []string

		if err := json.Unmarshal([]byte(d.pythonLint.seed), &seed); err != nil || seed == nil {
			throwFmt("Invalid PY_LINTER_FILES: expected a JSON list of source paths")
		}

		for _, file := range seed {
			if strings.HasPrefix(file, "$S/") {
				files = append(files, strings.TrimPrefix(file, "$S/"))
			} else if strings.HasPrefix(file, "${ARCADIA_ROOT}/") {
				files = append(files, strings.TrimPrefix(file, "${ARCADIA_ROOT}/"))
			}
		}

		if len(files) == 0 && slices.ContainsFunc(d.styleChecks, func(check StyleCheck) bool {
			return check.Python && (!d.pythonLint.disableF8 || check.Name != "flake8" && check.Name != "py2_flake8")
		}) {
			throwFmt("No files to lint for Python linter")
		}
	}

	return sortedUnique(files)
}
