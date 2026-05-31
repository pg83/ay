package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jon-codes/getopt"
)

var (
	targetStatsExtraFlagAllowlist = map[string]struct{}{
		"ALLOCATOR": {},
		"FAKEID":    {},
		"MUSL":      {},
		"RACE":      {},
	}
	targetStatsBaseFlagAllowlist = map[string]struct{}{
		"ALLOCATOR":      {},
		"FAKEID":         {},
		"MUSL":           {},
		"RACE":           {},
		"SANDBOXING":     {},
		"SANITIZER_TYPE": {},
		"USE_AFL":        {},
		"USE_LTO":        {},
		"USE_THINLTO":    {},
	}
	fatalOnce sync.Once
	ansiCols  = map[string]string{
		"red":           ansiESC + "[31m",
		"green":         ansiESC + "[32m",
		"yellow":        ansiESC + "[33m",
		"blue":          ansiESC + "[34m",
		"magenta":       ansiESC + "[35m",
		"cyan":          ansiESC + "[36m",
		"white":         ansiESC + "[37m",
		"light-red":     ansiESC + "[91m",
		"light-green":   ansiESC + "[92m",
		"light-yellow":  ansiESC + "[93m",
		"light-blue":    ansiESC + "[94m",
		"light-magenta": ansiESC + "[95m",
		"light-cyan":    ansiESC + "[96m",
		"light-white":   ansiESC + "[97m",
	}
)

type makeFlags struct {
	srcRoot           string
	bldRoot           string
	outRoot           string
	installRoot       string
	threads           int
	keepGoing         bool
	dumpGraph         bool
	stats             bool
	ninja             bool
	buildType         string
	targetPlat        string
	hostPlat          string
	tflags            map[string]string
	hflags            map[string]string
	targets           []string
	verbose           bool
	testLevel         int
	sandboxing        bool
	dumpIgnoredMacros bool
	cmdPrefixes       []cmdPrefix
}

// cmdPrefix prepends prefix tokens before any command argument whose path ends
// with suffix. It lets the user run fetched binaries through an explicit ELF
// loader on systems lacking the binary's default interpreter, e.g.
// --cmd-prefix=bin/java=/bin/ld.linux-so.2 turns `… <JDK>/bin/java …` into
// `… /bin/ld.linux-so.2 <JDK>/bin/java …`.
type cmdPrefix struct {
	suffix string
	prefix []string
}

func buildTargetStatsFlags(platformFlags, cliFlags map[string]string) map[string]string {
	flags := make(map[string]string, len(platformFlags)+len(cliFlags))
	copyAllowedStatsFlags(flags, platformFlags, targetStatsBaseFlagAllowlist)
	copyAllowedStatsFlags(flags, cliFlags, targetStatsExtraFlagAllowlist)
	if yes, ok := parseStatsBool(flags["SANDBOXING"]); ok && yes {
		if _, ok := flags["FAKEID"]; !ok {
			flags["FAKEID"] = "sandboxing"
		}
	}

	return flags
}

func copyAllowedStatsFlags(dst, src map[string]string, allowlist map[string]struct{}) {
	for k, v := range src {
		if v == "" {
			continue
		}
		if _, ok := allowlist[k]; ok {
			dst[k] = v
		}
	}
}

func copyStatsFlags(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

func buildHostStatsFlags(hostPlatformFlags, cliFlags map[string]string, sandboxing bool) map[string]string {
	flags := map[string]string{
		"CLANG_COVERAGE":   "no",
		"CONSISTENT_DEBUG": "yes",
		"NO_DEBUGINFO":     "yes",
		"TIDY":             "no",
		"TOOL_BUILD_MODE":  "yes",
		"TRAVERSE_RECURSE": "no",
	}

	copyStatsFlags(flags, hostPlatformFlags)
	copyStatsFlags(flags, cliFlags)
	if sandboxing {
		flags["SANDBOXING"] = "yes"
		flags["FAKEID"] = "sandboxing"
	}

	return flags
}

func readOptionalYaConfSection(fs FS, rel, wantSection string) map[string]string {
	if fs == nil || !fs.IsFile(rel) {
		return nil
	}

	return readYaConfSection(fs, rel, wantSection)
}

func joinCompilerFlagStrings(parts ...string) string {
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		out = append(out, part)
	}

	return strings.Join(out, " ")
}

func compilerFlagsFromConfig(primary, internal map[string]string, key, env string) string {
	return joinCompilerFlagStrings(primary[key], internal[key], env)
}

func shouldExposeSandboxingTargetTags(mf *makeFlags) bool {
	return mf != nil && mf.sandboxing && mf.testLevel > 0
}

func cmdMake(args []string) int {
	defer startProfilesFromEnv()()

	mf := parseMakeFlags(args)

	if len(mf.targets) == 0 {
		ThrowFmt("make: no targets supplied and current working directory is outside the source root")
	}

	fs := NewFS(mf.srcRoot)

	tools, conf := toolchainFlags(fs, nil)
	rootHostYaFlags := readYaConfSection(fs, "ya.conf", "host_platform_flags")
	rootTargetYaFlags := readYaConfSection(fs, "ya.conf", "flags")
	hostYaFlags := map[string]string{}
	targetYaFlags := map[string]string{}
	copyStatsFlags(hostYaFlags, rootHostYaFlags)
	copyStatsFlags(targetYaFlags, rootTargetYaFlags)
	var hostInternalYaFlags map[string]string
	var targetInternalYaFlags map[string]string
	if mf.testLevel == 0 {

		hostInternalYaFlags = readOptionalYaConfSection(fs, "build/internal/ya.conf", "host_platform_flags")
		targetInternalYaFlags = readOptionalYaConfSection(fs, "build/internal/ya.conf", "flags")
		copyStatsFlags(hostYaFlags, hostInternalYaFlags)
		copyStatsFlags(targetYaFlags, targetInternalYaFlags)
	}

	hOS, hISA := resolvePlatform(mf.hostPlat)
	hostFlags := make(map[string]string, len(tools)+len(hostYaFlags)+len(mf.hflags)+1)
	for k, v := range tools {
		hostFlags[k] = v
	}
	for k, v := range hostYaFlags {
		hostFlags[k] = v
	}
	for k, v := range mf.hflags {
		hostFlags[k] = v
	}
	hostFlags["PIC"] = "yes"
	if _, ok := hostFlags["GG_BUILD_TYPE"]; !ok {
		hostFlags["GG_BUILD_TYPE"] = "release"
	}
	hostP := NewPlatform(
		hOS,
		hISA,
		hostFlags,
		[]string{"tool"},
		compilerFlagsFromConfig(rootHostYaFlags, hostInternalYaFlags, "CFLAGS", ""),
		compilerFlagsFromConfig(rootHostYaFlags, hostInternalYaFlags, "CXXFLAGS", ""),
	)
	hostP.StatsFlags = buildHostStatsFlags(hostYaFlags, mf.hflags, mf.sandboxing)
	resourceFetches := newResourceFetchPlan(mf.srcRoot, conf, hostP)

	targetSpec := mf.targetPlat
	if targetSpec == "" {
		targetSpec = string(MakePlatformID(hOS, hISA))
	}
	tOS, tISA := resolvePlatform(targetSpec)
	targetFlags := make(map[string]string, len(tools)+len(targetYaFlags)+len(mf.tflags)+3)
	for k, v := range tools {
		targetFlags[k] = v
	}
	for k, v := range targetYaFlags {
		targetFlags[k] = v
	}
	for k, v := range mf.tflags {
		targetFlags[k] = v
	}
	if mf.buildType != "" {
		targetFlags["GG_BUILD_TYPE"] = mf.buildType
	}
	if mf.testLevel > 0 {
		targetFlags["TESTS_REQUESTED"] = "yes"
	}
	if mf.sandboxing {
		targetFlags["SANDBOXING"] = "yes"
	}
	targetFlags["PIC"] = "no"
	targetP := NewPlatform(
		tOS,
		tISA,
		targetFlags,
		nil,
		compilerFlagsFromConfig(rootTargetYaFlags, targetInternalYaFlags, "CFLAGS", os.Getenv("CFLAGS")),
		compilerFlagsFromConfig(rootTargetYaFlags, targetInternalYaFlags, "CXXFLAGS", os.Getenv("CXXFLAGS")),
	)
	targetP.StatsFlags = buildTargetStatsFlags(targetFlags, mf.tflags)
	if shouldExposeSandboxingTargetTags(mf) {
		targetP.Tags = sandboxingNodeTags(targetP)
	}

	onWarn := func(w Warn) {

		if w.Kind == WarnMissingInclude && !mf.keepGoing {
			ThrowFmt("%s: %s", w.Kind, w.Message)
		}
		if mf.verbose {
			fmt.Fprintf(os.Stderr, "\x1b[33m%s: %s\x1b[0m\n", w.Kind, w.Message)
		}
	}

	if mf.threads == 0 {
		if mf.dumpGraph {
			for _, target := range mf.targets {
				g := GenDumpGraphWithResources(fs, target, hostP, targetP, onWarn, resourceFetches, mf.testLevel > 0)
				applyGraphConf(g, conf)
				writeGraph("-", g)
			}
		} else {
			genStream(fs, mf.targets, hostP, targetP, resourceFetches, func(*Node) {}, onWarn, mf.testLevel > 0)
		}

		if mf.dumpIgnoredMacros {
			dumpMacroAudit(os.Stderr)
		}
		return 0
	}

	ex := newExecutor(mf.srcRoot, mf.bldRoot, mf.threads, mf.keepGoing, resourceFetches.mountMap(), mf.cmdPrefixes)

	go ex.eventLoop()

	defer ex.close()

	executorWarn := func(w Warn) {
		if w.Kind == WarnMissingInclude && !mf.keepGoing {
			ThrowFmt("%s: %s", w.Kind, w.Message)
		}

		if mf.verbose {
			kind := w.Kind
			message := w.Message

			ex.events <- func() {
				fmt.Fprintf(os.Stderr, "\x1b[33m%s: %s\x1b[0m\n", kind, message)
			}
		}
	}

	results := genStream(fs, mf.targets, hostP, targetP, resourceFetches, ex.onNode, executorWarn, mf.testLevel > 0)

	ex.run(results)

	failedRoots := ex.failedRoots(results)
	if len(failedRoots) > 0 {
		ThrowFmt("build failed: %s", strings.Join(failedRoots, ", "))
	}

	for _, uid := range results {
		ex.installRoot(uid, mf.installRoot)
	}

	return 0
}

func genStream(fs FS, targets []string, hostP, targetP *Platform, resources *resourceFetchPlan, onNode func(*Node), onWarn func(Warn), testMode bool) []string {
	all := []string{}

	for _, t := range targets {
		ec := genStreamOne(fs, t, hostP, targetP, resources, onNode, onWarn, testMode)
		all = append(all, ec...)
	}

	return all
}

func genStreamOne(fs FS, target string, hostP, targetP *Platform, resources *resourceFetchPlan, onNode func(*Node), onWarn func(Warn), testMode bool) []string {
	emitter := NewStreamingEmitter(onNode)
	runGenIntoWithResources(fs, target, hostP, targetP, emitter, onWarn, resources, testMode, true)

	return emitter.Finish()
}

type executor struct {
	srcRoot        string
	bldRoot        string
	sema           chan struct{}
	keepGoing      bool
	resourceMounts map[string]string
	cmdPrefixes    []cmdPrefix

	mu      sync.Mutex
	byUID   map[string]*nodeFuture
	events  chan func()
	stats   map[string][]time.Duration
	pending atomic.Uint64
	done    atomic.Uint64
}

type commandResult struct {
	Stderr string
}

type nodeFuture struct {
	node *Node
	once sync.Once
	err  *Exception
}

func newExecutor(srcRoot, bldRoot string, threads int, keepGoing bool, resourceMounts map[string]string, cmdPrefixes []cmdPrefix) *executor {
	return &executor{
		srcRoot:        srcRoot,
		bldRoot:        bldRoot,
		sema:           make(chan struct{}, threads),
		keepGoing:      keepGoing,
		resourceMounts: resourceMounts,
		cmdPrefixes:    cmdPrefixes,
		byUID:          make(map[string]*nodeFuture, 8192),
		events:         make(chan func(), 4096),
		stats:          map[string][]time.Duration{},
	}
}

func (ex *executor) onNode(n *Node) {
	// Dedup by uid: the generator may stream the same node (identical uid) more
	// than once. Each uid is one action and must run in exactly one goroutine —
	// two goroutines in the same tmp/<uid> would have one's forceRemoveAll wipe the
	// other's in-flight output (manifesting as clang "unable to rename temporary
	// … No such file or directory"). The first emit owns the future; later
	// duplicates are ignored (visit reuses the registered future).
	ex.mu.Lock()
	if _, ok := ex.byUID[n.UID]; ok {
		ex.mu.Unlock()
		return
	}
	f := &nodeFuture{node: n}
	ex.byUID[n.UID] = f
	ex.mu.Unlock()

	go ex.fire(f)
}

func (ex *executor) fire(f *nodeFuture) {
	Try(func() {
		ex.visit(f.node.UID)
	}).Catch(func(e *Exception) {
		if !ex.keepGoing {
			fatalException(e)
		}
	})
}

func fatalException(e *Exception) {
	fatalOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "\x1b[31m%s\x1b[0m\n", e.Error())
		os.Exit(1)
	})

	select {}
}

func (ex *executor) eventLoop() {
	for fn := range ex.events {
		fn()
	}
}

func (ex *executor) close() {
	close(ex.events)
}

func (ex *executor) run(roots []string) {
	if len(roots) == 0 {
		return
	}

	for _, uid := range roots {
		ex.visit(uid)
	}
}

func (ex *executor) visit(uid string) {
	f := ex.lookup(uid)
	if f == nil {
		ThrowFmt("executor: unknown UID %s", uid)
	}

	f.once.Do(func() {
		f.err = Try(func() {
			ex.execute(f.node)
		})
	})

	if f.err != nil {
		f.err.throw()
	}
}

func (ex *executor) failedRoots(roots []string) []string {
	var failed []string

	for _, uid := range roots {
		f := ex.lookup(uid)
		if f == nil || f.err == nil {
			continue
		}

		failed = append(failed, uid)
	}

	return failed
}

func (ex *executor) lookup(uid string) *nodeFuture {
	ex.mu.Lock()
	f := ex.byUID[uid]
	ex.mu.Unlock()

	return f
}

func (ex *executor) execute(n *Node) {
	cachePath := filepath.Join(ex.bldRoot, "uid", n.UID)
	if _, err := os.Stat(cachePath); err == nil {
		return
	}

	ex.pending.Add(1)
	defer ex.done.Add(1)

	if ex.keepGoing {
		for _, dep := range n.Deps {
			exc := Try(func() {
				ex.visit(dep)
			})
			if exc == nil {
				continue
			}

			ThrowFmt("deps failed: %s", dep)
		}
	} else {
		for _, dep := range n.Deps {
			ex.visit(dep)
		}
	}

	ex.sema <- struct{}{}
	defer func() { <-ex.sema }()

	tmp := filepath.Join(ex.bldRoot, "tmp", n.UID)
	_ = forceRemoveAll(tmp)
	defer forceRemoveAll(tmp)

	for _, dep := range n.Deps {
		ex.restoreInto(dep, tmp)
	}

	start := time.Now()
	cmdResult := ex.runNode(n, tmp)
	dur := time.Since(start)

	ex.storeOutputs(n, tmp)

	col, _ := n.KV["pc"].(string)
	kind, _ := n.KV["p"].(string)
	display := color(col, kind)

	done := ex.done.Load() + 1
	pending := ex.pending.Load()

	outFirst := ""
	if len(n.Outputs) > 0 {
		outFirst = n.Outputs[0].String()
	}

	rec := fmt.Sprintf("[%s] {%d/%d} %s", display, done, pending, outFirst)

	ex.events <- func() {
		if cmdResult.Stderr != "" {
			fmt.Fprintln(os.Stderr, cmdResult.Stderr)
		}

		ex.stats[kind] = append(ex.stats[kind], dur)
		fmt.Fprintln(os.Stderr, rec)
	}
}

// parseCmdPrefix parses a --cmd-prefix=<suffix>=<prefix tokens> value. The prefix
// (everything after the first '=') is split on whitespace into tokens.
func parseCmdPrefix(spec string) cmdPrefix {
	suffix, prefix, ok := strings.Cut(spec, "=")
	if !ok || suffix == "" {
		ThrowFmt("make: --cmd-prefix expects <suffix>=<prefix>, got %q", spec)
	}
	return cmdPrefix{suffix: suffix, prefix: strings.Fields(prefix)}
}

// applyCmdPrefixes inserts a rule's prefix tokens before every argument whose path
// ends with the rule's suffix. Operates on already-mounted (real-path) args, so a
// fetched binary referenced anywhere in the command — including as an argument to a
// wrapper that execs it — is run through the configured loader.
func applyCmdPrefixes(args []string, rules []cmdPrefix) []string {
	if len(rules) == 0 {
		return args
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		for _, r := range rules {
			if strings.HasSuffix(a, r.suffix) {
				out = append(out, r.prefix...)
				break
			}
		}
		out = append(out, a)
	}
	return out
}

const (
	cmdFileStartMarker = "--ya-start-command-file"
	cmdFileEndMarker   = "--ya-end-command-file"
)

// packCommandFiles replaces every `--ya-start-command-file … --ya-end-command-file`
// span with a single `@<buildRoot>/ya_command_file_<N>.args` argument whose file
// holds the enclosed arguments one per line. This is the response-file mechanism
// ya's runner applies before executing any command (NCommandFile::TCommandArgsPacker
// in devtools/ya/yalibrary/runner/command_file/command_file.cpp): the wrapper
// scripts (link_dyn_lib.py, …) and clang/lld consume `@file` but pass the markers
// through verbatim, so they must be resolved here, not left in the command. Nested
// spans recurse, the inner @file path being written into the outer file. counter is
// shared across one node's commands so the file names are unique.
func packCommandFiles(args []string, buildRoot string, counter *int) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == cmdFileStartMarker {
			i++ // skip the start marker
			out = append(out, consumeCommandFile(args, &i, buildRoot, counter))
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func consumeCommandFile(args []string, pos *int, buildRoot string, counter *int) string {
	path := filepath.Join(buildRoot, "ya_command_file_"+strconv.Itoa(*counter)+".args")
	*counter++

	var b strings.Builder
	for ; *pos < len(args); *pos++ {
		switch args[*pos] {
		case cmdFileStartMarker:
			*pos++ // skip the nested start marker
			b.WriteString(consumeCommandFile(args, pos, buildRoot, counter))
		case cmdFileEndMarker:
			Throw(os.WriteFile(path, []byte(b.String()), 0o644))
			return "@" + path
		default:
			b.WriteString(args[*pos])
		}
		b.WriteByte('\n')
	}

	Throw(os.WriteFile(path, []byte(b.String()), 0o644))
	return "@" + path
}

func (ex *executor) runNode(n *Node, tmp string) commandResult {
	var result commandResult

	for _, out := range n.Outputs {
		if !out.IsBuild() {
			continue
		}

		mounted := filepath.Join(tmp, out.Rel())
		Throw(os.MkdirAll(filepath.Dir(mounted), 0o755))
	}

	cmdFileCounter := 0
	for _, c := range n.Cmds {
		args := make([]string, len(c.CmdArgs))
		for i, a := range c.CmdArgs {
			args[i] = mountString(a, ex.srcRoot, tmp, ex.resourceMounts)
		}
		args = applyCmdPrefixes(args, ex.cmdPrefixes)
		args = packCommandFiles(args, tmp, &cmdFileCounter)

		dir := tmp
		if c.Cwd != "" {
			dir = mountString(c.Cwd, ex.srcRoot, tmp, ex.resourceMounts)
		}

		env := os.Environ()
		for k, v := range n.Env {
			env = append(env, k+"="+mountString(v, ex.srcRoot, tmp, ex.resourceMounts))
		}

		for k, v := range c.Env {
			env = append(env, k+"="+mountString(v, ex.srcRoot, tmp, ex.resourceMounts))
		}

		cmd := &exec.Cmd{
			Path: args[0],
			Args: args,
			Env:  env,
			Dir:  dir,
		}

		var stdoutW io.Writer = os.Stdout

		if c.Stdout != "" {
			path := mountString(c.Stdout, ex.srcRoot, tmp, ex.resourceMounts)
			Throw(os.MkdirAll(filepath.Dir(path), 0o755))

			f := Throw2(os.Create(path))
			defer f.Close()

			stdoutW = f
		}

		var stderr bytes.Buffer

		cmd.Stdout = stdoutW
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			msg := fmt.Sprintf("cmd failed (uid=%s): %v: %s", n.UID, err, strings.Join(args, " "))
			if stderr.Len() > 0 {
				msg += "\n" + strings.TrimRight(stderr.String(), "\n")
			}

			ThrowFmt("%s", msg)
		}

		if stderr.Len() > 0 {
			if result.Stderr != "" {
				result.Stderr += "\n"
			}

			result.Stderr += strings.TrimRight(stderr.String(), "\n")
		}
	}

	return result
}

func (ex *executor) storeOutputs(n *Node, tmp string) {
	meta := make(map[string]string, len(n.Outputs))

	for _, out := range n.Outputs {
		if !out.IsBuild() {
			ThrowFmt("node %s: non-Build output %v", n.UID, out)
		}

		src := filepath.Join(tmp, out.Rel())
		dst := casPath(ex.bldRoot, src)

		Throw(os.MkdirAll(filepath.Dir(dst), 0o755))
		_ = forceRemoveAll(dst)
		Throw(os.Rename(src, dst))

		meta[out.String()] = dst
	}

	uidPath := filepath.Join(ex.bldRoot, "uid", n.UID)
	Throw(os.MkdirAll(filepath.Dir(uidPath), 0o755))

	tmpPath := uidPath + ".tmp"
	Throw(os.WriteFile(tmpPath, Throw2(json.Marshal(meta)), 0o644))
	Throw(os.Rename(tmpPath, uidPath))
}

func (ex *executor) restoreInto(uid, where string) {
	metaPath := filepath.Join(ex.bldRoot, "uid", uid)
	data := Throw2(os.ReadFile(metaPath))

	var meta map[string]string

	Throw(json.Unmarshal(data, &meta))

	for outVFS, casLoc := range meta {
		if !vfsHasPrefix(outVFS) {
			ThrowFmt("malformed meta entry %q in %s", outVFS, metaPath)
		}
		// Resolve the $(B)/… path directly from the string. Do NOT Intern here:
		// execution goroutines run concurrently with the still-streaming generator,
		// and the global intern table is not safe for concurrent writes.
		target := mountString(outVFS, ex.srcRoot, where, nil)
		Throw(os.MkdirAll(filepath.Dir(target), 0o755))
		_ = os.Remove(target)
		Throw(os.Symlink(casLoc, target))
	}
}

func (ex *executor) installRoot(uid, where string) {
	if where == "" {
		return
	}

	ex.restoreInto(uid, where)
}

func mountString(s, srcRoot, bldRoot string, resources map[string]string) string {
	s = strings.ReplaceAll(s, "$(S)/", srcRoot+"/")
	s = strings.ReplaceAll(s, "$(B)/", bldRoot+"/")
	s = strings.ReplaceAll(s, "$(S)", srcRoot)
	s = strings.ReplaceAll(s, "$(B)", bldRoot)

	for pattern, rel := range resources {
		s = strings.ReplaceAll(s, "$("+pattern+")", filepath.Join(bldRoot, rel))
	}

	return s
}

func casPath(bldRoot, src string) string {
	h := sha256.New()
	info := Throw2(os.Stat(src))

	if info.IsDir() {
		hashDir(h, src)

		return filepath.Join(bldRoot, "cas", fmt.Sprintf("%x", h.Sum(nil)))
	}

	f := Throw2(os.Open(src))
	defer f.Close()

	Throw2(io.Copy(h, f))

	return filepath.Join(bldRoot, "cas", fmt.Sprintf("%x", h.Sum(nil)))
}

func hashDir(h hashWriter, root string) {
	Throw(filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == root {
			return nil
		}

		rel := Throw2(filepath.Rel(root, path))
		h.Write([]byte(rel))
		h.Write([]byte{0})

		if d.IsDir() {
			h.Write([]byte("dir"))
			h.Write([]byte{0})

			return nil
		}

		h.Write([]byte("file"))
		h.Write([]byte{0})

		f := Throw2(os.Open(path))
		defer f.Close()

		Throw2(io.Copy(h, f))

		return nil
	}))
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func parseMakeFlags(args []string) *makeFlags {

	state := getopt.NewState(append([]string{"ay-make"}, args...))

	config := getopt.Config{
		Opts:     getopt.OptStr("GrdktThD:j:B:o:I:"),
		LongOpts: getopt.LongOptStr("musl,help,xbuild:,install:,output:,stats,build-dir:,source-root:,keep-going,dump-graph,release,debug,target-platform:,host-platform:,host-platform-flag:,verbose,sandboxing,dump-ignored-macros,cmd-prefix:"),
		Mode:     getopt.ModeInOrder,
		Func:     getopt.FuncGetOptLong,
	}

	mf := &makeFlags{
		buildType: "debug",
		threads:   runtime.NumCPU(),
		tflags:    map[string]string{},
		hflags:    map[string]string{},
	}

	for opt, err := range state.All(config) {
		if err == getopt.ErrDone {
			break
		}

		Throw(err)

		switch {
		case opt.Char == 'h' || opt.Name == "help":
			printMakeUsage(os.Stdout)
			os.Exit(0)
		case opt.Char == 'k' || opt.Name == "keep-going":
			mf.keepGoing = true
		case opt.Char == 'G' || opt.Name == "dump-graph":
			mf.dumpGraph = true
		case opt.Name == "stats":
			mf.stats = true
		case opt.Name == "musl":
			parseKV(mf.tflags, "MUSL=yes")
		case opt.Char == 'T':
			mf.ninja = true
		case opt.Char == 't':
			mf.testLevel++
		case opt.Char == 'o' || opt.Name == "output":
			mf.outRoot = opt.OptArg
		case opt.Char == 'I' || opt.Name == "install":
			mf.installRoot = opt.OptArg
		case opt.Char == 'B' || opt.Name == "build-dir":
			mf.bldRoot = opt.OptArg
		case opt.Name == "source-root":
			mf.srcRoot = opt.OptArg
		case opt.Name == "cmd-prefix":
			mf.cmdPrefixes = append(mf.cmdPrefixes, parseCmdPrefix(opt.OptArg))
		case opt.Char == 'D':
			parseKV(mf.tflags, opt.OptArg)
		case opt.Char == 'j':
			n, perr := strconv.Atoi(opt.OptArg)
			Throw(perr)
			mf.threads = n
		case opt.Char == 'r' || opt.Name == "release":
			mf.buildType = "release"
		case opt.Char == 'd' || opt.Name == "debug":
			mf.buildType = "debug"
		case opt.Name == "xbuild":
			mf.buildType = opt.OptArg
		case opt.Name == "target-platform":
			mf.targetPlat = opt.OptArg
		case opt.Name == "host-platform":
			mf.hostPlat = opt.OptArg
		case opt.Name == "host-platform-flag":
			parseKV(mf.hflags, opt.OptArg)
		case opt.Name == "verbose":
			mf.verbose = true
		case opt.Name == "sandboxing":
			mf.sandboxing = true
		case opt.Name == "dump-ignored-macros":
			mf.dumpIgnoredMacros = true
			enableMacroAudit()
		case opt.Char == 1:

			mf.targets = append(mf.targets, opt.OptArg)
		default:
			ThrowFmt("make: unhandled flag %v", opt)
		}
	}

	if mf.srcRoot == "" {
		mf.srcRoot = Throw2(os.Getwd())
	}

	if mf.bldRoot == "" {
		mf.bldRoot = filepath.Join(Throw2(os.UserHomeDir()), ".ya", "ay")
	}

	if mf.outRoot == "" {
		mf.outRoot = filepath.Join(mf.bldRoot, "res")
	}

	if mf.installRoot == "" {
		mf.installRoot = mf.srcRoot
	}

	if len(mf.targets) == 0 {
		cwd, err := os.Getwd()
		if err == nil && strings.HasPrefix(cwd, mf.srcRoot+"/") {
			mf.targets = []string{cwd[len(mf.srcRoot)+1:]}
		}
	}

	return mf
}

func parseKV(into map[string]string, kv string) {
	idx := strings.IndexByte(kv, '=')

	if idx < 0 {
		into[kv] = "yes"
		return
	}

	into[kv[:idx]] = kv[idx+1:]
}

func printMakeUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: ay make [flags] [targets...]
Build the targets in dependency order, executing per-node cmds.

Layout flags:
    --source-root <path>          Source tree root.
    -B, --build-dir <path>        Build directory (default: ~/.ya/ay).
    -o, --output <path>           Output staging dir (default: <build-dir>/res).
    -I, --install <path>          Install outputs into this directory (default: source-root).

Execution flags:
    -j, --jobs <N>                Parallel exec slots (default: NumCPU); 0 = build-only.
    -k, --keep-going              Continue past per-node failures.
    --cmd-prefix <suffix>=<pfx>   Prepend <pfx> tokens before any command arg whose path
                                  ends with <suffix> (repeatable). E.g. run fetched glibc
                                  binaries through a loader: bin/java=/bin/ld.linux-so.2
    -T, --ninja                   Ninja-style per-line output (default: in-place repaint).
    -t, -tt, -ttt                 Generate test nodes (small / +medium / +large).
    --stats                       Print per-kind execution stats after the build.
    -G, --dump-graph              Log a graph summary to stderr after Gen.
    --verbose                     Emit Gen-time diagnostics (unsupported sysincl records, …) to stderr.
    --sandboxing                  Run test nodes under the filesystem sandbox.

Configuration flags:
    -r, --release                 GG_BUILD_TYPE=release.
    -d, --debug                   GG_BUILD_TYPE=debug (default).
    --xbuild <value>              GG_BUILD_TYPE=<value> (overrides -r/-d).
    --musl                        MUSL=yes.
    --target-platform <id>        Target platform id (default: <host>).
    --host-platform <id>          Host platform id (default: <host>).
    -D, --define KEY=VALUE        Target-axis -D flag (repeatable).
    --host-platform-flag KEY=V    Host-axis -D flag (repeatable).
`)
}

const (
	ansiESC = "\x1b"
	ansiRST = ansiESC + "[0m"
)

func color(name, s string) string {
	c, ok := ansiCols[name]
	if !ok {
		return s
	}

	return c + s + ansiRST
}
