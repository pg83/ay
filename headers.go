package main

import "path"

func applyHeaders(fs FS, modulePath string, args []ANY, d *ModuleData) {
	files := headerFiles(fs, modulePath, args)
	stmt := d.nextDeclSeq()

	for _, file := range files {
		d.srcs = append(d.srcs, SrcMeta{Source: source(file).any(), Prio: stmtPrioSrcs, Seq: d.nextDeclSeq(), Stmt: stmt})
	}
}

func headerFiles(fs FS, modulePath string, args []ANY) []string {
	dirs, lists := goMacroLists(args, "EXCLUDE")

	resolve := func(arg ANY) VFS {
		if rooted, ok := moduleRootedVFS(modulePath, arg.string()); ok {
			return rooted
		}

		return sourceJoinClean(modulePath, arg.string())
	}

	var excludes []string

	for _, exclude := range lists["EXCLUDE"] {
		excludes = append(excludes, resolve(exclude).relString())
	}

	var files []string

	for _, dir := range dirs {
		base := resolve(dir)

		if base.isBuild() {
			continue
		}

		files = append(files, ymakeGlob(fs, path.Join(base.relString(), "*.(h|H|hh|hpp|hxx|ipp)"), excludes...)...)
	}

	return files
}
