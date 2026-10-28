package main

import (
	"cmp"
	"encoding/base64"
	"path"
	"slices"
	"strconv"
	"strings"
)

func testSuiteName(name TOK) string {
	if isUnitTestModule(name) {
		if name == tokGtest {
			return "gtest"
		}

		return "unittest"
	}

	switch name {
	case tokExectest:
		return "exectest"
	case tokPy3Test:
		return "py3test"
	case tokPy2Test, tokPytestBin:
		return "pytest"
	case tokGBenchmark:
		return "g_benchmark"
	case tokYBenchmark:
		return "benchmark"
	case tokFuzz:
		return "fuzz"
	case tokBoosttest, tokBoosttestWithMain:
		return "boost_test"
	}

	return ""
}

func testExecDependencyDir(dep string) string {
	dir := path.Dir(dep)

	if dir == "." {
		dir = ""
	}

	return testBuildRoot + "/" + dir
}

func expandTestEnvValue(value string, env Environment) string {
	var kinds [len(dontExpandVars)]EnvKind

	for i, id := range dontExpandVars {
		kinds[i] = env.unbind(id)
	}

	value = expandConfigMacroBody(value, env)

	for i, id := range dontExpandVars {
		env.rebind(id, kinds[i])
	}

	return value
}

func applyTestMacro(v UnknownStmt, d *ModuleData, env Environment) bool {
	switch v.Name {
	case tokDockerImage, tokRun, tokSize, tokTimeout, tokTestCwd, tokData, tokDataFiles, tokExplicitData, tokDepends, tokUseRecipe, tokTag, tokEnv, tokNoDoctests, tokConftestLoadPolicyLocal, tokConftestLoadPolicyLegacyGlobal, tokRequirements, tokForkTests, tokForkSubtests, tokForkTestFiles, tokSplitFactor, tokBenchmarkOpts, tokFuzzOpts, tokFuzzDicts:
	default:
		return false
	}

	t := &d.test
	args := anyStrs(v.Args)
	value := strings.Join(args, " ")

	switch v.Name {
	case tokDockerImage:
		t.dockerImages = append(t.dockerImages, args...)
	case tokRun:
		t.commands += "\n" + testCommandLine(args)
	case tokSize:
		t.size = strings.ToLower(value)
	case tokTimeout:
		t.timeout = throw2(strconv.Atoi(value))
	case tokTestCwd:
		t.cwd = value
	case tokExplicitData:
		t.explicitData = true
	case tokData:

		for i := 0; i < len(args); i++ {
			if args[i] == "AUTOUPDATED" {
				i++

				continue
			}

			t.data = append(t.data, args[i])
		}
	case tokDataFiles:
		for _, a := range args {
			t.data = append(t.data, testDataFile(a))
		}
	case tokDepends:

		for _, arg := range v.Args {
			t.depends = append(t.depends, arg.relOrSelf().string())
		}
	case tokUseRecipe:
		t.recipes = append(t.recipes, value)
	case tokTag:
		t.tags = append(t.tags, args...)
	case tokEnv, tokNoDoctests, tokConftestLoadPolicyLocal, tokConftestLoadPolicyLegacyGlobal:
		if v.Name == tokEnv {
			value = expandTestEnvValue(strings.Join(anyStrs(expandSourceArguments(v.Args, env)), " "), env)
		} else if v.Name == tokNoDoctests {
			value = "YA_PYTEST_DISABLE_DOCTEST=yes"
		} else if v.Name == tokConftestLoadPolicyLocal {
			value = "CONFTEST_LOAD_POLICY=LOCAL"
		} else {
			value = "CONFTEST_LOAD_POLICY=LEGACY_GLOBAL"
		}

		quoted := `"` + value + `"`

		appendMacroEnv(env, "TEST_ENV_VALUE", expandSourceScalarVarRef(quoted, env))
		appendMacroEnv(env, "_TEST_ENV_SEM", expandSourceScalarVarRef("&& test_env "+value, env))
	case tokForkTests:
		t.forkMode = "tests"
		t.partition = strings.ToUpper(value)
	case tokForkSubtests:
		t.forkMode = "subtests"
		t.partition = strings.ToUpper(value)
	case tokForkTestFiles:
		t.forkFiles = true
	case tokSplitFactor:
		t.splitFactor = throw2(strconv.Atoi(value))
	case tokBenchmarkOpts, tokFuzzOpts:
		t.binaryArgs = append(t.binaryArgs, args...)
	case tokFuzzDicts:
		t.fuzzDicts = append(t.fuzzDicts, args...)
	case tokRequirements:
		t.requirements = append(t.requirements, args...)
	}

	return true
}

func testDataFile(file string) string {
	return "arcadia/" + strings.TrimPrefix(strings.TrimPrefix(file, "$(S)/"), "${ARCADIA_ROOT}/")
}

func normalizedTestData(data []string) []string {
	values := slices.Clone(data)

	for i, value := range values {
		values[i] = strings.TrimRight(value, `/\`)
	}

	return values
}

func (t *TestMeta) expandValues(env Environment) {
	words := splitShellWords(expandTestEnvValue(env.string(envTestEnvValue), env))

	if len(words) > 0 {
		t.env = append(t.env, strings.Split(strings.Trim(strings.Join(words, ";"), `"`), ";")...)
	}

	for i, value := range t.recipes {
		t.recipes[i] = expandConfigMacroBody(value, env)
	}
}

func collectTestRecipe(args []ANY, env Environment) string {
	var values []string

	for _, arg := range args {
		words := []string{arg.string()}

		if strings.ContainsRune(words[0], '$') {
			words = strings.Split(expandResourceArgument(words[0], env), " ")
			words = slices.DeleteFunc(words, func(word string) bool { return word == "" })
		}

		for _, word := range words {
			if word == "&&" || word == ";" {
				word = `${quo:"` + word + `"}`
			}

			values = append(values, word)
		}
	}

	value := expandConfigMacroBody(strings.Join(values, " "), env)
	words := strings.Split(value, " ")

	value = strings.Join(slices.DeleteFunc(words, func(word string) bool { return word == "" }), " ")

	return expandResourceArgument(value, env)
}

func expandConfigMacroBody(value string, env Environment) string {
	if !strings.ContainsRune(value, '$') {
		return value
	}

	return continueConfigMacroBody(value, env, 0, nil)
}

func continueConfigMacroBody(value string, env Environment, iteration int, parameters map[string][]string) string {
	return continueConfigMacroBodyTracked(value, env, iteration, parameters, nil)
}

func continueConfigMacroBodyTracked(value string, env Environment, iteration int, parameters map[string][]string, track *SourceTextTracker) string {
	for ; ; iteration++ {
		delta := 0

		expanded := replaceConfigMacroVariables(value, func(start, end int) string {
			reference := value[start:end]

			if reference[0] == '\\' || strings.HasPrefix(reference, `${"`) {
				return reference
			}

			name := strings.TrimSuffix(strings.TrimPrefix(reference[1:], "{"), "}")

			if values, ok := parameters[name]; ok {
				return strings.Join(values, " ")
			}

			if replacement, ok := env.lookup(name); ok {
				originalSize := len(replacement)

				replacement = strings.TrimPrefix(replacement, " ")

				if track != nil {
					ranges := sliceSourceTextRanges(env.s.textInputs[internedEnv(name)], originalSize-len(replacement), originalSize)

					track.replace(start+delta, end+delta, len(replacement), ranges)
					delta += len(replacement) - len(reference)
				}

				return replacement
			}

			return reference
		})

		if iteration == 127 {
			throwFmt("Macro subst exceeded the limit 128")
		}

		if expanded == value || expanded == "" {
			return expanded
		}

		value = expanded
	}
}

func testInnerCommand(s *TestSuite, meta TestMeta, binary, work string, chunk TestChunk, opts *TestOpts) []ANY {
	trace := work + "/ytest.report.trace"
	out := work + "/testing_out_stuff"
	args := []string{testToolHostPath}

	switch s.testType {
	case "py3test", "pytest", "exectest":
		command := []string{binary}

		if s.testType == "exectest" {
			command = []string{"--test-param", "commands=" + base64.StdEncoding.EncodeToString([]byte(meta.commands)), testToolHostPath, "run_exectest"}
		}

		command = append(command, "--basetemp", "$(B)/tmp", "--capture", "no", "-c", "pkg:library.python.pytest:pytest.yatest.ini", "-p", "no:factor", "--doctest-modules", "--ya-trace", trace, "--build-root", testBuildRoot, "--source-root", testSourceRoot, "--output-dir", out, "--durations", "0", "--project-path", s.projectPath, "--test-tool-bin", testToolHostPath, "--ya-version", "2")

		if !opts.dist {
			command = append(command, "--collect-cores")
		}

		buildType := opts.buildType

		command = append(command, "--build-type", buildType)

		if opts.traceback != "" {
			command = append(command, "--tb", opts.traceback)
		}

		if chunk.split > 1 {
			command = append(command, "--modulo", strconv.Itoa(chunk.split), "--modulo-index", strconv.Itoa(chunk.index), "--partition-mode", cmp.Or(meta.partition, "SEQUENTIAL"))
		}

		if meta.forkMode == "subtests" {
			command = append(command, "--split-by-tests")
		}

		for _, directory := range s.pytestOutputDirs {
			command = append(command, "--dep-root", directory)
		}

		for _, flag := range opts.flags {
			command = append(command, "--flags", flag)
		}

		for _, flag := range opts.flags {
			if sanitizer, found := strings.CutPrefix(flag, "SANITIZER_TYPE="); found && sanitizer != "" {
				command = append(command, "--sanitize", sanitizer)

				break
			}
		}

		if chunk.file != "" {
			command = append(command, "--test-file-filter", chunk.file)
		}

		if s.testType == "exectest" {
			command = append(command, "--noconftest")
		}

		return testArgs(command)

	case "g_benchmark", "benchmark":
		runner := "run_g_benchmark"

		if s.testType == "benchmark" {
			runner = "run_y_benchmark"
		}

		args = append(args, runner, "--binary", binary, "--tracefile", trace, "--output-dir", out, "--project-path", s.projectPath, "--verbose", "--gdb-path", testGDBPath)

		if opts.dist {
			args = append(args, "--truncate-logs")
		}

		for _, a := range meta.binaryArgs {
			args = append(args, "--test-binary-args="+a)
		}
	case "boost_test":
		args = append(args, "run_boost_test", "--binary", binary, "--tracefile", trace, "--output-dir", out, "--project-path", s.projectPath, "--verbose", "--gdb-path", testGDBPath)
	case "fuzz":
		args = append(args, "run_fuzz", "--binary", binary, "--tracefile", trace, "--modulo", strconv.Itoa(max(chunk.split, 1)), "--modulo-index", strconv.Itoa(chunk.index), "--output-dir", out, "--project-path", s.projectPath, "--source-root", testSourceRoot, "--verbose", "--gdb-path", testGDBPath)

		if opts.dist {
			args = append(args, "--truncate-logs")
		}

		if len(meta.binaryArgs) > 0 {
			args = append(args, "--fuzz-opts="+strings.Join(meta.binaryArgs, " "))
		}

		if s.corpusPartsExceeded > 0 {
			args = append(args, "--corpus-parts-limit-exceeded", strconv.Itoa(s.corpusPartsExceeded))
		}
	default:
		partition := meta.partition

		if partition == "" {
			partition = "SEQUENTIAL"
		}

		args = append(args, "run_ut", "--binary", binary, "--trace-path", trace, "--output-dir", out, "--modulo", strconv.Itoa(max(chunk.split, 1)), "--modulo-index", strconv.Itoa(chunk.index), "--partition-mode", partition, "--project-path", s.projectPath, "--list-timeout", strconv.Itoa(s.timeout/2), "--verbose", "--gdb-path", testGDBPath)

		if opts.dist {
			args = append(args, "--truncate-logs")
		}

		if meta.forkMode == "subtests" {
			args = append(args, "--split-by-tests")
		}
	}

	return testArgs(args)
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
