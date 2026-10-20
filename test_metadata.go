package main

import (
	"encoding/base64"
	"path"
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
	case tokPy2Test:
		return "pytest"
	case tokGBenchmark:
		return "g_benchmark"
	case tokFuzz:
		return "fuzz"
	}

	return ""
}

func applyTestMacro(v UnknownStmt, d *ModuleData) bool {
	switch v.Name {
	case tokRun, tokSize, tokTimeout, tokTestCwd, tokData, tokDataFiles, tokExplicitData, tokDepends, tokUseRecipe, tokTag, tokEnv, tokRequirements, tokForkTests, tokForkSubtests, tokForkTestFiles, tokSplitFactor, tokBenchmarkOpts, tokFuzzOpts:
	default:
		return false
	}

	if d.test == nil {
		d.test = &TestSuiteInfo{Size: "small", Requirements: testRunRequirements}
	}

	t := d.test
	args := anyStrs(v.Args)
	value := strings.Join(args, " ")

	switch v.Name {
	case tokRun:
		t.Commands += "\n" + strings.Join(args, " ")
	case tokSize:
		t.Size = strings.ToLower(value)
	case tokTimeout:
		t.Timeout = throw2(strconv.Atoi(value))
	case tokTestCwd:
		t.Cwd = value
	case tokExplicitData:
		t.ExplicitData = true
	case tokData:
		for i := 0; i < len(args); i++ {
			if args[i] == "AUTOUPDATED" {
				i++

				continue
			}

			t.Data = append(t.Data, args[i])
		}
	case tokDataFiles:
		for _, a := range args {
			t.Data = append(t.Data, "arcadia/"+a)
		}
	case tokDepends:
		t.Depends = append(t.Depends, args...)
	case tokUseRecipe:
		t.Recipes = append(t.Recipes, value)
	case tokTag:
		t.Tags = append(t.Tags, args...)
	case tokEnv:
		t.Env = append(t.Env, args...)
	case tokForkTests:
		t.ForkMode = "tests"
		t.Partition = strings.ToUpper(value)
	case tokForkSubtests:
		t.ForkMode = "subtests"
		t.Partition = strings.ToUpper(value)
	case tokForkTestFiles:
		t.ForkFiles = true
	case tokSplitFactor:
		t.SplitFactor = throw2(strconv.Atoi(value))
	case tokBenchmarkOpts, tokFuzzOpts:
		t.BinaryArgs = append(t.BinaryArgs, args...)
	case tokRequirements:
		for _, a := range args {
			key, val, _ := strings.Cut(a, ":")

			switch key {
			case "cpu":
				t.Requirements.CPU = throw2(strconv.ParseFloat(val, 64))
			case "ram":
				t.Requirements.RAM = throw2(strconv.ParseFloat(val, 64))
			case "ram_disk":
				t.Requirements.RAMDisk = throw2(strconv.ParseFloat(val, 64))
			case "network":
				if val == "full" {
					t.Requirements.Network = nwFull
				}
			}
		}
	}

	return true
}

func testInnerCommand(info TestSuiteInfo, resultsDir string, p *Platform) []ANY {
	trace := build(path.Join(resultsDir, "ytest.report.trace")).string()
	out := build(path.Join(resultsDir, "testing_out_stuff")).string()
	args := []string{testToolHostPath}

	switch info.Type {
	case "py3test", "pytest", "exectest":
		command := []string{info.BinaryPath}

		if info.Type == "exectest" {
			command = []string{"--test-param", "commands=" + base64.StdEncoding.EncodeToString([]byte(info.Commands)), testToolHostPath, "run_exectest"}
		}

		command = append(command, "--basetemp", "$(B)/tmp", "--capture", "no", "-c", "pkg:library.python.pytest:pytest.yatest.ini", "-p", "no:factor", "--doctest-modules", "--ya-trace", trace, "--build-root", testBuildRoot, "--source-root", testSourceRoot, "--output-dir", out, "--durations", "0", "--project-path", info.ProjectPath, "--test-tool-bin", testToolHostPath, "--ya-version", "2", "--collect-cores", "--build-type", p.BuildType)

		if info.SplitFactor > 1 {
			command = append(command, "--modulo", strconv.Itoa(info.SplitFactor), "--modulo-index", strconv.Itoa(info.SplitIndex), "--partition-mode", testPartition(info))
		}

		if info.ForkMode == "subtests" {
			command = append(command, "--split-by-tests")
		}

		command = append(command, "--dep-root", "$(B)/"+info.ProjectPath)

		if info.SplitFile != "" {
			command = append(command, "--test-file-filter", info.SplitFile)
		}

		if info.Type == "exectest" {
			command = append(command, "--noconftest")
		}

		return testCommandArgs(command)

	case "g_benchmark":
		args = append(args, "run_g_benchmark", "--binary", info.BinaryPath, "--tracefile", trace, "--output-dir", out, "--project-path", info.ProjectPath, "--verbose", "--gdb-path", testGDBPath)

		for _, a := range info.BinaryArgs {
			args = append(args, "--test-binary-args="+a)
		}
	case "fuzz":
		args = append(args, "run_fuzz", "--binary", info.BinaryPath, "--tracefile", trace, "--modulo", strconv.Itoa(max(info.SplitFactor, 1)), "--modulo-index", strconv.Itoa(info.SplitIndex), "--output-dir", out, "--project-path", info.ProjectPath, "--source-root", testSourceRoot, "--verbose", "--gdb-path", testGDBPath)

		if len(info.BinaryArgs) > 0 {
			args = append(args, "--fuzz-opts="+strings.Join(info.BinaryArgs, " "))
		}
	default:
		partition := info.Partition

		if partition == "" {
			partition = "SEQUENTIAL"
		}

		args = append(args, "run_ut", "--binary", info.BinaryPath, "--trace-path", trace, "--output-dir", out, "--modulo", strconv.Itoa(max(info.SplitFactor, 1)), "--modulo-index", strconv.Itoa(info.SplitIndex), "--partition-mode", partition, "--project-path", info.ProjectPath, "--list-timeout", strconv.Itoa(info.Timeout/2), "--verbose", "--gdb-path", testGDBPath)

		if info.ForkMode == "subtests" {
			args = append(args, "--split-by-tests")
		}
	}

	return testCommandArgs(args)
}

func testCommandArgs(args []string) []ANY {
	out := make([]ANY, len(args))

	for i, arg := range args {
		out[i] = internStr(arg).any()
	}

	return out
}

func testPartition(info TestSuiteInfo) string {
	if info.Partition == "" {
		return "SEQUENTIAL"
	}

	return info.Partition
}
