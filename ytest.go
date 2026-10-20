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
	testContextAllowedFlags  = []string{"AUTOCHECK", "TESTS_REQUESTED", "USE_ARCADIA_PYTHON", "USE_SYSTEM_PYTHON"}
	testDescriptorExtraFlags = []string{"MUSL", "ALLOCATOR", "FAKEID", "RACE"}
	testDescriptorFlagTags   = map[string]string{"USE_LTO": "lto", "USE_THINLTO": "thinlto", "MUSL": "musl", "USE_AFL": "AFL", "RACE": "race"}
	testSizeTimeouts         = map[string]int{"small": 60, "medium": 600, "large": 3600}
	testSizeNames            = map[string]string{"SMALL": "small", "MEDIUM": "medium", "LARGE": "large"}
	testPartitionModes       = map[string]string{"SEQUENTIAL": "SEQUENTIAL", "MODULO": "MODULO"}
	testSizeKinds            = map[string]ProcKind{"small": pkTS, "medium": pkTM, "large": pkTL}
	testCommonEnvSkip        = []string{"YA_CACHE_DIR", "YA_TIMEOUT", "YA_TOKEN", "YA_SOURCE_ROOT"}
	testSanitizerOptVars     = []string{"LSAN_OPTIONS", "ASAN_OPTIONS", "UBSAN_OPTIONS", "MSAN_OPTIONS", "TSAN_OPTIONS"}
	testSymbolizerVars       = []string{"ASAN_SYMBOLIZER_PATH", "LSAN_SYMBOLIZER_PATH", "MSAN_SYMBOLIZER_PATH", "TSAN_SYMBOLIZER_PATH", "UBSAN_SYMBOLIZER_PATH"}
	testAgentEnvSignatures   = []string{"OPENCODE", "CLAUDECODE", "CODEX_THREAD_ID", "CURSOR_TRACE_ID", "CODEIUM_EDITOR_APP_ROOT", "AIDER_API_KEY", "AMP_CURRENT_THREAD_ID", "ANTIGRAVITY_AGENT", "AUGMENT_AGENT", "GEMINI_CLI", "QWEN_CODE", "REPL_ID"}
	testAgentProcessNames    = []string{"opencode", "claude", "claude-code", "copilot", "codex-app", "codex", "hermes", "openclaw", "droid", "pi"}
	testNoPlatform           = &Platform{}
)

const (
	testNodeFinishingTime   = 300
	testCmdFileStart        = "--ya-start-command-file"
	testCmdFileEnd          = "--ya-end-command-file"
	testOutputStyleNinja    = "ninja"
	testOutputStyleJsonl    = "jsonl"
	testDistResultFileLimit = 100 * 1024
	testDefaultTestsLimit   = 100000
)

type TestOpts struct {
	dist         bool
	removeTos    bool
	dirOutputs   bool
	commandFile  bool
	jstyleServer bool
	outputStyle  string
	sizes        []string
	descriptor   string
	context      []string
	contextReqs  *Requirements
	commonEnv    [][2]string
	contextRef   NodeRef
}

func newTestOpts(mf *MakeFlags, yaConf map[string]any) *TestOpts {
	if mf.testLevel == 0 {
		return nil
	}

	opts := &TestOpts{
		dist:         mf.dist,
		removeTos:    yaConf["remove_tos"] == true || os.Getenv("YA_TEST_REMOVE_TOS") != "",
		dirOutputs:   true,
		commandFile:  yaConf["use_command_file_in_testtool"] == true,
		jstyleServer: yaConf["use_jstyle_server"] == true,
		outputStyle:  testOutputStyleNinja,
		sizes:        mf.testSizes,
		descriptor:   testPlatformDescriptor(mf),
		context:      testContextLines(mf),
		contextReqs:  &Requirements{Network: nwRestricted},
	}

	if v, ok := yaConf["dir_outputs"].(bool); ok {
		opts.dirOutputs = v
	}

	if v, ok := os.LookupEnv("YA_DIR_OUTPUTS"); ok {
		opts.dirOutputs = envEnabled(v)
	}

	if opts.dirOutputs {
		opts.removeTos = true
		opts.dirOutputs = !mf.dist && !mf.sandboxing && !mf.cacheTests
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

	if reqs, ok := yaConf["default_node_requirements"].(string); ok {
		var parsed map[string]any

		throw(json.Unmarshal([]byte(reqs), &parsed))

		cpu, _ := parsed["cpu"].(float64)
		ram, _ := parsed["ram"].(float64)

		opts.contextReqs = &Requirements{CPU: cpu, RAM: ram, Network: nwRestricted}
	}

	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")

		if k == "SVN_SSH" || strings.HasPrefix(k, "YA_") && !slices.ContainsFunc(testCommonEnvSkip, func(skip string) bool { return strings.HasPrefix(k, skip) }) {
			opts.commonEnv = append(opts.commonEnv, [2]string{k, v})
		}
	}

	return opts
}

func testPlatformDescriptor(mf *MakeFlags) string {
	targetSpec := mf.targetPlat

	if targetSpec == "" {
		targetSpec = string(makePlatformID(hostOS(), hostISA()))
	}

	tOS, tISA := resolvePlatform(targetSpec)
	flags := map[string]string{}

	for k, v := range mf.platformFlags {
		flags[k] = v
	}

	for _, k := range testDescriptorExtraFlags {
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

	return strings.Join(append([]string{string(makePlatformID(tOS, tISA)), mf.buildType}, tags...), "-")
}

func testFmtTag(k, v string) string {
	if k == "SANITIZER_TYPE" {
		return v[:1] + "san"
	}

	if name, ok := testDescriptorFlagTags[k]; ok {
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

	for k, v := range mf.dflags {
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

func testArgs(ss []string) []ANY {
	out := make([]ANY, len(ss))

	for i, s := range ss {
		if strings.HasPrefix(s, "$(S)/") || strings.HasPrefix(s, "$(B)/") {
			out[i] = intern(s).any()
		} else {
			out[i] = internStr(s).any()
		}
	}

	return out
}

func (o *TestOpts) sizeRequested(size string) bool {
	return len(o.sizes) == 0 || slices.Contains(o.sizes, size)
}

type TestSuite struct {
	p                      *Platform
	projectPath            string
	name                   string
	testType               string
	ciType                 string
	size                   string
	timeout                int
	relatedPaths           []string
	runInputs              []VFS
	deps                   []NodeRef
	env                    []string
	tags                   []string
	runCwd                 string
	setupPythonpathEnv     bool
	supportsCanonization   bool
	supportsTestParameters bool
	smoothShutdown         []string
	globals                []ResourceDecl
	requirements           map[string]string
	network                string
	split                  int
	prepare                [][]ANY
	prepareInputs          []VFS
	runCmd                 func(workDir string, split, index int) []ANY
}

func (s *TestSuite) workDir(split, index int) string {
	dir := path.Join("$(B)", s.projectPath, "test-results", s.name)

	if split > 1 {
		dir += "/chunk" + strconv.Itoa(index)
	}

	return dir
}

func (ctx *GenCtx) testContext() NodeRef {
	o := ctx.tests

	if o.contextRef != 0 {
		return o.contextRef
	}

	na := ctx.na
	args := []ANY{internStr(testYMakePython3).any(), source(testAppendFileScriptRel).any(), bldCommonTestContext.any()}

	for _, line := range o.context {
		args = append(args, internStr(line).any())
	}

	o.contextRef = ctx.emit.emit(&Node{
		Platform:     ctx.target,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(args)}),
		Inputs:       na.inputList(na.vfsList(source(testAppendFileScriptRel))),
		KV:           &testNodesKV,
		Outputs:      na.vfsList(bldCommonTestContext),
		Requirements: o.contextReqs,
		Resources:    usesPython3,
	})

	return o.contextRef
}

func (ctx *GenCtx) emitTestSuite(s *TestSuite) []NodeRef {
	if !ctx.tests.sizeRequested(s.size) {
		return nil
	}

	split := max(s.split, 1)

	if split == 1 {
		return []NodeRef{ctx.emit.emit(ctx.testNode(s, 1, 0))}
	}

	chunks := make([]*Node, split)
	refs := make([]NodeRef, split)

	for i := range chunks {
		chunks[i] = ctx.testNode(s, split, i)
		refs[i] = ctx.emit.emit(chunks[i])
	}

	return []NodeRef{ctx.emit.emit(ctx.testAccumulator(s, chunks, refs))}
}

func (ctx *GenCtx) testAccumulator(s *TestSuite, chunks []*Node, refs []NodeRef) *Node {
	o := ctx.tests
	na := ctx.na
	work := s.workDir(1, 0)
	log := work + "/results_accumulator.log"

	strs := []string{testToolHostPath, "results_accumulator", "--accumulator-path", work, "--source-root", "$(S)", "--log-path", log,
		"--concatenate-binaries", "report.exec", "--concatenate-binaries", "operation.log"}

	if o.dist {
		strs = append(strs, "--truncate", "--log-level", "DEBUG")
	}

	strs = append(strs, "--gdb-path", testGDBPath, "--tests-limit-in-suite", strconv.Itoa(testDefaultTestsLimit), "--keep-paths", testCmdFileStart)

	outputs := []string{work + "/meta.json", work + "/run_test.log", work + "/ytest.report.trace"}

	var inputs []VFS

	for i := range chunks {
		dir := s.workDir(len(chunks), i)

		strs = append(strs, "--output", dir)
		inputs = append(inputs, chunks[i].Outputs...)

		if !o.dirOutputs {
			outputs = append(outputs, dir+"/testing_out_stuff.tar.zstd")
		}
	}

	strs = append(strs, testCmdFileEnd)

	if o.dirOutputs {
		strs = append(strs, "--skip-dir", "testing_out_stuff", "--skip-file", "yt_run_test.tar")
	} else {
		strs = append(strs, "--skip-file", "testing_out_stuff.tar.zstd", "--skip-file", "yt_run_test.tar")
	}

	outs := make([]VFS, 0, len(outputs)+1)

	for _, out := range sortedUnique(outputs) {
		outs = append(outs, intern(out))
	}

	slices.SortFunc(inputs, func(a, b VFS) int { return strings.Compare(a.string(), b.string()) })

	return &Node{
		Platform: testNoPlatform,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(testArgs(strs)...)), Cwd: bld}),
		Env:      sortedEnvVars(o.commonEnvMap()),
		Inputs:   na.inputList(na.vfsList(slices.Compact(inputs)...)),
		KV:       &KV{P: pkTA, PC: pcLightCyan, ShowOutBool: true},
		Outputs:  na.vfsList(append(outs, intern(log))...),
		DepRefs:  na.refList(dedupRefs(refs)...),
	}
}

func (ctx *GenCtx) testNode(s *TestSuite, split, index int) *Node {
	o := ctx.tests
	na := ctx.na
	work := s.workDir(split, index)
	meta, trace, log := work+"/meta.json", work+"/ytest.report.trace", work+"/run_test.log"
	tar := work + "/testing_out_stuff.tar.zstd"
	outputs := []VFS{intern(meta), intern(trace), intern(log)}
	strs := []string{testToolHostPath, "run_test"}

	if o.commandFile {
		strs = append(strs, testCmdFileStart)
	}

	strs = append(strs,
		"--meta", meta, "--trace", trace, "--timeout", strconv.Itoa(s.timeout), "--log-path", log,
		"--test-size", s.size, "--test-type", s.testType, "--test-ci-type", s.ciType,
		"--context-filename", bldCommonTestContext.string(),
		"--source-root", "$(S)", "--build-root", "$(B)", "--test-suite-name", s.name, "--project-path", s.projectPath)

	related := dedupKeepOrder(s.relatedPaths)

	for _, p := range related {
		strs = append(strs, "--test-related-path", p)
	}

	strs = append(strs, "--target-platform-descriptor", o.descriptor)

	if o.removeTos {
		strs = append(strs, "--remove-tos")
	}

	strs = append(strs, "--gdb-path", testGDBPath)

	if o.dist {
		strs = append(strs, "--log-level", "DEBUG", "--result-max-file-size", strconv.Itoa(testDistResultFileLimit), "--disable-memory-snippet", "--truncate-files", "--propagate-timeout-info")
	} else {
		strs = append(strs, "--result-max-file-size", "0")
	}

	strs = append(strs, "--verify-results", "--tests-limit-in-chunk", strconv.Itoa(testDefaultTestsLimit), "--output-style", o.outputStyle, "--python-bin", testPythonBin)

	if s.runCwd != "" {
		strs = append(strs, "--test-run-cwd", s.runCwd)
	}

	for _, tag := range s.tags {
		strs = append(strs, "--tag", tag)
	}

	if _, ok := s.requirements["ram_disk"]; ok {
		strs = append(strs, "--requires-ram-disk")
	}

	for _, e := range s.env {
		for _, g := range s.globals {
			name, value, _ := strings.Cut(g.Token.string(), "::")

			e = strings.ReplaceAll(e, "$"+name, value)
		}

		if !strings.Contains(e, "=") {
			v, ok := os.LookupEnv(e)

			if !ok {
				continue
			}

			e += "=" + v
		}

		strs = append(strs, "--env", e)
	}

	if s.setupPythonpathEnv {
		strs = append(strs, "--setup-pythonpath-env")
	}

	if s.supportsCanonization {
		strs = append(strs, "--supports-canonization")
	}

	if s.supportsTestParameters {
		strs = append(strs, "--supports-test-parameters")
	}

	for _, sig := range s.smoothShutdown {
		strs = append(strs, "--smooth-shutdown-signals", sig)
	}

	if split > 1 {
		strs = append(strs, "--split-count", strconv.Itoa(split), "--split-index", strconv.Itoa(index))
	}

	strs = append(strs, "--compression-filter", "zstd", "--compression-level", "1")

	head := testArgs(strs)

	for _, g := range sortedResourceGlobals(s.globals) {
		head = append(head, argGlobalResource.any(), g.Token.any())
	}

	if o.dist {
		head = append(head, argNodeTimeout.any(), internStr(strconv.Itoa(s.timeout+testNodeFinishingTime)).any())
	}

	req := testRequirements(s)

	if req.RAM != 0 {
		head = append(head, argRamLimitGb.any(), internStr(strconv.Itoa(int(req.RAM))).any())
	}

	if o.dirOutputs {
		head = append(head, argDirOutputs.any())
	} else {
		head = append(head, argTar.any(), intern(tar).any())
		outputs = append(outputs, intern(tar))
	}

	head = append(head, s.runCmd(work, split, index)...)

	if o.commandFile {
		head = append(head, internArg(testCmdFileEnd).any())
	}

	cmds := make([]Cmd, 0, len(s.prepare)+1)

	for _, p := range s.prepare {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(p...))})
	}

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(head...)), Cwd: bld})

	inputs := append(append([]VFS{}, s.runInputs...), s.prepareInputs...)

	for _, p := range related {
		inputs = append(inputs, intern(p))
	}

	slices.SortFunc(inputs, func(a, b VFS) int { return strings.Compare(a.string(), b.string()) })

	kv := &KV{P: testSizeKinds[s.size], Path: s.projectPath + "/" + s.name, PC: pcYellow, RunTestNode: true, ShowOutBool: true, HasSpecialRunner: true}

	if split > 1 {
		kv.SplitIndex, kv.HasSplitIndex = index, true
	}

	deps := append([]NodeRef{ctx.testContext()}, s.deps...)

	return &Node{
		Platform:     s.p,
		Cmds:         na.cmdList(cmds...),
		Env:          ctx.testEnv(s),
		Inputs:       na.inputList(na.vfsList(slices.Compact(inputs)...)),
		KV:           kv,
		Outputs:      na.vfsList(outputs...),
		Requirements: req,
		DepRefs:      na.refList(dedupRefs(deps)...),
	}
}

func testRequirements(s *TestSuite) *Requirements {
	cpu, ram := 1, 8
	r := &Requirements{Network: nwRestricted, HasRAMDisk: true}

	if s.network == "full" || s.size == "large" {
		r.Network = nwFull
	}

	if v, ok := s.requirements["ram"]; ok {
		ram, _ = strconv.Atoi(v)
	}

	if v, ok := s.requirements["cpu"]; ok {
		cpu, _ = strconv.Atoi(v)
	} else if v, ok := s.requirements["ram"]; ok {
		n, _ := strconv.Atoi(v)

		switch {
		case n >= 12:
			cpu = 4
		case n >= 8:
			cpu = 3
		case n >= 4:
			cpu = 2
		}
	}

	if v, ok := s.requirements["network"]; ok {
		r.Network = nwRestricted

		if v == "full" {
			r.Network = nwFull
		}
	}

	if v, ok := s.requirements["ram_disk"]; ok {
		n, _ := strconv.Atoi(v)

		r.RAMDisk = float64(n)
	}

	r.CPU, r.RAM = float64(cpu), float64(ram)

	return r
}

func (o *TestOpts) commonEnvMap() map[string]string {
	env := make(map[string]string, len(o.commonEnv))

	for _, kv := range o.commonEnv {
		env[kv[0]] = kv[1]
	}

	return env
}

func (ctx *GenCtx) testEnv(s *TestSuite) EnvVars {
	env := ctx.tests.commonEnvMap()
	mandatory := map[string]bool{"YA_MANDATORY_ENV_VARS": true}

	for _, name := range strings.Split(env["YA_MANDATORY_ENV_VARS"], ":") {
		if name != "" {
			mandatory[name] = true
		}
	}

	env["ARCADIA_BUILD_ROOT"] = testBuildRoot
	env["ARCADIA_ROOT_DISTBUILD"] = testSourceRoot
	env["ARCADIA_SOURCE_ROOT"] = testSourceRoot
	env["TEST_NAME"] = s.name
	env["YA_TEST_RUNNER"] = "1"
	env["GORACE"] = "halt_on_error=1"

	extend := func(name, val string) {
		prev, ok := env[name]

		if !ok {
			prev = os.Getenv(name)
		}

		if prev != "" {
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

	if !ctx.tests.dist {
		env["TESTING_SAVE_OUTPUT"] = "yes"
	}

	names := make([]string, 0, len(mandatory))

	for name := range mandatory {
		names = append(names, name)
	}

	slices.Sort(names)

	env["YA_MANDATORY_ENV_VARS"] = strings.Join(names, ":")

	return sortedEnvVars(env)
}

func sortedEnvVars(env map[string]string) EnvVars {
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

func testRequirementsOf(entries []string) map[string]string {
	out := map[string]string{}

	for _, e := range entries {
		if k, v, ok := strings.Cut(e, ":"); ok {
			out[strings.ToLower(k)] = strings.ToLower(v)
		}
	}

	return out
}
