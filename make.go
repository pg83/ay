package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jon-codes/getopt"
)

var fatalOnce sync.Once

var ansiCols = map[string]string{
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

type MakeFlags struct {
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
	cmdPrefixes       []CmdPrefix
}

// cmdPrefix prepends prefix tokens before any command argument whose path ends
// with suffix. It lets the user run fetched binaries through an explicit ELF
// loader on systems lacking the binary's default interpreter, e.g.
// --cmd-prefix=bin/java=/bin/ld.linux-so.2 turns `… <JDK>/bin/java …` into
// `… /bin/ld.linux-so.2 <JDK>/bin/java …`.
type CmdPrefix struct {
	suffix string
	prefix []string
}

func copyStatsFlags(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

func readOptionalYaConfSection(fs FS, rel, wantSection string) map[string]string {
	if fs == nil || !fs.isFile(srcRootVFS, rel) {
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

func shouldExposeSandboxingTargetTags(mf *MakeFlags) bool {
	return mf != nil && mf.sandboxing && mf.testLevel > 0
}

// executorGCPercent is the GOGC value for builds (-j > 0), picked from the sg5
// GOGC scan — see the comment at the SetGCPercent call in cmdMake.
const executorGCPercent = 400

func cmdMake(args []string) int {
	if len(args) > 0 && args[0] == "cas" {
		return cmdCasAnalyze(args[1:])
	}

	defer startProfilesFromEnv()()

	mf := parseMakeFlags(args)

	if len(mf.targets) == 0 {
		throwFmt("make: no targets supplied and current working directory is outside the source root")
	}

	// -j 0 is generate-only: the process emits the graph and exits, so collecting
	// garbage along the way only costs CPU (measured on sg5: GC off cuts gen user
	// time ~20%, peak RSS 495->680 MB — fine for a short-lived process). With
	// executor threads the process lives for the whole build and shares RAM with
	// the compilers it spawns, so GC stays on — but far rarer than the default:
	// the sg5 GOGC scan plateaus at ~the GC-off user time from 400 up (the
	// 400..800 spread is the phase of the last mark cycle, not a trend), so 400
	// keeps near-full gen speed while still bounding the long-lived build
	// process's heap. An explicit GOGC in the environment wins in both modes.
	if os.Getenv("GOGC") == "" {
		if mf.threads == 0 {
			debug.SetGCPercent(-1)
		} else {
			debug.SetGCPercent(executorGCPercent)
		}
	}

	fs := newFS(mf.srcRoot)

	tools := toolchainFlags(fs)
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

	hostP := newPlatform(
		fs,
		hOS,
		hISA,
		hostFlags,
		[]string{"tool"},
		compilerFlagsFromConfig(rootHostYaFlags, hostInternalYaFlags, "CFLAGS", ""),
		compilerFlagsFromConfig(rootHostYaFlags, hostInternalYaFlags, "CXXFLAGS", ""),
	)

	targetSpec := mf.targetPlat

	if targetSpec == "" {
		targetSpec = string(makePlatformID(hOS, hISA))
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
	targetP := newPlatform(
		fs,
		tOS,
		tISA,
		targetFlags,
		nil,
		compilerFlagsFromConfig(rootTargetYaFlags, targetInternalYaFlags, "CFLAGS", os.Getenv("CFLAGS")),
		compilerFlagsFromConfig(rootTargetYaFlags, targetInternalYaFlags, "CXXFLAGS", os.Getenv("CXXFLAGS")),
	)

	if shouldExposeSandboxingTargetTags(mf) {
		targetP.Tags = sandboxingNodeTags(targetP)
	}

	onWarn := func(w Warn) {
		if w.Kind == WarnMissingInclude && !mf.keepGoing {
			throwFmt("%s: %s", w.Kind, w.Message)
		}

		if mf.verbose {
			fmt.Fprintf(os.Stderr, "\x1b[33m%s: %s\x1b[0m\n", w.Kind, w.Message)
		}
	}

	if mf.threads == 0 {
		if mf.dumpGraph {
			for _, target := range mf.targets {
				g := genDumpGraphWithResources(fs, target, hostP, targetP, onWarn, mf.testLevel > 0)
				writeGraph("-", g, !mf.sandboxing)
			}
		} else {
			genStream(fs, mf.targets, hostP, targetP, func(*Node, *UidVec) {}, onWarn, mf.testLevel > 0)
		}

		if mf.dumpIgnoredMacros {
			dumpMacroAudit(os.Stderr)
		}

		return 0
	}

	ex := newExecutor(mf.srcRoot, mf.bldRoot, mf.threads, mf.keepGoing, mf.ninja, mf.sandboxing, mf.cmdPrefixes)
	ex.startGarbageCollector()

	go ex.eventLoop()

	defer ex.close()

	executorWarn := func(w Warn) {
		if w.Kind == WarnMissingInclude && !mf.keepGoing {
			throwFmt("%s: %s", w.Kind, w.Message)
		}

		if mf.verbose {
			kind := w.Kind
			message := w.Message

			ex.events <- func() {
				fmt.Fprintf(os.Stderr, "\x1b[33m%s: %s\x1b[0m\n", kind, message)
			}
		}
	}

	results := genStream(fs, mf.targets, hostP, targetP, ex.onNode, executorWarn, mf.testLevel > 0)

	ex.run(results)

	failedRoots := ex.failedRoots(results)

	if len(failedRoots) > 0 {
		failedStrs := make([]string, len(failedRoots))

		for i, u := range failedRoots {
			failedStrs[i] = u.string()
		}

		throwFmt("build failed: %s", strings.Join(failedStrs, ", "))
	}

	for _, uid := range results {
		ex.installRoot(uid, mf.installRoot)
	}

	return 0
}

func genStream(fs FS, targets []string, hostP, targetP *Platform, onNode func(*Node, *UidVec), onWarn func(Warn), testMode bool) []UID {
	all := []UID{}

	for _, t := range targets {
		ec := genStreamOne(fs, t, hostP, targetP, onNode, onWarn, testMode)
		all = append(all, ec...)
	}

	return all
}

func genStreamOne(fs FS, target string, hostP, targetP *Platform, onNode func(*Node, *UidVec), onWarn func(Warn), testMode bool) []UID {
	emitter := newStreamingEmitter(onNode)
	runGenIntoWithResources(fs, target, hostP, targetP, emitter, onWarn, testMode)

	return emitter.finish()
}

type Executor struct {
	srcRoot     string
	bldRoot     string
	sema        chan struct{}
	keepGoing   bool
	cmdPrefixes []CmdPrefix
	// ninja selects per-line progress output (each status on its own line).
	// Default (false) repaints a single status line in place (\x1b[2K\r).
	ninja bool
	// sandboxing runs each node hermetically: its workspace X gets X/s + X/b, the
	// declared $(S) inputs are symlinked into X/s (and $(S) mounts to X/s), while the
	// dep outputs land in X/b as usual ($(B) mounts to X/b). A command that reads an
	// undeclared source then fails — the input set is exactly what the graph declares.
	sandboxing bool

	// grbDir (bldRoot/grb, sibling of cas/ and tmp/) is where discard renames doomed
	// workspaces; startGarbageCollector empties it in the background.
	grbDir string

	mu      sync.Mutex
	byUID   map[UID]*NodeFuture
	events  chan func()
	stats   map[string][]time.Duration
	pending atomic.Uint64
	done    atomic.Uint64
}

type CommandResult struct {
	Stderr string
}

type NodeFuture struct {
	node *Node
	uids *UidVec // resolves node.DepRefs -> dep uids (per emitter; deps are not materialized)
	once sync.Once
	err  *Exception
}

func newExecutor(srcRoot, bldRoot string, threads int, keepGoing bool, ninja bool, sandboxing bool, cmdPrefixes []CmdPrefix) *Executor {
	return &Executor{
		srcRoot:     srcRoot,
		bldRoot:     bldRoot,
		sema:        make(chan struct{}, threads),
		keepGoing:   keepGoing,
		ninja:       ninja,
		sandboxing:  sandboxing,
		grbDir:      filepath.Join(bldRoot, "grb"),
		cmdPrefixes: cmdPrefixes,
		byUID:       make(map[UID]*NodeFuture, 8192),
		events:      make(chan func(), 4096),
		stats:       map[string][]time.Duration{},
	}
}

func (ex *Executor) onNode(n *Node, uids *UidVec) {
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

	f := &NodeFuture{node: n, uids: uids}
	ex.byUID[n.UID] = f
	ex.mu.Unlock()

	go ex.fire(f)
}

func (ex *Executor) fire(f *NodeFuture) {
	try(func() {
		ex.visit(f.node.UID)
	}).catch(func(e *Exception) {
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

func (ex *Executor) eventLoop() {
	for fn := range ex.events {
		fn()
	}
}

func (ex *Executor) close() {
	close(ex.events)
}

func (ex *Executor) run(roots []UID) {
	if len(roots) == 0 {
		return
	}

	for _, uid := range roots {
		ex.visit(uid)
	}
}

func (ex *Executor) visit(uid UID) {
	f := ex.lookup(uid)

	if f == nil {
		throwFmt("executor: unknown UID %s", uid)
	}

	f.once.Do(func() {
		f.err = try(func() {
			ex.execute(f)
		})
	})

	if f.err != nil {
		f.err.throw()
	}
}

func (ex *Executor) failedRoots(roots []UID) []UID {
	var failed []UID

	for _, uid := range roots {
		f := ex.lookup(uid)

		if f == nil || f.err == nil {
			continue
		}

		failed = append(failed, uid)
	}

	return failed
}

func (ex *Executor) lookup(uid UID) *NodeFuture {
	ex.mu.Lock()
	f := ex.byUID[uid]
	ex.mu.Unlock()

	return f
}

func (ex *Executor) execute(f *NodeFuture) {
	n := f.node
	cachePath := ex.uidPath(n.UID)

	if _, err := os.Stat(cachePath); err == nil {
		return
	}

	ex.pending.Add(1)
	defer ex.done.Add(1)

	if ex.keepGoing {
		for _, r := range n.DepRefs {
			dep := f.uids.get(r)
			exc := try(func() {
				ex.visit(dep)
			})

			if exc == nil {
				continue
			}

			throwFmt("deps failed: %s", dep)
		}
	} else {
		for _, r := range n.DepRefs {
			ex.visit(f.uids.get(r))
		}
	}

	ex.sema <- struct{}{}

	defer func() { <-ex.sema }()

	tmp := filepath.Join(ex.bldRoot, "tmp", n.UID.string())
	throw(os.MkdirAll(tmp, 0o755))

	// Lock the workspace DIR (an exclusive flock on its fd) for the whole node, so a
	// second ay process building the same uid does not clean or clobber it while we
	// work. Best-effort serialization, not a correctness requirement — outputs are
	// deterministic and published atomically (CAS hard-link, uid temp+rename), so even
	// a concurrent rebuild is safe; the lock just avoids the wasted duplicate work.
	dir := throw2(os.Open(tmp))
	defer dir.Close()
	throw(syscall.Flock(int(dir.Fd()), syscall.LOCK_EX))

	// Another process may have finished this node while we waited for the lock.
	if _, err := os.Stat(cachePath); err == nil {
		return
	}

	ex.removeContents(tmp) // clear any stale workspace left by a crashed prior run
	defer ex.discard(tmp)

	// srcMount/bldMount are what $(S)/$(B) resolve to for this node. Without
	// sandboxing $(S) is the whole source root and $(B) is the workspace itself.
	// With sandboxing $(S) is X/s (only the declared source inputs, symlinked in) and
	// $(B) is X/b (the dep outputs, restored whole as usual).
	srcMount, bldMount := ex.srcRoot, tmp

	if ex.sandboxing {
		srcMount = filepath.Join(tmp, "s")
		bldMount = filepath.Join(tmp, "b")
		throw(os.MkdirAll(srcMount, 0o755))
		throw(os.MkdirAll(bldMount, 0o755))
		ex.linkSourceInputs(n, srcMount)
	}

	for _, r := range n.DepRefs {
		ex.restoreInto(f.uids.get(r), bldMount)
	}

	start := time.Now()
	cmdResult := ex.runNode(n, srcMount, bldMount)
	dur := time.Since(start)

	ex.storeOutputs(n, bldMount)

	col := n.KV.PC
	kind := n.KV.P
	display := color(col.string(), kind.string())

	done := ex.done.Load() + 1
	pending := ex.pending.Load()

	outFirst := ""

	if len(n.Outputs) > 0 {
		outFirst = n.Outputs[0].string()
	}

	rec := fmt.Sprintf("[%s] {%d/%d} %s", display, done, pending, outFirst)

	ex.events <- func() {
		if cmdResult.Stderr != "" {
			if !ex.ninja {
				// erase the in-place status line before committing real output
				fmt.Fprint(os.Stderr, ansiESC+"[2K\r")
			}

			fmt.Fprintln(os.Stderr, cmdResult.Stderr)
		}

		ex.stats[kind.string()] = append(ex.stats[kind.string()], dur)

		if ex.ninja {
			fmt.Fprintln(os.Stderr, rec)
		} else {
			// repaint a single status line in place: clear it, print, return to col 0
			fmt.Fprint(os.Stderr, ansiESC+"[2K\r"+rec+"\r")
		}
	}
}

// parseCmdPrefix parses a --cmd-prefix=<suffix>=<prefix tokens> value. The prefix
// (everything after the first '=') is split on whitespace into tokens.
func parseCmdPrefix(spec string) CmdPrefix {
	suffix, prefix, ok := strings.Cut(spec, "=")

	if !ok || suffix == "" {
		throwFmt("make: --cmd-prefix expects <suffix>=<prefix>, got %q", spec)
	}

	return CmdPrefix{suffix: suffix, prefix: strings.Fields(prefix)}
}

// applyCmdPrefixes inserts a rule's prefix tokens before every argument whose path
// ends with the rule's suffix. Operates on already-mounted (real-path) args, so a
// fetched binary referenced anywhere in the command — including as an argument to a
// wrapper that execs it — is run through the configured loader.
func applyCmdPrefixes(args []string, rules []CmdPrefix) []string {
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
			throw(os.WriteFile(path, []byte(b.String()), 0o644))
			return "@" + path
		default:
			b.WriteString(args[*pos])
		}

		b.WriteByte('\n')
	}

	throw(os.WriteFile(path, []byte(b.String()), 0o644))
	return "@" + path
}

func (ex *Executor) runNode(n *Node, srcMount, bldMount string) CommandResult {
	var result CommandResult

	for _, out := range n.Outputs {
		if !out.isBuild() {
			continue
		}

		mounted := filepath.Join(bldMount, out.rel())
		throw(os.MkdirAll(filepath.Dir(mounted), 0o755))
	}

	cmdFileCounter := 0

	for _, c := range n.Cmds {
		flatArgs := c.CmdArgs.flat()
		args := make([]string, len(flatArgs))

		for i, a := range flatArgs {
			args[i] = mountString(a.string(), srcMount, bldMount)
		}

		args = applyCmdPrefixes(args, ex.cmdPrefixes)
		args = packCommandFiles(args, bldMount, &cmdFileCounter)

		dir := bldMount

		if c.Cwd != 0 {
			dir = mountString(c.Cwd.string(), srcMount, bldMount)
		}

		env := os.Environ()

		for _, e := range n.Env {
			env = append(env, e.Name.string()+"="+mountString(e.Value.string(), srcMount, bldMount))
		}

		for _, e := range c.Env {
			env = append(env, e.Name.string()+"="+mountString(e.Value.string(), srcMount, bldMount))
		}

		cmd := &exec.Cmd{
			Path: args[0],
			Args: args,
			Env:  env,
			Dir:  dir,
		}

		var stdoutW io.Writer = os.Stdout

		if c.Stdout != 0 {
			path := mountString(c.Stdout.string(), srcMount, bldMount)
			throw(os.MkdirAll(filepath.Dir(path), 0o755))

			f := throw2(os.Create(path))
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

			throwFmt("%s", msg)
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

// linkSourceInputs symlinks the node's declared $(S) inputs into the sandbox source
// dir (mirroring each input's rel path), so a sandboxed command sees exactly the
// sources the graph declares and nothing else. $(B) inputs need no such filtering —
// they are the dep outputs, restored whole into the sandbox build dir.
func (ex *Executor) linkSourceInputs(n *Node, srcMount string) {
	for _, chunk := range n.Inputs {
		for _, in := range chunk {
			if !in.isSource() {
				continue
			}

			rel := in.rel()
			target := filepath.Join(srcMount, rel)
			throw(os.MkdirAll(filepath.Dir(target), 0o755))
			_ = os.Remove(target)
			throw(os.Symlink(filepath.Join(ex.srcRoot, rel), target))
		}
	}
}

// outputEntry is one materialized leaf of a node's output in the uid manifest:
// either a CAS-stored regular file (Cas = its content-addressed path) or a symlink
// (Link = the link target, verbatim). A directory output expands to one entry per
// leaf, so the CAS holds only files — never directories.
type OutputEntry struct {
	Cas  string `json:"cas,omitempty"`
	Link string `json:"link,omitempty"`
}

func (ex *Executor) storeOutputs(n *Node, tmp string) {
	meta := make(map[string]OutputEntry, len(n.Outputs))

	for _, out := range n.Outputs {
		if !out.isBuild() {
			throwFmt("node %s: non-Build output %v", n.UID, out)
		}

		ex.storePath(filepath.Join(tmp, out.rel()), out.string(), meta)
	}

	uidPath := ex.uidPath(n.UID)
	throw(os.MkdirAll(filepath.Dir(uidPath), 0o755))

	// Atomic publish: write to a UNIQUE temp in the same dir, then rename over the
	// final path (rename is atomic on one fs). CreateTemp's unique name means two
	// writers — even another ay process building the same node — never share the
	// temp, so the rename target is always a fully-written manifest.
	tf := throw2(os.CreateTemp(filepath.Dir(uidPath), "."+n.UID.string()+".*"))
	throw2(tf.Write(throw2(json.Marshal(meta))))
	throw(tf.Close())
	throw(os.Rename(tf.Name(), uidPath))
}

// storePath records src (a regular file, a symlink, or a directory tree) into meta,
// keyed by the $(B) output path. Regular files go to the CAS by content; symlinks
// are kept as symlinks (their target verbatim, NOT followed) so the link structure
// of a fetched tree (e.g. clang++ -> clang) survives; directories recurse, one entry
// per leaf — the CAS never holds a directory.
func (ex *Executor) storePath(src, outPath string, meta map[string]OutputEntry) {
	info := throw2(os.Lstat(src))

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		meta[outPath] = OutputEntry{Link: throw2(os.Readlink(src))}
	case info.IsDir():
		for _, e := range throw2(os.ReadDir(src)) {
			ex.storePath(filepath.Join(src, e.Name()), outPath+"/"+e.Name(), meta)
		}
	default:
		meta[outPath] = OutputEntry{Cas: ex.storeFileToCAS(src)}
	}
}

// storeFileToCAS hard-links src into the CAS at its content hash and returns the
// hash (the manifest stores the bare hash, not the path). Hard-link, not rename: an
// unpacked resource tree's dirs are often write-less (archived perms), so renaming a
// file out of one is denied — linking adds a name in the CAS dir without touching
// src's dir. Same inode, same content, so the workspace copy is dropped by the tmp
// cleanup. Content-addressed: a file already present (same content, possibly from a
// concurrent node) is a no-op.
func (ex *Executor) storeFileToCAS(src string) string {
	hash := casHash(src)
	dst := ex.casPathForHash(hash)

	if _, err := os.Stat(dst); err == nil {
		return hash
	}

	throw(os.MkdirAll(filepath.Dir(dst), 0o755))

	if err := os.Link(src, dst); err != nil && !os.IsExist(err) {
		throw(err)
	}

	return hash
}

func (ex *Executor) restoreInto(uid UID, where string) {
	metaPath := ex.uidPath(uid)
	data := throw2(os.ReadFile(metaPath))

	var meta map[string]OutputEntry

	throw(json.Unmarshal(data, &meta))

	for outVFS, e := range meta {
		if !vfsHasPrefix(outVFS) {
			throwFmt("malformed meta entry %q in %s", outVFS, metaPath)
		}

		// Resolve the $(B)/… path directly from the string. Do NOT Intern here:
		// execution goroutines run concurrently with the still-streaming generator,
		// and the global intern table is not safe for concurrent writes.
		target := mountString(outVFS, ex.srcRoot, where)
		throw(os.MkdirAll(filepath.Dir(target), 0o755))
		_ = os.Remove(target)

		switch {
		case e.Link != "":
			// Re-create the symlink verbatim (its target was kept, not followed); a
			// relative link like clang++ -> clang resolves within the restored tree.
			throw(os.Symlink(e.Link, target))

		case strings.HasPrefix(outVFS, "$(B)/resources/"):
			// Toolchain trees are hard-linked, not symlinked: a tool (clang, python)
			// finds its bundled resources (clang's builtin headers, python's stdlib)
			// relative to its OWN binary path. A symlink resolves to the flat CAS, so
			// those relative dirs vanish; a hard link keeps a real file at the tree
			// path (sharing the CAS inode), so the layout the tool expects survives.
			if err := os.Link(ex.casPathForHash(e.Cas), target); err != nil && !os.IsExist(err) {
				throw(err)
			}

		default:
			throw(os.Symlink(ex.casPathForHash(e.Cas), target))
		}
	}
}

func (ex *Executor) installRoot(uid UID, where string) {
	if where == "" {
		return
	}

	ex.restoreInto(uid, where)
}

// removeContents deletes everything inside dir but keeps dir itself — the under-lock
// clean of a possibly-stale workspace left by a crashed prior run. Best-effort.
func (ex *Executor) removeContents(dir string) {
	entries, err := os.ReadDir(dir)

	if err != nil {
		return
	}

	for _, e := range entries {
		ex.discard(filepath.Join(dir, e.Name()))
	}
}

// discard removes path the fast way: it renames path into grbDir under a random name
// (one rename — O(1) regardless of tree size, and fine on write-less trees since
// rename only touches the parent dirs) and leaves the real delete to the background
// collector. The random suffix keeps the name unique even against a neighbouring
// process. If the rename fails (missing path, cross-fs) it deletes in place.
func (ex *Executor) discard(path string) {
	dst := filepath.Join(ex.grbDir, strconv.FormatUint(rand.Uint64(), 36))

	if os.Rename(path, dst) == nil {
		return
	}

	_ = forceRemoveAll(path)
}

// startGarbageCollector deletes grbDir entries once a second in the background. It is
// best-effort and never waited on: the process exits with it still running, and any
// leftover is cleared by the next run's collector.
func (ex *Executor) startGarbageCollector() {
	throw(os.MkdirAll(ex.grbDir, 0o755))

	go func() {
		for {
			time.Sleep(time.Second)

			entries, err := os.ReadDir(ex.grbDir)

			if err != nil {
				continue
			}

			for _, e := range entries {
				_ = forceRemoveAll(filepath.Join(ex.grbDir, e.Name()))
			}
		}
	}()
}

// mountString substitutes the $(S)/$(B) roots. Resources are real graph nodes
// producing $(B)/resources/NAME (and vcs.json at $(B)/vcs.json), so they resolve
// through $(B) like any build output — no per-resource $(NAME) mount. The only
// $(NAME) left ($(TOOL_ROOT) in debug-/macro-prefix-map flags) is deliberately
// not expanded.
func mountString(s, srcRoot, bldRoot string) string {
	s = strings.ReplaceAll(s, "$(S)/", srcRoot+"/")
	s = strings.ReplaceAll(s, "$(B)/", bldRoot+"/")
	s = strings.ReplaceAll(s, "$(S)", srcRoot)
	s = strings.ReplaceAll(s, "$(B)", bldRoot)

	return s
}

// casHash is a regular file's content hash (hex sha256) — its identity in the CAS.
// Only files reach the CAS; directory outputs are expanded to files in storePath.
func casHash(src string) string {
	h := sha256.New()

	f := throw2(os.Open(src))
	defer f.Close()

	throw2(io.Copy(h, f))

	return fmt.Sprintf("%x", h.Sum(nil))
}

// casPathForHash is where a CAS object lives, sharded by the first 2 hash chars:
// cas/<hh>/<hash>. The uid manifest stores only the bare hash; this rebuilds the path.
func (ex *Executor) casPathForHash(hash string) string {
	return filepath.Join(ex.bldRoot, "cas", hash[:2], hash)
}

// uidPath is a node's manifest path, sharded by the first uid char: uid/<u>/<uid>.
func (ex *Executor) uidPath(uid UID) string {
	s := uid.string()

	return filepath.Join(ex.bldRoot, "uid", s[:1], s)
}

func parseMakeFlags(args []string) *MakeFlags {
	state := getopt.NewState(append([]string{"ay-make"}, args...))
	config := getopt.Config{
		Opts:     getopt.OptStr("GrdktThD:j:B:o:I:"),
		LongOpts: getopt.LongOptStr("musl,help,xbuild:,install:,output:,stats,build-dir:,source-root:,keep-going,dump-graph,release,debug,target-platform:,host-platform:,host-platform-flag:,verbose,sandboxing,dump-ignored-macros,cmd-prefix:"),
		Mode:     getopt.ModeInOrder,
		Func:     getopt.FuncGetOptLong,
	}

	mf := &MakeFlags{
		buildType: "debug",
		threads:   runtime.NumCPU(),
		tflags:    map[string]string{},
		hflags:    map[string]string{},
	}

	for opt, err := range state.All(config) {
		if err == getopt.ErrDone {
			break
		}

		throw(err)

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
			throw(perr)
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
			throwFmt("make: unhandled flag %v", opt)
		}
	}

	if mf.srcRoot == "" {
		mf.srcRoot = throw2(os.Getwd())
	}

	if mf.bldRoot == "" {
		mf.bldRoot = filepath.Join(throw2(os.UserHomeDir()), ".ya", "ay")
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
