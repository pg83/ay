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
	testContextAllowedFlags    = []string{"AUTOCHECK", "TESTS_REQUESTED", "USE_ARCADIA_PYTHON", "USE_SYSTEM_PYTHON"}
	testDescriptorExtraFlags   = []string{"MUSL", "ALLOCATOR", "FAKEID", "RACE"}
	testDescriptorFlagTags     = map[string]string{"USE_LTO": "lto", "USE_THINLTO": "thinlto", "MUSL": "musl", "USE_AFL": "AFL", "RACE": "race"}
	testNodeKVs                = map[ProcKind]*KV{0: testNodeKV(0), pkTS: testNodeKV(pkTS), pkTM: testNodeKV(pkTM), pkTL: testNodeKV(pkTL)}
	testCommonEnvSkip          = []string{"YA_CACHE_DIR", "YA_TIMEOUT", "YA_TOKEN", "YA_SOURCE_ROOT"}
	testSanitizerOptVars       = []string{"LSAN_OPTIONS", "ASAN_OPTIONS", "UBSAN_OPTIONS", "MSAN_OPTIONS", "TSAN_OPTIONS"}
	testSymbolizerVars         = []string{"ASAN_SYMBOLIZER_PATH", "LSAN_SYMBOLIZER_PATH", "MSAN_SYMBOLIZER_PATH", "TSAN_SYMBOLIZER_PATH", "UBSAN_SYMBOLIZER_PATH"}
	testAgentEnvSignatures     = []string{"OPENCODE", "CLAUDECODE", "CODEX_THREAD_ID", "CURSOR_TRACE_ID", "CODEIUM_EDITOR_APP_ROOT", "AIDER_API_KEY", "AMP_CURRENT_THREAD_ID", "ANTIGRAVITY_AGENT", "AUGMENT_AGENT", "GEMINI_CLI", "QWEN_CODE", "REPL_ID"}
	testAgentProcessNames      = []string{"opencode", "claude", "claude-code", "copilot", "codex-app", "codex", "hermes", "openclaw", "droid", "pi"}
	testNoPlatform             = &Platform{}
	testSecretRequirements     = []string{"sb_vault", "yav"}
	testToolHostPathArg        = internStr(testToolHostPath).any()
	testCmdFileStartArg        = internStr(testCmdFileStart).any()
	testCmdFileEndArg          = internStr(testCmdFileEnd).any()
	testBasetempArg            = intern("$(B)/tmp").any()
	testBuildRootArg           = internStr(testBuildRoot).any()
	testDistResultFileLimitArg = internStr(strconv.Itoa(testDistResultFileLimit)).any()
	testDefaultTestsLimitArg   = internStr(strconv.Itoa(testDefaultTestsLimit)).any()
	testSourceRootArg          = internStr(testSourceRoot).any()
	testGDBPathArg             = internStr(testGDBPath).any()
	testPythonBinArg           = internStr(testPythonBin).any()
	argTestCmdFileEnd          = internArg(testCmdFileEnd)
)

const (
	testNodeFinishingTime   = 300
	testCmdFileStart        = "--ya-start-command-file"
	testCmdFileEnd          = "--ya-end-command-file"
	testOutputStyleNinja    = "ninja"
	testOutputStyleJsonl    = "jsonl"
	testDistResultFileLimit = 100 * 1024
	testDefaultTestsLimit   = 100000
	testKVMDefaultRAM       = 4
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

	var buildType any = mf.buildType

	if mf.targetPlat != "" {
		buildType = mf.targetBuildType
	}

	context := map[string]any{"build_type": buildType, "flags": allowed}
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
	return testArgsInto(make([]ANY, 0, len(ss)), ss)
}

func testArgsInto(out []ANY, ss []string) []ANY {
	for _, s := range ss {
		out = append(out, testArg(s))
	}

	return out
}

func anyTextParts(a ANY) (string, string) {
	v := a.vfs()

	if v == 0 {
		return a.str().string(), ""
	}

	if rel := v.relString(); rel != "" {
		return v.prefix(), rel
	}

	return v.prefix()[:vfsPrefixLen-1], ""
}

func anyTextCompare(a, b ANY) int {
	a1, a2 := anyTextParts(a)
	b1, b2 := anyTextParts(b)
	n, m := len(a1)+len(a2), len(b1)+len(b2)

	for i := 0; i < n && i < m; i++ {
		ca, cb := concatByte(a1, a2, i), concatByte(b1, b2, i)

		if ca != cb {
			return int(ca) - int(cb)
		}
	}

	return n - m
}

func concatByte(head, tail string, i int) byte {
	if i < len(head) {
		return head[i]
	}

	return tail[i-len(head)]
}

func dedupRelatedPaths(paths []ANY) []ANY {
	out := paths[:0]

	dedupers.with(func(deduper *DeDuper) {
		for _, p := range paths {
			if v := p.vfs(); v != 0 {
				if deduper.addStable(uint32(v)) {
					out = append(out, p)
				}
			} else if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	})

	return out
}

func sortedUniqueByText(in []ANY) []ANY {
	out := slices.Clone(in)

	slices.SortFunc(out, anyTextCompare)

	return slices.Compact(out)
}

func testNodeKV(kind ProcKind) *KV {
	return &KV{P: kind, PC: pcYellow, RunTestNode: true, ShowOutBool: true, HasSpecialRunner: true}
}

func intArg(n int) ANY {
	if n >= 0 && n < len(intSTR) {
		return intSTR[n].any()
	}

	return internStr(strconv.Itoa(n)).any()
}

func testArg(s string) ANY {
	if strings.HasPrefix(s, "$(S)/") || strings.HasPrefix(s, "$(B)/") {
		return intern(s).any()
	}

	return internStr(s).any()
}

func testSizeTimeout(size STR) int {
	switch size {
	case strSmall:
		return 60
	case strMedium:
		return 600
	case strLarge:
		return 3600
	}

	return 0
}

func testSizeKind(size STR) ProcKind {
	switch size {
	case strSmall:
		return pkTS
	case strMedium:
		return pkTM
	case strLarge:
		return pkTL
	}

	return 0
}

func (o *TestOpts) sizeRequested(size string) bool {
	return len(o.sizes) == 0 || slices.Contains(o.sizes, size)
}

type TestSuite struct {
	dockerImages           []string
	depends                []string
	dependencyProjects     []VFS
	unpublishedDeps        []TestBuildDependency
	data                   []string
	sandboxResources       []string
	externalLocalFiles     []string
	corpus                 string
	corpusResources        []string
	corpusPartsExceeded    int
	recipes                string
	files                  []string
	pythonNoBytecode       bool
	disableLeaks           bool
	importCheckEnv         bool
	checkFiles             []VFS
	styleCheck             *StyleCheck
	p                      *Platform
	projectPath            STR
	name                   STR
	testType               STR
	ciType                 STR
	size                   STR
	timeout                int
	relatedPaths           []ANY
	runInputs              []VFS
	inputNames             []string
	uidSalt                string
	uidSaltID              STR
	workBase               string
	kvPath                 string
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
	sortedGlobals          []ResourceDecl
	nodeArgs               [5]ANY
	envArgs                []ANY
	requirements           map[string]string
	network                string
	split                  int
	prepare                [][]ANY
	prepareInputs          []VFS
	runCmd                 func(workDir string, chunk TestChunk) []ANY
	skipped                bool
}

type TestChunk struct {
	split, index, retry int
	file                string
}

func (s *TestSuite) workDir(chunk TestChunk) string {
	if s.workBase == "" {
		s.workBase = path.Join("$(B)", s.projectPath.string(), "test-results", s.name.string())
	}

	dir := s.workBase

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
	args := []ANY{strTestYMakePython3.any(), srcTestAppendFileScriptRel.any(), bldCommonTestContext.any()}

	for _, line := range o.context {
		args = append(args, internStr(line).any())
	}

	o.contextRef = ctx.emit.emit(&Node{
		Platform: ctx.target,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(args)}), Inputs: na.inputList(na.vfsList(srcTestAppendFileScriptRel)), KV: &testNodesKV,
		Outputs:      na.vfsList(bldCommonTestContext),
		Requirements: o.contextReqs,
		Resources:    usesPython3,
	})

	return o.contextRef
}

func (ctx *GenCtx) prepareTestDependencies(s *TestSuite) {
	var execDirs []string

	python := s.testType == strPytest || s.testType == strPy3test || s.testType == strExectest

	if python {
		s.pytestOutputDirs = append(s.pytestOutputDirs, s.projectPath.string())
	}

	if s.testType == strExectest {
		execDirs = append(execDirs, testExecDependencyDir(s.projectPath.string()))
	}

	var rewritten []string

	for _, dep := range s.depends {
		s.dependencyProjects = append(s.dependencyProjects, source(dep))

		published := !ctx.targetDirs[dep]

		for _, dependency := range ctx.runtimeTestDependencies(dep) {
			instance, res := dependency.instance, dependency.result
			output := testDependencyOutput(res)
			reg := ctx.scannerFor(instance).codegen

			if published {
				reg.use(output)
			} else {
				s.unpublishedDeps = append(s.unpublishedDeps, dependency)
			}

			info, _ := reg.byRel.get(output.rel())

			s.deps = append(s.deps, info.ProducerRef)
			rewritten = append(rewritten, output.relString())

			if s.testType == strExectest {
				execDirs = append(execDirs, testExecDependencyDir(output.relString()))
			}

			if python {
				clear(ctx.runtimeTestSeen)

				s.pytestOutputDirs = append(s.pytestOutputDirs, ctx.runtimeTestOutputDirs(dependency, ctx.runtimeTestSeen)...)
			}
		}
	}

	s.depends = sortedUnique(rewritten)

	if python {
		s.pytestOutputDirs = sortedUnique(s.pytestOutputDirs)
	}

	if s.testType == strExectest {
		s.execPath = strings.Join(sortedUnique(execDirs), ":")
	}
}

func (ctx *GenCtx) testSuiteSelected(s *TestSuite) bool {
	return !s.skipped && ctx.tests.sizeRequested(s.size.string()) && ctx.tests.tagFilter.matches(s.tags)
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

	var externalResources []string

	for _, entry := range s.data {
		if resource, ok := strings.CutPrefix(entry, "sbr://"); ok {
			s.sandboxResources = append(s.sandboxResources, resource)
		} else if resource, ok := strings.CutPrefix(entry, "mds:"); ok && s.supportsCanonization {
			s.deps = append(s.deps, ctx.emitCanonicalTestData(resource))
		} else if file, ok := strings.CutPrefix(entry, "ext:"); ok {
			rel := path.Join(s.projectPath.string(), strings.TrimSuffix(file, ".external"))

			if present, isDir := ctx.fs.exists(srcRootRel, rel); present && !isDir {
				s.externalLocalFiles = append(s.externalLocalFiles, rel)
				s.deps = append(s.deps, ctx.emitExternalLocalTestData(rel))
			} else if present, isDir := ctx.fs.exists(srcRootRel, rel+".external"); present && !isDir {
				storage, resource := readLargeFileDescriptor(ctx.fs, source(rel+".external"), rel+".external")

				if storage == "sandbox" {
					if dir := path.Dir(file); dir != "." {
						resource += "=" + dir
					}

					externalResources = append(externalResources, resource)
				}
			}
		}
	}

	s.sandboxResources = append(s.sandboxResources, externalResources...)

	if s.corpus != "" {
		var ok bool

		if s.corpusResources, s.corpusPartsExceeded, ok = ctx.fuzzCorpusResources(s.corpus); !ok {
			ctx.onWarn(Warn{Kind: WarnTestDependency, Message: "Broken suite: <Test [type=" + s.testType.string() + ", project=" + s.projectPath.string() + ", name=" + s.name.string() + "]>: corpus description " + s.corpus + " is not a JSON object"})

			return nil
		}
	}

	for _, resource := range s.sandboxResources {
		s.deps = append(s.deps, ctx.emitTestData("sbr://"+resource))
	}

	for _, resource := range s.corpusResources {
		s.deps = append(s.deps, ctx.emitTestData("sbr://"+resource))
	}

	split := max(s.split, 1)
	retries := 1

	if s.ciType == strTest && !s.noRetries && !slices.Contains(s.tags, "ya:noretries") {
		retries = ctx.tests.retries
	}

	files := s.files

	if len(files) == 0 {
		files = []string{""}
	}

	s.prepareNodeArgs()

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

func (s *TestSuite) prepareNodeArgs() {
	s.nodeArgs = [5]ANY{s.size.any(), s.testType.any(), s.ciType.any(), s.name.any(), s.projectPath.any()}
	s.sortedGlobals = sortedResourceGlobals(s.globals)
	s.envArgs = s.envArgs[:0]

	for _, e := range s.env {
		if strings.IndexByte(e, '$') >= 0 {
			for _, g := range s.globals {
				name, value, _ := strings.Cut(g.Token.string(), "::")

				e = strings.ReplaceAll(e, "$"+name, value)
			}
		}

		if !strings.Contains(e, "=") {
			v, ok := os.LookupEnv(e)

			if !ok {
				continue
			}

			e += "=" + v
		}

		s.envArgs = append(s.envArgs, testArg(e))
	}
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

	slices.SortFunc(inputs, vfsCompare)

	return &Node{
		Platform: testNoPlatform,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(testArgs(strs)...)), Cwd: bld.cwd()}),
		Env:      sortedEnvVars(o.commonEnvMap()), Inputs: na.inputList(na.vfsList(slices.Compact(inputs)...)), KV: &KV{P: pkTA, PC: pcLightCyan, ShowOutBool: true},
		Outputs: na.vfsList(append(outs, intern(log))...),
		DepRefs: na.refList(dedupRefs(refs)...),
	}
}

func (ctx *GenCtx) testNode(s *TestSuite, chunk TestChunk) *Node {
	o := ctx.tests
	na := ctx.na
	work := s.workDir(chunk)
	outputs := append(ctx.testOutputs[:0], internPrefixedJoin(work, "/meta.json"), internPrefixedJoin(work, "/ytest.report.trace"), internPrefixedJoin(work, "/run_test.log"))
	head := append(ctx.testAnys[:0], testToolHostPathArg, strRunTest.any())

	if o.commandFile {
		head = append(head, testCmdFileStartArg)
	}

	head = append(head, strMeta.any(), outputs[0].any(), strTrace.any(), outputs[1].any(), strTimeout.any(), intArg(s.timeout), strLogPath.any(), outputs[2].any(), strTestSize.any(), s.nodeArgs[0], strTestType.any(), s.nodeArgs[1], strTestCiType.any(), s.nodeArgs[2], strContextFilename.any(), bldCommonTestContext.any(), strSourceRoot2.any(), strS.any(), strBuildRoot2.any(), strB.any(), strTestSuiteName.any(), s.nodeArgs[3], strProjectPath.any(), s.nodeArgs[4])

	related := dedupRelatedPaths(s.relatedPaths)

	for _, p := range related {
		head = append(head, strTestRelatedPath.any(), p)
	}

	if s.noCleanEnvironment || slices.Contains(s.tags, "ya:dirty") {
		head = append(head, strNoCleanEnvironment.any())
	}

	head = append(head, strTargetPlatformDescriptor.any(), testArg(o.descriptor))

	if o.removeTos {
		head = append(head, strRemoveTos.any())
	}

	head = append(head, strGdbPath.any(), testGDBPathArg)

	if o.dist {
		head = append(head, strLogLevel.any(), strDebug.any(), strResultMaxFileSize.any(), testDistResultFileLimitArg, strDisableMemorySnippet.any(), strTruncateFiles.any(), strPropagateTimeoutInfo.any())
	} else {
		head = append(head, strResultMaxFileSize.any(), strZero.any())
	}

	head = append(head, strVerifyResults.any())

	if slices.Contains(s.tags, "ya:copydataro") {
		head = append(head, strDataToEnvironment.any(), strCopyro.any())
	} else if slices.Contains(s.tags, "ya:copydata") {
		head = append(head, strDataToEnvironment.any(), strCopy.any())
	}

	head = append(head, strTestsLimitInChunk.any(), testDefaultTestsLimitArg, strOutputStyle.any(), testArg(o.outputStyle), strPythonBin.any(), testPythonBinArg)

	if chunk.retry > 0 {
		head = append(head, strRetry.any(), intArg(chunk.retry))
	}

	if s.runCwd != "" {
		head = append(head, strTestRunCwd.any(), testArg(s.runCwd))
	}

	for _, tag := range s.tags {
		head = append(head, strTag.any(), testArg(tag))
	}

	if _, ok := s.requirements["ram_disk"]; ok {
		head = append(head, strRequiresRamDisk.any())
	}

	for _, e := range s.envArgs {
		head = append(head, strEnv.any(), e)
	}

	if s.setupPythonpathEnv {
		head = append(head, strSetupPythonpathEnv.any())
	}

	if s.supportsCanonization {
		head = append(head, strSupportsCanonization.any())
	}

	if s.supportsTestParameters {
		head = append(head, strSupportsTestParameters.any())
	}

	for _, sig := range s.smoothShutdown {
		head = append(head, strSmoothShutdownSignals.any(), testArg(sig))
	}

	if chunk.split > 1 {
		head = append(head, strSplitCount.any(), intArg(chunk.split), strSplitIndex.any(), intArg(chunk.index))
	}

	if chunk.file != "" {
		head = append(head, strSplitFile.any(), testArg(chunk.file))
	}

	head = append(head, strCompressionFilter.any(), strZstd.any(), strCompressionLevel.any(), strOne.any())

	for _, g := range s.sortedGlobals {
		head = append(head, argGlobalResource.any(), g.Token.any())
	}

	if o.dist {
		head = append(head, argNodeTimeout.any(), intArg(s.timeout+testNodeFinishingTime))
	}

	if s.recipes != "" {
		head = append(head, strRecipes.any(), internStr(s.recipes).any())
	}

	for _, resource := range s.sandboxResources {
		head = append(head, strSandboxResource.any(), internStr(resource).any())
	}

	for _, file := range s.externalLocalFiles {
		head = append(head, strExternalLocalFile.any(), internStr(file).any())
	}

	for _, resource := range s.corpusResources {
		head = append(head, strSandboxResource.any(), internStr(resource).any())
	}

	req := testRequirements(na, s)

	if req.RAM != 0 {
		head = append(head, argRamLimitGb.any(), intArg(int(req.RAM)))
	}

	if s.canonizeSubPath != "" {
		head = append(head, argSubPath.any(), internStr(s.canonizeSubPath).any())
	}

	if o.dirOutputs {
		head = append(head, argDirOutputs.any())
	} else {
		tar := internPrefixedJoin(work, "/testing_out_stuff.tar.zstd")

		head = append(head, argTar.any(), tar.any())
		outputs = append(outputs, tar)
	}

	head = append(head, s.runCmd(work, chunk)...)

	if o.commandFile {
		head = append(head, argTestCmdFileEnd.any())
	}

	cmds := ctx.testCmds[:0]

	for _, p := range s.prepare {
		cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(p...))})
	}

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(head...)), Cwd: bld.cwd()})
	ctx.testAnys = head[:0]

	inputs := append(append(append(ctx.testInputs[:0], s.runInputs...), s.checkFiles...), s.prepareInputs...)
	inputNames := s.inputNames

	for _, p := range related {
		if v := p.vfs(); v != 0 {
			inputs = append(inputs, v)
		} else {
			inputNames = append(inputNames, p.string())
		}
	}

	slices.SortFunc(inputs, vfsCompare)
	ctx.testInputs = inputs[:0]

	if s.kvPath == "" {
		s.kvPath = s.projectPath.string() + "/" + s.name.string()
	}

	deps := append(append(ctx.testDeps[:0], ctx.testContext()), s.deps...)
	names := na.strs.alloc(len(inputNames))[:len(inputNames):len(inputNames)]

	for i, name := range inputNames {
		names[i] = internStr(name)
	}

	na.strs.commit(len(names))

	if len(s.globals) != 0 {
		resources := na.strs.alloc(len(s.globals))[:len(s.globals):len(s.globals)]

		for i, decl := range s.globals {
			resources[i] = decl.FetchKey
		}

		na.strs.commit(len(resources))

		cmds[len(cmds)-1].Resources = resources
	}

	if s.uidSaltID == 0 {
		s.uidSaltID = internStr(s.uidSalt)
	}

	extra := na.extras.one()

	*extra = NodeExtra{InputNames: names, UIDSalt: s.uidSaltID, KVPath: s.kvPath, SplitFile: chunk.file, SplitIndex: chunk.index, HasSplitIndex: chunk.split > 1}

	node := na.nodes.one()

	*node = Node{
		Platform: s.p,
		Cmds:     na.cmdList(cmds...),
		Env:      ctx.testEnv(s), Inputs: na.inputList(na.vfsList(slices.Compact(inputs)...)), Extra: extra,
		KV:           testNodeKVs[testSizeKind(s.size)],
		Outputs:      na.vfsList(outputs...),
		Requirements: req,
		DepRefs:      na.refList(dedupRefs(deps)...),
	}

	ctx.testCmds, ctx.testOutputs, ctx.testDeps = cmds[:0], outputs[:0], deps[:0]

	return node
}

func testRequirements(na *NodeArenas, s *TestSuite) *Requirements {
	cpu, ram := 1, 8
	r := na.reqs.one()

	*r = Requirements{Network: nwRestricted, HasRAMDisk: true}
	r.DNS = s.requirements["dns"]

	if s.network == "full" || s.size == strLarge {
		r.Network = nwFull
	}

	if _, ok := s.requirements["kvm"]; ok {
		r.KVM, ram = true, testKVMDefaultRAM
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

	r.SbVault, r.Yav = s.requirements["sb_vault"], s.requirements["yav"]
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

type testEnvKey struct {
	clearEnvironment, pythonNoBytecode, disableLeaks, importCheckEnv bool
	execPath                                                         string
}

type testEnvTemplate struct {
	vars EnvVars
	name int
}

func (ctx *GenCtx) testEnv(s *TestSuite) EnvVars {
	key := testEnvKey{clearEnvironment: s.clearEnvironment, pythonNoBytecode: s.pythonNoBytecode, disableLeaks: s.disableLeaks, importCheckEnv: s.importCheckEnv}

	if s.testType == strExectest {
		key.execPath = s.execPath
	}

	template, ok := ctx.testEnvTemplates[key]

	if !ok {
		vars := ctx.testEnvVars(s)
		name := envTEST_NAME

		template = testEnvTemplate{vars: vars, name: slices.IndexFunc(vars, func(v EnvVar) bool { return v.Name == name })}

		if ctx.testEnvTemplates == nil {
			ctx.testEnvTemplates = map[testEnvKey]testEnvTemplate{}
		}

		ctx.testEnvTemplates[key] = template
	}

	n := len(template.vars)
	out := ctx.na.envs.alloc(n)[:n:n]

	copy(out, template.vars)

	if template.name >= 0 {
		out[template.name].Value = s.name.any()
	}

	ctx.na.envs.commit(n)

	return EnvVars(out)
}

func (ctx *GenCtx) testEnvVars(s *TestSuite) EnvVars {
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
	env["TEST_NAME"] = ""
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

	if s.testType == strExectest {
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
		if e == "kvm" {
			e = "kvm:true"
		}

		if k, v, ok := strings.Cut(e, ":"); ok {
			k = strings.ToLower(k)

			if slices.Contains(testSecretRequirements, k) {
				items := strings.Split(out[k], ",")

				if out[k] == "" {
					items = nil
				}

				for _, item := range strings.Split(v, ",") {
					if !slices.Contains(items, item) {
						items = append(items, item)
					}
				}

				out[k] = strings.Join(items, ",")

				continue
			}

			v = strings.ToLower(v)

			if _, exists := out[k]; exists && v == "0" {
				delete(out, k)
			} else {
				out[k] = v
			}
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

	if mf.runTests > 0 {
		flags["TESTS_REQUESTED"] = "yes"
	}

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

	strs := []string{testToolHostPath, "results_merger", "--project-path", s.projectPath.string(), "--suite-name", s.name.string(), "--source-root", testSourceRoot, "--log-path", log}

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

	slices.SortFunc(inputs, vfsCompare)

	return &Node{Platform: testNoPlatform, Env: sortedEnvVars(o.commonEnvMap()), Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(testArgs(strs)), Cwd: bld.cwd()}), Inputs: na.inputList(na.vfsList(slices.Compact(inputs)...)), Outputs: na.vfsList(outs...), DepRefs: na.refList(dedupRefs(refs)...), KV: &KV{P: pkTR, PC: pcLightCyan, ShowOutBool: true}}
}
