package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

var emptyDirNames = []uint32{}

const (
	readChunkSize      = 1 << 20
	sourceUnderHotMask = 1<<18 - 1
)

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

type OsFS struct {
	srcRoot        string
	rootSlash      string
	dirs           DenseMap[STR, DirView]
	dirNames       *BumpAllocator[uint32]
	dirEntries     *IntSet
	contentHashes  ContentHashes
	sourceUnder    *IntMap[STR]
	sourceUnderHot [sourceUnderHotMask + 1]sourceUnderHotEntry
	readResult     [2][]byte

	fsBackend
}

func newFS(srcRoot string) FS {
	fs := &OsFS{
		srcRoot:     srcRoot,
		rootSlash:   srcRoot + "/",
		dirNames:    newBumpAllocator[uint32](),
		dirEntries:  newIntSet(1 << 17),
		sourceUnder: newIntMap[STR](1 << 18),
	}

	fs.initBackend()

	return fs
}

func (fs *OsFS) listdir(dir STR) DirView {
	if cached, ok := fs.dirs.get(dir); ok {
		return cached
	}

	v := fs.readDirViewRel(dir, dir.string())

	fs.dirs.put(dir, v)

	return v
}

func (fs *OsFS) dirHas(v DirView, name string) (present bool, isDir bool) {
	id := interned(name)

	if id == 0 {
		return false, false
	}

	return fs.dirHasID(v, id)
}

func (fs *OsFS) dirHasID(v DirView, id STR) (present bool, isDir bool) {
	isDir, ok := fs.dirEntries.get(splitMix64(uint32(v.dir), uint32(id)))

	if !ok {
		return false, false
	}

	return true, isDir
}

func (fs *OsFS) exists(prefix STR, suffix string) (present bool, isDir bool) {
	return fs.existsClean(prefix, suffix, suffix != "" && pathIsClean(suffix))
}

func (fs *OsFS) existsClean(prefix STR, suffix string, clean bool) (present bool, isDir bool) {
	if suffix == "" {
		return fs.listdir(prefix).listable(), true
	}

	if !clean {
		var jb, nb [256]byte

		joined := joinRelInto(jb[:0], prefix.string(), suffix)
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

	v = fs.listdir(internJoined(prefix.string(), dname))

	if !v.listable() {
		return false, false
	}

	ok, d := fs.dirHas(v, base)

	return ok, d
}

func (fs *OsFS) isFile(prefix STR, suffix string) bool {
	p, d := fs.exists(prefix, suffix)

	return p && !d
}

func (fs *OsFS) isFileClean(prefix STR, suffix string) bool {
	p, d := fs.existsClean(prefix, suffix, true)

	return p && !d
}

func (fs *OsFS) isFilePathClean(rel string) bool {
	dir, name := splitDirName(rel)
	v := fs.listdir(internStr(dir))

	if !v.listable() {
		return false
	}

	present, isDir := fs.dirHas(v, name)

	return present && !isDir
}

func (fs *OsFS) resolveSourceUnder(prefix, target STR) STR {
	return fs.resolveSourceUnder0(prefix, target, false, false)
}

func (fs *OsFS) resolveSourceUnderClean(prefix, target STR, targetClean bool) STR {
	return fs.resolveSourceUnder0(prefix, target, targetClean, true)
}

func (fs *OsFS) resolveSourceUnder0(prefix, target STR, targetClean, cleanKnown bool) STR {
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
		clean = suffix != "" && pathIsClean(suffix)
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

	fs.sourceUnder.put(key, v)
	hot.key = pair
	hot.val = v

	return v
}

func (fs *OsFS) isDir(prefix STR, suffix string) bool {
	p, d := fs.exists(prefix, suffix)

	return p && d
}

func (fs *OsFS) existsRel(rel string) (present bool, isDir bool) {
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

func (fs *OsFS) listdirRel(rel string) DirView {
	return fs.listdir(dirKey(rel))
}

func (fs *OsFS) read(rel string) [][]byte {
	return fs.readPath(internStr(rel))
}

func (fs *OsFS) readPath(rel STR) [][]byte {
	chunks := fs.readFileRel(rel.string())

	fs.contentHashes.store(rel, contentHashChunks(chunks))

	return chunks
}

func (fs *OsFS) walk(rel string, visit func(rel string, isDir bool) bool) {
	rel = cleanRel(rel)

	present, isDir := fs.existsRel(rel)

	if !present {
		return
	}

	if !visit(rel, isDir) || !isDir {
		return
	}

	queue := []string{rel}

	for head := 0; head < len(queue); head++ {
		for _, packed := range fs.listdirRel(queue[head]).names {
			child := internJoined(queue[head], STR(packed>>1).string()).string()
			isDir := packed&1 != 0

			if visit(child, isDir) && isDir {
				queue = append(queue, child)
			}
		}
	}
}
