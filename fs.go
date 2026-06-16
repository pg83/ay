package main

// FS is the source-tree filesystem facade. Production code drives an osFS
// (rooted at a real directory and cached lazily); tests drive a memFS built
// inline (testfs_test.go) so the suite does no disk I/O for fixture trees.
type FS interface {
	// Listdir lists the directory named by its Source-rooted VFS ("$(S)/<dir>").
	listdir(dir VFS) DirView
	dirHas(v DirView, name string) (present bool, isDir bool)
	// Exists/IsFile/IsDir take a directory VFS prefix and a relative
	// suffix, so callers thread an already-interned prefix instead of building and
	// re-interning a concatenated path. See osFS.Exists for the gating algorithm.
	exists(prefix VFS, suffix string) (present bool, isDir bool)
	isFile(prefix VFS, suffix string) bool
	isDir(prefix VFS, suffix string) bool
	// Read returns the file content in a buffer reused across calls: the result is
	// valid only until the next Read on this FS. Callers that retain content past
	// another Read must copy (e.g. string(data), or ReadAbs for the parser).
	read(rel string) []byte
	// walk visits each entry; for a directory, visit returns whether to descend
	// into it (the return is ignored for files).
	walk(rel string, visit func(rel string, isDir bool) bool)
	// ContentHash returns the xxh3 of source VFS v's file content, recorded when the
	// FS last read that file. It is keyed by v.strID() (the full "$(S)/..." path STR),
	// so the uid serializer passes the VFS directly — no per-input re-intern of the
	// bare rel. It faults if no content was ever read for v — the hash must exist by
	// the time a node's uid is computed.
	contentHash(v VFS) uint64
	perfStats() FsPerfStats
}

// dirKey returns the directory cache key: the Source VFS of the directory
// ("$(S)/<cleandir>"). The hot resolve path already holds the addincl/includer
// dir as a VFS, so it keys Listdir for free; string callers intern here,
// bounded by the directory universe (~6k on sg5). cleanRel keeps the key
// canonical so the two routes agree.

func dirKey(dir string) VFS {
	return source(cleanRel(dir))
}

// DirView is a directory's window into the FS name store: dir is the half of
// the splitMix64 membership key (the directory's own STR), names the packed
// entries (name STR<<1 | isDir bit), a sub-slice of the FS's bump-arena store.
// A zero view (nil names) is "not listable"; emptyDirNames backs an existing
// empty directory — the same nil/empty distinction the map cache had.
type DirView struct {
	dir   STR
	names []uint32
}

func (v DirView) listable() bool {
	return v.names != nil
}

type FsPerfStats struct {
	listdirHits   uint64
	listdirMisses uint64
	existsHits    uint64
	existsMisses  uint64
	dirsCached    int
}
