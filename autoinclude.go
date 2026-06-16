package main

import (
	"encoding/json"
	"strings"
)

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

// loadAutoincludeIndex builds the autoinclude root index: source-VFS of each root
// directory -> source-VFS of that root's linters.make.inc. This mirrors ymake's
// AutoincludePathsTrie (autoincludes_conf.cpp:30), which maps "<root>/" to
// "<root>/linters.make.inc" for every json entry unconditionally — existence is
// checked at lookup time, not load.
func loadAutoincludeIndex(fs FS) *IntValueMap[VFS] {
	idx := newIntValueMap[VFS](512)

	for _, f := range autoincludePathsFiles {
		if !fs.isFile(srcRootVFS, f) {
			continue
		}

		var roots []string

		if err := json.Unmarshal(fs.read(f), &roots); err != nil {
			throwFmt("autoinclude: parse %s: %v", f, err)
		}

		for _, r := range roots {
			idx.put(uint64(source(r)), source(r+"/"+lintersMakeIncName))
		}
	}

	return idx
}

// lintersMakeIncFor returns the linters.make.inc that ymake auto-includes for a
// module at moduleDir, mirroring AutoincludePathsTrie.FindLongestPrefix
// (makefile_loader.cpp:226): the nearest (longest-prefix) autoinclude root
// enclosing moduleDir. The ancestor walk is allocation-free — internedPrefixed is
// a lookup-only intern probe (returns 0 for a non-root prefix) and the substrings
// share moduleDir's backing array. Returns (0,false) when no root encloses it.
func lintersMakeIncFor(idx *IntValueMap[VFS], moduleDir string) (VFS, bool) {
	for d := moduleDir; ; {
		if st := internedPrefixed("$(S)/", d); st != 0 {
			if v := idx.get(uint64(st.vfs())); v != nil {
				return *v, true
			}
		}

		i := strings.LastIndexByte(d, '/')

		if i < 0 {
			return 0, false
		}

		d = d[:i]
	}
}
