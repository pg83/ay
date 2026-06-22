package main

import "encoding/json"

// JSON arrays of directory prefixes under which the nearest enclosing
// linters.make.inc is auto-included.
var autoincludePathsFiles = []string{
	"build/conf/autoincludes.json",
	"build/internal/conf/autoincludes.json",
}

const lintersMakeIncName = "linters.make.inc"

// AutoincludeIndex resolves the auto-included linters.make.inc for a module:
// the nearest enclosing AUTOINCLUDE_PATHS root, by longest-prefix match on a
// trie keyed by "<root>/".
type AutoincludeIndex struct {
	darts   *Darts
	linters []VFS // parallel to the trie keys: each root's linters.make.inc
}

func loadAutoincludeIndex(fs FS) *AutoincludeIndex {
	var keys []string
	var linters []VFS

	for _, f := range autoincludePathsFiles {
		if !fs.isFile(srcRootVFS, f) {
			continue
		}

		var roots []string

		if err := json.Unmarshal(fs.read(f), &roots); err != nil {
			throwFmt("autoinclude: parse %s: %v", f, err)
		}

		for _, r := range roots {
			keys = append(keys, r+"/")
			linters = append(linters, source(r+"/"+lintersMakeIncName))
		}
	}

	return &AutoincludeIndex{darts: NewDarts(keys), linters: linters}
}

// lintersMakeIncFor returns the linters.make.inc of the nearest enclosing
// autoinclude root for moduleDir, or (0,false) when none encloses it. The
// trailing "/" as a separate part enforces a component-boundary match.
func (a *AutoincludeIndex) lintersMakeIncFor(moduleDir string) (VFS, bool) {
	i, ok := a.darts.longestMatch(moduleDir, "/")

	if !ok {
		return 0, false
	}

	return a.linters[i], true
}
