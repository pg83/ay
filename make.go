package main

import (
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

// make.go — `yatool make` subcommand.
//
// Pipelined Gen → Exec: as the emitter finalises each Node (UID + Deps
// resolved, dep-first order), it goes to an executor that schedules a
// goroutine per node. Each node's goroutine waits on its dep-futures,
// then runs the node's cmds. Leaf nodes start executing as soon as they
// arrive — the full materialised []*Node is never assembled.
//
// Cache layout matches the legacy gg layout so on-disk artefacts stay
// usable side-by-side:
//   <BldRoot>/cas/<sha256> — content-addressed output blobs
//   <BldRoot>/uid/<uid>    — per-node meta JSON: {output → cas path}

// makeFlags captures every CLI option `yatool make` accepts. Mirrors the
// legacy handleMake getopt grammar; the original short letters survive
// as Go-flag long names (stdlib flag rejects single-dash multi-char).
type makeFlags struct {
	srcRoot     string
	bldRoot     string
	outRoot     string
	installRoot string
	threads     int
	keepGoing   bool
	dumpGraph   bool
	stats       bool
	ninja       bool
	musl        bool
	buildType   string // release | debug | <custom xbuild value>
	targetPlat  string // empty → host
	hostPlat    string // empty → host
	tflags      map[string]string
	hflags      map[string]string
	targets     []string
	verbose     bool
}

// cmdMake parses CLI args, runs Gen (host walks recurse implicitly), and
// pipes the resulting node stream into the executor. Exit 0 on success;
// throws on any subcommand failure so main's Catch prints + exits non-zero.
func cmdMake(args []string) int {
	mf := parseMakeFlags(args)

	if len(mf.targets) == 0 {
		ThrowFmt("make: no targets supplied and current working directory is outside the source root")
	}

	// Toolchain flags feed both Platform halves (build-host invokes
	// these binaries regardless of which axis the cmd_args belong to).
	tools, conf := toolchainFlags(mf.srcRoot, nil)
	hostYaFlags := readYaConfSection(filepath.Join(mf.srcRoot, "ya.conf"), "host_platform_flags")
	targetYaFlags := readYaConfSection(filepath.Join(mf.srcRoot, "ya.conf"), "flags")

	// Host platform: `--host-platform` selects axes (mined when empty),
	// `--host-platform-flag KEY=VALUE` (mf.hflags) lands on host Flags,
	// PIC=yes, baseline tag "tool".
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
	hostP := NewPlatform(hOS, hISA, hostFlags, []string{"tool"}, true, "", "")

	// Target platform: `--target-platform` selects axes (defaults to
	// host when empty), `-D KEY=VALUE` (mf.tflags) lands on target Flags,
	// `--musl` toggles MUSL, `--xbuild` / `-r` / `-d` selects build type,
	// PIC=no.
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
	if mf.musl {
		targetFlags["MUSL"] = "yes"
	}
	if mf.buildType != "" {
		targetFlags["GG_BUILD_TYPE"] = mf.buildType
	}
	targetFlags["PIC"] = "no"
	targetP := NewPlatform(tOS, tISA, targetFlags, nil, false, os.Getenv("CFLAGS"), os.Getenv("CXXFLAGS"))

	// `-j 0` is no-exec mode (Gen runs, no subprocesses):
	//   - with `-G`: dump the graph as stable JSON (byte-exact match
	//     for `yatool gen --out -`).
	//   - without `-G`: Gen streams to a discard sink (smoke test).
	onWarn := func(w Warn) {
		// Unresolved includes are a build-stopping error by default —
		// they signal a real resolver gap. `--keep-going` downgrades
		// them to a warning (surfaced under `--verbose`).
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
				g := GenWithMode(mf.srcRoot, target, hostP, targetP, defaultScanCtxMode, onWarn)
				applyGraphConf(g, conf)
				writeGraph("-", g)
			}
		} else {
			genStream(mf.srcRoot, mf.targets, hostP, targetP, func(*Node) {}, onWarn)
		}

		return 0
	}

	ex := newExecutor(mf.srcRoot, mf.bldRoot, mf.threads, mf.keepGoing)

	go ex.eventLoop()

	defer ex.close()

	results := genStream(mf.srcRoot, mf.targets, hostP, targetP, ex.onNode, onWarn)

	ex.run(results)

	for _, uid := range results {
		ex.installRoot(uid, mf.installRoot)
	}

	return 0
}

// genStream runs in-process Gen for each target and streams finalized
// nodes to onNode. Returns the union of root UIDs. Targets run serially;
// the executor overlaps one target's emission with the previous one's
// execution.
func genStream(srcRoot string, targets []string, hostP, targetP *Platform, onNode func(*Node), onWarn func(Warn)) []string {
	all := []string{}

	for _, t := range targets {
		ec := genStreamOne(srcRoot, t, hostP, targetP, onNode, onWarn)
		all = append(all, ec...)
	}

	return all
}

func genStreamOne(srcRoot, target string, hostP, targetP *Platform, onNode func(*Node), onWarn func(Warn)) []string {
	emitter := NewStreamingEmitter(onNode)
	runGenInto(srcRoot, target, hostP, targetP, emitter, defaultScanCtxMode, onWarn)

	return emitter.Finish()
}

// executor — schedules and runs Node executions.
type executor struct {
	srcRoot   string
	bldRoot   string
	sema      chan struct{}
	keepGoing bool

	mu      sync.Mutex
	byUID   map[string]*nodeFuture
	events  chan func()
	stats   map[string][]time.Duration
	pending atomic.Uint64
	done    atomic.Uint64
}

type nodeFuture struct {
	node *Node
	once sync.Once
	err  *Exception
}

func newExecutor(srcRoot, bldRoot string, threads int, keepGoing bool) *executor {
	return &executor{
		srcRoot:   srcRoot,
		bldRoot:   bldRoot,
		sema:      make(chan struct{}, threads),
		keepGoing: keepGoing,
		byUID:     make(map[string]*nodeFuture, 8192),
		events:    make(chan func(), 4096),
		stats:     map[string][]time.Duration{},
	}
}

// onNode registers a freshly-finalized Node and spawns its future-runner
// so leaves start compiling while Gen is still emitting parents. The
// goroutine blocks inside visit→execute on its deps; the streaming
// emitter yields dep-first, so every dep is registered in byUID before
// the parent looks it up.
func (ex *executor) onNode(n *Node) {
	f := &nodeFuture{node: n}

	ex.mu.Lock()
	ex.byUID[n.UID] = f
	ex.mu.Unlock()

	go ex.fire(f)
}

// fire is the auto-spawned future-runner. It is a thin wrapper around
// ex.visit so a node's once.Do is triggered as soon as registration
// completes — without the caller (ex.run) needing to enumerate roots.
func (ex *executor) fire(f *nodeFuture) {
	Try(func() {
		ex.visit(f.node.UID)
	}).Catch(func(e *Exception) {
		// Errors are recorded on the future via the same path
		// ex.visit uses; nothing else to do here. The root waiter
		// in ex.run re-throws when it sees f.err != nil and
		// keepGoing is off.
		_ = e
	})
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

	var wg sync.WaitGroup

	for _, uid := range roots {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			ex.visit(u)
		}(uid)
	}

	wg.Wait()
}

// visit forces uid's future to run, blocking until it (and its
// transitive deps) complete.
func (ex *executor) visit(uid string) {
	f := ex.lookup(uid)
	if f == nil {
		ThrowFmt("executor: unknown UID %s", uid)
	}

	f.once.Do(func() {
		exc := Try(func() {
			ex.execute(f.node)
		})

		if exc != nil {
			f.err = exc
			if !ex.keepGoing {
				exc.throw()
			}

			fmt.Fprintf(os.Stderr, "node %s: %s\n", uid, exc.Error())
		}
	})

	if f.err != nil && !ex.keepGoing {
		f.err.throw()
	}
}

func (ex *executor) lookup(uid string) *nodeFuture {
	ex.mu.Lock()
	f := ex.byUID[uid]
	ex.mu.Unlock()

	return f
}

// execute is the core per-node lifecycle: wait on deps, check cache,
// run cmds, store outputs.
func (ex *executor) execute(n *Node) {
	cachePath := filepath.Join(ex.bldRoot, "uid", n.UID)
	if _, err := os.Stat(cachePath); err == nil {
		return
	}

	ex.pending.Add(1)
	defer ex.done.Add(1)

	// Visit deps in parallel.
	var wg sync.WaitGroup

	for _, dep := range n.Deps {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			ex.visit(d)
		}(dep)
	}

	wg.Wait()

	ex.sema <- struct{}{}
	defer func() { <-ex.sema }()

	tmp := filepath.Join(ex.bldRoot, "tmp", n.UID)
	_ = os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)

	// Restore dep outputs into tmp.
	for _, dep := range n.Deps {
		ex.restoreInto(dep, tmp)
	}

	start := time.Now()
	ex.runNode(n, tmp)
	dur := time.Since(start)

	ex.storeOutputs(n, tmp)

	col := n.KV["pc"]
	kind := n.KV["p"]
	display := color(col, kind)

	done := ex.done.Load() + 1
	pending := ex.pending.Load()

	outFirst := ""
	if len(n.Outputs) > 0 {
		outFirst = n.Outputs[0].String()
	}

	rec := fmt.Sprintf("[%s] {%d/%d} %s", display, done, pending, outFirst)

	ex.events <- func() {
		ex.stats[kind] = append(ex.stats[kind], dur)
	}

	fmt.Fprintln(os.Stderr, rec)
}

// runNode executes every Cmd in n. cwd / env / cmd_args paths are
// substituted with the per-node tmp dir for $(B) and the configured
// SrcRoot for $(S).
func (ex *executor) runNode(n *Node, tmp string) {
	// Pre-create every output's parent directory inside the tmp area
	// so subprocesses can write directly to their declared paths.
	for _, out := range n.Outputs {
		if !out.IsBuild() {
			continue
		}

		mounted := filepath.Join(tmp, out.Rel)
		Throw(os.MkdirAll(filepath.Dir(mounted), 0o755))
	}

	for _, c := range n.Cmds {
		args := make([]string, len(c.CmdArgs))
		for i, a := range c.CmdArgs {
			args[i] = mountString(a, ex.srcRoot, tmp)
		}

		dir := tmp
		if c.Cwd != "" {
			dir = mountString(c.Cwd, ex.srcRoot, tmp)
		}

		env := os.Environ()
		for k, v := range n.Env {
			env = append(env, k+"="+mountString(v, ex.srcRoot, tmp))
		}

		for k, v := range c.Env {
			env = append(env, k+"="+mountString(v, ex.srcRoot, tmp))
		}

		cmd := &exec.Cmd{
			Path: args[0],
			Args: args,
			Env:  env,
			Dir:  dir,
		}

		var stdoutW io.Writer = os.Stdout

		if c.Stdout != "" {
			path := mountString(c.Stdout, ex.srcRoot, tmp)
			Throw(os.MkdirAll(filepath.Dir(path), 0o755))

			f := Throw2(os.Create(path))
			defer f.Close()

			stdoutW = f
		}

		cmd.Stdout = stdoutW
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			ThrowFmt("cmd failed (uid=%s): %v: %s", n.UID, err, strings.Join(args, " "))
		}
	}
}

// storeOutputs moves each declared Build-rooted output from the tmp
// area into the content-addressable store and writes the per-node
// meta JSON. Source-rooted "outputs" never appear in production node
// shapes; if they ever do, we throw rather than guess.
func (ex *executor) storeOutputs(n *Node, tmp string) {
	meta := make(map[string]string, len(n.Outputs))

	for _, out := range n.Outputs {
		if !out.IsBuild() {
			ThrowFmt("node %s: non-Build output %v", n.UID, out)
		}

		src := filepath.Join(tmp, out.Rel)
		dst := casPath(ex.bldRoot, src)

		Throw(os.MkdirAll(filepath.Dir(dst), 0o755))
		Throw(os.Rename(src, dst))

		meta[out.String()] = dst
	}

	uidPath := filepath.Join(ex.bldRoot, "uid", n.UID)
	Throw(os.MkdirAll(filepath.Dir(uidPath), 0o755))

	tmpPath := uidPath + ".tmp"
	Throw(os.WriteFile(tmpPath, Throw2(json.Marshal(meta)), 0o644))
	Throw(os.Rename(tmpPath, uidPath))
}

// restoreInto reads the dep's meta JSON and symlinks each declared
// CAS-stored output back into `where` at its declared path. Used by
// both per-node tmp-dir staging and `installRoot` for final install.
func (ex *executor) restoreInto(uid, where string) {
	metaPath := filepath.Join(ex.bldRoot, "uid", uid)
	data := Throw2(os.ReadFile(metaPath))

	var meta map[string]string

	Throw(json.Unmarshal(data, &meta))

	for outVFS, casLoc := range meta {
		v, ok := ParseVFS(outVFS)
		if !ok {
			ThrowFmt("malformed meta entry %q in %s", outVFS, metaPath)
		}

		// Mount: $(B)/<rel> → where/<rel>; $(S)/<rel> → srcRoot/<rel>.
		target := mountVFS(v, ex.srcRoot, where)
		Throw(os.MkdirAll(filepath.Dir(target), 0o755))
		_ = os.Remove(target)
		Throw(os.Symlink(casLoc, target))
	}
}

// installRoot resolves the root UID's outputs at their declared paths
// under `where` (typically the source root or an explicit --install
// target). This is the final step after `run` so users can run the
// produced binary directly.
func (ex *executor) installRoot(uid, where string) {
	if where == "" {
		return
	}

	ex.restoreInto(uid, where)
}

// mountVFS materialises a VFS into a real filesystem path:
//   - Source rels stay anchored at srcRoot.
//   - Build rels stay anchored at bldRoot (the per-node tmp dir at
//     exec time, or the install target at install time).
func mountVFS(v VFS, srcRoot, bldRoot string) string {
	if v.IsSource() {
		return filepath.Join(srcRoot, v.Rel)
	}

	if v.IsBuild() {
		return filepath.Join(bldRoot, v.Rel)
	}

	ThrowFmt("mountVFS: zero-rooted VFS")

	return ""
}

// mountString substitutes "$(S)/" → srcRoot+"/", "$(B)/" → bldRoot+"/"
// inside a free-form cmd_arg / env value. Single pass per substring.
func mountString(s, srcRoot, bldRoot string) string {
	s = strings.ReplaceAll(s, "$(S)/", srcRoot+"/")
	s = strings.ReplaceAll(s, "$(B)/", bldRoot+"/")
	s = strings.ReplaceAll(s, "$(S)", srcRoot)
	s = strings.ReplaceAll(s, "$(B)", bldRoot)

	return s
}

// casPath returns the CAS storage path for the file at `src`. Each
// output is stored under <bldRoot>/cas/<sha256> so identical bytes
// across different uids share a single on-disk copy.
func casPath(bldRoot, src string) string {
	h := sha256.New()

	f := Throw2(os.Open(src))
	defer f.Close()

	Throw2(io.Copy(h, f))

	return filepath.Join(bldRoot, "cas", fmt.Sprintf("%x", h.Sum(nil)))
}

// --------------------------- CLI parsing ---------------------------

// parseMakeFlags parses argv via the same getopt grammar as the
// gg/ya.go original — short letters (-G/-r/-d/-k/-T) + value-bearing
// short letters (-D/-j/-B/-o/-I) + long names. Bare `-h`/`--help`
// prints usage and exits 0 (the gg/ya.go original didn't have one).
func parseMakeFlags(args []string) *makeFlags {
	// getopt's NewState convention: args[0] is the program name and is
	// skipped by the iterator. dispatch() hands us argv[2:] (the user
	// args only), so prepend a sentinel so the iterator sees every
	// flag the user typed.
	state := getopt.NewState(append([]string{"yatool-make"}, args...))

	config := getopt.Config{
		Opts:     getopt.OptStr("GrdkThD:j:B:o:I:"),
		LongOpts: getopt.LongOptStr("musl,help,xbuild:,install:,output:,stats,build-dir:,source-root:,keep-going,dump-graph,release,debug,target-platform:,host-platform:,host-platform-flag:,verbose"),
		Mode:     getopt.ModeInOrder,
		Func:     getopt.FuncGetOptLong,
	}

	mf := &makeFlags{
		buildType: "release",
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
			mf.musl = true
		case opt.Char == 'T':
			mf.ninja = true
		case opt.Char == 'o' || opt.Name == "output":
			mf.outRoot = opt.OptArg
		case opt.Char == 'I' || opt.Name == "install":
			mf.installRoot = opt.OptArg
		case opt.Char == 'B' || opt.Name == "build-dir":
			mf.bldRoot = opt.OptArg
		case opt.Name == "source-root":
			mf.srcRoot = opt.OptArg
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
		case opt.Char == 1:
			// Positional argument (target).
			mf.targets = append(mf.targets, opt.OptArg)
		default:
			ThrowFmt("make: unhandled flag %v", opt)
		}
	}

	if mf.srcRoot == "" {
		mf.srcRoot = "/home/pg/monorepo/yatool_orig"
	}

	if mf.bldRoot == "" {
		mf.bldRoot = filepath.Join(mf.srcRoot, "obj")
	}

	if mf.outRoot == "" {
		mf.outRoot = filepath.Join(mf.bldRoot, "res")
	}

	if mf.installRoot == "" {
		mf.installRoot = mf.srcRoot
	}

	// Default targets to the current module dir under source-root.
	if len(mf.targets) == 0 {
		cwd, err := os.Getwd()
		if err == nil && strings.HasPrefix(cwd, mf.srcRoot+"/") {
			mf.targets = []string{cwd[len(mf.srcRoot)+1:]}
		}
	}

	return mf
}

// parseKV splits `KEY=VALUE` on the first `=` and stores into `into`.
// A bare `KEY` (no `=`) is treated as `KEY=yes`, matching gg/ya.go's
// `Flags.parseInto` semantics.
func parseKV(into map[string]string, kv string) {
	idx := strings.IndexByte(kv, '=')

	if idx < 0 {
		into[kv] = "yes"
		return
	}

	into[kv[:idx]] = kv[idx+1:]
}

func printMakeUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: yatool make [flags] [targets...]
Build the targets in dependency order, executing per-node cmds.

Layout flags:
    --source-root <path>          Source tree root.
    -B, --build-dir <path>        Build directory (default: <source-root>/obj).
    -o, --output <path>           Output staging dir (default: <build-dir>/res).
    -I, --install <path>          Install outputs into this directory (default: source-root).

Execution flags:
    -j, --jobs <N>                Parallel exec slots (default: NumCPU); 0 = build-only.
    -k, --keep-going              Continue past per-node failures.
    -T, --ninja                   Ninja-style per-line output (default: in-place repaint).
    --stats                       Print per-kind execution stats after the build.
    -G, --dump-graph              Log a graph summary to stderr after Gen.
    --verbose                     Emit Gen-time diagnostics (unsupported sysincl records, …) to stderr.

Configuration flags:
    -r, --release                 GG_BUILD_TYPE=release (default).
    -d, --debug                   GG_BUILD_TYPE=debug.
    --xbuild <value>              GG_BUILD_TYPE=<value> (overrides -r/-d).
    --musl                        MUSL=yes.
    --target-platform <id>        Target platform id (default: <host>).
    --host-platform <id>          Host platform id (default: <host>).
    -D, --define KEY=VALUE        Target-axis -D flag (repeatable).
    --host-platform-flag KEY=V    Host-axis -D flag (repeatable).
`)
}

// --------------------------- colour helpers ---------------------------

const (
	ansiESC = "\x1b"
	ansiRST = ansiESC + "[0m"
)

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

func color(name, s string) string {
	c, ok := ansiCols[name]
	if !ok {
		return s
	}

	return c + s + ansiRST
}
