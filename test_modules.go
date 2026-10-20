package main

import (
	"path"
	"strings"
)

func isUnitTestModule(name TOK) bool {
	switch name {
	case tokUnittest, tokUnittestFor, tokUnittestWithCustomEntryPoint, tokYtUnittest, tokGtest:
		return true
	}

	return false
}

func applyCppTestModule(stmt *ModuleStmt, d *ModuleData, env Environment) {
	var peers []string

	switch stmt.Name {
	case tokUnittest, tokUnittestFor:
		peers = []string{"library/cpp/testing/unittest_main"}
	case tokYtUnittest:
		peers = []string{"library/cpp/testing/unittest_main", "yt/cpp/mapreduce/tests/yt_initialize_hook"}
	case tokGtest:
		peers = []string{"library/cpp/testing/gtest", "library/cpp/testing/gtest_main"}
	case tokPy3Test:
		peers = []string{"library/python/pytest"}
		d.pyMain = ptr(strLibraryPythonPytestMainMain.any())
	case tokPy2Test:
		d.pyMain = ptr(strLibraryPythonPytestMainMain.any())
	case tokGBenchmark:
		peers = []string{"library/cpp/testing/gbenchmark"}
	case tokFuzz:
		if afl, _ := env.lookup("USE_AFL"); afl == "yes" {
			peers = []string{"contrib/libs/afl"}
		} else {
			peers = []string{"contrib/libs/libfuzzer"}
		}

		if sanitizer, _ := env.lookup("SANITIZER_TYPE"); sanitizer == "" || sanitizer == "no" {
			peers = append(peers, "library/cpp/testing/nofuzz")
		}
	}

	d.peerdirs = append(d.peerdirs, internAnys(peers)...)

	if stmt.Name == tokUnittestFor && len(stmt.Args) > 0 {
		d.peerdirs = append(d.peerdirs, internStr(path.Clean(stmt.Args[0].string())).any())
	}

	if isUnitTestModule(stmt.Name) {
		if skip, _ := env.lookup("UT_SKIP_EXCEPTIONS"); skip == "yes" {
			d.cFlags = append(d.cFlags, strDutSkipExceptions.any())
		}
	}
}

func fullPathTestName(dir string) string {
	name := path.Clean(dir)
	limit := max(98-len(name), 0)

	if len(name) > limit {
		start := len(name) - limit - 1
		pos := strings.IndexByte(name[start:], '/')

		if pos >= 0 {
			pos += start
		} else {
			pos = strings.LastIndexByte(name, '/')
		}

		if pos >= 0 {
			name = name[pos+1:]
		}
	}

	return strings.ReplaceAll(name, "/", "-")
}

func isPythonTestModule(name TOK) bool {
	return name == tokPy3Test || name == tokPy2Test
}
