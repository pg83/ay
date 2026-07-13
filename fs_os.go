package main

import (
	"os"
	"path/filepath"
)

var emptyDirNames = []uint32{}

// readChunkSize is part of the source content-hash format: content hashes are
// XORs of hashes of these stable chunks. Changing it invalidates those hashes.
const readChunkSize = 256 << 10

const sourceUnderHotMask = 1<<16 - 1
const dirFDCacheSize = 512

type sourceUnderHotEntry struct {
	key uint64
	val STR
}

type dirFDCacheEntry struct {
	rel string
	fd  int
}

func hashSourceFile(srcRoot, rel string) uint64 {
	data, err := os.ReadFile(filepath.Join(srcRoot, cleanRel(rel)))

	if err != nil {
		return 0
	}

	return contentHashBytes(data)
}

type OsFS struct {
	srcRoot        string
	rootSlash      string
	dirs           DenseMap[STR, DirView]
	dirNames       *BumpAllocator[uint32]
	dirEntries     *IntSet
	contentHashes  PageVec[uint64]
	sourceUnder    *IntMap[STR]
	sourceUnderHot [sourceUnderHotMask + 1]sourceUnderHotEntry
	readBuf        []byte
	readResult     [2][]byte
	direntBuf      []byte
	mmapCur        []byte
	rootFD         int
	dirFD          int
	dirFDRel       string
	dirFDs         map[string]int
	dirFDRing      [dirFDCacheSize]dirFDCacheEntry
	dirFDNext      int
	pathBuf        []byte
}

func newFS(srcRoot string) FS {
	fs := &OsFS{
		srcRoot:     srcRoot,
		rootSlash:   srcRoot + "/",
		readBuf:     make([]byte, readChunkSize),
		dirNames:    newBumpAllocator[uint32](),
		dirEntries:  newIntSet(1 << 18),
		sourceUnder: newIntMap[STR](1 << 19),
		dirFD:       -1,
		dirFDs:      make(map[string]int, dirFDCacheSize),
	}

	fs.platformInit()

	return fs
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

func (fs *OsFS) resolveSourceUnder(prefix, target STR) STR {
	p, t := uint32(prefix), uint32(target)
	pair := uint64(p)<<32 | uint64(t)
	hot := &fs.sourceUnderHot[(p*0x9e3779b1^t)&sourceUnderHotMask]

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
	clean := suffix != "" && pathIsClean(suffix)

	var v STR

	if present, isDir := fs.existsClean(prefix, suffix, clean); present && !isDir {
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
	rel = cleanRel(rel)
	chunks := fs.readFileRel(rel)
	fs.contentHashes.set(uint32(internStr(rel)), contentHashChunks(chunks))

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

	for _, packed := range fs.listdirRel(rel).names {
		child := internJoined(rel, STR(packed>>1).string()).string()

		if packed&1 != 0 {
			fs.walk(child, visit)

			continue
		}

		visit(child, false)
	}
}
