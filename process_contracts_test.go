package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentProcessAncestry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		exe   string
		want  bool
	}{
		{name: "missing"},
		{name: "invalid-stat", files: map[string]string{"42/stat": "broken"}},
		{name: "truncated-stat", files: map[string]string{"42/stat": "42 (shell) S"}},
		{name: "ordinary-parent", files: map[string]string{"42/stat": "42 (go) S 20", "20/stat": "20 (bash) S 1"}},
		{name: "agent-parent", files: map[string]string{"42/stat": "42 (go) S 20", "20/stat": "20 (CoDeX) S 1"}, want: true},
		{name: "parentheses-in-name", files: map[string]string{"42/stat": "42 (a (shell)) S 20", "20/stat": "20 (claude) S 1"}, want: true},
		{name: "agent-executable", files: map[string]string{"42/stat": "42 (node) S 1"}, exe: "/opt/claude/bin/node", want: true},
		{name: "long-command", files: map[string]string{"42/stat": "42 (/some/long/path) S 1", "42/cmdline": "/some/long/path/ordinary\x00--flag"}},
		{name: "truncated-command", files: map[string]string{"42/stat": "42 (ordinary-long-n) S 1", "42/cmdline": "/bin/ordinary-long-name\x00--flag"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for rel, body := range tc.files {
				file := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.exe != "" {
				if err := os.Symlink(tc.exe, filepath.Join(root, "42/exe")); err != nil {
					t.Fatal(err)
				}
			}
			testEqual(t, launchedByAgentInProc(root, 42), tc.want)
		})
	}
	// With no identifying environment, the public entry point follows the
	// actual process tree. Compare it with the same walker independently of
	// whether the test itself runs inside an agent.
	for _, key := range testAgentEnvSignatures {
		value, present := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if present {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		})
	}
	testEqual(t, launchedByAgent(), launchedByAgentInProc("/proc", os.Getpid()))
}

func TestTestMetadataMacros(t *testing.T) {
	d := &ModuleData{}
	for _, tc := range []struct {
		name TOK
		args []string
	}{
		{tokDockerImage, []string{"image=tag"}}, {tokExplicitData, nil},
		{tokData, []string{"one", "AUTOUPDATED", "revision", "two"}},
		{tokDataFiles, []string{"$(S)/a", "${ARCADIA_ROOT}/b", "c"}},
		{tokForkSubtests, []string{"modulo"}}, {tokForkTestFiles, nil},
		{tokBenchmarkOpts, []string{"--benchmark_min_time=1"}}, {tokFuzzOpts, []string{"-runs=5"}},
	} {
		testEqual(t, applyTestMacro(UnknownStmt{Name: tc.name, Args: internAnys(tc.args)}, d), true)
	}
	testEqual(t, d.test.dockerImages, []string{"image=tag"})
	testEqual(t, d.test.explicitData, true)
	testEqual(t, d.test.data, []string{"one", "two", "arcadia/a", "arcadia/b", "arcadia/c"})
	testEqual(t, d.test.forkMode, "subtests")
	testEqual(t, d.test.partition, "MODULO")
	testEqual(t, d.test.forkFiles, true)
	testEqual(t, d.test.binaryArgs, []string{"--benchmark_min_time=1", "-runs=5"})
	for _, tc := range []struct{ k, v, w string }{{"SANITIZER_TYPE", "address", "asan"}, {"USE_LTO", "yes", "lto"}, {"USE_LTO", "no", ""}, {"CUSTOM", "on", "CUSTOM=on"}} {
		testEqual(t, testFmtTag(tc.k, tc.v), tc.w)
	}
	testEqual(t, testPlatformDescriptor(&MakeFlags{targetPlat: "default-linux-x86_64", buildType: "release", tflags: map[string]string{"RACE": "yes"}}), "default-linux-x86_64-release-race")
}

func TestTestCommandLineQuoting(t *testing.T) {
	for _, tc := range []struct{ arg, want string }{
		{"", `""`}, {`a b`, `"a b"`}, {`a"b`, `a\"b`},
		{`a\b`, `a\b`}, {`a\"b`, `a\\\"b`}, {`a b\`, `"a b\\"`},
	} {
		t.Run(fmt.Sprintf("%q", tc.arg), func(t *testing.T) { testEqual(t, testCommandLine([]string{"run", tc.arg}), "run "+tc.want) })
	}
}
