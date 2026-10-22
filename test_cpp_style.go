package main

import (
	"path"
	"strings"
)

func (d *ModuleData) collectCPPStyle(stmt UnknownStmt, env Environment) {
	check := StyleCheck{Name: "clang_format", CPP: true, ConfigMap: "build/config/tests/cpp_style/default_configs.json", Wrapper: testClangFormatWrapperRel, Resource: "CLANG_FORMAT_RESOURCE_GLOBAL"}

	switch stmt.Name {
	case tokStyleCppUserSessions:
		check.Name = "clang_format_18_user_sessions"
		check.Depends = []string{"quality/user_sessions/tools/clang-format"}
		check.ExtraParams = []string{"custom_clang_format=quality/user_sessions/tools/clang-format", "custom_clang_format_bin=clang-format"}
	case tokStyleCppYt:
		check.Name = "clang_format_yt"
		check.Depends = []string{"ads/core/contrib/libs/clang16/tools/clang-format"}
		check.ExtraParams = []string{"custom_clang_format=ads/core/contrib/libs/clang16/tools/clang-format"}
	}

	if len(check.Depends) != 0 {
		check.Resource = ""
	}

	if value, ok := env.lookup("CPP_LINTERS_DEFAULT_CONFIGS"); ok && value != "" {
		check.ConfigMap = value
	}

	for i := 0; i < len(stmt.Args); i++ {
		if stmt.Args[i].string() == "CONFIG_TYPE" && i+1 < len(stmt.Args) {
			i++
			check.ConfigType = stmt.Args[i].string()
			check.ConfigDir, _ = env.lookup("MODULE_COMMON_CONFIGS_DIR")
		}
	}

	d.styleChecks = append(d.styleChecks, check)
}

func cppLinterSource(name string) bool {
	switch path.Ext(name) {
	case ".cpp", ".cxx", ".cc", ".c", ".C", ".h", ".H", ".hh", ".hpp", ".hxx", ".ipp":
		return true
	}

	return false
}

func (e *EmitContext) cppStyleInputFiles(file VFS) []string {
	seen := map[VFS]bool{}

	var files []string
	var visit func(VFS)

	visit = func(input VFS) {
		if seen[input] {
			return
		}

		seen[input] = true

		if input.isSource() {
			if cppLinterSource(input.relString()) {
				files = append(files, input.relString())
			}

			return
		}

		if info := e.codegen.lookup(input); info != nil {
			if info.SourcePath != 0 {
				visit(info.SourcePath)
			}

			for _, dependency := range info.BuildInputs {
				visit(dependency)
			}
		}
	}

	visit(file)

	return files
}

func (e *EmitContext) cppStyleSource(src ANY) ANY {
	if !e.ctx.testMode || !cppLinterSource(src.string()) {
		return src
	}

	var resolved VFS
	var files []string

	for i := range e.d.styleChecks {
		check := &e.d.styleChecks[i]

		if !check.CPP {
			continue
		}

		if resolved == 0 {
			resolved = e.moduleSourceVFS(src)
			files = e.cppStyleInputFiles(resolved)
		}

		check.Files = append(check.Files, files...)
	}

	if resolved != 0 {
		return resolved.any()
	}

	return src
}

func customClangFormatAllowed(project string) bool {
	return strings.HasPrefix(project, "adfox") || strings.HasPrefix(project, "ads") ||
		strings.HasPrefix(project, "alice/agents/booking") || strings.HasPrefix(project, "bigrt") ||
		strings.HasPrefix(project, "grut") || strings.HasPrefix(project, "quality/antifraud") ||
		strings.HasPrefix(project, "quality/user_sessions") || strings.HasPrefix(project, "yabs") ||
		strings.HasPrefix(project, "maps") || strings.HasPrefix(project, "yt")
}
