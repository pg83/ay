package main

import (
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

	// dirsSplit indexes each listed directory's child set under every
	// prefix/suffix split of its path, so the include scanner's search tier can
	// fetch the children of <addinclDir>/<targetDir> as dirsSplit[addinclDir]
	// [targetDir] — no candidate-path concat, hits AND misses. The inner set is
	// the SAME map stored in dirs (a shared alias, not a copy). Keyed by string,
	// not STR: the directory tree has orders of magnitude more paths than the
	// codegen registry, so interning every split fragment would bloat the global
	// intern table (and idSet sizing) far more than it saves.
	dirsSplit map[string]map[string]map[string]bool

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
		dirsSplit:  make(map[string]map[string]map[string]bool, 1024),
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
		fs.indexDirSplits(rel, nil)
		return nil
	}

	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Name()] = e.IsDir()
	}
	fs.dirs[rel] = out
	fs.indexDirSplits(rel, out)

	return out
}

// indexDirSplits records set (the child map of directory d, possibly nil for a
// nonexistent d — the negative entry keeps later misses concat-free) under
// every prefix/suffix split of d: (d,""), each internal (a,b), and ("",d). The
// fragment keys are substrings of d, so they share its backing — no copies.
func (fs *FS) indexDirSplits(d string, set map[string]bool) {
	fs.putDirSplit(d, "", set)
	for i := 0; i < len(d); i++ {
		if d[i] == '/' {
			fs.putDirSplit(d[:i], d[i+1:], set)
		}
	}
	if d != "" {
		fs.putDirSplit("", d, set)
	}
}

func (fs *FS) putDirSplit(prefix, suffix string, set map[string]bool) {
	inner := fs.dirsSplit[prefix]
	if inner == nil {
		inner = make(map[string]map[string]bool, 4)
		fs.dirsSplit[prefix] = inner
	}

	inner[suffix] = set
}

// childrenAt returns the child set of directory <prefix>/<suffix> without
// concatenating them when that directory has been listed (the common case); on
// a cold miss it lists it once (the only concat) and Listdir indexes the split
// so subsequent probes are concat-free. nil ⇒ the directory does not exist.
func (fs *FS) childrenAt(prefix, suffix string) map[string]bool {
	if inner := fs.dirsSplit[prefix]; inner != nil {
		if set, ok := inner[suffix]; ok {
			return set
		}
	}

	return fs.Listdir(joinRel(prefix, suffix))
}

// IsFileAt reports whether <prefix>/<suffix>/<name> is an existing regular file
// — the concat-free existence probe for the include search tier.
func (fs *FS) IsFileAt(prefix, suffix, name string) bool {
	isDir, ok := fs.childrenAt(prefix, suffix)[name]
	return ok && !isDir
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

// Read returns the raw bytes of <sourceRoot>/<rel>. Uncached.
func (fs *FS) Read(rel string) ([]byte, error) {
	return os.ReadFile(fs.rootSlash + cleanRel(rel))
}

// ReadAbs reads an absolute path. Paths under sourceRoot route through
// Read so cache invariants stay consistent for callers that mix both
// forms (yamake INCLUDE resolution).
func (fs *FS) ReadAbs(absPath string) ([]byte, error) {
	if rel, ok := fs.relForAbs(absPath); ok {
		return fs.Read(rel)
	}
	return os.ReadFile(absPath)
}

// ExistsAbs is the absolute-path counterpart of Exists.
func (fs *FS) ExistsAbs(absPath string) (present bool, isDir bool) {
	if rel, ok := fs.relForAbs(absPath); ok {
		return fs.Exists(rel)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return false, false
	}
	return true, info.IsDir()
}

func (fs *FS) relForAbs(absPath string) (string, bool) {
	if absPath == fs.sourceRoot {
		return "", true
	}
	if strings.HasPrefix(absPath, fs.rootSlash) {
		return absPath[len(fs.rootSlash):], true
	}
	return "", false
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
