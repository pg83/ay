package main

import (
	"encoding/json"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
)

var (
	testContextAllowedFlags = []string{"AUTOCHECK", "TESTS_REQUESTED", "USE_ARCADIA_PYTHON", "USE_SYSTEM_PYTHON"}
	testSizeTimeouts        = map[string]int{"small": 60, "medium": 600, "large": 3600}
	testSizeNames           = map[string]string{"SMALL": "small", "MEDIUM": "medium", "LARGE": "large"}
	testPartitionModes      = map[string]string{"SEQUENTIAL": "SEQUENTIAL", "MODULO": "MODULO"}
	testSizeKinds           = map[string]ProcKind{"small": pkTS, "medium": pkTM, "large": pkTL}
	testCommonEnvSkip       = []string{"YA_CACHE_DIR", "YA_TIMEOUT", "YA_TOKEN", "YA_SOURCE_ROOT"}
	testSanitizerOptVars    = []string{"LSAN_OPTIONS", "ASAN_OPTIONS", "UBSAN_OPTIONS", "MSAN_OPTIONS", "TSAN_OPTIONS"}
	testSymbolizerVars      = []string{"ASAN_SYMBOLIZER_PATH", "LSAN_SYMBOLIZER_PATH", "MSAN_SYMBOLIZER_PATH", "TSAN_SYMBOLIZER_PATH", "UBSAN_SYMBOLIZER_PATH"}
	testAgentEnvSignatures  = []string{"OPENCODE", "CLAUDECODE", "CODEX_THREAD_ID", "CURSOR_TRACE_ID", "CODEIUM_EDITOR_APP_ROOT", "AIDER_API_KEY", "AMP_CURRENT_THREAD_ID", "ANTIGRAVITY_AGENT", "AUGMENT_AGENT", "GEMINI_CLI", "QWEN_CODE", "REPL_ID"}
	testAgentProcessNames   = []string{"opencode", "claude", "claude-code", "copilot", "codex-app", "codex", "hermes", "openclaw", "droid", "pi"}
)

const (
	testOutputStyleNinja      = "ninja"
	testOutputStyleJsonl      = "jsonl"
	testNodeFinishingTime     = 5 * 60
	testDistResultFileLimit   = 100 * 1024
	testDefaultTestsLimit     = 100000
	testOutputCompression     = "zstd"
	testOutputCompressionLvl  = "1"
	testDefaultRAMRequirement = 8
)

type TestOpts struct {
	dist        bool
	removeTos   bool
	commandFile bool
	outputStyle string
	sizes       []string
	descriptor  string
	context     []string
	contextReqs *Requirements
	commonEnv   [][2]string
	contextRefs map[*Platform]NodeRef
}

type TestSuite struct {
	Project       string
	Name          string
	Type          string
	CIType        string
	Size          string
	Timeout       int
	Related       []VFS
	Inputs        []VFS
	Deps          []NodeRef
	Globals       []ResourceDecl
	Tags          []string
	Env           []string
	RunCwd        string
	Canonization  bool
	TestParams    bool
	SmoothSignals []string
	RunCmd        func(workDir string) []ANY
}

func testSuiteWorkDir(project, name string) string {
	return path.Join(project, "test-results", name)
}

func newTestOpts(mf *MakeFlags, yaConf map[string]any) *TestOpts {
	opts := &TestOpts{
		dist:        mf.dist,
		removeTos:   true,
		commandFile: yaConf["use_command_file_in_testtool"] == true,
		outputStyle: testOutputStyleNinja,
		sizes:       mf.testSizes,
	}

	if v, ok := yaConf["dir_outputs"].(bool); ok {
		opts.removeTos = v
	}

	if v, ok := os.LookupEnv("YA_DIR_OUTPUTS"); ok {
		opts.removeTos = envEnabled(v)
	}

	autoDetect := yaConf["auto_detect_agent"] == true

	if v, ok := os.LookupEnv("YA_AUTO_DETECT_AGENT"); ok {
		autoDetect = envEnabled(v)
	}

	if style, ok := yaConf["output_style"].(string); ok {
		opts.outputStyle = style
	} else if autoDetect && launchedByAgent() {
		opts.outputStyle = testOutputStyleJsonl
	}

	opts.contextReqs = &Requirements{Network: nwRestricted}

	if reqs, ok := yaConf["default_node_requirements"].(string); ok {
		var parsed map[string]any

		throw(json.Unmarshal([]byte(reqs), &parsed))

		cpu, _ := parsed["cpu"].(float64)
		ram, _ := parsed["ram"].(float64)

		opts.contextReqs = &Requirements{CPU: cpu, RAM: ram, Network: nwRestricted}
	}

	opts.descriptor = testPlatformDescriptor(mf)
	opts.context = testContextLines(mf)

	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")

		if k == "SVN_SSH" || strings.HasPrefix(k, "YA_") && !slices.ContainsFunc(testCommonEnvSkip, func(skip string) bool { return strings.HasPrefix(k, skip) }) {
			opts.commonEnv = append(opts.commonEnv, [2]string{k, v})
		}
	}

	return opts
}

func testPlatformDescriptor(mf *MakeFlags) string {
	buildType := mf.buildType
	targetSpec := mf.targetPlat

	if targetSpec == "" {
		targetSpec = string(makePlatformID(hostOS(), hostISA()))
	}

	tOS, tISA := resolvePlatform(targetSpec)
	flags := map[string]string{}

	for k, v := range mf.platformFlags {
		flags[k] = v
	}

	for _, k := range []string{"MUSL", "ALLOCATOR", "FAKEID", "RACE"} {
		if v, ok := mf.tflags[k]; ok {
			flags[k] = v
		}
	}

	if mf.sandboxing {
		flags["FAKEID"] = "sandboxing"
		flags["SANDBOXING"] = "yes"
	}

	var tags []string

	for k, v := range flags {
		if tag := testFmtTag(k, v); tag != "" {
			tags = append(tags, tag)
		}
	}

	slices.Sort(tags)

	return strings.Join(append([]string{string(makePlatformID(tOS, tISA)), buildType}, tags...), "-")
}

func testFmtTag(k, v string) string {
	if k == "SANITIZER_TYPE" {
		return v[:1] + "san"
	}

	mapping := map[string]string{"USE_LTO": "lto", "USE_THINLTO": "thinlto", "MUSL": "musl", "USE_AFL": "AFL", "RACE": "race"}

	if name, ok := mapping[k]; ok {
		if envEnabled(v) {
			return name
		}

		return ""
	}

	return k + "=" + v
}

func testContextLines(mf *MakeFlags) []string {
	flags := map[string]string{}

	for k, v := range mf.platformFlags {
		flags[k] = v
	}

	for k, v := range mf.tflags {
		flags[k] = v
	}

	if mf.runTests > 0 {
		flags["TESTS_REQUESTED"] = "yes"
	}

	allowed := map[string]string{}

	for _, k := range testContextAllowedFlags {
		if v, ok := flags[k]; ok {
			allowed[k] = v
		}
	}

	context := map[string]any{"build_type": mf.buildType, "flags": allowed}
	text := throw2(json.MarshalIndent(context, "", "  "))

	return strings.Split(string(text), "\n")
}

func launchedByAgent() bool {
	for _, name := range testAgentEnvSignatures {
		if _, ok := os.LookupEnv(name); ok {
			return true
		}
	}

	for pid := os.Getpid(); pid > 1; {
		stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")

		if err != nil {
			return false
		}

		s := string(stat)
		open, closing := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')

		if open < 0 || closing < open {
			return false
		}

		fields := strings.Fields(s[closing+1:])

		if len(fields) < 2 {
			return false
		}

		name := strings.ToLower(s[open+1 : closing])
		exe, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")

		if cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline"); err == nil && len(name) >= 15 {
			if full := strings.ToLower(path.Base(strings.SplitN(string(cmdline), "\x00", 2)[0])); strings.HasPrefix(full, name) {
				name = full
			}
		}

		if slices.Contains(testAgentProcessNames, path.Base(name)) || strings.Contains(strings.ToLower(exe), "/claude/") {
			return true
		}

		pid, _ = strconv.Atoi(fields[1])
	}

	return false
}

func envEnabled(v string) bool {
	switch strings.ToLower(v) {
	case "1", "y", "yes", "t", "true", "on":
		return true
	}

	return false
}

func (ctx *GenCtx) testContextRef(p *Platform) NodeRef {
	opts := ctx.testOpts

	if ref, ok := opts.contextRefs[p]; ok {
		return ref
	}

	na := ctx.na
	args := []ANY{internStr(testYMakePython3).any(), source(testAppendFileScriptRel).any(), bldCommonTestContext.any()}

	for _, line := range opts.context {
		args = append(args, internStr(line).any())
	}

	ref := ctx.emit.emit(&Node{
		Platform:     p,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(args)}),
		Inputs:       na.inputList(na.vfsList(source(testAppendFileScriptRel))),
		KV:           &testNodesKV,
		Outputs:      na.vfsList(bldCommonTestContext),
		Requirements: opts.contextReqs,
		Resources:    usesPython3,
	})

	if opts.contextRefs == nil {
		opts.contextRefs = map[*Platform]NodeRef{}
	}

	opts.contextRefs[p] = ref

	return ref
}

func (ctx *GenCtx) emitTestSuite(p *Platform, s *TestSuite) (NodeRef, bool) {
	opts := ctx.testOpts

	if len(opts.sizes) > 0 && !slices.Contains(opts.sizes, s.Size) {
		return 0, false
	}

	na := ctx.na
	wd := testSuiteWorkDir(s.Project, s.Name)
	meta, trace, runLog, tar := build(wd, "/meta.json"), build(wd, "/ytest.report.trace"), build(wd, "/run_test.log"), build(wd, "/testing_out_stuff.tar.zstd")
	args := []ANY{internStr(testToolHostPath).any(), argRunTest.any()}

	push := func(xs ...string) {
		for _, x := range xs {
			args = append(args, internStr(x).any())
		}
	}

	if opts.commandFile {
		args = append(args, argYaStartCommandFile.any())
	}

	args = append(args, argMeta.any(), meta.any(), argTrace.any(), trace.any())
	push("--timeout", strconv.Itoa(s.Timeout))
	args = append(args, argLogPath.any(), runLog.any())
	push("--test-size", s.Size, "--test-type", s.Type, "--test-ci-type", s.CIType)
	args = append(args, argContextFilename.any(), bldCommonTestContext.any(), argSourceRoot.any(), strS.any(), argBuildRoot.any(), strB.any())
	push("--test-suite-name", s.Name, "--project-path", s.Project)

	for _, rp := range s.Related {
		args = append(args, argTestRelatedPath.any(), rp.any())
	}

	push("--target-platform-descriptor", opts.descriptor)

	if opts.removeTos {
		push("--remove-tos")
	}

	push("--gdb-path", testGDBPath)

	if opts.dist {
		push("--log-level", "DEBUG", "--result-max-file-size", strconv.Itoa(testDistResultFileLimit), "--disable-memory-snippet", "--truncate-files", "--propagate-timeout-info")
	} else {
		push("--result-max-file-size", "0")
	}

	push("--verify-results", "--tests-limit-in-chunk", strconv.Itoa(testDefaultTestsLimit), "--output-style", opts.outputStyle, "--python-bin", testPythonBin)

	if s.RunCwd != "" {
		args = append(args, strTestRunCwd.any(), source(s.RunCwd).any())
	}

	for _, tag := range s.Tags {
		push("--tag", tag)
	}

	for _, e := range s.Env {
		push("--env", e)
	}

	if s.Canonization {
		push("--supports-canonization")
	}

	if s.TestParams {
		push("--supports-test-parameters")
	}

	for _, sig := range s.SmoothSignals {
		push("--smooth-shutdown-signals", sig)
	}

	push("--compression-filter", testOutputCompression, "--compression-level", testOutputCompressionLvl)

	for _, g := range sortedResourceGlobals(s.Globals) {
		args = append(args, argGlobalResource.any(), g.Token.any())
	}

	if opts.dist {
		push("--node-timeout", strconv.Itoa(s.Timeout+testNodeFinishingTime))
	}

	push("--ram-limit-gb", strconv.Itoa(testDefaultRAMRequirement))
	args = append(args, argTar.any(), tar.any())
	args = append(args, s.RunCmd(wd)...)

	if opts.commandFile {
		args = append(args, argYaEndCommandFile.any())
	}

	kind, ok := testSizeKinds[s.Size]

	if !ok {
		throwFmt("test %s/%s: unknown size %q", s.Project, s.Name, s.Size)
	}

	deps := append(slices.Clone(s.Deps), ctx.testContextRef(p))

	return ctx.emit.emit(&Node{
		Platform: p,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(args), Cwd: bldRootDirVFS}),
		Env:      ctx.testEnv(s),
		Inputs:   na.inputList(s.Inputs),
		KV: &KV{
			P:                kind,
			Path:             path.Join(s.Project, s.Name),
			PC:               pcYellow,
			RunTestNode:      true,
			ShowOutBool:      true,
			HasSpecialRunner: true,
		},
		Outputs:      na.vfsList(meta, trace, runLog, tar),
		Requirements: &Requirements{CPU: 1, RAM: testDefaultRAMRequirement, Network: nwRestricted, HasRAMDisk: true},
		DepRefs:      deps,
	}), true
}

func (ctx *GenCtx) testEnv(s *TestSuite) EnvVars {
	opts := ctx.testOpts
	env := map[string]string{}
	mandatory := map[string]bool{}

	for _, kv := range opts.commonEnv {
		env[kv[0]] = kv[1]
	}

	for _, name := range strings.Split(env["YA_MANDATORY_ENV_VARS"], ":") {
		if name != "" {
			mandatory[name] = true
		}
	}

	env["ARCADIA_BUILD_ROOT"] = testBuildRoot
	env["ARCADIA_ROOT_DISTBUILD"] = testSourceRoot
	env["ARCADIA_SOURCE_ROOT"] = testSourceRoot
	env["TEST_NAME"] = s.Name
	env["YA_TEST_RUNNER"] = "1"
	env["GORACE"] = "halt_on_error=1"

	extend := func(name, val string) {
		if prev := env[name]; prev != "" {
			val = prev + ":" + val
		}

		env[name] = val
		mandatory[name] = true
	}

	for _, name := range testSanitizerOptVars {
		extend(name, "exitcode=100")
	}

	extend("UBSAN_OPTIONS", "print_stacktrace=1,halt_on_error=1")
	extend("MSAN_OPTIONS", "report_umrs=1")

	env["YA_PYTHON_BIN"] = testPythonBin

	for _, name := range testSymbolizerVars {
		env[name] = testClangSymbolizerPath
		mandatory[name] = true
	}

	env["YA_CXX"] = testClangCXXPath
	env["YA_CC"] = testClangCCPath

	if !opts.dist {
		env["TESTING_SAVE_OUTPUT"] = "yes"
	}

	mandatory["YA_MANDATORY_ENV_VARS"] = true

	names := make([]string, 0, len(mandatory))

	for name := range mandatory {
		names = append(names, name)
	}

	slices.Sort(names)

	env["YA_MANDATORY_ENV_VARS"] = strings.Join(names, ":")

	keys := make([]string, 0, len(env))

	for k := range env {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	out := make(EnvVars, 0, len(keys))

	for _, k := range keys {
		out = append(out, EnvVar{Name: internEnv(k), Value: internStr(env[k]).any()})
	}

	return out
}
