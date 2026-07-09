package main

import (
	"os"
	"path/filepath"

	"github.com/zeebo/xxh3"
)

var emptyDirNames = []uint32{}

const mmapReadThreshold = 2 << 20

func hashSourceFile(srcRoot, rel string) uint64 {
	data, err := os.ReadFile(filepath.Join(srcRoot, cleanRel(rel)))

	if err != nil {
		return 0
	}

	return xxh3.Hash(data)
}

type OsFS struct {
	srcRoot       string
	rootSlash     string
	dirs          DenseMap[STR, DirView]
	dirNames      *BumpAllocator[uint32]
	dirEntries    *IntSet
	contentHashes PageVec[uint64]
	sourceUnder   *IntMap[STR]
	readBuf       []byte
	mmapCur       []byte
	direntBuf     []byte
	rootFD        int
	pathBuf       []byte
}

func newFS(srcRoot string) FS {
	fs := &OsFS{
		srcRoot:     srcRoot,
		rootSlash:   srcRoot + "/",
		readBuf:     make([]byte, 0, mmapReadThreshold),
		dirNames:    newBumpAllocator[uint32](1 << 12),
		dirEntries:  newIntSet(1 << 12),
		sourceUnder: newIntMap[STR](1 << 16),
	}

	fs.platformInit()

	return fs
}

func (fs *OsFS) recordContentHash(rel string, data []byte) {
	fs.contentHashes.set(uint32(internStr(cleanRel(rel))), xxh3.Hash(data))
}

func (fs *OsFS) contentHash(rel STR) uint64 {
	return fs.contentHashes.getSafe(rel.strID())
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

	isDir, ok := fs.dirEntries.get(splitMix64(uint32(v.dir), uint32(id)))

	if !ok {
		return false, false
	}

	return true, isDir
}

func (fs *OsFS) exists(prefix STR, suffix string) (present bool, isDir bool) {
	if suffix == "" {
		return fs.listdir(prefix).listable(), true
	}

	prefixRel := prefix.string()

	if !pathIsClean(suffix) {
		var jb, nb [256]byte

		joined := joinRelInto(jb[:0], prefixRel, suffix)
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

	v = fs.listdir(internJoined(prefixRel, dname))

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

func (fs *OsFS) resolveSourceUnder(prefix, target STR) STR {
	key := splitMix64(uint32(prefix), uint32(target))

	if p := fs.sourceUnder.get(key); p != nil {
		return *p
	}

	suffix := target.string()

	var v STR

	if fs.isFile(prefix, suffix) {
		switch {
		case suffix != "" && pathIsClean(suffix):
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

func (fs *OsFS) read(rel string) []byte {
	data := fs.readIntoRaw(rel, fs.readBuf)

	if fs.mmapCur == nil {
		fs.readBuf = data
	}

	fs.recordContentHash(rel, data)

	return data
}

func (fs *OsFS) readIntoRaw(rel string, buf []byte) []byte {
	return fs.readFileRel(cleanRel(rel), buf)
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

	for _, packed := range fs.listdirRel(rel).names {
		child := internJoined(rel, STR(packed>>1).string()).string()

		if packed&1 != 0 {
			fs.walk(child, visit)

			continue
		}

		visit(child, false)
	}
}
