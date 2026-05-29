package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	fs := NewFS(root)

	if !fs.IsFile("a/b/c.txt") {
		t.Errorf("c.txt should be a file")
	}
	if !fs.IsFile("top.txt") {
		t.Errorf("top.txt should be a file")
	}
	if !fs.IsDir("a") {
		t.Errorf("a should be a dir")
	}
	if !fs.IsDir("a/b") {
		t.Errorf("a/b should be a dir")
	}
	if !fs.IsDir("") {
		t.Errorf("root should be a dir")
	}
	if fs.IsFile("a") {
		t.Errorf("a is a dir, not a file")
	}
	if fs.IsDir("a/b/c.txt") {
		t.Errorf("c.txt is a file, not a dir")
	}
	if fs.IsFile("a/b/missing") {
		t.Errorf("missing should not exist")
	}
	if fs.IsFile("totally/missing/path") {
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

	fs := NewFS(root)

	if !fs.IsFile("d/a.txt") {
		t.Fatal("a missing")
	}
	if !fs.IsFile("d/b.txt") {
		t.Fatal("b missing")
	}
	if !fs.IsFile("d/c.txt") {
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
	fs := NewFS(root)

	if fs.Listdir("nope") != nil {
		t.Error("missing dir should return nil listdir")
	}
	if fs.Listdir("nope") != nil {
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
	fs := NewFS(root)

	data := fs.Read("file.txt")
	if string(data) != "hello world" {
		t.Errorf("got %q", string(data))
	}

	if exc := Try(func() { fs.Read("missing.txt") }); exc == nil {
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

	fs := NewFS(root)

	files := map[string]bool{}
	fs.Walk("a", func(rel string, isDir bool) {
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

var testParserFS = NewFS("/")

// memFS is the test-side FS implementation. It serves Listdir/Read/Walk/
// Exists from in-memory maps populated once at construction; no method ever
// reads the OS. Tests should not mutate the returned *memFS (the package-
// level testFS is shared across the whole suite).
type memFS struct {
	sourceRoot string
	rootSlash  string
	files      map[string][]byte
	dirs       map[string]map[string]bool
}

// newMemFS builds a *memFS from a flat path→content map. Every intermediate
// directory is materialised so Exists / IsDir / Listdir match what an osFS
// rooted at a real tree with the same shape would return.
func newMemFS(files map[string]string) *memFS {
	const root = "/__fake_repo__"

	fs := &memFS{
		sourceRoot: root,
		rootSlash:  root + "/",
		files:      make(map[string][]byte, len(files)),
		dirs:       map[string]map[string]bool{"": {}},
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

func (fs *memFS) SourceRoot() string { return fs.sourceRoot }

func (fs *memFS) Listdir(rel string) map[string]bool {
	return fs.dirs[cleanRel(rel)]
}

func (fs *memFS) Exists(rel string) (present bool, isDir bool) {
	rel = cleanRel(rel)
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

func (fs *memFS) IsFile(rel string) bool {
	p, d := fs.Exists(rel)
	return p && !d
}

func (fs *memFS) IsDir(rel string) bool {
	p, d := fs.Exists(rel)
	return p && d
}

func (fs *memFS) Read(rel string) []byte {
	data, ok := fs.files[cleanRel(rel)]
	if !ok {
		ThrowFmt("memFS: no such file %q", rel)
	}

	return append([]byte(nil), data...)
}

func (fs *memFS) ReadInto(rel string, buf []byte) []byte {
	data, ok := fs.files[cleanRel(rel)]
	if !ok {
		ThrowFmt("memFS: no such file %q", rel)
	}

	if cap(buf) < len(data) {
		buf = make([]byte, 0, len(data))
	}

	return append(buf[:0], data...)
}

func (fs *memFS) ReadAbs(absPath string) []byte {
	return fs.Read(fs.relForAbs(absPath))
}

func (fs *memFS) ExistsAbs(absPath string) (present bool, isDir bool) {
	return fs.Exists(fs.relForAbs(absPath))
}

func (fs *memFS) relForAbs(absPath string) string {
	if absPath == fs.sourceRoot {
		return ""
	}

	if strings.HasPrefix(absPath, fs.rootSlash) {
		return absPath[len(fs.rootSlash):]
	}

	ThrowFmt("memFS.relForAbs: %q outside source root %q", absPath, fs.sourceRoot)

	return ""
}

func (fs *memFS) Walk(rel string, visit func(rel string, isDir bool)) {
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

func (fs *memFS) perfStats() fsPerfStats { return fsPerfStats{} }

// newTestScanner spins up a scanner backed by the given FS (typically a per-
// test memFS). Each call yields a fresh scanner so per-test CodegenRegistry /
// cache state does not leak.
func newTestScanner(fs FS, sysincl SysInclSet) *IncludeScanner {
	return newIncludeScannerWith(
		newIncludeParserManagerFS(fs, newSharedParseCache()),
		sysincl,
		func(Warn) {},
	)
}
