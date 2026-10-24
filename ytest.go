package main

import (
	"encoding/base64"
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
	retries      int
	traceback    string
	buildType    string
	flags        []string
	dist         bool
	cacheTests   bool
	removeTos    bool
	dirOutputs   bool
	commandFile  bool
	jstyleServer bool
	outputStyle  string
	sizes        []string
	tagFilter    TestTagFilter
	descriptor   string
	context      []string
	contextReqs  *Requirements
	commonEnv    [][2]string
	contextRef   NodeRef

	dirOutputsInNodes bool
}

func newTestOpts(mf *MakeFlags, yaConf map[string]any) *TestOpts {
	if mf.testLevel == 0 {
		return nil
	}

	opts := &TestOpts{
		retries:      max(mf.testRetries, 1),
		traceback:    mf.testTraceback,
		buildType:    mf.testBuildType,
		flags:        testFlags(mf),
		dist:         mf.dist,
		cacheTests:   mf.cacheTests,
		removeTos:    yaConf["remove_tos"] == true || os.Getenv("YA_TEST_REMOVE_TOS") != "",
		dirOutputs:   true,
		commandFile:  yaConf["use_command_file_in_testtool"] == true,
		jstyleServer: yaConf["use_jstyle_server"] == true,
		outputStyle:  testOutputStyleNinja,
		sizes:        mf.testSizes,
		tagFilter:    newTestTagFilter(mf),
		descriptor:   testPlatformDescriptor(mf),
		context:      testContextLines(mf),
		contextReqs:  &Requirements{Network: nwRestricted},
	}

	opts.dirOutputsInNodes = yaConf["dir_outputs_in_nodes"] == true

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

	if v, ok := os.LookupEnv("YA_DIR_OUTPUTS_IN_NODES"); ok {
		opts.dirOutputsInNodes = envEnabled(v)
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
		stat, err := chaosReadFile("/proc/" + strconv.Itoa(pid) + "/stat")

		if err != nil {
			return false
		}

		s := string(stat)
		open, closing := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
		fields := strings.Fields(s[closing+1:])
		name := strings.ToLower(s[open+1 : closing])
		exe, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")

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
	dockerImages           []string
	depends                []string
	dependencyProjects     []VFS
	data                   []string
	externalLocalFiles     []string
	recipes                string
	files                  []string
	pythonNoBytecode       bool
	disableLeaks           bool
	importCheckEnv         bool
	checkFiles             []VFS
	styleCheck             *StyleCheck
	p                      *Platform
	projectPath            string
	name                   string
	testType               string
	ciType                 string
	size                   string
	timeout                int
	relatedPaths           []string
	runInputs              []VFS
	inputNames             []string
	uidSalt                string
	noCleanEnvironment     bool
	clearEnvironment       bool
	noRetries              bool
	deps                   []NodeRef
	projectBuildDeps       bool
	env                    []string
	tags                   []string
	runCwd                 string
	canonizeSubPath        string
	canonicalData          bool
	execPath               string
	pytestOutputDirs       []string
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
	runCmd                 func(workDir string, chunk TestChunk) []ANY
}

type TestChunk struct {
	split, index, retry int
	file                string
}

func (s *TestSuite) workDir(chunk TestChunk) string {
	dir := path.Join("$(B)", s.projectPath, "test-results", s.name)

	if chunk.retry > 0 {
		dir += "/run" + strconv.Itoa(chunk.retry)
	}

	if chunk.file != "" {
		dir = path.Join(dir, strings.TrimSuffix(chunk.file, path.Ext(chunk.file)))
	}

	if chunk.split > 1 {
		dir += "/chunk" + strconv.Itoa(chunk.index)
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

func (ctx *GenCtx) prepareTestDependencies(s *TestSuite) {
	var execDirs []string

	python := s.testType == "pytest" || s.testType == "py3test" || s.testType == "exectest"

	if python {
		s.pytestOutputDirs = append(s.pytestOutputDirs, s.projectPath)
	}

	if s.testType == "exectest" {
		execDirs = append(execDirs, testExecDependencyDir(s.projectPath))
	}

	var rewritten []string

	for _, dep := range s.depends {
		s.dependencyProjects = append(s.dependencyProjects, source(dep))

		for _, dependency := range ctx.runtimeTestDependencies(dep) {
			instance, res := dependency.instance, dependency.result
			output := testDependencyOutput(res)
			reg := ctx.scannerFor(instance).codegen

			reg.use(output)

			info, _ := reg.byRel.get(output.rel())

			s.deps = append(s.deps, info.ProducerRef)
			rewritten = append(rewritten, output.relString())

			if s.testType == "exectest" {
				execDirs = append(execDirs, testExecDependencyDir(output.relString()))
			}

			if python {
				s.pytestOutputDirs = append(s.pytestOutputDirs, ctx.runtimeTestOutputDirs(dependency, map[*ModuleEmitResult]bool{})...)
			}
		}
	}

	s.depends = sortedUnique(rewritten)

	if python {
		s.pytestOutputDirs = sortedUnique(s.pytestOutputDirs)
	}

	if s.testType == "exectest" {
		s.execPath = strings.Join(sortedUnique(execDirs), ":")
	}
}

func (ctx *GenCtx) testSuiteSelected(s *TestSuite) bool {
	return ctx.tests.sizeRequested(s.size) && ctx.tests.tagFilter.matches(s.tags)
}

func (ctx *GenCtx) emitTestSuite(s *TestSuite) []NodeRef {
	if !ctx.testSuiteSelected(s) {
		return nil
	}

	for _, image := range s.dockerImages {
		s.deps = append(s.deps, ctx.emitDockerImage(image))
	}

	if s.canonicalData {
		s.data = sortedUnique(append(slices.Clone(s.data), ctx.canonicalTestResources(s)...))
	}

	for _, entry := range s.data {
		if strings.HasPrefix(entry, "sbr://") {
			s.deps = append(s.deps, ctx.emitTestData(entry))
		} else if resource, ok := strings.CutPrefix(entry, "mds:"); ok && s.supportsCanonization {
			s.deps = append(s.deps, ctx.emitCanonicalTestData(resource))
		} else if file, ok := strings.CutPrefix(entry, "ext:"); ok {
			rel := path.Join(s.projectPath, strings.TrimSuffix(file, ".external"))

			if present, isDir := ctx.fs.exists(srcRootRel, rel); present && !isDir {
				s.externalLocalFiles = append(s.externalLocalFiles, rel)
				s.deps = append(s.deps, ctx.emitExternalLocalTestData(rel))
			}
		}
	}

	split := max(s.split, 1)
	retries := 1

	if s.ciType == "test" && !s.noRetries && !slices.Contains(s.tags, "ya:noretries") {
		retries = ctx.tests.retries
	}

	files := s.files

	if len(files) == 0 {
		files = []string{""}
	}

	var runs []*Node
	var runRefs []NodeRef

	for r := 0; r < retries; r++ {
		retry := 0

		if retries > 1 {
			retry = r + 1
		}

		var chunks []*Node
		var refs []NodeRef

		for _, file := range files {
			for i := 0; i < split; i++ {
				node := ctx.testNode(s, TestChunk{split: split, index: i, retry: retry, file: file})

				chunks = append(chunks, node)
				refs = append(refs, ctx.emit.emit(node))
			}
		}

		node, ref := chunks[0], refs[0]

		if split > 1 || len(s.files) > 0 {
			node = ctx.testAccumulator(s, chunks, refs, retry)
			ref = ctx.emit.emit(node)
		}

		runs = append(runs, node)
		runRefs = append(runRefs, ref)
	}

	if retries > 1 {
		return []NodeRef{ctx.emit.emit(ctx.testMerger(s, runs, runRefs))}
	}

	return runRefs
}

func (ctx *GenCtx) testAccumulator(s *TestSuite, chunks []*Node, refs []NodeRef, retry int) *Node {
	o := ctx.tests
	na := ctx.na
	work := s.workDir(TestChunk{retry: retry})
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
		dir := path.Dir(chunks[i].Outputs[0].string())

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

func (ctx *GenCtx) testNode(s *TestSuite, chunk TestChunk) *Node {
	o := ctx.tests
	na := ctx.na
	work := s.workDir(chunk)
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

	if s.noCleanEnvironment {
		strs = append(strs, "--no-clean-environment")
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

	if chunk.retry > 0 {
		strs = append(strs, "--retry", strconv.Itoa(chunk.retry))
	}

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

	if chunk.split > 1 {
		strs = append(strs, "--split-count", strconv.Itoa(chunk.split), "--split-index", strconv.Itoa(chunk.index))
	}

	if chunk.file != "" {
		strs = append(strs, "--split-file", chunk.file)
	}

	strs = append(strs, "--compression-filter", "zstd", "--compression-level", "1")

	head := testArgs(strs)

	for _, g := range sortedResourceGlobals(s.globals) {
		head = append(head, argGlobalResource.any(), g.Token.any())
	}

	if o.dist {
		head = append(head, argNodeTimeout.any(), internStr(strconv.Itoa(s.timeout+testNodeFinishingTime)).any())
	}

	if s.recipes != "" {
		head = append(head, strRecipes.any(), internStr(s.recipes).any())
	}

	for _, data := range s.data {
		if uri, ok := strings.CutPrefix(data, "sbr://"); ok {
			head = append(head, strSandboxResource.any(), internStr(uri).any())
		}
	}

	for _, file := range s.externalLocalFiles {
		head = append(head, strExternalLocalFile.any(), internStr(file).any())
	}

	req := testRequirements(s)

	if req.RAM != 0 {
		head = append(head, argRamLimitGb.any(), internStr(strconv.Itoa(int(req.RAM))).any())
	}

	if s.canonizeSubPath != "" {
		head = append(head, argSubPath.any(), internStr(s.canonizeSubPath).any())
	}

	if o.dirOutputs {
		head = append(head, argDirOutputs.any())
	} else {
		head = append(head, argTar.any(), intern(tar).any())
		outputs = append(outputs, intern(tar))
	}

	head = append(head, s.runCmd(work, chunk)...)

	if o.commandFile {
		head = append(head, internArg(testCmdFileEnd).any())
	}

	cmds := make([]Cmd, 0, len(s.prepare)+1)

	for _, p := range s.prepare {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(p...))})
	}

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(head...)), Cwd: bld})

	inputs := append(append(append([]VFS{}, s.runInputs...), s.checkFiles...), s.prepareInputs...)
	inputNames := s.inputNames

	for _, p := range related {
		if path.IsAbs(p) {
			inputNames = append(inputNames, p)
		} else {
			inputs = append(inputs, intern(p))
		}
	}

	slices.SortFunc(inputs, func(a, b VFS) int { return strings.Compare(a.string(), b.string()) })

	kv := &KV{P: testSizeKinds[s.size], Path: s.projectPath + "/" + s.name, PC: pcYellow, RunTestNode: true, ShowOutBool: true, HasSpecialRunner: true}

	if chunk.split > 1 {
		kv.SplitIndex, kv.HasSplitIndex = chunk.index, true
	}

	kv.SplitFile = chunk.file

	deps := append([]NodeRef{ctx.testContext()}, s.deps...)
	names := na.strs.alloc(len(inputNames))[:len(inputNames):len(inputNames)]

	for i, name := range inputNames {
		names[i] = internStr(name)
	}

	na.strs.commit(len(names))

	return &Node{
		UIDSalt:      internStr(s.uidSalt),
		Platform:     s.p,
		Cmds:         na.cmdList(cmds...),
		Env:          ctx.testEnv(s),
		Inputs:       na.inputList(na.vfsList(slices.Compact(inputs)...)),
		InputNames:   names,
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

	if slices.Contains(s.tags, "ya:huge_logs") {
		r.TestOutputLimit = 1024 * 1024 * 1024
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

	if !s.importCheckEnv {
		env["YA_PYTHON_BIN"] = testPythonBin
	}

	if s.clearEnvironment {
		clear(env)
		clear(mandatory)
		mandatory["YA_MANDATORY_ENV_VARS"] = true
	}

	for _, name := range testSymbolizerVars {
		env[name] = testClangSymbolizerPath
		mandatory[name] = true
	}

	env["YA_CXX"] = testClangCXXPath
	env["YA_CC"] = testClangCCPath

	if !ctx.tests.dist {
		env["TESTING_SAVE_OUTPUT"] = "yes"
	}

	if s.pythonNoBytecode {
		env["PYTHONDONTWRITEBYTECODE"] = "1"
	}

	if s.disableLeaks {
		for _, name := range []string{"ASAN_OPTIONS", "LSAN_OPTIONS"} {
			env[name+"_ORIGINAL"] = env[name]
			env[name] += ":detect_leaks=0"
		}
	}

	if s.importCheckEnv {
		env["LSAN_OPTIONS"] += ":detect_leaks=0"
	}

	if s.testType == "exectest" {
		env["PATH"] = s.execPath
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

func testRecipes(recipes []string) string {
	if len(recipes) == 0 {
		return ""
	}

	value := strings.TrimSpace(strings.Join(recipes, ` "USE_RECIPE_DELIM" `) + ` "USE_RECIPE_DELIM"`)

	return base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(value, `"USE_RECIPE_DELIM"`, "\n")))
}

func testFlags(mf *MakeFlags) []string {
	flags := map[string]string{}

	copyStatsFlags(flags, mf.dflags)
	copyStatsFlags(flags, mf.tflags)

	if enabled, present := os.LookupEnv("YA_TEST_DISABLE_FLAKE8_MIGRATIONS"); !present || envEnabled(enabled) {
		flags["DISABLE_FLAKE8_MIGRATIONS"] = "yes"
	}

	if mf.sandboxing {
		flags["SANDBOXING"] = "yes"
		flags["FAKEID"] += "sandboxing"
	}

	var result []string

	for key, value := range flags {
		switch key {
		case "RECURSE_PARTITION_INDEX", "RECURSE_PARTITIONS_COUNT", "CONSISTENT_DEBUG":
			continue
		}

		result = append(result, key+"="+value)
	}

	slices.Sort(result)

	return result
}

func (ctx *GenCtx) testMerger(s *TestSuite, nodes []*Node, refs []NodeRef) *Node {
	o, na := ctx.tests, ctx.na
	work := s.workDir(TestChunk{})
	log := work + "/results_merge.log"
	outputs := []string{log}

	var inputs []VFS

	for _, node := range nodes {
		inputs = append(inputs, node.Outputs...)

		for _, out := range node.Outputs {
			base := path.Base(out.string())

			if base == "testing_out_stuff.tar.zstd" || base == "yt_run_test.tar" {
				outputs = append(outputs, out.string())
			} else {
				outputs = append(outputs, work+"/"+base)
			}
		}
	}

	strs := []string{testToolHostPath, "results_merger", "--project-path", s.projectPath, "--suite-name", s.name, "--source-root", testSourceRoot, "--log-path", log}

	if o.removeTos {
		strs = append(strs, "--remove-tos")
	}

	strs = append(strs, "--target-platform-descriptor", o.descriptor)

	for i := range nodes {
		strs = append(strs, "--output", s.workDir(TestChunk{retry: i + 1}))
	}

	var outs []VFS

	for _, out := range sortedUnique(outputs) {
		outs = append(outs, intern(out))
	}

	slices.SortFunc(inputs, func(a, b VFS) int { return strings.Compare(a.string(), b.string()) })

	return &Node{Platform: testNoPlatform, Env: sortedEnvVars(o.commonEnvMap()), Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(testArgs(strs)), Cwd: bld}), Inputs: na.inputList(na.vfsList(slices.Compact(inputs)...)), Outputs: na.vfsList(outs...), DepRefs: na.refList(dedupRefs(refs)...), KV: &KV{P: pkTR, PC: pcLightCyan, ShowOutBool: true}}
}
