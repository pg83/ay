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
	fs.walk("a", func(rel string, isDir bool) bool {
		if !isDir {
			files[rel] = true
		}

		return true
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

var testParserFS = newMemFS(nil)

func newTestScanner(fs FS, sysincl SysInclSet) *IncludeScanner {
	s := newIncludeScannerWith(
		newIncludeParserManagerFS(fs, newSharedParseCache()),
		sysincl,
		func(Warn) {},
		&TarjanCtx{},
		newBucketCache(),
	)

	s.codegen = newCodegenRegistry(newNodeArenas())
	s.moduleByRef = &DenseMap[NodeRef, *ModuleEmitResult]{}

	return s
}

func wireTestScanners(ctx *GenCtx) {
	fs := ctx.fs

	if fs == nil {
		fs = newMemFS(nil)
	}

	mk := func() *IncludeScanner {
		s := newTestScanner(fs, SysInclSet{})

		if ctx.parsers != nil {
			s.parsers = ctx.parsers
		}

		return s
	}

	ctx.scannerTarget = mk()
	ctx.scannerHost = mk()
}
