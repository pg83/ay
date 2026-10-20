package main

import (
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
)

type TestRunOptions struct {
	BuildType   string
	Traceback   string
	Flags       []string
	Dist        bool
	Cache       bool
	Retries     int
	Sizes       []string
	TargetFlags map[string]string
	CommonEnv   EnvVars
}

func (mf *MakeFlags) testRunOptions() TestRunOptions {
	options := mf.testOptions

	options.Dist = mf.dist
	options.TargetFlags = mf.tflags

	flags := map[string]string{}

	copyStatsFlags(flags, mf.dflags)
	copyStatsFlags(flags, mf.tflags)

	if enabled, present := os.LookupEnv("YA_TEST_DISABLE_FLAKE8_MIGRATIONS"); !present || strIsTruthy(internStr(enabled)) {
		flags["DISABLE_FLAKE8_MIGRATIONS"] = "yes"
	}

	if mf.sandboxing {
		flags["SANDBOXING"] = "yes"
		flags["FAKEID"] += "sandboxing"
	}

	for key, value := range flags {
		switch key {
		case "RECURSE_PARTITION_INDEX", "RECURSE_PARTITIONS_COUNT", "CONSISTENT_DEBUG":
			continue
		}

		options.Flags = append(options.Flags, key+"="+value)
	}

	slices.Sort(options.Flags)

	if mf.testLevel > 0 {
		for _, entry := range os.Environ() {
			key, val, _ := strings.Cut(entry, "=")

			if key != "SVN_SSH" && !strings.HasPrefix(key, "YA_") {
				continue
			}

			skip := false

			for _, prefix := range []string{"YA_CACHE_DIR", "YA_TIMEOUT", "YA_TOKEN", "YA_SOURCE_ROOT"} {
				skip = skip || strings.HasPrefix(key, prefix)
			}

			if !skip {
				options.CommonEnv = append(options.CommonEnv, EnvVar{Name: internEnv(key), Value: internStr(val).any()})
			}
		}
	}

	return options
}

func (o TestRunOptions) descriptor(p *Platform) string {
	flags := make(map[string]string, len(o.TargetFlags)+2)

	for k, v := range o.TargetFlags {
		flags[k] = v
	}

	if p.Flags[envSANDBOXING] == strYes {
		flags["FAKEID"], flags["SANDBOXING"] = "sandboxing", "yes"
	}

	var tags []string

	for key, val := range flags {
		tag := key + "=" + val

		switch key {
		case "SANITIZER_TYPE":
			if val != "" {
				tag = val[:1] + "san"
			}
		case "USE_LTO", "USE_THINLTO", "MUSL", "USE_AFL", "RACE":
			if !strIsTruthy(internStr(val)) {
				continue
			}

			tag = map[string]string{"USE_LTO": "lto", "USE_THINLTO": "thinlto", "MUSL": "musl", "USE_AFL": "AFL", "RACE": "race"}[key]
		}

		tags = append(tags, tag)
	}

	slices.Sort(tags)

	return strings.Join(append([]string{string(p.Target), p.BuildType}, tags...), "-")
}

func testLoggingArgs(options TestRunOptions) []ANY {
	if options.Dist {
		return testCommandArgs([]string{"--log-level", "DEBUG", "--result-max-file-size", "102400", "--disable-memory-snippet", "--truncate-files", "--propagate-timeout-info"})
	}

	return testCommandArgs([]string{"--result-max-file-size", "0"})
}

func testEnvironment(p *Platform, info TestSuiteInfo) EnvVars {
	vars := slices.Clone(info.Options.CommonEnv)

	for _, entry := range testEnv(p, info.Name) {
		if info.Options.Dist && entry.Name == envTESTING_SAVE_OUTPUT {
			continue
		}

		replaced := false

		for i := range vars {
			if vars[i].Name == entry.Name {
				vars[i] = entry
				replaced = true

				break
			}
		}

		if !replaced {
			vars = append(vars, entry)
		}
	}

	return vars
}

func testRunDir(info TestSuiteInfo) string {
	dir := path.Join(info.ProjectPath, "test-results", info.Name)

	if info.Retry > 0 {
		dir = path.Join(dir, "run"+strconv.Itoa(info.Retry))
	}

	return dir
}

func testCommandLine(args []string) string {
	var out strings.Builder

	for i, arg := range args {
		if i != 0 {
			out.WriteByte(' ')
		}

		quoted := arg == "" || strings.ContainsAny(arg, " \t")

		if quoted {
			out.WriteByte('"')
		}

		slashes := 0

		for _, ch := range arg {
			if ch == '\\' {
				slashes++

				continue
			}

			if ch == '"' {
				out.WriteString(strings.Repeat("\\", slashes*2+1))
			} else {
				out.WriteString(strings.Repeat("\\", slashes))
			}

			slashes = 0
			out.WriteRune(ch)
		}

		if quoted {
			slashes *= 2
		}

		out.WriteString(strings.Repeat("\\", slashes))

		if quoted {
			out.WriteByte('"')
		}
	}

	return out.String()
}

func testNodeKind(size string) ProcKind {
	switch size {
	case "medium":
		return pkTM
	case "large":
		return pkTL
	}

	return pkTS
}

func testContextCommand(p *Platform) []ANY {
	args := []string{testYMakePython3, source(testAppendFileScriptRel).string(), testContextPath, "{", `  "build_type": ` + strconv.Quote(p.BuildType) + ",", `  "flags": {`}
	flags := map[string]string{"TESTS_REQUESTED": "yes"}

	for _, key := range []string{"AUTOCHECK", "USE_ARCADIA_PYTHON", "USE_SYSTEM_PYTHON"} {
		if value := p.Flags[internEnv(key)]; value != 0 {
			flags[key] = value.string()
		}
	}

	var keys []string

	for key := range flags {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	for i, key := range keys {
		line := "    " + strconv.Quote(key) + ": " + strconv.Quote(flags[key])

		if i+1 < len(keys) {
			line += ","
		}

		args = append(args, line)
	}

	args = append(args, "  }", "}")

	return testCommandArgs(args)
}
