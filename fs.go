package main

import (
	"io"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/zeebo/xxh3"
)

// FS is the source-tree filesystem facade. Production code drives an osFS
// (rooted at a real directory and cached lazily); tests drive a memFS built
// inline (testfs_test.go) so the suite does no disk I/O for fixture trees.
type FS interface {
	SourceRoot() string
	// Listdir lists the directory named by its Source-rooted VFS ("$(S)/<dir>").
	Listdir(dir VFS) map[string]bool
	// Exists/IsFile/IsDir take a directory VFS prefix and a relative
	// suffix, so callers thread an already-interned prefix instead of building and
	// re-interning a concatenated path. See osFS.Exists for the gating algorithm.
	Exists(prefix VFS, suffix string) (present bool, isDir bool)
	IsFile(prefix VFS, suffix string) bool
	IsDir(prefix VFS, suffix string) bool
	// Read returns the file content in a buffer reused across calls: the result is
	// valid only until the next Read on this FS. Callers that retain content past
	// another Read must copy (e.g. string(data), or ReadAbs for the parser).
	Read(rel string) []byte
	ReadAbs(absPath string) []byte
	ExistsAbs(absPath string) (present bool, isDir bool)
	Walk(rel string, visit func(rel string, isDir bool))
	// ContentHash returns the xxh3 of source VFS v's file content, recorded when the
	// FS last read that file. It is keyed by v.strID() (the full "$(S)/..." path STR),
	// so the uid serializer passes the VFS directly — no per-input re-intern of the
	// bare rel. It faults if no content was ever read for v — the hash must exist by
	// the time a node's uid is computed.
	ContentHash(v VFS) uint64
	perfStats() fsPerfStats
}

// dirKey returns the directory cache key: the Source VFS of the directory
// ("$(S)/<cleandir>"). The hot resolve path already holds the addincl/includer
// dir as a VFS, so it keys Listdir for free; string callers intern here,
// bounded by the directory universe (~6k on sg5). cleanRel keeps the key
// canonical so the two routes agree.

func dirKey(dir string) VFS { return Source(cleanRel(dir)) }

// srcRootVFS is the source root ("$(S)/"), i.e. what dirKey("") returns —
// hoisted so the many IsFile(srcRootVFS, rel) call sites do not re-intern the
// constant "$(S)/" on every call (~118k/run on sg5).
var srcRootVFS = Source("")

type osFS struct {
	sourceRoot string
	rootSlash  string
	// dirs is keyed by the directory's STR (dir.strID()) rather than its VFS: a
	// source dir is always Source-rooted (VFS == STR<<1), so the STR is lossless
	// and halves DenseMap's idx array versus indexing the 2x-wider VFS space.
	dirs DenseMap[STR, map[string]bool]

	// contentHashes is the xxh3 of each read file's content, indexed directly by the
	// STR of its full "$(S)/..." path — i.e. the source VFS's own strID, so the uid
	// serializer indexes by v.strID() without re-interning the bare rel (STR ids are
	// dense, so a plain growing array beats a hash map). Slot 0 means "not recorded"
	// — xxh3 is effectively never 0. Both writes
	// (FS reads during gen) and reads (uid computation in StreamingEmitter.Emit)
	// happen on the single gen goroutine — the executor goroutine is spawned only
	// after a node's uid is computed — so no lock.
	contentHashes []uint64
	readBuf       []byte // reused buffer returned by Read (gen goroutine only)

	listdirHits   uint64
	listdirMisses uint64
	existsHits    uint64
	existsMisses  uint64
}

func NewFS(sourceRoot string) FS {
	return &osFS{
		sourceRoot: sourceRoot,
		rootSlash:  sourceRoot + "/",
	}
}

// recordContentHash stores xxh3(data) at the file's full-path STR (the source VFS
// strID), growing the array as ids advance.
func (fs *osFS) recordContentHash(rel string, data []byte) {
	s := internString("$(S)/" + cleanRel(rel))

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

func (fs *osFS) ContentHash(v VFS) uint64 {
	s := v.strID()

	if int(s) < len(fs.contentHashes) && fs.contentHashes[s] != 0 {
		return fs.contentHashes[s]
	}

	// Lazily read inputs gen never scanned — many $(S) inputs (data files,
	// tablegen .td, python stdlib, tzdata, …) are listed on nodes but their content
	// is never needed during graph construction. Read on first uid use (reusing one
	// buffer) so the hash is recorded; a genuinely missing file faults here.
	rel := v.Rel()

	if p, d := fs.existsRel(rel); p && d {
		return 0 // directory inputs (e.g. a test data dir) have no content hash
	}

	fs.Read(rel) // side effect: records the content hash into contentHashes[s]
	return fs.contentHashes[s]
}
func (fs *osFS) SourceRoot() string { return fs.sourceRoot }

// Listdir returns the entries of the directory whose Source-rooted path is dir
// ("$(S)/<cleandir>"). Keyed by VFS so the hot caller passes the addincl
// VFS directly with no string hashing; expected to hit the cache.
func (fs *osFS) Listdir(dir VFS) map[string]bool {
	key := STR(dir.strID())

	if cached, ok := fs.dirs.Get(key); ok {
		fs.listdirHits++
		return cached
	}

	fs.listdirMisses++

	rel := dir.Rel()
	full := fs.rootSlash + rel

	if rel == "" {
		full = fs.sourceRoot
	}

	entries, err := os.ReadDir(full)

	if err != nil {
		fs.dirs.Put(key, nil)
		return nil
	}

	out := make(map[string]bool, len(entries))

	for _, e := range entries {
		out[e.Name()] = e.IsDir()
	}

	fs.dirs.Put(key, out)

	return out
}

func (fs *osFS) bumpExists(ok bool) {
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
func (fs *osFS) Exists(prefix VFS, suffix string) (present bool, isDir bool) {
	if suffix == "" {
		return fs.Listdir(prefix) != nil, true
	}

	prefixRel := prefix.Rel()

	if !pathIsClean(suffix) {
		rel := normalisePath(joinRel(prefixRel, suffix))

		if rel == "" {
			return true, true
		}

		dir, name := splitDirName(rel)
		entries := fs.Listdir(dirKey(dir))

		if entries == nil {
			fs.existsMisses++
			return false, false
		}

		d, ok := entries[name]
		fs.bumpExists(ok)

		return ok, d
	}

	entries := fs.Listdir(prefix)

	if entries == nil {
		fs.existsMisses++
		return false, false
	}

	first, more := firstComponent(suffix)

	if !more {
		d, ok := entries[first]
		fs.bumpExists(ok)

		return ok, d
	}

	if d, ok := entries[first]; !ok || !d {
		fs.existsMisses++
		return false, false
	}

	dname, base := splitDirName(suffix)
	entries = fs.Listdir(dirKey(joinRel(prefixRel, dname)))

	if entries == nil {
		fs.existsMisses++
		return false, false
	}

	d, ok := entries[base]
	fs.bumpExists(ok)

	return ok, d
}

func (fs *osFS) IsFile(prefix VFS, suffix string) bool {
	p, d := fs.Exists(prefix, suffix)
	return p && !d
}

func (fs *osFS) IsDir(prefix VFS, suffix string) bool {
	p, d := fs.Exists(prefix, suffix)
	return p && d
}

// existsRel / listdirRel are the string-rel internal helpers for cold callers
// that hold a whole path (Walk, ExistsAbs, ContentHash): they split and intern
// the directory directly, no gating.
func (fs *osFS) existsRel(rel string) (present bool, isDir bool) {
	rel = cleanRel(rel)

	if rel == "" {
		return true, true
	}

	dir, name := splitDirName(rel)
	entries := fs.Listdir(dirKey(dir))

	if entries == nil {
		return false, false
	}

	d, ok := entries[name]

	return ok, d
}

func (fs *osFS) listdirRel(rel string) map[string]bool {
	return fs.Listdir(dirKey(rel))
}

func (fs *osFS) Read(rel string) []byte {
	fs.readBuf = fs.readIntoRaw(rel, fs.readBuf)
	fs.recordContentHash(rel, fs.readBuf)
	return fs.readBuf
}

func (fs *osFS) readIntoRaw(rel string, buf []byte) []byte {
	f := Throw2(os.Open(fs.rootSlash + cleanRel(rel)))
	defer f.Close()

	buf = buf[:0]

	// Fstat into a stack Stat_t instead of f.Stat() — the latter heap-allocates an
	// *os.fileStat (FileInfo) per read (~10MB churn over a run). Linux-only path.
	var st syscall.Stat_t

	if statErr := syscall.Fstat(int(f.Fd()), &st); statErr == nil {
		sz := int(st.Size)

		if sz > cap(buf) {
			buf = make([]byte, 0, sz)
		}

		for len(buf) < sz {
			n, err := f.Read(buf[len(buf):sz])
			buf = buf[:len(buf)+n]

			if err != nil {
				if err == io.EOF {
					return buf
				}

				Throw(err)
			}
		}

		return buf
	}

	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)]
		}

		n, err := f.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]

		if err != nil {
			if err == io.EOF {
				return buf
			}

			Throw(err)
		}
	}
}

// ReadAbs reads a file to be parsed. The yamake lexer holds its src lazily, and a
// nested INCLUDE triggers another Read mid-parse — which would overwrite the reused
// Read buffer the outer lexer still points at. So ReadAbs returns an owned copy that
// survives those nested reads.
func (fs *osFS) ReadAbs(absPath string) []byte {
	return append([]byte(nil), fs.Read(fs.relForAbs(absPath))...)
}

func (fs *osFS) ExistsAbs(absPath string) (present bool, isDir bool) {
	return fs.existsRel(fs.relForAbs(absPath))
}

func (fs *osFS) relForAbs(absPath string) string {
	if absPath == fs.sourceRoot {
		return ""
	}

	if strings.HasPrefix(absPath, fs.rootSlash) {
		return absPath[len(fs.rootSlash):]
	}

	ThrowFmt("relForAbs: %q is outside source root %q", absPath, fs.sourceRoot)

	return ""
}

func (fs *osFS) Walk(rel string, visit func(rel string, isDir bool)) {
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

	for name, childIsDir := range fs.listdirRel(rel) {
		child := prefix + name

		if childIsDir {
			fs.Walk(child, visit)
			continue
		}

		visit(child, false)
	}
}

type fsPerfStats struct {
	listdirHits   uint64
	listdirMisses uint64
	existsHits    uint64
	existsMisses  uint64
	dirsCached    int
}

func (fs *osFS) perfStats() fsPerfStats {
	return fsPerfStats{
		listdirHits:   fs.listdirHits,
		listdirMisses: fs.listdirMisses,
		existsHits:    fs.existsHits,
		existsMisses:  fs.existsMisses,
		dirsCached:    fs.dirs.Len(),
	}
}

func cleanRel(rel string) string {
	if rel == "" || rel == "." {
		return ""
	}

	if pathIsClean(rel) {
		return rel
	}

	rel = path.Clean(rel)

	if rel == "." || rel == "/" {
		return ""
	}

	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimSuffix(rel, "/")
	return rel
}

func pathIsClean(p string) bool {
	if p[0] == '/' || p[len(p)-1] == '/' {
		return false
	}

	if p[0] == '.' {
		if len(p) == 1 || p[1] == '/' || (p[1] == '.' && (len(p) == 2 || p[2] == '/')) {
			return false
		}
	}

	for i := 0; i < len(p); i++ {
		if p[i] != '/' {
			continue
		}

		if p[i+1] == '/' {
			return false
		}

		if p[i+1] == '.' {
			if i+2 == len(p) || p[i+2] == '/' {
				return false
			}

			if p[i+2] == '.' && (i+3 == len(p) || p[i+3] == '/') {
				return false
			}
		}
	}

	return true
}

func splitDirName(rel string) (string, string) {
	i := strings.LastIndexByte(rel, '/')

	if i < 0 {
		return "", rel
	}

	return rel[:i], rel[i+1:]
}

func firstComponent(p string) (first string, more bool) {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i], true
	}

	return p, false
}

func joinRel(prefix, suffix string) string {
	switch {
	case prefix == "":
		return suffix
	case suffix == "":
		return prefix
	default:
		return prefix + "/" + suffix
	}
}
