package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
)

var (
	emptyDirNames                  = []uint32{}
	recentNames, recentMacroLocals recentSTRs
)

const (
	readChunkSize      = 1 << 20
	sourceUnderHotMask = 1<<18 - 1
)

type recentSTRs struct {
	text [16]string
	id   [16]STR
	next int
}

type sourceUnderHotEntry struct {
	key uint64
	val STR
}

func hashSourceFile(srcRoot, rel string) uint64 {
	data, err := os.ReadFile(filepath.Join(srcRoot, cleanRel(rel)))

	if err != nil {
		return 0
	}

	return contentHashBytes(data)
}

type ContentHashes struct {
	vec PageVec[uint64]
}

func (h *ContentHashes) store(rel STR, hash uint64) {
	p, off := pageOffset(rel.strID())

	if page := h.vec.pages[p].Load(); page != nil {
		atomic.StoreUint64(unsafeAt(*page, uint64(off)), hash)

		return
	}

	h.vec.set(rel.strID(), hash)
}

func (h *ContentHashes) load(rel STR) uint64 {
	p, off := pageOffset(rel.strID())
	page := h.vec.pages[p].Load()

	if page == nil {
		return 0
	}

	return atomic.LoadUint64(unsafeAt(*page, uint64(off)))
}

type LiveTree struct {
	srcRoot        string
	rootSlash      string
	dirs           Vec[DirView]
	dirNames       *BumpAllocator[uint32]
	dirEntries     *IntSet
	contentHashes  ContentHashes
	knownFiles     BitSet
	sourceUnder    *IntMap[STR]
	sourceUnderHot [sourceUnderHotMask + 1]sourceUnderHotEntry
	emptyRead      [1][]byte
	fill           DirFill
	bundleKeys     []STR
	bundleVals     []map[string]ResourceBundleEntry
	walkQueue      []STR
	walkBuf        []byte

	backend        Backend
	parsed         DenseMap[STR, ParsedSource]
	extras         Vec[ParsedIncludeSet]
	directives     *BumpAllocator[IncludeDirective]
	parses         int
	testReadErrors map[string]error
	confRootOnce   sync.Once
	confRootDir    string

	onReadError func(rel string, err syscall.Errno)

	unreadableDirs map[string]syscall.Errno
}

func newLiveTree(srcRoot string) *LiveTree {
	fs := &LiveTree{
		srcRoot:        srcRoot,
		rootSlash:      srcRoot + "/",
		dirNames:       newBumpAllocator[uint32](),
		dirEntries:     newIntSet(1 << 17),
		testReadErrors: map[string]error{},
		directives:     newBumpAllocator[IncludeDirective](),
		sourceUnder:    newIntMap[STR](1 << 18),
	}

	if info, err := os.Stat(srcRoot); err == nil && !info.IsDir() {
		fs.backend = newIndexedBackend(srcRoot)
	} else {
		fs.backend = newDirBackend(srcRoot)
	}

	return fs
}

type DirFill struct {
	dir   STR
	block []uint32
	k     int
}

func (fs *LiveTree) dirBegin(limit int) {
	fs.fill.block = fs.dirNames.alloc(limit)
}

func (fs *LiveTree) dirEntry(name []byte, isDir bool) {
	id := internBytes(name)
	packed := uint32(id) << 1

	if isDir {
		packed |= 1
	}

	fs.fill.block[fs.fill.k] = packed
	fs.fill.k++
	fs.dirEntries.put(splitMix64(uint32(fs.fill.dir), uint32(id)), isDir)
}

func (fs *LiveTree) readDirView(dir STR, rel string) DirView {
	fs.fill = DirFill{dir: dir}

	if errno := fs.backend.readDir(rel, fs); errno != 0 {
		if errno == syscall.EACCES || errno == syscall.EPERM {
			if fs.unreadableDirs == nil {
				fs.unreadableDirs = map[string]syscall.Errno{}
			}

			fs.unreadableDirs[rel] = errno
		}

		return DirView{}
	}

	k := fs.fill.k

	if k == 0 {
		return DirView{dir: dir, names: emptyDirNames}
	}

	fs.dirNames.commit(k)

	names := fs.fill.block[:k]

	slices.SortFunc(names, func(a, b uint32) int {
		return strings.Compare(STR(a>>1).string(), STR(b>>1).string())
	})

	return DirView{dir: dir, names: names}
}

func (fs *LiveTree) dirHas(v DirView, name string) (present bool, isDir bool) {
	return fs.dirHasInterned(v, name)
}

func (fs *LiveTree) sourcePathExists(rel string) bool {
	return fs.backend.exists(rel)
}

func (fs *LiveTree) confRoot() string {
	if root := fs.backend.localRoot(); root != "" {
		return root
	}

	fs.confRootOnce.Do(func() {
		root := throw2(os.MkdirTemp("", "ay-gate-conf-"))

		atExit(func() { os.RemoveAll(root) })

		for _, rel := range []string{ymakeConfScript, "build/internal"} {
			fs.walk(rel, func(name string, isDir bool) bool {
				out := filepath.Join(root, name)

				if isDir {
					throw(os.MkdirAll(out, 0o755))

					return true
				}

				throw(os.MkdirAll(filepath.Dir(out), 0o755))
				throw(os.WriteFile(out, concatChunks(fs.read(name)), 0o644))

				return false
			})
		}

		fs.confRootDir = root
	})

	return fs.confRootDir
}

func (fs *LiveTree) listdir(dir STR) DirView {
	if row := dirRows.rowOf(dir); int(row) < len(fs.dirs.s) {
		if cached := fs.dirs.s[row]; cached.dir != 0 {
			return cached
		}
	}

	return fs.listdirSlow(dir)
}

func (fs *LiveTree) listdirSlow(dir STR) DirView {
	row := dirRows.rowFor(dir)
	v := fs.readDirView(dir, dir.string())

	v.dir = dir

	fs.dirs.ensureLen(int(row) + 1)
	fs.dirs.s[row] = v

	return v
}

func (c *recentSTRs) intern(name string) STR {
	if id := c.interned(name); id != 0 {
		return id
	}

	id := internStr(name)

	c.text[c.next], c.id[c.next] = id.string(), id
	c.next = (c.next + 1) & 15

	return id
}

func internedRecent(name string) STR {
	return recentNames.interned(name)
}

func (c *recentSTRs) interned(name string) STR {
	for i := range c.text {
		if c.id[i] != 0 && c.text[i] == name {
			return c.id[i]
		}
	}

	id := interned(name)

	if id != 0 {
		c.text[c.next], c.id[c.next] = id.string(), id
		c.next = (c.next + 1) & 15
	}

	return id
}

func (fs *LiveTree) dirHasInterned(v DirView, name string) (present bool, isDir bool) {
	id := internedRecent(name)

	if id == 0 {
		return false, false
	}

	return fs.dirHasID(v, id)
}

func (fs *LiveTree) dirHasID(v DirView, id STR) (present bool, isDir bool) {
	isDir, ok := fs.dirEntries.get(splitMix64(uint32(v.dir), uint32(id)))

	if !ok {
		return false, false
	}

	return true, isDir
}

func (fs *LiveTree) existsID(prefix, rel STR) (present bool, isDir bool) {
	if rel.isEmpty() {
		return fs.listdir(prefix).listable(), true
	}

	if !rel.cleanPath() {
		return fs.existsClean(prefix, rel.string(), false)
	}

	if s := rel.string(); strings.IndexByte(s, '/') >= 0 {
		return fs.existsClean(prefix, s, true)
	}

	view := fs.listdir(prefix)

	if !view.listable() {
		return false, false
	}

	return fs.dirHasID(view, rel)
}

func (fs *LiveTree) exists(prefix STR, suffix string) (present bool, isDir bool) {
	return fs.existsClean(prefix, suffix, suffix != "" && pathIsClean(suffix))
}

func (fs *LiveTree) existsClean(prefix STR, suffix string, clean bool) (present bool, isDir bool) {
	if suffix == "" {
		return fs.listdir(prefix).listable(), true
	}

	if !clean {
		var jb, nb [256]byte

		joined := joinRelInto(jb[:0], prefix.string(), suffix)

		if pathEscapesRoot(bytesString(joined)) {
			return false, false
		}

		normB, ok := normaliseAppend(nb[:0], bytesString(joined))
		rel := bytesString(normB)

		if !ok {
			rel = normalisePathSlow(string(joined))
		}

		if rel == "" {
			return true, true
		}

		dir, name := splitDirName(rel)
		v := fs.listdir(dirKey(dir))

		if !v.listable() {
			return false, false
		}

		ok, d := fs.dirHas(v, name)

		return ok, d
	}

	v := fs.listdir(prefix)

	if !v.listable() {
		return false, false
	}

	first, more := firstComponent(suffix)

	if !more {
		ok, d := fs.dirHas(v, first)

		return ok, d
	}

	if ok, d := fs.dirHas(v, first); !ok || !d {
		return false, false
	}

	dname, base := splitDirName(suffix)

	v = fs.listdir(internJoinedDir(prefix, dname))

	if !v.listable() {
		return false, false
	}

	ok, d := fs.dirHas(v, base)

	return ok, d
}

func (fs *LiveTree) isFile(prefix STR, suffix string) bool {
	p, d := fs.exists(prefix, suffix)

	return p && !d
}

func (fs *LiveTree) isFileClean(prefix STR, suffix string) bool {
	p, d := fs.existsClean(prefix, suffix, true)

	return p && !d
}

func (fs *LiveTree) isFileID(dir, name STR) bool {
	v := fs.listdir(dir)

	if !v.listable() {
		return false
	}

	present, isDir := fs.dirHasID(v, name)

	return present && !isDir
}

func (fs *LiveTree) isFilePathClean(rel string) bool {
	dir, name := splitDirName(rel)
	v := fs.listdir(internDirText(dir))

	if !v.listable() {
		return false
	}

	present, isDir := fs.dirHas(v, name)

	return present && !isDir
}

func (fs *LiveTree) isFileVFS(v VFS) bool {
	id := v.rel()

	if fs.knownFiles.has(uint32(id)) {
		return true
	}

	if !fs.isFilePathClean(id.string()) {
		return false
	}

	fs.knownFiles.add(uint32(id))

	return true
}

func (fs *LiveTree) isFileVFSAny(v VFS) bool {
	if v.rel().cleanPath() {
		return fs.isFileVFS(v)
	}

	return fs.isFile(srcRootRel, v.relString())
}

func (fs *LiveTree) noteFile(v VFS) {
	fs.knownFiles.add(uint32(v.rel()))
}

func (fs *LiveTree) isReadFile(rel STR) bool {
	return fs.contentHashes.load(rel) != 0
}

func (fs *LiveTree) contentHash(rel STR) uint64 {
	return fs.contentHashes.load(rel)
}

func (fs *LiveTree) resourceBundle(rel STR) (map[string]ResourceBundleEntry, bool) {
	for i, key := range fs.bundleKeys {
		if key == rel {
			return fs.bundleVals[i], true
		}
	}

	return nil, false
}

func (fs *LiveTree) storeResourceBundle(rel STR, bundle map[string]ResourceBundleEntry) {
	fs.bundleKeys = append(fs.bundleKeys, rel)
	fs.bundleVals = append(fs.bundleVals, bundle)
}

func (fs *LiveTree) resolveSourceUnder(prefix, target STR) STR {
	return fs.resolveSourceUnder0(prefix, target, false, false)
}

func (fs *LiveTree) resolveSourceUnderClean(prefix, target STR, targetClean bool) STR {
	return fs.resolveSourceUnder0(prefix, target, targetClean, true)
}

func (fs *LiveTree) resolveSourceUnder0(prefix, target STR, targetClean, cleanKnown bool) STR {
	p, t := uint32(prefix), uint32(target)
	pair := uint64(p)<<32 | uint64(t)
	hot := &fs.sourceUnderHot[chaosSourceUnderSlot((p*0x9e3779b1^t)&sourceUnderHotMask)]

	if hot.key == pair {
		return hot.val
	}

	key := mix64(pair)

	if p := fs.sourceUnder.get(key); p != nil {
		hot.key = pair
		hot.val = *p

		return *p
	}

	suffix := target.string()
	clean := targetClean

	if !cleanKnown {
		clean = target.cleanPath()
	}

	var v STR
	var present, isDir bool

	if clean && suffix != "" && strings.IndexByte(suffix, '/') < 0 {
		view := fs.listdir(prefix)

		if view.listable() {
			present, isDir = fs.dirHasID(view, target)
		}
	} else {
		present, isDir = fs.existsClean(prefix, suffix, clean)
	}

	if present && !isDir {
		switch {
		case clean:
			if prefix == srcRootRel {
				v = target
			} else {
				v = internJoined(prefix.string(), suffix)
			}
		default:
			var jb, nb [256]byte

			joined := joinRelInto(jb[:0], prefix.string(), suffix)
			normB, ok := normaliseAppend(nb[:0], bytesString(joined))

			if ok {
				v = internBytes(normB)
			} else {
				v = internStr(normalisePathSlow(string(joined)))
			}
		}
	}

	if v != 0 {
		fs.knownFiles.add(uint32(v))
	}

	fs.sourceUnder.put(key, v)
	hot.key = pair
	hot.val = v

	return v
}

func (fs *LiveTree) isDir(prefix STR, suffix string) bool {
	p, d := fs.exists(prefix, suffix)

	return p && d
}

func (fs *LiveTree) existsRel(rel string) (present bool, isDir bool) {
	rel = cleanRel(rel)

	if rel == "" {
		return true, true
	}

	dir, name := splitDirName(rel)
	v := fs.listdir(dirKey(dir))

	if !v.listable() {
		return false, false
	}

	ok, d := fs.dirHas(v, name)

	return ok, d
}

func (fs *LiveTree) listdirRel(rel string) DirView {
	return fs.listdir(dirKey(rel))
}

func (fs *LiveTree) read(rel string) [][]byte {
	return fs.readPath(internStr(rel))
}

func (fs *LiveTree) readPath(rel STR) [][]byte {
	chunks, errno := fs.backend.readFile(rel.string())

	switch {
	case errno == syscall.EISDIR:
		throwFmt("read %s: %v", rel.string(), errno)
	case errno != 0 && errno != syscall.ENOENT && fs.onReadError != nil:
		fs.onReadError(rel.string(), errno)

		fs.emptyRead[0] = nil
		chunks = fs.emptyRead[:1]
	case errno != 0:
		throwFmt("open %s: %v", rel.string(), errno)
	}

	fs.contentHashes.store(rel, contentHashChunks(chunks))

	return chunks
}

func (fs *LiveTree) walkDirID(root STR, visit func(rel string, dir STR, isDir bool) bool) {
	if !fs.listdir(root).listable() || !visit(root.string(), root, true) {
		return
	}

	queue, buf := append(fs.walkQueue[:0], root), fs.walkBuf[:0]

	fs.walkQueue, fs.walkBuf = nil, nil

	for head := 0; head < len(queue); head++ {
		buf = buf[:0]

		if dir := queue[head].string(); dir != "" {
			buf = append(append(buf, dir...), '/')
		}

		n := len(buf)

		for _, packed := range fs.listdir(queue[head]).names {
			buf = append(buf[:n], STR(packed>>1).string()...)

			if packed&1 == 0 {
				visit(bytesString(buf), 0, false)

				continue
			}

			child := internBytes(buf)

			if visit(child.string(), child, true) {
				queue = append(queue, child)
			}
		}
	}

	fs.walkQueue, fs.walkBuf = queue[:0], buf[:0]
}

func (fs *LiveTree) walk(rel string, visit func(rel string, isDir bool) bool) {
	rel = cleanRel(rel)

	present, isDir := fs.existsRel(rel)

	if !present {
		return
	}

	if !visit(rel, isDir) || !isDir {
		return
	}

	queue, buf := append(fs.walkQueue[:0], dirKey(rel)), fs.walkBuf[:0]

	fs.walkQueue, fs.walkBuf = nil, nil

	for head := 0; head < len(queue); head++ {
		buf = buf[:0]

		if dir := queue[head].string(); dir != "" {
			buf = append(append(buf, dir...), '/')
		}

		n := len(buf)

		for _, packed := range fs.listdir(queue[head]).names {
			buf = append(buf[:n], STR(packed>>1).string()...)

			if packed&1 == 0 {
				visit(bytesString(buf), false)

				continue
			}

			child := internBytes(buf)

			if visit(child.string(), true) {
				queue = append(queue, child)
			}
		}
	}

	fs.walkQueue, fs.walkBuf = queue[:0], buf[:0]
}

func (fs *LiveTree) root() string {
	return fs.srcRoot
}

func (fs *LiveTree) setReadErrorHook(hook func(rel string, errno syscall.Errno)) {
	fs.onReadError = hook
}

func (fs *LiveTree) unreadableDir(dir string) syscall.Errno {
	if len(fs.unreadableDirs) == 0 {
		return 0
	}

	return fs.unreadableDirs[dir]
}

func (fs *LiveTree) peekParsed(rel STR) (ParsedSource, bool) {
	return fs.parsed.get(rel)
}

func (fs *LiveTree) parse(vfsPath VFS, rel string, parser IncludeDirectiveParser, bucket ParsedIncludeBucket) ParsedSource {
	key := vfsPath.rel()

	if parser == nil || !fs.isFileVFS(vfsPath) {
		empty := ParsedSource{parser: includeParserKind(parser), bucket: bucket}

		fs.parsed.put(key, empty)

		return empty
	}

	fs.parses++

	data := fs.readPath(key)

	if first := data[0]; len(first) >= 3 && first[0] == 0xEF && first[1] == 0xBB && first[2] == 0xBF {
		data[0] = first[3:]
	}

	out := parser.parse(rel, data, fs.directives)
	parsed := ParsedSource{local: out[parsedIncludesLocal], parser: includeParserKind(parser), bucket: bucket}

	if len(parsed.local) > 0 {
		parsed.dir = internDirText(pathDir(rel))
	}

	for _, entries := range out[1:] {
		if entries != nil {
			fs.extras.pushBack(out)
			parsed.extra = uint32(fs.extras.len())

			if parsed.dir == 0 {
				parsed.dir = internDirText(pathDir(rel))
			}

			break
		}
	}

	fs.parsed.put(key, parsed)

	return parsed
}

func (fs *LiveTree) includes(p ParsedSource) ParsedIncludeSet {
	if p.extra != 0 {
		return fs.extras.s[p.extra-1]
	}

	return ParsedIncludeSet{parsedIncludesLocal: p.local}
}

func (fs *LiveTree) walkable(p ParsedSource) []IncludeDirective {
	switch {
	case p.bucket == parsedIncludesLocal:
		return p.local
	case p.extra == 0:
		return nil
	}

	return fs.extras.s[p.extra-1][p.bucket]
}
