package main

import (
	"strings"

	"github.com/zeebo/xxh3"
)

type OsFS struct {
	srcRoot       string
	rootSlash     string
	dirs          DenseMap[STR, DirView]
	dirNames      *BumpAllocator[uint32]
	dirEntries    *IntMap[bool]
	contentHashes []uint64
	readBuf       []byte
	direntBuf     []byte
	rootFD        int
	pathBuf       []byte
	listdirHits   uint64
	listdirMisses uint64
	existsHits    uint64
	existsMisses  uint64
}

var emptyDirNames = []uint32{}

func newFS(srcRoot string) FS {
	fs := &OsFS{
		srcRoot:    srcRoot,
		rootSlash:  srcRoot + "/",
		dirNames:   newBumpAllocator[uint32](1 << 12),
		dirEntries: newIntMap[bool](1 << 12),
	}

	fs.platformInit()

	return fs
}

func (fs *OsFS) readSourceRels() []string {
	out := make([]string, 0, len(fs.contentHashes))

	for s := 1; s < len(fs.contentHashes); s++ {
		if fs.contentHashes[s] == 0 {
			continue
		}

		if rel, ok := strings.CutPrefix(STR(s).String(), "$(S)/"); ok && rel != "" {
			out = append(out, rel)
		}
	}

	return out
}

func (fs *OsFS) recordContentHash(rel string, data []byte) {
	s := internPrefixed("$(S)/", cleanRel(rel))

	if int(s) >= len(fs.contentHashes) {
		n := len(fs.contentHashes) * 2

		if n <= int(s) {
			n = int(s) + 1
		}

		grown := make([]uint64, n)
		copy(grown, fs.contentHashes)
		fs.contentHashes = grown
	}

	fs.contentHashes[s] = xxh3.Hash(data)
}

func (fs *OsFS) contentHash(v VFS) uint64 {
	s := v.strID()

	if int(s) < len(fs.contentHashes) && fs.contentHashes[s] != 0 {
		return fs.contentHashes[s]
	}

	return fs.contentHashSlow(v)
}

func (fs *OsFS) contentHashSlow(v VFS) uint64 {
	rel := v.rel()

	if p, d := fs.existsRel(rel); p && d {
		return 0
	}

	fs.read(rel)

	return fs.contentHashes[v.strID()]
}

func (fs *OsFS) listdir(dir VFS) DirView {
	key := STR(dir.strID())

	if cached, ok := fs.dirs.get(key); ok {
		fs.listdirHits++

		return cached
	}

	fs.listdirMisses++

	v := fs.readDirViewRel(key, dir.rel())
	fs.dirs.put(key, v)

	return v
}

func (fs *OsFS) dirHas(v DirView, name string) (present bool, isDir bool) {
	id := interned(name)

	if id == 0 {
		return false, false
	}

	d := fs.dirEntries.get(splitMix64(uint32(v.dir), uint32(id)))

	if d == nil {
		return false, false
	}

	return true, *d
}

func (fs *OsFS) bumpExists(ok bool) {
	if ok {
		fs.existsHits++
	} else {
		fs.existsMisses++
	}
}

func (fs *OsFS) exists(prefix VFS, suffix string) (present bool, isDir bool) {
	if suffix == "" {
		return fs.listdir(prefix).listable(), true
	}

	prefixRel := prefix.rel()

	if !pathIsClean(suffix) {
		rel := normalisePath(joinRel(prefixRel, suffix))

		if rel == "" {
			return true, true
		}

		dir, name := splitDirName(rel)
		v := fs.listdir(dirKey(dir))

		if !v.listable() {
			fs.existsMisses++

			return false, false
		}

		ok, d := fs.dirHas(v, name)
		fs.bumpExists(ok)

		return ok, d
	}

	v := fs.listdir(prefix)

	if !v.listable() {
		fs.existsMisses++

		return false, false
	}

	first, more := firstComponent(suffix)

	if !more {
		ok, d := fs.dirHas(v, first)
		fs.bumpExists(ok)

		return ok, d
	}

	if ok, d := fs.dirHas(v, first); !ok || !d {
		fs.existsMisses++

		return false, false
	}

	dname, base := splitDirName(suffix)
	v = fs.listdir(dirKey(joinRel(prefixRel, dname)))

	if !v.listable() {
		fs.existsMisses++

		return false, false
	}

	ok, d := fs.dirHas(v, base)
	fs.bumpExists(ok)

	return ok, d
}

func (fs *OsFS) isFile(prefix VFS, suffix string) bool {
	p, d := fs.exists(prefix, suffix)

	return p && !d
}

func (fs *OsFS) isDir(prefix VFS, suffix string) bool {
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
	fs.readBuf = fs.readIntoRaw(rel, fs.readBuf)
	fs.recordContentHash(rel, fs.readBuf)

	return fs.readBuf
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

	prefix := rel

	if prefix != "" {
		prefix += "/"
	}

	for _, packed := range fs.listdirRel(rel).names {
		child := prefix + STR(packed>>1).string()

		if packed&1 != 0 {
			fs.walk(child, visit)

			continue
		}

		visit(child, false)
	}
}

func (fs *OsFS) perfStats() FsPerfStats {
	return FsPerfStats{
		listdirHits:   fs.listdirHits,
		listdirMisses: fs.listdirMisses,
		existsHits:    fs.existsHits,
		existsMisses:  fs.existsMisses,
		dirsCached:    fs.dirs.len(),
	}
}
