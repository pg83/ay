package main

import (
	"path"
	"strings"
)

const (
	prewarmRecurse = 1 << iota
	prewarmContent
	prewarmRoot
)

const (
	prewarmRootMissing = 1 + iota
	prewarmRootFound
)

type GenCaches struct {
	parsers     *IncludeParserManager
	parsedFiles map[string]*MakeFile
}

type PrewarmDir struct {
	dir  string
	mode uint8
}

type GenPrewarm struct {
	fs        FS
	caches    *GenCaches
	dirs      []PrewarmDir
	files     []VFS
	seenDirs  map[string]uint8
	seenFiles BitSet
	rooted    TwoBitSet
	onPICRoot func()
}

func newGenCaches(fs FS) *GenCaches {
	return &GenCaches{
		parsers:     newIncludeParserManagerFS(fs, newSharedParseCache()),
		parsedFiles: map[string]*MakeFile{},
	}
}

func (c *GenCaches) prewarm(fs FS, targets []string, onPICRoot func()) {
	w := &GenPrewarm{
		fs:        fs,
		caches:    c,
		seenDirs:  map[string]uint8{},
		onPICRoot: onPICRoot,
	}

	for _, t := range targets {
		w.pushDir(t, prewarmRecurse|prewarmContent|prewarmRoot)
	}

	for more := true; more; {
		try(func() {
			more = w.step()
		})
	}
}

func (w *GenPrewarm) step() bool {
	if n := len(w.files); n > 0 {
		f := w.files[n-1]

		w.files = w.files[:n-1]
		w.file(f)

		return true
	}

	if len(w.dirs) > 0 {
		d := w.dirs[0]

		w.dirs = w.dirs[1:]
		w.dir(d)

		return true
	}

	return false
}

func prewarmPath(p string) bool {
	return p != "" && p[0] != '/' && p[0] != '.' && !strings.Contains(p, "$") && !strings.Contains(p, "..")
}

func (w *GenPrewarm) pushDir(dir string, mode uint8) {
	if !prewarmPath(dir) {
		return
	}

	dir = cleanRel(dir)

	seen := w.seenDirs[dir]

	if mode&^seen == 0 {
		return
	}

	w.seenDirs[dir] = seen | mode
	w.dirs = append(w.dirs, PrewarmDir{dir: dir, mode: mode &^ seen})
}

func (w *GenPrewarm) pushFile(v VFS) {
	if w.seenFiles.has(uint32(v)) {
		return
	}

	w.seenFiles.add(uint32(v))
	w.files = append(w.files, v)
}

func (w *GenPrewarm) makeFile(dir string) *MakeFile {
	rel := path.Join(dir, "ya.make")

	if mf, ok := w.caches.parsedFiles[rel]; ok {
		return mf
	}

	if !w.fs.isFilePathClean(rel) {
		return nil
	}

	mf, err := parseFileClean(w.fs, rel)

	if err != nil {
		return nil
	}

	w.caches.parsedFiles[rel] = mf

	return mf
}

func (w *GenPrewarm) dir(d PrewarmDir) {
	mf := w.makeFile(d.dir)

	if mf == nil {
		return
	}

	var moduleName TOK
	var finalTarget bool

	for _, st := range mf.Stmts {
		switch v := st.(type) {
		case *ModuleStmt:
			moduleName = v.Name
			finalTarget = v.Schema || isFinalTargetModuleType(v.Name)
		case *UnknownStmt:
			if d.mode&prewarmRecurse == 0 || v.Name != tokRecurse && v.Name != tokRecurseRootRelative {
				continue
			}

			for _, a := range v.Args {
				child := a.string()

				if v.Name == tokRecurse {
					child = d.dir + "/" + child
				}

				w.pushDir(child, prewarmRecurse)
			}
		}
	}

	if d.mode&prewarmRoot != 0 && picTransition(moduleName) && w.onPICRoot != nil {
		w.onPICRoot()
		w.onPICRoot = nil
	}

	if d.mode&prewarmContent == 0 && moduleName != 0 && finalTarget &&
		w.seenDirs[d.dir]&prewarmContent == 0 {
		w.seenDirs[d.dir] |= prewarmContent
		d.mode |= prewarmContent
	}

	if d.mode&prewarmContent != 0 {
		w.stmts(d.dir, mf.Stmts)
	}
}

func (w *GenPrewarm) stmts(dir string, stmts []Stmt) {
	for _, st := range stmts {
		switch v := st.(type) {
		case *PeerdirStmt:
			for _, p := range v.Paths {
				w.pushDir(p.string(), prewarmContent)
			}
		case *SrcsStmt:
			for _, s := range v.Sources {
				if src := s.string(); prewarmPath(src) {
					w.pushFile(sourceJoinClean(dir, src))
				}
			}
		case *AddInclStmt:
			for _, a := range v.AllPaths {
				if inc := a.string(); prewarmPath(inc) {
					w.caches.parsers.indexAddincl(sourceClean(inc))
				}
			}
		case *IfStmt:
			w.stmts(dir, v.Then)
			w.stmts(dir, v.Else)
		}
	}
}

func (w *GenPrewarm) file(v VFS) {
	pm := w.caches.parsers
	rel := v.relString()

	if _, ok := pm.registry.registeredParserFor(rel).(CIncludeDirectiveParser); !ok || !w.fs.isFilePathClean(rel) {
		return
	}

	dir := internStr(path.Dir(rel))

	for _, d := range pm.sourceParsedWalkable(v, nil) {
		target := d.target.string()

		if d.kind == includeQuoted && prewarmPath(target) && w.fs.isFile(dir, target) {
			w.pushFile(sourceJoinClean(dir.string(), target))

			continue
		}

		w.probeRoot(d.target)
	}
}

func (w *GenPrewarm) probeRoot(target ANY) {
	if w.rooted.get(uint32(target)) != 0 {
		return
	}

	t := target.string()

	if !prewarmPath(t) || !w.fs.isFile(srcRootRel, t) {
		w.rooted.set(uint32(target), prewarmRootMissing)

		return
	}

	w.rooted.set(uint32(target), prewarmRootFound)
	w.pushFile(sourceClean(t))
}
