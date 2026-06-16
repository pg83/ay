package main

import "encoding/json"

// AUTOINCLUDE_PATHS (build/conf/settings.conf:126; internal settings.conf:26
// appends the internal list) names the JSON arrays of directory prefixes under
// which ymake auto-includes the nearest enclosing linters.make.inc. The internal
// file is absent in the open-source contour.
var autoincludePathsFiles = []string{
	"build/conf/autoincludes.json",
	"build/internal/conf/autoincludes.json",
}

// lintersMakeIncName mirrors devtools/ymake/autoincludes_conf.cpp's LINTERS_MAKE_INC.
const lintersMakeIncName = "linters.make.inc"

// AutoincludeIndex resolves the linters.make.inc that ymake auto-includes for a
// module — the nearest enclosing AUTOINCLUDE_PATHS root (longest-prefix match,
// component boundary). The roots are held in a byte double-array trie keyed by
// "<root>/", mirroring ymake's TCompactTrie<char> + FindLongestPrefix(Dir+"/")
// (autoincludes_conf.cpp:30, makefile_loader.cpp:226).
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
// autoinclude root for moduleDir (ymake's FindLongestPrefix), or (0,false) when
// no root encloses it. The trailing "/" — passed as a separate part so no string
// is concatenated — gives the component-boundary match ("arc/" ∌ "arcfoo/…").
func (a *AutoincludeIndex) lintersMakeIncFor(moduleDir string) (VFS, bool) {
	i, ok := a.darts.longestMatch(moduleDir, "/")

	if !ok {
		return 0, false
	}

	return a.linters[i], true
}
