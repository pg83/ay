package main

import (
	"strings"
	"testing"
)

func TestCythonImplicitFallthrough(t *testing.T) {
	tests := []struct {
		name        string
		stmt        *CythonStmt
		py23Variant bool
		want        bool
	}{
		{
			name:        "pyx in py3 library",
			stmt:        &CythonStmt{Src: "foo.pyx"},
			py23Variant: false,
			want:        true,
		},
		{
			name:        "pyx in py23 library",
			stmt:        &CythonStmt{Src: "foo.pyx"},
			py23Variant: true,
			want:        true,
		},
		{
			name:        "py source in py23 library",
			stmt:        &CythonStmt{Src: "graph.py"},
			py23Variant: true,
			want:        true,
		},
		{
			name:        "py source in py3 library",
			stmt:        &CythonStmt{Src: "graph.py"},
			py23Variant: false,
			want:        false,
		},
		{
			name:        "cmode never gets the flag",
			stmt:        &CythonStmt{Src: "foo.pyx", CMode: true},
			py23Variant: false,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cythonImplicitFallthrough(tt.stmt, tt.py23Variant)
			if got != tt.want {
				t.Fatalf("cythonImplicitFallthrough(%+v, %t) = %t, want %t", *tt.stmt, tt.py23Variant, got, tt.want)
			}
		})
	}
}

// arMemberIndex returns the position of $(B)/<dir>/<rel> among an archive node's
// members, failing when the member is absent.
func arMemberIndex(t *testing.T, ar *Node, dir, rel string) int {
	t.Helper()

	want := "$(B)/" + dir + "/" + rel

	for i, in := range ar.flatInputs() {
		if in.string() == want {
			return i
		}
	}

	t.Fatalf("archive %v missing member %q: %v", ar.Outputs, want, vfsStrings(ar.flatInputs()))

	return -1
}

// PY_SRCS cython sources group into five fixed-order variant buckets; each emits
// its generated compile and `.reg3.cpp` in bucket order. So cares + helper (C)
// sort BEFORE corecext (C_H) despite the reverse textual order, the SRCS callbacks
// object stays first, and the global .reg3.cpp members follow bucket order.
func TestGen_CythonVariantBucketARMemberOrder(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nSRCS(callbacks.c)\nPY_SRCS(TOP_LEVEL CYTHON_C_H corecext.pyx CYTHON_C cares.pyx CYTHONIZE_PY helper.py)\nEND()\n")
	writeTestModuleFile(files, "pkg/callbacks.c", "int cb(){return 0;}\n")
	writeTestModuleFile(files, "pkg/corecext.pyx", "def f():\n    return 0\n")
	writeTestModuleFile(files, "pkg/cares.pyx", "def g():\n    return 1\n")
	writeTestModuleFile(files, "pkg/helper.py", "def h():\n    return 2\n")

	g := testGen(newMemFS(files), "pkg")

	regular := mustNodeByOutput(t, g, "$(B)/pkg/libpy3pkg.a")
	cb := arMemberIndex(t, regular, "pkg", "callbacks.c.o")
	cares := arMemberIndex(t, regular, "pkg", "cares.pyx.c.o")
	helper := arMemberIndex(t, regular, "pkg", "helper.py.c.o")
	corecext := arMemberIndex(t, regular, "pkg", "corecext.c.o")

	if !(cb < cares && cares < helper && helper < corecext) {
		t.Fatalf("regular archive order callbacks(%d) < cares(%d) < helper(%d) < corecext(%d) violated: %v",
			cb, cares, helper, corecext, vfsStrings(regular.flatInputs()))
	}

	global := mustNodeByOutput(t, g, "$(B)/pkg/libpy3pkg.global.a")
	caresR := arMemberIndex(t, global, "pkg", "cares.reg3.cpp.o")
	helperR := arMemberIndex(t, global, "pkg", "helper.reg3.cpp.o")
	corecextR := arMemberIndex(t, global, "pkg", "corecext.reg3.cpp.o")

	if !(caresR < helperR && helperR < corecextR) {
		t.Fatalf("global .reg3.cpp order cares(%d) < helper(%d) < corecext(%d) violated: %v",
			caresR, helperR, corecextR, vfsStrings(global.flatInputs()))
	}
}

func TestGen_CythonizePyFollowsCythonCMode(t *testing.T) {
	// CYTHONIZE_PY only flips a flag; after CYTHON_C a following `.py` is built in C
	// mode and named `<src>.py.c`.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nPY_SRCS(NAMESPACE pkg CYTHON_C mod.pyx CYTHONIZE_PY helper.py)\nEND()\n")
	writeTestModuleFile(files, "pkg/mod.pyx", "def f():\n    return 0\n")
	writeTestModuleFile(files, "pkg/helper.py", "def g():\n    return 1\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/helper.py.c")
	if nodeByOutput(g, "$(B)/pkg/helper.py.cpp") != nil {
		t.Fatalf("CYTHONIZE_PY .py after CYTHON_C must not emit a C++ output")
	}

	for _, a := range cy.Cmds[0].CmdArgs.flat() {
		if a.string() == "--cplus" {
			t.Fatalf("C-mode cython invocation must not pass --cplus: %#v", cy.Cmds[0].CmdArgs.flat())
		}
	}
}

func TestGen_CythonCApiHeaderEmitsCompanionHeaders(t *testing.T) {
	// The _API_H variant uses `noext` naming and emits the generated `.c` plus
	// companion `.h` and `_api.h` outputs.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nPY_SRCS(NAMESPACE pkg CYTHON_C_API_H etree.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/etree.pyx", "def f():\n    return 0\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/etree.c")
	if nodeByOutput(g, "$(B)/pkg/etree.pyx.c") != nil {
		t.Fatalf("CYTHON_C_API_H must use noext naming (etree.c), not etree.pyx.c")
	}

	got := make(map[string]bool)
	for _, o := range cy.Outputs {
		got[o.string()] = true
	}

	for _, want := range []string{"$(B)/pkg/etree.c", "$(B)/pkg/etree.h", "$(B)/pkg/etree_api.h"} {
		if !got[want] {
			t.Fatalf("CY node missing output %q; outputs=%v", want, cy.Outputs)
		}
	}
}

func TestGen_CythonHeaderVariantOmitsInducedCppClosure(t *testing.T) {
	// The _H/_API_H macros route the induced "cpp" closure onto the generated .h
	// output, not the producer node. So a Header CY node carries only cython.py, the
	// bare embedded utility files, the source, and the pyx-language closure — never
	// the cdef extern-from C/C++ header.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"PY_SRCS(NAMESPACE pkg CYTHON_C_API_H api.pyx CYTHON_C plain.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/api.pyx", "cdef extern from \"extlib/foo.h\":\n    pass\n")
	writeTestModuleFile(files, "pkg/plain.pyx", "cdef extern from \"extlib/foo.h\":\n    pass\n")
	writeTestModuleFile(files, "pkg/extlib/foo.h", "#pragma once\n")

	g := testGen(newMemFS(files), "pkg")

	const externHeader = "$(S)/pkg/extlib/foo.h"
	const pythonInclude = "$(S)/contrib/libs/python/Include/Python.h"

	// Header variant (api.c): both must be absent.
	header := mustNodeByOutput(t, g, "$(B)/pkg/api.c")
	for _, in := range header.flatInputs() {
		s := in.string()
		if s == externHeader {
			t.Fatalf("Header CY node must not carry the cdef extern-from target %q; inputs=%v", externHeader, header.flatInputs())
		}
		if s == pythonInclude {
			t.Fatalf("Header CY node must not carry the CYTHON_OUTPUT_INCLUDES single %q; inputs=%v", pythonInclude, header.flatInputs())
		}
	}

	// Non-Header variant (plain.pyx.c): both must be present (unchanged).
	plain := mustNodeByOutput(t, g, "$(B)/pkg/plain.pyx.c")
	plainInputs := map[string]bool{}
	for _, in := range plain.flatInputs() {
		plainInputs[in.string()] = true
	}

	for _, want := range []string{externHeader, pythonInclude} {
		if !plainInputs[want] {
			t.Fatalf("non-Header CY node missing input %q; inputs=%v", want, plain.flatInputs())
		}
	}
}

func TestGen_CythonizePyPxdSideInputClosure(t *testing.T) {
	// For a CYTHONIZE_PY `.py` source with a sibling `<mod-as-path>.pxd`, `dep = pxd`
	// is passed as a hidden input whose transitive closure rides the CY node.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(FOR cython pkg)\n"+
			"PY_SRCS(TOP_LEVEL CYTHON_C gevent/mod.pyx CYTHONIZE_PY gevent/helper.py=gevent._helper gevent/plain.py=gevent._plain)\nEND()\n")
	writeTestModuleFile(files, "pkg/gevent/mod.pyx", "def f():\n    return 0\n")
	writeTestModuleFile(files, "pkg/gevent/helper.py", "def g():\n    return 1\n")
	writeTestModuleFile(files, "pkg/gevent/plain.py", "def h():\n    return 2\n")
	writeTestModuleFile(files, "pkg/gevent/_helper.pxd",
		"from gevent._dep cimport thing\ncdef extern from \"gevent/api.h\":\n    pass\n")
	writeTestModuleFile(files, "pkg/gevent/_dep.pxd", "cdef int x\n")
	writeTestModuleFile(files, "pkg/gevent/api.h", "#pragma once\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/gevent/helper.py.c")

	counts := map[string]int{}
	for _, in := range cy.flatInputs() {
		counts[in.string()]++
	}

	for _, want := range []string{
		"$(S)/pkg/gevent/_helper.pxd",
		"$(S)/pkg/gevent/_dep.pxd",
		"$(S)/pkg/gevent/api.h",
	} {
		switch counts[want] {
		case 0:
			t.Fatalf("CY node missing pxd-side input %q; inputs=%v", want, cy.flatInputs())
		case 1:
		default:
			t.Fatalf("CY node lists pxd-side input %q %d times, want exactly once", want, counts[want])
		}
	}

	// The generated .c's compile node carries the same pxd closure.
	cc := mustNodeByOutputSuffix(t, g, "gevent/helper.py.c.o")

	ccInputs := map[string]bool{}
	for _, in := range cc.flatInputs() {
		ccInputs[in.string()] = true
	}

	for _, want := range []string{
		"$(S)/pkg/gevent/_helper.pxd",
		"$(S)/pkg/gevent/_dep.pxd",
		"$(S)/pkg/gevent/api.h",
	} {
		if !ccInputs[want] {
			t.Fatalf("generated-.c compile node missing pxd-side input %q; inputs=%v", want, cc.flatInputs())
		}
	}

	// A CYTHONIZE_PY .py with no matching pxd carries no pxd side input.
	plain := mustNodeByOutput(t, g, "$(B)/pkg/gevent/plain.py.c")
	for _, in := range plain.flatInputs() {
		if strings.HasSuffix(in.string(), ".pxd") {
			t.Fatalf("CY node for a pxd-less .py must not carry a pxd input: %q", in.string())
		}
	}
}

func TestGen_CythonPyxCarriesNoPxdDep(t *testing.T) {
	// The macro's hidden `Dep` is the source itself for a `.pyx`; only a CYTHONIZE_PY
	// `.py` turns `dep` into `<mod-as-path>.pxd`. So a `.pyx` whose `<mod>.pxd`
	// resolves but is not cimported must NOT carry that pxd.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(FOR cython pkg)\n"+
			"PY_SRCS(NAMESPACE pkg sub/mod.pyx=foo)\nEND()\n")
	writeTestModuleFile(files, "pkg/sub/mod.pyx", "def f():\n    return 0\n")
	// `<mod>.pxd` for mod name `foo` resolves but the .pyx does not cimport it.
	writeTestModuleFile(files, "pkg/foo.pxd", "cdef extern from \"pkg/extra.h\":\n    pass\n")
	writeTestModuleFile(files, "pkg/extra.h", "#pragma once\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/sub/mod.pyx.cpp")
	for _, in := range cy.flatInputs() {
		s := in.string()
		if s == "$(S)/pkg/foo.pxd" || s == "$(S)/pkg/extra.h" {
			t.Fatalf("CY node for a .pyx must not carry a non-cimported <mod>.pxd Dep input %q; inputs=%v", s, cy.flatInputs())
		}
	}
}

func TestGen_CythonNameListCimportClosure(t *testing.T) {
	// CimportFrom: `from X cimport name-list` resolves the package `X/__init__.pxd`,
	// the module `X.pxd`, and each cimported name as a submodule `X/name.pxd` (or
	// `X/name/__init__.pxd`). CimportSimple (`cimport a.b`) resolves `a/b.pxd` then
	// `a/b/__init__.pxd`.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(FOR cython pkg)\n"+
			"PY_SRCS(NAMESPACE app mod.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/mod.pyx",
		"from app cimport top\n"+
			"from app.includes cimport tree, config\n"+
			"from libc cimport limits\n"+
			"include \"x.pxi\"\n")
	writeTestModuleFile(files, "pkg/app/__init__.pxd", "cdef int a\n")
	writeTestModuleFile(files, "pkg/app/top.pxd", "cdef int b\n")
	writeTestModuleFile(files, "pkg/app/includes/__init__.pxd", "cdef int c\n")
	writeTestModuleFile(files, "pkg/app/includes/tree.pxd", "cdef int d\n")
	writeTestModuleFile(files, "pkg/app/includes/config.pxd", "cdef int e\n")
	// Present in the same package but NOT cimported — must stay absent.
	writeTestModuleFile(files, "pkg/app/includes/other.pxd", "cdef int f\n")
	writeTestModuleFile(files, "pkg/x.pxi", "cdef int g\n")
	writeTestModuleFile(files, "contrib/tools/cython/Cython/Includes/libc/__init__.pxd", "")
	writeTestModuleFile(files, "contrib/tools/cython/Cython/Includes/libc/limits.pxd", "cdef int h\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/mod.pyx.cpp")

	counts := map[string]int{}
	for _, in := range cy.flatInputs() {
		counts[in.string()]++
	}

	for _, want := range []string{
		"$(S)/pkg/app/__init__.pxd",
		"$(S)/pkg/app/top.pxd",
		"$(S)/pkg/app/includes/__init__.pxd",
		"$(S)/pkg/app/includes/tree.pxd",
		"$(S)/pkg/app/includes/config.pxd",
		"$(S)/pkg/x.pxi",
		"$(S)/contrib/tools/cython/Cython/Includes/libc/limits.pxd",
	} {
		switch counts[want] {
		case 0:
			t.Fatalf("CY node missing name-list cimport input %q; inputs=%v", want, cy.flatInputs())
		case 1:
		default:
			t.Fatalf("CY node lists cimport input %q %d times, want exactly once", want, counts[want])
		}
	}

	if counts["$(S)/pkg/app/includes/other.pxd"] != 0 {
		t.Fatalf("CY node over-collects un-cimported package sibling $(S)/pkg/app/includes/other.pxd; inputs=%v", cy.flatInputs())
	}
}

func TestGen_CythonApiHeaderPyxClosurePassThrough(t *testing.T) {
	// A cython source that cdef-externs a generated `_api.h` Uses the producing
	// node's pyx closure as its own source deps. PY_SRCS lists the consumer (cons)
	// BEFORE the producer (prod), so phase 1 must register every header output up
	// front for the consumer's closure to resolve the api header.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(FOR cython pkg)\n"+
			"PY_SRCS(TOP_LEVEL CYTHON_C app/cons.pyx CYTHON_C_API_H app/prod.pyx)\nEND()\n")
	// Producer: pyx closure is prod.pyx + helper.pxd + h.pxi.
	writeTestModuleFile(files, "pkg/app/prod.pyx",
		"from app cimport helper\ninclude \"app/h.pxi\"\n")
	writeTestModuleFile(files, "pkg/app/helper.pxd", "cdef int a\n")
	writeTestModuleFile(files, "pkg/app/h.pxi", "cdef int b\n")
	// Consumer: cimports a pxd that cdef-externs the producer's generated _api.h.
	writeTestModuleFile(files, "pkg/app/cons.pyx", "from app.pub cimport thing\n")
	writeTestModuleFile(files, "pkg/app/pub.pxd", "cdef extern from \"prod_api.h\":\n    pass\n")
	// Present but NOT in the producer's closure — must stay absent.
	writeTestModuleFile(files, "pkg/app/unrelated.pxd", "cdef int z\n")

	g := testGen(newMemFS(files), "pkg")

	cons := mustNodeByOutput(t, g, "$(B)/pkg/app/cons.pyx.c")

	counts := map[string]int{}
	for _, in := range cons.flatInputs() {
		counts[in.string()]++
	}

	for _, want := range []string{
		"$(S)/pkg/app/prod.pyx",
		"$(S)/pkg/app/helper.pxd",
		"$(S)/pkg/app/h.pxi",
	} {
		switch counts[want] {
		case 0:
			t.Fatalf("consumer CY node missing producer pyx-closure input %q; inputs=%v", want, cons.flatInputs())
		case 1:
		default:
			t.Fatalf("consumer CY node lists producer pyx-closure input %q %d times, want exactly once", want, counts[want])
		}
	}

	if counts["$(S)/pkg/app/unrelated.pxd"] != 0 {
		t.Fatalf("consumer CY node pulls un-cimported sibling $(S)/pkg/app/unrelated.pxd; inputs=%v", cons.flatInputs())
	}
}

func TestGen_CythonGeneratedCompileCarriesInducedPyx(t *testing.T) {
	// The "pyx" passthrough rides the producer's "pyx" closure through files that
	// #include any of its outputs. The generated `cons.pyx.c` #includes `prod_api.h`
	// (main output `prod.c`), so its C++ compile Uses the producer's pyx closure and
	// lists prod.c. A hand-written C++ compile that does NOT #include it stays clean.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nSRCS(helper.cpp)\n"+
			"ADDINCL(FOR cython pkg)\n"+
			"PY_SRCS(TOP_LEVEL CYTHON_C app/cons.pyx CYTHON_C_API_H app/prod.pyx)\nEND()\n")
	// Producer: pyx closure is prod.pyx + helper.pxd + h.pxi; main output prod.c.
	writeTestModuleFile(files, "pkg/app/prod.pyx", "from app cimport helper\ninclude \"app/h.pxi\"\n")
	writeTestModuleFile(files, "pkg/app/helper.pxd", "cdef int a\n")
	writeTestModuleFile(files, "pkg/app/h.pxi", "cdef int b\n")
	// Consumer: the generated cons.pyx.c #includes prod_api.h.
	writeTestModuleFile(files, "pkg/app/cons.pyx", "from app.pub cimport thing\n")
	writeTestModuleFile(files, "pkg/app/pub.pxd", "cdef extern from \"prod_api.h\":\n    pass\n")
	// Present but NOT in the producer's closure — must stay absent.
	writeTestModuleFile(files, "pkg/app/unrelated.pxd", "cdef int z\n")
	// Hand-written C++ compile that does not #include the api header.
	writeTestModuleFile(files, "pkg/helper.cpp", "int f(){return 0;}\n")

	g := testGen(newMemFS(files), "pkg")

	compile := mustNodeByOutput(t, g, "$(B)/pkg/_/app/cons.pyx.c.o")

	counts := map[string]int{}
	for _, in := range compile.flatInputs() {
		counts[in.string()]++
	}

	for _, want := range []string{
		"$(S)/pkg/app/prod.pyx",
		"$(S)/pkg/app/helper.pxd",
		"$(S)/pkg/app/h.pxi",
		"$(B)/pkg/app/prod.c",
	} {
		switch counts[want] {
		case 0:
			t.Fatalf("generated compile missing producer induced input %q; inputs=%v", want, compile.flatInputs())
		case 1:
		default:
			t.Fatalf("generated compile lists producer induced input %q %d times, want exactly once", want, counts[want])
		}
	}

	if counts["$(S)/pkg/app/unrelated.pxd"] != 0 {
		t.Fatalf("generated compile pulls un-cimported sibling $(S)/pkg/app/unrelated.pxd; inputs=%v", compile.flatInputs())
	}

	// The hand-written C++ compile must not gain the producer's pyx source closure.
	helper := mustNodeByOutput(t, g, "$(B)/pkg/helper.cpp.o")
	for _, in := range helper.flatInputs() {
		switch in.string() {
		case "$(S)/pkg/app/prod.pyx", "$(S)/pkg/app/helper.pxd", "$(S)/pkg/app/h.pxi":
			t.Fatalf("hand-written C++ compile helper.cpp.o wrongly carries producer pyx closure input %q; inputs=%v", in.string(), helper.flatInputs())
		}
	}
}

func TestGen_CythonCimportFromModulePxdSuppressesNameList(t *testing.T) {
	// CimportFrom: once `from X cimport names`'s module `X.pxd` resolves,
	// needCheckLists=false — the per-name submodule probes are skipped. With `X.pxd`
	// reachable through one addincl root and `X/name.pxd` through another, only
	// `X.pxd` must ride the CY node.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(FOR cython pkg/ra pkg/rb)\n"+
			"PY_SRCS(NAMESPACE pkg mod.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/mod.pyx", "from sub cimport leaf\n")
	// Module `sub.pxd` via root ra resolves, sets needCheckLists=false.
	writeTestModuleFile(files, "pkg/ra/sub.pxd", "cdef int a\n")
	// Submodule `sub/leaf.pxd` via root rb must be skipped.
	writeTestModuleFile(files, "pkg/rb/sub/leaf.pxd", "cdef int b\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/mod.pyx.cpp")

	got := map[string]bool{}
	for _, in := range cy.flatInputs() {
		got[in.string()] = true
	}

	if !got["$(S)/pkg/ra/sub.pxd"] {
		t.Fatalf("CY node missing module pxd $(S)/pkg/ra/sub.pxd; inputs=%v", cy.flatInputs())
	}

	if got["$(S)/pkg/rb/sub/leaf.pxd"] {
		t.Fatalf("CY node over-collects submodule $(S)/pkg/rb/sub/leaf.pxd after module sub.pxd resolved (needCheckLists must be false); inputs=%v", cy.flatInputs())
	}
}

func TestGen_CythonCimportSimpleFirstResolvedFallback(t *testing.T) {
	// CimportSimple: `cimport a.b` resolves `a/b.pxd`, falling back to
	// `a/b/__init__.pxd`. With both reachable through different addincl roots, only
	// `sub/leaf.pxd` must ride the CY node.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(FOR cython pkg/ra pkg/rb)\n"+
			"PY_SRCS(NAMESPACE pkg mod.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/mod.pyx", "cimport sub.leaf\n")
	// `sub/leaf.pxd` via root ra wins.
	writeTestModuleFile(files, "pkg/ra/sub/leaf.pxd", "cdef int a\n")
	// `sub/leaf/__init__.pxd` via root rb is the skipped fallback.
	writeTestModuleFile(files, "pkg/rb/sub/leaf/__init__.pxd", "cdef int b\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/mod.pyx.cpp")

	got := map[string]bool{}
	for _, in := range cy.flatInputs() {
		got[in.string()] = true
	}

	if !got["$(S)/pkg/ra/sub/leaf.pxd"] {
		t.Fatalf("CY node missing module pxd $(S)/pkg/ra/sub/leaf.pxd; inputs=%v", cy.flatInputs())
	}

	if got["$(S)/pkg/rb/sub/leaf/__init__.pxd"] {
		t.Fatalf("CY node over-collects package fallback $(S)/pkg/rb/sub/leaf/__init__.pxd after sub/leaf.pxd resolved; inputs=%v", cy.flatInputs())
	}
}

func TestGen_CythonCimportFromNameFirstResolvedFallback(t *testing.T) {
	// CimportFrom per-name: `from X cimport name` resolves `X/name/__init__.pxd`,
	// falling back to `X/name.pxd`. With the module `sub.pxd` absent and both forms
	// reachable through different roots, only `sub/leaf/__init__.pxd` must ride.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(FOR cython pkg/ra pkg/rb)\n"+
			"PY_SRCS(NAMESPACE pkg mod.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/mod.pyx", "from sub cimport leaf\n")
	// `sub/leaf/__init__.pxd` via root ra wins.
	writeTestModuleFile(files, "pkg/ra/sub/leaf/__init__.pxd", "cdef int a\n")
	// Module-form `sub/leaf.pxd` via root rb is the skipped fallback.
	writeTestModuleFile(files, "pkg/rb/sub/leaf.pxd", "cdef int b\n")

	g := testGen(newMemFS(files), "pkg")

	cy := mustNodeByOutput(t, g, "$(B)/pkg/mod.pyx.cpp")

	got := map[string]bool{}
	for _, in := range cy.flatInputs() {
		got[in.string()] = true
	}

	if !got["$(S)/pkg/ra/sub/leaf/__init__.pxd"] {
		t.Fatalf("CY node missing submodule pxd $(S)/pkg/ra/sub/leaf/__init__.pxd; inputs=%v", cy.flatInputs())
	}

	if got["$(S)/pkg/rb/sub/leaf.pxd"] {
		t.Fatalf("CY node over-collects module-form fallback $(S)/pkg/rb/sub/leaf.pxd after sub/leaf/__init__.pxd resolved; inputs=%v", cy.flatInputs())
	}
}

func TestGen_ManualCompanionSourceUsesCythonCompanionCCInputs(t *testing.T) {
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make", "PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\nSRCS(helper.cpp)\nPY_SRCS(NAMESPACE pkg mod.pyx)\nEND()\n")
	writeTestModuleFile(files, "pkg/helper.cpp", "int f(){return 0;}\n")
	writeTestModuleFile(files, "pkg/mod.pyx", "def f():\n    return 0\n")

	g := testGen(newMemFS(files), "pkg")
	helper := mustNodeByOutput(t, g, "$(B)/pkg/helper.cpp.o")
	args := helper.Cmds[0].CmdArgs.flat()

	pythonIncludeIdx := indexOfArg(args, "-I$(S)/contrib/libs/python/Include")
	if pythonIncludeIdx < 0 {
		t.Fatalf("helper.cpp.o cmd_args missing python include: %#v", args)
	}

	wantNumpy := []string{
		"-I$(S)/contrib/python/numpy/include/numpy/core/include",
		"-I$(S)/contrib/python/numpy/include/numpy/core/include/numpy",
		"-I$(S)/contrib/python/numpy/include/numpy/core/src/common",
		"-I$(S)/contrib/python/numpy/include/numpy/core/src/npymath",
		"-I$(S)/contrib/python/numpy/include/numpy/distutils/include",
	}

	if pythonIncludeIdx+1+len(wantNumpy) > len(args) {
		t.Fatalf("helper.cpp.o cmd_args too short for numpy include bundle: %#v", args)
	}

	for i, want := range wantNumpy {
		if got := args[pythonIncludeIdx+1+i].string(); got != want {
			t.Fatalf("numpy include bundle mismatch at offset %d: got %q, want %q; cmd_args=%#v", i, got, want, args)
		}
	}

	for _, arg := range strStrs(args) {
		if strings.HasPrefix(arg, "-DPyInit_") || strings.HasPrefix(arg, "-Dinit_module_") {
			t.Fatalf("helper.cpp.o cmd_args still carry PY_REGISTER define %q: %#v", arg, args)
		}
	}
}

func TestGen_CythonCHeaderPassesInducedClosureToHandwrittenSrc(t *testing.T) {
	// _H attaches the induced "cpp"/"pyx" closure to the generated .h, which passes
	// through to any file that #includes it — including a handwritten SRCS C source.
	// That consumer lists the generated header, the cython main output, and the
	// pyx-language closure — but NOT the source's own `cdef extern` C closure.
	files := map[string]string{}

	writeTestModuleFile(files, "library/cpp/resource/ya.make", "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n")
	writeTestModuleFile(files, "pkg/ya.make",
		"PY3_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nNO_PYTHON_INCLUDES()\n"+
			"ADDINCL(${ARCADIA_BUILD_ROOT}/pkg/app FOR cython pkg)\n"+
			"SRCS(use.c)\n"+
			"PY_SRCS(TOP_LEVEL CYTHON_C_H app/prod.pyx)\nEND()\n")
	// Producer: pyx closure is prod.pyx + helper.pxd; it also cdef-externs a C header
	// that must NOT pass through to the C consumer.
	writeTestModuleFile(files, "pkg/app/prod.pyx", "from app cimport helper\ncdef extern from \"extlib/foo.h\":\n    pass\n")
	writeTestModuleFile(files, "pkg/app/helper.pxd", "cdef int a\n")
	writeTestModuleFile(files, "pkg/app/extlib/foo.h", "#pragma once\n")
	// Handwritten C source that #includes the Cython-generated header.
	writeTestModuleFile(files, "pkg/use.c", "#include \"prod.h\"\nint u(void){return 0;}\n")
	// A CYTHON_OUTPUT_INCLUDES infra header the generated header passes through.
	writeTestModuleFile(files, "contrib/tools/cython/generated_c_headers.h", "#pragma once\n")

	g := testGen(newMemFS(files), "pkg")

	use := mustNodeByOutput(t, g, "$(B)/pkg/use.c.o")

	present := map[string]bool{}
	for _, in := range use.flatInputs() {
		present[in.string()] = true
	}

	// The closure passing through the header: the generated header, the cython main
	// output, and the pyx-language closure.
	for _, want := range []string{
		"$(B)/pkg/app/prod.c",
		"$(B)/pkg/app/prod.h",
		"$(S)/pkg/app/prod.pyx",
		"$(S)/pkg/app/helper.pxd",
		// An input that ONLY reaches the consumer through the header's parsed-include
		// pass-through, so it pins the core path.
		"$(S)/contrib/tools/cython/generated_c_headers.h",
	} {
		if !present[want] {
			t.Fatalf("handwritten use.c.o missing induced header-closure input %q; inputs=%v", want, use.flatInputs())
		}
	}

	// The source's own `cdef extern` C closure must NOT pass through the header.
	if present["$(S)/pkg/app/extlib/foo.h"] {
		t.Fatalf("handwritten use.c.o wrongly carries the source's cdef-extern C header; inputs=%v", use.flatInputs())
	}
}
