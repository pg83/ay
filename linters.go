package main

import "strings"

const lintersMakeIncName = "linters.make.inc"

type AutoincludeIndex struct {
	darts   *Darts
	linters []VFS
	paths   string
}

func loadAutoincludeIndex(fs Tree, env Environment, onWarn func(Warn)) *AutoincludeIndex {
	var keys []string
	var linters []VFS

	report := func(msg string) {
		onWarn(Warn{Kind: WarnMisconfiguration, Where: "At top level", Error: msg})
	}

	files := strings.Fields(env.expand(env.string(envAUTOINCLUDE_PATHS)))

	for _, f := range files {
		if !fs.isFile(srcRootRel, f) {
			report(confFileMissing("autoinclude", fs.root()+"/"+f))

			continue
		}

		root, err := readConfJSON(concatChunks(fs.read(f)))

		switch {
		case err != "":
			report("Autoinclude config " + fs.root() + "/" + f + " is invalid. Json syntax error: " + err)
		case root.kind != confJSONArray:
			report("Autoinclude conf root section should be an array.")
		default:
			for _, r := range root.items {
				if r.kind == confJSONString {
					keys = append(keys, r.str+"/")
					linters = append(linters, source(r.str, "/", lintersMakeIncName))
				}
			}
		}
	}

	return &AutoincludeIndex{darts: newDarts(keys), linters: linters, paths: strings.Join(files, ", ")}
}

func (a *AutoincludeIndex) lintersMakeIncFor(moduleDir string) (VFS, bool) {
	i, ok := a.darts.longestMatch(moduleDir, "/")

	if !ok {
		return 0, false
	}

	return a.linters[i], true
}
