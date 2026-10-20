package main

import (
	"path"
	"slices"
	"strings"
)

type StyleCheck struct {
	Name      string
	ConfigMap string
	Config    string
	Wrapper   string
	Resource  string
	Files     []string
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
		peer, configVar = "build/external_resources/yamlfmt", "YAML_LINTER_DEFAULT_CONFIGS"
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

func (ctx *GenCtx) emitModuleTests(dir string, result *ModuleEmitResult) {
	if !ctx.testMode {
		return
	}

	if result.testSuiteInfo != nil {
		for _, ref := range ctx.emitTestRunNodes(*result.testSuiteInfo, result.LDRef, result.ResourceGlobalClosure) {
			ctx.emit.result(ref)
		}
	}

	for _, check := range result.styleChecks {
		config, ok := ctx.parsers.notsJSON(check.ConfigMap)[check.Name].(string)

		if !ok {
			throwFmt("STYLE: missing %s config in %s", check.Name, check.ConfigMap)
		}

		check.Config = config

		resource := ""

		for _, r := range sortedResourceGlobals(result.ResourceGlobalClosure) {
			if r.GlobalVar.string() == check.Resource {
				resource = r.Token.string()
				break
			}
		}

		node := buildStyleNode(ctx.na, ctx.target, TestSuiteInfo{ProjectPath: dir}, check, resource)

		node.DepRefs = []NodeRef{ctx.emit.emit(buildTestCtxNode(ctx.na, ctx.target))}
		ctx.emit.result(ctx.emit.emit(node))
	}
}
