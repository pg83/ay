package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for rel, data := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
		}
		if err := os.WriteFile(abs, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func assertHasVFS(t *testing.T, closure []VFS, want VFS) {
	t.Helper()

	if !containsVFS(closure, want) {
		t.Fatalf("closure missing %v: %v", want, closure)
	}
}

func assertLacksVFS(t *testing.T, closure []VFS, want VFS) {
	t.Helper()

	if containsVFS(closure, want) {
		t.Fatalf("closure unexpectedly contains %v: %v", want, closure)
	}
}

func TestScanner_CythonNestedPxdUsesPy2StringSibling(t *testing.T) {
	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"pkg/mod.pyx":             "from util.generic.string cimport TString\n",
		"util/generic/string.pxd": "from libcpp.string cimport string as _std_string\n",
		"contrib/tools/cython/Cython/Includes/libcpp/string.pxd":      "from libcpp.string_view cimport string_view\nfrom libc.string cimport memcpy\n",
		"contrib/tools/cython/Cython/Includes/libcpp/string_view.pxd": "# py3 string_view\n",
		"contrib/tools/cython/Cython/Includes/libc/string.pxd":        "# py3 libc string\n",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/string.pxd":  "from libc.string cimport memcpy\n",
		"contrib/tools/cython_py2/Cython/Includes/libc/string.pxd":    "# py2 libc string\n",
	})

	scanner := NewIncludeScanner(dir, SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "pkg/mod.pyx",
		OwnAddIncl: []VFS{
			Source(""),
			Source("contrib/tools/cython/Cython/Includes"),
		},
	})

	assertHasVFS(t, closure, Source("util/generic/string.pxd"))
	assertHasVFS(t, closure, Source("contrib/tools/cython_py2/Cython/Includes/libcpp/string.pxd"))
	assertHasVFS(t, closure, Source("contrib/tools/cython_py2/Cython/Includes/libc/string.pxd"))
	assertLacksVFS(t, closure, Source("contrib/tools/cython/Cython/Includes/libcpp/string.pxd"))
	assertLacksVFS(t, closure, Source("contrib/tools/cython/Cython/Includes/libc/string.pxd"))
	assertLacksVFS(t, closure, Source("contrib/tools/cython/Cython/Includes/libcpp/string_view.pxd"))
}

func TestScanner_CythonPyxDirectStdlibStaysPy3WhileNestedPxdAddsPy2(t *testing.T) {
	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"pkg/mod.pyx":           "from libcpp.pair cimport pair\nfrom util.generic.hash cimport THashMap\n",
		"util/generic/hash.pxd": "from libcpp.pair cimport pair\n",
		"contrib/tools/cython/Cython/Includes/libcpp/pair.pxd":        "from libcpp.utility cimport move\n",
		"contrib/tools/cython/Cython/Includes/libcpp/utility.pxd":     "# py3 utility\n",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/pair.pxd":    "from libcpp.utility cimport move\n",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/utility.pxd": "# py2 utility\n",
	})

	scanner := NewIncludeScanner(dir, SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "pkg/mod.pyx",
		OwnAddIncl: []VFS{
			Source(""),
			Source("contrib/tools/cython/Cython/Includes"),
		},
	})

	assertHasVFS(t, closure, Source("contrib/tools/cython/Cython/Includes/libcpp/pair.pxd"))
	assertHasVFS(t, closure, Source("contrib/tools/cython/Cython/Includes/libcpp/utility.pxd"))
	assertHasVFS(t, closure, Source("contrib/tools/cython_py2/Cython/Includes/libcpp/pair.pxd"))
	assertHasVFS(t, closure, Source("contrib/tools/cython_py2/Cython/Includes/libcpp/utility.pxd"))
}

func TestScanner_CythonStdintSplitKeepsPy3InitButAddsPy2Types(t *testing.T) {
	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"pkg/mod.pyx":            "from util.datetime.base cimport TInstant\nfrom util.system.types cimport ui64\n",
		"util/datetime/base.pxd": "from libc.stdint cimport uint64_t\nfrom libcpp cimport bool as bool_t\n",
		"util/system/types.pxd":  "from libc.stdint cimport uint64_t\n",
		"contrib/tools/cython/Cython/Includes/libcpp/__init__.pxd":     "# py3 libcpp init\n",
		"contrib/tools/cython_py2/Cython/Includes/libcpp/__init__.pxd": "# py2 libcpp init\n",
		"contrib/tools/cython/Cython/Includes/libc/stdint.pxd":         "# py3 stdint\n",
		"contrib/tools/cython_py2/Cython/Includes/libc/stdint.pxd":     "# py2 stdint\n",
	})

	scanner := NewIncludeScanner(dir, SysInclSet{})
	closure := scanner.WalkClosure(ScanContext{
		SourceRel: "pkg/mod.pyx",
		OwnAddIncl: []VFS{
			Source(""),
			Source("contrib/tools/cython/Cython/Includes"),
		},
	})

	assertHasVFS(t, closure, Source("contrib/tools/cython/Cython/Includes/libcpp/__init__.pxd"))
	assertLacksVFS(t, closure, Source("contrib/tools/cython_py2/Cython/Includes/libcpp/__init__.pxd"))
	assertHasVFS(t, closure, Source("contrib/tools/cython/Cython/Includes/libc/stdint.pxd"))
	assertHasVFS(t, closure, Source("contrib/tools/cython_py2/Cython/Includes/libc/stdint.pxd"))
}
