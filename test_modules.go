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
	case tokPy3Test, tokPy3TestBin, tokPy2Test, tokPy23Test:
		d.pyMain = ptr(strLibraryPythonPytestMainMain.any())
		d.pyMainDeclSeq = d.nextDeclSeq()
	case tokYqlUdfTest:
		d.pyMain = ptr(strLibraryPythonPytestMainMain.any())
		d.pyMainDeclSeq = d.nextDeclSeq()
		d.test.depends = append(d.test.depends, strYqlEssentialsToolsAstdiff, strYqlToolsYqlrun, strYqlEssentialsToolsUdfResolver)
		d.test.data = append(d.test.data, "arcadia/yql/essentials/cfg/udf_test")

		d.test.env = append(d.test.env,
			"YQL_ASTDIFF_PATH=yql/essentials/tools/astdiff/astdiff",
			"YQL_CONFIG_DIR=yql/essentials/cfg/udf_test",
			"YQL_YQLRUN_PATH=yql/tools/yqlrun/yqlrun",
			"YQL_SQL2YQL_PATH=yql/essentials/tools/sql2yql/sql2yql",
			"YQL_UDFRESOLVER_PATH=yql/essentials/tools/udf_resolver/udf_resolver",
		)
	case tokYqlUdfMinitest:
		d.pyMain = ptr(strLibraryPythonPytestMainMain.any())
		d.pyMainDeclSeq = d.nextDeclSeq()
		d.test.depends = append(d.test.depends, strYqlEssentialsToolsAstdiff, strYqlEssentialsToolsMinirun, strYqlEssentialsToolsUdfResolver)
		d.test.data = append(d.test.data, "arcadia/yql/essentials/cfg/tests")

		d.test.env = append(d.test.env,
			"YQL_ASTDIFF_PATH=yql/essentials/tools/astdiff/astdiff",
			"YQL_CONFIG_DIR=yql/essentials/cfg/tests",
			"YQL_MINIRUN_PATH=yql/essentials/tools/minirun/minirun",
			"YQL_UDFRESOLVER_PATH=yql/essentials/tools/udf_resolver/udf_resolver",
		)
	}

	d.peerdirs = append(d.peerdirs, internAnys(peers)...)

	if isUnitTestModule(stmt.Name) {
		if skip, _ := env.lookupENV(envUtSkipExceptions); skip == "yes" {
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
