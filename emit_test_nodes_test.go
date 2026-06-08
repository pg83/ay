package main

import (
	"reflect"
	"regexp"
	"testing"
)

var testNodeResourceRefPattern = regexp.MustCompile(`\$\((CLANG|LLD_ROOT|YMAKE_PYTHON3)-[0-9]+\)`)

func sandboxedX8664TargetPlatform() *Platform {
	flags := make(map[string]string, len(testToolchainFlags)+4)
	for k, v := range testToolchainFlags {
		flags[k] = v
	}
	flags["PIC"] = "no"
	flags["GG_BUILD_TYPE"] = "debug"
	flags["SANDBOXING"] = "yes"
	flags["TESTS_REQUESTED"] = "yes"

	return NewPlatform(newMemFS(map[string]string{"build/ymake_conf.py": "debug_info_flags.append('-gz=zstd')\n"}), OSLinux, ISAX8664, flags, expectedSandboxingTags(), "", "", nil)
}

func sandboxedTestSuite() testSuiteInfo {
	return testSuiteInfo{
		ProjectPath: "util/ut",
		BinaryPath:  "$(B)/util/ut/util-ut",
		CppSources: []string{
			"util/ysafeptr_ut.cpp",
			"util/ysaveload_ut.cpp",
		},
	}
}

func expectedSandboxingTags() []string {
	return []string{
		"default-linux-x86_64",
		"debug",
		"FAKEID=sandboxing",
		"SANDBOXING=yes",
	}
}

func expectedTargetPlatformDescriptor() string {
	return "default-linux-x86_64-debug-FAKEID=sandboxing-SANDBOXING=yes"
}

func expectedTestEnv(testName string) EnvVars {
	return EnvVars{{Name: "ARCADIA_BUILD_ROOT", Value: "$(B)"}, {Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}, {Name: "ARCADIA_SOURCE_ROOT", Value: "$(S)"}, {Name: "ASAN_OPTIONS", Value: "exitcode=100"}, {Name: "ASAN_SYMBOLIZER_PATH", Value: "$(CLANG-1274503668)/bin/llvm-symbolizer"}, {Name: "GORACE", Value: "halt_on_error=1"}, {Name: "LSAN_OPTIONS", Value: "exitcode=100"}, {Name: "LSAN_SYMBOLIZER_PATH", Value: "$(CLANG-1274503668)/bin/llvm-symbolizer"}, {Name: "MSAN_OPTIONS", Value: "exitcode=100:report_umrs=1"}, {Name: "MSAN_SYMBOLIZER_PATH", Value: "$(CLANG-1274503668)/bin/llvm-symbolizer"}, {Name: "TESTING_SAVE_OUTPUT", Value: "yes"}, {Name: "TEST_NAME", Value: testName}, {Name: "TSAN_SYMBOLIZER_PATH", Value: "$(CLANG-1274503668)/bin/llvm-symbolizer"}, {Name: "UBSAN_OPTIONS", Value: "exitcode=100:print_stacktrace=1,halt_on_error=1"}, {Name: "UBSAN_SYMBOLIZER_PATH", Value: "$(CLANG-1274503668)/bin/llvm-symbolizer"}, {Name: "YA_CC", Value: "$(CLANG-1274503668)/bin/clang"}, {Name: "YA_CXX", Value: "$(CLANG-1274503668)/bin/clang++"}, {Name: "YA_MANDATORY_ENV_VARS", Value: "ASAN_OPTIONS:ASAN_SYMBOLIZER_PATH:LSAN_OPTIONS:LSAN_SYMBOLIZER_PATH:MSAN_OPTIONS:MSAN_SYMBOLIZER_PATH:TSAN_SYMBOLIZER_PATH:UBSAN_OPTIONS:UBSAN_SYMBOLIZER_PATH:YA_MANDATORY_ENV_VARS"}, {Name: "YA_NO_RESPAWN", Value: "yes"}, {Name: "YA_PYTHON_BIN", Value: "$(PYTHON)/python"}, {Name: "YA_TC", Value: "no"}, {Name: "YA_TEST_RUNNER", Value: "1"}}
}

func expectedTestCtxNode() *Node {
	return &Node{
		Cmds: []Cmd{{
			CmdArgs: appendInternStrs(nil, []string{
				"$(YMAKE_PYTHON3)/bin/python3",
				"$(S)/build/scripts/append_file.py",
				"$(B)/common_test.context",
				"{",
				`  "build_type": "debug",`,
				`  "flags": {`,
				`    "TESTS_REQUESTED": "yes"`,
				"  }",
				"}",
			}),
		}},
		Env:              nil,
		Inputs:           []VFS{Intern("$(S)/build/scripts/append_file.py")},
		KV:               KV{P: pkCP, PC: pcLightBlue},
		Outputs:          []VFS{Intern("$(B)/common_test.context")},
		Platform:         "default-linux-x86_64",
		Requirements:     Requirements{Network: "restricted"},
		Tags:             expectedSandboxingTags(),
		TargetProperties: TargetProperties{},
	}
}

func expectedUnittestNode(info testSuiteInfo) *Node {
	return &Node{
		Cmds: []Cmd{{
			Cwd: "$(B)",
			CmdArgs: appendInternStrs(nil, []string{
				"$(TEST_TOOL_HOST-sbr:12080295773)/test_tool",
				"run_test",
				"--ya-start-command-file",
				"--meta", "$(B)/util/ut/test-results/unittest/meta.json",
				"--trace", "$(B)/util/ut/test-results/unittest/ytest.report.trace",
				"--timeout", "60",
				"--log-path", "$(B)/util/ut/test-results/unittest/run_test.log",
				"--test-size", "small",
				"--test-type", "unittest",
				"--test-ci-type", "test",
				"--context-filename", "$(B)/common_test.context",
				"--source-root", "$(S)",
				"--build-root", "$(B)",
				"--test-suite-name", "unittest",
				"--project-path", "util/ut",
				"--test-related-path", "$(S)/util/ut",
				"--target-platform-descriptor", "default-linux-x86_64-debug-FAKEID=sandboxing-SANDBOXING=yes",
				"--remove-tos",
				"--gdb-path", "$(GDB)/gdb/bin/gdb",
				"--result-max-file-size", "0",
				"--verify-results",
				"--tests-limit-in-chunk", "100000",
				"--output-style", "ninja",
				"--python-bin", "$(PYTHON)/python",
				"--supports-test-parameters",
				"--smooth-shutdown-signals", "SIGUSR2",
				"--compression-filter", "zstd",
				"--compression-level", "1",
				"--global-resource", "CLANG14_RESOURCE_GLOBAL::$(CLANG14-1922233694)",
				"--global-resource", "CLANG16_RESOURCE_GLOBAL::$(CLANG16-1380963495)",
				"--global-resource", "CLANG18_RESOURCE_GLOBAL::$(CLANG18-1866954364)",
				"--global-resource", "CLANG20_RESOURCE_GLOBAL::$(CLANG20-178457234)",
				"--global-resource", "CLANG_FORMAT_RESOURCE_GLOBAL::$(CLANG_FORMAT-2463648791)",
				"--global-resource", "CLANG_RESOURCE_GLOBAL::$(CLANG-1380963495)",
				"--global-resource", "LLD_ROOT_RESOURCE_GLOBAL::$(LLD_ROOT-3107549726)",
				"--global-resource", "YMAKE_PYTHON3_RESOURCE_GLOBAL::$(YMAKE_PYTHON3-1002064631)",
				"--ram-limit-gb", "8",
				"--tar", "$(B)/util/ut/test-results/unittest/testing_out_stuff.tar.zstd",
				"$(TEST_TOOL_HOST-sbr:12080295773)/test_tool",
				"run_ut",
				"--binary", info.BinaryPath,
				"--trace-path", "$(B)/util/ut/test-results/unittest/ytest.report.trace",
				"--output-dir", "$(B)/util/ut/test-results/unittest/testing_out_stuff",
				"--modulo", "1",
				"--modulo-index", "0",
				"--partition-mode", "SEQUENTIAL",
				"--project-path", "util/ut",
				"--list-timeout", "30",
				"--verbose",
				"--gdb-path", "$(GDB)/gdb/bin/gdb",
				"--ya-end-command-file",
			}),
		}},
		Env:    expectedTestEnv("unittest"),
		Inputs: []VFS{Intern("$(S)/util/ut")},
		KV:     KV{P: pkTS, Path: "util/ut/unittest", PC: pcYellow, RunTestNode: true, ShowOutBool: true, HasSpecialRunner: true},
		Outputs: []VFS{
			Intern("$(B)/util/ut/test-results/unittest/meta.json"),
			Intern("$(B)/util/ut/test-results/unittest/ytest.report.trace"),
			Intern("$(B)/util/ut/test-results/unittest/run_test.log"),
			Intern("$(B)/util/ut/test-results/unittest/testing_out_stuff.tar.zstd"),
		},
		Platform:         "default-linux-x86_64",
		Requirements:     Requirements{CPU: 1, Network: "restricted", RAM: 8, HasRAMDisk: true},
		Tags:             expectedSandboxingTags(),
		TargetProperties: TargetProperties{ModuleLang: "cpp"},
	}
}

func expectedClangFormatNode() *Node {
	return &Node{
		Cmds: []Cmd{{
			Cwd: "$(B)",
			CmdArgs: appendInternStrs(nil, []string{
				"$(TEST_TOOL_HOST-sbr:12080295773)/test_tool",
				"run_test",
				"--ya-start-command-file",
				"--meta", "$(B)/util/ut/test-results/clang_format/meta.json",
				"--trace", "$(B)/util/ut/test-results/clang_format/ytest.report.trace",
				"--timeout", "60",
				"--log-path", "$(B)/util/ut/test-results/clang_format/run_test.log",
				"--test-size", "small",
				"--test-type", "clang_format",
				"--test-ci-type", "style",
				"--context-filename", "$(B)/common_test.context",
				"--source-root", "$(S)",
				"--build-root", "$(B)",
				"--test-suite-name", "clang_format",
				"--project-path", "util/ut",
				"--test-related-path", "$(S)/build/scripts/c_templates/svn_interface.c",
				"--test-related-path", "$(S)/util/ysafeptr_ut.cpp",
				"--test-related-path", "$(S)/util/ysaveload_ut.cpp",
				"--test-related-path", "$(S)/build/config/tests/cpp_style/.clang-format",
				"--test-related-path", "$(S)/tools/cpp_style_checker/wrapper.py",
				"--target-platform-descriptor", "default-linux-x86_64-debug-FAKEID=sandboxing-SANDBOXING=yes",
				"--remove-tos",
				"--gdb-path", "$(GDB)/gdb/bin/gdb",
				"--result-max-file-size", "0",
				"--verify-results",
				"--tests-limit-in-chunk", "100000",
				"--output-style", "ninja",
				"--python-bin", "$(PYTHON)/python",
				"--supports-test-parameters",
				"--compression-filter", "zstd",
				"--compression-level", "1",
				"--global-resource", "CLANG_FORMAT_RESOURCE_GLOBAL::$(CLANG_FORMAT-2463648791)",
				"--ram-limit-gb", "8",
				"--tar", "$(B)/util/ut/test-results/clang_format/testing_out_stuff.tar.zstd",
				"$(TEST_TOOL_HOST-sbr:12080295773)/test_tool",
				"run_custom_lint",
				"--source-root", "$(S)",
				"--build-root", "$(B)",
				"--project-path", "$(S)/util/ut",
				"--trace-path", "$(B)/util/ut/test-results/clang_format/ytest.report.trace",
				"--out-path", "$(B)/util/ut/test-results/clang_format/testing_out_stuff",
				"--lint-name", "clang_format",
				"--wrapper-script", "tools/cpp_style_checker/wrapper.py",
				"--depends", "util/ut",
				"--config", "$(S)/build/config/tests/cpp_style/.clang-format",
				"--global-resource", "CLANG_FORMAT_RESOURCE_GLOBAL::$(CLANG_FORMAT-2463648791)",
				"$(S)/build/scripts/c_templates/svn_interface.c",
				"$(S)/util/ysafeptr_ut.cpp",
				"$(S)/util/ysaveload_ut.cpp",
				"--ya-end-command-file",
			}),
		}},
		Env: expectedTestEnv("clang_format"),
		Inputs: []VFS{
			Intern("$(S)/build/config/tests/cpp_style/.clang-format"),
			Intern("$(S)/build/scripts/c_templates/svn_interface.c"),
			Intern("$(S)/tools/cpp_style_checker/wrapper.py"),
			Intern("$(S)/util/ut"),
			Intern("$(S)/util/ysafeptr_ut.cpp"),
			Intern("$(S)/util/ysaveload_ut.cpp"),
		},
		KV: KV{P: pkTS, Path: "util/ut/clang_format", PC: pcYellow, RunTestNode: true, ShowOutBool: true, HasSpecialRunner: true},
		Outputs: []VFS{
			Intern("$(B)/util/ut/test-results/clang_format/meta.json"),
			Intern("$(B)/util/ut/test-results/clang_format/ytest.report.trace"),
			Intern("$(B)/util/ut/test-results/clang_format/run_test.log"),
			Intern("$(B)/util/ut/test-results/clang_format/testing_out_stuff.tar.zstd"),
		},
		Platform:         "default-linux-x86_64",
		Requirements:     Requirements{CPU: 1, Network: "restricted", RAM: 8, HasRAMDisk: true},
		Tags:             nil,
		TargetProperties: TargetProperties{ModuleLang: "unknown"},
	}
}

// cmdsEqual compares two Cmd slices treating CmdArgs by their materialized string
// content (an arg's source namespace — STR vs VFS vs ARG — is irrelevant to the emitted
// command; only the string matters), and the remaining Cmd fields structurally.
func cmdsEqual(got, want []Cmd) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if !reflect.DeepEqual(strStrs(got[i].CmdArgs), strStrs(want[i].CmdArgs)) {
			return false
		}

		g, w := got[i], want[i]
		g.CmdArgs, w.CmdArgs = nil, nil

		if !reflect.DeepEqual(g, w) {
			return false
		}
	}

	return true
}

func assertNodeFields(t *testing.T, name string, got, want *Node) {
	t.Helper()

	if !cmdsEqual(got.Cmds, want.Cmds) {
		t.Fatalf("%s cmds mismatch\n got: %#v\nwant: %#v", name, got.Cmds, want.Cmds)
	}
	if !reflect.DeepEqual(got.Env, want.Env) {
		t.Fatalf("%s env mismatch\n got: %#v\nwant: %#v", name, got.Env, want.Env)
	}
	if !reflect.DeepEqual(got.Inputs, want.Inputs) {
		t.Fatalf("%s inputs mismatch\n got: %#v\nwant: %#v", name, got.Inputs, want.Inputs)
	}
	if !reflect.DeepEqual(got.KV, want.KV) {
		t.Fatalf("%s kv mismatch\n got: %#v\nwant: %#v", name, got.KV, want.KV)
	}
	if !reflect.DeepEqual(got.Outputs, want.Outputs) {
		t.Fatalf("%s outputs mismatch\n got: %#v\nwant: %#v", name, got.Outputs, want.Outputs)
	}
	if got.Platform != want.Platform {
		t.Fatalf("%s platform = %q, want %q", name, got.Platform, want.Platform)
	}
	if !reflect.DeepEqual(got.Requirements, want.Requirements) {
		t.Fatalf("%s requirements mismatch\n got: %#v\nwant: %#v", name, got.Requirements, want.Requirements)
	}
	if !reflect.DeepEqual(got.Tags, want.Tags) {
		t.Fatalf("%s tags mismatch\n got: %#v\nwant: %#v", name, got.Tags, want.Tags)
	}
	if !reflect.DeepEqual(got.TargetProperties, want.TargetProperties) {
		t.Fatalf("%s target_properties mismatch\n got: %#v\nwant: %#v", name, got.TargetProperties, want.TargetProperties)
	}
}

type canonicalFixtureNode struct {
	Cmds             interface{}
	Env              map[string]interface{}
	KV               map[string]interface{}
	Inputs           []string
	Outputs          []string
	Platform         string
	Requirements     map[string]interface{}
	Tags             []string
	TargetProperties map[string]interface{}
}

func TestEmitTestRunNodes_BuildersMatchSpec(t *testing.T) {
	p := sandboxedX8664TargetPlatform()
	info := sandboxedTestSuite()

	if got := targetPlatformDescriptor(p); got != expectedTargetPlatformDescriptor() {
		t.Fatalf("targetPlatformDescriptor = %q, want %q", got, expectedTargetPlatformDescriptor())
	}

	if got := sandboxingNodeTags(p); !reflect.DeepEqual(got, expectedSandboxingTags()) {
		t.Fatalf("sandboxingNodeTags = %#v, want %#v", got, expectedSandboxingTags())
	}

	assertNodeFields(t, "ctx", buildTestCtxNode(p), expectedTestCtxNode())
	assertNodeFields(t, "unittest", buildUnittestNode(p, info), expectedUnittestNode(info))
	assertNodeFields(t, "clang_format", buildClangFormatNode(p, info), expectedClangFormatNode())

	if tags := buildClangFormatNode(p, info).Tags; tags != nil {
		t.Fatalf("clang_format tags = %#v, want nil", tags)
	}
}

func TestEmitTestRunNodes_WiringAndGenHook(t *testing.T) {
	p := sandboxedX8664TargetPlatform()
	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})

	fs := newMemFS(map[string]string{
		"util/ut/ya.make": "UNITTEST_FOR(util)\nSRCS(ysafeptr_ut.cpp ysaveload_ut.cpp)\nEND()\n",
		"util/ya.make":    "LIBRARY()\nSRCS(lib.cpp)\nEND()\n",
		"library/cpp/testing/unittest_main/ya.make":  "LIBRARY()\nSRCS(main.cpp)\nEND()\n",
		"util/ysafeptr_ut.cpp":                       "int ysafeptr_ut() { return 0; }\n",
		"util/ysaveload_ut.cpp":                      "int ysaveload_ut() { return 0; }\n",
		"util/lib.cpp":                               "int util_lib() { return 0; }\n",
		"library/cpp/testing/unittest_main/main.cpp": "int unittest_main() { return 0; }\n",
	})

	resources := &resourceFetchPlan{
		items: []resourceFetch{{
			Pattern: "YMAKE_PYTHON3",
			URI:     "sbr:dummy-ymake-python3",
			Output:  Intern("$(B)/resources/YMAKE_PYTHON3"),
		}},
	}

	g := GenWithResources(fs, "util/ut", host, p, func(Warn) {}, resources, true)
	if len(g.Result) != 3 {
		t.Fatalf("result len = %d, want 3", len(g.Result))
	}

	var ldNode, unittestNode, clangNode, ctxNode, fetchNode *Node
	byUID := make(map[UID]*Node, len(g.Graph))
	byOutput := make(map[string]*Node, len(g.Graph))
	for _, node := range g.Graph {
		byUID[node.UID] = node
		if len(node.Outputs) == 1 {
			byOutput[node.Outputs[0].String()] = node
		}

		kvPath := node.KV.Path
		switch {
		case node.KV.P == pkLD:
			ldNode = node
		case kvPath == "util/ut/unittest":
			unittestNode = node
		case kvPath == "util/ut/clang_format":
			clangNode = node
		case len(node.Outputs) == 1 && node.Outputs[0].String() == "$(B)/common_test.context":
			ctxNode = node
		case len(node.Outputs) == 1 && node.Outputs[0].String() == "$(B)/resources/YMAKE_PYTHON3":
			fetchNode = node
		}
	}

	if ldNode == nil || unittestNode == nil || clangNode == nil || ctxNode == nil || fetchNode == nil {
		t.Fatalf("missing expected nodes: ld=%v unittest=%v clang=%v ctx=%v fetch=%v", ldNode != nil, unittestNode != nil, clangNode != nil, ctxNode != nil, fetchNode != nil)
	}

	if g.Result[0] != ldNode.UID || g.Result[1] != unittestNode.UID || g.Result[2] != clangNode.UID {
		t.Fatalf("result order = %v, want [%s %s %s]", g.Result, ldNode.UID, unittestNode.UID, clangNode.UID)
	}

	if got := ldNode.Outputs[0].String(); got != "$(B)/util/ut/util-ut" {
		t.Fatalf("ld output = %q, want $(B)/util/ut/util-ut", got)
	}

	if len(graphDeps(g, ctxNode)) != 1 || graphDeps(g, ctxNode)[0] != fetchNode.UID {
		t.Fatalf("ctx deps = %v, want [%s]", graphDeps(g, ctxNode), fetchNode.UID)
	}

	unittestDeps := make(map[UID]struct{}, len(graphDeps(g, unittestNode)))
	for _, dep := range graphDeps(g, unittestNode) {
		unittestDeps[dep] = struct{}{}
	}
	if len(graphDeps(g, unittestNode)) != 2 {
		t.Fatalf("unittest deps = %v, want exactly [ld ctx]", graphDeps(g, unittestNode))
	}
	if _, ok := unittestDeps[ldNode.UID]; !ok {
		t.Fatalf("unittest deps missing LD uid %q", ldNode.UID)
	}
	if _, ok := unittestDeps[ctxNode.UID]; !ok {
		t.Fatalf("unittest deps missing ctx uid %q", ctxNode.UID)
	}
	if _, ok := unittestDeps[fetchNode.UID]; ok {
		t.Fatalf("unittest deps unexpectedly include fetch uid %q", fetchNode.UID)
	}
	if len(graphDeps(g, clangNode)) != 1 || graphDeps(g, clangNode)[0] != ctxNode.UID {
		t.Fatalf("clang deps = %v, want [%s]", graphDeps(g, clangNode), ctxNode.UID)
	}

	unittestArgs := strStrs(unittestNode.Cmds[0].CmdArgs)
	binaryValue := ""
	for i := 0; i+1 < len(unittestArgs); i++ {
		if unittestArgs[i] == "--binary" {
			binaryValue = unittestArgs[i+1]
			break
		}
	}
	if binaryValue != "$(B)/util/ut/util-ut" {
		t.Fatalf("unittest --binary = %q, want $(B)/util/ut/util-ut", binaryValue)
	}

	clangInputs := make([]string, 0, len(clangNode.Inputs))
	for _, input := range clangNode.Inputs {
		clangInputs = append(clangInputs, input.String())
	}
	for _, want := range []string{"$(S)/util/ysafeptr_ut.cpp", "$(S)/util/ysaveload_ut.cpp"} {
		if !containsString(clangInputs, want) {
			t.Fatalf("clang inputs missing %q in %v", want, clangInputs)
		}
	}
	for _, bad := range []string{"$(S)/util/ut/ysafeptr_ut.cpp", "$(S)/util/ut/ysaveload_ut.cpp"} {
		if containsString(clangInputs, bad) {
			t.Fatalf("clang inputs unexpectedly include %q in %v", bad, clangInputs)
		}
	}

	clangArgs := strStrs(clangNode.Cmds[0].CmdArgs)
	var relatedPaths []string
	for i := 0; i+1 < len(clangArgs); i++ {
		if clangArgs[i] == "--test-related-path" {
			relatedPaths = append(relatedPaths, clangArgs[i+1])
		}
	}
	for _, want := range []string{"$(S)/util/ysafeptr_ut.cpp", "$(S)/util/ysaveload_ut.cpp"} {
		if !containsString(relatedPaths, want) {
			t.Fatalf("clang related paths missing %q in %v", want, relatedPaths)
		}
	}
	for _, bad := range []string{"$(S)/util/ut/ysafeptr_ut.cpp", "$(S)/util/ut/ysaveload_ut.cpp"} {
		if containsString(relatedPaths, bad) {
			t.Fatalf("clang related paths unexpectedly include %q in %v", bad, relatedPaths)
		}
	}

	tail := clangArgs[len(clangArgs)-4:]
	wantTail := []string{
		"$(S)/build/scripts/c_templates/svn_interface.c",
		"$(S)/util/ysafeptr_ut.cpp",
		"$(S)/util/ysaveload_ut.cpp",
		"--ya-end-command-file",
	}
	if !reflect.DeepEqual(tail, wantTail) {
		t.Fatalf("clang lint-file tail = %v, want %v", tail, wantTail)
	}

	if byUID[g.Result[1]] != unittestNode || byUID[g.Result[2]] != clangNode {
		t.Fatalf("result uids do not resolve to expected test nodes")
	}

	ccSpecs := []struct {
		output string
		input  string
		bad    string
	}{
		{
			output: "$(B)/util/ut/__/ysafeptr_ut.cpp.o",
			input:  "$(S)/util/ysafeptr_ut.cpp",
			bad:    "$(S)/util/ut/ysafeptr_ut.cpp",
		},
		{
			output: "$(B)/util/ut/__/ysaveload_ut.cpp.o",
			input:  "$(S)/util/ysaveload_ut.cpp",
			bad:    "$(S)/util/ut/ysaveload_ut.cpp",
		},
	}
	for _, spec := range ccSpecs {
		ccNode := byOutput[spec.output]
		if ccNode == nil {
			t.Fatalf("missing rebased test-object output %q", spec.output)
		}
		if ccNode.TargetProperties.ModuleDir != "util/ut" {
			t.Fatalf("cc module_dir for %q = %q, want util/ut", spec.output, ccNode.TargetProperties.ModuleDir)
		}
		if !reflect.DeepEqual(ccNode.Tags, expectedSandboxingTags()) {
			t.Fatalf("cc tags for %q = %v, want %v", spec.output, ccNode.Tags, expectedSandboxingTags())
		}
		ccInputs := make([]string, 0, len(ccNode.Inputs))
		for _, input := range ccNode.Inputs {
			ccInputs = append(ccInputs, input.String())
		}
		if !containsString(ccInputs, spec.input) {
			t.Fatalf("cc inputs for %q missing %q in %v", spec.output, spec.input, ccInputs)
		}
		if containsString(ccInputs, spec.bad) {
			t.Fatalf("cc inputs for %q unexpectedly include %q in %v", spec.output, spec.bad, ccInputs)
		}
		if !containsString(strStrs(ccNode.Cmds[0].CmdArgs), "-gz=zstd") {
			t.Fatalf("cc cmd for %q missing -gz=zstd: %v", spec.output, ccNode.Cmds[0].CmdArgs)
		}
	}
	for _, badOutput := range []string{"$(B)/util/ut/ysafeptr_ut.cpp.o", "$(B)/util/ut/ysaveload_ut.cpp.o"} {
		if byOutput[badOutput] != nil {
			t.Fatalf("unexpected local test-object output %q", badOutput)
		}
	}

	if !reflect.DeepEqual(ldNode.Tags, expectedSandboxingTags()) {
		t.Fatalf("ld tags = %v, want %v", ldNode.Tags, expectedSandboxingTags())
	}
	if !containsString(strStrs(ldNode.Cmds[1].CmdArgs), "-gz=zstd") {
		t.Fatalf("ld vcs compile cmd missing -gz=zstd: %v", ldNode.Cmds[1].CmdArgs)
	}

	linkArgs := strStrs(ldNode.Cmds[2].CmdArgs)
	wantLinkPrefix := []string{
		"$(S)/build/scripts/link_exe.py",
		"--start-plugins",
		"--end-plugins",
	}
	if got := linkArgs[1 : 1+len(wantLinkPrefix)]; !reflect.DeepEqual(got, wantLinkPrefix) {
		t.Fatalf("ld link prefix = %v, want %v", got, wantLinkPrefix)
	}
	linkObjects := []string{
		"$(B)/util/ut/__vcs_version__.c.o",
		"$(B)/util/ut/__/ysafeptr_ut.cpp.o",
		"$(B)/util/ut/__/ysaveload_ut.cpp.o",
	}
	oIndex := -1
	for i, arg := range linkArgs {
		if arg == "-o" {
			oIndex = i
			break
		}
	}
	if oIndex < len(linkObjects) {
		t.Fatalf("ld link cmd missing object block before -o: %v", linkArgs)
	}
	if got := linkArgs[oIndex-len(linkObjects) : oIndex]; !reflect.DeepEqual(got, linkObjects) {
		t.Fatalf("ld object block = %v, want %v", got, linkObjects)
	}
}

func containsString(values []string, want string) bool {
	return slicesContains(values, want)
}
