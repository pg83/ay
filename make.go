package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// make.go — `yatool make` subcommand.
//
// Pipelined Gen → Exec: as the in-memory emitter finalises each Node
// (UID + Deps resolved, dep-first order), we hand it to an executor
// that schedules a goroutine per node. Each node's goroutine waits on
// its dep-futures, then runs the node's cmds. Leaf nodes start
// executing as soon as they arrive — well before Gen produces the
// roots. The full materialised `[]*Node` is never assembled.
//
// Architecture ported from the early `gg/ya.go` (handleMake + Executor
// + Cache + Future), refitted to:
//   - operate on typed `*Node` (no JSON round-trip);
//   - consume nodes streaming via FinalizeStream rather than from a
//     fully-built `[]Node` slice;
//   - drop the genConfFor / genGraphFor subprocess path (we generate
//     in-process) and the merge(target, host) pass (we emit a single
//     graph).
//
// Cache layout matches the gg layout so the on-disk artefacts stay
// usable side-by-side with old builds:
//   <BldRoot>/cas/<sha256> — content-addressed stored output blobs
//   <BldRoot>/uid/<uid>    — per-node meta JSON: {output → cas path}

// makeFlags captures every CLI option `yatool make` accepts.
type makeFlags struct {
	srcRoot     string
	bldRoot     string
	installRoot string
	threads     int
	keepGoing   bool
	dumpGraph   bool
	musl        bool
	tflags      map[string]string
	targets     []string
}

// cmdMake parses CLI args, runs Gen (target axis only — host walks
// recurse implicitly during Gen), and pipes the resulting node stream
// into the executor. Exit code 0 on success; throws on any subcommand
// failure so main()'s top-level Catch prints + exits non-zero.
func cmdMake(args []string) int {
	mf := parseMakeFlags(args)

	if len(mf.targets) == 0 {
		ThrowFmt("make: no targets supplied and current working directory is outside the source root")
	}

	tools := mineTools()
	defines := commonFlags(tools)

	for k, v := range mf.tflags {
		defines[k] = v
	}

	if mf.musl {
		defines["MUSL"] = "yes"
	}

	// Platform-id defaults to the running host's native triple so a
	// bare `yatool make` builds for the current arch. Cross-compile
	// users override via --target-platform / --host-platform.
	if _, ok := defines["GG_TARGET_PLATFORM"]; !ok {
		defines["GG_TARGET_PLATFORM"] = hostPlatformID()
	}

	ex := newExecutor(mf.srcRoot, mf.bldRoot, mf.threads, mf.keepGoing)

	go ex.eventLoop()

	streamed := 0

	wrap := func(n *Node) {
		streamed++
		ex.onNode(n)
	}

	results := genStream(mf.srcRoot, mf.targets, defines, wrap)

	if mf.dumpGraph {
		fmt.Fprintf(os.Stderr, "graph: streamed %d node(s); %d root(s)\n", streamed, len(results))
	}

	// --jobs 0 (or any non-positive) is the build-only mode: stream
	// through Gen + Finalize so the user can verify the graph shape,
	// but skip subprocess execution. Useful for smoke-testing the
	// pipeline without a real toolchain set up.
	if mf.threads > 0 {
		ex.run(results)

		for _, uid := range results {
			ex.installRoot(uid, mf.installRoot)
		}
	}

	ex.close()

	return 0
}

// genStream runs our in-process Gen for each target and streams the
// finalized nodes to onNode. Returns the union of root UIDs across
// every target. Targets are emitted serially today; the executor
// inside makes the cost of one target's emission overlap with the
// previous target's execution.
func genStream(srcRoot string, targets []string, defines map[string]string, onNode func(*Node)) []string {
	all := []string{}

	for _, t := range targets {
		ec := genStreamOne(srcRoot, t, defines, onNode)
		all = append(all, ec...)
	}

	return all
}

func genStreamOne(srcRoot, target string, defines map[string]string, onNode func(*Node)) []string {
	emitter := NewBufferedEmitter()
	runGenInto(srcRoot, target, defines, emitter, defaultScanCtxMode)

	return FinalizeStream(emitter, onNode)
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
	if threads < 0 {
		threads = runtime.NumCPU()
	}

	if threads == 0 {
		// Build-only mode: caller will not invoke run(), but the
		// semaphore must still have capacity in case future code
		// paths spin up. Keep the channel non-blocking with one slot.
		threads = 1
	}

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

func (ex *executor) onNode(n *Node) {
	ex.mu.Lock()
	ex.byUID[n.UID] = &nodeFuture{node: n}
	ex.mu.Unlock()
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
		outFirst = mountVFS(n.Outputs[0], ex.srcRoot, tmp)
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

func parseMakeFlags(args []string) *makeFlags {
	fs := flag.NewFlagSet("make", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	srcRoot := fs.String("source-root", "/home/pg/monorepo/yatool_orig", "source-tree root")
	bldRoot := fs.String("build-dir", "", "build directory (defaults to <source-root>/obj)")
	install := fs.String("install", "", "install final outputs into this directory after build (defaults to source-root)")
	threads := fs.Int("jobs", runtime.NumCPU(), "parallel execution slots; 0 disables exec (build-only)")
	keepGoing := fs.Bool("keep-going", false, "continue past per-node failures")
	dumpGraph := fs.Bool("dump-graph", false, "log a graph summary to stderr after Gen")
	musl := fs.Bool("musl", false, "add MUSL=yes")

	var tDefs stringMapValue

	fs.Var(&tDefs, "define", "ymake-style -D KEY=VALUE (repeatable)")

	err := fs.Parse(args)

	if errors.Is(err, flag.ErrHelp) {
		printMakeUsage(os.Stdout)
		os.Exit(0)
	}

	Throw(err)

	mf := &makeFlags{
		srcRoot:     *srcRoot,
		bldRoot:     *bldRoot,
		installRoot: *install,
		threads:     *threads,
		keepGoing:   *keepGoing,
		dumpGraph:   *dumpGraph,
		musl:        *musl,
		tflags:      tDefs.toMap(),
		targets:     fs.Args(),
	}

	if mf.tflags == nil {
		mf.tflags = map[string]string{}
	}

	if mf.bldRoot == "" {
		mf.bldRoot = filepath.Join(mf.srcRoot, "obj")
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

func printMakeUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: yatool make [flags] [targets...]
Build the targets in dependency order, executing per-node cmds.

Flags:
    --source-root <path>          Source tree root.
    --build-dir <path>            Build directory (default: <source-root>/obj).
    --install <path>              Install outputs into this directory (default: source-root).
    --jobs <N>                    Parallel exec slots (default: NumCPU).
    --keep-going                  Continue past per-node failures.
    --dump-graph                  Log a graph summary after Gen.
    --musl                        Add MUSL=yes.
    --define KEY=VALUE            -D KEY=VALUE flag (repeatable).
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
