package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	iofs "io/fs"
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

const (
	executorGCPercent = 400
	prepBatchMax      = 128
)

type CmdPrefix struct {
	suffix string
	prefix []string
}

type Executor struct {
	srcRoot      string
	bldRoot      string
	fs           FS
	sema         chan struct{}
	keepGoing    bool
	cmdPrefixes  []CmdPrefix
	ninja        bool
	sandboxing   bool
	grbDir       string
	futs         *PageVec[*NodeFuture]
	fetchRefs    *DenseMap[STR, NodeRef]
	uidSet       map[string]struct{}
	uidSetReady  chan struct{}
	events       *EventQueue
	stats        map[string][]time.Duration
	canon        CanonBuf
	localHash    map[STR]uint64
	prepBatch    []*NodeFuture
	prepBatchCap int
	pending      atomic.Uint64
	done         atomic.Uint64
	failed       atomic.Uint64
	inFlight     sync.WaitGroup
	tokenOnce    sync.Once
	token        string
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
	ex := &Executor{
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

		prepBatchCap: 1,
	}

	ex.canon = CanonBuf{hash: ex.contentHash, futs: ex.futs, chunkMemo: map[ChunkKey]ChunkAccum{}}

	if osfs, ok := fs.(*OsFS); ok {
		ex.canon.fsHashes = &osfs.contentHashes
	}

	ex.uidSetReady = make(chan struct{})

	go ex.loadUidSet()

	return ex
}

func (ex *Executor) loadUidSet() {
	set := map[string]struct{}{}
	base := filepath.Join(ex.bldRoot, "uid")

	if tops, err := os.ReadDir(base); err == nil {
		for _, top := range tops {
			entries, err := os.ReadDir(filepath.Join(base, top.Name()))

			if err != nil {
				continue
			}

			for _, e := range entries {
				set[e.Name()] = struct{}{}
			}
		}
	}

	ex.uidSet = set

	close(ex.uidSetReady)
}

func (ex *Executor) uidCached(uid UID) bool {
	<-ex.uidSetReady

	if _, ok := ex.uidSet[uid.string()]; ok {
		return true
	}

	_, err := os.Stat(ex.uidPath(uid))

	return err == nil
}

func (ex *Executor) contentHash(v VFS) uint64 {
	if h := ex.fs.contentHash(v.rel()); h != 0 {
		return h
	}

	s := v.any()

	if h, ok := ex.localHash[s.str()]; ok {
		return h
	}

	h := hashSourceFile(ex.srcRoot, v.sharedRel())

	ex.localHash[s.str()] = h

	return h
}

func (ex *Executor) onNode(n *Node, fetchRefs *DenseMap[STR, NodeRef]) {
	if ex.fetchRefs == nil {
		ex.fetchRefs = fetchRefs
	} else if ex.fetchRefs != fetchRefs {
		throwFmt("executor: fetchRefs changed mid-stream")
	}

	f := &NodeFuture{node: n, ref: n.Ref}

	ex.futs.set(uint32(n.Ref), f)
	ex.prepBatch = append(ex.prepBatch, f)

	if len(ex.prepBatch) >= ex.prepBatchCap {
		ex.flushPrepBatch()
	}
}

func (ex *Executor) flushPrepBatch() {
	if len(ex.prepBatch) == 0 {
		return
	}

	batch := ex.prepBatch

	ex.prepBatch = nil

	if ex.prepBatchCap < prepBatchMax {
		ex.prepBatchCap <<= 1
	}

	ex.events.post(func() {
		for _, f := range batch {
			ex.prepare(f)
		}
	})
}

func (ex *Executor) prepare(f *NodeFuture) {
	exc := try(func() {
		f.uid = ex.uidOf(f.node)
	})

	if exc != nil {
		f.err = exc

		f.once.Do(func() {})

		if !ex.keepGoing {
			fatalException(exc)
		}

		return
	}

	if ex.uidCached(f.uid) {
		f.once.Do(func() {})

		return
	}

	ex.pending.Add(1)
	ex.inFlight.Add(1)

	go ex.fire(f)
}

func (ex *Executor) fire(f *NodeFuture) {
	defer ex.inFlight.Done()

	try(func() {
		ex.visit(f.ref)
	}).catch(func(e *Exception) {
		if !ex.keepGoing {
			fatalException(e)
		}
	})
}

func (ex *Executor) uidOf(n *Node) UID {
	if n.PresetUID != nil {
		return *n.PresetUID
	}

	ex.canon.fetchRefs = ex.fetchRefs

	return ex.canon.calcUID(n)
}

func fatalException(e *Exception) {
	fatalOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "\x1b[31m%s\x1b[0m\n", e.Error())
		os.Exit(1)
	})

	select {}
}

func (ex *Executor) run(roots []NodeRef) {
	ex.flushPrepBatch()
	ex.events.sync()

	for _, r := range roots {
		if ex.keepGoing {
			_ = try(func() { ex.visit(r) })

			continue
		}

		ex.visit(r)
	}

	ex.inFlight.Wait()
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

		if f.err != nil && ex.keepGoing {
			ex.reportFailure(f.err)
		}
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

func nodeOutName(n *Node) string {
	if len(n.Outputs) > 0 {
		return n.Outputs[0].sharedString()
	}

	return fmt.Sprintf("ref=%d", n.Ref)
}

func (ex *Executor) reportFailure(err *Exception) {
	ex.failed.Add(1)

	msg := err.Error()

	ex.events.post(func() {
		if !ex.ninja {
			fmt.Fprint(os.Stderr, ansiESC+"[2K\r")
		}

		fmt.Fprintln(os.Stderr, color("red", msg))
	})
}

func (ex *Executor) execute(f *NodeFuture) {
	n := f.node

	defer ex.done.Add(1)

	if ex.keepGoing {
		for r := range n.buildDeps(ex.fetchRefs) {
			exc := try(func() {
				ex.visit(r)
			})

			if exc == nil {
				continue
			}

			throwFmt("%s broken by dep %s", nodeOutName(n), nodeOutName(ex.futs.get(uint32(r)).node))
		}
	} else {
		for r := range n.buildDeps(ex.fetchRefs) {
			ex.visit(r)
		}
	}

	cachePath := ex.uidPath(f.uid)

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
		outFirst = n.Outputs[0].sharedString()
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

		mounted := filepath.Join(bldMount, out.sharedRel())

		throw(os.MkdirAll(filepath.Dir(mounted), 0o755))
	}

	cmdFileCounter := 0

	for _, c := range n.Cmds {
		flatArgs := c.CmdArgs.flat()
		args := make([]string, len(flatArgs))

		for i, a := range flatArgs {
			args[i] = mountANY(a, srcMount, bldMount)
		}

		if n.KV.P == pkSB {
			args = append([]string{throw2(os.Executable()), "fetch", "sandbox", "--source-root", ex.srcRoot}, args[2:]...)
		} else {
			args = applyCmdPrefixes(args, ex.cmdPrefixes)
			args = packCommandFiles(args, bldMount, &cmdFileCounter)
		}

		dir := bldMount

		if c.Cwd != 0 {
			dir = mountANY(c.Cwd.any(), srcMount, bldMount)
		}

		env := os.Environ()

		for _, e := range n.Env {
			env = append(env, e.Name.sharedString()+"="+mountANY(e.Value, srcMount, bldMount))
		}

		for _, e := range c.Env {
			env = append(env, e.Name.sharedString()+"="+mountANY(e.Value, srcMount, bldMount))
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
			path := mountANY(c.Stdout.any(), srcMount, bldMount)

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

			rel := in.sharedRel()
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

		ex.storePath(filepath.Join(tmp, out.sharedRel()), out.sharedString(), meta)
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
				throw(os.Symlink(throw2(filepath.Abs(src)), target))
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

		if _, err := os.Lstat(path); err != nil {
			return
		}
	}
}

func (ex *Executor) startGarbageCollector() {
	go func() {
		for {
			cmd := exec.Command("rm", "-rf", ex.grbDir)

			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

			if cmd.Start() == nil && cmd.Wait() != nil {
				removeAllForce(ex.grbDir)
			}

			time.Sleep(time.Second)
		}
	}()
}

func removeAllForce(dir string) {
	if err := os.RemoveAll(dir); err == nil {
		return
	}

	_ = filepath.WalkDir(dir, func(path string, d iofs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o755)
		}

		return nil
	})

	_ = os.RemoveAll(dir)
}

func mountANY(a ANY, srcRoot, bldRoot string) string {
	if v := a.vfs(); v != 0 {
		root := srcRoot

		if v.isBuild() {
			root = bldRoot
		}

		rel := v.sharedRel()

		if rel == "" {
			return root
		}

		return root + "/" + rel
	}

	return mountString(a.str().sharedString(), srcRoot, bldRoot)
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
