package main

import (
	"slices"
	"testing"
)

func TestTestRequirements(t *testing.T) {
	for _, tc := range []struct {
		ram string
		cpu float64
	}{
		{"2", 1}, {"4", 2}, {"8", 3}, {"12", 4},
	} {
		r := testRequirements(&TestSuite{requirements: map[string]string{"ram": tc.ram}})

		testEqual(t, r.CPU, tc.cpu)
	}

	r := testRequirements(&TestSuite{size: "large", requirements: map[string]string{"cpu": "6", "ram": "16", "ram_disk": "2", "network": "restricted"}})
	testEqual(t, r.CPU, 6.0)
	testEqual(t, r.RAM, 16.0)
	testEqual(t, r.RAMDisk, 2.0)
	testEqual(t, r.Network, nwRestricted)
	testEqual(t, testRequirements(&TestSuite{requirements: map[string]string{"network": "full"}}).Network, nwFull)
}

func TestTestOptionOverrides(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "test")
	t.Setenv("YA_AUTO_DETECT_AGENT", "yes")
	t.Setenv("YA_DIR_OUTPUTS", "no")
	mf := &MakeFlags{testLevel: 1, buildType: "release", testBuildType: "release"}
	opts := newTestOpts(mf, map[string]any{"dir_outputs": true, "default_node_requirements": `{"cpu":2,"ram":3}`})

	testEqual(t, opts.outputStyle, testOutputStyleJsonl)
	testEqual(t, opts.dirOutputs, false)
	testEqual(t, opts.contextReqs.CPU, 2.0)
	testEqual(t, opts.contextReqs.RAM, 3.0)
	testEqual(t, newTestOpts(mf, map[string]any{"output_style": "custom"}).outputStyle, "custom")
	testThrows(t, "invalid character", func() { newTestOpts(mf, map[string]any{"default_node_requirements": "invalid"}) })
}

func TestPytestRuntimeArguments(t *testing.T) {
	s := &TestSuite{projectPath: "app/tests", testType: "py3test"}
	opts := &TestOpts{dist: true, buildType: "release", traceback: "short", flags: []string{"SANITIZER_TYPE=address"}}
	meta := TestMeta{forkMode: "subtests"}
	args := anyStrs(testInnerCommand(s, meta, "$(B)/app/tests/bin", "$(B)/work", TestChunk{split: 3, index: 1, file: "unit.py"}, opts))

	for _, word := range []string{"--tb", "short", "--split-by-tests", "--sanitize", "address", "--test-file-filter", "unit.py", "--modulo", "3", "--modulo-index", "1"} {
		testEqual(t, slices.Contains(args, word), true)
	}

	testEqual(t, slices.Contains(args, "--collect-cores"), false)
}

func TestNativeTestRuntimeArguments(t *testing.T) {
	for _, tc := range []struct{ kind, command string }{
		{"g_benchmark", "run_g_benchmark"}, {"boost_test", "run_boost_test"}, {"fuzz", "run_fuzz"}, {"unittest", "run_ut"},
	} {
		s := &TestSuite{projectPath: "app", testType: tc.kind, timeout: 60}
		args := anyStrs(testInnerCommand(s, TestMeta{binaryArgs: []string{"--arg"}, forkMode: "subtests"}, "bin", "work", TestChunk{}, &TestOpts{dist: true}))

		testEqual(t, args[1], tc.command)

		if tc.kind == "unittest" || tc.kind == "g_benchmark" {
			testEqual(t, slices.Contains(args, "--truncate-logs"), true)
		}

		if tc.kind == "fuzz" {
			testEqual(t, slices.Contains(args, "--fuzz-opts=--arg"), true)
		}
	}
}
