package main

import "encoding/json"

var autoincludePathsFiles = []string{
	"build/conf/autoincludes.json",
	"build/internal/conf/autoincludes.json",
}

const lintersMakeIncName = "linters.make.inc"

type AutoincludeIndex struct {
	darts   *Darts
	linters []VFS
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

			linters = append(linters, source(r, "/", lintersMakeIncName))
		}
	}

	return &AutoincludeIndex{darts: NewDarts(keys), linters: linters}
}

func (a *AutoincludeIndex) lintersMakeIncFor(moduleDir string) (VFS, bool) {
	i, ok := a.darts.longestMatch(moduleDir, "/")

	if !ok {
		return 0, false
	}

	return a.linters[i], true
}
