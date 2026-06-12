package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeebo/xxh3"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFS_ExistsAndIsDir(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a/b/c.txt":     "hi",
		"a/b/d.txt":     "bye",
		"a/empty/.keep": "",
		"top.txt":       "top",
	})

	fs := newFS(root)

	if !fs.isFile(dirKey(""), "a/b/c.txt") {
		t.Errorf("c.txt should be a file")
	}
	if !fs.isFile(dirKey(""), "top.txt") {
		t.Errorf("top.txt should be a file")
	}
	if !fs.isDir(dirKey(""), "a") {
		t.Errorf("a should be a dir")
	}
	if !fs.isDir(dirKey(""), "a/b") {
		t.Errorf("a/b should be a dir")
	}
	if !fs.isDir(dirKey(""), "") {
		t.Errorf("root should be a dir")
	}
	if fs.isFile(dirKey(""), "a") {
		t.Errorf("a is a dir, not a file")
	}
	if fs.isDir(dirKey(""), "a/b/c.txt") {
		t.Errorf("c.txt is a file, not a dir")
	}
	if fs.isFile(dirKey(""), "a/b/missing") {
		t.Errorf("missing should not exist")
	}
	if fs.isFile(dirKey(""), "totally/missing/path") {
		t.Errorf("missing parent dir should not exist")
	}
}

func TestFS_ExistsRoutesThroughListdir(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"d/a.txt": "1",
		"d/b.txt": "2",
		"d/c.txt": "3",
	})

	fs := newFS(root)

	if !fs.isFile(dirKey("d"), "a.txt") {
		t.Fatal("a missing")
	}
	if !fs.isFile(dirKey("d"), "b.txt") {
		t.Fatal("b missing")
	}
	if !fs.isFile(dirKey("d"), "c.txt") {
		t.Fatal("c missing")
	}

	stats := fs.perfStats()
	if stats.listdirMisses != 1 {
		t.Errorf("expected exactly 1 listdir miss for shared dir, got %d", stats.listdirMisses)
	}
	if stats.listdirHits != 2 {
		t.Errorf("expected 2 listdir hits (3 calls - 1 miss), got %d", stats.listdirHits)
	}
}

func TestFS_ListdirCachesNegative(t *testing.T) {
	root := t.TempDir()
	fs := newFS(root)

	if fs.listdir(dirKey("nope")).listable() {
		t.Error("missing dir should return nil listdir")
	}
	if fs.listdir(dirKey("nope")).listable() {
		t.Error("missing dir should still return nil on cache hit")
	}
	stats := fs.perfStats()
	if stats.listdirMisses != 1 {
		t.Errorf("expected exactly 1 listdir miss after two calls, got %d", stats.listdirMisses)
	}
}

func TestFS_Read(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"file.txt": "hello world",
	})
	fs := newFS(root)

	data := fs.read("file.txt")
	if string(data) != "hello world" {
		t.Errorf("got %q", string(data))
	}

	if exc := try(func() { fs.read("missing.txt") }); exc == nil {
		t.Error("missing file should Throw")
	}
}

func TestFS_Walk(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a/b/c.txt": "1",
		"a/b/d.txt": "2",
		"a/e.txt":   "3",
		"top.txt":   "4",
	})

	fs := newFS(root)

	files := map[string]bool{}
	fs.walk("a", func(rel string, isDir bool) {
		if !isDir {
			files[rel] = true
		}
	})

	want := map[string]bool{
		"a/b/c.txt": true,
		"a/b/d.txt": true,
		"a/e.txt":   true,
	}
	for k := range want {
		if !files[k] {
			t.Errorf("Walk missed %q", k)
		}
	}
	if files["top.txt"] {
		t.Errorf("Walk leaked outside the subtree")
	}
}

func TestFS_CleanRel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{".", ""},
		{"/", ""},
		{"a", "a"},
		{"/a", "a"},
		{"a/", "a"},
		{"a/b", "a/b"},
		{"a//b", "a/b"},
		{"a/./b", "a/b"},
	}
	for _, c := range cases {
		if got := cleanRel(c.in); got != c.want {
			t.Errorf("cleanRel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

var testParserFS = newFS("/")

// memFS is the test-side FS implementation. It serves Listdir/Read/Walk/
// Exists from in-memory maps populated once at construction; no method ever
// reads the OS. Tests should not mutate the returned *memFS (the package-
// level newMemFS(nil) is shared across the whole suite).
type MemFS struct {
	srcRoot   string
	rootSlash string
	files     map[string][]byte
	dirs      map[string]map[string]bool

	// views/entries mirror OsFS's DirView model over the in-memory tree,
	// built lazily per directory.
	views   map[string]DirView
	entries *IntMap[bool]
}

// newMemFS builds a *memFS from a flat path→content map. Every intermediate
// directory is materialised so Exists / IsDir / Listdir match what an osFS
// rooted at a real tree with the same shape would return.
func newMemFS(files map[string]string) *MemFS {
	const root = "/__fake_repo__"

	fs := &MemFS{
		srcRoot:   root,
		rootSlash: root + "/",
		files:     make(map[string][]byte, len(files)),
		dirs:      map[string]map[string]bool{"": {}},
		views:     map[string]DirView{},
		entries:   newIntMap[bool](64),
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

func (fs *MemFS) sourceRoot() string { return fs.srcRoot }

func (fs *MemFS) listdir(dir VFS) DirView {
	rel := dir.rel()

	if v, ok := fs.views[rel]; ok {
		return v
	}

	entries, ok := fs.dirs[rel]

	if !ok {
		fs.views[rel] = DirView{}

		return DirView{}
	}

	key := STR(dir.strID())
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

	d := fs.entries.get(splitMix64(uint32(v.dir), uint32(id)))

	if d == nil {
		return false, false
	}

	return true, *d
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

func (fs *MemFS) exists(prefix VFS, suffix string) (present bool, isDir bool) {
	return fs.existsRel(joinRel(prefix.rel(), suffix))
}

func (fs *MemFS) isFile(prefix VFS, suffix string) bool {
	p, d := fs.exists(prefix, suffix)
	return p && !d
}

func (fs *MemFS) isDir(prefix VFS, suffix string) bool {
	p, d := fs.exists(prefix, suffix)
	return p && d
}

func (fs *MemFS) read(rel string) []byte {
	data, ok := fs.files[cleanRel(rel)]
	if !ok {
		throwFmt("memFS: no such file %q", rel)
	}

	return append([]byte(nil), data...)
}

// ContentHash computes xxh3 of source VFS v's file content, on demand from the
// in-memory tree (the shared test FS is never mutated). Fixtures are minimal, so a
// file absent from the tree hashes to 0 rather than faulting — tests assert
// structure, not exact uids.
func (fs *MemFS) contentHash(v VFS) uint64 {
	data, ok := fs.files[cleanRel(v.rel())]
	if !ok {
		return 0
	}
	return xxh3.Hash(data)
}

func (fs *MemFS) walk(rel string, visit func(rel string, isDir bool)) {
	rel = cleanRel(rel)

	present, isDir := fs.existsRel(rel)
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

	for name, childIsDir := range fs.dirs[rel] {
		child := prefix + name
		if childIsDir {
			fs.walk(child, visit)
			continue
		}
		visit(child, false)
	}
}

func (fs *MemFS) perfStats() FsPerfStats { return FsPerfStats{} }

// newTestScanner spins up a scanner backed by the given FS (typically a per-
// test memFS). Each call yields a fresh scanner so per-test CodegenRegistry /
// cache state does not leak.
func newTestScanner(fs FS, sysincl SysInclSet) *IncludeScanner {
	s := newIncludeScannerWith(
		newIncludeParserManagerFS(fs, newSharedParseCache()),
		sysincl,
		func(Warn) {},
		&TarjanCtx{},
	)
	// Prod wiring always attaches a registry (gen.go); the scanner relies on it.
	s.codegen = newCodegenRegistry()

	return s
}
