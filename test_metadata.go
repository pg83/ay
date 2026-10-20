package main

import (
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
	case tokGBenchmark:
		return "g_benchmark"
	case tokFuzz:
		return "fuzz"
	}

	return ""
}

func applyTestMacro(v UnknownStmt, d *ModuleData) bool {
	switch v.Name {
	case tokSize, tokTimeout, tokTestCwd, tokData, tokDataFiles, tokExplicitData, tokDepends, tokUseRecipe, tokTag, tokEnv, tokRequirements, tokForkTests, tokForkSubtests, tokForkTestFiles, tokSplitFactor, tokBenchmarkOpts, tokFuzzOpts:
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

func testInnerCommand(info TestSuiteInfo, resultsDir string) []ANY {
	trace := build(path.Join(resultsDir, "ytest.report.trace")).string()
	out := build(path.Join(resultsDir, "testing_out_stuff")).string()
	args := []string{testToolHostPath}

	switch info.Type {
	case "g_benchmark":
		args = append(args, "run_g_benchmark", "--binary", info.BinaryPath, "--tracefile", trace, "--output-dir", out, "--project-path", info.ProjectPath, "--verbose", "--gdb-path", testGDBPath)

		for _, a := range info.BinaryArgs {
			args = append(args, "--test-binary-args="+a)
		}
	case "fuzz":
		args = append(args, "run_fuzz", "--binary", info.BinaryPath, "--tracefile", trace, "--modulo", "1", "--modulo-index", "0", "--output-dir", out, "--project-path", info.ProjectPath, "--source-root", testSourceRoot, "--verbose", "--gdb-path", testGDBPath)

		if len(info.BinaryArgs) > 0 {
			args = append(args, "--fuzz-opts="+strings.Join(info.BinaryArgs, " "))
		}
	default:
		partition := info.Partition

		if partition == "" {
			partition = "SEQUENTIAL"
		}

		args = append(args, "run_ut", "--binary", info.BinaryPath, "--trace-path", trace, "--output-dir", out, "--modulo", "1", "--modulo-index", "0", "--partition-mode", partition, "--project-path", info.ProjectPath, "--list-timeout", strconv.Itoa(info.Timeout/2), "--verbose", "--gdb-path", testGDBPath)

		if info.ForkMode == "subtests" {
			args = append(args, "--split-by-tests")
		}
	}

	return internAnys(args)
}
