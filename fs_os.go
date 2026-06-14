package main

import (
	"github.com/zeebo/xxh3"
)

// OsFS is the production FS: rooted at a real directory, cached lazily, with
// the per-platform syscall fast paths in fs_linux.go / fs_other.go.
type OsFS struct {
	srcRoot   string
	rootSlash string
	// dirs is keyed by the directory's STR (dir.strID()) rather than its VFS: a
	// source dir is always Source-rooted (VFS == STR<<1), so the STR is lossless
	// and halves DenseMap's idx array versus indexing the 2x-wider VFS space.
	dirs DenseMap[STR, DirView]

	// dirNames is the packed name store every DirView windows (bump arena —
	// address-stable blocks, filled exactly once per listed directory).
	// dirEntries is the membership/isDir index over ALL directories, keyed by
	// splitMix64(dirSTR, nameSTR) — a bijection over the packed id pair, so a
	// probe is exact. Names intern once globally (repeated basenames share one
	// table entry instead of one map-key string per directory).
	dirNames   *BumpAllocator[uint32]
	dirEntries *IntMap[bool]

	// contentHashes is the xxh3 of each read file's content, indexed directly by the
	// STR of its full "$(S)/..." path — i.e. the source VFS's own strID, so the uid
	// serializer indexes by v.strID() without re-interning the bare rel (STR ids are
	// dense, so a plain growing array beats a hash map). Slot 0 means "not recorded"
	// — xxh3 is effectively never 0. Both writes
	// (FS reads during gen) and reads (uid computation in StreamingEmitter.Emit)
	// happen on the single gen goroutine — the executor goroutine is spawned only
	// after a node's uid is computed — so no lock.
	contentHashes []uint64
	readBuf       []byte

	// direntBuf is the reused getdents64 block for listdir misses.
	direntBuf []byte

	// rootFD pins the source root directory (linux): every read/listdir opens
	// via openat(rootFD, rel) — no rootSlash+rel concat, no per-open path
	// bytes. pathBuf is the reused NUL-terminated rel scratch for those calls.
	rootFD  int
	pathBuf []byte // reused buffer returned by Read (gen goroutine only)

	listdirHits   uint64
	listdirMisses uint64
	existsHits    uint64
	existsMisses  uint64
}

// emptyDirNames is the shared store of every listable-but-empty directory.
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

// recordContentHash stores xxh3(data) at the file's full-path STR (the source VFS
// strID), growing the array as ids advance.
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

// ContentHash's hot path is small enough to inline into the uid writer's
// monomorphic instantiation (see canonWriter); the lazy read lives in
// contentHashSlow so it does not blow the inlining budget.
func (fs *OsFS) contentHash(v VFS) uint64 {
	s := v.strID()

	if int(s) < len(fs.contentHashes) && fs.contentHashes[s] != 0 {
		return fs.contentHashes[s]
	}

	return fs.contentHashSlow(v)
}

// contentHashSlow lazily reads inputs gen never scanned — many $(S) inputs
// (data files, tablegen .td, python stdlib, tzdata, …) are listed on nodes but
// their content is never needed during graph construction. Read on first uid
// use (reusing one buffer) so the hash is recorded; a genuinely missing file
// faults here.
func (fs *OsFS) contentHashSlow(v VFS) uint64 {
	rel := v.rel()

	if p, d := fs.existsRel(rel); p && d {
		return 0 // directory inputs (e.g. a test data dir) have no content hash
	}

	fs.read(rel) // side effect: records the content hash into contentHashes[s]

	return fs.contentHashes[v.strID()]
}

// Listdir returns the entries of the directory whose Source-rooted path is dir
// ("$(S)/<cleandir>"). Keyed by VFS so the hot caller passes the addincl
// VFS directly with no string hashing; expected to hit the cache.
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

// dirHas probes one (dir, name) membership: an un-interned name cannot be a
// directory entry (every listed name interns at fill), and the splitMix64 key
// is a bijection over the id pair.
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

// Exists reports whether prefix/suffix exists (and whether it is a directory).
// prefix is a directory VFS; suffix is relative to it. For a clean
// suffix it gates on the first component being a directory under prefix before
// listing (and interning) the deeper directory — so dead candidate paths never
// grow the intern table. A suffix carrying ../././// is normalised jointly with
// prefix (the boundary-crossing case) and looked up directly.
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

// existsRel / listdirRel are the string-rel internal helpers for cold callers
// that hold a whole path (Walk, ExistsAbs, ContentHash): they split and intern
// the directory directly, no gating.
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

// readIntoRaw reads rel through the per-platform fast path (fs_linux.go /
// fs_other.go) — on linux an openat from the pinned source-root fd, with no
// rootSlash+rel concat and no per-open path conversion.
func (fs *OsFS) readIntoRaw(rel string, buf []byte) []byte {
	return fs.readFileRel(cleanRel(rel), buf)
}

func (fs *OsFS) walk(rel string, visit func(rel string, isDir bool)) {
	rel = cleanRel(rel)

	present, isDir := fs.existsRel(rel)

	if !present {
		return
	}

	visit(rel, isDir)

	if !isDir {
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
