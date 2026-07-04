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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const executorGCPercent = 400

type CmdPrefix struct {
	suffix string
	prefix []string
}

type Executor struct {
	srcRoot     string
	bldRoot     string
	fs          FS
	sema        chan struct{}
	keepGoing   bool
	cmdPrefixes []CmdPrefix
	ninja       bool
	sandboxing  bool
	grbDir      string
	futs        *PageVec[*NodeFuture]
	fetchRefs   *DenseMap[STR, NodeRef]
	events      *EventQueue
	stats       map[string][]time.Duration
	hashMu      sync.Mutex
	localHash   map[STR]uint64
	pending     atomic.Uint64
	done        atomic.Uint64
	tokenOnce   sync.Once
	token       string
}

func (ex *Executor) sandboxToken() string {
	ex.tokenOnce.Do(func() {
		ex.token = resolveSandboxToken()
	})

	return ex.token
}

type CommandResult struct {
	Stderr string
}

type NodeFuture struct {
	node *Node
	ref  NodeRef
	uid  UID
	once sync.Once
	err  *Exception
}

func newExecutor(srcRoot, bldRoot string, fs FS, threads int, keepGoing bool, ninja bool, sandboxing bool, cmdPrefixes []CmdPrefix, events *EventQueue) *Executor {
	return &Executor{
		srcRoot:     srcRoot,
		bldRoot:     bldRoot,
		fs:          fs,
		sema:        make(chan struct{}, threads),
		keepGoing:   keepGoing,
		ninja:       ninja,
		sandboxing:  sandboxing,
		grbDir:      filepath.Join(bldRoot, "grb"),
		cmdPrefixes: cmdPrefixes,
		futs:        &PageVec[*NodeFuture]{},
		events:      events,
		stats:       map[string][]time.Duration{},
		localHash:   map[STR]uint64{},
	}
}

func (ex *Executor) contentHash(v VFS) uint64 {
	if h := ex.fs.contentHash(v); h != 0 {
		return h
	}

	s := v.str()

	ex.hashMu.Lock()

	defer ex.hashMu.Unlock()

	if h, ok := ex.localHash[s]; ok {
		return h
	}

	h := hashSourceFile(ex.srcRoot, v.rel())

	ex.localHash[s] = h

	return h
}

func (ex *Executor) onNode(n *Node, fetchRefs *DenseMap[STR, NodeRef]) {
	ex.fetchRefs = fetchRefs

	f := &NodeFuture{node: n, ref: n.Ref}

	ex.futs.set(uint32(n.Ref), f)

	go ex.fire(f)
}

func (ex *Executor) fire(f *NodeFuture) {
	try(func() {
		ex.visit(f.ref)
	}).catch(func(e *Exception) {
		if !ex.keepGoing {
			fatalException(e)
		}
	})
}

func (ex *Executor) uidOf(n *Node) UID {
	if n.presetUID != (UID{}) {
		return n.presetUID
	}

	c := CanonBuf{hash: ex.contentHash, futs: ex.futs, fetchRefs: ex.fetchRefs}

	return c.calcUID(n)
}

func fatalException(e *Exception) {
	fatalOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "\x1b[31m%s\x1b[0m\n", e.Error())
		os.Exit(1)
	})

	select {}
}

func (ex *Executor) run(roots []NodeRef) {
	for _, r := range roots {
		ex.visit(r)
	}
}

func (ex *Executor) visit(ref NodeRef) {
	f := ex.futs.get(uint32(ref))

	if f == nil {
		throwFmt("executor: unknown NodeRef %d", ref)
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

func (ex *Executor) failedRoots(roots []NodeRef) []NodeRef {
	var failed []NodeRef

	for _, r := range roots {
		f := ex.futs.get(uint32(r))

		if f == nil || f.err == nil {
			continue
		}

		failed = append(failed, r)
	}

	return failed
}

func (ex *Executor) execute(f *NodeFuture) {
	n := f.node

	if ex.keepGoing {
		for r := range n.buildDeps(ex.fetchRefs) {
			exc := try(func() {
				ex.visit(r)
			})

			if exc == nil {
				continue
			}

			throwFmt("deps failed: %d", r)
		}
	} else {
		for r := range n.buildDeps(ex.fetchRefs) {
			ex.visit(r)
		}
	}

	f.uid = ex.uidOf(n)

	cachePath := ex.uidPath(f.uid)

	if _, err := os.Stat(cachePath); err == nil {
		return
	}

	ex.pending.Add(1)

	defer ex.done.Add(1)

	ex.sema <- struct{}{}

	defer func() { <-ex.sema }()

	tmp := filepath.Join(ex.bldRoot, "tmp", f.uid.string())

	throw(os.MkdirAll(tmp, 0o755))

	dir := throw2(os.Open(tmp))

	defer dir.Close()

	throw(syscall.Flock(int(dir.Fd()), syscall.LOCK_EX))

	if _, err := os.Stat(cachePath); err == nil {
		return
	}

	ex.removeContents(tmp)

	defer ex.discard(tmp)

	srcMount, bldMount := ex.srcRoot, tmp

	if ex.sandboxing {
		srcMount = filepath.Join(tmp, "s")
		bldMount = filepath.Join(tmp, "b")
		throw(os.MkdirAll(srcMount, 0o755))
		throw(os.MkdirAll(bldMount, 0o755))
		ex.linkSourceInputs(n, srcMount)
	}

	for r := range n.buildDeps(ex.fetchRefs) {
		ex.restoreInto(ex.futs.get(uint32(r)).uid, bldMount)
	}

	start := time.Now()
	cmdResult := ex.runNode(n, srcMount, bldMount)
	dur := time.Since(start)

	ex.storeOutputs(n, f.uid, bldMount)

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

	ex.events.post(func() {
		if cmdResult.Stderr != "" {
			if !ex.ninja {
				fmt.Fprint(os.Stderr, ansiESC+"[2K\r")
			}

			fmt.Fprintln(os.Stderr, cmdResult.Stderr)
		}

		ex.stats[kind.string()] = append(ex.stats[kind.string()], dur)

		if ex.ninja {
			fmt.Fprintln(os.Stderr, rec)
		} else {
			fmt.Fprint(os.Stderr, ansiESC+"[2K\r"+rec+"\r")
		}
	})
}

func parseCmdPrefix(spec string) CmdPrefix {
	suffix, prefix, ok := strings.Cut(spec, "=")

	if !ok || suffix == "" {
		throwFmt("make: --cmd-prefix expects <suffix>=<prefix>, got %q", spec)
	}

	return CmdPrefix{suffix: suffix, prefix: strings.Fields(prefix)}
}

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

func packCommandFiles(args []string, buildRoot string, counter *int) []string {
	out := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		if args[i] == cmdFileStartMarker {
			i++
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
			*pos++
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

		if n.KV.P == pkSB {
			args = append([]string{throw2(os.Executable()), "fetch", "sandbox", "--source-root", ex.srcRoot}, args[2:]...)
		} else {
			args = applyCmdPrefixes(args, ex.cmdPrefixes)
			args = packCommandFiles(args, bldMount, &cmdFileCounter)
		}

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

		if os.Getenv("YA_TOKEN") == "" && (n.KV.P == pkSB || (n.KV.P == pkFETCH && argsNeedSandboxToken(args))) {
			if tok := ex.sandboxToken(); tok != "" {
				env = append(env, "YA_TOKEN="+tok)
			}
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
			msg := fmt.Sprintf("cmd failed (ref=%d): %v: %s", n.Ref, err, strings.Join(args, " "))

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

type OutputEntry struct {
	Cas  string `json:"cas,omitempty"`
	Link string `json:"link,omitempty"`
}

func (ex *Executor) storeOutputs(n *Node, uid UID, tmp string) {
	meta := make(map[string]OutputEntry, len(n.Outputs))

	for _, out := range n.Outputs {
		if !out.isBuild() {
			throwFmt("node ref=%d: non-Build output %v", n.Ref, out)
		}

		ex.storePath(filepath.Join(tmp, out.rel()), out.string(), meta)
	}

	uidPath := ex.uidPath(uid)

	throw(os.MkdirAll(filepath.Dir(uidPath), 0o755))

	tf := throw2(os.CreateTemp(filepath.Dir(uidPath), "."+uid.string()+".*"))

	throw2(tf.Write(throw2(json.Marshal(meta))))
	throw(tf.Close())
	throw(os.Rename(tf.Name(), uidPath))
}

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
	if exc := try(func() { ex.restoreManifest(uid, where) }); exc != nil {
		_ = os.Remove(ex.uidPath(uid))
		exc.throw()
	}
}

func (ex *Executor) restoreManifest(uid UID, where string) {
	metaPath := ex.uidPath(uid)
	data := throw2(os.ReadFile(metaPath))

	var meta map[string]OutputEntry

	throw(json.Unmarshal(data, &meta))

	for outVFS, e := range meta {
		if !vfsHasPrefix(outVFS) {
			throwFmt("malformed meta entry %q in %s", outVFS, metaPath)
		}

		target := mountString(outVFS, ex.srcRoot, where)

		throw(os.MkdirAll(filepath.Dir(target), 0o755))
		_ = os.Remove(target)

		switch {
		case e.Link != "":
			throw(os.Symlink(e.Link, target))

		default:
			src := ex.casPathForHash(e.Cas)

			if err := os.Link(src, target); err != nil && !os.IsExist(err) {
				throw(copyFile(src, target))
			}
		}
	}
}

func (ex *Executor) installRoot(ref NodeRef, where string) {
	if where == "" {
		return
	}

	ex.restoreInto(ex.futs.get(uint32(ref)).uid, where)
}

func (ex *Executor) removeContents(dir string) {
	entries, err := os.ReadDir(dir)

	if err != nil {
		return
	}

	for _, e := range entries {
		ex.discard(filepath.Join(dir, e.Name()))
	}
}

func (ex *Executor) clearCache() {
	for _, name := range []string{"cas", "tmp", "uid"} {
		ex.discard(filepath.Join(ex.bldRoot, name))
	}
}

func (ex *Executor) discard(path string) {
	for {
		_ = os.MkdirAll(ex.grbDir, 0o755)

		dst := filepath.Join(ex.grbDir, strconv.FormatUint(rand.Uint64(), 36))

		if os.Rename(path, dst) == nil {
			return
		}
	}
}

func (ex *Executor) startGarbageCollector() {
	go func() {
		for {
			_ = exec.Command("rm", "-rf", ex.grbDir).Run()

			time.Sleep(time.Second)
		}
	}()
}

func mountString(s, srcRoot, bldRoot string) string {
	s = strings.ReplaceAll(s, "$(S)/", srcRoot+"/")
	s = strings.ReplaceAll(s, "$(B)/", bldRoot+"/")
	s = strings.ReplaceAll(s, "$(S)", srcRoot)
	s = strings.ReplaceAll(s, "$(B)", bldRoot)

	return s
}

func casHash(src string) string {
	h := sha256.New()
	f := throw2(os.Open(src))

	defer f.Close()

	throw2(io.Copy(h, f))

	return fmt.Sprintf("%x", h.Sum(nil))
}

func (ex *Executor) casPathForHash(hash string) string {
	return filepath.Join(ex.bldRoot, "cas", hash[:2], hash)
}

func (ex *Executor) uidPath(uid UID) string {
	s := uid.string()

	return filepath.Join(ex.bldRoot, "uid", s[:1], s)
}
