package main

// FS is the source-tree filesystem facade. Production drives an osFS (cached
// lazily); tests drive a memFS so the suite does no disk I/O.
type FS interface {
	// Listdir lists the directory named by its Source-rooted VFS ("$(S)/<dir>").
	listdir(dir VFS) DirView
	dirHas(v DirView, name string) (present bool, isDir bool)
	// Exists/IsFile/IsDir take a directory VFS prefix and a relative suffix. See
	// osFS.Exists for the gating.
	exists(prefix VFS, suffix string) (present bool, isDir bool)
	isFile(prefix VFS, suffix string) bool
	isDir(prefix VFS, suffix string) bool
	// Read returns the file content in a buffer reused across calls: valid only
	// until the next Read. Callers retaining content must copy.
	read(rel string) []byte
	// walk visits each entry; for a directory, visit returns whether to descend.
	walk(rel string, visit func(rel string, isDir bool) bool)
	// ContentHash returns the xxh3 of source VFS v's content, keyed by v.strID()
	// (the full "$(S)/..." path STR). Faults if v was never read.
	contentHash(v VFS) uint64
	perfStats() FsPerfStats
}

// dirKey returns the directory cache key: the Source VFS ("$(S)/<cleandir>"). The
// hot resolve path already holds the dir as a VFS; string callers intern here.

func dirKey(dir string) VFS {
	return source(cleanRel(dir))
}

// DirView is a directory's window into the FS name store: dir is half the
// splitMix64 membership key (the directory's STR), names the packed entries (name
// STR<<1 | isDir bit). A zero view (nil names) is "not listable"; emptyDirNames
// backs an empty directory.
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
