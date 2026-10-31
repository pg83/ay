package main

import (
	"path"
	"strconv"
	"strings"
)

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

	limits := map[string]int{
		"NAME": 1, "WRAPPER_SCRIPT": 1, "FILES": -1, "DEPENDS": -1, "CONFIG_TYPE": -1,
		"FILE_PROCESSING_TIME": 1, "EXTRA_PARAMS": -1, "DEFAULT_CONFIGS": 1, "GLOBAL_RESOURCES": -1,
	}

	fields := map[string][]string{}
	key, limit := "", -1

	for _, arg := range args {
		value := arg.string()

		if n, ok := limits[value]; ok {
			key, limit = value, n

			continue
		}

		if limit == 0 {
			key, limit = "", -1
		}

		if key != "" {
			fields[key] = append(fields[key], value)
			limit--
		}
	}

	if len(fields["FILES"]) == 0 {
		return
	}

	if len(fields["NAME"]) == 0 || len(fields["WRAPPER_SCRIPT"]) == 0 {
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

	d.configurationDepends = append(d.configurationDepends, check.Depends...)
	d.styleChecks = append(d.styleChecks, check)
}
