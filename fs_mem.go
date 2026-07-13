package main

type MemFS struct {
	srcRoot     string
	rootSlash   string
	files       map[string][]byte
	dirs        map[string]map[string]bool
	views       map[string]DirView
	entries     *IntSet
	sourceUnder *IntMap[STR]
}

func newMemFS(files map[string]string) *MemFS {
	const root = "/__fake_repo__"

	fs := &MemFS{
		srcRoot:     root,
		rootSlash:   root + "/",
		files:       make(map[string][]byte, len(files)),
		dirs:        map[string]map[string]bool{"": {}},
		views:       map[string]DirView{},
		entries:     newIntSet(64),
		sourceUnder: newIntMap[STR](256),
	}

	addEntry := func(parent, name string, isDir bool) {
		entries := fs.dirs[parent]

		if entries == nil {
			entries = map[string]bool{}
			fs.dirs[parent] = entries
		}

		if prev, ok := entries[name]; !ok || (isDir && !prev) {
			entries[name] = isDir
		}
	}

	for rel, content := range files {
		rel = cleanRel(rel)
		fs.files[rel] = []byte(content)

		cur := rel
		isDirEntry := false

		for {
			parent, name := splitDirName(cur)

			addEntry(parent, name, isDirEntry)

			if parent == "" {
				break
			}

			cur = parent
			isDirEntry = true
		}
	}

	return fs
}

func (fs *MemFS) listdir(dir STR) DirView {
	rel := dir.string()

	if v, ok := fs.views[rel]; ok {
		return v
	}

	entries, ok := fs.dirs[rel]

	if !ok {
		fs.views[rel] = DirView{}

		return DirView{}
	}

	key := dir
	packed := make([]uint32, 0, len(entries))

	for name, isDir := range entries {
		id := internStr(name)
		p := uint32(id) << 1

		if isDir {
			p |= 1
		}

		packed = append(packed, p)
		fs.entries.put(splitMix64(uint32(key), uint32(id)), isDir)
	}

	v := DirView{dir: key, names: packed}

	fs.views[rel] = v

	return v
}

func (fs *MemFS) dirHas(v DirView, name string) (present bool, isDir bool) {
	id := interned(name)

	if id == 0 {
		return false, false
	}

	isDir, ok := fs.entries.get(splitMix64(uint32(v.dir), uint32(id)))

	if !ok {
		return false, false
	}

	return true, isDir
}

func (fs *MemFS) existsRel(rel string) (present bool, isDir bool) {
	rel = normalisePath(cleanRel(rel))

	if rel == "" {
		return true, true
	}

	dir, name := splitDirName(rel)
	entries, ok := fs.dirs[dir]

	if !ok {
		return false, false
	}

	isDir, ok = entries[name]

	return ok, isDir
}

func (fs *MemFS) exists(prefix STR, suffix string) (present bool, isDir bool) {
	return fs.existsRel(joinRel(prefix.string(), suffix))
}

func (fs *MemFS) isFile(prefix STR, suffix string) bool {
	p, d := fs.exists(prefix, suffix)

	return p && !d
}

func (fs *MemFS) resolveSourceUnder(prefix, target STR) STR {
	key := splitMix64(uint32(prefix), uint32(target))

	if p := fs.sourceUnder.get(key); p != nil {
		return *p
	}

	rel := joinRel(prefix.string(), target.string())

	var v STR

	if p, d := fs.existsRel(rel); p && !d {
		v = internStr(normalisePath(cleanRel(rel)))
	}

	fs.sourceUnder.put(key, v)

	return v
}

func (fs *MemFS) resolveSourceUnderClean(prefix, target STR, _ bool) STR {
	return fs.resolveSourceUnder(prefix, target)
}

func (fs *MemFS) isDir(prefix STR, suffix string) bool {
	p, d := fs.exists(prefix, suffix)

	return p && d
}

func (fs *MemFS) read(rel string) [][]byte {
	data, ok := fs.files[cleanRel(rel)]

	if !ok {
		throwFmt("memFS: no such file %q", rel)
	}

	if len(data) <= readChunkSize {
		return [][]byte{data}
	}

	return [][]byte{data[:readChunkSize], data[readChunkSize:]}
}

func (fs *MemFS) contentHash(rel STR) uint64 {
	data, ok := fs.files[cleanRel(rel.string())]

	if !ok {
		return 0
	}

	return contentHashBytes(data)
}

func (fs *MemFS) walk(rel string, visit func(rel string, isDir bool) bool) {
	rel = cleanRel(rel)

	present, isDir := fs.existsRel(rel)

	if !present {
		return
	}

	if !visit(rel, isDir) || !isDir {
		return
	}

	prefix := rel

	if prefix != "" {
		prefix += "/"
	}

	for name, childIsDir := range fs.dirs[rel] {
		child := prefix + name

		if childIsDir {
			fs.walk(child, visit)

			continue
		}

		visit(child, false)
	}
}
