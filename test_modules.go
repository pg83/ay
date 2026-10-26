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
	case tokBoosttest:
		peers = []string{"library/cpp/testing/boost_test"}
	case tokBoosttestWithMain:
		peers = []string{"library/cpp/testing/boost_test", "library/cpp/testing/boost_test_main"}
	case tokPy3Test, tokPy3TestBin, tokPy2Test, tokPy23Test:
		d.pyMain = ptr(strLibraryPythonPytestMainMain.any())
	case tokYqlUdfTest:
		peers = []string{"yql/essentials/tests/common/udf_test"}

		d.pyMain = ptr(strLibraryPythonPytestMainMain.any())
		d.test.depends = append(d.test.depends, "yql/essentials/tools/astdiff", "yql/tools/yqlrun", "yql/essentials/tools/udf_resolver")
		d.test.data = append(d.test.data, "arcadia/yql/essentials/cfg/udf_test")

		d.test.env = append(d.test.env,
			"YQL_ASTDIFF_PATH=yql/essentials/tools/astdiff/astdiff",
			"YQL_CONFIG_DIR=yql/essentials/cfg/udf_test",
			"YQL_YQLRUN_PATH=yql/tools/yqlrun/yqlrun",
			"YQL_SQL2YQL_PATH=yql/essentials/tools/sql2yql/sql2yql",
			"YQL_UDFRESOLVER_PATH=yql/essentials/tools/udf_resolver/udf_resolver",
		)
	case tokYqlUdfMinitest:
		peers = []string{"yql/essentials/tests/common/udf_minitest"}

		d.pyMain = ptr(strLibraryPythonPytestMainMain.any())
		d.test.depends = append(d.test.depends, "yql/essentials/tools/astdiff", "yql/essentials/tools/minirun", "yql/essentials/tools/udf_resolver")
		d.test.data = append(d.test.data, "arcadia/yql/essentials/cfg/tests")

		d.test.env = append(d.test.env,
			"YQL_ASTDIFF_PATH=yql/essentials/tools/astdiff/astdiff",
			"YQL_CONFIG_DIR=yql/essentials/cfg/tests",
			"YQL_MINIRUN_PATH=yql/essentials/tools/minirun/minirun",
			"YQL_UDFRESOLVER_PATH=yql/essentials/tools/udf_resolver/udf_resolver",
		)
	case tokGBenchmark:
		peers = []string{"library/cpp/testing/gbenchmark"}
	case tokYBenchmark:
		peers = []string{"library/cpp/testing/benchmark/main"}
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
		peer := path.Clean(stmt.Args[0].string())

		d.peerdirs = append(d.peerdirs, internStr(peer).any())
		d.addLocalIncl(prioAddIncl, parseModulePathVFS(peer))
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
	return name == tokPy3Test || name == tokPy2Test || name == tokPytestBin
}
