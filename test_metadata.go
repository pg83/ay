package main

import (
	"cmp"
	"encoding/base64"
	"path"
	"slices"
	"strconv"
	"strings"
)

func testSuiteName(name TOK) STR {
	if isUnitTestModule(name) {
		if name == tokGtest {
			return strGtest
		}

		return strUnittest
	}

	switch name {
	case tokExectest:
		return strExectest
	case tokPy3Test:
		return strPy3test
	case tokPy2Test, tokPytestBin:
		return strPytest
	case tokGBenchmark:
		return strGBenchmark
	case tokYBenchmark:
		return strBenchmark
	case tokFuzz:
		return strFuzz
	case tokBoosttest, tokBoosttestWithMain:
		return strBoostTest
	}

	return 0
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
			words = strings.Split(expandMakeExpression(words[0], env, keepDeferredVar), " ")
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

	return expandMakeExpression(value, env, keepDeferredVar)
}

func expandConfigMacroBody(value string, env Environment) string {
	if !strings.ContainsRune(value, '$') {
		return value
	}

	return continueConfigMacroBody(value, env, nil)
}

func continueConfigMacroBody(value string, env Environment, parameters map[string][]string) string {
	return continueConfigMacroBodyTracked(value, env, parameters, nil)
}

func continueConfigMacroBodyTracked(value string, env Environment, parameters map[string][]string, track *SourceTextTracker) string {
	return substMacroDeeply(value, bindingsOf(parameters), env, nil, &substPolicy{keepModifiers: true, track: track})
}

func testInnerCommand(s *TestSuite, meta TestMeta, binary VFS, work string, chunk TestChunk, opts *TestOpts) []ANY {
	trace := testArg(work + "/ytest.report.trace")
	out := testArg(work + "/testing_out_stuff")
	args := []ANY{testToolHostPathArg}

	switch s.testType {
	case strPy3test, strPytest, strExectest:
		command := []ANY{binary.any()}

		if s.testType == strExectest {
			command = []ANY{strTestParam.any(), testArg("commands=" + base64.StdEncoding.EncodeToString([]byte(meta.commands))), testToolHostPathArg, strRunExectest.any()}
		}

		command = append(command, strBasetemp.any(), testBasetempArg, strCapture.any(), strNo.any(), strC2.any(), strPkgLibraryPythonPytestPytestYatestIni.any(), strP2.any(), strNoFactor.any(), strDoctestModules.any(), strYaTrace.any(), trace, strBuildRoot2.any(), testBuildRootArg, strSourceRoot2.any(), testSourceRootArg, strOutputDir.any(), out, strDurations.any(), strZero.any(), strProjectPath.any(), s.projectPath.any(), strTestToolBin.any(), testToolHostPathArg, strYaVersion.any(), str22.any())

		if !opts.dist {
			command = append(command, strCollectCores.any())
		}

		buildType := opts.buildType

		command = append(command, strBuildType.any(), testArg(buildType))

		if opts.traceback != "" {
			command = append(command, strTb.any(), testArg(opts.traceback))
		}

		if chunk.split > 1 {
			command = append(command, strModulo.any(), intArg(chunk.split), strModuloIndex.any(), intArg(chunk.index), strPartitionMode.any(), testArg(cmp.Or(meta.partition, "SEQUENTIAL")))
		}

		if meta.forkMode == "subtests" {
			command = append(command, strSplitByTests.any())
		}

		project := s.projectPath.string()

		for _, directory := range s.pytestOutputDirs {
			arg := s.projectPath.any()

			if directory != project {
				arg = testArg(directory)
			}

			command = append(command, strDepRoot.any(), arg)
		}

		for _, flag := range opts.flags {
			command = append(command, strFlags.any(), testArg(flag))
		}

		for _, flag := range opts.flags {
			if sanitizer, found := strings.CutPrefix(flag, "SANITIZER_TYPE="); found && sanitizer != "" {
				command = append(command, strSanitize.any(), testArg(sanitizer))

				break
			}
		}

		if chunk.file != "" {
			command = append(command, strTestFileFilter.any(), testArg(chunk.file))
		}

		if s.testType == strExectest {
			command = append(command, strNoconftest.any())
		}

		return command

	case strGBenchmark, strBenchmark:
		runner := strRunGBenchmark.any()

		if s.testType == strBenchmark {
			runner = strRunYBenchmark.any()
		}

		args = append(args, runner, strBinary.any(), binary.any(), strTracefile.any(), trace, strOutputDir.any(), out, strProjectPath.any(), s.projectPath.any(), strVerbose.any(), strGdbPath.any(), testGDBPathArg)

		if opts.dist {
			args = append(args, strTruncateLogs.any())
		}

		for _, a := range meta.binaryArgs {
			args = append(args, testArg("--test-binary-args="+a))
		}
	case strBoostTest:
		args = append(args, strRunBoostTest.any(), strBinary.any(), binary.any(), strTracefile.any(), trace, strOutputDir.any(), out, strProjectPath.any(), s.projectPath.any(), strVerbose.any(), strGdbPath.any(), testGDBPathArg)
	case strFuzz:
		args = append(args, strRunFuzz.any(), strBinary.any(), binary.any(), strTracefile.any(), trace, strModulo.any(), intArg(max(chunk.split, 1)), strModuloIndex.any(), intArg(chunk.index), strOutputDir.any(), out, strProjectPath.any(), s.projectPath.any(), strSourceRoot2.any(), testSourceRootArg, strVerbose.any(), strGdbPath.any(), testGDBPathArg)

		if opts.dist {
			args = append(args, strTruncateLogs.any())
		}

		if len(meta.binaryArgs) > 0 {
			args = append(args, testArg("--fuzz-opts="+strings.Join(meta.binaryArgs, " ")))
		}

		if s.corpusPartsExceeded > 0 {
			args = append(args, strCorpusPartsLimitExceeded.any(), intArg(s.corpusPartsExceeded))
		}
	default:
		partition := meta.partition

		if partition == "" {
			partition = "SEQUENTIAL"
		}

		args = append(args, strRunUt.any(), strBinary.any(), binary.any(), strTracePath.any(), trace, strOutputDir.any(), out, strModulo.any(), intArg(max(chunk.split, 1)), strModuloIndex.any(), intArg(chunk.index), strPartitionMode.any(), testArg(partition), strProjectPath.any(), s.projectPath.any(), strListTimeout.any(), intArg(s.timeout/2), strVerbose.any(), strGdbPath.any(), testGDBPathArg)

		if opts.dist {
			args = append(args, strTruncateLogs.any())
		}

		if meta.forkMode == "subtests" {
			args = append(args, strSplitByTests.any())
		}
	}

	return args
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
