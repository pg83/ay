package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
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

	return NewPlatform(OSLinux, ISAX8664, flags, expectedSandboxingTags(), "", "")
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

func expectedTestEnv(testName string) map[string]string {
	return map[string]string{
		"ARCADIA_BUILD_ROOT":     "$(B)",
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
		"ARCADIA_SOURCE_ROOT":    "$(S)",
		"ASAN_OPTIONS":           "exitcode=100",
		"ASAN_SYMBOLIZER_PATH":   "$(CLANG-1274503668)/bin/llvm-symbolizer",
		"GORACE":                 "halt_on_error=1",
		"LSAN_OPTIONS":           "exitcode=100",
		"LSAN_SYMBOLIZER_PATH":   "$(CLANG-1274503668)/bin/llvm-symbolizer",
		"MSAN_OPTIONS":           "exitcode=100:report_umrs=1",
		"MSAN_SYMBOLIZER_PATH":   "$(CLANG-1274503668)/bin/llvm-symbolizer",
		"TESTING_SAVE_OUTPUT":    "yes",
		"TEST_NAME":              testName,
		"TSAN_SYMBOLIZER_PATH":   "$(CLANG-1274503668)/bin/llvm-symbolizer",
		"UBSAN_OPTIONS":          "exitcode=100:print_stacktrace=1,halt_on_error=1",
		"UBSAN_SYMBOLIZER_PATH":  "$(CLANG-1274503668)/bin/llvm-symbolizer",
		"YA_CC":                  "$(CLANG-1274503668)/bin/clang",
		"YA_CXX":                 "$(CLANG-1274503668)/bin/clang++",
		"YA_MANDATORY_ENV_VARS":  "ASAN_OPTIONS:ASAN_SYMBOLIZER_PATH:LSAN_OPTIONS:LSAN_SYMBOLIZER_PATH:MSAN_OPTIONS:MSAN_SYMBOLIZER_PATH:TSAN_SYMBOLIZER_PATH:UBSAN_OPTIONS:UBSAN_SYMBOLIZER_PATH:YA_MANDATORY_ENV_VARS",
		"YA_NO_RESPAWN":          "yes",
		"YA_PYTHON_BIN":          "$(PYTHON)/python",
		"YA_TC":                  "no",
		"YA_TEST_RUNNER":         "1",
	}
}

func expectedTestCtxNode() *Node {
	return &Node{
		Cmds: []Cmd{{
			CmdArgs: []string{
				"$(YMAKE_PYTHON3)/bin/python3",
				"$(S)/build/scripts/append_file.py",
				"$(B)/common_test.context",
				"{",
				`  "build_type": "debug",`,
				`  "flags": {`,
				`    "TESTS_REQUESTED": "yes"`,
				"  }",
				"}",
			},
		}},
		Env:      map[string]string{},
		Inputs:   []VFS{Source("build/scripts/append_file.py")},
		KV:       map[string]interface{}{"p": "CP", "pc": "light-blue"},
		Outputs:  []VFS{Build("common_test.context")},
		Platform: "default-linux-x86_64",
		Requirements: map[string]interface{}{
			"network": "restricted",
		},
		Tags:             expectedSandboxingTags(),
		TargetProperties: map[string]string{},
	}
}

func expectedUnittestNode(info testSuiteInfo) *Node {
	return &Node{
		Cmds: []Cmd{{
			Cwd: "$(B)",
			CmdArgs: []string{
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
			},
		}},
		Env:    expectedTestEnv("unittest"),
		Inputs: []VFS{Source("util/ut")},
		KV: map[string]interface{}{
			"p":              "TS",
			"path":           "util/ut/unittest",
			"pc":             "yellow",
			"run_test_node":  true,
			"show_out":       true,
			"special_runner": "",
		},
		Outputs: []VFS{
			Build("util/ut/test-results/unittest/meta.json"),
			Build("util/ut/test-results/unittest/ytest.report.trace"),
			Build("util/ut/test-results/unittest/run_test.log"),
			Build("util/ut/test-results/unittest/testing_out_stuff.tar.zstd"),
		},
		Platform: "default-linux-x86_64",
		Requirements: map[string]interface{}{
			"cpu":      float64(1),
			"network":  "restricted",
			"ram":      float64(8),
			"ram_disk": float64(0),
		},
		Tags: expectedSandboxingTags(),
		TargetProperties: map[string]string{
			"module_lang": "cpp",
		},
	}
}

func expectedClangFormatNode() *Node {
	return &Node{
		Cmds: []Cmd{{
			Cwd: "$(B)",
			CmdArgs: []string{
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
			},
		}},
		Env: expectedTestEnv("clang_format"),
		Inputs: []VFS{
			Source("build/config/tests/cpp_style/.clang-format"),
			Source("build/scripts/c_templates/svn_interface.c"),
			Source("tools/cpp_style_checker/wrapper.py"),
			Source("util/ut"),
			Source("util/ysafeptr_ut.cpp"),
			Source("util/ysaveload_ut.cpp"),
		},
		KV: map[string]interface{}{
			"p":              "TS",
			"path":           "util/ut/clang_format",
			"pc":             "yellow",
			"run_test_node":  true,
			"show_out":       true,
			"special_runner": "",
		},
		Outputs: []VFS{
			Build("util/ut/test-results/clang_format/meta.json"),
			Build("util/ut/test-results/clang_format/ytest.report.trace"),
			Build("util/ut/test-results/clang_format/run_test.log"),
			Build("util/ut/test-results/clang_format/testing_out_stuff.tar.zstd"),
		},
		Platform: "default-linux-x86_64",
		Requirements: map[string]interface{}{
			"cpu":      float64(1),
			"network":  "restricted",
			"ram":      float64(8),
			"ram_disk": float64(0),
		},
		Tags: nil,
		TargetProperties: map[string]string{
			"module_lang": "unknown",
		},
	}
}

func assertNodeFields(t *testing.T, name string, got, want *Node) {
	t.Helper()

	if !reflect.DeepEqual(got.Cmds, want.Cmds) {
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

func normalizeGraphJSON(raw []byte) map[string]interface{} {
	s := string(raw)
	s = testNodeResourceRefPattern.ReplaceAllString(s, "$$($1)")
	s = regexp.MustCompile(`\$\((BUILD_ROOT|SOURCE_ROOT)\)`).ReplaceAllStringFunc(s, func(m string) string {
		if m == "$(BUILD_ROOT)" {
			return "$(B)"
		}

		return "$(S)"
	})

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		panic(err)
	}

	return out
}

func graphNodes(doc map[string]interface{}) []map[string]interface{} {
	rawNodes, _ := doc["graph"].([]interface{})
	nodes := make([]map[string]interface{}, 0, len(rawNodes))
	for _, raw := range rawNodes {
		node, _ := raw.(map[string]interface{})
		nodes = append(nodes, node)
	}

	return nodes
}

func findSerializedNode(t *testing.T, nodes []map[string]interface{}, predicate func(map[string]interface{}) bool, name string) map[string]interface{} {
	t.Helper()

	for _, node := range nodes {
		if predicate(node) {
			return node
		}
	}

	t.Fatalf("missing %s node", name)

	return nil
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

func logicalFixtureNodes(t *testing.T, nodes []map[string]interface{}, scope string) map[string]map[string]interface{} {
	t.Helper()

	return map[string]map[string]interface{}{
		"ctx": findSerializedNode(t, nodes, func(node map[string]interface{}) bool {
			outputs := stringSlice(node["outputs"])
			return len(outputs) == 1 && outputs[0] == "$(B)/common_test.context"
		}, scope+" ctx"),
		"unittest": findSerializedNode(t, nodes, func(node map[string]interface{}) bool {
			return stringValue(mapStringAny(node["kv"])["path"]) == "util/ut/unittest"
		}, scope+" unittest"),
		"clang_format": findSerializedNode(t, nodes, func(node map[string]interface{}) bool {
			return stringValue(mapStringAny(node["kv"])["path"]) == "util/ut/clang_format"
		}, scope+" clang_format"),
		"ld": findSerializedNode(t, nodes, func(node map[string]interface{}) bool {
			outputs := stringSlice(node["outputs"])
			return len(outputs) == 1 && outputs[0] == "$(B)/util/ut/util-ut"
		}, scope+" ld"),
	}
}

func depLabelMap(logical map[string]map[string]interface{}) map[string]string {
	out := make(map[string]string, len(logical))
	for label, node := range logical {
		out[stringValue(node["uid"])] = label
	}

	return out
}

func depLabels(t *testing.T, name string, node map[string]interface{}, uidToLabel map[string]string) []string {
	t.Helper()

	deps := stringSlice(node["deps"])
	labels := make([]string, 0, len(deps))
	for _, dep := range deps {
		label, ok := uidToLabel[dep]
		if !ok {
			t.Fatalf("%s has unknown dep uid %q", name, dep)
		}
		labels = append(labels, label)
	}
	sort.Strings(labels)

	return labels
}

func canonicalizeFixtureNode(node map[string]interface{}) canonicalFixtureNode {
	inputs := stringSlice(node["inputs"])
	sort.Strings(inputs)

	tags := stringSlice(node["tags"])
	if len(tags) > 0 {
		sort.Strings(tags)
	}

	return canonicalFixtureNode{
		Cmds:             node["cmds"],
		Env:              mapStringAny(node["env"]),
		KV:               mapStringAny(node["kv"]),
		Inputs:           inputs,
		Outputs:          stringSlice(node["outputs"]),
		Platform:         stringValue(node["platform"]),
		Requirements:     mapStringAny(node["requirements"]),
		Tags:             tags,
		TargetProperties: mapStringAny(node["target_properties"]),
	}
}

func stringSlice(v interface{}) []string {
	raw, _ := v.([]interface{})
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

func mapStringAny(v interface{}) map[string]interface{} {
	raw, _ := v.(map[string]interface{})
	if raw == nil {
		return map[string]interface{}{}
	}

	return raw
}

func stringValue(v interface{}) string {
	s, _ := v.(string)

	return s
}

func serializeGraphJSON(g *Graph) []byte {
	var buf bytes.Buffer
	writeGraphIndented(&buf, g)

	return buf.Bytes()
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

func TestEmitTestRunNodes_FixtureCrossCheck(t *testing.T) {
	p := sandboxedX8664TargetPlatform()
	info := sandboxedTestSuite()

	emit := NewBufferedEmitter()
	ldRef := emit.Emit(&Node{
		Cmds:             []Cmd{{CmdArgs: []string{"ld"}, Env: map[string]string{}}},
		Env:              map[string]string{},
		Inputs:           []VFS{},
		KV:               map[string]interface{}{"p": "LD"},
		Outputs:          []VFS{Build("util/ut/util-ut")},
		Platform:         string(p.Target),
		Requirements:     map[string]interface{}{},
		Tags:             []string{},
		TargetProperties: map[string]string{},
	})
	emitTestRunNodes(emit, emit, p, info, ldRef)

	ourDoc := normalizeGraphJSON(serializeGraphJSON(Finalize(emit)))
	refRaw, err := os.ReadFile("/home/pg/monorepo/ydb/sg4.json")
	if err != nil {
		t.Fatalf("read sg4.json: %v", err)
	}
	refDoc := normalizeGraphJSON(refRaw)

	ourLogical := logicalFixtureNodes(t, graphNodes(ourDoc), "our")
	refLogical := logicalFixtureNodes(t, graphNodes(refDoc), "ref")
	ourDepLabels := depLabelMap(ourLogical)
	refDepLabels := depLabelMap(refLogical)

	expectedDeps := map[string][]string{
		"ctx":          {},
		"unittest":     {"ctx", "ld"},
		"clang_format": {"ctx"},
	}

	for _, name := range []string{"ctx", "unittest", "clang_format"} {
		got := canonicalizeFixtureNode(ourLogical[name])
		want := canonicalizeFixtureNode(refLogical[name])
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s canonical fixture mismatch\n got: %#v\nwant: %#v", name, got, want)
		}

		gotDeps := depLabels(t, "our "+name, ourLogical[name], ourDepLabels)
		if !reflect.DeepEqual(gotDeps, expectedDeps[name]) {
			t.Fatalf("our %s dep labels = %#v, want %#v", name, gotDeps, expectedDeps[name])
		}

		wantDeps := depLabels(t, "ref "+name, refLogical[name], refDepLabels)
		if !reflect.DeepEqual(wantDeps, expectedDeps[name]) {
			t.Fatalf("ref %s dep labels = %#v, want %#v", name, wantDeps, expectedDeps[name])
		}
	}
}

func TestEmitTestRunNodes_WiringAndGenHook(t *testing.T) {
	p := sandboxedX8664TargetPlatform()
	host := newTestPlatform(OSLinux, ISAX8664, "yes", []string{"tool"})
	root := t.TempDir()

	mk := func(dir, body string) {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(path, "ya.make"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s/ya.make: %v", dir, err)
		}
	}

	mkfile := func(rel, body string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mk("util/ut", "UNITTEST_FOR(util)\nSRCS(ysafeptr_ut.cpp ysaveload_ut.cpp)\nEND()\n")
	mk("util", "LIBRARY()\nSRCS(lib.cpp)\nEND()\n")
	mk("library/cpp/testing/unittest_main", "LIBRARY()\nSRCS(main.cpp)\nEND()\n")
	mkfile("util/ysafeptr_ut.cpp", "int ysafeptr_ut() { return 0; }\n")
	mkfile("util/ysaveload_ut.cpp", "int ysaveload_ut() { return 0; }\n")
	mkfile("util/lib.cpp", "int util_lib() { return 0; }\n")
	mkfile("library/cpp/testing/unittest_main/main.cpp", "int unittest_main() { return 0; }\n")

	resources := &resourceFetchPlan{
		items: []resourceFetch{{
			Pattern: "YMAKE_PYTHON3",
			URI:     "sbr:dummy-ymake-python3",
			Output:  Build("resources/YMAKE_PYTHON3"),
		}},
	}

	g := GenWithModeWithResources(root, "util/ut", host, p, defaultScanCtxMode, func(Warn) {}, resources, true)
	if len(g.Result) != 3 {
		t.Fatalf("result len = %d, want 3", len(g.Result))
	}

	var ldNode, unittestNode, clangNode, ctxNode, fetchNode *Node
	byUID := make(map[string]*Node, len(g.Graph))
	byOutput := make(map[string]*Node, len(g.Graph))
	for _, node := range g.Graph {
		byUID[node.UID] = node
		if len(node.Outputs) == 1 {
			byOutput[node.Outputs[0].String()] = node
		}

		kvPath, _ := node.KV["path"].(string)
		switch {
		case node.KV["p"] == "LD":
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

	if len(ctxNode.Deps) != 1 || ctxNode.Deps[0] != fetchNode.UID {
		t.Fatalf("ctx deps = %v, want [%s]", ctxNode.Deps, fetchNode.UID)
	}

	unittestDeps := make(map[string]struct{}, len(unittestNode.Deps))
	for _, dep := range unittestNode.Deps {
		unittestDeps[dep] = struct{}{}
	}
	if len(unittestNode.Deps) != 2 {
		t.Fatalf("unittest deps = %v, want exactly [ld ctx]", unittestNode.Deps)
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
	if len(clangNode.Deps) != 1 || clangNode.Deps[0] != ctxNode.UID {
		t.Fatalf("clang deps = %v, want [%s]", clangNode.Deps, ctxNode.UID)
	}

	unittestArgs := unittestNode.Cmds[0].CmdArgs
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

	clangArgs := clangNode.Cmds[0].CmdArgs
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
		if ccNode.TargetProperties["module_dir"] != "util/ut" {
			t.Fatalf("cc module_dir for %q = %q, want util/ut", spec.output, ccNode.TargetProperties["module_dir"])
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
		if !containsString(ccNode.Cmds[0].CmdArgs, "-gz=zstd") {
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
	if !containsString(ldNode.Cmds[1].CmdArgs, "-gz=zstd") {
		t.Fatalf("ld vcs compile cmd missing -gz=zstd: %v", ldNode.Cmds[1].CmdArgs)
	}

	linkArgs := ldNode.Cmds[2].CmdArgs
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
