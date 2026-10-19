package main

import (
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
)

var (
	yaTestContextFlags   = []string{"AUTOCHECK", "TESTS_REQUESTED", "USE_ARCADIA_PYTHON", "USE_SYSTEM_PYTHON"}
	yaTestCommonEnvSkip  = []string{"YA_CACHE_DIR", "YA_TIMEOUT", "YA_TOKEN", "YA_SOURCE_ROOT"}
	yaTestSanitizerOpts  = []string{"LSAN_OPTIONS", "ASAN_OPTIONS", "UBSAN_OPTIONS", "MSAN_OPTIONS", "TSAN_OPTIONS"}
	yaTestSymbolizerVars = []string{"ASAN_SYMBOLIZER_PATH", "LSAN_SYMBOLIZER_PATH", "MSAN_SYMBOLIZER_PATH", "TSAN_SYMBOLIZER_PATH", "UBSAN_SYMBOLIZER_PATH"}
	yaTestSizeKinds      = map[string]ProcKind{"small": pkTS, "medium": pkTM, "large": pkTL}
	yaTestSizeTimeouts   = map[string]int{"small": 60, "medium": 600, "large": 3600}
	yaTestNoPlatform     = &Platform{}
)

const (
	yaTestNodeFinishingTime = 300
	yaTestTool              = "$(TEST_TOOL_HOST)/test_tool"
	yaTestCmdFileStart      = "--ya-start-command-file"
	yaTestCmdFileEnd        = "--ya-end-command-file"
)

type YaTestOpts struct {
	dist         bool
	removeTos    bool
	dirOutputs   bool
	commandFile  bool
	jstyleServer bool
	sizes        []string
	descriptor   string
	buildType    string
	contextFlags [][2]string
	processEnv   [][2]string
	contextRef   NodeRef
	goOpts       *TestOpts
}

func newYaTestOpts(mf *MakeFlags, conf map[string]string, buildType string) *YaTestOpts {
	if mf.testLevel == 0 {
		return nil
	}

	opts := &YaTestOpts{
		dist:         mf.dist,
		removeTos:    conf["dir_outputs"] != "false",
		commandFile:  conf["use_command_file_in_testtool"] == "true",
		jstyleServer: conf["use_jstyle_server"] == "true",
		sizes:        mf.testSizes,
		buildType:    buildType,
	}

	opts.dirOutputs = opts.removeTos && !mf.dist && !mf.sandboxing

	tags := make([]string, 0, len(mf.tflags)+2)

	for k, v := range mf.tflags {
		tags = append(tags, k+"="+v)
	}

	if mf.sandboxing {
		tags = append(tags, "FAKEID=sandboxing", "SANDBOXING=yes")
	}

	slices.Sort(tags)

	target := mf.targetPlat

	opts.descriptor = strings.Join(append([]string{target, buildType}, tags...), "-")

	flags := map[string]string{}

	for k, v := range mf.tflags {
		flags[k] = v
	}

	for k, v := range mf.dflags {
		flags[k] = v
	}

	for _, k := range yaTestContextFlags {
		if v, ok := flags[k]; ok {
			opts.contextFlags = append(opts.contextFlags, [2]string{k, v})
		}
	}

	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")

		if !strings.HasPrefix(k, "YA_") || slices.ContainsFunc(yaTestCommonEnvSkip, func(s string) bool { return strings.HasPrefix(k, s) }) {
			continue
		}

		opts.processEnv = append(opts.processEnv, [2]string{k, v})
	}

	return opts
}

func yaTestArgs(ss []string) []ANY {
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

func (o *YaTestOpts) sizeRequested(size string) bool {
	return len(o.sizes) == 0 || slices.Contains(o.sizes, size)
}

type yaTestSuite struct {
	p                      *Platform
	projectPath            string
	name                   string
	testType               string
	ciType                 string
	size                   string
	timeout                int
	moduleLang             string
	relatedPaths           []string
	runInputs              []VFS
	deps                   []NodeRef
	env                    []string
	tags                   []string
	cleanEnv               bool
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

func (s *yaTestSuite) workDir(split, index int) string {
	dir := path.Join("$(B)", s.projectPath, "test-results", s.name)

	if split > 1 {
		dir += "/chunk" + strconv.Itoa(index)
	}

	return dir
}

func yaTestDescriptor(o *YaTestOpts) string {
	return o.descriptor
}

func (ctx *GenCtx) yaTestContext() NodeRef {
	o := ctx.tests

	if o.contextRef != 0 {
		return o.contextRef
	}

	lines := []string{"{", `  "build_type": "` + o.buildType + `",`}

	if len(o.contextFlags) == 0 {
		lines = append(lines, `  "flags": {}`)
	} else {
		lines = append(lines, `  "flags": {`)

		for i, kv := range o.contextFlags {
			line := `    "` + kv[0] + `": "` + kv[1] + `"`

			if i+1 < len(o.contextFlags) {
				line += ","
			}

			lines = append(lines, line)
		}

		lines = append(lines, "  }")
	}

	lines = append(lines, "}")

	na := ctx.na
	args := []ANY{javaPython3.any(), source(testAppendFileScriptRel).any(), bldCommonTestContext.any()}

	args = append(args, yaTestArgs(lines)...)

	o.contextRef = ctx.emit.emit(&Node{
		Platform:  ctx.target,
		Cmds:      na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...))}),
		Inputs:    na.inputList(na.vfsList(source(testAppendFileScriptRel))),
		KV:        &testNodesKV,
		Outputs:   na.vfsList(bldCommonTestContext),
		Resources: usesPython3,
	})

	return o.contextRef
}

func (ctx *GenCtx) emitYaTestSuite(s *yaTestSuite) []NodeRef {
	o := ctx.tests

	if !o.sizeRequested(s.size) {
		return nil
	}

	split := max(s.split, 1)

	if split == 1 {
		return []NodeRef{ctx.emit.emit(ctx.yaTestNode(s, 1, 0))}
	}

	chunks := make([]*Node, split)
	refs := make([]NodeRef, split)

	for i := range chunks {
		chunks[i] = ctx.yaTestNode(s, split, i)
		refs[i] = ctx.emit.emit(chunks[i])
	}

	return []NodeRef{ctx.emit.emit(ctx.yaTestAccumulator(s, chunks, refs))}
}

func (ctx *GenCtx) yaTestAccumulator(s *yaTestSuite, chunks []*Node, refs []NodeRef) *Node {
	na := ctx.na
	work := s.workDir(1, 0)
	log := work + "/results_accumulator.log"

	strs := []string{yaTestTool, "results_accumulator", "--accumulator-path", work, "--source-root", "$(S)", "--log-path", log,
		"--concatenate-binaries", "report.exec", "--concatenate-binaries", "operation.log"}

	if ctx.tests.dist {
		strs = append(strs, "--truncate", "--log-level", "DEBUG")
	}

	strs = append(strs, "--gdb-path", testGDBPath, "--tests-limit-in-suite", "100000", "--keep-paths", yaTestCmdFileStart)

	outputs := []string{work + "/meta.json", work + "/run_test.log", work + "/ytest.report.trace"}

	var inputs []VFS

	for i := range chunks {
		dir := s.workDir(len(chunks), i)

		strs = append(strs, "--output", dir)
		inputs = append(inputs, chunks[i].Outputs...)

		if !ctx.tests.dirOutputs {
			outputs = append(outputs, dir+"/testing_out_stuff.tar.zstd")
		}
	}

	strs = append(strs, yaTestCmdFileEnd)

	if ctx.tests.dirOutputs {
		strs = append(strs, "--skip-dir", "testing_out_stuff", "--skip-file", "yt_run_test.tar")
	} else {
		strs = append(strs, "--skip-file", "testing_out_stuff.tar.zstd", "--skip-file", "yt_run_test.tar")
	}

	outs := make([]VFS, 0, len(outputs)+1)

	for _, o := range sortedUnique(outputs) {
		outs = append(outs, intern(o))
	}

	slices.SortFunc(inputs, func(a, b VFS) int { return strings.Compare(a.string(), b.string()) })

	return &Node{
		Platform: yaTestNoPlatform,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(yaTestArgs(strs)...)), Cwd: bld}),
		Env:      ctx.yaTestProcessEnv(),
		Inputs:   na.inputList(na.vfsList(slices.Compact(inputs)...)),
		KV:       &KV{P: pkTA, PC: pcLightCyan, ShowOutBool: true},
		Outputs:  na.vfsList(append(outs, intern(log))...),
		DepRefs:  na.refList(dedupRefs(refs)...),
	}
}

func (ctx *GenCtx) yaTestNode(s *yaTestSuite, split, index int) *Node {
	o := ctx.tests
	na := ctx.na
	work := s.workDir(split, index)
	meta, trace, log := work+"/meta.json", work+"/ytest.report.trace", work+"/run_test.log"
	tar := work + "/testing_out_stuff.tar.zstd"
	outputs := []VFS{intern(meta), intern(trace), intern(log)}
	strs := []string{yaTestTool, "run_test"}

	if o.commandFile {
		strs = append(strs, yaTestCmdFileStart)
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

	strs = append(strs, "--target-platform-descriptor", yaTestDescriptor(o))

	if o.removeTos {
		strs = append(strs, "--remove-tos")
	}

	strs = append(strs, "--gdb-path", testGDBPath)

	if o.dist {
		strs = append(strs, "--log-level", "DEBUG", "--result-max-file-size", strconv.Itoa(100*1024), "--disable-memory-snippet", "--truncate-files", "--propagate-timeout-info")
	} else {
		strs = append(strs, "--result-max-file-size", "0")
	}

	strs = append(strs, "--verify-results", "--tests-limit-in-chunk", "100000", "--output-style", "ninja", "--python-bin", testPythonBin)

	for _, tag := range s.tags {
		strs = append(strs, "--tag", tag)
	}

	if _, ok := s.requirements["ram_disk"]; ok {
		strs = append(strs, "--requires-ram-disk")
	}

	for _, e := range s.env {
		if !strings.Contains(e, "=") {
			continue
		}

		if strings.Contains(e, "$") {
			for _, g := range s.globals {
				name, value, _ := strings.Cut(g.Token.string(), "::")

				e = strings.ReplaceAll(e, "$"+name, value)
			}
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

	head := yaTestArgs(strs)

	for _, g := range sortedResourceGlobals(s.globals) {
		head = append(head, argGlobalResource.any(), g.Token.any())
	}

	if o.dist {
		head = append(head, argNodeTimeout.any(), internStr(strconv.Itoa(s.timeout+yaTestNodeFinishingTime)).any())
	}

	req := ctx.yaTestRequirements(s)

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
		head = append(head, internArg(yaTestCmdFileEnd).any())
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

	kv := &KV{P: yaTestSizeKinds[s.size], Path: s.projectPath + "/" + s.name, PC: pcYellow, RunTestNode: true, ShowOutBool: true, HasSpecialRunner: true}

	if split > 1 {
		kv.SplitIndex, kv.HasSplitIndex = index, true
	}

	deps := append([]NodeRef{ctx.yaTestContext()}, s.deps...)

	return &Node{
		Platform:     s.p,
		Cmds:         na.cmdList(cmds...),
		Env:          ctx.yaTestEnv(s),
		Inputs:       na.inputList(na.vfsList(slices.Compact(inputs)...)),
		KV:           kv,
		Outputs:      na.vfsList(outputs...),
		Requirements: req,
		DepRefs:      na.refList(dedupRefs(deps)...),
	}
}

func (ctx *GenCtx) yaTestRequirements(s *yaTestSuite) *Requirements {
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

func (ctx *GenCtx) yaTestEnv(s *yaTestSuite) EnvVars {
	env := map[string]string{}
	mandatory := map[string]bool{}

	if !s.cleanEnv {
		for _, kv := range ctx.tests.processEnv {
			env[kv[0]] = kv[1]
		}

		env["ARCADIA_BUILD_ROOT"] = "$(B)"
		env["ARCADIA_ROOT_DISTBUILD"] = "$(S)"
		env["ARCADIA_SOURCE_ROOT"] = "$(S)"
		env["TEST_NAME"] = s.name
		env["YA_TEST_RUNNER"] = "1"
		env["GORACE"] = "halt_on_error=1"

		for _, name := range yaTestSanitizerOpts {
			env[name] = "exitcode=100"
			mandatory[name] = true
		}

		env["UBSAN_OPTIONS"] += ":print_stacktrace=1,halt_on_error=1"
		env["MSAN_OPTIONS"] += ":report_umrs=1"
		env["YA_PYTHON_BIN"] = testPythonBin
	}

	for _, name := range yaTestSymbolizerVars {
		env[name] = testClangSymbolizerPath
		mandatory[name] = true
	}

	env["YA_CXX"] = testClangCXXPath
	env["YA_CC"] = testClangCCPath

	if !ctx.tests.dist {
		env["TESTING_SAVE_OUTPUT"] = "yes"
	}

	names := []string{"YA_MANDATORY_ENV_VARS"}

	for name := range mandatory {
		names = append(names, name)
	}

	slices.Sort(names)

	env["YA_MANDATORY_ENV_VARS"] = strings.Join(names, ":")

	return sortedEnvVars(env)
}

func (ctx *GenCtx) yaTestProcessEnv() EnvVars {
	env := map[string]string{}

	for _, kv := range ctx.tests.processEnv {
		env[kv[0]] = kv[1]
	}

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

func yaTestRequirementsOf(entries []string) map[string]string {
	out := map[string]string{}

	for _, e := range entries {
		if k, v, ok := strings.Cut(e, ":"); ok {
			out[strings.ToLower(k)] = strings.ToLower(v)
		}
	}

	return out
}
