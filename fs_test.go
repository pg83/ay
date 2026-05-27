package main

import (
	"os"
	"path/filepath"
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
