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
type FS struct {
	sourceRoot string
	rootSlash  string
	dirs       map[string]map[string]bool

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
		dirs:       make(map[string]map[string]bool, 1024),
	}
}

// SourceRoot returns the configured absolute root path.
func (fs *FS) SourceRoot() string { return fs.sourceRoot }

// Listdir returns the basename→isDir map for the directory at rel.
// rel is SOURCE_ROOT-relative ("" addresses the root). Missing or
// non-directory rels return nil. Cached.
func (fs *FS) Listdir(rel string) map[string]bool {
	rel = cleanRel(rel)
	if cached, ok := fs.dirs[rel]; ok {
		fs.listdirHits++
		return cached
	}
	fs.listdirMisses++

	full := fs.rootSlash + rel
	if rel == "" {
		full = fs.sourceRoot
	}

	entries, err := os.ReadDir(full)
	if err != nil {
		fs.dirs[rel] = nil
		return nil
	}

	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Name()] = e.IsDir()
	}
	fs.dirs[rel] = out

	return out
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
	entries := fs.Listdir(dir)
	if entries == nil {
		fs.existsMisses++
		return false, false
	}

	isDir, ok := entries[name]
	if ok {
		fs.existsHits++
	} else {
		fs.existsMisses++
	}

	return ok, isDir
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

	// Sources are immutable for the duration of a Gen run, so Stat's size is
	// exact: read precisely that many bytes and skip the trailing zero-length
	// EOF-probe read (one fewer syscall per file across ~44k files). Falls back
	// to grow-and-loop-to-EOF only if Stat fails.
	if fi, statErr := f.Stat(); statErr == nil {
		sz := int(fi.Size())
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

	for name, childIsDir := range fs.Listdir(rel) {
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
	// Fast path: most rels reaching the FS probe path are already clean.
	// path.Clean is an O(n) scan that also sets up a lazybuf (heap alloc) on
	// any rewrite; a single conservative scan returns the input untouched for
	// the common case and only falls through to path.Clean when needed.
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

// pathIsClean reports whether p is already a clean SOURCE_ROOT-relative path:
// no leading/trailing '/', and no empty / "." / ".." segment. Conservative —
// any uncertain case returns false and routes through path.Clean, so a false
// negative only costs a slow path, never correctness.
func pathIsClean(p string) bool {
	if p[0] == '/' || p[len(p)-1] == '/' {
		return false
	}

	// Leading segment: "." or ".." (no preceding '/').
	if p[0] == '.' {
		if len(p) == 1 || p[1] == '/' || (p[1] == '.' && (len(p) == 2 || p[2] == '/')) {
			return false
		}
	}

	// Interior segments: a '/' followed by '/', "./", "..", or a trailing ".".
	for i := 0; i < len(p); i++ {
		if p[i] != '/' {
			continue
		}
		// p[len-1] != '/' (checked above), so i+1 is in range.
		if p[i+1] == '/' {
			return false
		}
		if p[i+1] == '.' {
			if i+2 == len(p) || p[i+2] == '/' {
				return false // "/." segment
			}
			if p[i+2] == '.' && (i+3 == len(p) || p[i+3] == '/') {
				return false // "/.." segment
			}
		}
	}

	return true
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
