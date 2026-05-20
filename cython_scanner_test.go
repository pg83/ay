package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanner_CythonNestedPxdUsesPy2StringSibling(t *testing.T) {
	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"pkg/mod.pyx": `from util.generic.string cimport TString
`,
		"util/generic/string.pxd": `from libcpp.string cimport string as _std_string
`,
		"contrib/tools/cython/Cython/Includes/libcpp/string.pxd": `from libcpp.string_view cimport string_view
from libc.string cimport const_char
`,
		"contrib/tools/cython/Cython/Includes/libc/string.pxd":        "",
		"contrib/tools/cython/Cython/Includes/libcpp/string_view.pxd": "",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/string.pxd": `from libc.string cimport const_char
`,
		"contrib/tools/cython_py2/Cython/Includes/libc/string.pxd": "",
	})

	scanner := NewIncludeScanner(dir, SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "pkg/mod.pyx",
		OwnAddIncl: []VFS{
			Source(""),
			Source("contrib/tools/cython/Cython/Includes"),
		},
	})

	if !containsVFS(closure, Source("util/generic/string.pxd")) {
		t.Fatalf("closure missing util/generic/string.pxd: %v", closure)
	}

	if !containsVFS(closure, Source("contrib/tools/cython_py2/Cython/Includes/libcpp/string.pxd")) {
		t.Fatalf("closure missing py2 libcpp/string.pxd: %v", closure)
	}

	if !containsVFS(closure, Source("contrib/tools/cython_py2/Cython/Includes/libc/string.pxd")) {
		t.Fatalf("closure missing py2 libc/string.pxd: %v", closure)
	}

	if containsVFS(closure, Source("contrib/tools/cython/Cython/Includes/libcpp/string.pxd")) {
		t.Fatalf("closure unexpectedly kept py3 libcpp/string.pxd: %v", closure)
	}

	if containsVFS(closure, Source("contrib/tools/cython/Cython/Includes/libc/string.pxd")) {
		t.Fatalf("closure unexpectedly kept py3 libc/string.pxd: %v", closure)
	}

	if containsVFS(closure, Source("contrib/tools/cython/Cython/Includes/libcpp/string_view.pxd")) {
		t.Fatalf("closure unexpectedly pulled py3-only string_view.pxd: %v", closure)
	}
}

func TestScanner_CythonPyxDirectStdlibStaysPy3WhileNestedPxdAddsPy2(t *testing.T) {
	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"pkg/mod.pyx": `from libcpp.pair cimport pair
from util.generic.hash cimport THashMap
`,
		"util/generic/hash.pxd": `from libcpp.pair cimport pair
`,
		"contrib/tools/cython/Cython/Includes/libcpp/pair.pxd": `from .utility cimport pair
`,
		"contrib/tools/cython/Cython/Includes/libcpp/utility.pxd": "",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/pair.pxd": `from .utility cimport pair
`,
		"contrib/tools/cython_py2/Cython/Includes/libcpp/utility.pxd": "",
	})

	scanner := NewIncludeScanner(dir, SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "pkg/mod.pyx",
		OwnAddIncl: []VFS{
			Source(""),
			Source("contrib/tools/cython/Cython/Includes"),
		},
	})

	for _, want := range []VFS{
		Source("contrib/tools/cython/Cython/Includes/libcpp/pair.pxd"),
		Source("contrib/tools/cython/Cython/Includes/libcpp/utility.pxd"),
		Source("contrib/tools/cython_py2/Cython/Includes/libcpp/pair.pxd"),
		Source("contrib/tools/cython_py2/Cython/Includes/libcpp/utility.pxd"),
	} {
		if !containsVFS(closure, want) {
			t.Fatalf("closure missing %s: %v", want, closure)
		}
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}
