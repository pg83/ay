package main

import (
	"io"
	"os"
	"path"
	"strings"
)

// FS — read-only abstraction over the SOURCE_ROOT-rooted source tree.
//
// All paths are SOURCE_ROOT-relative forward-slash strings ("" addresses
// the root). Directory listings and per-entry presence are cached for
// the lifetime of the FS; file contents are not cached (callers layer
// their own caches — e.g. includeParserManager caches parsed include
// directives).
//
// Exists routes through Listdir(dirname): on miss the directory is read
// once and every subsequent lookup against the same dir is O(1). Cache
// never invalidates — sources are immutable for the duration of a Gen
// run.
// dirChildren maps an interned basename id to whether that child is a
// directory. The id space is the global string-intern table (vfs.go), so a
// child lookup keyed by an already-interned name — e.g. an include target's
// STR — is a mapaccess_fast32 hit with no string hashing.
type dirChildren map[STR]bool

type FS struct {
	sourceRoot string
	rootSlash  string
	// dirs maps an interned CLEAN dir-rel id to its children. A present key
	// with a nil value is a cached negative: the dir is absent or not a
	// directory. Keyed by STR so the hot existence-probe path (search-tier
	// resolution) is integer-keyed once its dir id is in hand.
	dirs map[STR]dirChildren

	listdirHits   uint64
	listdirMisses uint64
	existsHits    uint64
	existsMisses  uint64
}

// NewFS constructs an FS rooted at sourceRoot. sourceRoot must be a
// non-empty absolute path.
func NewFS(sourceRoot string) *FS {
	return &FS{
		sourceRoot: sourceRoot,
		rootSlash:  sourceRoot + "/",
		dirs:       make(map[STR]dirChildren, 1024),
	}
}

// SourceRoot returns the configured absolute root path.
func (fs *FS) SourceRoot() string { return fs.sourceRoot }

// listdirSTR returns the children of the directory whose interned CLEAN rel is
// dirSTR, reading and interning the entries on first use. A nil result is a
// cached "absent / not a directory". The fast path is integer-keyed: a caller
// holding a pre-interned dir id (a source search-path prefix) pays no string
// hashing here.
func (fs *FS) listdirSTR(dirSTR STR) dirChildren {
	if cached, ok := fs.dirs[dirSTR]; ok {
		fs.listdirHits++
		return cached
	}
	fs.listdirMisses++

	rel := dirSTR.String()
	full := fs.rootSlash + rel
	if rel == "" {
		full = fs.sourceRoot
	}

	entries, err := os.ReadDir(full)
	if err != nil {
		fs.dirs[dirSTR] = nil
		return nil
	}

	out := make(dirChildren, len(entries))
	for _, e := range entries {
		out[internString(e.Name())] = e.IsDir()
	}
	fs.dirs[dirSTR] = out

	return out
}

// Listdir returns the basename→isDir map for the directory at rel (string entry
// point — interns the cleaned rel and delegates to listdirSTR). rel is
// SOURCE_ROOT-relative ("" addresses the root). Missing or non-directory rels
// return nil. Cached.
func (fs *FS) Listdir(rel string) dirChildren {
	return fs.listdirSTR(internString(cleanRel(rel)))
}

// existsSTR reports (present, isDir) for basename nameSTR inside the directory
// dirSTR — both pre-interned, so it is a pure integer lookup once the dir is
// cached.
func (fs *FS) existsSTR(dirSTR, nameSTR STR) (present bool, isDir bool) {
	children := fs.listdirSTR(dirSTR)
	if children == nil {
		fs.existsMisses++
		return false, false
	}

	isDir, ok := children[nameSTR]
	if ok {
		fs.existsHits++
	} else {
		fs.existsMisses++
	}

	return ok, isDir
}

// Exists reports (present, isDir) for rel. Smart: routes through
// Listdir(dirname(rel)) so neighbouring lookups share one disk call.
// Empty rel addresses the source root and returns (true, true).
func (fs *FS) Exists(rel string) (present bool, isDir bool) {
	rel = cleanRel(rel)
	if rel == "" {
		return true, true
	}

	dir, name := splitDirName(rel)

	return fs.existsSTR(internString(dir), internString(name))
}

// IsFile is the common-case shorthand for `present && !isDir`.
func (fs *FS) IsFile(rel string) bool {
	p, d := fs.Exists(rel)
	return p && !d
}

// IsDir is the common-case shorthand for `present && isDir`.
func (fs *FS) IsDir(rel string) bool {
	p, d := fs.Exists(rel)
	return p && d
}

// Read returns the raw bytes of <sourceRoot>/<rel>. Uncached. Throws on any
// read error (a missing file included): callers that legitimately tolerate an
// absent file gate on Exists/IsFile first, which separates "optional file
// absent" (a query) from "file present but unreadable" (an exception).
func (fs *FS) Read(rel string) []byte {
	return Throw2(os.ReadFile(fs.rootSlash + cleanRel(rel)))
}

// ReadInto reads <sourceRoot>/<rel> into buf, reusing and growing it as needed,
// and returns the bytes (which alias buf — valid only until buf is reused). For
// the include scanner's parse path, which reads each source once, consumes it
// immediately, and retains nothing: one caller-owned buffer then serves every
// read with no per-file allocation. Throws on any read error (see Read).
func (fs *FS) ReadInto(rel string, buf []byte) []byte {
	f := Throw2(os.Open(fs.rootSlash + cleanRel(rel)))
	defer f.Close()

	buf = buf[:0]
	if fi, statErr := f.Stat(); statErr == nil {
		if sz := int(fi.Size()); sz > cap(buf) {
			buf = make([]byte, 0, sz)
		}
	}

	for {
		if len(buf) == cap(buf) {
			buf = append(buf, 0)[:len(buf)] // grow capacity, keep length
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

// ReadAbs reads an absolute path under sourceRoot, routing through Read so
// cache invariants stay consistent for callers that mix both forms (yamake
// INCLUDE resolution). Throws via relForAbs if the path is outside the root.
func (fs *FS) ReadAbs(absPath string) []byte {
	return fs.Read(fs.relForAbs(absPath))
}

// ExistsAbs is the absolute-path counterpart of Exists; Throws via relForAbs
// if absPath is outside the source root.
func (fs *FS) ExistsAbs(absPath string) (present bool, isDir bool) {
	return fs.Exists(fs.relForAbs(absPath))
}

// relForAbs maps an absolute path to its SOURCE_ROOT-relative form. Every path
// the generator touches lives under the root, so a path outside it is a
// programming error, not a fallback case — Throw rather than silently reading
// off-tree.
func (fs *FS) relForAbs(absPath string) string {
	if absPath == fs.sourceRoot {
		return ""
	}
	if strings.HasPrefix(absPath, fs.rootSlash) {
		return absPath[len(fs.rootSlash):]
	}

	ThrowFmt("relForAbs: %q is outside source root %q", absPath, fs.sourceRoot)

	return "" // unreachable
}

// Walk traverses the subtree rooted at rel in DFS order, invoking
// visit(relPath, isDir) for every entry (including rel itself when
// present). Children of a directory are visited in the OS-returned
// order — callers that need a stable order must sort the collected
// output themselves. Built on Listdir so the traversal shares the FS
// directory cache.
func (fs *FS) Walk(rel string, visit func(rel string, isDir bool)) {
	rel = cleanRel(rel)

	present, isDir := fs.Exists(rel)
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

	for nameSTR, childIsDir := range fs.Listdir(rel) {
		child := prefix + nameSTR.String()
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

func (fs *FS) perfStats() fsPerfStats {
	return fsPerfStats{
		listdirHits:   fs.listdirHits,
		listdirMisses: fs.listdirMisses,
		existsHits:    fs.existsHits,
		existsMisses:  fs.existsMisses,
		dirsCached:    len(fs.dirs),
	}
}

// cleanRel normalises a SOURCE_ROOT-relative path: forward-slash,
// no leading slash, no trailing slash, "." → "".
func cleanRel(rel string) string {
	if rel == "" || rel == "." {
		return ""
	}
	rel = path.Clean(rel)
	if rel == "." || rel == "/" {
		return ""
	}
	rel = strings.TrimPrefix(rel, "/")
	rel = strings.TrimSuffix(rel, "/")
	return rel
}

// splitDirName splits a clean rel into (dir, name); dir is "" for
// top-level entries.
func splitDirName(rel string) (string, string) {
	i := strings.LastIndexByte(rel, '/')
	if i < 0 {
		return "", rel
	}
	return rel[:i], rel[i+1:]
}

// firstComponent returns p's leading path component (up to the first '/') and
// whether more components follow. A substring of p — no allocation.
func firstComponent(p string) (first string, more bool) {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i], true
	}
	return p, false
}

// joinRel joins a prefix and suffix rel, either of which may be "".
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
