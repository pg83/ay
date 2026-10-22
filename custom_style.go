package main

import (
	"crypto/md5"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const customExplicitLinter = "_ADD_CUSTOM_EXPLICIT_LINTER_CHECK"

type ConfStyleMacro struct {
	file   string
	params []string
	body   string
	once   sync.Once
	stmts  []Stmt
}

func (f *ConfFile) addStyleMacro(header string, lines []string) {
	if !strings.Contains(strings.Join(lines, "\n"), customExplicitLinter) {
		return
	}

	name, params, _ := strings.Cut(strings.TrimPrefix(header, "macro "), "(")
	macro := &ConfStyleMacro{file: f.rel, params: strings.Fields(strings.ReplaceAll(strings.TrimSuffix(params, ")"), ",", " "))}

	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), ".") {
			macro.body += line + "\n"
		}
	}

	if f.styleMacros == nil {
		f.styleMacros = map[string]*ConfStyleMacro{}
	}

	f.styleMacros[name] = macro
}

func applyCustomStyle(fs FS, module string, stmt UnknownStmt, d *ModuleData, env Environment, platform *Platform) bool {
	if stmt.Name != tokInvalid {
		return false
	}

	name := stmt.Raw.string()

	if name == customExplicitLinter {
		d.collectCustomStyle(module, stmt.Args, env)

		return true
	}

	macro := platform.customStyleMacros[name]

	if macro == nil {
		return false
	}

	macro.once.Do(func() { macro.stmts = parseInternal(fs, macro.file, []byte(macro.body)).Stmts })

	local := env.clone()

	local.setString(envARCADIA_ROOT, "${ARCADIA_ROOT}")

	for i, param := range macro.params {
		value := ""

		if i < len(stmt.Args) {
			value = stmt.Args[i].string()
		}

		local.setString(internEnv(param), value)
	}

	executeStyleMacro(fs, module, macro.stmts, d, local)

	return true
}

func executeStyleMacro(fs FS, module string, stmts []Stmt, d *ModuleData, env Environment) {
	for _, stmt := range stmts {
		switch v := stmt.(type) {
		case *SetStmt:
			env.setFromString(v.NameEnv, expandStyleValue(v.Value, env))
		case *IfStmt:
			body := v.Then

			if !evalCond(v.Cond, env) {
				body = v.Else
			}

			executeStyleMacro(fs, module, body, d, env)
		case *UnknownStmt:
			var args []ANY

			for _, arg := range v.Args {
				args = append(args, internAnys(strings.Fields(expandStyleValue(arg.string(), env)))...)
			}

			switch v.Raw.string() {
			case "_GLOB":
				var files []string

				for _, pattern := range args[1:] {
					for _, file := range ymakeGlob(fs, styleSourcePath(module, pattern.string())) {
						files = append(files, "${ARCADIA_ROOT}/"+file)
					}
				}

				env.setString(internEnv(args[0].string()), strings.Join(sortedUnique(files), " "))
			case customExplicitLinter:
				d.collectCustomStyle(module, args, env)
			default:
				throwFmt("custom lint: unsupported configuration statement %s", v.Raw.string())
			}
		default:
			throwFmt("custom lint: unsupported configuration statement %T", stmt)
		}
	}
}

func expandStyleValue(value string, env Environment) string {
	var out strings.Builder

	for {
		start := strings.Index(value, "${")

		if start < 0 {
			out.WriteString(value)

			return out.String()
		}

		out.WriteString(value[:start])

		end, depth := start+2, 1

		for depth != 0 && end < len(value) {
			switch value[end] {
			case '{':
				depth++
			case '}':
				depth--
			}

			end++
		}

		if depth != 0 {
			throwFmt("custom lint: unclosed variable %s", value)
		}

		name := expandStyleValue(value[start+2:end-1], env)
		hash := strings.HasPrefix(name, "hash:")

		name = strings.TrimPrefix(name, "hash:")

		replacement, _ := env.lookup(name)

		if hash {
			replacement = fmt.Sprintf("%x", md5.Sum([]byte(replacement)))
		}

		out.WriteString(replacement)
		value = value[end:]
	}
}

func styleSourcePath(module, file string) string {
	for _, prefix := range []string{"${ARCADIA_ROOT}/", "$(S)/", "$S/"} {
		if rel, ok := strings.CutPrefix(file, prefix); ok {
			return rel
		}
	}

	return path.Join(module, file)
}

func (d *ModuleData) collectCustomStyle(module string, args []ANY, env Environment) {
	noLint, _ := env.lookup("_NO_LINT_VALUE")

	if d.noLint == "none" || d.noLint == "none_internal" || noLint == "none" || noLint == "none_internal" || env.string(envTidy) == "yes" {
		return
	}

	keywords := []string{"NAME", "WRAPPER_SCRIPT", "FILES", "DEPENDS", "CONFIG_TYPE", "FILE_PROCESSING_TIME", "EXTRA_PARAMS", "DEFAULT_CONFIGS", "GLOBAL_RESOURCES"}
	fields := map[string][]string{}
	key := ""

	for _, arg := range args {
		value := arg.string()

		if slices.Contains(keywords, value) {
			key = value
		} else {
			fields[key] = append(fields[key], value)
		}
	}

	if len(fields["FILES"]) == 0 {
		return
	}

	if len(fields["NAME"]) != 1 || len(fields["WRAPPER_SCRIPT"]) != 1 {
		throwFmt("custom lint: NAME and WRAPPER_SCRIPT require one value each")
	}

	check := StyleCheck{Name: fields["NAME"][0], Wrapper: fields["WRAPPER_SCRIPT"][0], Depends: fields["DEPENDS"], ExtraParams: fields["EXTRA_PARAMS"]}

	for _, file := range fields["FILES"] {
		if strings.HasSuffix(file, "ya.make") {
			throwFmt("custom lint: can't have ya.make in collected files")
		}

		if strings.HasPrefix(file, "$(B)/") || strings.HasPrefix(file, "$B/") {
			continue
		}

		check.Files = append(check.Files, styleSourcePath(module, file))
	}

	for _, param := range check.ExtraParams {
		if !strings.Contains(param, "=") {
			throwFmt("custom lint: extra param requires key=value: %s", param)
		}
	}

	check.Files = sortedUnique(check.Files)
	check.ConfigMap = strings.Join(fields["DEFAULT_CONFIGS"], "")
	check.ConfigType = strings.Join(fields["CONFIG_TYPE"], "")
	check.ConfigDir, _ = env.lookup("MODULE_COMMON_CONFIGS_DIR")

	if values := fields["FILE_PROCESSING_TIME"]; len(values) != 0 {
		check.FileProcessingTime = throw2(strconv.ParseFloat(values[0], 64))
	}

	d.styleChecks = append(d.styleChecks, check)
}
