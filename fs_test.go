package main

import (
	"bytes"
	"fmt"
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

}

func TestFS_ResolveSourceUnder(t *testing.T) {
	files := map[string]string{
		"a/b/c.txt": "hi",
		"top.txt":   "top",
	}

	root := t.TempDir()
	writeTree(t, root, files)

	for _, tc := range []struct {
		name string
		fs   FS
	}{
		{"os", newFS(root)},
		{"mem", newMemFS(files)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := tc.fs

			check := func(prefix, target, want string) {
				t.Helper()

				wantSTR := STR(0)

				if want != "" {
					wantSTR = internStr(want)
				}

				got := fs.resolveSourceUnder(dirKey(prefix), internStr(target))

				if got != wantSTR {
					t.Errorf("resolveSourceUnder(%q, %q) = %q, want %q", prefix, target, got.string(), want)
				}

				if again := fs.resolveSourceUnder(dirKey(prefix), internStr(target)); again != got {
					t.Errorf("resolveSourceUnder(%q, %q) not idempotent: %q then %q", prefix, target, got.string(), again.string())
				}
			}

			check("a/b", "c.txt", "a/b/c.txt")
			check("a", "b/c.txt", "a/b/c.txt")
			check("", "a/b/c.txt", "a/b/c.txt")
			check("", "top.txt", "top.txt")

			check("a/b", "missing.txt", "")
			check("", "nope/x.txt", "")
			check("a", "c.txt", "")

			check("a", "b/../b/c.txt", "a/b/c.txt")
			check("", "a/./b/c.txt", "a/b/c.txt")
		})
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

	data := concatChunks(fs.read("file.txt"))

	if string(data) != "hello world" {
		t.Errorf("got %q", string(data))
	}

	if exc := try(func() { fs.read("missing.txt") }); exc == nil {
		t.Error("missing file should Throw")
	}
}

func TestFS_ReadAroundChunkSize(t *testing.T) {
	for _, size := range []int{readChunkSize - 1, readChunkSize, readChunkSize + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			root := t.TempDir()
			want := make([]byte, size)

			for i := range want {
				want[i] = byte(i*31 + 7)
			}

			if err := os.WriteFile(filepath.Join(root, "data"), want, 0o644); err != nil {
				t.Fatal(err)
			}

			fs := newFS(root)

			chunks := fs.read("data")

			if got := concatChunks(chunks); !bytes.Equal(got, want) {
				t.Fatalf("read %d bytes: got %d bytes with different content", size, len(got))
			}

			wantChunks := 1

			if size > readChunkSize {
				wantChunks = 2
			}

			if len(chunks) != wantChunks {
				t.Fatalf("read %d bytes returned %d chunks, want %d", size, len(chunks), wantChunks)
			}

			if len(chunks[0]) != min(size, readChunkSize) {
				t.Fatalf("read %d bytes: first chunk has %d bytes", size, len(chunks[0]))
			}

			if len(chunks) == 2 && len(chunks[1]) != size-readChunkSize {
				t.Fatalf("read %d bytes: mmap tail has %d bytes", size, len(chunks[1]))
			}

			wantHash := xxh3.Hash(want)

			if size > readChunkSize {
				wantHash = xxh3.Hash(want[:readChunkSize]) ^ xxh3.Hash(want[readChunkSize:])
			}

			if got := fs.contentHash(internStr("data")); got != wantHash {
				t.Fatalf("content hash for %d bytes = %#x, want %#x", size, got, wantHash)
			}
		})
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
